#!/bin/sh
set -eu

PI_HOST=${PI_HOST:-${1:-192.168.0.7}}
PI_USER=${PI_USER:-root}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/localbox_phase3_known_hosts -o ConnectTimeout=10"}
REMOTE_UTC=${REMOTE_UTC:-$(date -u '+%Y-%m-%d %H:%M:%S')}

echo "Phase 3 target: $PI_USER@$PI_HOST"
echo "Goal: package trust, repos, chrony, base utilities, lbu persist"

ssh $SSH_OPTS "$PI_USER@$PI_HOST" "REMOTE_UTC='$REMOTE_UTC' sh -s" <<'REMOTE'
set -eu

find_apk_cache() {
    for dir in /media/mmcblk0p1/apks /boot/apks /media/*/apks; do
        if [ -d "$dir/armv7" ]; then
            printf '%s\n' "$dir"
            return 0
        fi
    done
    return 1
}

ensure_lbu_media() {
    mkdir -p /etc/lbu
    [ -f /etc/lbu/lbu.conf ] || touch /etc/lbu/lbu.conf
    if grep -q '^# LBU_MEDIA=' /etc/lbu/lbu.conf; then
        sed -i 's/^# LBU_MEDIA=.*/LBU_MEDIA=mmcblk0p1/' /etc/lbu/lbu.conf
    elif grep -q '^LBU_MEDIA=' /etc/lbu/lbu.conf; then
        sed -i 's/^LBU_MEDIA=.*/LBU_MEDIA=mmcblk0p1/' /etc/lbu/lbu.conf
    else
        printf '%s\n' 'LBU_MEDIA=mmcblk0p1' >> /etc/lbu/lbu.conf
    fi
}

install_local_apk_if_present() {
    cache=$1
    shift
    for pattern in "$@"; do
        set -- "$cache"/armv7/$pattern
        [ -f "$1" ] || continue
        apk add --force-non-repository "$@"
    done
}

echo ">> Finding boot APK cache..."
APK_CACHE=$(find_apk_cache) || {
    echo "ERROR: could not find boot APK cache under /media or /boot" >&2
    exit 1
}
echo "   $APK_CACHE"

echo ">> Installing certificate and time packages from local cache where available..."
install_local_apk_if_present "$APK_CACHE" \
    'ca-certificates-bundle-*.apk' \
    'ssl_client-*.apk' \
    'zlib-*.apk' \
    'libcrypto3-*.apk' \
    'libssl3-*.apk' \
    'libunistring-*.apk' \
    'libidn2-*.apk' \
    'pcre2-*.apk' \
    'libseccomp-*.apk' \
    'chrony-common-*.apk' \
    'chrony-openrc-*.apk' \
    'chrony-*.apk' \
    'openssl-*.apk' \
    'wget-*.apk'

echo ">> Setting clock from development machine..."
date -u -s "$REMOTE_UTC"

echo ">> Configuring Alpine repositories..."
cat > /etc/apk/repositories <<'EOF'
https://dl-cdn.alpinelinux.org/alpine/v3.24/main
https://dl-cdn.alpinelinux.org/alpine/v3.24/community
EOF

echo ">> Updating certificate store..."
if command -v update-ca-certificates >/dev/null 2>&1; then
    update-ca-certificates 2>/dev/null || true
fi

echo ">> Verifying apk update over HTTPS..."
if ! apk update -q; then
    echo ">> HTTPS apk update still failed; bootstrapping ca-certificates over HTTP once..."
    cat > /etc/apk/repositories <<'EOF'
http://dl-cdn.alpinelinux.org/alpine/v3.24/main
http://dl-cdn.alpinelinux.org/alpine/v3.24/community
EOF
    apk update -q
    apk add -q ca-certificates ca-certificates-bundle openssl wget
    update-ca-certificates 2>/dev/null || true
    cat > /etc/apk/repositories <<'EOF'
https://dl-cdn.alpinelinux.org/alpine/v3.24/main
https://dl-cdn.alpinelinux.org/alpine/v3.24/community
EOF
    apk update -q
fi

echo ">> Installing base utilities..."
apk add -q ca-certificates chrony chrony-openrc wget openssl

echo ">> Enabling clock sync..."
rc-update add chronyd default 2>/dev/null || true
rc-service chronyd restart 2>/dev/null || rc-service chronyd start 2>/dev/null || true
chronyd -q 2>/dev/null || true

echo ">> Persisting diskless Alpine overlay..."
ensure_lbu_media
mount -o remount,rw /media/mmcblk0p1 2>/dev/null || true
lbu add /etc/apk/repositories
lbu add /etc/ssl 2>/dev/null || true
lbu add /etc/chrony 2>/dev/null || true
lbu add /etc/conf.d/chronyd 2>/dev/null || true
lbu add /etc/runlevels/default/chronyd 2>/dev/null || true
lbu add /etc/network/interfaces 2>/dev/null || true
lbu add /etc/runlevels/default/networking 2>/dev/null || true
lbu add /etc/ssh 2>/dev/null || true
lbu add /etc/ssh/ssh_host_*_key /etc/ssh/ssh_host_*_key.pub 2>/dev/null || true
lbu add /root/.ssh 2>/dev/null || true
lbu add /etc/runlevels/default/sshd 2>/dev/null || true
lbu add /etc/apk/world
lbu add /etc/lbu/lbu.conf
lbu commit -d
mount -o remount,ro /media/mmcblk0p1 2>/dev/null || true

echo ">> Phase 3 complete."
hostname
date
rc-service sshd status || true
rc-service chronyd status || true
apk update -q
REMOTE

echo
echo "Phase 3 finished."
