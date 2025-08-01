package models

type LoginResponse struct {
	UID   string `json:"uid"`
	DisplayName string `json:"displayName"`
	Email string `json:"email"`
	Token string `json:"token"`
	PhotoURL string `json:"photoURL"`
	PhoneNumber string `json:"phoneNumber"`
	ProviderID string `json:"providerId"`
	RefreshToken string `json:"refreshToken"`
	TenantID string `json:"tenantId"`
	Provider string `json:"provider"`
	EmailVerified bool `json:"emailVerified"`
	PhoneNumberVerified bool `json:"phoneNumberVerified"`
}
