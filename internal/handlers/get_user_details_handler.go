package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	getdetailsmodels "io.winapps.journeyapp/internal/models/get_user_details"
)

// GetUserDetails returns user details of the given uid
func (h *UsersHandler) GetUserDetails(c *gin.Context) {
	// Ensure user is authenticated (middleware populates context)
	uidCtx, exists := c.Get("uid")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	authenticatedUID, ok := uidCtx.(string)
	if !ok || authenticatedUID == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user context"})
		return
	}

	targetUID := c.Query("uid")
	if targetUID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required query parameter: uid"})
		return
	}

	ctx := context.Background()

	// Attempt Redis cache first
	cacheKey := fmt.Sprintf("user_details:%s", targetUID)
	if cached, err := h.redis.Get(ctx, cacheKey).Result(); err == nil && cached != "" {
		var resp getdetailsmodels.GetUserDetailsResponse
		if err := json.Unmarshal([]byte(cached), &resp); err == nil {
			c.JSON(http.StatusOK, resp)
			return
		}
	}

	// Fetch aggregate counts
	var totalEntries int
	countsQuery := `
		SELECT
			(SELECT COUNT(*) FROM entries e WHERE e.user_uid = $1) AS total_entries,
	`
	if err := h.postgres.QueryRow(ctx, countsQuery, targetUID).Scan(
		&totalEntries,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to compute aggregate count of entries"})
		return
	}

	// Fetch user details
	var (
		uid string
		displayName string
		email string
		photoURL string
		createdAt time.Time
		isPremium bool
	)

	query := `
		SELECT u.uid, u.display_name, u.email, u.photo_url, u.created_at, u.is_premium
		FROM users u
		WHERE u.uid = $1
	`

	if err := h.postgres.QueryRow(context.Background(), query, targetUID).Scan(
		&uid,
		&displayName,
		&email,
		&photoURL,
		&createdAt,
		&isPremium,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch user"})
		return
	}

	resp := getdetailsmodels.GetUserDetailsResponse{
		UID: uid,
		DisplayName: displayName,
		Email: email,
		PhotoURL: photoURL,
		CreatedAt: createdAt,
		TotalEntries: totalEntries,
		IsPremium: isPremium,
	}

	// Cache response for a short period
	if payload, err := json.Marshal(resp); err == nil {
		_ = h.redis.Set(ctx, cacheKey, payload, 10*time.Minute).Err()
	}

	c.JSON(http.StatusOK, resp)
}