package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"io.winapps.journeyapp/internal/db"
	firebaseutil "io.winapps.journeyapp/internal/firebase"
	"io.winapps.journeyapp/internal/handlers"
	"io.winapps.journeyapp/internal/middleware"
)

func main() {
	// Load environment variables from .env file (try multiple locations)
	if err := godotenv.Load(); err != nil {
		_ = godotenv.Load(".env", "../.env", "../../.env", "JourneyAppServer/.env", "cmd/api/.env")
	}

	log.Println("STREAM_API_KEY", os.Getenv("STREAM_API_KEY"))
	if os.Getenv("STREAM_API_KEY") == "" || os.Getenv("STREAM_API_SECRET") == "" {
		log.Println("Warning: STREAM_API_KEY or STREAM_API_SECRET not set. Stream-dependent endpoints will fail.")
	}

	// Initialize Firebase
	firebaseApp, err := firebaseutil.InitFirebase()
	if err != nil {
		log.Fatalf("Failed to initialize Firebase: %v", err)
	}

	// Initialize PostgreSQL
	postgresDB, err := db.InitPostgres()
	if err != nil {
		log.Fatalf("Failed to initialize PostgreSQL: %v", err)
	}
	defer postgresDB.Close()

	// Initialize Redis
	redisClient, err := db.InitRedis()
	if err != nil {
		log.Fatalf("Failed to initialize Redis: %v", err)
	}
	defer redisClient.Close()

	// Initialize Gin router
	router := gin.Default()

	// Add CORS middleware for mobile app
	router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(firebaseApp, postgresDB, redisClient)
	entryHandler := handlers.NewEntryHandler(firebaseApp, postgresDB, redisClient)
	usersHandler := handlers.NewUsersHandler(firebaseApp, postgresDB, redisClient)

	// Define routes
	v1 := router.Group("/api/v1")
	{
		auth := v1.Group("/auth")
		{
			auth.POST("/create-account", authHandler.CreateAccount)
			auth.POST("/validate-display-name", authHandler.ValidateDisplayName)
			auth.POST("/delete-account", middleware.AuthMiddleware(firebaseApp, postgresDB, redisClient), authHandler.DeleteAccount)
			auth.POST("/update-settings", middleware.AuthMiddleware(firebaseApp, postgresDB, redisClient), authHandler.UpdateSettings)
			auth.GET("/get-account-details", middleware.AuthMiddleware(firebaseApp, postgresDB, redisClient), authHandler.GetAccountDetails)
			auth.POST("/add-profile-pic", middleware.AuthMiddleware(firebaseApp, postgresDB, redisClient), authHandler.AddProfilePic)
			auth.POST("/export-data", middleware.AuthMiddleware(firebaseApp, postgresDB, redisClient), authHandler.ExportData)
			auth.GET("/export-progress", middleware.AuthMiddleware(firebaseApp, postgresDB, redisClient), authHandler.ExportProgress)
			auth.GET("/download-exported-data", middleware.AuthMiddleware(firebaseApp, postgresDB, redisClient), authHandler.DownloadExportedData)
		}

		// Notifications routes
		notificationsHandler := handlers.NewNotificationsHandler(firebaseApp, postgresDB, redisClient)
		notifications := v1.Group("/notifications")
		notifications.Use(middleware.AuthMiddleware(firebaseApp, postgresDB, redisClient))
		{
			notifications.POST("/register-for-notifications", notificationsHandler.RegisterPushToken)
			notifications.POST("/stream-chat-webhook", notificationsHandler.HandleStreamChatWebhook)
			notifications.GET("/get-notification-stats", notificationsHandler.GetNotificationStats)
		}

		// Protected entries routes
		entries := v1.Group("/entries")
		entries.Use(middleware.AuthMiddleware(firebaseApp, postgresDB, redisClient))
		{
			entries.POST("/create-entry", entryHandler.CreateEntry)
			entries.POST("/get-entry", entryHandler.GetEntry)
			entries.POST("/search-entries", entryHandler.SearchEntries)
			entries.POST("/add-tag", entryHandler.AddTag)
			entries.POST("/update-tag", entryHandler.UpdateTag)
			entries.POST("/remove-tag", entryHandler.RemoveTag)
			entries.POST("/add-location", entryHandler.AddLocation)
			entries.POST("/update-location", entryHandler.UpdateLocation)
			entries.POST("/remove-location", entryHandler.RemoveLocation)
			entries.POST("/add-image", entryHandler.AddImage)
			entries.POST("/remove-image", entryHandler.RemoveImage)
			entries.POST("/add-audio", entryHandler.AddAudio)
			entries.POST("/remove-audio", entryHandler.RemoveAudio)
			entries.POST("/get-unique-tags", entryHandler.GetUniqueTags)
			entries.POST("/get-unique-locations", entryHandler.GetUniqueLocations)
			entries.POST("/update-entry", entryHandler.UpdateEntry)
			entries.DELETE("/delete-entry", entryHandler.DeleteEntry)
		}

		// Protected users routes
		users := v1.Group("/users")
		users.Use(middleware.AuthMiddleware(firebaseApp, postgresDB, redisClient))
		{
			users.GET("/get-user-details", usersHandler.GetUserDetails)
			users.GET("/search-users", usersHandler.SearchUsers)
			users.GET("/list-friends", usersHandler.ListFriends)
			users.POST("/add-friend", usersHandler.AddFriendship)
			users.POST("/approve-friend-request", usersHandler.ApproveFriendRequest)
			users.POST("/reject-friend-request", usersHandler.RejectFriendRequest)
			users.DELETE("/remove-friend", usersHandler.RemoveFriendship)
			users.GET("/list-feeds", usersHandler.ListFeeds)
		}
	}

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Serve static image files
	router.Static("/images", "./internal/images")

	// Serve static audio files
	router.Static("/audio", "./internal/audio")

	// Create HTTP server
	srv := &http.Server{
		Addr:    ":9091",
		Handler: router,
	}

	// Start server in a goroutine
	go func() {
		log.Println("Server starting on port 9091...")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Give a 5 second timeout for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}
