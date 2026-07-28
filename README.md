# MicroOS - Ultra-Light Video Streaming Server OS

**Version:** 0.1.0 | **Target ISO:** < 200MB | **Target RAM:** < 200MB

MicroOS is a custom, standalone, headless, and immutable Linux appliance designed specifically for **video hosting companies, data centers, and streaming platforms**. It integrates two essential functions into a single software package:

1. **Secure digital file storage** — receive, store, and manage video files via a modern REST API
2. **Intelligent real-time video transcoding & streaming** — automatically transcode uploaded videos with GPU-accelerated encoding (AV1/H.264/HEVC) and serve them as HLS/DASH adaptive streams

---

## System Architecture

```
┌──────────────────────────────────────────────────────┐
│                  MicroOS ISO (<200MB)                  │
│                                                        │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐ │
│  │ Alpine Linux  │  │ Go API Server│  │   FFmpeg     │ │
│  │ (virt kernel) │  │ (static bin) │  │ (GPU accel)  │ │
│  │   + OpenRC    │  │  + SQLite    │  │ + VA-API     │ │
│  │   + Mesa/GPU  │  │  + BadgerDB  │  │ + NVENC      │ │
│  │   ~50MB       │  │  ~10-15MB    │  │  ~40MB       │ │
│  └──────────────┘  └──────────────┘  └──────────────┘ │
│                                                        │
│  ┌──────────────────────────────────────────────────┐ │
│  │              RAM Budget (<200MB)                   │ │
│  │  Kernel+init: 50MB │ API: 15MB │ GPU libs: 20MB │ │
│  │  FFmpeg: 5MB │ System: 10MB │ Buffer: 100MB    │ │
│  └──────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────┘
```

---

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/v1/upload` | Upload video file (multipart form) |
| `GET` | `/api/v1/videos` | List all stored videos |
| `GET` | `/api/v1/videos/{id}` | Get video metadata |
| `GET` | `/api/v1/videos/{id}/stream` | Get HLS/DASH streaming URLs |
| `GET` | `/api/v1/videos/{id}/status` | Check transcoding progress |
| `DELETE` | `/api/v1/videos/{id}` | Delete video and all streams |
| `GET` | `/api/v1/health` | System health check |
| `GET` | `/api/v1/system` | System resource information |

---

## GPU Acceleration Support

MicroOS automatically detects and uses available GPU hardware for transcoding:

| GPU Type | Codec Support | Detection Method |
|----------|--------------|-----------------|
| **VA-API** (Intel/AMD) | h264_vaapi, hevc_vaapi, av1_vaapi | `/dev/dri/renderD128` |
| **NVENC** (NVIDIA) | h264_nvenc, hevc_nvenc, av1_nvenc | `/dev/nvidia0` |
| **Vulkan** (Cross-GPU) | av1_vulkan, h264_vulkan | Vulkan ICD files |
| **Software** | libsvtav1, libx264, libx265 | Fallback |

FFmpeg 8.0's Vulkan AV1 encoder provides **cross-GPU, cross-OS video acceleration** — ideal for data centers with mixed GPU hardware.

---

## Resolution Ladder

Videos are transcoded at multiple resolutions for adaptive streaming:

| Name | Resolution | Bitrate | Max Bitrate |
|------|-----------|---------|-------------|
| 1080p | 1920×1080 | 5M | 8M |
| 720p | 1280×720 | 2.5M | 4M |
| 480p | 854×480 | 1M | 1.5M |
| 360p | 640×360 | 500k | 750k |

---

## Quick Start

### Local Development (Docker)

```bash
# Build and run locally
docker-compose up -d

# Upload a video
curl -X POST http://localhost:8080/api/v1/upload -F "video=@test.mp4"

# Check health
curl http://localhost:8080/api/v1/health

# Get streaming URLs (after transcoding completes)
curl http://localhost:8080/api/v1/videos/{id}/stream
```

### Build ISO from Source

```bash
# Build API binary
make api

# Build complete ISO
make iso

# Check ISO size against target
make size-check
```

### Deploy to Server

```bash
# Flash ISO to USB
dd if=microos-0.1.0-x86_64.iso of=/dev/sdX bs=4M status=progress

# Boot USB on server — MicroOS starts automatically
# API available at http://server-ip:8080
```

### CI/CD Automated Build

Push to GitHub with a version tag to trigger automatic ISO generation:

```bash
git tag v0.1.0
git push origin v0.1.0
# GitHub Actions builds the ISO and publishes a release
```

---

## Project Structure

```
os/
├── .github/workflows/build.yml    # CI/CD: automated ISO generation
├── api/                            # Go backend API
│   ├── cmd/server/main.go          # Entry point
│   ├── internal/
│   │   ├── config/config.go        # Configuration management
│   │   ├── models/video.go         # Data models
│   │   ├── database/db.go          # SQLite database
│   │   ├── store/store.go          # File storage
│   │   ├── server/server.go        # HTTP server + handlers
│   │   └── transcode/
│   │       ├── engine.go           # FFmpeg transcoding engine
│   │       └── hls.go              # HLS/DASH generation
│   └── go.mod
├── build/                           # Alpine rootfs and ISO build
│   ├── kernel/config-x86_64        # Stripped kernel config
│   ├── rootfs/etc/
│   │   ├── init.d/microos          # OpenRC service script
│   │   ├── microos/config.toml     # System configuration
│   │   └── network/interfaces      # Network config
├── scripts/
│   ├── build-iso.sh                # ISO build orchestration
│   ├── build-api.sh                # Go binary compilation
│   └── test-api.sh                 # API testing script
├── Dockerfile                       # Development container
├── docker-compose.yml               # Local development setup
├── Makefile                         # Build automation
└── .gitignore
```

---

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `MICROOS_HOST` | `0.0.0.0` | API listen address |
| `MICROOS_PORT` | `8080` | API listen port |
| `MICROOS_DB_PATH` | `/var/lib/microos/microos.db` | SQLite database path |
| `MICROOS_STORAGE_PATH` | `/var/lib/microos` | Video storage base path |
| `MICROOS_GPU` | `auto` | GPU acceleration: `auto`, `vaapi`, `nvenc`, `vulkan`, `software` |
| `MICROOS_CODEC` | `av1` | Preferred codec: `av1`, `h264`, `hevc`, `vp9` |
| `MICROOS_WORKERS` | `2` | Transcoding worker count |
| `MICROOS_AUTH` | `false` | Enable API key authentication |
| `MICROOS_ADMIN_KEY` | `""` | Admin API key |

---

## System Requirements

| Requirement | Minimum | Recommended |
|------------|---------|-------------|
| RAM | 256MB | 1GB+ |
| CPU | 1 core | 4+ cores |
| Storage | 10GB | 100GB+ |
| GPU | Optional | Intel/AMD (VA-API) or NVIDIA (NVENC) |
| Network | Ethernet | 1Gbps+ |

---

## Security

⚠️ **Important:** This project uses GitHub Secrets for CI/CD. Never hardcode tokens in workflow files.

1. Go to your repo **Settings → Secrets → Actions**
2. Add `MICROOS_TOKEN` with your GitHub PAT
3. The workflow uses `${{ secrets.GITHUB_TOKEN }}` (automatic) and `${{ secrets.MICROOS_TOKEN }}` for publishing

---

## Technology Stack (2026)

| Component | Technology | Version |
|-----------|-----------|---------|
| Base OS | Alpine Linux (virt) | 3.22 |
| Kernel | Linux (stripped virt) | 6.12 |
| Init System | OpenRC | Latest |
| API Language | Go (static binary) | 1.24 |
| Database | SQLite (modernc.org/sqlite, pure Go) | Latest |
| HTTP Router | chi/v5 | 5.2 |
| Video Engine | FFmpeg | 8.0+ |
| GPU Accel | VA-API + NVENC + Vulkan | Latest |
| Video Codec | AV1 (SVT-AV1) / H.264 / HEVC | Latest |
| Streaming | HLS + DASH | Latest |
| Build | mkimage + xorriso | Latest |
| CI/CD | GitHub Actions | Latest |

---

## License

MIT License — Free for commercial use by video hosting companies, data centers, and streaming platforms.
