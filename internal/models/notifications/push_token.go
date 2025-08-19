package models

import "time"

type PushToken struct {
	ID            string    `json:"id" db:"id"`
	UserID        string    `json:"user_id" db:"user_id"`
	ExpoPushToken string    `json:"expo_push_token" db:"expo_push_token"`
	FCMToken      *string   `json:"fcm_token,omitempty" db:"fcm_token"`
	Platform      string    `json:"platform" db:"platform"`
	Timezone      string    `json:"timezone" db:"timezone"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
	Active        bool      `json:"active" db:"active"`
}