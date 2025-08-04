package models

import "time"

type UserSettings struct {
	UID       string    `json:"uid" db:"uid"`
	ThemeMode string    `json:"themeMode" db:"theme_mode"`
	Theme     string    `json:"theme" db:"theme"`
	AppFont   string    `json:"appFont" db:"app_font"`
	Lang      string    `json:"lang" db:"lang"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt time.Time `json:"updatedAt" db:"updated_at"`
}