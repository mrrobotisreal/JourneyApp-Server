package models

import (
	accountmodels "io.winapps.journeyapp/internal/models/account"
)

type UpdateTagResponse struct {
	EntryID string                `json:"entryId"`
	OldTag  accountmodels.Tag     `json:"oldTag"`
	NewTag  accountmodels.Tag     `json:"newTag"`
	Message string                `json:"message"`
}