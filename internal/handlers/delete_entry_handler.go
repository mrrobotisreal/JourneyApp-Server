package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	deleteentrymodels "io.winapps.journeyapp/internal/models/delete_entry"
)

// DeleteEntry handles the deletion of an entry
func (h *EntryHandler) DeleteEntry(c *gin.Context) {
	var req deleteentrymodels.DeleteEntryRequest
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

	ctx := context.Background()

	// Delete entry from database
	tx, err := h.postgres.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
		return
	}

	// Delete entry from database
	query := `
		DELETE FROM entries
		WHERE id = $1 AND user_uid = $2
	`
	_, err = tx.Exec(ctx, query, req.EntryID, userUID)
	if err != nil {
		_ = tx.Rollback(ctx)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete entry"})
		return
	}

	// Delete entry from Redis cache
	redisKey := fmt.Sprintf("entry:%s", req.EntryID)
	if err := h.redis.Del(ctx, redisKey).Err(); err != nil {
		_ = tx.Rollback(ctx)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete entry from Redis cache"})
		return
	}

	// Commit transaction
	if err = tx.Commit(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete entry"})
		return
	}

	// Return success response
	c.JSON(http.StatusOK, gin.H{"isDeleted": true, "message": "Entry deleted successfully"})
}