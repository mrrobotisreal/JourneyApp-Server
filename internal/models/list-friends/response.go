package models

import "time"

type ListFriendsResponse struct {
	Friends []ListFriend `json:"friends"`
}

type ListFriend struct {
	UID string `json:"uid"`
	DisplayName string `json:"displayName"`
	Email string `json:"email"`
	PhotoURL string `json:"photoURL"`
	Status string `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}