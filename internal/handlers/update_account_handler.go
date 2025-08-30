package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	firebaseauth "firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"

	firebaseutil "io.winapps.journeyapp/internal/firebase"
	updatemodels "io.winapps.journeyapp/internal/models/update-account"
)

// UpdateAccount updates user fields and optionally handles profile image upload
func (h *AuthHandler) UpdateAccount(c *gin.Context) {
	var req updatemodels.UpdateAccountRequest
	// Accept JSON body if provided; it's OK if binding fails (we may be using multipart)
	_ = c.ShouldBindJSON(&req)

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

	// Determine target UID
	targetUID := strings.TrimSpace(req.UID)
	if targetUID == "" {
		// fallback: read from query
		targetUID = strings.TrimSpace(c.Query("uid"))
	}
	if targetUID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing uid in request"})
		return
	}
	if targetUID != authenticatedUID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot update another user's account"})
		return
	}

	ctx := context.Background()

	// Handle photo update logic (supports external URL, base64 data URL, or multipart file)
	finalPhotoURL := strings.TrimSpace(req.PhotoURL)
	if finalPhotoURL != "" {
		// If the provided photoURL looks like base64 data, treat it as an attached image
		if strings.HasPrefix(strings.ToLower(finalPhotoURL), "data:") || strings.Contains(finalPhotoURL, ",") {
			relativeURL, absoluteURL, err := h.saveProfileImageToFileSystem(finalPhotoURL, targetUID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save image: " + err.Error()})
				return
			}

			// Update Firebase Auth photo URL
			authClient, err := firebaseutil.GetAuthClient(h.firebaseApp)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize auth client"})
				return
			}
			params := (&firebaseauth.UserToUpdate{}).PhotoURL(absoluteURL)
			if _, err := authClient.UpdateUser(ctx, targetUID, params); err != nil {
				// Attempt cleanup on failure
				_ = relativeURL // path already relative; best-effort cleanup handled elsewhere if needed
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update Firebase photo URL"})
				return
			}

			finalPhotoURL = absoluteURL
		}
	} else {
		// No photoURL provided; check if a multipart file is attached under key "photo"
		fileHeader, err := c.FormFile("photo")
		if err == nil && fileHeader != nil {
			file, err := fileHeader.Open()
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to open uploaded image"})
				return
			}
			defer file.Close()
			data, err := io.ReadAll(file)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read uploaded image"})
				return
			}
			// Convert to base64 (without prefix) and reuse existing saver (it can infer type)
			base64Body := base64.StdEncoding.EncodeToString(data)
			relativeURL, absoluteURL, err := h.saveProfileImageToFileSystem(base64Body, targetUID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save image: " + err.Error()})
				return
			}

			// Update Firebase Auth photo URL
			authClient, err := firebaseutil.GetAuthClient(h.firebaseApp)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize auth client"})
				return
			}
			params := (&firebaseauth.UserToUpdate{}).PhotoURL(absoluteURL)
			if _, err := authClient.UpdateUser(ctx, targetUID, params); err != nil {
				_ = relativeURL
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update Firebase photo URL"})
				return
			}

			finalPhotoURL = absoluteURL
		}
	}

	// Update user fields in Postgres
	updateQuery := `
		UPDATE users
		SET display_name = $2,
		    email = $3,
		    phone_number = $4,
		    photo_url = $5,
		    email_verified = $6,
		    phone_number_verified = $7,
		    is_premium = $8,
		    premium_expires_at = $9,
		    updated_at = NOW()
		WHERE uid = $1
		RETURNING uid, display_name, email, phone_number, photo_url,
		          email_verified, phone_number_verified, is_premium, premium_expires_at, created_at, updated_at
	`

	var (
		uid string
		displayName string
		email string
		phoneNumber string
		photoURL string
		emailVerified bool
		phoneNumberVerified bool
		isPremium bool
		premiumExpiresAtPtr *time.Time
		createdAt time.Time
		updatedAt time.Time
	)

	if err := h.postgres.QueryRow(
		ctx,
		updateQuery,
		targetUID,
		strings.TrimSpace(req.DisplayName),
		strings.TrimSpace(req.Email),
		strings.TrimSpace(req.PhoneNumber),
		finalPhotoURL,
		req.EmailVerified,
		req.PhoneNumberVerified,
		req.IsPremium,
		func() *time.Time { if req.PremiumExpiresAt.IsZero() { return nil }; return &req.PremiumExpiresAt }(),
	).Scan(
		&uid,
		&displayName,
		&email,
		&phoneNumber,
		&photoURL,
		&emailVerified,
		&phoneNumberVerified,
		&isPremium,
		&premiumExpiresAtPtr,
		&createdAt,
		&updatedAt,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user"})
		return
	}

	// Invalidate cached account details
	cacheKey := fmt.Sprintf("account_details:%s", targetUID)
	_ = h.redis.Del(ctx, cacheKey).Err()

	resp := updatemodels.UpdateAccountResponse{
		UID: uid,
		DisplayName: displayName,
		Email: email,
		PhoneNumber: phoneNumber,
		PhotoURL: photoURL,
		UpdatedAt: updatedAt,
		CreatedAt: createdAt,
		EmailVerified: emailVerified,
		PhoneNumberVerified: phoneNumberVerified,
		IsPremium: isPremium,
		PremiumExpiresAt: func() time.Time { if premiumExpiresAtPtr != nil { return *premiumExpiresAtPtr }; return time.Time{} }(),
	}

	// Also put latest user basic in Redis session cache (non-critical)
	if payload, err := json.Marshal(resp); err == nil {
		_ = h.redis.Set(ctx, "user:"+uid, payload, 24*time.Hour).Err()
	}

	c.JSON(http.StatusOK, resp)
}