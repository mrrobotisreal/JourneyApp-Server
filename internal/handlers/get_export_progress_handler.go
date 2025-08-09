package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ExportProgress returns the status/progress for the provided exportJobId
// Query params: exportJobId (required)
func (h *AuthHandler) ExportProgress(c *gin.Context) {
	uuidCtx, exists := c.Get("uid")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	authUID, ok := uuidCtx.(string)
	if !ok || authUID == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user context"})
		return
	}

	jobID := c.Query("exportJobId")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required query parameter: exportJobId"})
		return
	}

	ctx := context.Background()
	st, err := h.loadExportStatus(ctx, jobID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Export job not found"})
		return
	}
	if st.UID != authUID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot view another user's export job"})
		return
	}

	// Refresh TTL on read so jobs don't expire while being polled
	_ = h.saveExportStatus(ctx, *st)

	// Shape response
	resp := gin.H{
		"exportJobId": st.JobID,
		"status":      st.Status,
		"progress":    st.Progress,
		"startedAt":   st.StartedAt.Format(time.RFC3339),
		"completedAt": nil,
		"totals": gin.H{
			"entries": st.TotalEntries,
			"images":  st.TotalImages,
			"audio":   st.TotalAudio,
		},
	}
	if st.CompletedAt != nil {
		resp["completedAt"] = st.CompletedAt.Format(time.RFC3339)
	}
	if st.Error != "" {
		resp["error"] = st.Error
	}

	c.JSON(http.StatusOK, resp)
}
