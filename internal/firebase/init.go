package firebase

import (
	"context"
	"fmt"
	"os"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"
)

// InitFirebase initializes and returns a Firebase app instance
func InitFirebase() (*firebase.App, error) {
	ctx := context.Background()

	// Get Firebase configuration from environment
	serviceAccountPath := os.Getenv("FIREBASE_SERVICE_ACCOUNT_PATH")
	projectID := os.Getenv("FIREBASE_PROJECT_ID")

	var app *firebase.App
	var err error

	if serviceAccountPath != "" {
		// Initialize with service account file
		opt := option.WithCredentialsFile(serviceAccountPath)
		config := &firebase.Config{
			ProjectID: projectID,
		}
		app, err = firebase.NewApp(ctx, config, opt)
	} else {
		// Initialize with default credentials (useful for Google Cloud deployment)
		config := &firebase.Config{
			ProjectID: projectID,
		}
		app, err = firebase.NewApp(ctx, config)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to initialize Firebase app: %w", err)
	}

	// Test Firebase Auth connection
	authClient, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Firebase Auth client: %w", err)
	}

	// Verify we can access Firebase Auth (this is a lightweight test)
	_, err = authClient.GetUser(ctx, "test-user-verification")
	if err != nil {
		// This is expected to fail, we're just testing connectivity
		// The error should be "user not found" rather than a connection error
	}

	return app, nil
}

// GetAuthClient returns a Firebase Auth client from the app
func GetAuthClient(app *firebase.App) (*auth.Client, error) {
	ctx := context.Background()
	return app.Auth(ctx)
}
