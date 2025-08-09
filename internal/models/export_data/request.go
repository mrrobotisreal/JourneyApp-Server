package models

type ExportDataRequest struct {
	UID string `json:"uid" binding:"required"`
}