package models

type Location struct {
	Latitude  float64 `json:"latitude,omitempty"`
	Longitude float64 `json:"longitude,omitempty"`
	Address   string  `json:"address,omitempty"`
	City      string  `json:"city,omitempty"`
	State     string  `json:"state,omitempty"`
	Zip       string  `json:"zip,omitempty"`
	Country   string  `json:"country,omitempty"`
	CountryCode string  `json:"countryCode,omitempty"`
	DisplayName string  `json:"displayName,omitempty"`
}
