package models

import "time"

type UpdateAccountResponse struct {
	UID         string    `json:"uid"`
	DisplayName string    `json:"displayName"`
	Email       string    `json:"email"`
	PhoneNumber string    `json:"phoneNumber"`
	PhotoURL    string    `json:"photoURL"`
	UpdatedAt   time.Time `json:"updatedAt"`
	CreatedAt   time.Time `json:"createdAt"`
	EmailVerified bool `json:"emailVerified"`
	PhoneNumberVerified bool `json:"phoneNumberVerified"`
	IsPremium bool `json:"isPremium"` // TODO: remove this later
	PremiumExpiresAt time.Time `json:"premiumExpiresAt"` // TODO: remove this later
}