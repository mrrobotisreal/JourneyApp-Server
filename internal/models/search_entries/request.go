package models

import (
	"time"

	accountmodels "io.winapps.journeyapp/internal/models/account"
)

type SearchEntriesRequest struct {
	SearchQuery string                      `json:"searchQuery,omitempty"`
	Filters     SearchFilters              `json:"filters,omitempty"`
	Page        int                        `json:"page,omitempty"`        // Default: 1
	Limit       int                        `json:"limit,omitempty"`       // Default: 20
}

type SearchFilters struct {
	Timeframe TimeframeFilter             `json:"timeframe,omitempty"`
	SortRule  string                     `json:"sortRule,omitempty"`    // "Newest" (default) or "Oldest"
	Locations []accountmodels.Location   `json:"locations,omitempty"`
	Tags      []accountmodels.Tag        `json:"tags,omitempty"`
	Visibilities []string                `json:"visibilities,omitempty"`
}

type TimeframeFilter struct {
	Type     string     `json:"type,omitempty"`     // "All" (default), "custom", "Past year", "Past 6 months", "Past 3 months", "Past 30 days"
	FromDate *time.Time `json:"fromDate,omitempty"` // Required when Type is "custom"
	ToDate   *time.Time `json:"toDate,omitempty"`   // Required when Type is "custom"
}