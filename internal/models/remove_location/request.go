package models

import (
	accountmodels "io.winapps.journeyapp/internal/models/account"
)

type RemoveLocationRequest struct {
	EntryID  string                    `json:"entryId" binding:"required"`
	Location accountmodels.Location    `json:"location" binding:"required"`
}