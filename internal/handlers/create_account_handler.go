package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	firebase "firebase.google.com/go/v4"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	stream "github.com/GetStream/stream-chat-go/v5"

	firebaseutil "io.winapps.journeyapp/internal/firebase"
	createmodels "io.winapps.journeyapp/internal/models/create_account"
	usermodels "io.winapps.journeyapp/internal/models/account"
)

// Public channels to auto-join for every user
var publicChannelIDs = []string{
	"general",
	"wellness",
	"motivation",
	"mindfulness",
	"goals",
	"books",
	"creativity",
	"tech-tips",
}

func addUserToPublicChannels(ctx context.Context, client *stream.Client, uid string) {
	for _, channelID := range publicChannelIDs {
		ch := client.Channel("livestream", channelID)
		if _, err := ch.AddMembers(ctx, []string{uid}, nil, nil); err != nil {
			log.Printf("Failed adding user %s to channel %s: %v", uid, channelID, err)
		}
	}
}

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

// CreateAccount handles user account creation from client-side Firebase authentication
func (h *AuthHandler) CreateAccount(c *gin.Context) {
	var req createmodels.CreateAccountRequest
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

	// Verify the ID token from client
	idToken, err := authClient.VerifyIDToken(ctx, req.IDToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired ID token"})
		return
	}

	// Ensure the UID matches the token
	if idToken.UID != req.UID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "UID mismatch with token"})
		return
	}

	client, err := stream.NewClient(os.Getenv("STREAM_API_KEY"), os.Getenv("STREAM_API_SECRET"))
	streamToken, err := client.CreateToken(req.UID, time.Time{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create stream token"})
		return
	}

	// Check if user already exists in our database
	existingUser, _ := h.getUserFromDatabase(ctx, req.UID)
	if existingUser != nil {
		response := createmodels.CreateUserResponse{
			Success: false,
			Message: "User already exists, sending stream token and uid",
			UID:     req.UID,
			StreamToken: streamToken,
		}
		c.JSON(http.StatusOK, response)
		return
	}

	// Create user object for storage
	user := &usermodels.User{
		UID:                 req.UID,
		DisplayName:         req.DisplayName,
		Email:               req.Email,
		Token:               req.IDToken, // Store the ID token for session management
		PhotoURL:            req.PhotoURL,
		PhoneNumber:         req.PhoneNumber,
		EmailVerified:       req.EmailVerified,
		PhoneNumberVerified: req.PhoneNumberVerified,
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

	// Store user in PostgreSQL
	if err := h.storeUserInPostgres(ctx, user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store user in database"})
		return
	}

	// Create default user settings
	if err := h.createDefaultUserSettings(ctx, user.UID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user settings"})
		return
	}

	// Add user to public channels (server-side membership)
	addUserToPublicChannels(ctx, client, user.UID)

	// Create success response
	response := createmodels.CreateUserResponse{
		Success: true,
		Message: "Account created successfully",
		UID:     user.UID,
		StreamToken: streamToken,
	}

	c.JSON(http.StatusCreated, response)
}

// storeUserInPostgres stores or updates user information in PostgreSQL
func (h *AuthHandler) storeUserInPostgres(ctx context.Context, user *usermodels.User) error {
	query := `
		INSERT INTO users (uid, display_name, email, token, photo_url, phone_number, email_verified, phone_number_verified, is_premium, premium_expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, FALSE, NULL, NOW(), NOW())
		ON CONFLICT (uid)
		DO UPDATE SET
			display_name = EXCLUDED.display_name,
			email = EXCLUDED.email,
			token = EXCLUDED.token,
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
		user.Token,
		user.PhotoURL,
		user.PhoneNumber,
		user.EmailVerified,
		user.PhoneNumberVerified,
	)

	return err
}

// getUserFromDatabase retrieves user information from PostgreSQL by UID
func (h *AuthHandler) getUserFromDatabase(ctx context.Context, uid string) (*usermodels.User, error) {
	query := `
		SELECT uid, display_name, email, token, photo_url, phone_number, email_verified, phone_number_verified
		FROM users
		WHERE uid = $1
	`

	var user usermodels.User
	err := h.postgres.QueryRow(ctx, query, uid).Scan(
		&user.UID,
		&user.DisplayName,
		&user.Email,
		&user.Token,
		&user.PhotoURL,
		&user.PhoneNumber,
		&user.EmailVerified,
		&user.PhoneNumberVerified,
	)

	if err != nil {
		return nil, err
	}

	return &user, nil
}

// createDefaultUserSettings creates default user settings for a new user
func (h *AuthHandler) createDefaultUserSettings(ctx context.Context, uid string) error {
	query := `
		INSERT INTO user_settings (uid, theme_mode, theme, app_font, lang, created_at, updated_at)
		VALUES ($1, 'light', 'default', 'Montserrat', 'en', NOW(), NOW())
	`

	_, err := h.postgres.Exec(ctx, query, uid)
	return err
}