package handlers

import (
	firebase "firebase.google.com/go/v4"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type UsersHandler struct {
	firebaseApp *firebase.App
	postgres    *pgxpool.Pool
	redis       *redis.Client
	logger      *zap.SugaredLogger
}

// NewUsersHandler creates a new users handler
func NewUsersHandler(firebaseApp *firebase.App, postgres *pgxpool.Pool, redis *redis.Client, logger *zap.SugaredLogger) *UsersHandler {
	return &UsersHandler{
		firebaseApp: firebaseApp,
		postgres:    postgres,
		redis:       redis,
		logger:      logger,
	}
}