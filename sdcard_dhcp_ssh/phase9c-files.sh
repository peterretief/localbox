#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
INVENTORY=${INVENTORY:-$SCRIPT_DIR/localbox-inventory.sh}

PI_HOST=${PI_HOST:-192.168.0.7}
PI_USER=${PI_USER:-root}
DEVICE_NAME=${DEVICE_NAME:-localbox-001}
LOCALBOX_BASE=${LOCALBOX_BASE:-/mnt/data/localbox}
LOCALBOX_BIN_DIR=${LOCALBOX_BIN_DIR:-$LOCALBOX_BASE/bin}
FILEBROWSER_VERSION=${FILEBROWSER_VERSION:-2.63.18}
FILEBROWSER_ARCHIVE=${FILEBROWSER_ARCHIVE:-linux-armv7-filebrowser.tar.gz}
FILEBROWSER_BASE_URL=${FILEBROWSER_BASE_URL:-https://github.com/filebrowser/filebrowser/releases/download/v$FILEBROWSER_VERSION}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/localbox_phase9c_known_hosts -o ConnectTimeout=10"}

echo "Phase 9c target: $PI_USER@$PI_HOST"
echo "Goal: install File Browser behind LocalBox /apps/files proxy"
echo "Device: $DEVICE_NAME"
echo "Version: $FILEBROWSER_VERSION"

if [ -x "$INVENTORY" ]; then
    "$INVENTORY" validate-device "$DEVICE_NAME" >/dev/null
fi

ssh $SSH_OPTS "$PI_USER@$PI_HOST" \
    "LOCALBOX_BASE='$LOCALBOX_BASE' LOCALBOX_BIN_DIR='$LOCALBOX_BIN_DIR' FILEBROWSER_VERSION='$FILEBROWSER_VERSION' FILEBROWSER_ARCHIVE='$FILEBROWSER_ARCHIVE' FILEBROWSER_BASE_URL='$FILEBROWSER_BASE_URL' sh -s" <<'REMOTE'
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

mountpoint -q /mnt/data || {
    echo "ERROR: /mnt/data is not mounted. Finish Phase 4 first." >&2
    exit 1
}

apk add -q ca-certificates curl tar
update-ca-certificates >/dev/null 2>&1 || true

FILES_ROOT=$LOCALBOX_BASE/files
CONFIG_DIR=/etc/filebrowser
DATA_DIR=$LOCALBOX_BASE/filebrowser
DB_PATH=$DATA_DIR/filebrowser.db
SERVICE_NAME=filebrowser

mkdir -p "$FILES_ROOT" "$CONFIG_DIR" "$DATA_DIR" "$LOCALBOX_BIN_DIR" /usr/local/bin

if [ ! -x "$LOCALBOX_BIN_DIR/filebrowser" ]; then
    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT
    curl -fsSL "$FILEBROWSER_BASE_URL/$FILEBROWSER_ARCHIVE" -o "$tmpdir/$FILEBROWSER_ARCHIVE"
    curl -fsSL "$FILEBROWSER_BASE_URL/filebrowser_${FILEBROWSER_VERSION}_checksums.txt" -o "$tmpdir/checksums.txt"
    expected=$(awk -v f="$FILEBROWSER_ARCHIVE" '$2 == f { print $1 }' "$tmpdir/checksums.txt")
    [ -n "$expected" ] || {
        echo "ERROR: checksum for $FILEBROWSER_ARCHIVE not found" >&2
        exit 1
    }
    actual=$(sha256sum "$tmpdir/$FILEBROWSER_ARCHIVE" | awk '{ print $1 }')
    [ "$actual" = "$expected" ] || {
        echo "ERROR: checksum mismatch for $FILEBROWSER_ARCHIVE" >&2
        exit 1
    }
    tar -xzf "$tmpdir/$FILEBROWSER_ARCHIVE" -C "$tmpdir"
    install -m 755 "$tmpdir/filebrowser" "$LOCALBOX_BIN_DIR/filebrowser"
fi
ln -sf "$LOCALBOX_BIN_DIR/filebrowser" /usr/local/bin/filebrowser

filebrowser config init --database "$DB_PATH" 2>/dev/null || true
filebrowser config set \
    --database "$DB_PATH" \
    --root "$FILES_ROOT" \
    --address 127.0.0.1 \
    --port 8082 \
    --baseurl /apps/files \
    --auth.method proxy \
    --auth.header X-Remote-User \
    --auth.logoutPage /logout \
    --branding.name "LocalBox Files" \
    --commands "" >/dev/null

cat > /etc/init.d/$SERVICE_NAME <<EOF
#!/sbin/openrc-run
name="$SERVICE_NAME"
description="LocalBox File Browser"
command="/usr/local/bin/filebrowser"
command_args="-d $DB_PATH"
command_background="yes"
pidfile="/run/$SERVICE_NAME.pid"
output_log="/var/log/$SERVICE_NAME.log"
error_log="/var/log/$SERVICE_NAME.log"
depend() {
    need localmount net
    after nginx
}
EOF
chmod 755 /etc/init.d/$SERVICE_NAME
touch /var/log/$SERVICE_NAME.log

rc-update add "$SERVICE_NAME" default 2>/dev/null || true
rc-service "$SERVICE_NAME" restart

ensure_lbu_media
mount -o remount,rw /media/mmcblk0p1 2>/dev/null || true
lbu add /etc/apk/world
lbu add /usr/local/bin/filebrowser
lbu add /etc/filebrowser
lbu add /etc/init.d/$SERVICE_NAME
lbu add /etc/runlevels/default/$SERVICE_NAME 2>/dev/null || true
lbu add /etc/lbu/lbu.conf
lbu commit -d
mount -o remount,ro /media/mmcblk0p1 2>/dev/null || true

rc-service "$SERVICE_NAME" status
curl -I -sS http://127.0.0.1:8082/apps/files/ | sed -n '1,8p'
REMOTE

if [ -x "$INVENTORY" ]; then
    "$INVENTORY" phase "$DEVICE_NAME" phase9c-files complete detail="filebrowser 127.0.0.1:8082"
fi

echo
echo "Phase 9c finished."
