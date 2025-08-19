package db

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// InitPostgres initializes and returns a PostgreSQL connection pool
func InitPostgres() (*pgxpool.Pool, error) {
	// Get database URL from environment variable or use default
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		// Default local development configuration
		host := getEnvOrDefault("POSTGRES_HOST", "localhost")
		port := getEnvOrDefault("POSTGRES_PORT", "5432")
		user := getEnvOrDefault("POSTGRES_USER", "mitchwintrow")
		password := getEnvOrDefault("POSTGRES_PASSWORD", "")
		dbname := getEnvOrDefault("POSTGRES_DB", "journeyapp")
		sslmode := getEnvOrDefault("POSTGRES_SSLMODE", "disable")

		databaseURL = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
			user, password, host, port, dbname, sslmode)
	}

	// Configure connection pool
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	// Set connection pool settings
	config.MaxConns = 25
	config.MinConns = 5
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = time.Minute * 30
	config.HealthCheckPeriod = time.Minute * 5

	// Create connection pool
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test the connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Create tables if they don't exist
	if err := createTables(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return pool, nil
}

// createTables creates all required tables if they don't exist
func createTables(ctx context.Context, pool *pgxpool.Pool) error {
	// Users table - stores Firebase user information
	usersTable := `
		CREATE TABLE IF NOT EXISTS users (
			uid VARCHAR(255) PRIMARY KEY,
			display_name VARCHAR(255),
			email VARCHAR(255) UNIQUE NOT NULL,
			token TEXT,
			photo_url TEXT,
			phone_number VARCHAR(20),
			provider_id VARCHAR(100),
			refresh_token TEXT,
			tenant_id VARCHAR(100),
			provider VARCHAR(100),
			email_verified BOOLEAN DEFAULT FALSE,
			phone_number_verified BOOLEAN DEFAULT FALSE,
			is_premium BOOLEAN NOT NULL DEFAULT FALSE,
			premium_expires_at TIMESTAMP NULL,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW(),
			CONSTRAINT users_premium_consistency CHECK (
				(is_premium = TRUE AND premium_expires_at IS NOT NULL) OR
				(is_premium = FALSE AND premium_expires_at IS NULL)
			)
		);
	`

	// User Settings table - stores user preferences and settings
	userSettingsTable := `
		CREATE TABLE IF NOT EXISTS user_settings (
			uid VARCHAR(255) PRIMARY KEY REFERENCES users(uid) ON DELETE CASCADE,
			theme_mode VARCHAR(10) DEFAULT 'light' CHECK (theme_mode IN ('light', 'dark')),
			theme VARCHAR(20) DEFAULT 'default' CHECK (theme IN ('default', 'royal', 'sunset', 'coral', 'beach', 'rose', 'ocean')),
			app_font VARCHAR(20) DEFAULT 'Montserrat' CHECK (app_font IN ('Montserrat', 'Bauhaus', 'PlayfairDisplay', 'Ubuntu')),
			lang VARCHAR(5) DEFAULT 'en' CHECK (lang IN ('en', 'ar', 'de', 'es', 'fr', 'he', 'ja', 'ko', 'pt', 'ru', 'uk', 'vi', 'zh')),
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		);
	`

	// Entries table - stores journal entries
	entriesTable := `
		CREATE TABLE IF NOT EXISTS entries (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_uid VARCHAR(255) NOT NULL REFERENCES users(uid) ON DELETE CASCADE,
			title VARCHAR(500) NOT NULL,
			description TEXT,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		);
	`

	// Locations table - stores location information for entries
	locationsTable := `
		CREATE TABLE IF NOT EXISTS locations (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			entry_id UUID NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
			latitude DECIMAL(10, 8),
			longitude DECIMAL(11, 8),
			address TEXT,
			city VARCHAR(255),
			state VARCHAR(255),
			zip VARCHAR(20),
			country VARCHAR(255),
			country_code VARCHAR(10),
			display_name VARCHAR(500),
			created_at TIMESTAMP DEFAULT NOW()
		);
	`

	// Tags table - stores tags for entries
	tagsTable := `
		CREATE TABLE IF NOT EXISTS tags (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			entry_id UUID NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
			key VARCHAR(255) NOT NULL,
			value TEXT,
			created_at TIMESTAMP DEFAULT NOW(),
			UNIQUE(entry_id, key)
		);
	`

	// Images table - stores image information for entries
	imagesTable := `
		CREATE TABLE IF NOT EXISTS images (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			entry_id UUID NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
			url TEXT NOT NULL,
			filename VARCHAR(500),
			file_size BIGINT,
			mime_type VARCHAR(100),
			width INTEGER,
			height INTEGER,
			upload_order INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT NOW()
		);
	`

	// Audio table - stores audio information for entries
	audioTable := `
		CREATE TABLE IF NOT EXISTS audio (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			entry_id UUID NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
			url TEXT NOT NULL,
			filename VARCHAR(500),
			file_size BIGINT,
			mime_type VARCHAR(100),
			duration INTEGER,
			upload_order INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT NOW()
		);
	`

	// Push tokens - stores device push registration
	pushTokensTable := `
		CREATE TABLE IF NOT EXISTS push_tokens (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id VARCHAR(255) NOT NULL,
			expo_push_token TEXT NOT NULL,
			fcm_token TEXT,
			platform VARCHAR(20) NOT NULL,
			timezone VARCHAR(50) NOT NULL DEFAULT 'UTC',
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			active BOOLEAN DEFAULT TRUE,
			UNIQUE(user_id)
		);
	`

	// Daily prompts - stores generated/selected prompts by date
	dailyPromptsTable := `
		CREATE TABLE IF NOT EXISTS daily_prompts (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			prompt TEXT NOT NULL,
			date DATE NOT NULL UNIQUE,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);
	`

	// Create indexes for better performance
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);`,
		`CREATE INDEX IF NOT EXISTS idx_user_settings_uid ON user_settings(uid);`,
		`CREATE INDEX IF NOT EXISTS idx_entries_user_uid ON entries(user_uid);`,
		`CREATE INDEX IF NOT EXISTS idx_entries_created_at ON entries(created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_locations_entry_id ON locations(entry_id);`,
		`CREATE INDEX IF NOT EXISTS idx_locations_coords ON locations(latitude, longitude);`,
		`CREATE INDEX IF NOT EXISTS idx_tags_entry_id ON tags(entry_id);`,
		`CREATE INDEX IF NOT EXISTS idx_tags_key ON tags(key);`,
		`CREATE INDEX IF NOT EXISTS idx_images_entry_id ON images(entry_id);`,
		`CREATE INDEX IF NOT EXISTS idx_images_upload_order ON images(entry_id, upload_order);`,
		`CREATE INDEX IF NOT EXISTS idx_audio_entry_id ON audio(entry_id);`,
		`CREATE INDEX IF NOT EXISTS idx_audio_upload_order ON audio(entry_id, upload_order);`,
		`CREATE INDEX IF NOT EXISTS idx_push_tokens_user_id ON push_tokens(user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_push_tokens_active ON push_tokens(active);`,
		`CREATE INDEX IF NOT EXISTS idx_push_tokens_timezone ON push_tokens(timezone);`,
		`CREATE INDEX IF NOT EXISTS idx_daily_prompts_date ON daily_prompts(date);`,
	}

	// Execute table creation statements
	tables := []string{usersTable, userSettingsTable, entriesTable, locationsTable, tagsTable, imagesTable, audioTable, pushTokensTable, dailyPromptsTable}

	for _, table := range tables {
		if _, err := pool.Exec(ctx, table); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	// Ensure premium columns exist on users for existing databases
	if _, err := pool.Exec(ctx, `ALTER TABLE users ADD COLUMN IF NOT EXISTS is_premium BOOLEAN NOT NULL DEFAULT FALSE;`); err != nil {
		return fmt.Errorf("failed to add is_premium column: %w", err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE users ADD COLUMN IF NOT EXISTS premium_expires_at TIMESTAMP NULL;`); err != nil {
		return fmt.Errorf("failed to add premium_expires_at column: %w", err)
	}
	// Note: adding a CHECK constraint conditionally is version-dependent; skipping if already present
	if _, err := pool.Exec(ctx, `DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'users_premium_consistency') THEN ALTER TABLE users ADD CONSTRAINT users_premium_consistency CHECK ((is_premium = TRUE AND premium_expires_at IS NOT NULL) OR (is_premium = FALSE AND premium_expires_at IS NULL)); END IF; END $$;`); err != nil {
		return fmt.Errorf("failed to add users_premium_consistency constraint: %w", err)
	}

	// Execute index creation statements
	for _, index := range indexes {
		if _, err := pool.Exec(ctx, index); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// getEnvOrDefault returns the environment variable value or a default value if not set
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
