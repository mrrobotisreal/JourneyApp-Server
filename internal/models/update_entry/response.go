package models

import (
	"time"

	accountmodels "io.winapps.journeyapp/internal/models/account"
)

type UpdateEntryResponse struct {
	ID          string                      `json:"id"`
	Title       string                      `json:"title"`
	Description string                      `json:"description"`
	Images      []string                    `json:"images"`
	Audio       []string                    `json:"audio"`
	Tags        []accountmodels.Tag         `json:"tags"`
	Locations   []accountmodels.Location    `json:"locations"`
	CreatedAt   time.Time                   `json:"createdAt"`
	UpdatedAt   time.Time                   `json:"updatedAt"`
}