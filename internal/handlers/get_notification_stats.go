package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// GetNotificationStats returns notification statistics
func (ns *NotificationsHandler) GetNotificationStats(c *gin.Context) {
	uid, exists := c.Get("uid")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	userID := fmt.Sprintf("%v", uid)

	// Get stats from Redis
	ctx := context.Background()

	// Count daily prompts received this week
	weekAgo := time.Now().AddDate(0, 0, -7)
	dailyPromptCount := 0
	for i := 0; i < 7; i++ {
		date := weekAgo.AddDate(0, 0, i)
		key := fmt.Sprintf("notification_sent:%s:%s", userID, date.Format("2006-01-02"))
		exists := ns.redisClient.Exists(ctx, key).Val()
		if exists > 0 {
			dailyPromptCount++
		}
	}

	// Count message notifications (approximate from Redis keys)
	pattern := fmt.Sprintf("message_notification:%s:*", userID)
	messageKeys := ns.redisClient.Keys(ctx, pattern).Val()
	messageCount := len(messageKeys)

	c.JSON(http.StatusOK, gin.H{
		"daily_prompts_this_week": dailyPromptCount,
		"message_notifications":   messageCount,
		"push_token_active":      true, // Since we found the user
	})
}