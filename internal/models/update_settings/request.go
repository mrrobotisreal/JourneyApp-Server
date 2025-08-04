package models

type UpdateSettingsRequest struct {
	ThemeMode *string `json:"themeMode,omitempty"`
	Theme     *string `json:"theme,omitempty"`
	AppFont   *string `json:"appFont,omitempty"`
	Lang      *string `json:"lang,omitempty"`
}