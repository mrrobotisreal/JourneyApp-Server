package models

type RemoveImageRequest struct {
	EntryID string `json:"entryId" binding:"required"`
	ImageURL string `json:"imageUrl" binding:"required"`
}