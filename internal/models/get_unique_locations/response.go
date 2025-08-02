package models

import (
	accountmodels "io.winapps.journeyapp/internal/models/account"
)

type GetUniqueLocationsResponse struct {
	Locations []accountmodels.Location `json:"locations"`
}