package models

import (
	accountmodels "io.winapps.journeyapp/internal/models/account"
)

type UpdateLocationRequest struct {
	EntryID     string                    `json:"entryId" binding:"required"`
	OldLocation accountmodels.Location    `json:"oldLocation" binding:"required"`
	NewLocation accountmodels.Location    `json:"newLocation" binding:"required"`
}