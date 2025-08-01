package models

type AddImageRequest struct {
	EntryID string `json:"entryId" binding:"required"`
	ImageURL string `json:"imageUrl" binding:"required"`
}