package models

type CreateAccountRequest struct {
	IDToken             string `json:"idToken" binding:"required"`
	UID                 string `json:"uid" binding:"required"`
	DisplayName         string `json:"displayName"`
	Email               string `json:"email" binding:"required"`
	PhotoURL            string `json:"photoURL"`
	PhoneNumber         string `json:"phoneNumber"`
	EmailVerified       bool   `json:"emailVerified"`
	PhoneNumberVerified bool   `json:"phoneNumberVerified"`
}
