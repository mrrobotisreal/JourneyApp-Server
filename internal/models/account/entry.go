package models

import "time"

type Entry struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Images      []string  `json:"images"`
	Audio       []string  `json:"audio"`
	Tags        []Tag     `json:"tags"`
	Locations   []Location  `json:"locations"`
	Visibility  string    `json:"visibility"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}