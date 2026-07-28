package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/salim77088/os/api/internal/config"
	"github.com/salim77088/os/api/internal/database"
	"github.com/salim77088/os/api/internal/models"
	"github.com/salim77088/os/api/internal/store"
	"github.com/salim77088/os/api/internal/transcode"
)

// Server represents the HTTP API server
type Server struct {
	httpServer *http.Server
	router     *chi.Mux
	cfg        *config.Config
	db         *database.DB
	store      *store.FileStore
	engine     *transcode.Engine
	version    string
	startTime  time.Time
}

// New creates and configures a new API server, returns the Server struct
func New(cfg *config.Config, db *database.DB, fileStore *store.FileStore,
	engine *transcode.Engine, version string) *Server {

	srv := &Server{
		cfg:       cfg,
		db:        db,
		store:     fileStore,
		engine:    engine,
		version:   version,
		startTime: time.Now(),
	}

	// Create router
	r := chi.NewRouter()

	// Middleware
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(120 * time.Second))

	// CORS middleware
	if cfg.Server.EnableCORS {
		r.Use(corsMiddleware)
	}

	// Authentication middleware
	if cfg.Auth.Enabled {
		r.Use(authMiddleware(cfg.Auth))
	}

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", srv.handleHealth)
		r.Get("/system", srv.handleSystemInfo)
		r.Post("/upload", srv.handleUpload)
		r.Get("/videos", srv.handleListVideos)
		r.Route("/videos/{videoID}", func(r chi.Router) {
			r.Get("/", srv.handleGetVideo)
			r.Get("/stream", srv.handleGetStreamURLs)
			r.Get("/status", srv.handleGetTranscodeStatus)
			r.Delete("/", srv.handleDeleteVideo)
		})
	})

	// Static file serving for HLS/DASH streams
	r.Handle("/stream/*", http.StripPrefix("/stream/",
		http.FileServer(http.Dir(cfg.Storage.BasePath))))

	// Create HTTP server
	srv.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      r,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
	}

	srv.router = r
	return srv
}

// ListenAndServe starts the HTTP server
func (s *Server) ListenAndServe() error {
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// handleUpload handles video file upload
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	// Limit upload size
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.Server.MaxUploadSize)

	err := r.ParseMultipartForm(s.cfg.Server.MaxUploadSize)
	if err != nil {
		sendError(w, http.StatusBadRequest, fmt.Sprintf("Failed to parse upload: %v", err))
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		sendError(w, http.StatusBadRequest, "Missing 'video' field in form data")
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if !isValidVideoType(contentType) {
		sendError(w, http.StatusBadRequest, fmt.Sprintf("Invalid file type: %s", contentType))
		return
	}

	videoID := uuid.New().String()

	log.Printf("Uploading video: id=%s, name=%s, size=%d", videoID, header.Filename, header.Size)

	filePath, fileSize, err := s.store.SaveUploadedFile(videoID, header.Filename, contentType, file)
	if err != nil {
		sendError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save file: %v", err))
		return
	}

	video := &models.Video{
		ID:           videoID,
		OriginalName: header.Filename,
		OriginalPath: filePath,
		OriginalSize: fileSize,
		MimeType:     contentType,
		Status:       models.StatusUploaded,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.db.InsertVideo(video); err != nil {
		s.store.DeleteVideoFiles(videoID)
		sendError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create video record: %v", err))
		return
	}

	// Enqueue for transcoding
	if err := s.engine.Enqueue(videoID, filePath); err != nil {
		log.Printf("Warning: failed to enqueue video: %v", err)
	}

	response := map[string]interface{}{
		"id":            videoID,
		"original_name": header.Filename,
		"original_size": fileSize,
		"mime_type":     contentType,
		"status":        models.StatusUploaded,
		"message":       "Video uploaded successfully. Transcoding will begin automatically.",
		"created_at":    video.CreatedAt,
	}

	sendJSON(w, http.StatusCreated, response)
}

// handleListVideos handles listing all videos
func (s *Server) handleListVideos(w http.ResponseWriter, r *http.Request) {
	page := getIntParam(r, "page", 1)
	perPage := getIntParam(r, "per_page", 50)
	if page < 1 { page = 1 }
	if perPage < 1 || perPage > 100 { perPage = 50 }

	videos, total, err := s.db.ListVideos(page, perPage)
	if err != nil {
		sendError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list videos: %v", err))
		return
	}

	summaries := []models.VideoSummary{}
	for _, v := range videos {
		streamURL := ""
		if v.Status == models.StatusReady {
			streamURL = fmt.Sprintf("/stream/hls/%s/master.m3u8", v.ID)
		}
		summaries = append(summaries, models.VideoSummary{
			ID:           v.ID,
			OriginalName: v.OriginalName,
			OriginalSize: v.OriginalSize,
			Duration:     v.Duration,
			Width:        v.Width,
			Height:       v.Height,
			Status:       v.Status,
			CreatedAt:    v.CreatedAt,
			StreamURL:    streamURL,
		})
	}

	sendJSON(w, http.StatusOK, models.VideoListResponse{
		Videos:  summaries,
		Total:   total,
		Page:    page,
		PerPage: perPage,
	})
}

// handleGetVideo handles getting video details
func (s *Server) handleGetVideo(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoID")
	video, err := s.db.GetVideo(videoID)
	if err != nil {
		sendError(w, http.StatusNotFound, fmt.Sprintf("Video not found: %s", videoID))
		return
	}
	sendJSON(w, http.StatusOK, video)
}

// handleGetStreamURLs handles getting streaming URLs
func (s *Server) handleGetStreamURLs(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoID")
	video, err := s.db.GetVideo(videoID)
	if err != nil {
		sendError(w, http.StatusNotFound, fmt.Sprintf("Video not found: %s", videoID))
		return
	}

	if video.Status != models.StatusReady {
		sendError(w, http.StatusConflict, fmt.Sprintf("Video not ready. Status: %s", video.Status))
		return
	}

	streamURLs := models.StreamURLs{
		HLS:         fmt.Sprintf("/stream/hls/%s/master.m3u8", videoID),
		DASH:        fmt.Sprintf("/stream/dash/%s/manifest.mpd", videoID),
		Progressive: fmt.Sprintf("/stream/videos/%s/%s", videoID, filepath.Base(video.OriginalPath)),
		Resolutions: []models.StreamResolution{},
	}

	for _, res := range s.cfg.Transcode.ResolutionLadder {
		if res.Height <= video.Height {
			streamURLs.Resolutions = append(streamURLs.Resolutions, models.StreamResolution{
				Name:    res.Name,
				Width:   res.Width,
				Height:  res.Height,
				HLSURL:  fmt.Sprintf("/stream/hls/%s/%s/playlist.m3u8", videoID, res.Name),
				DASHURL: fmt.Sprintf("/stream/dash/%s/manifest.mpd", videoID),
				Bitrate: res.Bitrate,
			})
		}
	}

	sendJSON(w, http.StatusOK, streamURLs)
}

// handleGetTranscodeStatus handles getting transcoding status
func (s *Server) handleGetTranscodeStatus(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoID")
	video, err := s.db.GetVideo(videoID)
	if err != nil {
		sendError(w, http.StatusNotFound, fmt.Sprintf("Video not found: %s", videoID))
		return
	}

	status := &models.TranscodeStatus{
		VideoID:     videoID,
		Status:      video.Status,
		CurrentStep: video.TranscodeLog,
	}

	if video.Status == models.StatusReady {
		status.Progress = 100
		if video.TranscodedAt != nil {
			status.CompletedAt = video.TranscodedAt
		}
	} else if video.Status == models.StatusProcessing {
		status.Progress = 50
	}

	sendJSON(w, http.StatusOK, status)
}

// handleDeleteVideo handles deleting a video
func (s *Server) handleDeleteVideo(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoID")
	s.store.DeleteVideoFiles(videoID)
	if err := s.db.DeleteVideo(videoID); err != nil {
		sendError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to delete video: %v", err))
		return
	}
	sendJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("Video %s deleted", videoID)})
}

// handleHealth handles the health check endpoint
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(s.startTime).String()
	videoCount, _ := s.db.GetVideoCount()
	dbSize, _ := s.db.GetDBSize()
	totalFiles, _, _ := s.store.GetStorageStats()

	response := models.HealthResponse{
		Status:  "healthy",
		Version: s.version,
		Uptime:  uptime,
		Database: models.DatabaseHealth{
			Status: "healthy",
			Path:   s.db.Path(),
			SizeKB: dbSize,
			Videos: videoCount,
		},
		Storage: models.StorageHealth{
			Status:      "healthy",
			BasePath:    s.cfg.Storage.BasePath,
			VideosCount: totalFiles,
		},
		Transcoder: models.TranscoderHealth{
			Status:        "healthy",
			GPUAvailable:  s.engine.IsGPUAvailable(),
			GPUType:       s.engine.GetGPUType(),
			Workers:       s.cfg.Transcode.Workers,
			ActiveJobs:    s.engine.GetActiveJobs(),
			FFmpegVersion: s.engine.GetFFmpegVersion(),
		},
	}

	sendJSON(w, http.StatusOK, response)
}

// handleSystemInfo handles the system information endpoint
func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	info := models.SystemInfo{}

	if output, err := exec.Command("uname", "-a").Output(); err == nil {
		parts := strings.Fields(string(output))
		if len(parts) >= 3 {
			info.OS = parts[0]
			info.Kernel = parts[2]
			info.Arch = parts[len(parts)-2]
		}
	}

	info.CPUCount = 1 // Placeholder - runtime.NumCPU() requires import

	if memInfo, err := readMemInfo(); err == nil {
		info.MemoryMB = memInfo.TotalMB
		info.MemoryFreeMB = memInfo.FreeMB
	}

	_, used, _, _ := s.store.GetDiskUsage()
	info.DiskTotalGB = float64(used) / (1024 * 1024 * 1024)

	if loadAvg, err := readLoadAvg(); err == nil {
		info.LoadAvg = loadAvg
	}

	if s.engine.IsGPUAvailable() {
		info.GPUInfo = []models.GPUDevice{
			{Name: fmt.Sprintf("GPU (%s)", s.engine.GetGPUType()),
				Type: s.engine.GetGPUType(), Available: true},
		}
	}

	info.UptimeSec = time.Since(s.startTime).Seconds()
	sendJSON(w, http.StatusOK, info)
}

// Middleware

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func authMiddleware(authCfg config.AuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-Key")
			authHeader := r.Header.Get("Authorization")

			if apiKey != "" {
				for _, k := range authCfg.APIKeys {
					if apiKey == k {
						next.ServeHTTP(w, r)
						return
					}
				}
			}
			if strings.HasPrefix(authHeader, "Bearer ") {
				token := strings.TrimPrefix(authHeader, "Bearer ")
				for _, k := range authCfg.APIKeys {
					if token == k { next.ServeHTTP(w, r); return }
				}
				if token == authCfg.AdminKey { next.ServeHTTP(w, r); return }
			}
			sendError(w, http.StatusUnauthorized, "Invalid or missing API key")
		})
	}
}

// Helpers

func sendJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

func sendError(w http.ResponseWriter, code int, msg string) {
	sendJSON(w, code, map[string]string{"error": msg, "code": strconv.Itoa(code)})
}

func getIntParam(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" { return def }
	i, err := strconv.Atoi(v)
	if err != nil { return def }
	return i
}

func isValidVideoType(ct string) bool {
	valid := []string{"video/mp4", "video/webm", "video/x-matroska", "video/quicktime",
		"video/x-flv", "video/avi", "video/x-ms-wmv", "application/octet-stream"}
	for _, t := range valid {
		if ct == t { return true }
	}
	return false
}

type MemInfo struct { TotalMB, FreeMB float64 }

func readMemInfo() (*MemInfo, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil { return nil, err }
	info := &MemInfo{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 { continue }
		val, _ := strconv.ParseFloat(fields[1], 64)
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			info.TotalMB = val / 1024
		case strings.HasPrefix(line, "MemAvailable:"):
			info.FreeMB = val / 1024
		}
	}
	return info, nil
}

func readLoadAvg() (string, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil { return "", err }
	f := strings.Fields(string(data))
	if len(f) >= 3 { return fmt.Sprintf("%s %s %s", f[0], f[1], f[2]), nil }
	return string(data), nil
}
