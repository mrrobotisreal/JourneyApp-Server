package models

import "time"

type SearchUserResult struct {
	UID string `json:"uid"`
	DisplayName string `json:"displayName"`
	Email string `json:"email"`
	PhotoURL string `json:"photoURL"`
	CreatedAt time.Time `json:"createdAt"`
	IsPremium bool `json:"isPremium"`
}

type SearchUsersResponse struct {
	Results []SearchUserResult `json:"results"`
}