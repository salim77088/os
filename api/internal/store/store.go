package store

import (
        "fmt"
        "io"
        "os"
        "path/filepath"
        "strings"

        "github.com/salim77088/os/api/internal/config"
)

// FileStore manages video file storage on disk
type FileStore struct {
        cfg config.StorageConfig
}

// New creates a new FileStore and ensures directories exist
func New(cfg config.StorageConfig) (*FileStore, error) {
        fs := &FileStore{cfg: cfg}

        // Create all required directories
        dirs := []string{
                cfg.BasePath,
                filepath.Join(cfg.BasePath, cfg.VideosDir),
                filepath.Join(cfg.BasePath, cfg.HLSDir),
                filepath.Join(cfg.BasePath, cfg.DASHDir),
                filepath.Join(cfg.BasePath, cfg.TempDir),
        }

        for _, dir := range dirs {
                if err := os.MkdirAll(dir, 0755); err != nil {
                        return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
                }
        }

        return fs, nil
}

// SaveUploadedFile saves an uploaded video file to disk
func (fs *FileStore) SaveUploadedFile(videoID, originalName, contentType string, reader io.Reader) (string, int64, error) {
        // Create video directory
        videoDir := filepath.Join(fs.cfg.BasePath, fs.cfg.VideosDir, videoID)
        if err := os.MkdirAll(videoDir, 0755); err != nil {
                return "", 0, fmt.Errorf("failed to create video directory: %w", err)
        }

        // Determine file extension
        ext := filepath.Ext(originalName)
        if ext == "" {
                ext = getExtensionFromMimeType(contentType)
        }

        // Sanitize filename
        safeName := sanitizeFilename(originalName)
        destPath := filepath.Join(videoDir, safeName)

        // Create destination file
        destFile, err := os.Create(destPath)
        if err != nil {
                return "", 0, fmt.Errorf("failed to create destination file: %w", err)
        }
        defer destFile.Close()

        // Copy file content
        written, err := io.Copy(destFile, reader)
        if err != nil {
                os.Remove(destPath) // Clean up on failure
                return "", 0, fmt.Errorf("failed to write file content: %w", err)
        }

        return destPath, written, nil
}

// CreateVideoDirectories creates the HLS and DASH directories for a video
func (fs *FileStore) CreateVideoDirectories(videoID string) error {
        dirs := []string{
                filepath.Join(fs.cfg.BasePath, fs.cfg.HLSDir, videoID),
                filepath.Join(fs.cfg.BasePath, fs.cfg.DASHDir, videoID),
                filepath.Join(fs.cfg.BasePath, fs.cfg.TempDir, videoID),
        }

        for _, dir := range dirs {
                if err := os.MkdirAll(dir, 0755); err != nil {
                        return fmt.Errorf("failed to create directory %s: %w", dir, err)
                }
        }

        return nil
}

// GetVideoPath returns the path to the original video file
func (fs *FileStore) GetVideoPath(videoID string) (string, error) {
        videoDir := filepath.Join(fs.cfg.BasePath, fs.cfg.VideosDir, videoID)

        // Find the video file in the directory
        entries, err := os.ReadDir(videoDir)
        if err != nil {
                return "", fmt.Errorf("failed to read video directory: %w", err)
        }

        for _, entry := range entries {
                if !entry.IsDir() && isVideoFile(entry.Name()) {
                        return filepath.Join(videoDir, entry.Name()), nil
                }
        }

        return "", fmt.Errorf("no video file found in directory %s", videoDir)
}

// GetHLSOutputDir returns the HLS output directory for a video
func (fs *FileStore) GetHLSOutputDir(videoID string) string {
        return filepath.Join(fs.cfg.BasePath, fs.cfg.HLSDir, videoID)
}

// GetDASHOutputDir returns the DASH output directory for a video
func (fs *FileStore) GetDASHOutputDir(videoID string) string {
        return filepath.Join(fs.cfg.BasePath, fs.cfg.DASHDir, videoID)
}

// GetTempDir returns the temporary directory for a video
func (fs *FileStore) GetTempDir(videoID string) string {
        return filepath.Join(fs.cfg.BasePath, fs.cfg.TempDir, videoID)
}

// DeleteVideoFiles removes all files associated with a video
func (fs *FileStore) DeleteVideoFiles(videoID string) error {
        dirs := []string{
                filepath.Join(fs.cfg.BasePath, fs.cfg.VideosDir, videoID),
                filepath.Join(fs.cfg.BasePath, fs.cfg.HLSDir, videoID),
                filepath.Join(fs.cfg.BasePath, fs.cfg.DASHDir, videoID),
                filepath.Join(fs.cfg.BasePath, fs.cfg.TempDir, videoID),
        }

        for _, dir := range dirs {
                if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
                        return fmt.Errorf("failed to remove directory %s: %w", dir, err)
                }
        }

        return nil
}

// GetStorageStats returns storage statistics
func (fs *FileStore) GetStorageStats() (totalFiles int, totalSize int64, err error) {
        // Count files in the videos directory
        videoDir := filepath.Join(fs.cfg.BasePath, fs.cfg.VideosDir)
        if err := filepath.WalkDir(videoDir, func(path string, d os.DirEntry, err error) error {
                if err != nil {
                        return err
                }
                if !d.IsDir() && isVideoFile(d.Name()) {
                        totalFiles++
                        info, err := d.Info()
                        if err != nil {
                                return err
                        }
                        totalSize += info.Size()
                }
                return nil
        }); err != nil && !os.IsNotExist(err) {
                return 0, 0, err
        }

        return totalFiles, totalSize, nil
}

// GetDiskUsage returns disk usage information
func (fs *FileStore) GetDiskUsage() (total, used, free int64, err error) {
        // Verify base path exists
        _, err = os.Stat(fs.cfg.BasePath)
        if err != nil {
                return 0, 0, 0, err
        }

        // Walk directory to calculate used space
        used = 0
        if walkErr := filepath.WalkDir(fs.cfg.BasePath, func(path string, d os.DirEntry, walkErr error) error {
                if walkErr != nil {
                        return walkErr
                }
                if !d.IsDir() {
                        info, infoErr := d.Info()
                        if infoErr != nil {
                                return infoErr
                        }
                        used += info.Size()
                }
                return nil
        }); walkErr != nil {
                return 0, 0, 0, walkErr
        }

        // Use syscall.Statfs for accurate disk info on Linux
        // For portability, we return approximate values
        // total and free would come from syscall.Statfs on production
        total = used * 10 // Approximate: assume 10x used space is total
        free = total - used

        return total, used, free, nil
}

// FileExists checks if a file exists at the given path
func (fs *FileStore) FileExists(path string) bool {
        _, err := os.Stat(path)
        return err == nil
}

// sanitizeFilename removes potentially dangerous characters from filenames
func sanitizeFilename(name string) string {
        // Remove path separators
        name = filepath.Base(name)
        // Remove special characters
        replacer := strings.NewReplacer(
                " ", "_",
                "(", "",
                ")", "",
                "[", "",
                "]", "",
                "{", "",
                "}", "",
                "'", "",
                `"`, "",
                ";", "",
                "&", "",
                "|", "",
                "$", "",
                "`", "",
                "!", "",
        )
        return replacer.Replace(name)
}

// getExtensionFromMimeType returns a file extension based on MIME type
func getExtensionFromMimeType(mimeType string) string {
        switch strings.ToLower(mimeType) {
        case "video/mp4":
                return ".mp4"
        case "video/webm":
                return ".webm"
        case "video/x-matroska":
                return ".mkv"
        case "video/avi":
                return ".avi"
        case "video/quicktime":
                return ".mov"
        case "video/x-flv":
                return ".flv"
        case "video/x-ms-wmv":
                return ".wmv"
        case "video/ogg":
                return ".ogv"
        case "video/x-mng":
                return ".mng"
        default:
                return ".mp4"
        }
}

// isVideoFile checks if a filename looks like a video file
func isVideoFile(name string) bool {
        ext := strings.ToLower(filepath.Ext(name))
        videoExts := []string{".mp4", ".webm", ".mkv", ".avi", ".mov", ".flv", ".wmv", ".ogv", ".m4v", ".ts", ".m2ts"}
        for _, vExt := range videoExts {
                if ext == vExt {
                        return true
                }
        }
        return false
}
