package models

import (
	"time"

	accountmodels "io.winapps.journeyapp/internal/models/account"
)

type SearchEntriesResponse struct {
	Entries    []EntryResult `json:"entries"`
	Pagination Pagination   `json:"pagination"`
}

type EntryResult struct {
	ID          string                      `json:"id"`
	Title       string                      `json:"title"`
	Description string                      `json:"description"`
	Images      []string                    `json:"images"`
	Audio       []string                    `json:"audio"`
	Tags        []accountmodels.Tag         `json:"tags"`
	Locations   []accountmodels.Location    `json:"locations"`
	Visibility  string                      `json:"visibility"`
	CreatedAt   time.Time                   `json:"createdAt"`
	UpdatedAt   time.Time                   `json:"updatedAt"`
}

type Pagination struct {
	Page         int  `json:"page"`
	Limit        int  `json:"limit"`
	Total        int  `json:"total"`
	TotalPages   int  `json:"totalPages"`
	HasNext      bool `json:"hasNext"`
	HasPrevious  bool `json:"hasPrevious"`
}