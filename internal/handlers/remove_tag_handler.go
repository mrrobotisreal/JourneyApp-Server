package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	removetagmodels "io.winapps.journeyapp/internal/models/remove_tag"
)

// RemoveTag handles removing a tag from an existing journal entry
func (h *EntryHandler) RemoveTag(c *gin.Context) {
	var req removetagmodels.RemoveTagRequest
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

	if req.Tag.Key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tag key is required"})
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

	// Start database transaction
	tx, err := h.postgres.Begin(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start database transaction"})
		return
	}
	defer tx.Rollback(ctx)

	// Remove tag
	now := time.Now()
	tagQuery := `
		DELETE FROM tags WHERE entry_id = $1 AND key = $2 AND value = $3
	`
	result, err := tx.Exec(ctx, tagQuery, req.EntryID, req.Tag.Key, req.Tag.Value)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove tag"})
		return
	}

	rowsAffected := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found"})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove tag"})
		return
	}

	// Invalidate Redis cache for this entry
	redisKey := "entry:" + req.EntryID
	h.redis.Del(ctx, redisKey)

	// Create response
	response := removetagmodels.RemoveTagResponse{
		EntryID: req.EntryID,
		Tag:     req.Tag,
		Message: "Tag removed successfully",
	}

	c.JSON(http.StatusOK, response)
}