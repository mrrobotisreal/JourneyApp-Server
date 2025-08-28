package models

import "time"

type GetUserDetailsResponse struct {
	UID string `json:"uid" binding:"required"`
	DisplayName string `json:"displayName" binding:"required"`
	Email string `json:"email" binding:"required"`
	PhotoURL string `json:"photoURL" binding:"required"`
	CreatedAt time.Time `json:"createdAt" binding:"required"`
	TotalEntries int `json:"totalEntries" binding:"required"`
	IsPremium bool `json:"isPremium" binding:"required"`
}