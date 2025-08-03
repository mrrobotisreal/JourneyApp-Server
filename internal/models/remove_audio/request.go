package models

type RemoveAudioRequest struct {
	EntryID  string `json:"entryId" binding:"required"`
	AudioURL string `json:"audioUrl" binding:"required"`
}