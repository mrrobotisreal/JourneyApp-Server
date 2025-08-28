package models

type SearchUserResult struct {
	UID string `json:"uid"`
	DisplayName string `json:"displayName"`
	Email string `json:"email"`
	PhotoURL string `json:"photoURL"`
	CreatedAt string `json:"createdAt"`
	IsPremium bool `json:"isPremium"`
}

type SearchUsersResponse struct {
	Results []SearchUserResult `json:"results"`
}