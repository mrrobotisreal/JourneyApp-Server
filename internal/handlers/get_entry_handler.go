package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	models "io.winapps.journeyapp/internal/models/account"
	getentrymodels "io.winapps.journeyapp/internal/models/get_entry"
)

// GetEntry handles fetching a specific journal entry with all its data
func (h *EntryHandler) GetEntry(c *gin.Context) {
	var req getentrymodels.GetEntryRequest
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

	// Check Redis cache first
	redisKey := fmt.Sprintf("entry:%s", req.EntryID)
	cachedEntry, err := h.redis.Get(ctx, redisKey).Result()
	if err == nil && cachedEntry != "" {
		var entry getentrymodels.GetEntryResponse
		if err := json.Unmarshal([]byte(cachedEntry), &entry); err == nil {
			c.JSON(http.StatusOK, entry)
			return
		}
	}

	// Fetch entry from database
	entry, err := h.fetchEntryWithDetails(ctx, req.EntryID, userUID)
	if err != nil {
		if err.Error() == "entry not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entry not found or access denied"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch entry"})
		return
	}

	// Cache the entry in Redis
	entryJSON, err := json.Marshal(entry)
	if err == nil {
		h.redis.Set(ctx, redisKey, entryJSON, 24*time.Hour)
	}

	c.JSON(http.StatusOK, entry)
}

// fetchEntryWithDetails retrieves an entry with all its related data
func (h *EntryHandler) fetchEntryWithDetails(ctx context.Context, entryID, userUID string) (*getentrymodels.GetEntryResponse, error) {
	// First, get the basic entry information
	var entry getentrymodels.GetEntryResponse
	entryQuery := `
		SELECT id, title, description, created_at, updated_at
		FROM entries
		WHERE id = $1 AND user_uid = $2
	`
	err := h.postgres.QueryRow(ctx, entryQuery, entryID, userUID).Scan(
		&entry.ID,
		&entry.Title,
		&entry.Description,
		&entry.CreatedAt,
		&entry.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("entry not found")
	}

	// Initialize slices
	entry.Images = []string{}
	entry.Tags = []models.Tag{}
	entry.Locations = []models.Location{}

	// Fetch tags
	tagsQuery := `
		SELECT key, value FROM tags WHERE entry_id = $1 ORDER BY created_at
	`
	tagRows, err := h.postgres.Query(ctx, tagsQuery, entryID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tags: %w", err)
	}
	defer tagRows.Close()

	for tagRows.Next() {
		var tag models.Tag
		if err := tagRows.Scan(&tag.Key, &tag.Value); err != nil {
			return nil, fmt.Errorf("failed to scan tag: %w", err)
		}
		entry.Tags = append(entry.Tags, tag)
	}

	// Fetch locations
	locationsQuery := `
		SELECT latitude, longitude, address, city, state, zip, country, country_code, display_name
		FROM locations WHERE entry_id = $1 ORDER BY created_at
	`
	locationRows, err := h.postgres.Query(ctx, locationsQuery, entryID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch locations: %w", err)
	}
	defer locationRows.Close()

	for locationRows.Next() {
		var location models.Location
		if err := locationRows.Scan(
			&location.Latitude,
			&location.Longitude,
			&location.Address,
			&location.City,
			&location.State,
			&location.Zip,
			&location.Country,
			&location.CountryCode,
			&location.DisplayName,
		); err != nil {
			return nil, fmt.Errorf("failed to scan location: %w", err)
		}
		entry.Locations = append(entry.Locations, location)
	}

	// Fetch images
	imagesQuery := `
		SELECT url FROM images WHERE entry_id = $1 ORDER BY upload_order
	`
	imageRows, err := h.postgres.Query(ctx, imagesQuery, entryID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch images: %w", err)
	}
	defer imageRows.Close()

	for imageRows.Next() {
		var imageURL string
		if err := imageRows.Scan(&imageURL); err != nil {
			return nil, fmt.Errorf("failed to scan image: %w", err)
		}
		entry.Images = append(entry.Images, imageURL)
	}

	// Fetch audio
	audioQuery := `
		SELECT url FROM audio WHERE entry_id = $1 ORDER BY upload_order
	`
	audioRows, err := h.postgres.Query(ctx, audioQuery, entryID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch audio: %w", err)
	}
	defer audioRows.Close()

	for audioRows.Next() {
		var audioURL string
		if err := audioRows.Scan(&audioURL); err != nil {
			return nil, fmt.Errorf("failed to scan audio: %w", err)
		}
		entry.Audio = append(entry.Audio, audioURL)
	}

	return &entry, nil
}