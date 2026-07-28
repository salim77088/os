#!/bin/bash
# ============================================================================
# MicroOS API Testing Script
# Tests all API endpoints against a running MicroOS server
# ============================================================================

set -euo pipefail

API_URL="${API_URL:-http://localhost:8080}"
VIDEO_FILE="${VIDEO_FILE:-}"

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}✓ PASS${NC}: $1"; }
fail() { echo -e "${RED}✗ FAIL${NC}: $1"; }
info() { echo -e "${YELLOW}→ INFO${NC}: $1"; }

echo "=========================================="
echo " MicroOS API Test Suite"
echo " Server: ${API_URL}"
echo "=========================================="
echo ""

# ============================================================================
# Test 1: Health Check
# ============================================================================
test_health() {
    info "Testing health endpoint..."
    response=$(curl -sf "${API_URL}/api/v1/health")
    if [ $? -eq 0 ]; then
        status=$(echo "$response" | grep -o '"status":"[^"]*"' | head -1 | cut -d'"' -f4)
        if [ "$status" = "healthy" ]; then
            pass "Health check: server is healthy"
            echo "$response" | python3 -m json.tool 2>/dev/null || echo "$response"
        else
            fail "Health check: status is '$status'"
        fi
    else
        fail "Health check: server not responding"
    fi
}

# ============================================================================
# Test 2: System Info
# ============================================================================
test_system() {
    info "Testing system info endpoint..."
    response=$(curl -sf "${API_URL}/api/v1/system")
    if [ $? -eq 0 ]; then
        pass "System info endpoint responding"
        echo "$response" | python3 -m json.tool 2>/dev/null || echo "$response"
    else
        fail "System info endpoint not responding"
    fi
}

# ============================================================================
# Test 3: Upload Video (if file provided)
# ============================================================================
test_upload() {
    if [ -z "${VIDEO_FILE}" ]; then
        info "No video file provided. Skipping upload test."
        info "Use: VIDEO_FILE=test.mp4 bash scripts/test-api.sh"
        return
    fi
    
    info "Testing video upload with: ${VIDEO_FILE}"
    response=$(curl -sf -X POST "${API_URL}/api/v1/upload" \
        -F "video=@${VIDEO_FILE}")
    
    if [ $? -eq 0 ]; then
        video_id=$(echo "$response" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
        pass "Video upload: id=${video_id}"
        echo "$response" | python3 -m json.tool 2>/dev/null || echo "$response"
        
        # Test subsequent endpoints with the video ID
        if [ -n "${video_id}" ]; then
            test_video_details "${video_id}"
            test_transcode_status "${video_id}"
        fi
    else
        fail "Video upload failed"
    fi
}

# ============================================================================
# Test 4: Get Video Details
# ============================================================================
test_video_details() {
    video_id="${1:-}"
    if [ -z "${video_id}" ]; then
        info "No video ID provided. Skipping."
        return
    fi
    
    info "Testing video details for: ${video_id}"
    response=$(curl -sf "${API_URL}/api/v1/videos/${video_id}")
    if [ $? -eq 0 ]; then
        pass "Video details endpoint responding"
        echo "$response" | python3 -m json.tool 2>/dev/null || echo "$response"
    else
        fail "Video details endpoint failed"
    fi
}

# ============================================================================
# Test 5: Transcode Status
# ============================================================================
test_transcode_status() {
    video_id="${1:-}"
    if [ -z "${video_id}" ]; then
        info "No video ID. Skipping transcode status test."
        return
    fi
    
    info "Testing transcode status for: ${video_id}"
    response=$(curl -sf "${API_URL}/api/v1/videos/${video_id}/status")
    if [ $? -eq 0 ]; then
        status=$(echo "$response" | grep -o '"status":"[^"]*"' | head -1 | cut -d'"' -f4)
        pass "Transcode status: ${status}"
    else
        fail "Transcode status endpoint failed"
    fi
}

# ============================================================================
# Test 6: List Videos
# ============================================================================
test_list_videos() {
    info "Testing video listing..."
    response=$(curl -sf "${API_URL}/api/v1/videos")
    if [ $? -eq 0 ]; then
        total=$(echo "$response" | grep -o '"total":[0-9]*' | head -1 | cut -d':' -f2)
        pass "Video listing: ${total} videos"
    else
        fail "Video listing endpoint failed"
    fi
}

# ============================================================================
# Run all tests
# ============================================================================
test_health
test_system
test_list_videos
test_upload

echo ""
echo "=========================================="
echo " Tests completed"
echo "=========================================="
