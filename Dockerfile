# ============================================================================
# MicroOS Development Dockerfile
# Used for building and testing the API in a consistent environment
# NOT used for the production ISO (that's built via scripts/build-iso.sh)
# ============================================================================

FROM alpine:3.22 AS builder

# Install build dependencies
RUN apk add --no-cache \
    go \
    git \
    gcc \
    musl-dev \
    zig \
    ffmpeg \
    ffmpeg-dev \
    mesa-dev \
    libva-dev \
    intel-media-driver-dev

# Set working directory
WORKDIR /build

# Copy API source
COPY api/ ./api/

# Build static binary
WORKDIR /build/api
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags "-s -w -X main.Version=dev -X main.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /build/output/microos-server ./cmd/server/

# ============================================================================
# Production stage (for local testing only)
# ============================================================================
FROM alpine:3.22 AS runtime

# Install runtime dependencies only
RUN apk add --no-cache \
    ffmpeg \
    ffmpeg-libs \
    mesa \
    mesa-va-gallium \
    mesa-vulkan \
    libva \
    intel-media-driver \
    ca-certificates \
    curl

# Copy API binary
COPY --from=builder /build/output/microos-server /opt/microos/bin/microos-server

# Create data directories
RUN mkdir -p /var/lib/microos/videos \
             /var/lib/microos/hls \
             /var/lib/microos/dash \
             /var/lib/microos/tmp \
             /var/log/microos

# Copy configuration
COPY build/rootfs/etc/microos/config.toml /etc/microos/config.toml

# Set environment variables
ENV MICROOS_PORT=8080
ENV MICROOS_STORAGE_PATH=/var/lib/microos
ENV MICROOS_DB_PATH=/var/lib/microos/microos.db
ENV MICROOS_GPU=auto
ENV MICROOS_CODEC=av1

# Expose API port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:8080/api/v1/health || exit 1

# Start MicroOS API server
CMD ["/opt/microos/bin/microos-server"]
