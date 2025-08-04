package handlers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	deleteaccountmodels "io.winapps.journeyapp/internal/models/delete_account"
)

// DeleteAccount handles the complete deletion of a user account and all associated data
func (h *AuthHandler) DeleteAccount(c *gin.Context) {
	var req deleteaccountmodels.DeleteAccountRequest
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

	// Ensure the user can only delete their own account
	if req.UID != "" && req.UID != userUID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot delete another user's account"})
		return
	}

	ctx := context.Background()

	// Perform the complete account deletion
	err := h.deleteAccountCompletely(ctx, userUID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete account: " + err.Error()})
		return
	}

	response := deleteaccountmodels.DeleteAccountResponse{
		Success: true,
		Message: "Account and all associated data have been successfully deleted",
	}

	c.JSON(http.StatusOK, response)
}

// deleteAccountCompletely performs a comprehensive deletion of all user data
func (h *AuthHandler) deleteAccountCompletely(ctx context.Context, userUID string) error {
	// Start a database transaction for atomicity
	tx, err := h.postgres.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Step 1: Get all entry IDs for this user (we'll need them for related data deletion)
	entryIDs, err := h.getUserEntryIDs(ctx, tx, userUID)
	if err != nil {
		return fmt.Errorf("failed to get user entries: %w", err)
	}

	// Step 2: Delete all related data for each entry
	for _, entryID := range entryIDs {
		if err := h.deleteEntryRelatedData(ctx, tx, entryID); err != nil {
			return fmt.Errorf("failed to delete entry data for %s: %w", entryID, err)
		}
	}

	// Step 3: Delete all entries for this user
	if err := h.deleteUserEntries(ctx, tx, userUID); err != nil {
		return fmt.Errorf("failed to delete user entries: %w", err)
	}

	// Step 4: Delete user settings
	if err := h.deleteUserSettings(ctx, tx, userUID); err != nil {
		return fmt.Errorf("failed to delete user settings: %w", err)
	}

	// Step 5: Delete user record from PostgreSQL
	if err := h.deleteUserRecord(ctx, tx, userUID); err != nil {
		return fmt.Errorf("failed to delete user record: %w", err)
	}

	// Step 6: Delete all physical image files for this user
	if err := h.deleteUserImageFiles(userUID); err != nil {
		// Log but don't fail - file deletion is not critical for data privacy
		fmt.Printf("Warning: failed to delete image files for user %s: %v\n", userUID, err)
	}

	// Step 7: Delete all physical audio files for this user
	if err := h.deleteUserAudioFiles(userUID); err != nil {
		// Log but don't fail - file deletion is not critical for data privacy
		fmt.Printf("Warning: failed to delete audio files for user %s: %v\n", userUID, err)
	}

	// Step 8: Clear Redis cache for this user
	if err := h.clearUserRedisCache(ctx, userUID, entryIDs); err != nil {
		// Log but don't fail - Redis cache clearing is not critical
		fmt.Printf("Warning: failed to clear Redis cache for user %s: %v\n", userUID, err)
	}

	// Step 9: Delete Firebase user
	if err := h.deleteFirebaseUser(ctx, userUID); err != nil {
		return fmt.Errorf("failed to delete Firebase user: %w", err)
	}

	// Commit the transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// getUserEntryIDs retrieves all entry IDs for a user
func (h *AuthHandler) getUserEntryIDs(ctx context.Context, tx pgx.Tx, userUID string) ([]string, error) {
	query := `SELECT id FROM entries WHERE user_uid = $1`
	rows, err := tx.Query(ctx, query, userUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entryIDs []string
	for rows.Next() {
		var entryID string
		if err := rows.Scan(&entryID); err != nil {
			return nil, err
		}
		entryIDs = append(entryIDs, entryID)
	}

	return entryIDs, nil
}

// deleteEntryRelatedData deletes all data related to a specific entry
func (h *AuthHandler) deleteEntryRelatedData(ctx context.Context, tx pgx.Tx, entryID string) error {
	// Delete images
	if _, err := tx.Exec(ctx, `DELETE FROM images WHERE entry_id = $1`, entryID); err != nil {
		return fmt.Errorf("failed to delete images: %w", err)
	}

	// Delete audio
	if _, err := tx.Exec(ctx, `DELETE FROM audio WHERE entry_id = $1`, entryID); err != nil {
		return fmt.Errorf("failed to delete audio: %w", err)
	}

	// Delete tags
	if _, err := tx.Exec(ctx, `DELETE FROM tags WHERE entry_id = $1`, entryID); err != nil {
		return fmt.Errorf("failed to delete tags: %w", err)
	}

	// Delete locations
	if _, err := tx.Exec(ctx, `DELETE FROM locations WHERE entry_id = $1`, entryID); err != nil {
		return fmt.Errorf("failed to delete locations: %w", err)
	}

	return nil
}

// deleteUserEntries deletes all entries for a user
func (h *AuthHandler) deleteUserEntries(ctx context.Context, tx pgx.Tx, userUID string) error {
	query := `DELETE FROM entries WHERE user_uid = $1`
	_, err := tx.Exec(ctx, query, userUID)
	if err != nil {
		return fmt.Errorf("failed to delete entries: %w", err)
	}
	return nil
}

// deleteUserRecord deletes the user record from the users table (if it exists)
func (h *AuthHandler) deleteUserRecord(ctx context.Context, tx pgx.Tx, userUID string) error {
	query := `DELETE FROM users WHERE uid = $1`
	_, err := tx.Exec(ctx, query, userUID)
	if err != nil {
		return fmt.Errorf("failed to delete user record: %w", err)
	}
	return nil
}

// clearUserRedisCache clears all Redis cache entries for the user
func (h *AuthHandler) clearUserRedisCache(ctx context.Context, userUID string, entryIDs []string) error {
	// Clear entry caches
	for _, entryID := range entryIDs {
		entryKey := fmt.Sprintf("entry:%s", entryID)
		if err := h.redis.Del(ctx, entryKey).Err(); err != nil {
			// Log but continue - cache clearing is not critical
			fmt.Printf("Warning: failed to clear cache for entry %s: %v\n", entryID, err)
		}
	}

	// Clear any user-specific caches (if they exist)
	userKey := fmt.Sprintf("user:%s", userUID)
	if err := h.redis.Del(ctx, userKey).Err(); err != nil {
		// Log but continue
		fmt.Printf("Warning: failed to clear cache for user %s: %v\n", userUID, err)
	}

	return nil
}

// deleteFirebaseUser deletes the user from Firebase Authentication
func (h *AuthHandler) deleteFirebaseUser(ctx context.Context, userUID string) error {
	authClient, err := h.firebaseApp.Auth(ctx)
	if err != nil {
		return fmt.Errorf("failed to get Firebase auth client: %w", err)
	}

	err = authClient.DeleteUser(ctx, userUID)
	if err != nil {
		return fmt.Errorf("failed to delete Firebase user: %w", err)
	}

	return nil
}

// deleteUserImageFiles deletes all physical image files for a user
func (h *AuthHandler) deleteUserImageFiles(userUID string) error {
	userImageDir := filepath.Join("internal", "images", userUID)

	// Check if user image directory exists
	if _, err := os.Stat(userImageDir); os.IsNotExist(err) {
		// Directory doesn't exist, nothing to delete
		return nil
	}

	// Remove the entire user directory and all its contents
	if err := os.RemoveAll(userImageDir); err != nil {
		return fmt.Errorf("failed to delete user image directory %s: %w", userImageDir, err)
	}

	return nil
}

// deleteUserAudioFiles deletes all physical audio files for a user
func (h *AuthHandler) deleteUserAudioFiles(userUID string) error {
	userAudioDir := filepath.Join("internal", "audio", userUID)

	// Check if user audio directory exists
	if _, err := os.Stat(userAudioDir); os.IsNotExist(err) {
		// Directory doesn't exist, nothing to delete
		return nil
	}

	// Remove the entire user directory and all its contents
	if err := os.RemoveAll(userAudioDir); err != nil {
		return fmt.Errorf("failed to delete user audio directory %s: %w", userAudioDir, err)
	}

	return nil
}

// deleteUserSettings deletes user settings from the user_settings table
func (h *AuthHandler) deleteUserSettings(ctx context.Context, tx pgx.Tx, userUID string) error {
	query := `DELETE FROM user_settings WHERE uid = $1`
	_, err := tx.Exec(ctx, query, userUID)
	if err != nil {
		return fmt.Errorf("failed to delete user settings: %w", err)
	}
	return nil
}