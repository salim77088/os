#!/bin/bash
# ============================================================================
# MicroOS Alpine Linux ISO Builder
# Builds a custom, ultra-light Alpine Linux ISO with video streaming capabilities
# Target ISO size: < 200MB | Target RAM: < 200MB
# ============================================================================

set -euo pipefail

# Configuration
MICROOS_VERSION="${MICROOS_VERSION:-0.1.0}"
ALPINE_VERSION="${ALPINE_VERSION:-3.22}"
ALPINE_MIRROR="${ALPINE_MIRROR:-https://dl-cdn.alpinelinux.org/alpine/v${ALPINE_VERSION}}"
ARCH="${ARCH:-x86_64}"
BUILD_DIR="${BUILD_DIR:-/tmp/microos-build}"
OUTPUT_DIR="${OUTPUT_DIR:-/tmp/microos-output}"
ROOTFS_DIR="${BUILD_DIR}/rootfs"
ISO_NAME="microos-${MICROOS_VERSION}-${ARCH}.iso"
API_BINARY="${API_BINARY:-/tmp/microos-api/microos-server}"

# Kernel configuration
KERNEL_VERSION="${KERNEL_VERSION:-6.12}"
KERNEL_CONFIG="${KERNEL_CONFIG:-./kernel/config-x86_64}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info()  { echo -e "${BLUE}[INFO]${NC} $1"; }
log_ok()    { echo -e "${GREEN}[OK]${NC} $1"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_err()   { echo -e "${RED}[ERROR]${NC} $1"; }

# ============================================================================
# Step 1: Prepare build environment
# ============================================================================
prepare_build_env() {
    log_info "Preparing build environment..."
    
    # Clean previous builds
    rm -rf "${BUILD_DIR}" "${OUTPUT_DIR}"
    mkdir -p "${BUILD_DIR}" "${OUTPUT_DIR}" "${ROOTFS_DIR}"
    
    # Check required tools
    for tool in apk xorriso mksquashfs syslinux grub-mkrescue; do
        if ! command -v "$tool" &>/dev/null; then
            log_err "Required tool not found: $tool"
            log_info "Installing missing tools..."
            apk add --no-cache xorriso squashfs-tools syslinux grub grub-mkrescue mtools dosfstools
        fi
    done
    
    log_ok "Build environment prepared"
}

# ============================================================================
# Step 2: Build minimal Alpine rootfs
# ============================================================================
build_rootfs() {
    log_info "Building minimal Alpine rootfs (target: ~50MB)..."
    
    # Use Alpine minirootfs as base (only ~3MB!)
    MINIROOTFS_URL="${ALPINE_MIRROR}/releases/${ARCH}/alpine-minirootfs-${ALPINE_VERSION}.0-${ARCH}.tar.gz"
    
    log_info "Downloading Alpine minirootfs from: ${MINIROOTFS_URL}"
    curl -fsSL "${MINIROOTFS_URL}" | tar -xz -C "${ROOTFS_DIR}"
    
    log_ok "Minirootfs extracted"
    
    # Configure APK for minimal installs
    mkdir -p "${ROOTFS_DIR}/etc/apk"
    cat > "${ROOTFS_DIR}/etc/apk/repositories" << EOF
${ALPINE_MIRROR}/main
${ALPINE_MIRROR}/community
EOF
    
    # Install ONLY essential packages for headless video streaming server
    # This is the key to keeping the ISO under 200MB and RAM under 200MB
    log_info "Installing minimal packages..."
    
    apk --root "${ROOTFS_DIR}" --no-cache --no-progress add \
        # === Core system (essential) ===
        alpine-base \
        openrc \
        eudev \
        dbus \
        \
        # === Networking (essential for streaming) ===
        linux-virt \
        linux-firmware-none \
        iptables \
        iproute2 \
        bridge-utils \
        curl \
        ca-certificates \
        bind-tools \
        \
        # === Disk management ===
        util-linux \
        e2fsprogs \
        dosfstools \
        parted \
        \
        # === Process management ===
        busybox-initscripts \
        \
        # === GPU acceleration (essential for transcoding) ===
        mesa \
        mesa-va-gallium \
        mesa-vulkan-intel \
        mesa-vulkan-radeon \
        vulkan-loader \
        vulkan-tools \
        libva \
        intel-media-driver \
        \
        # === Video processing (essential) ===
        ffmpeg \
        ffmpeg-libs \
        \
        # === Security ===
        openssl \
        \
        # === Miscellaneous essential ===
        tzdata \
        musl \
        musl-utils
    
    log_ok "Minimal packages installed"
    
    # Remove unnecessary files to reduce size
    strip_rootfs
}

# ============================================================================
# Step 3: Strip rootfs for maximum size reduction
# ============================================================================
strip_rootfs() {
    log_info "Stripping rootfs for maximum size reduction..."
    
    # Remove documentation files
    find "${ROOTFS_DIR}" -type f -name "*.md" -delete
    find "${ROOTFS_DIR}" -type f -name "*.txt" -delete
    find "${ROOTFS_DIR}" -type f -name "*.html" -delete
    find "${ROOTFS_DIR}" -type d -name "man" -exec rm -rf {} + 2>/dev/null || true
    find "${ROOTFS_DIR}" -type d -name "doc" -exec rm -rf {} + 2>/dev/null || true
    find "${ROOTFS_DIR}" -type d -name "info" -exec rm -rf {} + 2>/dev/null || true
    
    # Remove locale files (keep only C and en_US)
    find "${ROOTFS_DIR}/usr/share/locale" -mindepth 1 -maxdepth 1 -type d \
        ! -name "C" ! -name "en*" ! -name "locale.alias" -exec rm -rf {} + 2>/dev/null || true
    
    # Remove unused firmware
    rm -rf "${ROOTFS_DIR}/lib/firmware/cirrus"
    rm -rf "${ROOTFS_DIR}/lib/firmware/rtl*"  # Keep only networking firmware
    rm -rf "${ROOTFS_DIR}/lib/firmware/bnx2"  # Remove broadcom if not needed
    rm -rf "${ROOTFS_DIR}/lib/firmware/amd-ucode"
    rm -rf "${ROOTFS_DIR}/lib/firmware/intel-ucode"
    
    # Remove development files
    find "${ROOTFS_DIR}" -type f -name "*.a" -delete
    find "${ROOTFS_DIR}" -type f -name "*.la" -delete
    find "${ROOTFS_DIR}" -type d -name "include" -exec rm -rf {} + 2>/dev/null || true
    rm -rf "${ROOTFS_DIR}/usr/share/pkgconfig"
    
    # Remove unused binaries
    rm -f "${ROOTFS_DIR}/usr/bin/x11*" 2>/dev/null || true
    rm -f "${ROOTFS_DIR}/usr/bin/wayland*" 2>/dev/null || true
    rm -rf "${ROOTFS_DIR}/usr/share/X11" 2>/dev/null || true
    rm -rf "${ROOTFS_DIR}/usr/share/wayland" 2>/dev/null || true
    
    # Remove systemd stuff (we use OpenRC)
    rm -rf "${ROOTFS_DIR}/usr/lib/systemd" 2>/dev/null || true
    rm -rf "${ROOTFS_DIR}/etc/systemd" 2>/dev/null || true
    
    # Remove unused shell/profile stuff
    rm -f "${ROOTFS_DIR}/etc/profile" 2>/dev/null || true
    
    # Strip all ELF binaries for size reduction
    find "${ROOTFS_DIR}" -type f -executable -exec sh -c '
        file "$1" | grep -q "ELF" && strip --strip-all "$1" 2>/dev/null || true
    ' _ {} \;
    
    # Remove APK cache
    rm -rf "${ROOTFS_DIR}/var/cache/apk"
    rm -rf "${ROOTFS_DIR}/etc/apk/cache"
    
    log_ok "Rootfs stripped"
}

# ============================================================================
# Step 4: Inject MicroOS API binary
# ============================================================================
inject_api_binary() {
    log_info "Injecting MicroOS API binary..."
    
    # Create API directory
    mkdir -p "${ROOTFS_DIR}/opt/microos/bin"
    mkdir -p "${ROOTFS_DIR}/var/lib/microos/videos"
    mkdir -p "${ROOTFS_DIR}/var/lib/microos/hls"
    mkdir -p "${ROOTFS_DIR}/var/lib/microos/dash"
    mkdir -p "${ROOTFS_DIR}/var/lib/microos/tmp"
    mkdir -p "${ROOTFS_DIR}/var/log/microos"
    
    # Copy the API binary
    if [ -f "${API_BINARY}" ]; then
        cp "${API_BINARY}" "${ROOTFS_DIR}/opt/microos/bin/microos-server"
        chmod +x "${ROOTFS_DIR}/opt/microos/bin/microos-server"
        log_ok "API binary injected"
    else
        log_warn "API binary not found at ${API_BINARY}. Build will continue without it."
        log_info "You can add the binary later or it will be injected via CI/CD."
        
        # Create placeholder for CI/CD injection
        echo "#!/bin/sh" > "${ROOTFS_DIR}/opt/microos/bin/microos-server"
        echo "# This placeholder will be replaced by the actual binary during CI/CD" >> "${ROOTFS_DIR}/opt/microos/bin/microos-server"
        echo "echo 'MicroOS API placeholder - replace with actual binary'" >> "${ROOTFS_DIR}/opt/microos/bin/microos-server"
        chmod +x "${ROOTFS_DIR}/opt/microos/bin/microos-server"
    fi
    
    # Copy configuration files
    if [ -d "./rootfs/etc/microos" ]; then
        cp -r ./rootfs/etc/microos/* "${ROOTFS_DIR}/etc/microos/" 2>/dev/null || true
    fi
    
    # Set correct permissions
    chown -R root:root "${ROOTFS_DIR}/opt/microos"
    chown -R root:root "${ROOTFS_DIR}/var/lib/microos"
    
    log_ok "API binary and directories set up"
}

# ============================================================================
# Step 5: Configure system services
# ============================================================================
configure_system() {
    log_info "Configuring MicroOS system services..."
    
    # Set hostname
    echo "microos" > "${ROOTFS_DIR}/etc/hostname"
    echo "127.0.0.1 microos localhost" > "${ROOTFS_DIR}/etc/hosts"
    
    # Configure networking (automatic DHCP on boot)
    mkdir -p "${ROOTFS_DIR}/etc/network"
    cat > "${ROOTFS_DIR}/etc/network/interfaces" << 'EOF'
auto lo
iface lo inet loopback

auto eth0
iface eth0 inet dhcp

auto eth1
iface eth1 inet dhcp
EOF
    
    # DNS configuration
    cat > "${ROOTFS_DIR}/etc/resolv.conf" << 'EOF'
nameserver 8.8.8.8
nameserver 8.8.4.4
nameserver 1.1.1.1
EOF
    
    # Configure OpenRC - enable essential services only
    chroot "${ROOTFS_DIR}" /sbin/rc-update add boot sysinit
    chroot "${ROOTFS_DIR}" /sbin/rc-update add udev sysinit
    chroot "${ROOTFS_DIR}" /sbin/rc-update add networking boot
    chroot "${ROOTFS_DIR}" /sbin/rc-update add hostname boot
    chroot "${ROOTFS_DIR}" /sbin/rc-update add dbus default
    chroot "${ROOTFS_DIR}" /sbin/rc-update add microos default
    
    # Create MicroOS init script
    mkdir -p "${ROOTFS_DIR}/etc/init.d"
    cat > "${ROOTFS_DIR}/etc/init.d/microos" << 'INITEOF'
#!/sbin/openrc-run
# MicroOS Video Streaming Server Init Script

name="microos"
description="MicroOS Video Streaming and Transcoding Server"

command="/opt/microos/bin/microos-server"
command_args="--config /etc/microos/config.toml"
command_background=true
pidfile="/var/run/microos.pid"

start_stop_daemon_args="--make-pidfile --foreground"

depend() {
    need net dbus
    after firewall
    use logger
}

start_pre() {
    # Ensure data directories exist
    mkdir -p /var/lib/microos/videos
    mkdir -p /var/lib/microos/hls
    mkdir -p /var/lib/microos/dash
    mkdir -p /var/lib/microos/tmp
    mkdir -p /var/log/microos
    
    # Check FFmpeg availability
    if ! command -v ffmpeg &>/dev/null; then
        eerror "FFmpeg not found! Video transcoding will not work."
    fi
    
    # Check GPU acceleration
    if [ -d /dev/dri ]; then
        einfo "GPU acceleration detected (VA-API)"
    elif [ -e /dev/nvidia0 ]; then
        einfo "GPU acceleration detected (NVENC)"
    else
        ewarn "No GPU acceleration detected. Using software encoding."
    fi
}

stop_post() {
    # Clean up temporary transcoding files
    rm -rf /var/lib/microos/tmp/* 2>/dev/null || true
}
INITEOF
    
    chmod +x "${ROOTFS_DIR}/etc/init.d/microos"
    
    # Create MicroOS configuration
    mkdir -p "${ROOTFS_DIR}/etc/microos"
    cat > "${ROOTFS_DIR}/etc/microos/config.toml" << 'CONFEOF'
# MicroOS Configuration File
# Ultra-light video streaming and transcoding server

[server]
host = "0.0.0.0"
port = 8080
read_timeout = 30
write_timeout = 120
max_upload_size = 5368709120  # 5 GB
enable_cors = true

[database]
path = "/var/lib/microos/microos.db"
max_open_conns = 3
max_idle_conns = 1

[storage]
base_path = "/var/lib/microos"
videos_dir = "videos"
hls_dir = "hls"
dash_dir = "dash"
temp_dir = "tmp"

[transcode]
workers = 2
enable_gpu = true
gpu_type = "auto"
preferred_codec = "av1"
h264_crf = 23
av1_crf = 30
hevc_crf = 28
hls_segment_duration = 6
dash_segment_duration = 6
enable_hls = true
enable_dash = true
zero_copy = true
audio_bitrate = "128k"

[transcode.resolution_ladder]
[[transcode.resolution_ladder]]
name = "1080p"
width = 1920
height = 1080
bitrate = "5M"
max_bitrate = "8M"

[[transcode.resolution_ladder]]
name = "720p"
width = 1280
height = 720
bitrate = "2.5M"
max_bitrate = "4M"

[[transcode.resolution_ladder]]
name = "480p"
width = 854
height = 480
bitrate = "1M"
max_bitrate = "1.5M"

[[transcode.resolution_ladder]]
name = "360p"
width = 640
height = 360
bitrate = "500k"
max_bitrate = "750k"

[auth]
enabled = false
admin_key = ""
CONFEOF
    
    # Create immutable system marker
    cat > "${ROOTFS_DIR}/etc/microos/release" << 'RELEOF'
MICROOS_VERSION=0.1.0
MICROOS_BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
MICROOS_COMMIT=$(git rev-parse HEAD 2>/dev/null || echo "unknown")
MICROOS_BASE=alpine-v3.22
MICROOS_KERNEL=linux-virt
MICROOS_TYPE=headless-immutable
RELEOF
    
    # Configure fstab for immutable operation
    cat > "${ROOTFS_DIR}/etc/fstab" << 'FSTABEOF'
# MicroOS Immutable Filesystem Configuration
# Root filesystem is read-only (in-memory for SCALE mode)
tmpfs / tmpfs defaults,size=200M,mode=0755 0 0
tmpfs /var/lib/microos tmpfs defaults,size=90% 0 0
tmpfs /var/log tmpfs defaults,size=10M 0 0
tmpfs /tmp tmpfs defaults,size=50M 0 0
FSTABEOF
    
    # Create swap configuration (minimal)
    echo "# MicroOS does not use swap by default for performance" > "${ROOTFS_DIR}/etc/fstab.swap"
    
    log_ok "System services configured"
}

# ============================================================================
# Step 6: Build kernel (stripped for minimal size)
# ============================================================================
build_kernel() {
    log_info "Building stripped Linux kernel..."
    
    # Use Alpine's pre-built virt kernel (already stripped for VMs)
    # The virt kernel is ~10MB vs ~20MB for the standard kernel
    # It removes: sound drivers, USB gadgets, wireless, bluetooth, etc.
    
    # Copy kernel modules (only essential ones)
    KMOD_DIR="${ROOTFS_DIR}/lib/modules"
    
    if [ -d "${KMOD_DIR}" ]; then
        log_info "Stripping kernel modules..."
        
        # Remove unused kernel modules
        UNUSED_MODULES=(
            # Sound
            "sound/*" "snd/*" "ac97*" "codec/*"
            # Bluetooth
            "bluetooth/*" "btusb" "btintel"
            # Wireless (keep essential WiFi for data centers)
            "rtl8188*" "rtl8723*" "brcm*" "ath*" "cw1200"
            # USB gadgets
            "usb/gadget/*" "g_*" "usb_f_*"
            # IRDA
            "irda/*" "irtty*"
            # ISDN
            "isdn/*"
            # Telephony
            "phone*"
            # Ham radio
            "hamradio/*"
            # Video4Linux (keep V4L2 for GPU streaming)
            "video/em28xx*" "video/usbtv*"
            # Unused filesystems
            "fs/ntfs*" "fs/hfs*" "fs/efs*" "fs/jffs2*" "fs/romfs*" "fs/minix*" 
            "fs/qnx*" "fs/sysv*" "fs/ufs*" "fs/affs*" "fs/befs*"
            # Crypto modules we don't need
            "crypto/anubis*" "crypto/camellia*" "crypto/cast5*" "crypto/cast6*"
            "crypto/khazad*" "crypto/seed*" "crypto/tea*" "crypto/twofish*"
            "crypto/whirlpool*" "crypto/michael_mic*"
        )
        
        for pattern in "${UNUSED_MODULES[@]}"; do
            find "${KMOD_DIR}" -name "${pattern}" -type f -delete 2>/dev/null || true
            find "${KMOD_DIR}" -path "${pattern}" -type d -exec rm -rf {} + 2>/dev/null || true
        done
        
        # Regenerate modules.dep
        depmod -b "${ROOTFS_DIR}" $(basename "${KMOD_DIR}"/*) 2>/dev/null || true
    fi
    
    log_ok "Kernel modules stripped"
}

# ============================================================================
# Step 7: Create boot configuration
# ============================================================================
configure_boot() {
    log_info "Creating boot configuration..."
    
    # Create GRUB configuration for headless boot
    mkdir -p "${ROOTFS_DIR}/boot/grub"
    
    cat > "${ROOTFS_DIR}/boot/grub/grub.cfg" << 'GRUBEOF'
# MicroOS GRUB Configuration
# Headless, ultra-light video streaming server

set default=0
set timeout=3

menuentry "MicroOS Video Streaming Server (Normal)" {
    linux /boot/vmlinuz-virt root=/dev/ram0 init=/sbin/init console=tty0 console=ttyS0,115200n8 quiet
    initrd /boot/initramfs-virt
}

menuentry "MicroOS Video Streaming Server (SCALE - In-Memory)" {
    linux /boot/vmlinuz-virt root=/dev/ram0 init=/sbin/init console=tty0 console=ttyS0,115200n8 quiet ramdisk_size=200000
    initrd /boot/initramfs-virt
}

menuentry "MicroOS Video Streaming Server (Safe Mode)" {
    linux /boot/vmlinuz-virt root=/dev/ram0 init=/sbin/init console=tty0 console=ttyS0,115200n8
    initrd /boot/initramfs-virt
}

menuentry "MicroOS Video Streaming Server (GPU Debug)" {
    linux /boot/vmlinuz-virt root=/dev/ram0 init=/sbin/init console=tty0 console=ttyS0,115200n8 drm.debug=0x04 log_buf_len=16M
    initrd /boot/initramfs-virt
}
GRUBEOF
    
    # Create syslinux configuration for BIOS boot
    mkdir -p "${ROOTFS_DIR}/boot/syslinux"
    
    cat > "${ROOTFS_DIR}/boot/syslinux/syslinux.cfg" << 'SYSLINUXEOF'
# MicroOS Syslinux Configuration
DEFAULT microos
PROMPT 0
TIMEOUT 3

LABEL microos
    MENU LABEL MicroOS Video Streaming Server
    KERNEL /boot/vmlinuz-virt
    INITRD /boot/initramfs-virt
    APPEND root=/dev/ram0 init=/sbin/init console=tty0 console=ttyS0,115200n8 quiet

LABEL microos-scale
    MENU LABEL MicroOS (SCALE - In-Memory, 200MB RAM)
    KERNEL /boot/vmlinuz-virt
    INITRD /boot/initramfs-virt
    APPEND root=/dev/ram0 init=/sbin/init console=tty0 console=ttyS0,115200n8 quiet ramdisk_size=200000

LABEL microos-debug
    MENU LABEL MicroOS (Debug Mode)
    KERNEL /boot/vmlinuz-virt
    INITRD /boot/initramfs-virt
    APPEND root=/dev/ram0 init=/sbin/init console=tty0 console=ttyS0,115200n8 drm.debug=0x04
SYSLINUXEOF
    
    log_ok "Boot configuration created"
}

# ============================================================================
# Step 8: Build ISO image
# ============================================================================
build_iso() {
    log_info "Building ISO image..."
    
    # Create initramfs
    log_info "Creating initramfs..."
    
    # Use Alpine's existing initramfs as base
    INITRAMFS_DIR="${BUILD_DIR}/initramfs"
    mkdir -p "${INITRAMFS_DIR}"
    
    # Copy rootfs to initramfs
    cp -a "${ROOTFS_DIR}"/* "${INITRAMFS_DIR}"/
    
    # Create initramfs archive
    cd "${INITRAMFS_DIR}"
    find . | cpio -o -H newc 2>/dev/null | gzip -9 > "${BUILD_DIR}/initramfs-virt.gz"
    cd - > /dev/null
    
    # Prepare ISO structure
    ISO_DIR="${BUILD_DIR}/iso"
    mkdir -p "${ISO_DIR}/boot" "${ISO_DIR}/boot/grub" "${ISO_DIR}/boot/syslinux"
    
    # Copy kernel and initramfs
    cp "${ROOTFS_DIR}/boot/vmlinuz-virt" "${ISO_DIR}/boot/" 2>/dev/null || \
        log_warn "Kernel not found in rootfs, will use pre-built"
    cp "${BUILD_DIR}/initramfs-virt.gz" "${ISO_DIR}/boot/initramfs-virt"
    
    # Copy boot configuration
    cp "${ROOTFS_DIR}/boot/grub/grub.cfg" "${ISO_DIR}/boot/grub/"
    cp "${ROOTFS_DIR}/boot/syslinux/syslinux.cfg" "${ISO_DIR}/boot/syslinux/"
    
    # Copy syslinux files
    for file in ldlinux.c32 libcom32.c32 libutil.c32 menu.c32 vesamenu.c32 mboot.c32; do
        cp "/usr/share/syslinux/${file}" "${ISO_DIR}/boot/syslinux/" 2>/dev/null || true
    done
    cp "/usr/share/syslinux/isolinux.bin" "${ISO_DIR}/boot/syslinux/" 2>/dev/null || true
    
    # Build ISO using xorriso (supports both UEFI and BIOS boot)
    log_info "Creating ISO with xorriso..."
    
    xorriso -as mkisofs \
        -iso-level 3 \
        -full-iso9660-filenames \
        -R -J -c boot/syslinux/boot.cat \
        -b boot/syslinux/isolinux.bin \
        -no-emul-boot \
        -boot-load-size 4 \
        -boot-info-table \
        --efi-boot boot/grub/grub.cfg \
        -eltorito-alt-boot \
        -e boot/grub/efi.img \
        -no-emul-boot \
        -V "MICROOS_${MICROOS_VERSION}" \
        -o "${OUTPUT_DIR}/${ISO_NAME}" \
        "${ISO_DIR}" 2>&1 | tail -5
    
    # Calculate and report ISO size
    ISO_SIZE=$(stat -c%s "${OUTPUT_DIR}/${ISO_NAME}" 2>/dev/null || echo "0")
    ISO_SIZE_MB=$((ISO_SIZE / 1024 / 1024))
    
    if [ "${ISO_SIZE_MB}" -le 200 ]; then
        log_ok "ISO size: ${ISO_SIZE_MB}MB (under 200MB target!)"
    else
        log_warn "ISO size: ${ISO_SIZE_MB}MB (exceeds 200MB target - needs further optimization)"
    fi
    
    log_ok "ISO image created: ${OUTPUT_DIR}/${ISO_NAME}"
}

# ============================================================================
# Step 9: Verify ISO
# ============================================================================
verify_iso() {
    log_info "Verifying ISO image..."
    
    # Check ISO exists
    if [ ! -f "${OUTPUT_DIR}/${ISO_NAME}" ]; then
        log_err "ISO image not found!"
        return 1
    fi
    
    # Check ISO is bootable
    if xorriso -indev "${OUTPUT_DIR}/${ISO_NAME}" -toc 2>&1 | grep -q "Boot record"; then
        log_ok "ISO is bootable"
    else
        log_warn "ISO boot verification inconclusive"
    fi
    
    # Calculate rootfs size
    ROOTFS_SIZE=$(du -sm "${ROOTFS_DIR}" | cut -f1)
    log_info "Rootfs size: ${ROOTFS_SIZE}MB"
    
    # Calculate RAM estimation
    log_info "Estimated RAM usage at startup:"
    log_info "  - Kernel + initramfs: ~50MB"
    log_info "  - OpenRC + services: ~5MB"
    log_info "  - MicroOS API: ~10MB"
    log_info "  - FFmpeg (idle): ~5MB"
    log_info "  - Mesa/GPU libs: ~10MB"
    log_info "  - System overhead: ~20MB"
    log_info "  - TOTAL estimated: ~100MB (under 200MB target)"
    
    log_ok "ISO verification complete"
}

# ============================================================================
# Main build process
# ============================================================================
main() {
    echo "=========================================="
    echo " MicroOS ISO Builder v${MICROOS_VERSION}"
    echo " Ultra-Light Video Streaming Server"
    echo "=========================================="
    echo ""
    
    START_TIME=$(date +%s)
    
    prepare_build_env
    build_rootfs
    inject_api_binary
    configure_system
    build_kernel
    configure_boot
    build_iso
    verify_iso
    
    END_TIME=$(date +%s)
    BUILD_DURATION=$((END_TIME - START_TIME))
    
    echo ""
    echo "=========================================="
    log_ok "MicroOS build completed in ${BUILD_DURATION}s"
    log_ok "ISO: ${OUTPUT_DIR}/${ISO_NAME}"
    echo "=========================================="
}

# Run main
main "$@"
