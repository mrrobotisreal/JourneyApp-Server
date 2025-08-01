package db

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// InitRedis initializes and returns a Redis client
func InitRedis() (*redis.Client, error) {
	// Get Redis configuration from environment variables or use defaults
	host := getEnvOrDefault("REDIS_HOST", "localhost")
	port := getEnvOrDefault("REDIS_PORT", "6379")
	password := os.Getenv("REDIS_PASSWORD") // No default for password
	dbStr := getEnvOrDefault("REDIS_DB", "0")

	// Parse database number
	db, err := strconv.Atoi(dbStr)
	if err != nil {
		return nil, fmt.Errorf("invalid REDIS_DB value: %w", err)
	}

	// Create Redis client
	client := redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%s", host, port),
		Password:     password,
		DB:           db,
		DialTimeout:  10 * time.Second,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		PoolSize:     10,
		PoolTimeout:  30 * time.Second,
		MaxRetries:   3,
	})

	// Test the connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return client, nil
}
