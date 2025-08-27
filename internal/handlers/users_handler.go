package handlers

import (
	firebase "firebase.google.com/go/v4"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type UsersHandler struct {
	firebaseApp *firebase.App
	postgres    *pgxpool.Pool
	redis       *redis.Client
}

// NewUsersHandler creates a new users handler
func NewUsersHandler(firebaseApp *firebase.App, postgres *pgxpool.Pool, redis *redis.Client) *UsersHandler {
	return &UsersHandler{
		firebaseApp: firebaseApp,
		postgres:    postgres,
		redis:       redis,
	}
}