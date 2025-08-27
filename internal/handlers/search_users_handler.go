package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// SearchUsers finds users by display name or email using a case-insensitive partial match
func (h *UsersHandler) SearchUsers(c *gin.Context) {
	// Ensure request is authenticated (middleware sets uid)
	if _, exists := c.Get("uid"); !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	query := strings.TrimSpace(c.Query("search-query"))
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "search-query is required"})
		return
	}

	ctx := context.Background()
	cacheKey := fmt.Sprintf("search_users:%s", strings.ToLower(query))

	// Try Redis cache first
	if cached, err := h.redis.Get(ctx, cacheKey).Result(); err == nil && cached != "" {
		var cachedResponse map[string][]map[string]string
		if err := json.Unmarshal([]byte(cached), &cachedResponse); err == nil {
			c.JSON(http.StatusOK, cachedResponse)
			return
		}
	}

	like := fmt.Sprintf("%%%s%%", query)
	rows, err := h.postgres.Query(ctx, `
		SELECT uid, display_name, email, photo_url
		FROM users
		WHERE display_name ILIKE $1 OR email ILIKE $1
		ORDER BY display_name
		LIMIT 50
	`, like)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to search users"})
		return
	}
	defer rows.Close()

	results := make([]map[string]string, 0)
	for rows.Next() {
		var uid, displayName, email, photoURL string
		if err := rows.Scan(&uid, &displayName, &email, &photoURL); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read results"})
			return
		}
		results = append(results, map[string]string{
			"uid":          uid,
			"displayName":  displayName,
			"email":        email,
			"photoURL":     photoURL,
		})
	}

	response := map[string]interface{}{
		"results": results,
	}

	// Cache for a short period
	if data, err := json.Marshal(response); err == nil {
		_ = h.redis.Set(ctx, cacheKey, data, 5*time.Minute).Err()
	}

	c.JSON(http.StatusOK, response)
}
