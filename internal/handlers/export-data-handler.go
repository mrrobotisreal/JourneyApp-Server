package handlers

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	exportmodels "io.winapps.journeyapp/internal/models/export_data"
)

// ExportJobStatus represents the progress and state of an export job
// Stored in Redis as JSON under key: export_job:<jobID>
// TTL is applied at creation and refreshed on updates
// Note: Progress is a whole number percentage [0..100]
type ExportJobStatus struct {
	JobID             string    `json:"jobId"`
	UID               string    `json:"uid"`
	Status            string    `json:"status"` // pending, running, completed, failed
	Progress          int       `json:"progress"`
	StartedAt         time.Time `json:"startedAt"`
	CompletedAt       *time.Time `json:"completedAt,omitempty"`
	TotalEntries      int       `json:"totalEntries"`
	TotalImages       int       `json:"totalImages"`
	TotalAudio        int       `json:"totalAudio"`
	ProcessedEntries  int       `json:"processedEntries"`
	ProcessedImages   int       `json:"processedImages"`
	ProcessedAudio    int       `json:"processedAudio"`
	ZipPath           string    `json:"zipPath"`
	Error             string    `json:"error,omitempty"`
}

const exportJobRedisKeyPrefix = "export_job:"
const exportJobTTL = 24 * time.Hour

// ExportData starts an asynchronous export job for the authenticated user.
// Expects JSON body with { uid: string }. The uid must match the authenticated user.
func (h *AuthHandler) ExportData(c *gin.Context) {
	var req exportmodels.ExportDataRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	uidCtx, exists := c.Get("uid")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	authenticatedUID, ok := uidCtx.(string)
	if !ok || authenticatedUID == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user context"})
		return
	}
	if req.UID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "uid is required"})
		return
	}
	if req.UID != authenticatedUID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot export another user's data"})
		return
	}

	jobID := uuid.New().String()
	status := ExportJobStatus{
		JobID:       jobID,
		UID:         authenticatedUID,
		Status:      "pending",
		Progress:    0,
		StartedAt:   time.Now(),
		ZipPath:     "",
	}

	ctx := context.Background()
	if err := h.saveExportStatus(ctx, status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize export job"})
		return
	}

	// Launch the export in background
	go h.runExportJob(jobID, authenticatedUID)

	resp := exportmodels.ExportDataResponse{ExportJobID: jobID, Message: "Export started"}
	c.JSON(http.StatusAccepted, resp)
}

func (h *AuthHandler) saveExportStatus(ctx context.Context, status ExportJobStatus) error {
	key := exportJobRedisKeyPrefix + status.JobID
	data, err := json.Marshal(status)
	if err != nil {
		return err
	}
	return h.redis.Set(ctx, key, data, exportJobTTL).Err()
}

func (h *AuthHandler) loadExportStatus(ctx context.Context, jobID string) (*ExportJobStatus, error) {
	key := exportJobRedisKeyPrefix + jobID
	val, err := h.redis.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	var st ExportJobStatus
	if err := json.Unmarshal([]byte(val), &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (h *AuthHandler) updateProgress(ctx context.Context, st *ExportJobStatus) {
	// Ensure TTL is refreshed as we update
	_ = h.saveExportStatus(ctx, *st)
}

// runExportJob performs the export work and updates progress in Redis
func (h *AuthHandler) runExportJob(jobID, uid string) {
	ctx := context.Background()
	// Load current status
	st, err := h.loadExportStatus(ctx, jobID)
	if err != nil {
		return
	}
	st.Status = "running"
	h.updateProgress(ctx, st)

	defer func() {
		// Final persistence on exit
		h.updateProgress(ctx, st)
	}()

	// Prepare directories
	userRoot := filepath.Join("internal", "exports", uid)
	jobRoot := filepath.Join(userRoot, jobID)
	entriesDir := filepath.Join(jobRoot, "entries")
	if err := os.MkdirAll(entriesDir, 0755); err != nil {
		st.Status = "failed"
		st.Error = fmt.Sprintf("failed to create export directories: %v", err)
		return
	}

	// Compute totals for progress
	var totalEntries, totalImages, totalAudio int
	if err := h.postgres.QueryRow(ctx, `SELECT COUNT(*) FROM entries WHERE user_uid = $1`, uid).Scan(&totalEntries); err != nil {
		st.Status = "failed"
		st.Error = fmt.Sprintf("failed to count entries: %v", err)
		return
	}
	// Images total
	if err := h.postgres.QueryRow(ctx, `SELECT COUNT(*) FROM images i WHERE i.entry_id IN (SELECT id FROM entries e WHERE e.user_uid = $1)`, uid).Scan(&totalImages); err != nil {
		st.Status = "failed"
		st.Error = fmt.Sprintf("failed to count images: %v", err)
		return
	}
	// Audio total
	if err := h.postgres.QueryRow(ctx, `SELECT COUNT(*) FROM audio a WHERE a.entry_id IN (SELECT id FROM entries e WHERE e.user_uid = $1)`, uid).Scan(&totalAudio); err != nil {
		st.Status = "failed"
		st.Error = fmt.Sprintf("failed to count audio: %v", err)
		return
	}

	st.TotalEntries = totalEntries
	st.TotalImages = totalImages
	st.TotalAudio = totalAudio
	h.updateProgress(ctx, st)

	// Create CSV file
	csvPath := filepath.Join(entriesDir, "entries.csv")
	csvFile, err := os.Create(csvPath)
	if err != nil {
		st.Status = "failed"
		st.Error = fmt.Sprintf("failed to create CSV file: %v", err)
		return
	}
	defer csvFile.Close()
	csvWriter := csv.NewWriter(csvFile)
	defer csvWriter.Flush()

	// Header
	_ = csvWriter.Write([]string{"id", "title", "description", "locations", "tags", "createdAt", "updatedAt"})

	// Iterate entries
	rows, err := h.postgres.Query(ctx, `SELECT id, title, description, created_at, updated_at FROM entries WHERE user_uid = $1 ORDER BY created_at`, uid)
	if err != nil {
		st.Status = "failed"
		st.Error = fmt.Sprintf("failed to fetch entries: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var entryID, title, description string
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&entryID, &title, &description, &createdAt, &updatedAt); err != nil {
			st.Status = "failed"
			st.Error = fmt.Sprintf("failed to scan entry: %v", err)
			return
		}

		// Fetch tags
		tagsJSON, err := h.fetchTagsJSON(ctx, entryID)
		if err != nil {
			st.Status = "failed"
			st.Error = fmt.Sprintf("failed to fetch tags: %v", err)
			return
		}
		// Fetch locations
		locationsJSON, err := h.fetchLocationsJSON(ctx, entryID)
		if err != nil {
			st.Status = "failed"
			st.Error = fmt.Sprintf("failed to fetch locations: %v", err)
			return
		}

		// Write CSV row
		_ = csvWriter.Write([]string{
			entryID,
			title,
			description,
			locationsJSON,
			tagsJSON,
			createdAt.Format(time.RFC3339),
			updatedAt.Format(time.RFC3339),
		})
		csvWriter.Flush()
		st.ProcessedEntries++
		h.recalculateAndPersistProgress(ctx, st)

		// Prepare media directories
		entryDir := filepath.Join(entriesDir, entryID)
		imagesDir := filepath.Join(entryDir, "images")
		audioDir := filepath.Join(entryDir, "audio")
		_ = os.MkdirAll(imagesDir, 0755)
		_ = os.MkdirAll(audioDir, 0755)

		// Copy images
		imgRows, err := h.postgres.Query(ctx, `SELECT url FROM images WHERE entry_id = $1 ORDER BY upload_order`, entryID)
		if err != nil {
			st.Status = "failed"
			st.Error = fmt.Sprintf("failed to fetch images: %v", err)
			return
		}
		for imgRows.Next() {
			var imageURL string
			if err := imgRows.Scan(&imageURL); err != nil {
				imgRows.Close()
				st.Status = "failed"
				st.Error = fmt.Sprintf("failed to scan image: %v", err)
				return
			}
			if err := copyMediaFromURL(imageURL, filepath.Join(imagesDir, filepath.Base(imageURL))); err != nil {
				// Log and continue; don't fail the entire job for a missing file
				fmt.Printf("warning: failed to copy image %s: %v\n", imageURL, err)
			}
			st.ProcessedImages++
			h.recalculateAndPersistProgress(ctx, st)
		}
		imgRows.Close()

		// Copy audio
		audRows, err := h.postgres.Query(ctx, `SELECT url FROM audio WHERE entry_id = $1 ORDER BY upload_order`, entryID)
		if err != nil {
			st.Status = "failed"
			st.Error = fmt.Sprintf("failed to fetch audio: %v", err)
			return
		}
		for audRows.Next() {
			var audioURL string
			if err := audRows.Scan(&audioURL); err != nil {
				audRows.Close()
				st.Status = "failed"
				st.Error = fmt.Sprintf("failed to scan audio: %v", err)
				return
			}
			if err := copyMediaFromURL(audioURL, filepath.Join(audioDir, filepath.Base(audioURL))); err != nil {
				// Log and continue; don't fail the entire job for a missing file
				fmt.Printf("warning: failed to copy audio %s: %v\n", audioURL, err)
			}
			st.ProcessedAudio++
			h.recalculateAndPersistProgress(ctx, st)
		}
		audRows.Close()
	}

	// Zip the job directory
	zipPath := filepath.Join(userRoot, fmt.Sprintf("%s.zip", jobID))
	if err := zipDirectory(jobRoot, zipPath); err != nil {
		st.Status = "failed"
		st.Error = fmt.Sprintf("failed to create zip: %v", err)
		return
	}
	st.ZipPath = zipPath
	completed := time.Now()
	st.CompletedAt = &completed
	st.Status = "completed"
	st.Progress = 100
	h.updateProgress(ctx, st)
}

func (h *AuthHandler) recalculateAndPersistProgress(ctx context.Context, st *ExportJobStatus) {
	total := st.TotalEntries + st.TotalImages + st.TotalAudio
	processed := st.ProcessedEntries + st.ProcessedImages + st.ProcessedAudio
	if total <= 0 {
		st.Progress = 100
	} else {
		pct := int(float64(processed) / float64(total) * 100.0)
		if pct > 100 {
			pct = 100
		}
		st.Progress = pct
	}
	h.updateProgress(ctx, st)
}

func (h *AuthHandler) fetchTagsJSON(ctx context.Context, entryID string) (string, error) {
	type tag struct {
		Key   string `json:"key"`
		Value string `json:"value,omitempty"`
	}
	rows, err := h.postgres.Query(ctx, `SELECT key, value FROM tags WHERE entry_id = $1 ORDER BY created_at`, entryID)
	if err != nil {
		return "[]", nil
	}
	defer rows.Close()
	var tags []tag
	for rows.Next() {
		var t tag
		if err := rows.Scan(&t.Key, &t.Value); err == nil {
			tags = append(tags, t)
		}
	}
	b, _ := json.Marshal(tags)
	return string(b), nil
}

func (h *AuthHandler) fetchLocationsJSON(ctx context.Context, entryID string) (string, error) {
	type loc struct {
		Latitude    float64 `json:"latitude,omitempty"`
		Longitude   float64 `json:"longitude,omitempty"`
		Address     string  `json:"address,omitempty"`
		City        string  `json:"city,omitempty"`
		State       string  `json:"state,omitempty"`
		Zip         string  `json:"zip,omitempty"`
		Country     string  `json:"country,omitempty"`
		CountryCode string  `json:"countryCode,omitempty"`
		DisplayName string  `json:"displayName,omitempty"`
	}
	rows, err := h.postgres.Query(ctx, `SELECT latitude, longitude, address, city, state, zip, country, country_code, display_name FROM locations WHERE entry_id = $1 ORDER BY created_at`, entryID)
	if err != nil {
		return "[]", nil
	}
	defer rows.Close()
	var locs []loc
	for rows.Next() {
		var l loc
		if err := rows.Scan(&l.Latitude, &l.Longitude, &l.Address, &l.City, &l.State, &l.Zip, &l.Country, &l.CountryCode, &l.DisplayName); err == nil {
			locs = append(locs, l)
		}
	}
	b, _ := json.Marshal(locs)
	return string(b), nil
}

// copyMediaFromURL takes a URL like "/images/<uid>/<entryID>/<filename>" or "/audio/..." and copies
// the file into destPath. The destination directory must already exist.
func copyMediaFromURL(urlPath, destPath string) error {
	var srcPath string
	if strings.HasPrefix(urlPath, "/images/") {
		rel := strings.TrimPrefix(urlPath, "/images/")
		srcPath = filepath.Join("internal", "images", rel)
	} else if strings.HasPrefix(urlPath, "/audio/") {
		rel := strings.TrimPrefix(urlPath, "/audio/")
		srcPath = filepath.Join("internal", "audio", rel)
	} else {
		return fmt.Errorf("unsupported media URL: %s", urlPath)
	}
	// Open source
	s, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer s.Close()
	// Create destination file
	d, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer d.Close()
	_, err = io.Copy(d, s)
	return err
}

// zipDirectory zips the entire contents of srcDir into destZipPath
func zipDirectory(srcDir, destZipPath string) error {
	zipFile, err := os.Create(destZipPath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	archive := zip.NewWriter(zipFile)
	defer archive.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		w, err := archive.Create(relPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, f); err != nil {
			return err
		}
		return nil
	})
}
