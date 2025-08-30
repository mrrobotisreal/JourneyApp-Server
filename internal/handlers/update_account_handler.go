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

	ctx := context.Background()

	// Parse JSON body into a raw map to detect which keys are present
	var raw map[string]json.RawMessage
	if strings.Contains(strings.ToLower(c.ContentType()), "application/json") {
		_ = c.ShouldBindJSON(&raw)
	}

	// Determine target UID from JSON or query
	targetUID := strings.TrimSpace(c.Query("uid"))
	if b, ok := raw["uid"]; ok {
		var uidStr string
		if err := json.Unmarshal(b, &uidStr); err == nil && strings.TrimSpace(uidStr) != "" {
			targetUID = strings.TrimSpace(uidStr)
		}
	}
	if targetUID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing uid in request"})
		return
	}
	if targetUID != authenticatedUID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot update another user's account"})
		return
	}

	setClauses := make([]string, 0)
	args := make([]interface{}, 0)
	argIndex := 1

	// displayName
	if b, ok := raw["displayName"]; ok {
		var v string
		if err := json.Unmarshal(b, &v); err == nil {
			v = strings.TrimSpace(v)
			if v != "" {
				setClauses = append(setClauses, fmt.Sprintf("display_name = $%d", argIndex))
				args = append(args, v)
				argIndex++
			}
		}
	}

	// email
	if b, ok := raw["email"]; ok {
		var v string
		if err := json.Unmarshal(b, &v); err == nil {
			v = strings.TrimSpace(v)
			if v != "" {
				setClauses = append(setClauses, fmt.Sprintf("email = $%d", argIndex))
				args = append(args, v)
				argIndex++
			}
		}
	}

	// phoneNumber
	if b, ok := raw["phoneNumber"]; ok {
		var v string
		if err := json.Unmarshal(b, &v); err == nil {
			v = strings.TrimSpace(v)
			if v != "" {
				setClauses = append(setClauses, fmt.Sprintf("phone_number = $%d", argIndex))
				args = append(args, v)
				argIndex++
			}
		}
	}

	// emailVerified
	if b, ok := raw["emailVerified"]; ok {
		var v bool
		if err := json.Unmarshal(b, &v); err == nil {
			setClauses = append(setClauses, fmt.Sprintf("email_verified = $%d", argIndex))
			args = append(args, v)
			argIndex++
		}
	}

	// phoneNumberVerified
	if b, ok := raw["phoneNumberVerified"]; ok {
		var v bool
		if err := json.Unmarshal(b, &v); err == nil {
			setClauses = append(setClauses, fmt.Sprintf("phone_number_verified = $%d", argIndex))
			args = append(args, v)
			argIndex++
		}
	}

	// isPremium
	if b, ok := raw["isPremium"]; ok {
		var v bool
		if err := json.Unmarshal(b, &v); err == nil {
			setClauses = append(setClauses, fmt.Sprintf("is_premium = $%d", argIndex))
			args = append(args, v)
			argIndex++
		}
	}

	// premiumExpiresAt (supports explicit null)
	if b, ok := raw["premiumExpiresAt"]; ok {
		if strings.TrimSpace(string(b)) == "null" {
			setClauses = append(setClauses, "premium_expires_at = NULL")
		} else {
			var v time.Time
			if err := json.Unmarshal(b, &v); err == nil {
				setClauses = append(setClauses, fmt.Sprintf("premium_expires_at = $%d", argIndex))
				args = append(args, v)
				argIndex++
			}
		}
	}

	// Photo handling (photoURL in JSON or multipart file)
	photoWasUpdated := false
	if b, ok := raw["photoURL"]; ok {
		var v string
		if err := json.Unmarshal(b, &v); err == nil {
			v = strings.TrimSpace(v)
			if v != "" {
				if strings.HasPrefix(strings.ToLower(v), "data:") || strings.Contains(v, ",") {
					_, absoluteURL, err := h.saveProfileImageToFileSystem(v, targetUID)
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
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update Firebase photo URL"})
						return
					}
					setClauses = append(setClauses, fmt.Sprintf("photo_url = $%d", argIndex))
					args = append(args, absoluteURL)
					argIndex++
					photoWasUpdated = true
				} else {
					setClauses = append(setClauses, fmt.Sprintf("photo_url = $%d", argIndex))
					args = append(args, v)
					argIndex++
					photoWasUpdated = true
				}
			}
		}
	}

	if !photoWasUpdated {
		if fileHeader, err := c.FormFile("photo"); err == nil && fileHeader != nil {
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
			base64Body := base64.StdEncoding.EncodeToString(data)
			_, absoluteURL, err := h.saveProfileImageToFileSystem(base64Body, targetUID)
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
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update Firebase photo URL"})
				return
			}
			setClauses = append(setClauses, fmt.Sprintf("photo_url = $%d", argIndex))
			args = append(args, absoluteURL)
			argIndex++
		}
	}

	// Always update updated_at
	setClauses = append(setClauses, "updated_at = NOW()")

	setSQL := strings.Join(setClauses, ", ")
	updateQuery := fmt.Sprintf(`
		UPDATE users
		SET %s
		WHERE uid = $%d
		RETURNING uid, display_name, email, phone_number, photo_url,
		          email_verified, phone_number_verified, is_premium, premium_expires_at, created_at, updated_at
	`, setSQL, argIndex)
	args = append(args, targetUID)

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

	if err := h.postgres.QueryRow(ctx, updateQuery, args...).Scan(
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

	if payload, err := json.Marshal(resp); err == nil {
		_ = h.redis.Set(ctx, "user:"+uid, payload, 24*time.Hour).Err()
	}

	c.JSON(http.StatusOK, resp)
}