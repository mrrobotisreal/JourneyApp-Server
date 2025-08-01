package models

import (
	accountmodels "io.winapps.journeyapp/internal/models/account"
)

type RemoveLocationResponse struct {
	EntryID  string                    `json:"entryId"`
	Location accountmodels.Location    `json:"location"`
	Message  string                    `json:"message"`
}