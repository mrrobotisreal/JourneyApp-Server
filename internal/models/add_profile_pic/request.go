package models

type AddProfilePicRequest struct {
	IsPhotoAttached bool 	 `json:"isPhotoAttached"`
	PhotoURL 				string `json:"photoURL,omitempty"`
}