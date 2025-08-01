package models

type LoginRequest struct {
	Email    string `json:"email,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
}
