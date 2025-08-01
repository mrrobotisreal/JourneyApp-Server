package models

import (
	accountmodels "io.winapps.journeyapp/internal/models/account"
)

type RemoveTagRequest struct {
	EntryID string                `json:"entryId" binding:"required"`
	Tag     accountmodels.Tag     `json:"tag" binding:"required"`
}