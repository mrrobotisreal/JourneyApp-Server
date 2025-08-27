package models

import (
	"time"

	accountmodels "io.winapps.journeyapp/internal/models/account"
)

type CreateEntryResponse struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Images      []string  `json:"images"`
	Tags        []accountmodels.Tag     `json:"tags"`
	Locations   []accountmodels.Location  `json:"locations"`
	Visibility  string    `json:"visibility"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}