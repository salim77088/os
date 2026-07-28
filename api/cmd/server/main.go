package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/salim77088/os/api/internal/config"
	"github.com/salim77088/os/api/internal/database"
	"github.com/salim77088/os/api/internal/server"
	"github.com/salim77088/os/api/internal/store"
	"github.com/salim77088/os/api/internal/transcode"
)

// Build information injected at compile time
var (
	Version   = "0.1.0"
	BuildDate = "unknown"
	Commit    = "unknown"
)

func main() {
	log.Printf("MicroOS Video Streaming Server v%s (build: %s, commit: %s)", Version, BuildDate, Commit)

	// Load configuration
	cfg, err := config.Load("")
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	log.Printf("Configuration loaded: listening on %s:%d", cfg.Server.Host, cfg.Server.Port)

	// Initialize database
	db, err := database.New(cfg.Database.Path)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()
	log.Printf("Database initialized: %s", cfg.Database.Path)

	// Initialize file storage
	fileStore, err := store.New(cfg.Storage)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	log.Printf("Storage initialized: %s", cfg.Storage.BasePath)

	// Initialize transcoding engine
	engine, err := transcode.NewEngine(cfg.Transcode, db, fileStore)
	if err != nil {
		log.Fatalf("Failed to initialize transcoding engine: %v", err)
	}

	// Start the transcoding worker pool
	engine.Start()
	defer engine.Stop()
	log.Printf("Transcoding engine started: %d workers, GPU: %v", cfg.Transcode.Workers, cfg.Transcode.EnableGPU)

	// Create HTTP server
	srv := server.New(cfg, db, fileStore, engine, Version)

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("Received signal: %v, shutting down...", sig)
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("Shutdown error: %v", err)
		}
	}()

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("MicroOS API server starting on %s", addr)
	log.Printf("Endpoints:")
	log.Printf("  POST   /api/v1/upload       - Upload video")
	log.Printf("  GET    /api/v1/videos       - List videos")
	log.Printf("  GET    /api/v1/videos/{id}  - Video details")
	log.Printf("  GET    /api/v1/videos/{id}/stream - Stream URLs")
	log.Printf("  DELETE /api/v1/videos/{id}  - Delete video")
	log.Printf("  GET    /api/v1/videos/{id}/status - Transcode status")
	log.Printf("  GET    /api/v1/health       - Health check")
	log.Printf("  GET    /api/v1/system       - System info")

	if err := srv.ListenAndServe(); err != nil {
		if ctx.Err() != nil {
			log.Printf("Server stopped gracefully")
		} else {
			log.Fatalf("Server error: %v", err)
		}
	}
}
