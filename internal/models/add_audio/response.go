package models

type AddAudioResponse struct {
	EntryID  string `json:"entryId"`
	AudioURL string `json:"audioUrl"`
	Message  string `json:"message"`
}