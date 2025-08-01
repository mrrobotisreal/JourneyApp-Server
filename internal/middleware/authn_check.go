package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	firebase "firebase.google.com/go/v4"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	firebaseutil "io.winapps.journeyapp/internal/firebase"
	usermodels "io.winapps.journeyapp/internal/models/account"
)

// AuthMiddleware checks custom token and sets user context
func AuthMiddleware(firebaseApp *firebase.App, postgres *pgxpool.Pool, redisClient *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header is required"})
			c.Abort()
			return
		}

		// Check if header starts with "Bearer "
		const bearerPrefix = "Bearer "
		if !strings.HasPrefix(authHeader, bearerPrefix) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header must start with 'Bearer '"})
			c.Abort()
			return
		}

		// Extract token
		token := strings.TrimPrefix(authHeader, bearerPrefix)
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token is required"})
			c.Abort()
			return
		}

						// Validate custom token - try Redis first, then Postgres, then Firebase
		ctx := context.Background()

		// Step 1: Try to find user in Redis cache
		var userUID string
		iter := redisClient.Scan(ctx, 0, "user:*", 0).Iterator()
		for iter.Next(ctx) {
			key := iter.Val()
			userJSON, err := redisClient.Get(ctx, key).Result()
			if err != nil {
				continue
			}

			var user usermodels.User
			if err := json.Unmarshal([]byte(userJSON), &user); err != nil {
				continue
			}

			// Check if this user has the provided token
			if user.Token == token {
				userUID = user.UID
				break
			}
		}

		// Step 2: If not found in Redis, try Postgres
		if userUID == "" {
			query := `SELECT uid FROM users WHERE token = $1`
			err := postgres.QueryRow(ctx, query, token).Scan(&userUID)
			if err != nil {
				// Step 3: If not in Postgres, verify token with Firebase as last resort
				authClient, err := firebaseutil.GetAuthClient(firebaseApp)
				if err == nil {
					// Try to verify as ID token
					if idToken, err := authClient.VerifyIDToken(ctx, token); err == nil {
						userUID = idToken.UID
					}
				}
			}
		}

		if userUID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
			c.Abort()
			return
		}

		// Set user UID in context for use in handlers
		c.Set("uid", userUID)
		c.Next()
	}
}
