package models

import (
	accountmodels "io.winapps.journeyapp/internal/models/account"
)

type ListFeedResult struct {
	UID     string              `json:"uid"`
	Entries []accountmodels.Entry `json:"entries"`
}

type ListFeedsResponse struct {
	Feeds []ListFeedResult `json:"feeds"`
}
