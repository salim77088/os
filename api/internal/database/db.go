package database

import (
        "database/sql"
        "fmt"
        "time"

        "github.com/salim77088/os/api/internal/models"

        _ "modernc.org/sqlite"
)

// DB wraps the SQLite database connection
type DB struct {
        conn *sql.DB
        path string
}

// New creates a new database connection and initializes the schema
func New(dbPath string) (*DB, error) {
        conn, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=synchronous(normal)&_pragma=cache_size(-20000)&_pragma=foreign_keys(1)")
        if err != nil {
                return nil, fmt.Errorf("failed to open database: %w", err)
        }

        // Configure connection pool for low memory usage
        conn.SetMaxOpenConns(3)
        conn.SetMaxIdleConns(1)
        conn.SetConnMaxLifetime(5 * time.Minute)

        db := &DB{conn: conn, path: dbPath}

        // Initialize schema
        if err := db.initSchema(); err != nil {
                conn.Close()
                return nil, fmt.Errorf("failed to initialize schema: %w", err)
        }

        return db, nil
}

// Close closes the database connection
func (db *DB) Close() error {
        return db.conn.Close()
}

// Path returns the database file path
func (db *DB) Path() string {
        return db.path
}

// Conn returns the raw database connection (for health checks)
func (db *DB) Conn() *sql.DB {
        return db.conn
}

// initSchema creates the database tables if they don't exist
func (db *DB) initSchema() error {
        schema := `
        CREATE TABLE IF NOT EXISTS videos (
                id TEXT PRIMARY KEY,
                original_name TEXT NOT NULL,
                original_path TEXT NOT NULL,
                original_size INTEGER NOT NULL,
                mime_type TEXT NOT NULL DEFAULT 'video/mp4',
                duration REAL NOT NULL DEFAULT 0,
                width INTEGER NOT NULL DEFAULT 0,
                height INTEGER NOT NULL DEFAULT 0,
                codec TEXT NOT NULL DEFAULT '',
                bitrate INTEGER NOT NULL DEFAULT 0,
                fps REAL NOT NULL DEFAULT 0,
                status TEXT NOT NULL DEFAULT 'uploaded',
                transcode_log TEXT NOT NULL DEFAULT '',
                transcoded_at TEXT,
                hls_path TEXT NOT NULL DEFAULT '',
                dash_path TEXT NOT NULL DEFAULT '',
                created_at TEXT NOT NULL DEFAULT (datetime('now')),
                updated_at TEXT NOT NULL DEFAULT (datetime('now'))
        );

        CREATE INDEX IF NOT EXISTS idx_videos_status ON videos(status);
        CREATE INDEX IF NOT EXISTS idx_videos_created ON videos(created_at);

        CREATE TABLE IF NOT EXISTS transcode_jobs (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                video_id TEXT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
                status TEXT NOT NULL DEFAULT 'pending',
                progress REAL NOT NULL DEFAULT 0,
                current_step TEXT NOT NULL DEFAULT '',
                error TEXT NOT NULL DEFAULT '',
                started_at TEXT,
                completed_at TEXT,
                created_at TEXT NOT NULL DEFAULT (datetime('now'))
        );

        CREATE INDEX IF NOT EXISTS idx_jobs_video_id ON transcode_jobs(video_id);
        CREATE INDEX IF NOT EXISTS idx_jobs_status ON transcode_jobs(status);
        `

        _, err := db.conn.Exec(schema)
        if err != nil {
                return fmt.Errorf("failed to execute schema: %w", err)
        }

        return nil
}

// InsertVideo adds a new video record to the database
func (db *DB) InsertVideo(video *models.Video) error {
        query := `
        INSERT INTO videos (id, original_name, original_path, original_size, mime_type,
                duration, width, height, codec, bitrate, fps, status, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        `

        now := time.Now().UTC().Format(time.RFC3339)
        _, err := db.conn.Exec(query,
                video.ID, video.OriginalName, video.OriginalPath, video.OriginalSize,
                video.MimeType, video.Duration, video.Width, video.Height, video.Codec,
                video.Bitrate, video.FPS, video.Status, now, now,
        )
        if err != nil {
                return fmt.Errorf("failed to insert video: %w", err)
        }
        return nil
}

// GetVideo retrieves a video by ID
func (db *DB) GetVideo(id string) (*models.Video, error) {
        query := `
        SELECT id, original_name, original_path, original_size, mime_type,
                duration, width, height, codec, bitrate, fps, status, transcode_log,
                transcoded_at, hls_path, dash_path, created_at, updated_at
        FROM videos WHERE id = ?
        `

        row := db.conn.QueryRow(query, id)
        video := &models.Video{}
        var transcodedAt sql.NullString

        err := row.Scan(
                &video.ID, &video.OriginalName, &video.OriginalPath, &video.OriginalSize,
                &video.MimeType, &video.Duration, &video.Width, &video.Height, &video.Codec,
                &video.Bitrate, &video.FPS, &video.Status, &video.TranscodeLog,
                &transcodedAt, &video.HLSPath, &video.DASHPath, &video.CreatedAt, &video.UpdatedAt,
        )
        if err != nil {
                if err == sql.ErrNoRows {
                        return nil, fmt.Errorf("video not found: %s", id)
                }
                return nil, fmt.Errorf("failed to get video: %w", err)
        }

        if transcodedAt.Valid {
                t, err := time.Parse(time.RFC3339, transcodedAt.String)
                if err == nil {
                        video.TranscodedAt = &t
                }
        }

        return video, nil
}

// ListVideos retrieves a paginated list of videos
func (db *DB) ListVideos(page, perPage int) ([]models.Video, int, error) {
        // Get total count
        var total int
        countQuery := "SELECT COUNT(*) FROM videos"
        err := db.conn.QueryRow(countQuery).Scan(&total)
        if err != nil {
                return nil, 0, fmt.Errorf("failed to count videos: %w", err)
        }

        // Get paginated results
        offset := (page - 1) * perPage
        query := `
        SELECT id, original_name, original_path, original_size, mime_type,
                duration, width, height, codec, bitrate, fps, status, transcode_log,
                transcoded_at, hls_path, dash_path, created_at, updated_at
        FROM videos ORDER BY created_at DESC LIMIT ? OFFSET ?
        `

        rows, err := db.conn.Query(query, perPage, offset)
        if err != nil {
                return nil, 0, fmt.Errorf("failed to list videos: %w", err)
        }
        defer rows.Close()

        videos := []models.Video{}
        for rows.Next() {
                video := models.Video{}
                var transcodedAt sql.NullString

                err := rows.Scan(
                        &video.ID, &video.OriginalName, &video.OriginalPath, &video.OriginalSize,
                        &video.MimeType, &video.Duration, &video.Width, &video.Height, &video.Codec,
                        &video.Bitrate, &video.FPS, &video.Status, &video.TranscodeLog,
                        &transcodedAt, &video.HLSPath, &video.DASHPath, &video.CreatedAt, &video.UpdatedAt,
                )
                if err != nil {
                        return nil, 0, fmt.Errorf("failed to scan video row: %w", err)
                }

                if transcodedAt.Valid {
                        t, err := time.Parse(time.RFC3339, transcodedAt.String)
                        if err == nil {
                                video.TranscodedAt = &t
                        }
                }

                videos = append(videos, video)
        }

        return videos, total, nil
}

// UpdateVideoStatus updates the status of a video
func (db *DB) UpdateVideoStatus(id string, status string, log string) error {
        query := `
        UPDATE videos SET status = ?, transcode_log = ?, updated_at = ?
        WHERE id = ?
        `

        now := time.Now().UTC().Format(time.RFC3339)
        _, err := db.conn.Exec(query, status, log, now, id)
        if err != nil {
                return fmt.Errorf("failed to update video status: %w", err)
        }
        return nil
}

// UpdateVideoTranscoded marks a video as transcoded with paths
func (db *DB) UpdateVideoTranscoded(id string, hlsPath, dashPath string, duration float64, width, height int, codec string) error {
        query := `
        UPDATE videos SET status = ?, hls_path = ?, dash_path = ?, duration = ?,
                width = ?, height = ?, codec = ?, transcoded_at = ?, updated_at = ?
        WHERE id = ?
        `

        now := time.Now().UTC().Format(time.RFC3339)
        _, err := db.conn.Exec(query, models.StatusReady, hlsPath, dashPath,
                duration, width, height, codec, now, now, id)
        if err != nil {
                return fmt.Errorf("failed to update video transcoded info: %w", err)
        }
        return nil
}

// DeleteVideo removes a video from the database
func (db *DB) DeleteVideo(id string) error {
        query := "DELETE FROM videos WHERE id = ?"
        _, err := db.conn.Exec(query, id)
        if err != nil {
                return fmt.Errorf("failed to delete video: %w", err)
        }
        return nil
}

// CreateTranscodeJob creates a new transcode job record
func (db *DB) CreateTranscodeJob(videoID string) (int64, error) {
        query := `
        INSERT INTO transcode_jobs (video_id, status, created_at)
        VALUES (?, 'pending', ?)
        `

        now := time.Now().UTC().Format(time.RFC3339)
        result, err := db.conn.Exec(query, videoID, now)
        if err != nil {
                return 0, fmt.Errorf("failed to create transcode job: %w", err)
        }

        return result.LastInsertId()
}

// UpdateTranscodeJob updates a transcode job's progress
func (db *DB) UpdateTranscodeJob(id int64, status string, progress float64, step string, errorMsg string) error {
        query := `
        UPDATE transcode_jobs SET status = ?, progress = ?, current_step = ?, error = ?, updated_at = ?
        WHERE id = ?
        `

        now := time.Now().UTC().Format(time.RFC3339)
        _, err := db.conn.Exec(query, status, progress, step, errorMsg, now, id)
        if err != nil {
                return fmt.Errorf("failed to update transcode job: %w", err)
        }
        return nil
}

// CompleteTranscodeJob marks a transcode job as completed
func (db *DB) CompleteTranscodeJob(id int64) error {
        query := `
        UPDATE transcode_jobs SET status = 'completed', progress = 100, completed_at = ?
        WHERE id = ?
        `

        now := time.Now().UTC().Format(time.RFC3339)
        _, err := db.conn.Exec(query, now, id)
        if err != nil {
                return fmt.Errorf("failed to complete transcode job: %w", err)
        }
        return nil
}

// GetTranscodeJobByVideoID retrieves the transcode job for a video
func (db *DB) GetTranscodeJobByVideoID(videoID string) (*models.TranscodeStatus, error) {
        query := `
        SELECT id, video_id, status, progress, current_step, error, started_at, completed_at
        FROM transcode_jobs WHERE video_id = ? ORDER BY created_at DESC LIMIT 1
        `

        row := db.conn.QueryRow(query, videoID)
        job := &models.TranscodeStatus{}
        var startedAt, completedAt sql.NullString
        var jobID int64 // Discard the integer job ID - we only need video_id

        err := row.Scan(&jobID, &job.VideoID, &job.Status, &job.Progress,
                &job.CurrentStep, &job.Error, &startedAt, &completedAt)
        if err != nil {
                if err == sql.ErrNoRows {
                        return nil, nil // No job found
                }
                return nil, fmt.Errorf("failed to get transcode job: %w", err)
        }

        if startedAt.Valid {
                t, err := time.Parse(time.RFC3339, startedAt.String)
                if err == nil {
                        job.StartedAt = &t
                }
        }
        if completedAt.Valid {
                t, err := time.Parse(time.RFC3339, completedAt.String)
                if err == nil {
                        job.CompletedAt = &t
                }
        }

        return job, nil
}

// GetVideoCount returns the total number of videos
func (db *DB) GetVideoCount() (int, error) {
        var count int
        err := db.conn.QueryRow("SELECT COUNT(*) FROM videos").Scan(&count)
        return count, err
}

// GetDBSize returns the database file size in KB
func (db *DB) GetDBSize() (int64, error) {
        // Use PRAGMA to get page info
        var pageCount, pageSize int64
        err := db.conn.QueryRow("PRAGMA page_count").Scan(&pageCount)
        if err != nil {
                return 0, err
        }
        err = db.conn.QueryRow("PRAGMA page_size").Scan(&pageSize)
        if err != nil {
                return 0, err
        }
        return (pageCount * pageSize) / 1024, nil
}
