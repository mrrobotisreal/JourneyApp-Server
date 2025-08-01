package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	firebaseutil "io.winapps.journeyapp/internal/firebase"
	models "io.winapps.journeyapp/internal/models/login"
	usermodels "io.winapps.journeyapp/internal/models/account"
)

type AuthHandler struct {
	firebaseApp *firebase.App
	postgres    *pgxpool.Pool
	redis       *redis.Client
}

// NewAuthHandler creates a new authentication handler
func NewAuthHandler(firebaseApp *firebase.App, postgres *pgxpool.Pool, redis *redis.Client) *AuthHandler {
	return &AuthHandler{
		firebaseApp: firebaseApp,
		postgres:    postgres,
		redis:       redis,
	}
}

// Login handles user login via Firebase
func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	ctx := context.Background()
	authClient, err := firebaseutil.GetAuthClient(h.firebaseApp)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize auth client"})
		return
	}

	var userRecord *auth.UserRecord
	var customToken string

	// Handle token validation case
	if req.Token != "" && req.Email == "" && req.Password == "" {
		// Validate the provided token
		token, err := authClient.VerifyIDToken(ctx, req.Token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			return
		}

		// Get user record
		userRecord, err = authClient.GetUser(ctx, token.UID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user information"})
			return
		}

		customToken = req.Token
	} else if req.Email != "" && req.Password != "" {
		// Handle email/password login
		// Note: Firebase Admin SDK doesn't support email/password authentication directly
		// In a real app, you'd typically use Firebase Auth REST API or client SDK for this
		// For now, we'll create a custom token for demonstration

		// Get user by email
		userRecord, err = authClient.GetUserByEmail(ctx, req.Email)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
			return
		}

		// Create custom token
		customToken, err = authClient.CustomToken(ctx, userRecord.UID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create authentication token"})
			return
		}
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Either token or email/password must be provided"})
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
		PhoneNumberVerified: false, // Firebase doesn't provide this directly
	}

	// Store user in Redis for session management
	userJSON, err := json.Marshal(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process user data"})
		return
	}

	// Set Redis key with 24-hour expiration
	redisKey := "user:" + user.UID
	if err := h.redis.Set(ctx, redisKey, userJSON, 24*time.Hour).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create session"})
		return
	}

	// Store/update user in PostgreSQL
	if err := h.storeUserInPostgres(ctx, user); err != nil {
		// Log error but don't fail the login
		// In production, you might want to handle this differently
	}

	// Create response
	response := models.LoginResponse{
		UID:                 user.UID,
		DisplayName:         user.DisplayName,
		Email:               user.Email,
		Token:               customToken,
		PhotoURL:            user.PhotoURL,
		PhoneNumber:         user.PhoneNumber,
		EmailVerified:       user.EmailVerified,
		PhoneNumberVerified: user.PhoneNumberVerified,
	}

	c.JSON(http.StatusOK, response)
}

// storeUserInPostgres stores or updates user information in PostgreSQL
func (h *AuthHandler) storeUserInPostgres(ctx context.Context, user *usermodels.User) error {
	query := `
		INSERT INTO users (uid, display_name, email, photo_url, phone_number, email_verified, phone_number_verified, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
		ON CONFLICT (uid)
		DO UPDATE SET
			display_name = EXCLUDED.display_name,
			email = EXCLUDED.email,
			photo_url = EXCLUDED.photo_url,
			phone_number = EXCLUDED.phone_number,
			email_verified = EXCLUDED.email_verified,
			phone_number_verified = EXCLUDED.phone_number_verified,
			updated_at = NOW()
	`

	_, err := h.postgres.Exec(ctx, query,
		user.UID,
		user.DisplayName,
		user.Email,
		user.PhotoURL,
		user.PhoneNumber,
		user.EmailVerified,
		user.PhoneNumberVerified,
	)

	return err
}
