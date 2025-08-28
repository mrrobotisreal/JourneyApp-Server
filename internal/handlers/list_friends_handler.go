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

// ListFriends returns user profiles for all friends of the given uid
func (h *UsersHandler) ListFriends(c *gin.Context) {
	// Ensure request is authenticated (middleware sets uid)
	_, authed := c.Get("uid")
	if !authed {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	targetUID := c.Query("uid")
	if targetUID == "" {
		// Fallback to authenticated user if no uid query provided
		if v, ok := c.Get("uid"); ok {
			if s, ok := v.(string); ok {
				targetUID = s
			}
		}
	}
	if targetUID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "uid is required"})
		return
	}

	statusParam := strings.ToLower(strings.TrimSpace(c.Query("status")))
	allowedStatuses := map[string]bool{
		"pending":  true,
		"approved": true,
		"rejected": true,
		"blocked":  true,
	}

	var statuses []string
	switch statusParam {
	case "":
		statuses = []string{"pending", "approved"}
	case "all":
		statuses = []string{"pending", "approved", "rejected", "blocked"}
	default:
		if !allowedStatuses[statusParam] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
			return
		}
		statuses = []string{statusParam}
	}

	ctx := context.Background()
	cacheKey := fmt.Sprintf("friends:%s:%s", targetUID, func() string {
		if statusParam == "" {
			return "default"
		}
		return statusParam
	}())

	// Try Redis cache first
	if cached, err := h.redis.Get(ctx, cacheKey).Result(); err == nil && cached != "" {
		var cachedResponse map[string][]map[string]string
		if err := json.Unmarshal([]byte(cached), &cachedResponse); err == nil {
			c.JSON(http.StatusOK, cachedResponse)
			return
		}
	}

	// Build IN clause for statuses
	placeholders := make([]string, len(statuses))
	args := make([]interface{}, 0, 1+len(statuses))
	args = append(args, targetUID)
	for i, s := range statuses {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args = append(args, s)
	}

	query := fmt.Sprintf(`
		SELECT u.uid, u.display_name, u.email, u.photo_url, f.status, f.created_at
		FROM friendships f
		JOIN users u ON u.uid = CASE WHEN f.uid = $1 THEN f.fid ELSE f.uid END
		WHERE (f.uid = $1 OR f.fid = $1) AND f.status IN (%s)
		ORDER BY u.display_name
	`, strings.Join(placeholders, ","))

	rows, err := h.postgres.Query(ctx, query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list friends"})
		return
	}
	defer rows.Close()

	friends := make([]map[string]string, 0)
	for rows.Next() {
		var uid, displayName, email, photoURL, status, createdAt string
		if err := rows.Scan(&uid, &displayName, &email, &photoURL, &status, &createdAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read results"})
			return
		}
		friends = append(friends, map[string]string{
			"uid":         uid,
			"displayName": displayName,
			"email":       email,
			"photoURL":    photoURL,
			"status":      status,
			"createdAt":   createdAt,
		})
	}

	response := map[string]interface{}{
		"friends": friends,
	}

	// Cache for a short period
	if data, err := json.Marshal(response); err == nil {
		_ = h.redis.Set(ctx, cacheKey, data, 5*time.Minute).Err()
	}

	c.JSON(http.StatusOK, response)
}
