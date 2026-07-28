# ============================================================================
# MicroOS Makefile
# Build automation for the complete MicroOS video streaming system
# ============================================================================

MICROOS_VERSION := 0.1.0
ALPINE_VERSION := 3.22
GO_VERSION := 1.24
ARCH := x86_64

# Directories
API_DIR := ./api
BUILD_DIR := ./build
OUTPUT_DIR := ./build/output
SCRIPTS_DIR := ./scripts

# Binary names
API_BINARY := microos-server
ISO_NAME := microos-$(MICROOS_VERSION)-$(ARCH).iso

# Go build flags
GO_LDFLAGS := -s -w \
	-X main.Version=$(MICROOS_VERSION) \
	-X main.BuildDate=$(shell date -u +%Y-%m-%dT%H:%M:%SZ) \
	-X main.Commit=$(shell git rev-parse HEAD 2>/dev/null || echo unknown)

.PHONY: all api api-test api-clean iso iso-clean docker clean help

# ============================================================================
# Default target
# ============================================================================
all: api iso

# ============================================================================
# API targets
# ============================================================================
api: ## Build the Go API static binary
	@echo "Building MicroOS API v$(MICROOS_VERSION)..."
	@mkdir -p $(OUTPUT_DIR)
	cd $(API_DIR) && \
		CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -ldflags "$(GO_LDFLAGS)" \
		-o ../$(OUTPUT_DIR)/$(API_BINARY)-amd64 ./cmd/server/
	@echo "API binary: $(OUTPUT_DIR)/$(API_BINARY)-amd64"
	@ls -lh $(OUTPUT_DIR)/$(API_BINARY)-amd64

api-arm64: ## Build the Go API for ARM64
	@echo "Building MicroOS API for ARM64..."
	@mkdir -p $(OUTPUT_DIR)
	cd $(API_DIR) && \
		CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
		go build -ldflags "$(GO_LDFLAGS)" \
		-o ../$(OUTPUT_DIR)/$(API_BINARY)-arm64 ./cmd/server/
	@echo "API binary: $(OUTPUT_DIR)/$(API_BINARY)-arm64"

api-test: ## Run Go API tests
	cd $(API_DIR) && go test -v -short ./...

api-clean: ## Clean API build artifacts
	rm -rf $(OUTPUT_DIR)/$(API_BINARY)*

# ============================================================================
# ISO targets
# ============================================================================
iso: api ## Build the complete MicroOS ISO image
	@echo "Building MicroOS ISO v$(MICROOS_VERSION)..."
	@chmod +x $(SCRIPTS_DIR)/build-iso.sh
	export API_BINARY="$(OUTPUT_DIR)/$(API_BINARY)-amd64" && \
	export MICROOS_VERSION="$(MICROOS_VERSION)" && \
	export ALPINE_VERSION="$(ALPINE_VERSION)" && \
	bash $(SCRIPTS_DIR)/build-iso.sh
	@echo "ISO: $(ISO_NAME)"

iso-clean: ## Clean ISO build artifacts
	rm -rf /tmp/microos-build /tmp/microos-output

# ============================================================================
# Docker targets
# ============================================================================
docker: ## Build Docker image for development/testing
	docker build -t microos:$(MICROOS_VERSION) .
	docker tag microos:$(MICROOS_VERSION) microos:latest

docker-run: ## Run MicroOS in Docker container
	docker run -d --name microos-dev \
		-p 8080:8080 \
		-v /dev/dri:/dev/dri \
		-v microos-data:/var/lib/microos \
		microos:latest

docker-stop: ## Stop MicroOS Docker container
	docker stop microos-dev && docker rm microos-dev

# ============================================================================
# Utility targets
# ============================================================================
clean: api-clean iso-clean ## Clean all build artifacts
	@echo "All build artifacts cleaned"

deps: ## Download Go dependencies
	cd $(API_DIR) && go mod download && go mod verify

fmt: ## Format Go code
	cd $(API_DIR) && go fmt ./...

lint: ## Lint Go code
	cd $(API_DIR) && go vet ./...

size-check: ## Check ISO size against target
	@ISO_FILE="/tmp/microos-output/$(ISO_NAME)"
	@if [ -f "$$ISO_FILE" ]; then \
		ISO_SIZE=$$(stat -c%s "$$ISO_FILE"); \
		ISO_SIZE_MB=$$((ISO_SIZE / 1024 / 1024)); \
		echo "ISO size: $$ISO_SIZE_MB MB"; \
		if [ $$ISO_SIZE_MB -le 200 ]; then \
			echo "✅ Under 200MB target!"; \
		else \
			echo "⚠️ Exceeds 200MB target"; \
		fi; \
	else \
		echo "ISO not found. Build it first with: make iso"; \
	fi

ram-check: ## Estimate RAM usage
	@echo "Estimated MicroOS RAM usage:"
	@echo "  Kernel + initramfs: ~50MB"
	@echo "  OpenRC + services: ~5MB"
	@echo "  MicroOS API: ~10-15MB"
	@echo "  FFmpeg (idle): ~5MB"
	@echo "  GPU libraries: ~10-20MB"
	@echo "  System overhead: ~20MB"
	@echo "  ---"
	@echo "  Total estimated: ~100-110MB (under 200MB target)"

help: ## Show help
	@echo "MicroOS Build System v$(MICROOS_VERSION)"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'
	@echo ""
	@echo "Environment Variables:"
	@echo "  MICROOS_VERSION=$(MICROOS_VERSION)"
	@echo "  ALPINE_VERSION=$(ALPINE_VERSION)"
	@echo "  GO_VERSION=$(GO_VERSION)"
	@echo "  ARCH=$(ARCH)"
