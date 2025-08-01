package models

import (
	accountmodels "io.winapps.journeyapp/internal/models/account"
)

type UpdateTagRequest struct {
	EntryID string                `json:"entryId" binding:"required"`
	OldTag  accountmodels.Tag     `json:"oldTag" binding:"required"`
	NewTag  accountmodels.Tag     `json:"newTag" binding:"required"`
}