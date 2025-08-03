package handlers

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	models "io.winapps.journeyapp/internal/models/account"
	searchmodels "io.winapps.journeyapp/internal/models/search_entries"
)

// SearchEntries handles searching and filtering journal entries with pagination
func (h *EntryHandler) SearchEntries(c *gin.Context) {
	var req searchmodels.SearchEntriesRequest

	// Parse URL query parameters for pagination
	if pageStr := c.Query("page"); pageStr != "" {
		if page, err := strconv.Atoi(pageStr); err == nil && page > 0 {
			req.Page = page
		}
	}
	if limitStr := c.Query("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 && limit <= 100 {
			req.Limit = limit
		}
	}

	// Parse JSON body for search query and filters
	if err := c.ShouldBindJSON(&req); err != nil {
		// If JSON parsing fails, it's still OK - we can search with just query params
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

	// Set defaults
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Filters.Timeframe.Type == "" {
		req.Filters.Timeframe.Type = "All"
	}
	if req.Filters.SortRule == "" {
		req.Filters.SortRule = "Newest"
	}

	ctx := context.Background()

	// Build the search query
	entries, total, err := h.searchEntriesWithFilters(ctx, userUID, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to search entries"})
		return
	}

	// Calculate pagination
	totalPages := int(math.Ceil(float64(total) / float64(req.Limit)))
	hasNext := req.Page < totalPages
	hasPrevious := req.Page > 1

	response := searchmodels.SearchEntriesResponse{
		Entries: entries,
		Pagination: searchmodels.Pagination{
			Page:        req.Page,
			Limit:       req.Limit,
			Total:       total,
			TotalPages:  totalPages,
			HasNext:     hasNext,
			HasPrevious: hasPrevious,
		},
	}

	c.JSON(http.StatusOK, response)
}

// searchEntriesWithFilters performs the actual search with all filters and returns entries
func (h *EntryHandler) searchEntriesWithFilters(ctx context.Context, userUID string, req searchmodels.SearchEntriesRequest) ([]searchmodels.EntryResult, int, error) {
	// Build WHERE clause
	whereConditions := []string{"e.user_uid = $1"}
	args := []interface{}{userUID}
	argCounter := 2

	// Add timeframe filter
	if req.Filters.Timeframe.Type != "All" {
		timeCondition, timeArgs := h.buildTimeframeCondition(req.Filters.Timeframe, argCounter)
		if timeCondition != "" {
			whereConditions = append(whereConditions, timeCondition)
			args = append(args, timeArgs...)
			argCounter += len(timeArgs)
		}
	}

	// Add search query filter
	searchJoins := ""
	if req.SearchQuery != "" {
		searchCondition := fmt.Sprintf(`(
			e.title ILIKE $%d OR
			e.description ILIKE $%d OR
			EXISTS (SELECT 1 FROM locations l WHERE l.entry_id = e.id AND l.display_name ILIKE $%d)
		)`, argCounter, argCounter, argCounter)
		whereConditions = append(whereConditions, searchCondition)
		searchTerm := "%" + req.SearchQuery + "%"
		args = append(args, searchTerm)
		argCounter++
	}

	// Add location filter
	if len(req.Filters.Locations) > 0 {
		locationConditions := []string{}
		for _, location := range req.Filters.Locations {
			if location.Latitude != 0 && location.Longitude != 0 {
				condition := fmt.Sprintf(`EXISTS (SELECT 1 FROM locations l WHERE l.entry_id = e.id AND l.latitude = $%d AND l.longitude = $%d)`, argCounter, argCounter+1)
				locationConditions = append(locationConditions, condition)
				args = append(args, location.Latitude, location.Longitude)
				argCounter += 2
			}
		}
		if len(locationConditions) > 0 {
			whereConditions = append(whereConditions, "("+strings.Join(locationConditions, " OR ")+")")
		}
	}

	// Add tags filter
	if len(req.Filters.Tags) > 0 {
		tagConditions := []string{}
		for _, tag := range req.Filters.Tags {
			condition := fmt.Sprintf(`EXISTS (SELECT 1 FROM tags t WHERE t.entry_id = e.id AND t.key = $%d AND t.value = $%d)`, argCounter, argCounter+1)
			tagConditions = append(tagConditions, condition)
			args = append(args, tag.Key, tag.Value)
			argCounter += 2
		}
		if len(tagConditions) > 0 {
			whereConditions = append(whereConditions, "("+strings.Join(tagConditions, " AND ")+")")
		}
	}

	whereClause := "WHERE " + strings.Join(whereConditions, " AND ")

	// Build ORDER BY clause
	orderBy := "ORDER BY e.created_at DESC"
	if req.Filters.SortRule == "Oldest" {
		orderBy = "ORDER BY e.created_at ASC"
	}

	// Count total entries
	countQuery := fmt.Sprintf(`
		SELECT COUNT(DISTINCT e.id)
		FROM entries e
		%s
		%s
	`, searchJoins, whereClause)

	var total int
	err := h.postgres.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count entries: %w", err)
	}

	// Calculate offset
	offset := (req.Page - 1) * req.Limit

	// Get entries
	entriesQuery := fmt.Sprintf(`
		SELECT DISTINCT e.id, e.title, e.description, e.created_at, e.updated_at
		FROM entries e
		%s
		%s
		%s
		LIMIT $%d OFFSET $%d
	`, searchJoins, whereClause, orderBy, argCounter, argCounter+1)

	args = append(args, req.Limit, offset)

	rows, err := h.postgres.Query(ctx, entriesQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query entries: %w", err)
	}
	defer rows.Close()

	var entryIDs []string
	entryMap := make(map[string]*searchmodels.EntryResult)

	for rows.Next() {
		var entry searchmodels.EntryResult
		if err := rows.Scan(&entry.ID, &entry.Title, &entry.Description, &entry.CreatedAt, &entry.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan entry: %w", err)
		}

		// Initialize slices
		entry.Images = []string{}
		entry.Tags = []models.Tag{}
		entry.Locations = []models.Location{}

		entryIDs = append(entryIDs, entry.ID)
		entryMap[entry.ID] = &entry
	}

	// Fetch related data for all entries
	if len(entryIDs) > 0 {
		if err := h.fetchRelatedDataForEntries(ctx, entryIDs, entryMap); err != nil {
			return nil, 0, fmt.Errorf("failed to fetch related data: %w", err)
		}
	}

	// Convert map to slice maintaining order
	var entries []searchmodels.EntryResult
	for _, entryID := range entryIDs {
		if entry, exists := entryMap[entryID]; exists {
			entries = append(entries, *entry)
		}
	}

	return entries, total, nil
}

// buildTimeframeCondition creates SQL condition for timeframe filter
func (h *EntryHandler) buildTimeframeCondition(timeframe searchmodels.TimeframeFilter, argCounter int) (string, []interface{}) {
	now := time.Now()

	switch timeframe.Type {
	case "custom":
		if timeframe.FromDate != nil && timeframe.ToDate != nil {
			return fmt.Sprintf("e.created_at BETWEEN $%d AND $%d", argCounter, argCounter+1),
				   []interface{}{*timeframe.FromDate, *timeframe.ToDate}
		}
	case "Past year":
		oneYearAgo := now.AddDate(-1, 0, 0)
		return fmt.Sprintf("e.created_at >= $%d", argCounter), []interface{}{oneYearAgo}
	case "Past 6 months":
		sixMonthsAgo := now.AddDate(0, -6, 0)
		return fmt.Sprintf("e.created_at >= $%d", argCounter), []interface{}{sixMonthsAgo}
	case "Past 3 months":
		threeMonthsAgo := now.AddDate(0, -3, 0)
		return fmt.Sprintf("e.created_at >= $%d", argCounter), []interface{}{threeMonthsAgo}
	case "Past 30 days":
		thirtyDaysAgo := now.AddDate(0, 0, -30)
		return fmt.Sprintf("e.created_at >= $%d", argCounter), []interface{}{thirtyDaysAgo}
	}

	return "", []interface{}{}
}

// fetchRelatedDataForEntries efficiently fetches tags, locations, and images for multiple entries
func (h *EntryHandler) fetchRelatedDataForEntries(ctx context.Context, entryIDs []string, entryMap map[string]*searchmodels.EntryResult) error {
	if len(entryIDs) == 0 {
		return nil
	}

	// Create placeholders for IN clause
	placeholders := make([]string, len(entryIDs))
	args := make([]interface{}, len(entryIDs))
	for i, id := range entryIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	inClause := strings.Join(placeholders, ",")

	// Fetch tags
	tagsQuery := fmt.Sprintf(`
		SELECT entry_id, key, value FROM tags
		WHERE entry_id IN (%s)
		ORDER BY entry_id, created_at
	`, inClause)

	tagRows, err := h.postgres.Query(ctx, tagsQuery, args...)
	if err != nil {
		return fmt.Errorf("failed to fetch tags: %w", err)
	}
	defer tagRows.Close()

	for tagRows.Next() {
		var entryID string
		var tag models.Tag
		if err := tagRows.Scan(&entryID, &tag.Key, &tag.Value); err != nil {
			return fmt.Errorf("failed to scan tag: %w", err)
		}
		if entry, exists := entryMap[entryID]; exists {
			entry.Tags = append(entry.Tags, tag)
		}
	}

	// Fetch locations
	locationsQuery := fmt.Sprintf(`
		SELECT entry_id, latitude, longitude, address, city, state, zip, country, country_code, display_name
		FROM locations
		WHERE entry_id IN (%s)
		ORDER BY entry_id, created_at
	`, inClause)

	locationRows, err := h.postgres.Query(ctx, locationsQuery, args...)
	if err != nil {
		return fmt.Errorf("failed to fetch locations: %w", err)
	}
	defer locationRows.Close()

	for locationRows.Next() {
		var entryID string
		var location models.Location
		if err := locationRows.Scan(
			&entryID,
			&location.Latitude,
			&location.Longitude,
			&location.Address,
			&location.City,
			&location.State,
			&location.Zip,
			&location.Country,
			&location.CountryCode,
			&location.DisplayName,
		); err != nil {
			return fmt.Errorf("failed to scan location: %w", err)
		}
		if entry, exists := entryMap[entryID]; exists {
			entry.Locations = append(entry.Locations, location)
		}
	}

	// Fetch images
	imagesQuery := fmt.Sprintf(`
		SELECT entry_id, url FROM images
		WHERE entry_id IN (%s)
		ORDER BY entry_id, upload_order
	`, inClause)

	imageRows, err := h.postgres.Query(ctx, imagesQuery, args...)
	if err != nil {
		return fmt.Errorf("failed to fetch images: %w", err)
	}
	defer imageRows.Close()

	for imageRows.Next() {
		var entryID, imageURL string
		if err := imageRows.Scan(&entryID, &imageURL); err != nil {
			return fmt.Errorf("failed to scan image: %w", err)
		}
		if entry, exists := entryMap[entryID]; exists {
			entry.Images = append(entry.Images, imageURL)
		}
	}

	// Fetch audio
	audioQuery := fmt.Sprintf(`
		SELECT entry_id, url FROM audio
		WHERE entry_id IN (%s)
		ORDER BY entry_id, upload_order
	`, inClause)

	audioRows, err := h.postgres.Query(ctx, audioQuery, args...)
	if err != nil {
		return fmt.Errorf("failed to fetch audio: %w", err)
	}
	defer audioRows.Close()

	for audioRows.Next() {
		var entryID, audioURL string
		if err := audioRows.Scan(&entryID, &audioURL); err != nil {
			return fmt.Errorf("failed to scan audio: %w", err)
		}
		if entry, exists := entryMap[entryID]; exists {
			entry.Audio = append(entry.Audio, audioURL)
		}
	}

	return nil
}