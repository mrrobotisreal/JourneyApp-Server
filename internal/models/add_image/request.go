package models

type AddImageRequest struct {
	EntryID string `json:"entryId" binding:"required"`
	Image   string `json:"image" binding:"required"` // Base64 encoded image data
}