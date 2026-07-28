#!/bin/bash
# ============================================================================
# MicroOS API Binary Builder
# Compiles the Go backend API into a static binary for Alpine Linux
# Target: musl-based static binary, CGO_ENABLED=0 for maximum portability
# ============================================================================

set -euo pipefail

# Configuration
MICROOS_VERSION="${MICROOS_VERSION:-0.1.0}"
API_DIR="${API_DIR:-./api}"
OUTPUT_DIR="${OUTPUT_DIR:-./build/output}"
BINARY_NAME="microos-server"
ARCH="${ARCH:-x86_64}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info()  { echo -e "${BLUE}[INFO]${NC} $1"; }
log_ok()    { echo -e "${GREEN}[OK]${NC} $1"; }
log_err()   { echo -e "${RED}[ERROR]${NC} $1"; }

# ============================================================================
# Step 1: Setup Go environment
# ============================================================================
setup_go() {
    log_info "Setting up Go environment..."
    
    # Check if Go is installed
    if command -v go &>/dev/null; then
        GO_VERSION=$(go version | grep -oP 'go\K[0-9]+\.[0-9]+')
        log_info "Go ${GO_VERSION} found"
    else
        log_err "Go not found. Please install Go 1.24+"
        exit 1
    fi
    
    # Ensure minimum Go version (1.24)
    if [[ "${GO_VERSION}" < "1.24" ]]; then
        log_err "Go version ${GO_VERSION} is too old. Need 1.24+"
        exit 1
    fi
    
    log_ok "Go environment ready"
}

# ============================================================================
# Step 2: Download dependencies
# ============================================================================
download_deps() {
    log_info "Downloading Go dependencies..."
    
    cd "${API_DIR}"
    
    # Download all dependencies
    go mod download
    
    # Verify dependencies
    go mod verify
    
    log_ok "Dependencies downloaded and verified"
}

# ============================================================================
# Step 3: Build static binary
# ============================================================================
build_binary() {
    log_info "Building static binary..."
    
    cd "${API_DIR}"
    
    # Build parameters
    BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    COMMIT_HASH=$(git rev-parse HEAD 2>/dev/null || echo "unknown")
    
    mkdir -p "${OUTPUT_DIR}"
    
    # Build with CGO_ENABLED=0 for static binary (no C dependencies)
    # This produces a fully static binary that runs on any Linux system
    # Including Alpine Linux with musl libc
    log_info "Compiling with CGO_ENABLED=0 (fully static binary)..."
    
    GOOS=linux \
    GOARCH=amd64 \
    CGO_ENABLED=0 \
    go build \
        -ldflags "-s -w -X main.Version=${MICROOS_VERSION} -X main.BuildDate=${BUILD_TIME} -X main.Commit=${COMMIT_HASH}" \
        -o "${OUTPUT_DIR}/${BINARY_NAME}" \
        ./cmd/server/
    
    # Verify binary
    if [ -f "${OUTPUT_DIR}/${BINARY_NAME}" ]; then
        BINARY_SIZE=$(stat -c%s "${OUTPUT_DIR}/${BINARY_NAME}")
        BINARY_SIZE_MB=$((BINARY_SIZE / 1024 / 1024))
        log_ok "Binary created: ${OUTPUT_DIR}/${BINARY_NAME} (${BINARY_SIZE_MB}MB)"
        
        # Verify it's a static binary
        if file "${OUTPUT_DIR}/${BINARY_NAME}" | grep -q "statically linked"; then
            log_ok "Binary is statically linked"
        else
            log_warn "Binary may not be fully static - verify with 'ldd'"
        fi
    else
        log_err "Binary build failed"
        exit 1
    fi
}

# ============================================================================
# Step 4: Build with musl (for Alpine compatibility, if CGO needed)
# ============================================================================
build_musl_binary() {
    log_info "Building musl-based static binary (for CGO dependencies like SQLite)..."
    
    cd "${API_DIR}"
    
    BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    COMMIT_HASH=$(git rev-parse HEAD 2>/dev/null || echo "unknown")
    
    # Use Zig as C compiler for musl cross-compilation
    # This allows us to create static binaries even with CGO
    if command -v zig &>/dev/null; then
        log_info "Using Zig for musl cross-compilation..."
        
        CC="zig cc -target x86_64-linux-musl" \
        CGO_ENABLED=1 \
        GOOS=linux \
        GOARCH=amd64 \
        go build \
            -ldflags "-s -w -X main.Version=${MICROOS_VERSION} -X main.BuildDate=${BUILD_TIME} -X main.Commit=${COMMIT_HASH} -extldflags '-static'" \
            -o "${OUTPUT_DIR}/${BINARY_NAME}-musl" \
            ./cmd/server/
        
        log_ok "Musl binary created"
    else
        log_warn "Zig not found. Musl build skipped."
        log_info "Install Zig for musl cross-compilation: https://ziglang.org/"
    fi
}

# ============================================================================
# Step 5: Test binary
# ============================================================================
test_binary() {
    log_info "Testing binary..."
    
    cd "${API_DIR}"
    
    # Run Go tests
    go test -v -short ./...
    
    log_ok "Tests passed"
}

# ============================================================================
# Main build process
# ============================================================================
main() {
    echo "=========================================="
    echo " MicroOS API Builder v${MICROOS_VERSION}"
    echo " Static Binary for Alpine Linux"
    echo "=========================================="
    echo ""
    
    setup_go
    download_deps
    build_binary
    build_musl_binary
    test_binary
    
    echo ""
    echo "=========================================="
    log_ok "API build completed"
    log_ok "Binary: ${OUTPUT_DIR}/${BINARY_NAME}"
    echo "=========================================="
}

main "$@"
