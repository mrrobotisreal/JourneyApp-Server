package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify entry"})
		return
	}

	if !entryExists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Entry not found or access denied"})
		return
	}

	// Check if image already exists for this entry
	var imageExists bool
	imageCheckQuery := `
		SELECT EXISTS(SELECT 1 FROM images WHERE entry_id = $1 AND url = $2)
	`
	err = h.postgres.QueryRow(ctx, imageCheckQuery, req.EntryID, req.ImageURL).Scan(&imageExists)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check existing image"})
		return
	}

	if imageExists {
		c.JSON(http.StatusConflict, gin.H{"error": "Image already exists for this entry"})
		return
	}

	// Get the current highest upload_order for this entry to set the new order
	var maxOrder int
	orderQuery := `
		SELECT COALESCE(MAX(upload_order), -1) FROM images WHERE entry_id = $1
	`
	err = h.postgres.QueryRow(ctx, orderQuery, req.EntryID).Scan(&maxOrder)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to determine image order"})
		return
	}

	// Start database transaction
	tx, err := h.postgres.Begin(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start database transaction"})
		return
	}
	defer tx.Rollback(ctx)

	// Insert new image
	now := time.Now()
	newOrder := maxOrder + 1
	imageQuery := `
		INSERT INTO images (entry_id, url, upload_order, created_at)
		VALUES ($1, $2, $3, $4)
	`
	_, err = tx.Exec(ctx, imageQuery, req.EntryID, req.ImageURL, newOrder, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add image"})
		return
	}

	// Update entry's updated_at timestamp
	updateEntryQuery := `
		UPDATE entries SET updated_at = $1 WHERE id = $2
	`
	_, err = tx.Exec(ctx, updateEntryQuery, now, req.EntryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update entry timestamp"})
		return
	}

	// Commit transaction
	if err = tx.Commit(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save image"})
		return
	}

	// Invalidate Redis cache for this entry
	redisKey := "entry:" + req.EntryID
	h.redis.Del(ctx, redisKey)

	// Create response
	response := addimagemodels.AddImageResponse{
		EntryID:  req.EntryID,
		ImageURL: req.ImageURL,
		Message:  "Image added successfully",
	}

	c.JSON(http.StatusOK, response)
}