package models

type Tag struct {
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}
