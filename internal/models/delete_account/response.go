package models

type DeleteAccountResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}