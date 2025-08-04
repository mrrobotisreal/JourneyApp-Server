package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	accountmodels "io.winapps.journeyapp/internal/models/account"
	updatesettingsmodels "io.winapps.journeyapp/internal/models/update_settings"
)

// UpdateSettings handles updating user settings
func (h *AuthHandler) UpdateSettings(c *gin.Context) {
	var req updatesettingsmodels.UpdateSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	// Get UID from context (set by auth middleware)
	uid, exists := c.Get("uid")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	userUID, ok := uid.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user context"})
		return
	}

	ctx := context.Background()

	// Validate the request fields
	if err := h.validateSettingsRequest(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Update the settings
	updatedSettings, err := h.updateUserSettings(ctx, userUID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update settings: " + err.Error()})
		return
	}

	// Create success response
	response := updatesettingsmodels.UpdateSettingsResponse{
		Success:   true,
		Message:   "Settings updated successfully",
		UID:       updatedSettings.UID,
		ThemeMode: updatedSettings.ThemeMode,
		Theme:     updatedSettings.Theme,
		AppFont:   updatedSettings.AppFont,
		Lang:      updatedSettings.Lang,
		UpdatedAt: updatedSettings.UpdatedAt,
	}

	c.JSON(http.StatusOK, response)
}

// validateSettingsRequest validates the update settings request
func (h *AuthHandler) validateSettingsRequest(req *updatesettingsmodels.UpdateSettingsRequest) error {
	// Validate theme_mode
	if req.ThemeMode != nil {
		validThemeModes := []string{"light", "dark"}
		if !contains(validThemeModes, *req.ThemeMode) {
			return fmt.Errorf("invalid theme_mode: must be one of %v", validThemeModes)
		}
	}

	// Validate theme
	if req.Theme != nil {
		validThemes := []string{"default", "royal", "sunset", "coral", "beach", "rose", "ocean"}
		if !contains(validThemes, *req.Theme) {
			return fmt.Errorf("invalid theme: must be one of %v", validThemes)
		}
	}

	// Validate app_font
	if req.AppFont != nil {
		validFonts := []string{"Montserrat", "Bauhaus", "PlayfairDisplay", "Ubuntu"}
		if !contains(validFonts, *req.AppFont) {
			return fmt.Errorf("invalid app_font: must be one of %v", validFonts)
		}
	}

	// Validate lang
	if req.Lang != nil {
		validLangs := []string{"en", "ar", "de", "es", "fr", "he", "ja", "ko", "pt", "ru", "uk", "vi", "zh"}
		if !contains(validLangs, *req.Lang) {
			return fmt.Errorf("invalid lang: must be one of %v", validLangs)
		}
	}

	return nil
}

// updateUserSettings updates user settings in the database
func (h *AuthHandler) updateUserSettings(ctx context.Context, uid string, req *updatesettingsmodels.UpdateSettingsRequest) (*accountmodels.UserSettings, error) {
	// Build dynamic update query
	setParts := []string{}
	args := []interface{}{}
	argIndex := 1

	if req.ThemeMode != nil {
		setParts = append(setParts, fmt.Sprintf("theme_mode = $%d", argIndex))
		args = append(args, *req.ThemeMode)
		argIndex++
	}

	if req.Theme != nil {
		setParts = append(setParts, fmt.Sprintf("theme = $%d", argIndex))
		args = append(args, *req.Theme)
		argIndex++
	}

	if req.AppFont != nil {
		setParts = append(setParts, fmt.Sprintf("app_font = $%d", argIndex))
		args = append(args, *req.AppFont)
		argIndex++
	}

	if req.Lang != nil {
		setParts = append(setParts, fmt.Sprintf("lang = $%d", argIndex))
		args = append(args, *req.Lang)
		argIndex++
	}

	if len(setParts) == 0 {
		// No fields to update, just return current settings
		return h.getUserSettings(ctx, uid)
	}

	// Add updated_at and uid to query
	setParts = append(setParts, fmt.Sprintf("updated_at = $%d", argIndex))
	args = append(args, time.Now())
	argIndex++

	// Add uid as last parameter
	args = append(args, uid)

	query := fmt.Sprintf(`
		UPDATE user_settings
		SET %s
		WHERE uid = $%d
		RETURNING uid, theme_mode, theme, app_font, lang, created_at, updated_at
	`, strings.Join(setParts, ", "), argIndex)

	var settings accountmodels.UserSettings
	err := h.postgres.QueryRow(ctx, query, args...).Scan(
		&settings.UID,
		&settings.ThemeMode,
		&settings.Theme,
		&settings.AppFont,
		&settings.Lang,
		&settings.CreatedAt,
		&settings.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return &settings, nil
}

// getUserSettings retrieves current user settings
func (h *AuthHandler) getUserSettings(ctx context.Context, uid string) (*accountmodels.UserSettings, error) {
	query := `
		SELECT uid, theme_mode, theme, app_font, lang, created_at, updated_at
		FROM user_settings
		WHERE uid = $1
	`

	var settings accountmodels.UserSettings
	err := h.postgres.QueryRow(ctx, query, uid).Scan(
		&settings.UID,
		&settings.ThemeMode,
		&settings.Theme,
		&settings.AppFont,
		&settings.Lang,
		&settings.CreatedAt,
		&settings.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return &settings, nil
}

// contains checks if a string is in a slice
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}