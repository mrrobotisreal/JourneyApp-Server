package handlers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	removeimagemodels "io.winapps.journeyapp/internal/models/remove_image"
)

// RemoveImage handles removing an image from an existing journal entry
func (h *EntryHandler) RemoveImage(c *gin.Context) {
	var req removeimagemodels.RemoveImageRequest
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

	if req.ImageURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Image URL is required"})
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
		h.logError(c, err, "verify entry failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify entry"})
		return
	}

	if !entryExists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Entry not found or access denied"})
		return
	}

	// Start database transaction
	tx, err := h.postgres.Begin(ctx)
	if err != nil {
		h.logError(c, err, "begin transaction failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start database transaction"})
		return
	}
	defer tx.Rollback(ctx)

	// Remove image from database
	now := time.Now()
	imageQuery := `
		DELETE FROM images WHERE entry_id = $1 AND url = $2
	`
	result, err := tx.Exec(ctx, imageQuery, req.EntryID, req.ImageURL)
	if err != nil {
		h.logError(c, err, "delete image failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove image"})
		return
	}

	rowsAffected := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Image not found"})
		return
	}

	// Delete the physical file
	if err := h.deleteImageFile(req.ImageURL); err != nil {
		// Log the error but don't fail the request since the database record is already deleted
		h.logError(c, err, "delete image file failed", "image_url", req.ImageURL)
	}

	// Update entry's updated_at timestamp
	updateEntryQuery := `
		UPDATE entries SET updated_at = $1 WHERE id = $2
	`
	_, err = tx.Exec(ctx, updateEntryQuery, now, req.EntryID)
	if err != nil {
		h.logError(c, err, "update entry timestamp failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update entry timestamp"})
		return
	}

	// Commit transaction
	if err = tx.Commit(ctx); err != nil {
		h.logError(c, err, "commit remove image tx failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove image"})
		return
	}

	// Invalidate Redis cache for this entry
	redisKey := "entry:" + req.EntryID
	h.redis.Del(ctx, redisKey)

	// Create response
	response := removeimagemodels.RemoveImageResponse{
		EntryID:  req.EntryID,
		ImageURL: req.ImageURL,
		Message:  "Image removed successfully",
	}

	c.JSON(http.StatusOK, response)
}

// deleteImageFile deletes the physical image file from the file system
func (h *EntryHandler) deleteImageFile(imageURL string) error {
	// Extract the file path from the URL
	// imageURL format: "/images/{userUID}/{entryID}/{filename}"
	if !strings.HasPrefix(imageURL, "/images/") {
		return fmt.Errorf("invalid image URL format: %s", imageURL)
	}

	// Remove the "/images/" prefix
	relativePath := strings.TrimPrefix(imageURL, "/images/")

	// Construct the full file path
	filePath := filepath.Join("internal", "images", relativePath)

	// Check if file exists before trying to delete
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// File doesn't exist, which is fine - maybe it was already deleted
		return nil
	}

	// Delete the file
	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to delete file %s: %w", filePath, err)
	}

	return nil
}