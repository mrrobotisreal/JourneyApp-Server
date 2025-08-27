package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func (h *UsersHandler) ApproveFriendRequest(c *gin.Context) {
	// Require auth
	uidVal, ok := c.Get("uid")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	authUID, _ := uidVal.(string)

	var req friendshipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	req.UID = strings.TrimSpace(req.UID)
	req.FID = strings.TrimSpace(req.FID)
	if req.UID == "" || req.FID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "uid and fid are required"})
		return
	}

	// Only involved users can approve
	if authUID != req.UID && authUID != req.FID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Not authorized to approve this request"})
		return
	}

	ctx := context.Background()
	res, err := h.postgres.Exec(ctx, `
		UPDATE friendships
		SET status = 'approved'
		WHERE (uid = $1 AND fid = $2) OR (uid = $2 AND fid = $1)
	`, req.UID, req.FID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update friendship"})
		return
	}
	if res.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Friendship not found"})
		return
	}

	// Invalidate caches
	_ = h.redis.Del(ctx, "friends:"+req.UID).Err()
	_ = h.redis.Del(ctx, "friends:"+req.FID).Err()

	c.JSON(http.StatusOK, gin.H{"success": true, "status": "approved"})
}
