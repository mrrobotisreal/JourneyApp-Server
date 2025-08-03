package models

type AddAudioRequest struct {
	EntryID string `json:"entryId" binding:"required"`
	Audio   string `json:"audio" binding:"required"` // Base64 encoded audio data
}