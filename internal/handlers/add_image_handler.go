package handlers

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	addimagemodels "io.winapps.journeyapp/internal/models/add_image"
)

// AddImage handles adding an image to an existing journal entry
func (h *EntryHandler) AddImage(c *gin.Context) {
	var req addimagemodels.AddImageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	// Get UID from context (set by auth middleware)
	uid, exists := c.Get("uid")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	userUID, ok := uid.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user context"})
		return
	}

	// Validate required fields
	if req.EntryID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Entry ID is required"})
		return
	}

	if req.Image == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Image data is required"})
		return
	}

	ctx := context.Background()

	// Verify entry exists and belongs to user
	var entryExists bool
	entryCheckQuery := `
		SELECT EXISTS(SELECT 1 FROM entries WHERE id = $1 AND user_uid = $2)
	`
	err := h.postgres.QueryRow(ctx, entryCheckQuery, req.EntryID, userUID).Scan(&entryExists)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify entry"})
		return
	}

	if !entryExists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Entry not found or access denied"})
		return
	}

	// Process and save the image
	imageURL, err := h.saveImageToFileSystem(req.Image, userUID, req.EntryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save image: " + err.Error()})
		return
	}

	// Get the current highest upload_order for this entry to set the new order
	var maxOrder int
	orderQuery := `
		SELECT COALESCE(MAX(upload_order), -1) FROM images WHERE entry_id = $1
	`
	err = h.postgres.QueryRow(ctx, orderQuery, req.EntryID).Scan(&maxOrder)
	if err != nil {
		// Clean up the saved file on error
		os.Remove(imageURL)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to determine image order"})
		return
	}

	// Start database transaction
	tx, err := h.postgres.Begin(ctx)
	if err != nil {
		// Clean up the saved file on error
		os.Remove(imageURL)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start database transaction"})
		return
	}
	defer tx.Rollback(ctx)

	// Insert new image with URL
	now := time.Now()
	newOrder := maxOrder + 1
	imageQuery := `
		INSERT INTO images (entry_id, url, upload_order, created_at)
		VALUES ($1, $2, $3, $4)
	`
	_, err = tx.Exec(ctx, imageQuery, req.EntryID, imageURL, newOrder, now)
	if err != nil {
		// Clean up the saved file on error
		os.Remove(imageURL)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add image"})
		return
	}

	// Update entry's updated_at timestamp
	updateEntryQuery := `
		UPDATE entries SET updated_at = $1 WHERE id = $2
	`
	_, err = tx.Exec(ctx, updateEntryQuery, now, req.EntryID)
	if err != nil {
		// Clean up the saved file on error
		os.Remove(imageURL)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update entry timestamp"})
		return
	}

	// Commit transaction
	if err = tx.Commit(ctx); err != nil {
		// Clean up the saved file on error
		os.Remove(imageURL)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save image"})
		return
	}

	// Invalidate Redis cache for this entry
	redisKey := "entry:" + req.EntryID
	h.redis.Del(ctx, redisKey)

	// Create response
	response := addimagemodels.AddImageResponse{
		EntryID:  req.EntryID,
		ImageURL: imageURL,
		Message:  "Image added successfully",
	}

	c.JSON(http.StatusOK, response)
}

// saveImageToFileSystem saves the base64 encoded image to the file system
func (h *EntryHandler) saveImageToFileSystem(base64Image, userUID, entryID string) (string, error) {
	// Strip data URL prefix if present (e.g., "data:image/png;base64,")
	if strings.Contains(base64Image, ",") {
		parts := strings.Split(base64Image, ",")
		if len(parts) > 1 {
			base64Image = parts[1]
		}
	}

	// Decode base64 image
	imageData, err := base64.StdEncoding.DecodeString(base64Image)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64 image: %w", err)
	}

	// Detect file extension from image data
	var ext string
	if len(imageData) >= 4 {
		// Check for common image format signatures
		switch {
		case imageData[0] == 0xFF && imageData[1] == 0xD8 && imageData[2] == 0xFF:
			ext = ".jpg"
		case imageData[0] == 0x89 && imageData[1] == 0x50 && imageData[2] == 0x4E && imageData[3] == 0x47:
			ext = ".png"
		case imageData[0] == 0x47 && imageData[1] == 0x49 && imageData[2] == 0x46:
			ext = ".gif"
		case imageData[0] == 0x52 && imageData[1] == 0x49 && imageData[2] == 0x46 && imageData[3] == 0x46:
			ext = ".webp"
		default:
			ext = ".jpg" // Default to jpg if format is unknown
		}
	} else {
		ext = ".jpg"
	}

	// Create directory structure: internal/images/{userUID}/{entryID}/
	userDir := filepath.Join("internal", "images", userUID)
	entryDir := filepath.Join(userDir, entryID)

	// Create user directory if it doesn't exist
	if err := os.MkdirAll(userDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create user directory: %w", err)
	}

	// Create entry directory if it doesn't exist
	if err := os.MkdirAll(entryDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create entry directory: %w", err)
	}

	// Generate unique filename
	imageID := uuid.New().String()
	filename := imageID + ext
	filePath := filepath.Join(entryDir, filename)

	// Write image data to file
	if err := os.WriteFile(filePath, imageData, 0644); err != nil {
		return "", fmt.Errorf("failed to write image file: %w", err)
	}

	// Return the URL path for accessing the image
	// This will be served by the static file server we'll add to main.go
	imageURL := fmt.Sprintf("/images/%s/%s/%s", userUID, entryID, filename)

	return imageURL, nil
}