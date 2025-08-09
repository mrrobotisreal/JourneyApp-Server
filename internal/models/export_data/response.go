package models

type ExportDataResponse struct {
	ExportJobID string `json:"exportJobId"`
	Message     string `json:"message"`
}