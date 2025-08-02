package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	models "io.winapps.journeyapp/internal/models/account"
	uniquelocationsmodels "io.winapps.journeyapp/internal/models/get_unique_locations"
)

// GetUniqueLocations handles fetching all unique locations for the authenticated user
func (h *EntryHandler) GetUniqueLocations(c *gin.Context) {
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

	ctx := context.Background()

	// Fetch unique locations from database
	locations, err := h.fetchUniqueLocations(ctx, userUID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch unique locations"})
		return
	}

	response := uniquelocationsmodels.GetUniqueLocationsResponse{
		Locations: locations,
	}

	c.JSON(http.StatusOK, response)
}

// fetchUniqueLocations retrieves all unique locations for a user
func (h *EntryHandler) fetchUniqueLocations(ctx context.Context, userUID string) ([]models.Location, error) {
	// Query to get unique locations based on a combination of fields
	// We'll use display_name as the primary uniqueness criteria, but fall back to coordinates if display_name is empty
	query := `
		SELECT DISTINCT ON (
			COALESCE(NULLIF(l.display_name, ''), l.latitude::text || ',' || l.longitude::text)
		)
		l.latitude, l.longitude, l.address, l.city, l.state, l.zip, l.country, l.country_code, l.display_name
		FROM locations l
		INNER JOIN entries e ON l.entry_id = e.id
		WHERE e.user_uid = $1
		ORDER BY COALESCE(NULLIF(l.display_name, ''), l.latitude::text || ',' || l.longitude::text), l.created_at DESC
	`

	rows, err := h.postgres.Query(ctx, query, userUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var locations []models.Location
	for rows.Next() {
		var location models.Location
		if err := rows.Scan(
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
			return nil, err
		}
		locations = append(locations, location)
	}

	return locations, nil
}