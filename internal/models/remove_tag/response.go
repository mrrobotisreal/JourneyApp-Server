package models

import (
	accountmodels "io.winapps.journeyapp/internal/models/account"
)

type RemoveTagResponse struct {
	EntryID string                `json:"entryId"`
	Tag     accountmodels.Tag     `json:"tag"`
	Message string                `json:"message"`
}