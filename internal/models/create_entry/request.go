package models

import (
	accountmodels "io.winapps.journeyapp/internal/models/account"
)

type CreateEntryRequest struct {
	UID         string    `json:"uid"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Tags        []accountmodels.Tag     `json:"tags"`
	Locations   []accountmodels.Location  `json:"locations"`
	Images      []string  `json:"images"`
}
