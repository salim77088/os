package config

import (
        "fmt"
        "os"
        "path/filepath"
        "strconv"
        "strings"
)

// Config holds the entire system configuration
type Config struct {
        Server   ServerConfig   `json:"server"`
        Database DatabaseConfig `json:"database"`
        Storage  StorageConfig  `json:"storage"`
        Transcode TranscodeConfig `json:"transcode"`
        Auth     AuthConfig     `json:"auth"`
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
        Host            string `json:"host"`
        Port            int    `json:"port"`
        ReadTimeout     int    `json:"read_timeout"`
        WriteTimeout    int    `json:"write_timeout"`
        MaxUploadSize   int64  `json:"max_upload_size"`
        EnableCORS      bool   `json:"enable_cors"`
        TLSCertFile     string `json:"tls_cert_file"`
        TLSKeyFile      string `json:"tls_key_file"`
}

// DatabaseConfig holds SQLite database configuration
type DatabaseConfig struct {
        Path           string `json:"path"`
        MaxOpenConns   int    `json:"max_open_conns"`
        MaxIdleConns   int    `json:"max_idle_conns"`
        ConnMaxLifetime int   `json:"conn_max_lifetime"`
}

// StorageConfig holds file storage configuration
type StorageConfig struct {
        BasePath       string `json:"base_path"`
        VideosDir      string `json:"videos_dir"`
        HLSDir         string `json:"hls_dir"`
        DASHDir        string `json:"dash_dir"`
        TempDir        string `json:"temp_dir"`
        MaxStorageGB   int    `json:"max_storage_gb"`
}

// TranscodeConfig holds transcoding engine configuration
type TranscodeConfig struct {
        Workers           int     `json:"workers"`
        EnableGPU         bool    `json:"enable_gpu"`
        GPUType           string  `json:"gpu_type"` // "vaapi", "nvenc", "vulkan", "auto"
        PreferredCodec    string  `json:"preferred_codec"` // "av1", "h264", "hevc", "vp9"
        H264CRF           int     `json:"h264_crf"`
        AV1CRF            int     `json:"av1_crf"`
        HEVCCRF           int     `json:"hevc_crf"`
        HLSSegmentDuration int    `json:"hls_segment_duration"`
        HLSPlaylistSize   int     `json:"hls_playlist_size"`
        DASHSegmentDuration int   `json:"dash_segment_duration"`
        EnableHLS         bool    `json:"enable_hls"`
        EnableDASH        bool    `json:"enable_dash"`
        ResolutionLadder  []ResProfile `json:"resolution_ladder"`
        AudioBitrate      string  `json:"audio_bitrate"`
        ZeroCopy          bool    `json:"zero_copy"`
}

// ResProfile defines a resolution profile for adaptive streaming
type ResProfile struct {
        Name       string `json:"name"`
        Width      int    `json:"width"`
        Height     int    `json:"height"`
        Bitrate    string `json:"bitrate"`
        MaxBitrate string `json:"max_bitrate"`
}

// AuthConfig holds API authentication configuration
type AuthConfig struct {
        Enabled      bool     `json:"enabled"`
        APIKeys      []string `json:"api_keys"`
        AdminKey     string   `json:"admin_key"`
}

// DefaultConfig returns the default configuration values
func DefaultConfig() *Config {
        return &Config{
                Server: ServerConfig{
                        Host:            getEnv("MICROOS_HOST", "0.0.0.0"),
                        Port:            getEnvInt("MICROOS_PORT", 8080),
                        ReadTimeout:     30,
                        WriteTimeout:    120,
                        MaxUploadSize:   5 << 30, // 5 GB
                        EnableCORS:      true,
                        TLSCertFile:     "",
                        TLSKeyFile:      "",
                },
                Database: DatabaseConfig{
                        Path:           getEnv("MICROOS_DB_PATH", "/var/lib/microos/microos.db"),
                        MaxOpenConns:   5,
                        MaxIdleConns:   2,
                        ConnMaxLifetime: 300,
                },
                Storage: StorageConfig{
                        BasePath:       getEnv("MICROOS_STORAGE_PATH", "/var/lib/microos"),
                        VideosDir:      "videos",
                        HLSDir:         "hls",
                        DASHDir:        "dash",
                        TempDir:        "tmp",
                        MaxStorageGB:   0, // unlimited
                },
                Transcode: TranscodeConfig{
                        Workers:           getEnvInt("MICROOS_WORKERS", 2),
                        EnableGPU:         getEnvBool("MICROOS_GPU", true),
                        GPUType:           getEnv("MICROOS_GPU_TYPE", "auto"),
                        PreferredCodec:    getEnv("MICROOS_CODEC", "av1"),
                        H264CRF:           23,
                        AV1CRF:            30,
                        HEVCCRF:           28,
                        HLSSegmentDuration: 6,
                        HLSPlaylistSize:   0,
                        DASHSegmentDuration: 6,
                        EnableHLS:         true,
                        EnableDASH:        true,
                        ZeroCopy:          true,
                        AudioBitrate:      "128k",
                        ResolutionLadder: []ResProfile{
                                {Name: "1080p", Width: 1920, Height: 1080, Bitrate: "5M", MaxBitrate: "8M"},
                                {Name: "720p", Width: 1280, Height: 720, Bitrate: "2.5M", MaxBitrate: "4M"},
                                {Name: "480p", Width: 854, Height: 480, Bitrate: "1M", MaxBitrate: "1.5M"},
                                {Name: "360p", Width: 640, Height: 360, Bitrate: "500k", MaxBitrate: "750k"},
                        },
                },
                Auth: AuthConfig{
                        Enabled:  getEnvBool("MICROOS_AUTH", false),
                        APIKeys:  []string{},
                        AdminKey: getEnv("MICROOS_ADMIN_KEY", ""),
                },
        }
}

// Load loads configuration from environment variables and optional config file path
func Load(configPath string) (*Config, error) {
        cfg := DefaultConfig()

        // If config path is provided, load from file (TOML/JSON)
        if configPath != "" {
                if _, err := os.Stat(configPath); err == nil {
                        // Config file loading would be implemented here
                        // For now, environment variables take precedence
                        logConfigSource(configPath)
                }
        }

        // Validate configuration
        if err := validate(cfg); err != nil {
                return nil, fmt.Errorf("configuration validation failed: %w", err)
        }

        // Ensure storage directories exist
        if err := ensureStorageDirs(cfg); err != nil {
                return nil, fmt.Errorf("failed to create storage directories: %w", err)
        }

        return cfg, nil
}

func validate(cfg *Config) error {
        if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
                return fmt.Errorf("invalid server port: %d", cfg.Server.Port)
        }
        if cfg.Server.MaxUploadSize < 1<<20 { // minimum 1MB
                return fmt.Errorf("max upload size too small: %d bytes", cfg.Server.MaxUploadSize)
        }
        if cfg.Transcode.Workers < 1 {
                return fmt.Errorf("transcode workers must be at least 1")
        }
        if cfg.Transcode.HLSSegmentDuration < 2 {
                return fmt.Errorf("HLS segment duration must be at least 2 seconds")
        }
        if cfg.Transcode.PreferredCodec != "av1" && cfg.Transcode.PreferredCodec != "h264" &&
                cfg.Transcode.PreferredCodec != "hevc" && cfg.Transcode.PreferredCodec != "vp9" {
                return fmt.Errorf("unsupported preferred codec: %s", cfg.Transcode.PreferredCodec)
        }
        if cfg.Transcode.GPUType != "vaapi" && cfg.Transcode.GPUType != "nvenc" &&
                cfg.Transcode.GPUType != "vulkan" && cfg.Transcode.GPUType != "auto" && cfg.Transcode.GPUType != "software" {
                return fmt.Errorf("unsupported GPU type: %s", cfg.Transcode.GPUType)
        }
        return nil
}

func ensureStorageDirs(cfg *Config) error {
        dirs := []string{
                cfg.Storage.BasePath,
                filepath.Join(cfg.Storage.BasePath, cfg.Storage.VideosDir),
                filepath.Join(cfg.Storage.BasePath, cfg.Storage.HLSDir),
                filepath.Join(cfg.Storage.BasePath, cfg.Storage.DASHDir),
                filepath.Join(cfg.Storage.BasePath, cfg.Storage.TempDir),
        }
        for _, dir := range dirs {
                if err := os.MkdirAll(dir, 0755); err != nil {
                        return fmt.Errorf("failed to create directory %s: %w", dir, err)
                }
        }
        return nil
}

func logConfigSource(path string) {
        // Simple logging - would be enhanced with actual file loading
        fmt.Printf("Config file path specified: %s\n", path)
}

// Environment variable helpers
func getEnv(key, defaultValue string) string {
        if value := os.Getenv(key); value != "" {
                return value
        }
        return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
        if value := os.Getenv(key); value != "" {
                if intVal, err := strconv.Atoi(value); err == nil {
                        return intVal
                }
        }
        return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
        if value := os.Getenv(key); value != "" {
                if boolVal, err := strconv.ParseBool(value); err == nil {
                        return boolVal
                }
        }
        return defaultValue
}

// GetFullStoragePath returns the absolute path for a storage subdirectory
func (c *Config) GetFullStoragePath(subdir string) string {
        return filepath.Join(c.Storage.BasePath, subdir)
}

// GetVideoPath returns the absolute path for a video file
func (c *Config) GetVideoPath(videoID, filename string) string {
        return filepath.Join(c.GetFullStoragePath(c.Storage.VideosDir), videoID, filename)
}

// GetHLSPath returns the absolute path for HLS segments
func (c *Config) GetHLSPath(videoID string) string {
        return filepath.Join(c.GetFullStoragePath(c.Storage.HLSDir), videoID)
}

// GetDASHPath returns the absolute path for DASH segments
func (c *Config) GetDASHPath(videoID string) string {
        return filepath.Join(c.GetFullStoragePath(c.Storage.DASHDir), videoID)
}

// GetStreamBaseURL returns the base URL for streaming content
func (c *Config) GetStreamBaseURL() string {
        host := c.Server.Host
        if host == "0.0.0.0" {
                host = "localhost"
        }
        return fmt.Sprintf("http://%s:%d/stream", host, c.Server.Port)
}

// GetGPUDetectionCommand returns the FFmpeg command to detect available GPU acceleration
func (c *Config) GetGPUDetectionCommand() []string {
        switch c.Transcode.GPUType {
        case "vaapi":
                return []string{"-hwaccel", "vaapi", "-hwaccel_device", "/dev/dri/renderD128", "-hwaccel_output_format", "vaapi"}
        case "nvenc":
                return []string{"-hwaccel", "cuda", "-hwaccel_output_format", "cuda"}
        case "vulkan":
                return []string{"-init_hw_device", "vulkan=vk:0", "-hwaccel", "vulkan", "-hwaccel_output_format", "vulkan"}
        case "auto":
                return []string{} // auto-detect at runtime
        default:
                return []string{} // software encoding
        }
}

// GetVideoCodec returns the FFmpeg video codec name based on configuration and GPU availability
func (c *Config) GetVideoCodec(gpuAvailable bool) string {
        if !gpuAvailable || !c.Transcode.EnableGPU {
                return getSoftwareCodec(c.Transcode.PreferredCodec)
        }
        return getHardwareCodec(c.Transcode.PreferredCodec, c.Transcode.GPUType)
}

func getSoftwareCodec(preferred string) string {
        switch preferred {
        case "av1":
                return "libsvtav1"
        case "h264":
                return "libx264"
        case "hevc":
                return "libx265"
        case "vp9":
                return "libvpx-vp9"
        default:
                return "libsvtav1"
        }
}

func getHardwareCodec(preferred, gpuType string) string {
        switch gpuType {
        case "vaapi":
                switch preferred {
                case "av1":
                        return "av1_vaapi"
                case "h264":
                        return "h264_vaapi"
                case "hevc":
                        return "hevc_vaapi"
                case "vp9":
                        return "vp9_vaapi"
                default:
                        return "h264_vaapi"
                }
        case "nvenc":
                switch preferred {
                case "av1":
                        return "av1_nvenc"
                case "h264":
                        return "h264_nvenc"
                case "hevc":
                        return "hevc_nvenc"
                default:
                        return "h264_nvenc"
                }
        case "vulkan":
                if preferred == "av1" {
                        return "av1_vulkan"
                }
                return "h264_vulkan"
        default:
                return getSoftwareCodec(preferred)
        }
}

// GetCRFValue returns the CRF value for the configured codec (on TranscodeConfig)
func (tc TranscodeConfig) GetCRFValue(codec string) int {
        switch {
        case strings.Contains(codec, "av1"):
                return tc.AV1CRF
        case strings.Contains(codec, "h264"):
                return tc.H264CRF
        case strings.Contains(codec, "hevc") || strings.Contains(codec, "h265"):
                return tc.HEVCCRF
        default:
                return 28
        }
}

// GetCRFValueConfig returns the CRF value from the full Config (delegates to TranscodeConfig)
func (c *Config) GetCRFValueConfig(codec string) int {
        return c.Transcode.GetCRFValue(codec)
}

// StringSliceEnv parses a comma-separated environment variable into a string slice
func StringSliceEnv(key string, defaultValue []string) []string {
        value := os.Getenv(key)
        if value == "" {
                return defaultValue
        }
        return strings.Split(value, ",")
}
