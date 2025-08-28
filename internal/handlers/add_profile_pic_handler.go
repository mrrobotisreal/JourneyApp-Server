package handlers

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	firebaseauth "firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	firebaseutil "io.winapps.journeyapp/internal/firebase"
	addprofilemodels "io.winapps.journeyapp/internal/models/add_profile_pic"
)

// AddProfilePic updates the user's profile picture
func (h *AuthHandler) AddProfilePic(c *gin.Context) {
	var req addprofilemodels.AddProfilePicRequest
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
	if !ok || userUID == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user context"})
		return
	}

	ctx := context.Background()

	var finalPhotoURL string

	if req.IsPhotoAttached {
		// Expect a data URL/base64 payload in PhotoURL when the image is attached
		if strings.TrimSpace(req.PhotoURL) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing image data"})
			return
		}

		relativeURL, absoluteURL, err := h.saveProfileImageToFileSystem(req.PhotoURL, userUID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save image: " + err.Error()})
			return
		}

		// Update Firebase Auth photo URL
		authClient, err := firebaseutil.GetAuthClient(h.firebaseApp)
		if err != nil {
			// Best effort: still proceed to update Postgres and respond
			// but surface the error to the client
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize auth client"})
			return
		}

		params := (&firebaseauth.UserToUpdate{}).PhotoURL(absoluteURL)
		if _, err := authClient.UpdateUser(ctx, userUID, params); err != nil {
			// If Firebase update fails, remove saved file to avoid orphaned storage
			// Note: relativeURL is like /images/<uid>/profile/<file>
			_ = os.Remove(strings.TrimPrefix(relativeURL, "/"))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update Firebase photo URL"})
			return
		}

		finalPhotoURL = absoluteURL
	} else {
		// Use provided external URL directly
		if strings.TrimSpace(req.PhotoURL) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "photoURL is required when isPhotoAttached is false"})
			return
		}
		finalPhotoURL = req.PhotoURL
	}

	// Update user's photo_url in Postgres
	updateQuery := `
		UPDATE users
		SET photo_url = $1, updated_at = NOW()
		WHERE uid = $2
	`
	if _, err := h.postgres.Exec(ctx, updateQuery, finalPhotoURL, userUID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user photo URL"})
		return
	}

	// Invalidate cached account details
	cacheKey := fmt.Sprintf("account_details:%s", userUID)
	_ = h.redis.Del(ctx, cacheKey).Err()

	resp := addprofilemodels.AddProfilePicResponse{
		Success:  true,
		Message:  "Profile photo updated successfully",
		PhotoURL: finalPhotoURL,
	}
	c.JSON(http.StatusOK, resp)
}

// saveProfileImageToFileSystem saves a base64 image to internal/images/<uid>/profile/ and returns both relative and absolute URLs
func (h *AuthHandler) saveProfileImageToFileSystem(base64Image, userUID string) (string, string, error) {
	// Strip data URL prefix if present (e.g., "data:image/png;base64,")
	if strings.Contains(base64Image, ",") {
		parts := strings.Split(base64Image, ",")
		if len(parts) > 1 {
			base64Image = parts[1]
		}
	}

	// Decode base64 image
	imageData, err := base64.StdEncoding.DecodeString(base64Image)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode base64 image: %w", err)
	}

	// Detect file extension from image data
	var ext string
	if len(imageData) >= 4 {
		switch {
		case imageData[0] == 0xFF && imageData[1] == 0xD8 && imageData[2] == 0xFF:
			ext = ".jpg"
		case imageData[0] == 0x89 && imageData[1] == 0x50 && imageData[2] == 0x4E && imageData[3] == 0x47:
			ext = ".png"
		case imageData[0] == 0x47 && imageData[1] == 0x49 && imageData[2] == 0x46:
			ext = ".gif"
		case imageData[0] == 0x52 && imageData[1] == 0x49 && imageData[2] == 0x46 && imageData[3] == 0x46:
			ext = ".webp"
		default:
			ext = ".jpg"
		}
	} else {
		ext = ".jpg"
	}

	// Create directory structure: internal/images/{userUID}/profile/
	userDir := filepath.Join("internal", "images", userUID)
	profileDir := filepath.Join(userDir, "profile")

	if err := os.MkdirAll(profileDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create profile directory: %w", err)
	}

	// Generate unique filename
	imageID := uuid.New().String()
	filename := imageID + ext
	filePath := filepath.Join(profileDir, filename)

	// Write image data to file
	if err := os.WriteFile(filePath, imageData, 0644); err != nil {
		return "", "", fmt.Errorf("failed to write image file: %w", err)
	}

	// Relative URL served by static file server
	relativeURL := fmt.Sprintf("/images/%s/profile/%s", userUID, filename)

	// Absolute URL for public access (as specified)
	absoluteURL := fmt.Sprintf("https://journey-app-api.winapps.dev%s", relativeURL)

	return relativeURL, absoluteURL, nil
}