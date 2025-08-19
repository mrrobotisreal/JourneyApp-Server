package models

import "time"

type DailyPrompt struct {
	ID        string    `json:"id" db:"id"`
	Prompt    string    `json:"prompt" db:"prompt"`
	Date      time.Time `json:"date" db:"date"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}