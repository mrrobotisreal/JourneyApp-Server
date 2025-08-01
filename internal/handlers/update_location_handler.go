package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	updatelocationmodels "io.winapps.journeyapp/internal/models/update_location"
)

// UpdateLocation handles updating a location in an existing journal entry
func (h *EntryHandler) UpdateLocation(c *gin.Context) {
	var req updatelocationmodels.UpdateLocationRequest
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

	// Check if old location exists
	var oldLocationExists bool
	oldLocationCheckQuery := `
		SELECT EXISTS(SELECT 1 FROM locations WHERE entry_id = $1 AND latitude = $2 AND longitude = $3)
	`
	err = h.postgres.QueryRow(ctx, oldLocationCheckQuery, req.EntryID, req.OldLocation.Latitude, req.OldLocation.Longitude).Scan(&oldLocationExists)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check existing location"})
		return
	}

	if !oldLocationExists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Old location not found"})
		return
	}

	// Check if new location already exists (prevent duplicates) if coordinates are different
	if req.OldLocation.Latitude != req.NewLocation.Latitude || req.OldLocation.Longitude != req.NewLocation.Longitude {
		if req.NewLocation.Latitude != 0 && req.NewLocation.Longitude != 0 {
			var newLocationExists bool
			newLocationCheckQuery := `
				SELECT EXISTS(SELECT 1 FROM locations WHERE entry_id = $1 AND latitude = $2 AND longitude = $3)
			`
			err = h.postgres.QueryRow(ctx, newLocationCheckQuery, req.EntryID, req.NewLocation.Latitude, req.NewLocation.Longitude).Scan(&newLocationExists)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check new location"})
				return
			}

			if newLocationExists {
				c.JSON(http.StatusConflict, gin.H{"error": "New location coordinates already exist for this entry"})
				return
			}
		}
	}

	// Start database transaction
	tx, err := h.postgres.Begin(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start database transaction"})
		return
	}
	defer tx.Rollback(ctx)

	// Update location
	now := time.Now()
	locationQuery := `
		UPDATE locations SET latitude = $1, longitude = $2, address = $3, city = $4, state = $5, zip = $6,
		country = $7, country_code = $8, display_name = $9, created_at = $10
		WHERE entry_id = $11 AND latitude = $12 AND longitude = $13
	`
	result, err := tx.Exec(ctx, locationQuery,
		req.NewLocation.Latitude,
		req.NewLocation.Longitude,
		req.NewLocation.Address,
		req.NewLocation.City,
		req.NewLocation.State,
		req.NewLocation.Zip,
		req.NewLocation.Country,
		req.NewLocation.CountryCode,
		req.NewLocation.DisplayName,
		now,
		req.EntryID,
		req.OldLocation.Latitude,
		req.OldLocation.Longitude,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update location"})
		return
	}

	rowsAffected := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Location not found"})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save location update"})
		return
	}

	// Invalidate Redis cache for this entry
	redisKey := "entry:" + req.EntryID
	h.redis.Del(ctx, redisKey)

	// Create response
	response := updatelocationmodels.UpdateLocationResponse{
		EntryID:     req.EntryID,
		OldLocation: req.OldLocation,
		NewLocation: req.NewLocation,
		Message:     "Location updated successfully",
	}

	c.JSON(http.StatusOK, response)
}