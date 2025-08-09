package handlers

import (
	"context"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

// DownloadExportedData sends the completed export zip for the given exportJobId
// Query params: exportJobId (required)
func (h *AuthHandler) DownloadExportedData(c *gin.Context) {
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
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot download another user's export"})
		return
	}
	if st.Status != "completed" || st.ZipPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Export is not ready for download"})
		return
	}

	// Ensure file exists
	if _, err := os.Stat(st.ZipPath); os.IsNotExist(err) {
		c.JSON(http.StatusGone, gin.H{"error": "Export file no longer exists"})
		return
	}

	filename := filepath.Base(st.ZipPath)
	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Header("Content-Type", "application/zip")
	c.File(st.ZipPath)
}
