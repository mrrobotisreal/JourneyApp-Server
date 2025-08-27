package models

type UpdateEntryRequest struct {
	EntryID     string `json:"entryId"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Visibility  string `json:"visibility,omitempty"`
	SharedWith  []string `json:"sharedWith,omitempty"`
}