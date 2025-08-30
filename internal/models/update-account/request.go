package models

import "time"

type UpdateAccountRequest struct {
	UID         				string    `json:"uid"`
	DisplayName 				string    `json:"displayName,omitempty"`
	Email       				string    `json:"email,omitempty"`
	PhoneNumber 				string    `json:"phoneNumber,omitempty"`
	PhotoURL    				string    `json:"photoURL,omitempty"`
	UpdatedAt   				time.Time `json:"updatedAt,omitempty"`
	CreatedAt   				time.Time `json:"createdAt,omitempty"`
	EmailVerified 			bool 			`json:"emailVerified,omitempty"`
	PhoneNumberVerified bool 			`json:"phoneNumberVerified,omitempty"`
	IsPremium 					bool 			`json:"isPremium,omitempty"` // TODO: remove this later
	PremiumExpiresAt 		time.Time `json:"premiumExpiresAt,omitempty"` // TODO: remove this later
}