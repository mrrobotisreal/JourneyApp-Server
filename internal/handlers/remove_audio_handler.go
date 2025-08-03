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

	removeaudiomodels "io.winapps.journeyapp/internal/models/remove_audio"
)

// RemoveAudio handles removing an audio file from an existing journal entry
func (h *EntryHandler) RemoveAudio(c *gin.Context) {
	var req removeaudiomodels.RemoveAudioRequest
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

	if req.AudioURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Audio URL is required"})
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

	// Remove audio from database
	now := time.Now()
	audioQuery := `
		DELETE FROM audio WHERE entry_id = $1 AND url = $2
	`
	result, err := tx.Exec(ctx, audioQuery, req.EntryID, req.AudioURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove audio"})
		return
	}

	rowsAffected := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Audio not found"})
		return
	}

	// Delete the physical file
	if err := h.deleteAudioFile(req.AudioURL); err != nil {
		// Log the error but don't fail the request since the database record is already deleted
		fmt.Printf("Warning: failed to delete audio file %s: %v\n", req.AudioURL, err)
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove audio"})
		return
	}

	// Invalidate Redis cache for this entry
	redisKey := "entry:" + req.EntryID
	h.redis.Del(ctx, redisKey)

	// Create response
	response := removeaudiomodels.RemoveAudioResponse{
		EntryID:  req.EntryID,
		AudioURL: req.AudioURL,
		Message:  "Audio removed successfully",
	}

	c.JSON(http.StatusOK, response)
}

// deleteAudioFile deletes the physical audio file from the file system
func (h *EntryHandler) deleteAudioFile(audioURL string) error {
	// Extract the file path from the URL
	// audioURL format: "/audio/{userUID}/{entryID}/{filename}"
	if !strings.HasPrefix(audioURL, "/audio/") {
		return fmt.Errorf("invalid audio URL format: %s", audioURL)
	}

	// Remove the "/audio/" prefix
	relativePath := strings.TrimPrefix(audioURL, "/audio/")

	// Construct the full file path
	filePath := filepath.Join("internal", "audio", relativePath)

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