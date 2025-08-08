package models

import "time"

type GetAccountDetailsResponse struct {
	IDToken             string    `json:"idToken" binding:"required"`
	UID                 string    `json:"uid" binding:"required"`
	DisplayName         string    `json:"displayName"`
	Email               string    `json:"email" binding:"required"`
	PhotoURL            string    `json:"photoURL"`
	PhoneNumber         string    `json:"phoneNumber"`
	EmailVerified       bool      `json:"emailVerified"`
	PhoneNumberVerified bool      `json:"phoneNumberVerified"`
	ThemeMode           string    `json:"themeMode" binding:"required"`
	Theme               string    `json:"theme" binding:"required"`
	AppFont             string    `json:"appFont" binding:"required"`
	Lang                string    `json:"lang" binding:"required"`
	AccountCreatedAt    time.Time `json:"accountCreatedAt" binding:"required"`
	AccountUpdatedAt    time.Time `json:"accountUpdatedAt" binding:"required"`
	SettingsCreatedAt   time.Time `json:"settingsCreatedAt" binding:"required"`
	SettingsUpdatedAt   time.Time `json:"settingsUpdatedAt" binding:"required"`
	TotalEntries        int       `json:"totalEntries" binding:"required"`
	TotalTags           int       `json:"totalTags" binding:"required"`
	TotalLocations      int       `json:"totalLocations" binding:"required"`
	TotalImages         int       `json:"totalImages" binding:"required"`
	TotalAudios         int       `json:"totalAudios" binding:"required"`
	TotalVideos         int       `json:"totalVideos" binding:"required"`
	IsPremium           bool      `json:"isPremium" binding:"required"`
	PremiumExpiresAt    time.Time `json:"premiumExpiresAt" binding:"required"`
}