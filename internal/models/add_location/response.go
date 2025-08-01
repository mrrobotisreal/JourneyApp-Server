package models

import (
	accountmodels "io.winapps.journeyapp/internal/models/account"
)

type AddLocationResponse struct {
	EntryID  string                    `json:"entryId"`
	Location accountmodels.Location    `json:"location"`
	Message  string                    `json:"message"`
}