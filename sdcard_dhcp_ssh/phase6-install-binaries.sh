#!/bin/sh
set -eu

PI_HOST=${PI_HOST:-192.168.0.7}
PI_USER=${PI_USER:-root}
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
BUILD_DIR=${BUILD_DIR:-"$PROJECT_DIR/build/phase6-armv7"}
LOCALBOX_BIN_DIR=${LOCALBOX_BIN_DIR:-/mnt/data/localbox/bin}
SYNC_MAILMAPS_SOURCE=${SYNC_MAILMAPS_SOURCE:-"$PROJECT_DIR/scripts/localbox-sync-mailmaps.sh"}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/localbox_phase6_known_hosts -o ConnectTimeout=10"}

echo "Phase 6 target: $PI_USER@$PI_HOST"
echo "Goal: build and install LocalBox binaries and OpenRC service definitions"

mkdir -p "$BUILD_DIR"
[ -f "$SYNC_MAILMAPS_SOURCE" ] || {
    echo "ERROR: sync helper not found: $SYNC_MAILMAPS_SOURCE" >&2
    exit 1
}

echo ">> Building mail-ingest for linux/armv7..."
(
    cd "$PROJECT_DIR"
    GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "-s -w" -o "$BUILD_DIR/mail-ingest" .
)

echo ">> Building contact-service for linux/armv7..."
(
    cd "$PROJECT_DIR"
    GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "-s -w" -o "$BUILD_DIR/contact-service" ./cmd/contact
)

echo ">> Installing binaries on Pi..."
ssh $SSH_OPTS "$PI_USER@$PI_HOST" "LOCALBOX_BIN_DIR='$LOCALBOX_BIN_DIR' sh -s" <<'REMOTE'
set -eu
mountpoint -q /mnt/data || {
    echo "ERROR: /mnt/data is not mounted. Finish Phase 4 first." >&2
    exit 1
}
mkdir -p "$LOCALBOX_BIN_DIR" /usr/local/bin /usr/local/sbin /var/log /var/localbox /etc/localbox
REMOTE
cat "$BUILD_DIR/mail-ingest" | ssh $SSH_OPTS "$PI_USER@$PI_HOST" "cat > /tmp/mail-ingest.new && chmod +x /tmp/mail-ingest.new && mv /tmp/mail-ingest.new '$LOCALBOX_BIN_DIR/mail-ingest' && ln -sf '$LOCALBOX_BIN_DIR/mail-ingest' /usr/local/bin/mail-ingest"
cat "$BUILD_DIR/contact-service" | ssh $SSH_OPTS "$PI_USER@$PI_HOST" "cat > /tmp/contact-service.new && chmod +x /tmp/contact-service.new && mv /tmp/contact-service.new '$LOCALBOX_BIN_DIR/contact-service' && ln -sf '$LOCALBOX_BIN_DIR/contact-service' /usr/local/bin/contact-service"
cat "$SYNC_MAILMAPS_SOURCE" | ssh $SSH_OPTS "$PI_USER@$PI_HOST" 'mkdir -p /usr/local/sbin && cat > /tmp/localbox-sync-mailmaps.new && chmod 755 /tmp/localbox-sync-mailmaps.new && mv /tmp/localbox-sync-mailmaps.new /usr/local/sbin/localbox-sync-mailmaps && test -x /usr/local/sbin/localbox-sync-mailmaps'

echo ">> Installing OpenRC service definitions..."
ssh $SSH_OPTS "$PI_USER@$PI_HOST" 'sh -s' <<'REMOTE'
set -eu

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

mkdir -p /etc/localbox /var/localbox /var/log
touch /var/log/mail-ingest.log /var/log/contact-service.log
touch /var/localbox/contact.log /var/localbox/send.log /var/localbox/security.log
touch /etc/localbox/password-reset-tokens.conf
chown www:mail /var/log/mail-ingest.log /var/log/contact-service.log /var/localbox/contact.log /var/localbox/send.log /var/localbox/security.log /etc/localbox/password-reset-tokens.conf 2>/dev/null || true
chmod 664 /var/log/mail-ingest.log /var/log/contact-service.log /var/localbox/contact.log /var/localbox/send.log /var/localbox/security.log /etc/localbox/password-reset-tokens.conf

cat > /etc/init.d/mail-ingest <<'EOF'
#!/sbin/openrc-run
name="mail-ingest"
description="LocalBox mail ingest daemon"
command="/usr/local/bin/mail-ingest"
command_user="www:mail"
directory="/etc/localbox"
supervisor="supervise-daemon"
output_log="/var/log/mail-ingest.log"
error_log="/var/log/mail-ingest.log"
export LOCALBOX_BASE="/mnt/data/localbox"
export LOCALBOX_WEBMAIL_URL="${LOCALBOX_WEBMAIL_URL:-https://mail.localboxmail.co.za}"
if [ -f /etc/localbox/secret ]; then
    export LOCALBOX_SECRET="$(cat /etc/localbox/secret)"
fi
depend() {
    need localmount net nginx dovecot postfix
    after localmount nginx dovecot postfix
}
start_pre() {
    mountpoint -q /mnt/data
    checkpath -f -m 0664 -o www:mail /var/log/mail-ingest.log
}
EOF
chmod +x /etc/init.d/mail-ingest

cat > /etc/init.d/contact-service <<'EOF'
#!/sbin/openrc-run
name="contact-service"
description="LocalBox contact form service"
command="/usr/local/bin/contact-service"
command_user="www:mail"
supervisor="supervise-daemon"
output_log="/var/log/contact-service.log"
error_log="/var/log/contact-service.log"
export LOCALBOX_BASE="/mnt/data/localbox"
depend() {
    need localmount net nginx
    after localmount nginx
}
start_pre() {
    mountpoint -q /mnt/data
    checkpath -f -m 0664 -o www:mail /var/log/contact-service.log
}
EOF
chmod +x /etc/init.d/contact-service

rc-update add mail-ingest default 2>/dev/null || true
rc-update add contact-service default 2>/dev/null || true

if ! command -v sudo >/dev/null 2>&1 && command -v apk >/dev/null 2>&1; then
    ensure_apk_cache
    apk add sudo
fi
command -v sudo >/dev/null 2>&1 || {
    echo "ERROR: sudo did not install" >&2
    exit 1
}
ensure_apk_cache
apk cache sync 2>/dev/null || true
if rc-service mail-ingest status >/dev/null 2>&1; then
    rc-service mail-ingest restart
fi
if rc-service contact-service status >/dev/null 2>&1; then
    rc-service contact-service restart
fi

mkdir -p /etc/sudoers.d
cat > /etc/sudoers.d/localbox-mailmaps <<'EOF'
www ALL=(root) NOPASSWD: /usr/local/sbin/localbox-sync-mailmaps
EOF
chmod 440 /etc/sudoers.d/localbox-mailmaps

echo ">> Persisting binaries and service definitions..."
mount -o remount,rw /media/mmcblk0p1 2>/dev/null || true
lbu add /usr/local/bin/mail-ingest
lbu add /usr/local/bin/contact-service
lbu add /etc/init.d/mail-ingest
lbu add /etc/init.d/contact-service
lbu add /usr/local/sbin/localbox-sync-mailmaps
lbu add /etc/sudoers.d/localbox-mailmaps 2>/dev/null || true
lbu add /etc/runlevels/default/mail-ingest 2>/dev/null || true
lbu add /etc/runlevels/default/contact-service 2>/dev/null || true
lbu add /var/localbox
lbu add /etc/localbox/password-reset-tokens.conf
lbu add /etc/apk/world 2>/dev/null || true
lbu add /etc/apk/cache 2>/dev/null || true
lbu commit -d
mount -o remount,ro /media/mmcblk0p1 2>/dev/null || true

echo ">> Phase 6 complete."
ls -lh /usr/local/bin/mail-ingest /usr/local/bin/contact-service
rc-service mail-ingest status || true
rc-service contact-service status || true
REMOTE

echo
echo "Phase 6 finished."
echo "Services are installed and enabled. Phase 7 should add settings/domain config before starting them intentionally."
