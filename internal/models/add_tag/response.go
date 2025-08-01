package models

import (
	accountmodels "io.winapps.journeyapp/internal/models/account"
)

type AddTagResponse struct {
	EntryID string                `json:"entryId"`
	Tag     accountmodels.Tag     `json:"tag"`
	Message string                `json:"message"`
}