package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	validatedisplaymodels "io.winapps.journeyapp/internal/models/validate_display_name"
)

// ValidateDisplayName checks if a provided displayName (username) is available
func (h *AuthHandler) ValidateDisplayName(c *gin.Context) {
	// Basic origin check to mitigate abuse from untrusted origins
	origin := c.GetHeader("Origin")
	if origin != "https://app.lifethread.me" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden"})
		return
	}

	var req validatedisplaymodels.ValidateDisplayNameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		c.JSON(http.StatusOK, validatedisplaymodels.ValidateDisplayNameResponse{IsValid: false})
		return
	}

	ctx := context.Background()

	// Case-insensitive existence check
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM users WHERE LOWER(display_name) = LOWER($1))`
	if err := h.postgres.QueryRow(ctx, query, displayName).Scan(&exists); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to validate display name"})
		return
	}

	c.JSON(http.StatusOK, validatedisplaymodels.ValidateDisplayNameResponse{IsValid: !exists})
}