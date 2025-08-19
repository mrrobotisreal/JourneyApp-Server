package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"

	notificationsmodels "io.winapps.journeyapp/internal/models/notifications"
)

type NotificationsHandler struct {
	fcmClient   *messaging.Client
	db          *pgxpool.Pool
	redisClient *redis.Client
	cronManager *cron.Cron
}

func NewNotificationsHandler(firebaseApp *firebase.App, dbPool *pgxpool.Pool, redisClient *redis.Client) *NotificationsHandler {
	ctx := context.Background()

	fcmClient, err := firebaseApp.Messaging(ctx)
	if err != nil {
		log.Printf("error getting FCM client: %v", err)
	}

	c := cron.New(cron.WithLocation(time.UTC))

	h := &NotificationsHandler{
		fcmClient:   fcmClient,
		db:          dbPool,
		redisClient: redisClient,
		cronManager: c,
	}

	// Setup cron jobs for daily prompts
	h.setupDailyPromptScheduler()

	return h
}

// RegisterPushToken handles registering user push tokens
func (ns *NotificationsHandler) RegisterPushToken(c *gin.Context) {
	var tokenData notificationsmodels.PushToken
	if err := c.ShouldBindJSON(&tokenData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get user ID from Firebase JWT (set by AuthMiddleware)
	uid, exists := c.Get("uid")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	tokenData.UserID = uid.(string)
	tokenData.UpdatedAt = time.Now()

	// Upsert the token in PostgreSQL
	query := `
		INSERT INTO push_tokens (user_id, expo_push_token, fcm_token, platform, timezone, active)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id)
		DO UPDATE SET
			expo_push_token = EXCLUDED.expo_push_token,
			fcm_token = EXCLUDED.fcm_token,
			platform = EXCLUDED.platform,
			timezone = EXCLUDED.timezone,
			active = EXCLUDED.active,
			updated_at = NOW()
		RETURNING id`

	var id string
	err := ns.db.QueryRow(context.Background(), query,
		tokenData.UserID,
		tokenData.ExpoPushToken,
		tokenData.FCMToken,
		tokenData.Platform,
		tokenData.Timezone,
		true,
	).Scan(&id)

	if err != nil {
		log.Printf("Error saving push token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save token"})
		return
	}

	// Cache the token in Redis for quick access
	tokenKey := fmt.Sprintf("push_token:%s", tokenData.UserID)
	tokenJSON, _ := json.Marshal(tokenData)
	ns.redisClient.Set(context.Background(), tokenKey, tokenJSON, 24*time.Hour)

	c.JSON(http.StatusOK, gin.H{
		"message": "Token registered successfully",
		"id":      id,
	})
}

// SendNotification sends a notification via FCM or Expo push as fallback
func (ns *NotificationsHandler) SendNotification(expoOrFcmToken, title, body string, data map[string]string, channelID string) error {
	// If token looks like Expo token, use Expo push service
	if len(expoOrFcmToken) > 0 && (expoOrFcmToken[:6] == "ExpoPush" || expoOrFcmToken[:4] == "Expo") {
		return ns.sendExpoPush(expoOrFcmToken, title, body, data)
	}

	if ns.fcmClient == nil {
		return fmt.Errorf("FCM client not initialized")
	}

	message := &messaging.Message{
		Token: expoOrFcmToken,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Data: data,
		Android: &messaging.AndroidConfig{
			Notification: &messaging.AndroidNotification{
				ChannelID: channelID,
				Priority:  messaging.PriorityHigh,
			},
		},
		APNS: &messaging.APNSConfig{
			Payload: &messaging.APNSPayload{
				Aps: &messaging.Aps{
					Alert: &messaging.ApsAlert{
						Title: title,
						Body:  body,
					},
					Sound: "default",
					Badge: intPtr(1),
				},
			},
		},
	}

	response, err := ns.fcmClient.Send(context.Background(), message)
	if err != nil {
		return fmt.Errorf("error sending message: %v", err)
	}

	log.Printf("Successfully sent message: %s", response)
	return nil
}

func (ns *NotificationsHandler) sendExpoPush(expoToken, title, body string, data map[string]string) error {
	payload := []map[string]interface{}{
		{
			"to": expoToken,
			"title": title,
			"body": body,
			"sound": "default",
			"data": data,
		},
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", "https://exp.host/--/api/v2/push/send", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("expo push failed with status %d", resp.StatusCode)
	}
	return nil
}

// setupDailyPromptScheduler sets up cron jobs for each timezone at local 8 PM
func (ns *NotificationsHandler) setupDailyPromptScheduler() {
	// Get all unique timezones from users
	timezones := ns.getAllUserTimezones()

	for _, tzName := range timezones {
		loc, err := time.LoadLocation(tzName)
		if err != nil {
			log.Printf("Invalid timezone %s: %v", tzName, err)
			continue
		}

		// Compute next 8 PM local in this timezone, convert to UTC cron
		// We schedule each minute of the hour once per timezone to simplify cron expression
		// Use a specific minute derived from timezone name hash to stagger load
		minute := (int(hashString(tzName)) % 60)
		// 8 PM local => hour 20; cron runs in UTC, but we will run a function that checks local time inside
		// Simpler: Add a cron that runs every day at minute M of every hour, but gate inside by local 20:00
		// Better: Calculate UTC hour equivalent for 20:00 local at present offset
		now := time.Now().In(loc)
		_ = now
		localEight := time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(), 20, minute, 0, 0, loc)
		utcHour := localEight.UTC().Hour()

		// Add cron at computed UTC hour and selected minute
		spec := fmt.Sprintf("%d %d * * *", minute, utcHour)
		z := tzName
		_, err = ns.cronManager.AddFunc(spec, func() {
			ns.sendDailyPromptsForTimezone(z)
		})
		if err != nil {
			log.Printf("Error scheduling daily prompts for timezone %s: %v", tzName, err)
		}
	}

	ns.cronManager.Start()

	// Also schedule a job to refresh timezone list daily (in case new users register)
	ns.cronManager.AddFunc("0 0 * * *", func() {
		ns.refreshTimezoneScheduler()
	})
}

// getAllUserTimezones gets all unique timezones from registered users
func (ns *NotificationsHandler) getAllUserTimezones() []string {
	// First check Redis cache
	cacheKey := "user_timezones"
	cached := ns.redisClient.Get(context.Background(), cacheKey)
	if cached.Err() == nil {
		var timezones []string
		if err := json.Unmarshal([]byte(cached.Val()), &timezones); err == nil {
			return timezones
		}
	}

	// If not in cache, query PostgreSQL
	query := `SELECT DISTINCT timezone FROM push_tokens WHERE active = true`
	rows, err := ns.db.Query(context.Background(), query)
	if err != nil {
		log.Printf("Error getting timezones: %v", err)
		return []string{"UTC"} // Fallback
	}
	defer rows.Close()

	var timezones []string
	for rows.Next() {
		var timezone string
		if err := rows.Scan(&timezone); err == nil && timezone != "" {
			timezones = append(timezones, timezone)
		}
	}

	if len(timezones) == 0 {
		timezones = []string{"UTC"}
	}

	// Cache the result for 1 hour
	timezonesJSON, _ := json.Marshal(timezones)
	ns.redisClient.Set(context.Background(), cacheKey, timezonesJSON, time.Hour)

	return timezones
}

// refreshTimezoneScheduler refreshes the cron jobs when new timezones are added
func (ns *NotificationsHandler) refreshTimezoneScheduler() {
	// Clear cache to force refresh
	ns.redisClient.Del(context.Background(), "user_timezones")

	// Get updated timezones
	timezones := ns.getAllUserTimezones()
	_ = timezones
	// In production, we would reschedule jobs; for now, we log the list.
	log.Printf("Refreshed timezone scheduler. Active timezones: %v", timezones)
}

// sendDailyPromptsForTimezone sends daily prompts to users in a specific timezone
func (ns *NotificationsHandler) sendDailyPromptsForTimezone(timezone string) {
	log.Printf("Sending daily prompts for timezone: %s", timezone)

	// Generate or get today's prompt
	prompt := ns.getTodaysPrompt()

	// Get all users in this timezone from PostgreSQL
	query := `SELECT user_id, COALESCE(fcm_token, ''), expo_push_token FROM push_tokens WHERE timezone = $1 AND active = true`
	rows, err := ns.db.Query(context.Background(), query, timezone)
	if err != nil {
		log.Printf("Error finding users for timezone %s: %v", timezone, err)
		return
	}
	defer rows.Close()

	// Send notifications to each user
	for rows.Next() {
		var userID, fcmToken, expoToken string
		if err := rows.Scan(&userID, &fcmToken, &expoToken); err != nil {
			continue
		}

		var tokenToUse string
		if fcmToken != "" {
			tokenToUse = fcmToken
		} else {
			tokenToUse = expoToken
		}
		if tokenToUse == "" {
			continue
		}

		data := map[string]string{
			"type":   "daily_prompt",
			"prompt": prompt.Prompt,
			"date":   prompt.Date.Format("2006-01-02"),
		}

		err := ns.SendNotification(
			tokenToUse,
			"Daily Writing Prompt",
			prompt.Prompt,
			data,
			"prompts",
		)

		if err != nil {
			log.Printf("Failed to send daily prompt to user %s: %v", userID, err)
		}

		// Track notification sent in Redis (for analytics)
		notificationKey := fmt.Sprintf("notification_sent:%s:%s", userID, prompt.Date.Format("2006-01-02"))
		ns.redisClient.Set(context.Background(), notificationKey, "daily_prompt", 7*24*time.Hour)
	}
}

// getTodaysPrompt gets or generates today's writing prompt
func (ns *NotificationsHandler) getTodaysPrompt() notificationsmodels.DailyPrompt {
	today := time.Now().Truncate(24 * time.Hour)

	// First check Redis cache
	cacheKey := fmt.Sprintf("daily_prompt:%s", today.Format("2006-01-02"))
	cached := ns.redisClient.Get(context.Background(), cacheKey)
	if cached.Err() == nil {
		var prompt notificationsmodels.DailyPrompt
		if err := json.Unmarshal([]byte(cached.Val()), &prompt); err == nil {
			return prompt
		}
	}

	// Check PostgreSQL
	var prompt notificationsmodels.DailyPrompt
	query := `SELECT id, prompt, date, created_at FROM daily_prompts WHERE date = $1`
	err := ns.db.QueryRow(context.Background(), query, today).Scan(
		&prompt.ID, &prompt.Prompt, &prompt.Date, &prompt.CreatedAt,
	)

	if err != nil {
		// Generate a new prompt
		prompts := []string{
			// "What made you smile today?",
			// "Describe a moment when you felt truly grateful.",
			// "What's one thing you learned about yourself recently?",
			// "Write about a challenge you overcame this week.",
			// "What would you tell your younger self?",
			// "Describe your perfect day in detail.",
			// "What are three things you're looking forward to?",
			// "Write about someone who has positively influenced your life.",
			// "What's a skill you'd like to develop and why?",
			// "Describe a place that makes you feel peaceful.",
			// "What's the best advice you've ever received?",
			// "Write about a time you stepped out of your comfort zone.",
			"What made you smile today? Describe the moment in vivid detail and why it brought you joy.",
			"Write about a time you surprised yourself. What did you discover about your capabilities or character?",
			"If your current self could give advice to your past self from five years ago, what would you say?",
			"Describe a challenge you're currently facing as if you're explaining it to a wise friend. What insights emerge?",
			"What's one belief you held strongly in the past that you've since changed your mind about? What caused the shift?",
			"You wake up with the ability to communicate with inanimate objects for one day. What conversations do you have?",
			"Write a letter from your 80-year-old self to your current self. What wisdom do they share?",
			"Imagine you could time travel but only to witness (not change) one moment in history. Where would you go and why?",
			"You discover a door in your home that wasn't there yesterday. Where does it lead and what do you find?",
			"Write about your life as if it were a book. What would the current chapter be titled and why?",
			"Describe someone who has influenced your life without them knowing it. How did they impact you?",
			"Write about a conversation you wish you could have with someone no longer in your life.",
			"What's the most valuable lesson someone taught you without trying to teach you anything?",
			"If you could have dinner with any three people (living or dead), who would they be and what would you want to discuss?",
			"Write about a moment when someone showed you unexpected kindness. How did it change your day or perspective?",
			"Describe your perfect day, from morning to night, with unlimited resources and no constraints.",
			"What would you attempt if you knew you couldn't fail? Why haven't you started already?",
			"Write about a skill you'd love to master. What draws you to it and how would it change your life?",
			"If you could solve one problem in the world, what would it be and how would you approach it?",
			"Imagine you're 90 years old, looking back on your life. What are you most proud of accomplishing?",
			"Choose an ordinary object near you and write its secret life story. What adventures has it been on?",
			"Describe a place that feels magical to you. What makes it special and how does it affect your mood?",
			"Write about a small ritual or habit that brings you comfort. Why is it meaningful to you?",
			"If you could pause time for an hour while everyone else is frozen, how would you spend it?",
			"Describe the view from your window as if you're seeing it for the first time. What details stand out?",
			"What's something you've been avoiding that you know would be good for you? Explore why you're resisting it.",
			"Write about a fear you've overcome or are working to overcome. What steps have you taken?",
			"Describe a moment when you felt truly proud of yourself. What did you accomplish and why did it matter?",
			"If you could develop one new habit that would improve your life, what would it be and how would you implement it?",
			"Write about something you're grateful for that you might normally take for granted. Why does it deserve appreciation?",
		}

		// Simple rotation based on day of year
		dayOfYear := today.YearDay()
		selectedPrompt := prompts[dayOfYear%len(prompts)]

		prompt = notificationsmodels.DailyPrompt{
			ID:        uuid.New().String(),
			Prompt:    selectedPrompt,
			Date:      today,
			CreatedAt: time.Now(),
		}

		// Save the prompt to PostgreSQL
		insertQuery := `
			INSERT INTO daily_prompts (id, prompt, date, created_at)
			VALUES ($1, $2, $3, $4)`
		_, err := ns.db.Exec(context.Background(), insertQuery,
			prompt.ID, prompt.Prompt, prompt.Date, prompt.CreatedAt)

		if err != nil {
			log.Printf("Error saving daily prompt: %v", err)
		}
	}

	// Cache the prompt in Redis for quick access
	promptJSON, _ := json.Marshal(prompt)
	ns.redisClient.Set(context.Background(), cacheKey, promptJSON, 24*time.Hour)

	return prompt
}

// getPushTokenFromCache gets a user's push token from Redis cache first, then PostgreSQL
func (ns *NotificationsHandler) getPushTokenFromCache(userID string) (*notificationsmodels.PushToken, error) {
	// Check Redis first
	tokenKey := fmt.Sprintf("push_token:%s", userID)
	cached := ns.redisClient.Get(context.Background(), tokenKey)
	if cached.Err() == nil {
		var token notificationsmodels.PushToken
		if err := json.Unmarshal([]byte(cached.Val()), &token); err == nil {
			return &token, nil
		}
	}

	// If not in cache, query PostgreSQL
	var token notificationsmodels.PushToken
	query := `
		SELECT user_id, expo_push_token, fcm_token, platform, timezone, active
		FROM push_tokens
		WHERE user_id = $1 AND active = true`

	err := ns.db.QueryRow(context.Background(), query, userID).Scan(
		&token.UserID,
		&token.ExpoPushToken,
		&token.FCMToken,
		&token.Platform,
		&token.Timezone,
		&token.Active,
	)

	if err != nil {
		return nil, fmt.Errorf("user token not found: %v", err)
	}

	// Cache it for next time
	tokenJSON, _ := json.Marshal(token)
	ns.redisClient.Set(context.Background(), tokenKey, tokenJSON, 24*time.Hour)

	return &token, nil
}

// SendMessageNotification sends notification when user receives a message
func (ns *NotificationsHandler) SendMessageNotification(recipientUserID, senderName, messagePreview string) error {
	token, err := ns.getPushTokenFromCache(recipientUserID)
	if err != nil {
		return err
	}

	var tokenToUse string
	if token.FCMToken != nil && *token.FCMToken != "" {
		tokenToUse = *token.FCMToken
	} else {
		tokenToUse = token.ExpoPushToken
	}
	if tokenToUse == "" {
		return fmt.Errorf("no push token available for user %s", recipientUserID)
	}

	data := map[string]string{
		"type":         "new_message",
		"sender_name":  senderName,
		"preview":      messagePreview,
		"recipient_id": recipientUserID,
	}

	title := fmt.Sprintf("New message from %s", senderName)
	body := messagePreview

	// Track message notification in Redis
	notificationKey := fmt.Sprintf("message_notification:%s:%d", recipientUserID, time.Now().Unix())
	ns.redisClient.Set(context.Background(), notificationKey, senderName, 24*time.Hour)

	return ns.SendNotification(tokenToUse, title, body, data, "messages")
}

// Webhook handler for Stream Chat integration
func (ns *NotificationsHandler) HandleStreamChatWebhook(c *gin.Context) {
	var webhookData map[string]interface{}
	if err := c.ShouldBindJSON(&webhookData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if this is a new message event
	eventType, ok := webhookData["type"].(string)
	if !ok || eventType != "message.new" {
		c.JSON(http.StatusOK, gin.H{"message": "Event ignored"})
		return
	}

	// Extract message data
	message, ok := webhookData["message"].(map[string]interface{})
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid message data"})
		return
	}

	senderID, _ := message["user_id"].(string)
	messageText, _ := message["text"].(string)

	// Get channel members and send notifications to everyone except sender
	channelMembers := ns.getChannelMembers(webhookData)

	for _, memberID := range channelMembers {
		if memberID != senderID {
			senderName := ns.getUserDisplayName(senderID)

			err := ns.SendMessageNotification(memberID, senderName, messageText)
			if err != nil {
				log.Printf("Failed to send message notification to %s: %v", memberID, err)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Notifications sent"})
}

// Helper functions
func (ns *NotificationsHandler) getChannelMembers(webhookData map[string]interface{}) []string {
	// Extract channel members from Stream Chat webhook
	// This structure depends on your Stream Chat configuration
	channel, ok := webhookData["channel"].(map[string]interface{})
	if !ok {
		return []string{}
	}

	members, ok := channel["members"].([]interface{})
	if !ok {
		return []string{}
	}

	var memberIDs []string
	for _, member := range members {
		if memberMap, ok := member.(map[string]interface{}); ok {
			if userID, ok := memberMap["user_id"].(string); ok {
				memberIDs = append(memberIDs, userID)
			}
		}
	}

	return memberIDs
}

func (ns *NotificationsHandler) getUserDisplayName(userID string) string {
	// Check Redis cache first
	cacheKey := fmt.Sprintf("user_name:%s", userID)
	cached := ns.redisClient.Get(context.Background(), cacheKey)
	if cached.Err() == nil {
		return cached.Val()
	}

	// Query your users table in PostgreSQL
	var displayName string
	query := `SELECT display_name FROM users WHERE uid = $1`
	err := ns.db.QueryRow(context.Background(), query, userID).Scan(&displayName)

	if err != nil {
		displayName = "User" // Fallback
	}

	// Cache the result
	ns.redisClient.Set(context.Background(), cacheKey, displayName, time.Hour)

	return displayName
}

func intPtr(i int) *int {
	return &i
}

func hashString(s string) uint32 {
	var h uint32 = 2166136261
	const prime32 = 16777619
	for i := 0; i < len(s); i++ {
		h *= prime32
		h ^= uint32(s[i])
	}
	return h
}
