package models

type AddImageResponse struct {
	EntryID  string `json:"entryId"`
	ImageURL string `json:"imageUrl"`
	Message  string `json:"message"`
}