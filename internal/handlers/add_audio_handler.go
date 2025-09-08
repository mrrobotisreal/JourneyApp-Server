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

	addaudiomodels "io.winapps.journeyapp/internal/models/add_audio"
)

// AddAudio handles adding an audio file to an existing journal entry
func (h *EntryHandler) AddAudio(c *gin.Context) {
	var req addaudiomodels.AddAudioRequest
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

	if req.Audio == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Audio data is required"})
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

	// Process and save the audio
	audioURL, err := h.saveAudioToFileSystem(req.Audio, userUID, req.EntryID)
	if err != nil {
		h.logError(c, err, "save audio to filesystem failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save audio: " + err.Error()})
		return
	}

	// Get the current highest upload_order for this entry to set the new order
	var maxOrder int
	orderQuery := `
		SELECT COALESCE(MAX(upload_order), -1) FROM audio WHERE entry_id = $1
	`
	err = h.postgres.QueryRow(ctx, orderQuery, req.EntryID).Scan(&maxOrder)
	if err != nil {
		// Clean up the saved file on error
		os.Remove(audioURL)
		h.logError(c, err, "determine audio order failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to determine audio order"})
		return
	}

	// Start database transaction
	tx, err := h.postgres.Begin(ctx)
	if err != nil {
		// Clean up the saved file on error
		os.Remove(audioURL)
		h.logError(c, err, "begin transaction failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start database transaction"})
		return
	}
	defer tx.Rollback(ctx)

	// Insert new audio with URL
	now := time.Now()
	newOrder := maxOrder + 1
	audioQuery := `
		INSERT INTO audio (entry_id, url, upload_order, created_at)
		VALUES ($1, $2, $3, $4)
	`
	_, err = tx.Exec(ctx, audioQuery, req.EntryID, audioURL, newOrder, now)
	if err != nil {
		// Clean up the saved file on error
		os.Remove(audioURL)
		h.logError(c, err, "insert audio failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add audio"})
		return
	}

	// Update entry's updated_at timestamp
	updateEntryQuery := `
		UPDATE entries SET updated_at = $1 WHERE id = $2
	`
	_, err = tx.Exec(ctx, updateEntryQuery, now, req.EntryID)
	if err != nil {
		// Clean up the saved file on error
		os.Remove(audioURL)
		h.logError(c, err, "update entry timestamp failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update entry timestamp"})
		return
	}

	// Commit transaction
	if err = tx.Commit(ctx); err != nil {
		// Clean up the saved file on error
		os.Remove(audioURL)
		h.logError(c, err, "commit audio tx failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save audio"})
		return
	}

	// Invalidate Redis cache for this entry
	redisKey := "entry:" + req.EntryID
	h.redis.Del(ctx, redisKey)

	// Create response
	response := addaudiomodels.AddAudioResponse{
		EntryID:  req.EntryID,
		AudioURL: audioURL,
		Message:  "Audio added successfully",
	}

	c.JSON(http.StatusOK, response)
}

// saveAudioToFileSystem saves the base64 encoded audio to the file system
func (h *EntryHandler) saveAudioToFileSystem(base64Audio, userUID, entryID string) (string, error) {
	// Strip data URL prefix if present (e.g., "data:audio/mp3;base64,")
	if strings.Contains(base64Audio, ",") {
		parts := strings.Split(base64Audio, ",")
		if len(parts) > 1 {
			base64Audio = parts[1]
		}
	}

	// Decode base64 audio
	audioData, err := base64.StdEncoding.DecodeString(base64Audio)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64 audio: %w", err)
	}

	// Detect file extension from audio data
	var ext string
	if len(audioData) >= 4 {
		// Check for common audio format signatures
		switch {
		case len(audioData) >= 3 && audioData[0] == 0x49 && audioData[1] == 0x44 && audioData[2] == 0x33:
			ext = ".mp3" // ID3 tag (MP3 with metadata)
		case len(audioData) >= 11 && string(audioData[0:11]) == "FLV\x01\x05\x00\x00\x00\x09\x00\x00":
			ext = ".flv"
		case len(audioData) >= 4 && string(audioData[0:4]) == "OggS":
			ext = ".ogg"
		case len(audioData) >= 12 && string(audioData[8:12]) == "WAVE":
			ext = ".wav"
		case len(audioData) >= 8 && string(audioData[4:8]) == "ftyp":
			ext = ".m4a" // MP4 audio
		case len(audioData) >= 2 && audioData[0] == 0xFF && (audioData[1]&0xE0) == 0xE0:
			ext = ".mp3" // MP3 frame sync
		default:
			ext = ".mp3" // Default to mp3 if format is unknown
		}
	} else {
		ext = ".mp3"
	}

	// Create directory structure: internal/audio/{userUID}/{entryID}/
	userDir := filepath.Join("internal", "audio", userUID)
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
	audioID := uuid.New().String()
	filename := audioID + ext
	filePath := filepath.Join(entryDir, filename)

	// Write audio data to file
	if err := os.WriteFile(filePath, audioData, 0644); err != nil {
		return "", fmt.Errorf("failed to write audio file: %w", err)
	}

	// Return the URL path for accessing the audio
	// This will be served by the static file server we'll add to main.go
	audioURL := fmt.Sprintf("/audio/%s/%s/%s", userUID, entryID, filename)

	return audioURL, nil
}