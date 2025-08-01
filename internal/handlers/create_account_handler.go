package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"

	firebaseutil "io.winapps.journeyapp/internal/firebase"
	createmodels "io.winapps.journeyapp/internal/models/create_account"
	usermodels "io.winapps.journeyapp/internal/models/account"
)

// CreateAccount handles user account creation via Firebase
func (h *AuthHandler) CreateAccount(c *gin.Context) {
	var req createmodels.CreateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	// Validate required fields
	if req.Email == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email and password are required"})
		return
	}

	ctx := context.Background()
	authClient, err := firebaseutil.GetAuthClient(h.firebaseApp)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize auth client"})
		return
	}

	// Create user parameters
	params := (&auth.UserToCreate{}).
		Email(req.Email).
		EmailVerified(false).
		Password(req.Password).
		Disabled(false)

	// Set display name if provided
	if req.DisplayName != "" {
		params = params.DisplayName(req.DisplayName)
	}

	// Create user in Firebase
	userRecord, err := authClient.CreateUser(ctx, params)
	if err != nil {
		// Handle specific Firebase errors
		if auth.IsEmailAlreadyExists(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "Email already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user account"})
		return
	}

	// Create custom token for immediate login
	customToken, err := authClient.CustomToken(ctx, userRecord.UID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Account created but failed to generate login token"})
		return
	}

	// Create user object for storage
	user := &usermodels.User{
		UID:                 userRecord.UID,
		DisplayName:         userRecord.DisplayName,
		Email:               userRecord.Email,
		Token:               customToken,
		PhotoURL:            userRecord.PhotoURL,
		PhoneNumber:         userRecord.PhoneNumber,
		EmailVerified:       userRecord.EmailVerified,
		PhoneNumberVerified: false,
	}

	// Store user in Redis for immediate session
	userJSON, err := json.Marshal(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Account created but failed to create session"})
		return
	}

	// Set Redis key with 24-hour expiration
	redisKey := "user:" + user.UID
	if err := h.redis.Set(ctx, redisKey, userJSON, 24*time.Hour).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Account created but failed to create session"})
		return
	}

	// Store user in PostgreSQL
	if err := h.storeUserInPostgres(ctx, user); err != nil {
		// Log error but don't fail the account creation
		// In production, you might want to handle this differently
	}

	// Create response
	response := createmodels.CreateUserResponse{
		UID:                 user.UID,
		DisplayName:         user.DisplayName,
		Email:               user.Email,
		Token:               customToken,
		PhotoURL:            user.PhotoURL,
		PhoneNumber:         user.PhoneNumber,
		EmailVerified:       user.EmailVerified,
		PhoneNumberVerified: user.PhoneNumberVerified,
	}

	c.JSON(http.StatusCreated, response)
}