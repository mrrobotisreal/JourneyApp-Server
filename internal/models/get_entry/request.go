package models

type GetEntryRequest struct {
	EntryID string `json:"entryId" binding:"required"`
}