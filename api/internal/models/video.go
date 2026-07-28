package models

import (
        "time"
)

// Video represents a stored video in the system
type Video struct {
        ID             string    `json:"id"`
        OriginalName   string    `json:"original_name"`
        OriginalPath   string    `json:"original_path"`
        OriginalSize   int64     `json:"original_size"`
        MimeType       string    `json:"mime_type"`
        Duration       float64   `json:"duration"`
        Width          int       `json:"width"`
        Height         int       `json:"height"`
        Codec          string    `json:"codec"`
        Bitrate        int64     `json:"bitrate"`
        FPS            float64   `json:"fps"`
        Status         string    `json:"status"` // "uploaded", "processing", "ready", "failed"
        TranscodeLog   string    `json:"transcode_log"`
        TranscodedAt   *time.Time `json:"transcoded_at"`
        HLSPath        string    `json:"hls_path"`
        DASHPath       string    `json:"dash_path"`
        CreatedAt      time.Time `json:"created_at"`
        UpdatedAt      time.Time `json:"updated_at"`
}

// VideoUpload represents a video upload request
type VideoUpload struct {
        FileName    string `json:"file_name"`
        FileSize    int64  `json:"file_size"`
        MimeType    string `json:"mime_type"`
}

// VideoListResponse represents the response for listing videos
type VideoListResponse struct {
        Videos  []VideoSummary `json:"videos"`
        Total   int            `json:"total"`
        Page    int            `json:"page"`
        PerPage  int            `json:"per_page"`
}

// VideoSummary is a compact video representation for listing
type VideoSummary struct {
        ID           string    `json:"id"`
        OriginalName string    `json:"original_name"`
        OriginalSize int64     `json:"original_size"`
        Duration     float64   `json:"duration"`
        Width        int       `json:"width"`
        Height       int       `json:"height"`
        Status       string    `json:"status"`
        CreatedAt    time.Time `json:"created_at"`
        StreamURL    string    `json:"stream_url"`
}

// StreamURLs represents streaming URLs for a video
type StreamURLs struct {
        HLS       string            `json:"hls"`
        DASH      string            `json:"dash"`
        Progressive string          `json:"progressive"`
        Resolutions []StreamResolution `json:"resolutions"`
}

// StreamResolution represents a specific resolution stream
type StreamResolution struct {
        Name      string `json:"name"`
        Width     int    `json:"width"`
        Height    int    `json:"height"`
        HLSURL    string `json:"hls_url"`
        DASHURL   string `json:"dash_url"`
        Bitrate   string `json:"bitrate"`
}

// TranscodeStatus represents the transcoding status of a video
type TranscodeStatus struct {
        VideoID     string    `json:"video_id"`
        Status      string    `json:"status"`
        Progress    float64   `json:"progress"`
        CurrentStep string    `json:"current_step"`
        Error       string    `json:"error,omitempty"`
        StartedAt   *time.Time `json:"started_at"`
        CompletedAt *time.Time `json:"completed_at,omitempty"`
        ETA         string    `json:"eta,omitempty"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
        Status       string            `json:"status"`
        Version      string            `json:"version"`
        Uptime       string            `json:"uptime"`
        Database     DatabaseHealth    `json:"database"`
        Storage      StorageHealth     `json:"storage"`
        Transcoder   TranscoderHealth  `json:"transcoder"`
}

// DatabaseHealth represents database health information
type DatabaseHealth struct {
        Status  string `json:"status"`
        Path    string `json:"path"`
        SizeKB  int64  `json:"size_kb"`
        Videos  int    `json:"videos_count"`
}

// StorageHealth represents storage health information
type StorageHealth struct {
        Status      string `json:"status"`
        BasePath    string `json:"base_path"`
        TotalGB     float64 `json:"total_gb"`
        UsedGB      float64 `json:"used_gb"`
        FreeGB      float64 `json:"free_gb"`
        VideosCount int    `json:"videos_count"`
}

// TranscoderHealth represents transcoder health information
type TranscoderHealth struct {
        Status       string `json:"status"`
        GPUAvailable bool   `json:"gpu_available"`
        GPUType      string `json:"gpu_type"`
        Workers      int    `json:"workers"`
        ActiveJobs   int    `json:"active_jobs"`
        FFmpegVersion string `json:"ffmpeg_version"`
}

// SystemInfo represents system information
type SystemInfo struct {
        OS          string  `json:"os"`
        Kernel      string  `json:"kernel"`
        Arch        string  `json:"arch"`
        CPUCount    int     `json:"cpu_count"`
        MemoryMB    float64 `json:"memory_mb"`
        MemoryFreeMB float64 `json:"memory_free_mb"`
        DiskTotalGB float64 `json:"disk_total_gb"`
        DiskFreeGB  float64 `json:"disk_free_gb"`
        GPUInfo     []GPUDevice `json:"gpu_devices"`
        LoadAvg     string  `json:"load_avg"`
        UptimeSec   float64 `json:"uptime_sec"`
}

// GPUDevice represents a GPU device
type GPUDevice struct {
        Name     string `json:"name"`
        Type     string `json:"type"` // "vaapi", "nvenc", "vulkan"
        VRAMMB   int    `json:"vram_mb"`
        Available bool  `json:"available"`
}

// VideoStatus constants
const (
        StatusUploaded  = "uploaded"
        StatusProcessing = "processing"
        StatusReady     = "ready"
        StatusFailed    = "failed"
)

// TranscodeStep constants
const (
        StepProbe       = "probing_source"
        StepTranscoding = "transcoding"
        StepHLS         = "generating_hls"
        StepDASH        = "generating_dash"
        StepFinalizing  = "finalizing"
)
