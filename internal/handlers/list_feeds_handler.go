package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	accountmodels "io.winapps.journeyapp/internal/models/account"
	listfeedsmodels "io.winapps.journeyapp/internal/models/list-feeds"
)

// ListFeeds returns feeds of friends' entries visible to the requesting user
func (h *UsersHandler) ListFeeds(c *gin.Context) {
	// Ensure request is authenticated (middleware sets uid)
	_, authed := c.Get("uid")
	if !authed {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	// Get target UID from query or fallback to authenticated user
	targetUID := c.Query("uid")
	if targetUID == "" {
		if v, ok := c.Get("uid"); ok {
			if s, ok := v.(string); ok {
				targetUID = s
			}
		}
	}
	if targetUID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "uid is required"})
		return
	}

	ctx := context.Background()
	cacheKey := fmt.Sprintf("feeds:%s", targetUID)

	// Try Redis cache first
	if cached, err := h.redis.Get(ctx, cacheKey).Result(); err == nil && cached != "" {
		var cachedResponse listfeedsmodels.ListFeedsResponse
		if err := json.Unmarshal([]byte(cached), &cachedResponse); err == nil {
			c.JSON(http.StatusOK, cachedResponse)
			return
		}
	}

	// 1) Find approved friends for the target user
	friendsQuery := `
		SELECT DISTINCT CASE WHEN f.uid = $1 THEN f.fid ELSE f.uid END AS friend_uid
		FROM friendships f
		WHERE (f.uid = $1 OR f.fid = $1) AND f.status = 'approved'
	`

	friendRows, err := h.postgres.Query(ctx, friendsQuery, targetUID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list feeds"})
		return
	}
	defer friendRows.Close()

	friendUIDs := make([]string, 0)
	friendUIDSeen := make(map[string]bool)
	for friendRows.Next() {
		var uid string
		if err := friendRows.Scan(&uid); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read friends"})
			return
		}
		if !friendUIDSeen[uid] {
			friendUIDSeen[uid] = true
			friendUIDs = append(friendUIDs, uid)
		}
	}

	// If no friends, return empty feeds
	if len(friendUIDs) == 0 {
		response := listfeedsmodels.ListFeedsResponse{Feeds: []listfeedsmodels.ListFeedResult{}}
		// Cache empty result briefly
		if data, err := json.Marshal(response); err == nil {
			_ = h.redis.Set(ctx, cacheKey, data, 5*time.Minute).Err()
		}
		c.JSON(http.StatusOK, response)
		return
	}

	// 2) Fetch entries for all friends that are visible to target user
	placeholders := make([]string, len(friendUIDs))
	args := make([]interface{}, 0, 1+len(friendUIDs))
	args = append(args, targetUID) // $1 = target user for semi-private share check
	for i, uid := range friendUIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args = append(args, uid)
	}

	entriesQuery := fmt.Sprintf(`
		SELECT e.id, e.title, e.description, e.visibility, e.created_at, e.updated_at, e.user_uid
		FROM entries e
		WHERE e.user_uid IN (%s)
			AND (
				e.visibility = 'public'
				OR (
					e.visibility = 'semi-private'
					AND EXISTS (
						SELECT 1 FROM entry_shares es
						WHERE es.entry_id = e.id AND es.shared_user_uid = $1
					)
				)
			)
		ORDER BY e.user_uid, e.created_at DESC
	`, strings.Join(placeholders, ","))

	rows, err := h.postgres.Query(ctx, entriesQuery, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query feeds"})
		return
	}
	defer rows.Close()

	// Prepare maps for grouping and related data hydration
	friendToEntries := make(map[string][]accountmodels.Entry)
	entryMap := make(map[string]*accountmodels.Entry)
	entryIDs := make([]string, 0)

	for rows.Next() {
		var (
			id string
			title string
			description string
			visibility string
			createdAt time.Time
			updatedAt time.Time
			ownerUID string
		)
		if err := rows.Scan(&id, &title, &description, &visibility, &createdAt, &updatedAt, &ownerUID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read entries"})
			return
		}

		entry := accountmodels.Entry{
			ID:         id,
			Title:      title,
			Description: description,
			Images:     []string{},
			Audio:      []string{},
			Tags:       []accountmodels.Tag{},
			Locations:  []accountmodels.Location{},
			Visibility: visibility,
			CreatedAt:  createdAt,
			UpdatedAt:  updatedAt,
		}

		entryMap[id] = &entry
		entryIDs = append(entryIDs, id)
		friendToEntries[ownerUID] = append(friendToEntries[ownerUID], entry)
	}

	// 3) Hydrate related data (tags, locations, images, audio) for all entries in bulk
	if len(entryIDs) > 0 {
		placeholders = make([]string, len(entryIDs))
		idArgs := make([]interface{}, len(entryIDs))
		for i, eid := range entryIDs {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
			idArgs[i] = eid
		}
		inClause := strings.Join(placeholders, ",")

		// Tags
		tagsQuery := fmt.Sprintf(`
			SELECT entry_id, key, value FROM tags
			WHERE entry_id IN (%s)
			ORDER BY entry_id, created_at
		`, inClause)
		tagRows, err := h.postgres.Query(ctx, tagsQuery, idArgs...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tags"})
			return
		}
		for tagRows.Next() {
			var entryID string
			var tag accountmodels.Tag
			if err := tagRows.Scan(&entryID, &tag.Key, &tag.Value); err != nil {
				tagRows.Close()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read tags"})
				return
			}
			if e := entryMap[entryID]; e != nil {
				e.Tags = append(e.Tags, tag)
			}
		}
		tagRows.Close()

		// Locations
		locationsQuery := fmt.Sprintf(`
			SELECT entry_id, latitude, longitude, address, city, state, zip, country, country_code, display_name
			FROM locations
			WHERE entry_id IN (%s)
			ORDER BY entry_id, created_at
		`, inClause)
		locationRows, err := h.postgres.Query(ctx, locationsQuery, idArgs...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch locations"})
			return
		}
		for locationRows.Next() {
			var entryID string
			var loc accountmodels.Location
			if err := locationRows.Scan(
				&entryID,
				&loc.Latitude,
				&loc.Longitude,
				&loc.Address,
				&loc.City,
				&loc.State,
				&loc.Zip,
				&loc.Country,
				&loc.CountryCode,
				&loc.DisplayName,
			); err != nil {
				locationRows.Close()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read locations"})
				return
			}
			if e := entryMap[entryID]; e != nil {
				e.Locations = append(e.Locations, loc)
			}
		}
		locationRows.Close()

		// Images
		imagesQuery := fmt.Sprintf(`
			SELECT entry_id, url FROM images
			WHERE entry_id IN (%s)
			ORDER BY entry_id, upload_order
		`, inClause)
		imageRows, err := h.postgres.Query(ctx, imagesQuery, idArgs...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch images"})
			return
		}
		for imageRows.Next() {
			var entryID, url string
			if err := imageRows.Scan(&entryID, &url); err != nil {
				imageRows.Close()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read images"})
				return
			}
			if e := entryMap[entryID]; e != nil {
				e.Images = append(e.Images, url)
			}
		}
		imageRows.Close()

		// Audio
		audioQuery := fmt.Sprintf(`
			SELECT entry_id, url FROM audio
			WHERE entry_id IN (%s)
			ORDER BY entry_id, upload_order
		`, inClause)
		audioRows, err := h.postgres.Query(ctx, audioQuery, idArgs...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch audio"})
			return
		}
		for audioRows.Next() {
			var entryID, url string
			if err := audioRows.Scan(&entryID, &url); err != nil {
				audioRows.Close()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read audio"})
				return
			}
			if e := entryMap[entryID]; e != nil {
				e.Audio = append(e.Audio, url)
			}
		}
		audioRows.Close()
	}

	// 4) Build response grouped by friend UID
	feeds := make([]listfeedsmodels.ListFeedResult, 0, len(friendUIDs))
	for _, fuid := range friendUIDs {
		entries := friendToEntries[fuid]
		// Ensure entries reflect hydrated data from pointers
		for i := range entries {
			if e := entryMap[entries[i].ID]; e != nil {
				entries[i] = *e
			}
		}
		feeds = append(feeds, listfeedsmodels.ListFeedResult{
			UID:     fuid,
			Entries: entries,
		})
	}

	response := listfeedsmodels.ListFeedsResponse{Feeds: feeds}

	// Cache for a short period
	if data, err := json.Marshal(response); err == nil {
		_ = h.redis.Set(ctx, cacheKey, data, 5*time.Minute).Err()
	}

	c.JSON(http.StatusOK, response)
}
