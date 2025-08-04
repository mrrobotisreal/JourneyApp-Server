package models

import "time"

type UpdateSettingsResponse struct {
	Success   bool      `json:"success"`
	Message   string    `json:"message"`
	UID       string    `json:"uid"`
	ThemeMode string    `json:"themeMode"`
	Theme     string    `json:"theme"`
	AppFont   string    `json:"appFont"`
	Lang      string    `json:"lang"`
	UpdatedAt time.Time `json:"updatedAt"`
}