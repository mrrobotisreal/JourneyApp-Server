package models

type AddProfilePicResponse struct {
	Success bool `json:"success"`
	Message string `json:"message"`
	PhotoURL string `json:"photoURL"`
}