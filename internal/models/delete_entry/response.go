package models

type DeleteEntryResponse struct {
	IsDeleted bool   `json:"isDeleted"`
	Message   string `json:"message"`
}