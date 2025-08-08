package models

type DeleteEntryRequest struct {
	EntryID string `json:"entryId" binding:"required"`
}