package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type friendshipRequest struct {
	UID string `json:"uid"`
	FID string `json:"fid"`
}

func (h *UsersHandler) AddFriendship(c *gin.Context) {
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
	if req.UID != authUID {
		c.JSON(http.StatusForbidden, gin.H{"error": "uid must match authenticated user"})
		return
	}
	if req.UID == req.FID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot friend yourself"})
		return
	}

	ctx := context.Background()

	// Check existing friendship in either order
	var exists bool
	if err := h.postgres.QueryRow(ctx, `
		SELECT TRUE FROM friendships WHERE (uid = $1 AND fid = $2) OR (uid = $2 AND fid = $1)
	`, req.UID, req.FID).Scan(&exists); err == nil && exists {
		c.JSON(http.StatusConflict, gin.H{"error": "Friendship already exists"})
		return
	}

	// Insert pending friendship in canonical ordering (uid, fid)
	// We'll store as provided order; unique symmetric index prevents duplicates in other order
	_, err := h.postgres.Exec(ctx, `
		INSERT INTO friendships (uid, fid, status, created_at)
		VALUES ($1, $2, 'pending', NOW())
		ON CONFLICT (uid, fid) DO NOTHING
	`, req.UID, req.FID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create friendship"})
		return
	}

	// Invalidate caches
	_ = h.redis.Del(ctx, "friends:"+req.UID).Err()
	_ = h.redis.Del(ctx, "friends:"+req.FID).Err()

	c.JSON(http.StatusOK, gin.H{"success": true, "status": "pending"})
}