package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	updatetagmodels "io.winapps.journeyapp/internal/models/update_tag"
)

// UpdateTag handles updating a tag in an existing journal entry
func (h *EntryHandler) UpdateTag(c *gin.Context) {
	var req updatetagmodels.UpdateTagRequest
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

	if req.OldTag.Key == "" || req.NewTag.Key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Both old and new tag keys are required"})
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

	// Check if old tag exists
	var oldTagExists bool
	oldTagCheckQuery := `
		SELECT EXISTS(SELECT 1 FROM tags WHERE entry_id = $1 AND key = $2 AND value = $3)
	`
	err = h.postgres.QueryRow(ctx, oldTagCheckQuery, req.EntryID, req.OldTag.Key, req.OldTag.Value).Scan(&oldTagExists)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check existing tag"})
		return
	}

	if !oldTagExists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Old tag not found"})
		return
	}

	// Check if new tag already exists (prevent duplicates)
	if req.OldTag.Key != req.NewTag.Key || req.OldTag.Value != req.NewTag.Value {
		var newTagExists bool
		newTagCheckQuery := `
			SELECT EXISTS(SELECT 1 FROM tags WHERE entry_id = $1 AND key = $2 AND value = $3)
		`
		err = h.postgres.QueryRow(ctx, newTagCheckQuery, req.EntryID, req.NewTag.Key, req.NewTag.Value).Scan(&newTagExists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check new tag"})
			return
		}

		if newTagExists {
			c.JSON(http.StatusConflict, gin.H{"error": "New tag already exists for this entry"})
			return
		}
	}

	// Start database transaction
	tx, err := h.postgres.Begin(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start database transaction"})
		return
	}
	defer tx.Rollback(ctx)

	// Update tag
	now := time.Now()
	tagQuery := `
		UPDATE tags SET key = $1, value = $2, created_at = $3
		WHERE entry_id = $4 AND key = $5 AND value = $6
	`
	result, err := tx.Exec(ctx, tagQuery, req.NewTag.Key, req.NewTag.Value, now, req.EntryID, req.OldTag.Key, req.OldTag.Value)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update tag"})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save tag update"})
		return
	}

	// Invalidate Redis cache for this entry
	redisKey := "entry:" + req.EntryID
	h.redis.Del(ctx, redisKey)

	// Create response
	response := updatetagmodels.UpdateTagResponse{
		EntryID: req.EntryID,
		OldTag:  req.OldTag,
		NewTag:  req.NewTag,
		Message: "Tag updated successfully",
	}

	c.JSON(http.StatusOK, response)
}