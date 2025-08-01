package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	addlocationmodels "io.winapps.journeyapp/internal/models/add_location"
)

// AddLocation handles adding a location to an existing journal entry
func (h *EntryHandler) AddLocation(c *gin.Context) {
	var req addlocationmodels.AddLocationRequest
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

	// Check if location already exists for this entry (matching coordinates)
	if req.Location.Latitude != 0 && req.Location.Longitude != 0 {
		var locationExists bool
		locationCheckQuery := `
			SELECT EXISTS(SELECT 1 FROM locations WHERE entry_id = $1 AND latitude = $2 AND longitude = $3)
		`
		err = h.postgres.QueryRow(ctx, locationCheckQuery, req.EntryID, req.Location.Latitude, req.Location.Longitude).Scan(&locationExists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check existing location"})
			return
		}

		if locationExists {
			c.JSON(http.StatusConflict, gin.H{"error": "Location with these coordinates already exists for this entry"})
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

	// Insert new location
	now := time.Now()
	locationQuery := `
		INSERT INTO locations (entry_id, latitude, longitude, address, city, state, zip, country, country_code, display_name, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err = tx.Exec(ctx, locationQuery,
		req.EntryID,
		req.Location.Latitude,
		req.Location.Longitude,
		req.Location.Address,
		req.Location.City,
		req.Location.State,
		req.Location.Zip,
		req.Location.Country,
		req.Location.CountryCode,
		req.Location.DisplayName,
		now,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add location"})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save location"})
		return
	}

	// Invalidate Redis cache for this entry
	redisKey := "entry:" + req.EntryID
	h.redis.Del(ctx, redisKey)

	// Create response
	response := addlocationmodels.AddLocationResponse{
		EntryID:  req.EntryID,
		Location: req.Location,
		Message:  "Location added successfully",
	}

	c.JSON(http.StatusOK, response)
}