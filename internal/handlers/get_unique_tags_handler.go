package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	models "io.winapps.journeyapp/internal/models/account"
	uniquetagsmodels "io.winapps.journeyapp/internal/models/get_unique_tags"
)

// GetUniqueTags handles fetching all unique tag keys for the authenticated user
func (h *EntryHandler) GetUniqueTags(c *gin.Context) {
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

	// Fetch unique tags from database
	tags, err := h.fetchUniqueTags(ctx, userUID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch unique tags"})
		return
	}

	response := uniquetagsmodels.GetUniqueTagsResponse{
		Tags: tags,
	}

	c.JSON(http.StatusOK, response)
}

// fetchUniqueTags retrieves all unique tag keys for a user
func (h *EntryHandler) fetchUniqueTags(ctx context.Context, userUID string) ([]models.Tag, error) {
	// Query to get unique tag keys with their most recent value for each key
	// This ensures we get one Tag object per unique key
	query := `
		SELECT DISTINCT ON (t.key) t.key, t.value
		FROM tags t
		INNER JOIN entries e ON t.entry_id = e.id
		WHERE e.user_uid = $1
		ORDER BY t.key, t.created_at DESC
	`

	rows, err := h.postgres.Query(ctx, query, userUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []models.Tag
	for rows.Next() {
		var tag models.Tag
		if err := rows.Scan(&tag.Key, &tag.Value); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}

	return tags, nil
}