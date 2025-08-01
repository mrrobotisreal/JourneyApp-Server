package models

type CreateAccountRequest struct {
	DisplayName string `json:"displayName"`
	Email    string `json:"email"`
	Password string `json:"password"`
}
