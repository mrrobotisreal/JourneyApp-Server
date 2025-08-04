package models

type CreateUserResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	UID     string `json:"uid"`
}
