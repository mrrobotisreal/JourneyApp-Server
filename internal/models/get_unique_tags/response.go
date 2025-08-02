package models

import (
	accountmodels "io.winapps.journeyapp/internal/models/account"
)

type GetUniqueTagsResponse struct {
	Tags []accountmodels.Tag `json:"tags"`
}