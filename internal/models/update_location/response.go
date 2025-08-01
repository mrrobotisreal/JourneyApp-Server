package models

import (
	accountmodels "io.winapps.journeyapp/internal/models/account"
)

type UpdateLocationResponse struct {
	EntryID     string                    `json:"entryId"`
	OldLocation accountmodels.Location    `json:"oldLocation"`
	NewLocation accountmodels.Location    `json:"newLocation"`
	Message     string                    `json:"message"`
}