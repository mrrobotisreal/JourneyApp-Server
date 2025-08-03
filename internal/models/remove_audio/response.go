package models

type RemoveAudioResponse struct {
	EntryID  string `json:"entryId"`
	AudioURL string `json:"audioUrl"`
	Message  string `json:"message"`
}