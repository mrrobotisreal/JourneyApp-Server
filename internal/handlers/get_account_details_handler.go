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

	getdetailsmodels "io.winapps.journeyapp/internal/models/get_account_details"
)

// GetAccountDetails returns the authenticated user's account and usage details
func (h *AuthHandler) GetAccountDetails(c *gin.Context) {
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

	// Read uid from query string (must match authenticated user)
	requestedUID := c.Query("uid")
	if requestedUID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required query parameter: uid"})
		return
	}
	if requestedUID != authenticatedUID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot access another user's account details"})
		return
	}

	ctx := context.Background()

	// Attempt Redis cache first
	cacheKey := fmt.Sprintf("account_details:%s", requestedUID)
	if cached, err := h.redis.Get(ctx, cacheKey).Result(); err == nil && cached != "" {
		var resp getdetailsmodels.GetAccountDetailsResponse
		if err := json.Unmarshal([]byte(cached), &resp); err == nil {
			c.JSON(http.StatusOK, resp)
			return
		}
	}

	// Fetch base user data
	var (
		idToken             string
		displayName         string
		email               string
		photoURL            string
		phoneNumber         string
		emailVerified       bool
		phoneNumberVerified bool
		isPremium           bool
		premiumExpiresAtPtr *time.Time
		accountCreatedAt    time.Time
		accountUpdatedAt    time.Time
	)

	userQuery := `
		SELECT token, display_name, email, photo_url, phone_number,
		       email_verified, phone_number_verified,
		       is_premium, premium_expires_at,
		       created_at, updated_at
		FROM users
		WHERE uid = $1
	`
	if err := h.postgres.QueryRow(ctx, userQuery, requestedUID).Scan(
		&idToken,
		&displayName,
		&email,
		&photoURL,
		&phoneNumber,
		&emailVerified,
		&phoneNumberVerified,
		&isPremium,
		&premiumExpiresAtPtr,
		&accountCreatedAt,
		&accountUpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch user"})
		return
	}

	// Fetch settings
	var (
		themeMode         string
		theme             string
		appFont           string
		lang              string
		settingsCreatedAt time.Time
		settingsUpdatedAt time.Time
	)
	settingsQuery := `
		SELECT theme_mode, theme, app_font, lang, created_at, updated_at
		FROM user_settings
		WHERE uid = $1
	`
	if err := h.postgres.QueryRow(ctx, settingsQuery, requestedUID).Scan(
		&themeMode,
		&theme,
		&appFont,
		&lang,
		&settingsCreatedAt,
		&settingsUpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// If settings are missing, initialize defaults in-memory
			themeMode = "light"
			theme = "default"
			appFont = "Montserrat"
			lang = "en"
			settingsCreatedAt = accountCreatedAt
			settingsUpdatedAt = accountUpdatedAt
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch settings"})
			return
		}
	}

	// Fetch aggregate counts
	var (
		totalEntries   int
		totalTags      int
		totalLocations int
		totalImages    int
		totalAudios    int
	)
	countsQuery := `
		SELECT
			(SELECT COUNT(*) FROM entries e WHERE e.user_uid = $1) AS total_entries,
			(SELECT COUNT(*) FROM tags t JOIN entries e ON t.entry_id = e.id WHERE e.user_uid = $1) AS total_tags,
			(SELECT COUNT(*) FROM locations l JOIN entries e ON l.entry_id = e.id WHERE e.user_uid = $1) AS total_locations,
			(SELECT COUNT(*) FROM images i JOIN entries e ON i.entry_id = e.id WHERE e.user_uid = $1) AS total_images,
			(SELECT COUNT(*) FROM audio a JOIN entries e ON a.entry_id = e.id WHERE e.user_uid = $1) AS total_audios
	`
	if err := h.postgres.QueryRow(ctx, countsQuery, requestedUID).Scan(
		&totalEntries,
		&totalTags,
		&totalLocations,
		&totalImages,
		&totalAudios,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to compute aggregates"})
		return
	}

	// Assemble response
	resp := getdetailsmodels.GetAccountDetailsResponse{
		IDToken:             idToken,
		UID:                 requestedUID,
		DisplayName:         displayName,
		Email:               email,
		PhotoURL:            photoURL,
		PhoneNumber:         phoneNumber,
		EmailVerified:       emailVerified,
		PhoneNumberVerified: phoneNumberVerified,
		ThemeMode:           themeMode,
		Theme:               theme,
		AppFont:             appFont,
		Lang:                lang,
		AccountCreatedAt:    accountCreatedAt,
		AccountUpdatedAt:    accountUpdatedAt,
		SettingsCreatedAt:   settingsCreatedAt,
		SettingsUpdatedAt:   settingsUpdatedAt,
		TotalEntries:        totalEntries,
		TotalTags:           totalTags,
		TotalLocations:      totalLocations,
		TotalImages:         totalImages,
		TotalAudios:         totalAudios,
		TotalVideos:         0,
		IsPremium:           isPremium,
		PremiumExpiresAt:    func() time.Time { if premiumExpiresAtPtr != nil { return *premiumExpiresAtPtr }; return time.Time{} }(),
	}

	// Cache response for a short period
	if payload, err := json.Marshal(resp); err == nil {
		_ = h.redis.Set(ctx, cacheKey, payload, 10*time.Minute).Err()
	}

	c.JSON(http.StatusOK, resp)
}
