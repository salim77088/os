package transcode

import (
        "context"
        "fmt"
        "log"
        "os"
        "os/exec"
        "path/filepath"
        "strconv"
        "strings"
        "sync"
        "time"

        "github.com/salim77088/os/api/internal/config"
        "github.com/salim77088/os/api/internal/database"
        "github.com/salim77088/os/api/internal/models"
        "github.com/salim77088/os/api/internal/store"
)

// Engine manages video transcoding operations
type Engine struct {
        cfg      config.TranscodeConfig
        db       *database.DB
        store    *store.FileStore
        queue    chan TranscodeTask
        workers  int
        wg       sync.WaitGroup
        cancel   context.CancelFunc

        gpuAvailable bool
        gpuType      string
        ffmpegPath   string
        ffprobePath  string

        mu        sync.Mutex
        activeJobs int
}

// TranscodeTask represents a video transcoding task
type TranscodeTask struct {
        VideoID    string
        SourcePath string
}

// NewEngine creates a new transcoding engine
func NewEngine(cfg config.TranscodeConfig, db *database.DB, fileStore *store.FileStore) (*Engine, error) {
        engine := &Engine{
                cfg:     cfg,
                db:      db,
                store:   fileStore,
                queue:   make(chan TranscodeTask, 100),
                workers: cfg.Workers,
        }

        // Detect FFmpeg and FFprobe
        if err := engine.detectFFmpeg(); err != nil {
                return nil, fmt.Errorf("FFmpeg detection failed: %w", err)
        }

        // Detect GPU acceleration capabilities
        engine.detectGPU()

        log.Printf("Transcoding engine initialized: FFmpeg=%s, GPU=%v, GPUType=%s",
                engine.ffmpegPath, engine.gpuAvailable, engine.gpuType)

        return engine, nil
}

// Start launches the transcoding worker pool
func (e *Engine) Start() {
        ctx, cancel := context.WithCancel(context.Background())
        e.cancel = cancel

        for i := 0; i < e.workers; i++ {
                e.wg.Add(1)
                go e.worker(ctx, i)
        }
}

// Stop gracefully shuts down the transcoding workers
func (e *Engine) Stop() {
        e.cancel()
        e.wg.Wait()
}

// Enqueue adds a video to the transcoding queue
func (e *Engine) Enqueue(videoID, sourcePath string) error {
        task := TranscodeTask{
                VideoID:    videoID,
                SourcePath: sourcePath,
        }

        select {
        case e.queue <- task:
                log.Printf("Video %s enqueued for transcoding", videoID)
                return nil
        default:
                return fmt.Errorf("transcoding queue is full, try again later")
        }
}

// GetActiveJobs returns the number of currently active transcoding jobs
func (e *Engine) GetActiveJobs() int {
        e.mu.Lock()
        defer e.mu.Unlock()
        return e.activeJobs
}

// IsGPUAvailable returns whether GPU acceleration is available
func (e *Engine) IsGPUAvailable() bool {
        return e.gpuAvailable
}

// GetGPUType returns the detected GPU acceleration type
func (e *Engine) GetGPUType() string {
        return e.gpuType
}

// GetFFmpegVersion returns the FFmpeg version string
func (e *Engine) GetFFmpegVersion() string {
        cmd := exec.Command(e.ffmpegPath, "-version")
        output, err := cmd.CombinedOutput()
        if err != nil {
                return "unknown"
        }
        lines := strings.Split(string(output), "\n")
        if len(lines) > 0 {
                return lines[0]
        }
        return "unknown"
}

// worker processes transcoding tasks from the queue
func (e *Engine) worker(ctx context.Context, id int) {
        defer e.wg.Done()

        for {
                select {
                case <-ctx.Done():
                        log.Printf("Transcode worker %d shutting down", id)
                        return
                case task := <-e.queue:
                        e.mu.Lock()
                        e.activeJobs++
                        e.mu.Unlock()

                        log.Printf("Worker %d: starting transcoding for video %s", id, task.VideoID)
                        err := e.processVideo(ctx, task)

                        e.mu.Lock()
                        e.activeJobs--
                        e.mu.Unlock()

                        if err != nil {
                                log.Printf("Worker %d: transcoding failed for video %s: %v", id, task.VideoID, err)
                                e.db.UpdateVideoStatus(task.VideoID, models.StatusFailed, err.Error())
                        } else {
                                log.Printf("Worker %d: transcoding completed for video %s", id, task.VideoID)
                        }
                }
        }
}

// processVideo handles the complete transcoding pipeline for a video
func (e *Engine) processVideo(ctx context.Context, task TranscodeTask) error {
        videoID := task.VideoID
        sourcePath := task.SourcePath

        // Update status to processing
        e.db.UpdateVideoStatus(videoID, models.StatusProcessing, "Starting transcoding pipeline")

        // Step 1: Probe source video
        e.db.UpdateVideoStatus(videoID, models.StatusProcessing, models.StepProbe)
        probeData, err := e.probeVideo(sourcePath)
        if err != nil {
                return fmt.Errorf("failed to probe video: %w", err)
        }

        log.Printf("Video %s: duration=%.2f, resolution=%dx%d, codec=%s, bitrate=%d",
                videoID, probeData.Duration, probeData.Width, probeData.Height,
                probeData.VideoCodec, probeData.Bitrate)

        // Create output directories
        if err := e.store.CreateVideoDirectories(videoID); err != nil {
                return fmt.Errorf("failed to create output directories: %w", err)
        }

        hlsDir := e.store.GetHLSOutputDir(videoID)
        dashDir := e.store.GetDASHOutputDir(videoID)

        // Step 2: Determine codec and generate transcoding profiles
        codec := e.determineCodec(probeData)
        resolutions := e.determineResolutions(probeData)

        log.Printf("Video %s: using codec=%s, resolutions=%v", videoID, codec, len(resolutions))

        // Step 3: Transcode to multiple resolutions
        e.db.UpdateVideoStatus(videoID, models.StatusProcessing, models.StepTranscoding)
        
        var transcodedFiles []TranscodedOutput
        for _, res := range resolutions {
                output, err := e.transcodeResolution(ctx, sourcePath, videoID, res, codec, probeData)
                if err != nil {
                        // If one resolution fails, continue with others
                        log.Printf("Warning: failed to transcode %s for video %s: %v", res.Name, videoID, err)
                        continue
                }
                transcodedFiles = append(transcodedFiles, output)
        }

        if len(transcodedFiles) == 0 {
                return fmt.Errorf("all transcoding resolutions failed")
        }

        // Step 4: Generate HLS packages
        if e.cfg.EnableHLS {
                e.db.UpdateVideoStatus(videoID, models.StatusProcessing, models.StepHLS)
                for _, tf := range transcodedFiles {
                        if err := e.generateHLS(ctx, tf.OutputPath, hlsDir, tf.Profile, codec); err != nil {
                                log.Printf("Warning: HLS generation failed for %s: %v", tf.Profile.Name, err)
                                continue
                        }
                }
                // Generate master HLS playlist
                if err := e.generateMasterHLSPlaylist(hlsDir, transcodedFiles); err != nil {
                        return fmt.Errorf("failed to generate master HLS playlist: %w", err)
                }
        }

        // Step 5: Generate DASH packages
        if e.cfg.EnableDASH {
                e.db.UpdateVideoStatus(videoID, models.StatusProcessing, models.StepDASH)
                if err := e.generateDASH(ctx, sourcePath, dashDir, transcodedFiles, codec, probeData); err != nil {
                        log.Printf("Warning: DASH generation failed: %v", err)
                }
        }

        // Step 6: Finalize - update database
        e.db.UpdateVideoStatus(videoID, models.StatusProcessing, models.StepFinalizing)

        hlsPath := filepath.Join(hlsDir, "master.m3u8")
        dashPath := filepath.Join(dashDir, "manifest.mpd")

        // Update video metadata in database
        e.db.UpdateVideoTranscoded(videoID, hlsPath, dashPath,
                probeData.Duration, probeData.Width, probeData.Height, codec)

        // Clean up intermediate transcoded files (keep only HLS/DASH segments)
        e.cleanupIntermediateFiles(videoID, transcodedFiles)

        return nil
}

// probeVideo extracts video metadata using ffprobe
func (e *Engine) probeVideo(path string) (*ProbeData, error) {
        cmd := exec.CommandContext(context.Background(), e.ffprobePath,
                "-v", "quiet",
                "-print_format", "json",
                "-show_format",
                "-show_streams",
                path,
        )

        output, err := cmd.CombinedOutput()
        if err != nil {
                return nil, fmt.Errorf("ffprobe failed: %w (output: %s)", err, string(output))
        }

        return parseProbeOutput(output)
}

// determineCodec selects the best codec based on GPU availability and configuration
func (e *Engine) determineCodec(probe *ProbeData) string {
        if e.gpuAvailable {
                switch e.gpuType {
                case "vaapi":
                        if e.cfg.PreferredCodec == "av1" {
                                return "av1_vaapi"
                        }
                        return "h264_vaapi"
                case "nvenc":
                        if e.cfg.PreferredCodec == "av1" {
                                return "av1_nvenc"
                        }
                        return "h264_nvenc"
                case "vulkan":
                        if e.cfg.PreferredCodec == "av1" {
                                return "av1_vulkan"
                        }
                        return "h264_vulkan"
                }
        }

        // Software encoding fallback
        switch e.cfg.PreferredCodec {
        case "av1":
                return "libsvtav1"
        case "h264":
                return "libx264"
        case "hevc":
                return "libx265"
        default:
                return "libsvtav1"
        }
}

// determineResolutions selects the appropriate resolution ladder based on source
func (e *Engine) determineResolutions(probe *ProbeData) []config.ResProfile {
        resolutions := []config.ResProfile{}

        for _, res := range e.cfg.ResolutionLadder {
                // Only include resolutions smaller than or equal to source
                if res.Height <= probe.Height {
                        resolutions = append(resolutions, res)
                }
        }

        // If no resolutions match (very small source), use source resolution
        if len(resolutions) == 0 {
                resolutions = append(resolutions, config.ResProfile{
                        Name:      "source",
                        Width:     probe.Width,
                        Height:    probe.Height,
                        Bitrate:   "1M",
                        MaxBitrate: "2M",
                })
        }

        return resolutions
}

// TranscodedOutput represents a transcoded video file
type TranscodedOutput struct {
        OutputPath string
        Profile    config.ResProfile
}

// transcodeResolution transcodes a video to a specific resolution
func (e *Engine) transcodeResolution(ctx context.Context, sourcePath, videoID string,
        profile config.ResProfile, codec string, probe *ProbeData) (*TranscodedOutput, error) {

        outputPath := filepath.Join(e.store.GetTempDir(videoID),
                fmt.Sprintf("%s_%s.mp4", videoID, profile.Name))

        args := e.buildTranscodeArgs(sourcePath, outputPath, profile, codec, probe)

        log.Printf("Transcoding %s to %s (%s): ffmpeg %s", videoID, profile.Name, codec,
                strings.Join(args, " "))

        cmd := exec.CommandContext(ctx, e.ffmpegPath, args...)
        output, err := cmd.CombinedOutput()
        if err != nil {
                return nil, fmt.Errorf("FFmpeg transcode failed: %w (output: %s)", err, truncateString(string(output), 500))
        }

        return &TranscodedOutput{
                OutputPath: outputPath,
                Profile:    profile,
        }, nil
}

// buildTranscodeArgs constructs FFmpeg command arguments for transcoding
func (e *Engine) buildTranscodeArgs(input, output string, profile config.ResProfile,
        codec string, probe *ProbeData) []string {

        args := []string{}

        // Input and hardware acceleration
        if e.gpuAvailable && e.cfg.ZeroCopy {
                args = append(args, e.buildHWAccelInputArgs(probe)...)
        }

        args = append(args, "-i", input)

        // Video codec and quality
        args = append(args, e.buildVideoCodecArgs(codec, profile)...)

        // Scaling filter
        if profile.Width != probe.Width || profile.Height != probe.Height {
                args = append(args, e.buildScaleFilter(profile, codec)...)
        }

        // Audio codec
        args = append(args, "-c:a", "aac", "-b:a", e.cfg.AudioBitrate, "-ac", "2")

        // Output format
        args = append(args, "-y", output)

        return args
}

// buildHWAccelInputArgs builds hardware acceleration input arguments
func (e *Engine) buildHWAccelInputArgs(probe *ProbeData) []string {
        switch e.gpuType {
        case "vaapi":
                return []string{
                        "-hwaccel", "vaapi",
                        "-hwaccel_device", "/dev/dri/renderD128",
                        "-hwaccel_output_format", "vaapi",
                }
        case "nvenc":
                return []string{
                        "-hwaccel", "cuda",
                        "-hwaccel_output_format", "cuda",
                }
        case "vulkan":
                return []string{
                        "-init_hw_device", "vulkan=vk:0",
                        "-hwaccel", "vulkan",
                        "-hwaccel_output_format", "vulkan",
                }
        default:
                return []string{}
        }
}

// buildVideoCodecArgs builds video codec arguments
func (e *Engine) buildVideoCodecArgs(codec string, profile config.ResProfile) []string {
        args := []string{"-c:v", codec}

        crf := e.cfg.GetCRFValue(codec)
        isHardware := isHardwareCodec(codec)

        if isHardware {
                // Hardware encoding uses bitrate control, not CRF
                args = append(args,
                        "-b:v", profile.Bitrate,
                        "-maxrate", profile.MaxBitrate,
                        "-bufsize", multiplyBitrate(profile.MaxBitrate, 2),
                )
        } else {
                // Software encoding uses CRF
                args = append(args, "-crf", strconv.Itoa(crf))

                // Codec-specific options
                switch codec {
                case "libsvtav1":
                        args = append(args, "-preset", "6") // Speed preset for SVT-AV1
                case "libx264":
                        args = append(args, "-preset", "medium", "-tune", "film")
                case "libx265":
                        args = append(args, "-preset", "medium")
                case "libvpx-vp9":
                        args = append(args, "-b:v", profile.Bitrate, "-maxrate", profile.MaxBitrate)
                }
        }

        return args
}

// buildScaleFilter builds the video scaling filter
func (e *Engine) buildScaleFilter(profile config.ResProfile, codec string) []string {
        if isHardwareCodec(codec) {
                // Hardware scaling filter
                switch e.gpuType {
                case "vaapi":
                        return []string{
                                "-vf", fmt.Sprintf("format=vaapi|nv12,hwupload,scale_vaapi=w=%d:h=%d",
                                        profile.Width, profile.Height),
                        }
                case "nvenc":
                        return []string{
                                "-vf", fmt.Sprintf("scale_cuda=%d:%d", profile.Width, profile.Height),
                        }
                case "vulkan":
                        return []string{
                                "-vf", fmt.Sprintf("scale_vulkan=w=%d:h=%d", profile.Width, profile.Height),
                        }
                default:
                        return []string{
                                "-vf", fmt.Sprintf("scale=%d:%d", profile.Width, profile.Height),
                        }
                }
        }

        // Software scaling filter (high quality)
        return []string{
                "-vf", fmt.Sprintf("scale=%d:%d:flags=lanczos", profile.Width, profile.Height),
        }
}

// detectFFmpeg locates FFmpeg and FFprobe binaries
func (e *Engine) detectFFmpeg() error {
        // Check common locations
        ffmpegPaths := []string{
                "/usr/bin/ffmpeg",
                "/usr/local/bin/ffmpeg",
                "/opt/microos/bin/ffmpeg",
        }

        ffprobePaths := []string{
                "/usr/bin/ffprobe",
                "/usr/local/bin/ffprobe",
                "/opt/microos/bin/ffprobe",
        }

        // Find FFmpeg
        for _, path := range ffmpegPaths {
                if _, err := os.Stat(path); err == nil {
                        e.ffmpegPath = path
                        break
                }
        }

        if e.ffmpegPath == "" {
                // Try PATH lookup
                path, err := exec.LookPath("ffmpeg")
                if err != nil {
                        return fmt.Errorf("FFmpeg not found: %w", err)
                }
                e.ffmpegPath = path
        }

        // Find FFprobe
        for _, path := range ffprobePaths {
                if _, err := os.Stat(path); err == nil {
                        e.ffprobePath = path
                        break
                }
        }

        if e.ffprobePath == "" {
                path, err := exec.LookPath("ffprobe")
                if err != nil {
                        return fmt.Errorf("FFprobe not found: %w", err)
                }
                e.ffprobePath = path
        }

        return nil
}

// detectGPU detects available GPU acceleration hardware
func (e *Engine) detectGPU() {
        if !e.cfg.EnableGPU {
                e.gpuAvailable = false
                e.gpuType = "software"
                return
        }

        // Auto-detect GPU type
        if e.cfg.GPUType == "auto" {
                e.autoDetectGPU()
                return
        }

        // Verify the configured GPU type is available
        switch e.cfg.GPUType {
        case "vaapi":
                e.gpuAvailable = e.checkVAAPI()
                if !e.gpuAvailable {
                        e.gpuType = "software"
                } else {
                        e.gpuType = "vaapi"
                }
        case "nvenc":
                e.gpuAvailable = e.checkNVENC()
                if !e.gpuAvailable {
                        e.gpuType = "software"
                } else {
                        e.gpuType = "nvenc"
                }
        case "vulkan":
                e.gpuAvailable = e.checkVulkan()
                if !e.gpuAvailable {
                        e.gpuType = "software"
                } else {
                        e.gpuType = "vulkan"
                }
        default:
                e.gpuAvailable = false
                e.gpuType = "software"
        }
}

// autoDetectGPU automatically detects the best available GPU acceleration
func (e *Engine) autoDetectGPU() {
        // Priority: NVENC > Vulkan > VAAPI > Software
        if e.checkNVENC() {
                e.gpuAvailable = true
                e.gpuType = "nvenc"
                log.Printf("GPU detected: NVIDIA NVENC")
                return
        }

        if e.checkVulkan() {
                e.gpuAvailable = true
                e.gpuType = "vulkan"
                log.Printf("GPU detected: Vulkan Video")
                return
        }

        if e.checkVAAPI() {
                e.gpuAvailable = true
                e.gpuType = "vaapi"
                log.Printf("GPU detected: VA-API (Intel/AMD)")
                return
        }

        e.gpuAvailable = false
        e.gpuType = "software"
        log.Printf("No GPU acceleration detected, using software encoding")
}

// checkVAAPI checks if VA-API is available
func (e *Engine) checkVAAPI() bool {
        // Check for DRI device
        if _, err := os.Stat("/dev/dri/renderD128"); err != nil {
                return false
        }

        // Check FFmpeg VA-API support
        cmd := exec.Command(e.ffmpegPath, "-hide_banner", "-encoders")
        output, err := cmd.CombinedOutput()
        if err != nil {
                return false
        }

        return strings.Contains(string(output), "h264_vaapi") ||
                strings.Contains(string(output), "hevc_vaapi") ||
                strings.Contains(string(output), "av1_vaapi")
}

// checkNVENC checks if NVIDIA NVENC is available
func (e *Engine) checkNVENC() bool {
        // Check for NVIDIA device
        if _, err := os.Stat("/dev/nvidia0"); err != nil {
                return false
        }

        // Check FFmpeg NVENC support
        cmd := exec.Command(e.ffmpegPath, "-hide_banner", "-encoders")
        output, err := cmd.CombinedOutput()
        if err != nil {
                return false
        }

        return strings.Contains(string(output), "h264_nvenc") ||
                strings.Contains(string(output), "hevc_nvenc") ||
                strings.Contains(string(output), "av1_nvenc")
}

// checkVulkan checks if Vulkan video encoding is available
func (e *Engine) checkVulkan() bool {
        // Check for Vulkan ICD
        icdPaths := []string{
                "/etc/vulkan/icd.d",
                "/usr/share/vulkan/icd.d",
        }
        for _, dir := range icdPaths {
                entries, err := os.ReadDir(dir)
                if err == nil && len(entries) > 0 {
                        // Check FFmpeg Vulkan support
                        cmd := exec.Command(e.ffmpegPath, "-hide_banner", "-encoders")
                        output, err := cmd.CombinedOutput()
                        if err != nil {
                                return false
                        }
                        return strings.Contains(string(output), "av1_vulkan") ||
                                strings.Contains(string(output), "h264_vulkan")
                }
        }
        return false
}

// cleanupIntermediateFiles removes intermediate transcoded files
func (e *Engine) cleanupIntermediateFiles(videoID string, outputs []TranscodedOutput) {
        tempDir := e.store.GetTempDir(videoID)
        os.RemoveAll(tempDir) // Clean up temp directory
}

// Helper functions

func isHardwareCodec(codec string) bool {
        return strings.Contains(codec, "_vaapi") ||
                strings.Contains(codec, "_nvenc") ||
                strings.Contains(codec, "_vulkan")
}

func multiplyBitrate(bitrate string, multiplier int) string {
        // Parse bitrate (e.g., "5M", "2500k")
        bitrate = strings.TrimSpace(bitrate)
        value, err := strconv.ParseFloat(bitrate[:len(bitrate)-1], 64)
        if err != nil {
                return bitrate // Return as-is if parsing fails
        }

        unit := bitrate[len(bitrate)-1:]
        result := value * float64(multiplier)
        return fmt.Sprintf("%.0f%s", result, unit)
}

func truncateString(s string, maxLen int) string {
        if len(s) <= maxLen {
                return s
        }
        return s[:maxLen] + "..."
}

// ProbeData holds video metadata from ffprobe
type ProbeData struct {
        Duration     float64
        Width        int
        Height       int
        VideoCodec   string
        AudioCodec   string
        Bitrate      int64
        FPS          float64
        Container    string
        HasAudio     bool
        AudioChannels int
}

// parseProbeOutput parses ffprobe JSON output into ProbeData
func parseProbeOutput(output []byte) (*ProbeData, error) {
        // Simple JSON parsing - extract key fields
        data := &ProbeData{}
        content := string(output)

        // Extract duration
        data.Duration = extractFloat(content, "duration")

        // Extract video stream info
        data.VideoCodec = extractString(content, "codec_name")
        data.Width = extractInt(content, "width")
        data.Height = extractInt(content, "height")
        data.FPS = extractFPS(content)
        data.Bitrate = extractInt64(content, "bit_rate")

        // Check for audio
        data.HasAudio = strings.Contains(content, "\"codec_type\": \"audio\"")
        data.AudioCodec = extractAudioCodec(content)

        // Extract container format
        data.Container = extractString(content, "format_name")

        return data, nil
}

func extractFloat(content, key string) float64 {
        // Look for "key": "value" or "key": value patterns
        patterns := []string{
                fmt.Sprintf("\"%s\": \"", key),
                fmt.Sprintf("\"%s\": ", key),
        }
        for _, pattern := range patterns {
                idx := strings.Index(content, pattern)
                if idx >= 0 {
                        start := idx + len(pattern)
                        end := start
                        for end < len(content) && content[end] != '"' && content[end] != ',' && content[end] != '}' {
                                end++
                        }
                        val, err := strconv.ParseFloat(strings.TrimSpace(content[start:end]), 64)
                        if err == nil {
                                return val
                        }
                }
        }
        return 0
}

func extractInt(content, key string) int {
        return int(extractFloat(content, key))
}

func extractInt64(content, key string) int64 {
        return int64(extractFloat(content, key))
}

func extractString(content, key string) string {
        pattern := fmt.Sprintf("\"%s\": \"", key)
        idx := strings.Index(content, pattern)
        if idx < 0 {
                return ""
        }
        start := idx + len(pattern)
        end := strings.Index(content[start:], "\"")
        if end < 0 {
                return ""
        }
        return content[start:start+end]
}

func extractFPS(content string) float64 {
        // Look for r_frame_rate field
        fpsStr := extractString(content, "r_frame_rate")
        if fpsStr == "" {
                return 30.0 // default
        }
        parts := strings.Split(fpsStr, "/")
        if len(parts) == 2 {
                num, err1 := strconv.ParseFloat(parts[0], 64)
                den, err2 := strconv.ParseFloat(parts[1], 64)
                if err1 == nil && err2 == nil && den > 0 {
                        return num / den
                }
        }
        val, err := strconv.ParseFloat(fpsStr, 64)
        if err == nil {
                return val
        }
        return 30.0
}

func extractAudioCodec(content string) string {
        // Find audio stream section
        audioIdx := strings.Index(content, "\"codec_type\": \"audio\"")
        if audioIdx < 0 {
                return ""
        }

        // Look backwards for codec_name in the same stream block
        section := content[:audioIdx]
        lastCodecIdx := strings.LastIndex(section, "\"codec_name\": \"")
        if lastCodecIdx < 0 {
                return ""
        }
        start := lastCodecIdx + len("\"codec_name\": \"")
        end := strings.Index(section[start:], "\"")
        if end < 0 {
                return ""
        }
        return section[start:start+end]
}
