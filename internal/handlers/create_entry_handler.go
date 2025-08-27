package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	firebase "firebase.google.com/go/v4"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	models "io.winapps.journeyapp/internal/models/account"
	createmodels "io.winapps.journeyapp/internal/models/create_entry"
)

type EntryHandler struct {
	firebaseApp *firebase.App
	postgres    *pgxpool.Pool
	redis       *redis.Client
}

// NewEntryHandler creates a new entry handler
func NewEntryHandler(firebaseApp *firebase.App, postgres *pgxpool.Pool, redis *redis.Client) *EntryHandler {
	return &EntryHandler{
		firebaseApp: firebaseApp,
		postgres:    postgres,
		redis:       redis,
	}
}

// CreateEntry handles creation of new journal entries
func (h *EntryHandler) CreateEntry(c *gin.Context) {
	var req createmodels.CreateEntryRequest
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
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user context"})
		return
	}

	// Validate required fields
	if req.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Title is required"})
		return
	}

	// Determine visibility (default private)
	visibility := strings.ToLower(strings.TrimSpace(req.Visibility))
	switch visibility {
	case "public", "semi-private", "private":
		// ok
	default:
		visibility = "private"
	}

	ctx := context.Background()

	// Generate new entry ID
	entryID := uuid.New().String()
	now := time.Now()

	// Create entry object
	entry := &models.Entry{
		ID:          entryID,
		Title:       req.Title,
		Description: req.Description,
		Images:      req.Images,
		Tags:        req.Tags,
		Locations:   req.Locations,
		Visibility:  visibility,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Start database transaction
	tx, err := h.postgres.Begin(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start database transaction"})
		return
	}
	defer tx.Rollback(ctx)

	// Insert entry into PostgreSQL
	entryQuery := `
		INSERT INTO entries (id, user_uid, title, description, visibility, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err = tx.Exec(ctx, entryQuery, entryID, userUID, req.Title, req.Description, visibility, now, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create entry"})
		return
	}

	// Insert entry shares if semi-private
	if visibility == "semi-private" && len(req.SharedWith) > 0 {
		seen := make(map[string]struct{})
		for _, sharedUID := range req.SharedWith {
			sharedUID = strings.TrimSpace(sharedUID)
			if sharedUID == "" {
				continue
			}
			if _, ok := seen[sharedUID]; ok {
				continue
			}
			seen[sharedUID] = struct{}{}
			shareQuery := `
				INSERT INTO entry_shares (entry_id, shared_user_uid, created_at)
				VALUES ($1, $2, $3)
			`
			if _, err := tx.Exec(ctx, shareQuery, entryID, sharedUID, now); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save shared users"})
				return
			}
		}
	}

	// Insert locations if provided
	if len(req.Locations) > 0 {
		for _, location := range req.Locations {
			locationQuery := `
				INSERT INTO locations (entry_id, latitude, longitude, address, city, state, zip, country, country_code, display_name, created_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			`
			_, err = tx.Exec(ctx, locationQuery,
				entryID,
				location.Latitude,
				location.Longitude,
				location.Address,
				location.City,
				location.State,
				location.Zip,
				location.Country,
				location.CountryCode,
				location.DisplayName,
				now,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save location data"})
				return
			}
		}
	}

	// Insert tags if provided
	if len(req.Tags) > 0 {
		for _, tag := range req.Tags {
			tagQuery := `
				INSERT INTO tags (entry_id, key, value, created_at)
				VALUES ($1, $2, $3, $4)
			`
			_, err = tx.Exec(ctx, tagQuery, entryID, tag.Key, tag.Value, now)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save tag data"})
				return
			}
		}
	}

	// Insert images if provided
	if len(req.Images) > 0 {
		for i, imageURL := range req.Images {
			imageQuery := `
				INSERT INTO images (entry_id, url, upload_order, created_at)
				VALUES ($1, $2, $3, $4)
			`
			_, err = tx.Exec(ctx, imageQuery, entryID, imageURL, i, now)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save image data"})
				return
			}
		}
	}

	// Commit transaction
	if err = tx.Commit(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save entry"})
		return
	}

	// Cache entry in Redis
	entryJSON, err := json.Marshal(entry)
	if err != nil {
		// Log error but don't fail the request since entry was saved
		fmt.Printf("Failed to marshal entry for Redis: %v\n", err)
	} else {
		redisKey := fmt.Sprintf("entry:%s", entryID)
		if err := h.redis.Set(ctx, redisKey, entryJSON, 24*time.Hour).Err(); err != nil {
			// Log error but don't fail the request since entry was saved
			fmt.Printf("Failed to cache entry in Redis: %v\n", err)
		}

		// Cache user's entry list
		userEntriesKey := fmt.Sprintf("user_entries:%s", userUID)
		if err := h.redis.SAdd(ctx, userEntriesKey, entryID).Err(); err != nil {
			fmt.Printf("Failed to update user entries cache: %v\n", err)
		}
		// Set expiration for user entries list
		h.redis.Expire(ctx, userEntriesKey, 24*time.Hour)

		// Maintain public entries sets
		if visibility == "public" {
			if err := h.redis.SAdd(ctx, "public_entries", entryID).Err(); err != nil {
				fmt.Printf("Failed to update public entries set: %v\n", err)
			}
			h.redis.Expire(ctx, "public_entries", 24*time.Hour)
			byUserKey := fmt.Sprintf("public_entries_by_user:%s", userUID)
			if err := h.redis.SAdd(ctx, byUserKey, entryID).Err(); err != nil {
				fmt.Printf("Failed to update public entries by user set: %v\n", err)
			}
			h.redis.Expire(ctx, byUserKey, 24*time.Hour)
		}

		// Maintain shared entries sets
		if visibility == "semi-private" && len(req.SharedWith) > 0 {
			entrySharesKey := fmt.Sprintf("entry_shares:%s", entryID)
			for _, sharedUID := range req.SharedWith {
				sharedUID = strings.TrimSpace(sharedUID)
				if sharedUID == "" {
					continue
				}
				_ = h.redis.SAdd(ctx, entrySharesKey, sharedUID).Err()
				userSharedKey := fmt.Sprintf("shared_entries:%s", sharedUID)
				_ = h.redis.SAdd(ctx, userSharedKey, entryID).Err()
				_ = h.redis.Expire(ctx, userSharedKey, 24*time.Hour).Err()
			}
			_ = h.redis.Expire(ctx, entrySharesKey, 24*time.Hour).Err()
		}
	}

	// Create response
	response := createmodels.CreateEntryResponse{
		ID:          entryID,
		Title:       req.Title,
		Description: req.Description,
		Images:      req.Images,
		Tags:        req.Tags,
		Locations:   req.Locations,
		Visibility:  visibility,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	c.JSON(http.StatusCreated, response)
}