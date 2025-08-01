package models

type RemoveImageResponse struct {
	EntryID  string `json:"entryId"`
	ImageURL string `json:"imageUrl"`
	Message  string `json:"message"`
}