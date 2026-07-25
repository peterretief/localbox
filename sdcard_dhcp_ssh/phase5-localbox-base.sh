#!/bin/sh
set -eu

PI_HOST=${PI_HOST:-192.168.0.7}
PI_USER=${PI_USER:-root}
MOUNTPOINT=${MOUNTPOINT:-/mnt/data}
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/localbox_phase5_known_hosts -o ConnectTimeout=10"}

echo "Phase 5 target: $PI_USER@$PI_HOST"
echo "Goal: LocalBox base directories, packages, templates, lbu persist"

echo ">> Preparing base system on Pi..."
ssh $SSH_OPTS "$PI_USER@$PI_HOST" "MOUNTPOINT='$MOUNTPOINT' sh -s" <<'REMOTE'
set -eu

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

ensure_apk_cache() {
    boot_media=/media/mmcblk0p1
    if [ -d "$boot_media" ]; then
        mount -o remount,rw "$boot_media" 2>/dev/null || true
        mkdir -p "$boot_media/cache"
        if [ ! -L /etc/apk/cache ] || [ "$(readlink /etc/apk/cache 2>/dev/null || true)" != "$boot_media/cache" ]; then
            rm -rf /etc/apk/cache
            ln -s "$boot_media/cache" /etc/apk/cache
        fi
    fi
}

mountpoint -q "$MOUNTPOINT" || {
    echo "ERROR: $MOUNTPOINT is not mounted. Finish Phase 4 first." >&2
    exit 1
}

echo ">> Updating package index..."
ensure_apk_cache
apk update -q

echo ">> Installing base LocalBox packages..."
apk add -q nginx nginx-openrc ca-certificates openssl curl wget
apk cache sync 2>/dev/null || true

echo ">> Creating users and groups..."
addgroup -S mail 2>/dev/null || true
adduser -D -H -s /sbin/nologin www 2>/dev/null || true
adduser www mail 2>/dev/null || true
adduser nginx mail 2>/dev/null || true

echo ">> Creating LocalBox directories..."
mkdir -p \
    /etc/localbox/templates \
    /etc/localbox/examples \
    /var/localbox \
    "$MOUNTPOINT/localbox" \
    "$MOUNTPOINT/localbox/domains" \
    "$MOUNTPOINT/localbox/shared" \
    "$MOUNTPOINT/backups"

touch \
    /etc/localbox/domains.conf \
    /etc/localbox/users.conf \
    /etc/localbox/admins.conf \
    /etc/localbox/box-admins.conf \
    /etc/localbox/password-reset-tokens.conf \
    /var/localbox/contact.log \
    /var/localbox/send.log \
    /var/localbox/security.log

chmod 755 /etc/localbox /etc/localbox/templates /etc/localbox/examples
chown -R www:mail "$MOUNTPOINT/localbox" /var/localbox
chmod -R g+rwX "$MOUNTPOINT/localbox" /var/localbox
find "$MOUNTPOINT/localbox" -type d -exec chmod 2775 {} \;
chown www:mail /etc/localbox/*.conf /etc/localbox/password-reset-tokens.conf 2>/dev/null || true
chmod 640 /etc/localbox/*.conf /etc/localbox/password-reset-tokens.conf 2>/dev/null || true
chmod 664 /var/localbox/*.log

echo ">> Keeping nginx installed but not domain-configured yet..."
rc-update add nginx default 2>/dev/null || true

ensure_lbu_media
REMOTE

echo ">> Copying templates and example configs..."
tmp_remote=/tmp/localbox-phase5-assets
ssh $SSH_OPTS "$PI_USER@$PI_HOST" "rm -rf '$tmp_remote' && mkdir -p '$tmp_remote'"
tar -C "$PROJECT_DIR" -cf - templates \
    localbox-template.conf.example \
    domainscoza.conf.example \
    cloudflare.conf.example \
    cloudflare-tunnel.conf.example \
    paystack.conf.example |
    ssh $SSH_OPTS "$PI_USER@$PI_HOST" "tar -xf - -C '$tmp_remote'"

ssh $SSH_OPTS "$PI_USER@$PI_HOST" "MOUNTPOINT='$MOUNTPOINT' sh -s" <<'REMOTE'
set -eu

tmp_remote=/tmp/localbox-phase5-assets
cp "$tmp_remote"/templates/*.html /etc/localbox/templates/
mkdir -p /etc/localbox/examples
for f in "$tmp_remote"/*.example; do
    [ -f "$f" ] || continue
    cp "$f" /etc/localbox/examples/
done
rm -rf "$tmp_remote"

echo ">> Persisting LocalBox base..."
mount -o remount,rw /media/mmcblk0p1 2>/dev/null || true
lbu add /etc/apk/world
lbu add /etc/group /etc/passwd /etc/shadow 2>/dev/null || true
lbu add /etc/localbox
lbu add /var/localbox
lbu add /etc/init.d/nginx 2>/dev/null || true
lbu add /etc/runlevels/default/nginx 2>/dev/null || true
lbu add /etc/lbu/lbu.conf
lbu add /etc/apk/cache 2>/dev/null || true
lbu commit -d
mount -o remount,ro /media/mmcblk0p1 2>/dev/null || true

echo ">> Phase 5 complete."
hostname
mount | grep " $MOUNTPOINT " || true
ls -la /etc/localbox
ls -la "$MOUNTPOINT"
REMOTE

echo
echo "Phase 5 finished."
