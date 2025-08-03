package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	models "io.winapps.journeyapp/internal/models/account"
	updateentrymodels "io.winapps.journeyapp/internal/models/update_entry"
)

// UpdateEntry handles updating the title and/or description of an entry
func (h *EntryHandler) UpdateEntry(c *gin.Context) {
	var req updateentrymodels.UpdateEntryRequest
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

	// At least one field must be provided for update
	if req.Title == "" && req.Description == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "At least title or description must be provided"})
		return
	}

	ctx := context.Background()

	// Update the entry
	updatedEntry, err := h.updateEntryFields(ctx, req.EntryID, userUID, req.Title, req.Description)
	if err != nil {
		if err.Error() == "entry not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entry not found or access denied"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update entry"})
		return
	}

	c.JSON(http.StatusOK, updatedEntry)
}

// updateEntryFields updates the entry title and/or description in the database
func (h *EntryHandler) updateEntryFields(ctx context.Context, entryID, userUID, title, description string) (*updateentrymodels.UpdateEntryResponse, error) {
	// Start transaction
	tx, err := h.postgres.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Build dynamic update query based on provided fields
	updateFields := []string{}
	args := []interface{}{}
	argCounter := 1

		if title != "" {
		updateFields = append(updateFields, "title = $"+strconv.Itoa(argCounter))
		args = append(args, title)
		argCounter++
	}

	if description != "" {
		updateFields = append(updateFields, "description = $"+strconv.Itoa(argCounter))
		args = append(args, description)
		argCounter++
	}

	// Add updated_at timestamp
	now := time.Now()
	updateFields = append(updateFields, "updated_at = $"+strconv.Itoa(argCounter))
	args = append(args, now)
	argCounter++

	// Add WHERE clause parameters
	args = append(args, entryID, userUID)

	updateQuery := `
		UPDATE entries
		SET ` + strings.Join(updateFields, ", ") + `
		WHERE id = $` + strconv.Itoa(argCounter) + ` AND user_uid = $` + strconv.Itoa(argCounter+1) + `
	`

	// Execute update
	result, err := tx.Exec(ctx, updateQuery, args...)
	if err != nil {
		return nil, err
	}

	// Check if any rows were affected
	if result.RowsAffected() == 0 {
		return nil, fmt.Errorf("entry not found")
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	// Fetch the updated entry with all its data
	return h.fetchUpdatedEntryWithDetails(ctx, entryID, userUID)
}

// fetchUpdatedEntryWithDetails retrieves the updated entry with all its related data
func (h *EntryHandler) fetchUpdatedEntryWithDetails(ctx context.Context, entryID, userUID string) (*updateentrymodels.UpdateEntryResponse, error) {
	// Get the basic entry information
	var entry updateentrymodels.UpdateEntryResponse
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