#!/bin/sh
set -eu

VERSION=${FILEBROWSER_VERSION:-2.63.18}
ARCHIVE=${FILEBROWSER_ARCHIVE:-linux-armv7-filebrowser.tar.gz}
BASE_URL=${FILEBROWSER_BASE_URL:-https://github.com/filebrowser/filebrowser/releases/download/v$VERSION}
LOCALBOX_BASE=${LOCALBOX_BASE:-/mnt/data/localbox}
FILES_ROOT=${FILES_ROOT:-$LOCALBOX_BASE/files}
CONFIG_DIR=${CONFIG_DIR:-/etc/filebrowser}
DATA_DIR=${DATA_DIR:-$LOCALBOX_BASE/filebrowser}
DB_PATH=${DB_PATH:-$DATA_DIR/filebrowser.db}
AUTH_HOOK=${AUTH_HOOK:-/usr/local/bin/localbox-filebrowser-auth}
LISTEN_ADDR=${LISTEN_ADDR:-127.0.0.1}
LISTEN_PORT=${LISTEN_PORT:-8082}
BASEURL=${BASEURL:-/portal}
NGINX_CONF=${NGINX_CONF:-/etc/nginx/http.d/localbox-webdav.conf}
SERVICE_NAME=${SERVICE_NAME:-filebrowser}

require_cmd() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "ERROR: required command not found: $1" >&2
        exit 1
    fi
}

require_cmd curl
require_cmd tar
require_cmd sha256sum
require_cmd doveadm

mkdir -p "$FILES_ROOT" "$CONFIG_DIR" "$DATA_DIR"

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

curl -fsSL "$BASE_URL/$ARCHIVE" -o "$tmpdir/$ARCHIVE"
curl -fsSL "$BASE_URL/filebrowser_${VERSION}_checksums.txt" -o "$tmpdir/checksums.txt"
expected=$(awk -v f="$ARCHIVE" '$2 == f { print $1 }' "$tmpdir/checksums.txt")
if [ -z "$expected" ]; then
    echo "ERROR: checksum for $ARCHIVE not found" >&2
    exit 1
fi
actual=$(sha256sum "$tmpdir/$ARCHIVE" | awk '{ print $1 }')
if [ "$actual" != "$expected" ]; then
    echo "ERROR: checksum mismatch for $ARCHIVE" >&2
    exit 1
fi

tar -xzf "$tmpdir/$ARCHIVE" -C "$tmpdir"
install -m 755 "$tmpdir/filebrowser" /usr/local/bin/filebrowser

cat > "$AUTH_HOOK" <<'HOOK'
#!/bin/sh
set -eu

username=${USERNAME:-}
password=${PASSWORD:-}

case "$username" in
    ""|*/*|*..*|*\\*|*:*|*@.*|*..*)
        echo "hook.action=block"
        exit 0
        ;;
esac

if command -v mail-ingest >/dev/null 2>&1 && printf '%s\n' "$password" | mail-ingest auth-check "$username" >/dev/null 2>&1; then
    authenticated=1
elif doveadm auth test "$username" "$password" >/dev/null 2>&1; then
    authenticated=1
else
    authenticated=0
fi

if [ "$authenticated" = "1" ]; then
    mkdir -p "/mnt/data/localbox/files/$username"
    chown nginx:nginx "/mnt/data/localbox/files/$username" 2>/dev/null || true
    chmod 750 "/mnt/data/localbox/files/$username" 2>/dev/null || true
    echo "hook.action=auth"
    echo "user.scope=/$username"
    echo "user.perm.create=true"
    echo "user.perm.delete=true"
    echo "user.perm.download=true"
    echo "user.perm.modify=true"
    echo "user.perm.rename=true"
    echo "user.perm.share=true"
    echo "user.perm.execute=false"
    echo "user.lockPassword=true"
    echo "user.hideDotfiles=true"
    echo "user.viewMode=list"
else
    echo "hook.action=block"
fi
HOOK
chmod 755 "$AUTH_HOOK"

filebrowser config init --database "$DB_PATH" 2>/dev/null || true
filebrowser config set \
    --database "$DB_PATH" \
    --root "$FILES_ROOT" \
    --address "$LISTEN_ADDR" \
    --port "$LISTEN_PORT" \
    --baseurl "$BASEURL" \
    --auth.method hook \
    --auth.command "$AUTH_HOOK" \
    --branding.name "LocalBox Files" \
    --commands "" >/dev/null

cat > "/etc/init.d/$SERVICE_NAME" <<EOF
#!/sbin/openrc-run
name="$SERVICE_NAME"
description="LocalBox File Browser portal"
command="/usr/local/bin/filebrowser"
command_args="-d $DB_PATH"
command_background="yes"
pidfile="/run/$SERVICE_NAME.pid"
output_log="/var/log/$SERVICE_NAME.log"
error_log="/var/log/$SERVICE_NAME.log"
depend() {
    need net
    after nginx dovecot
}
EOF
chmod +x "/etc/init.d/$SERVICE_NAME"
touch "/var/log/$SERVICE_NAME.log"
chown nginx:nginx "/var/log/$SERVICE_NAME.log" "$DB_PATH" "$DATA_DIR" 2>/dev/null || true
chmod 640 "/var/log/$SERVICE_NAME.log" "$DB_PATH" 2>/dev/null || true

if ! grep -q "location /portal/" "$NGINX_CONF"; then
    cp -p "$NGINX_CONF" "$NGINX_CONF.bak-filebrowser-$(date +%Y%m%d%H%M%S)"
    tmp=$(mktemp)
    awk -v port="$LISTEN_PORT" '
        /^}/ && !done {
            print "    location = /portal {"
            print "        auth_basic off;"
            print "        return 302 /portal/;"
            print "    }"
            print ""
            print "    location /portal/ {"
            print "        auth_basic off;"
            print "        proxy_pass http://127.0.0.1:" port "/portal/;"
            print "        proxy_http_version 1.1;"
            print "        proxy_set_header Host $host;"
            print "        proxy_set_header X-Real-IP $remote_addr;"
            print "        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;"
            print "        proxy_set_header X-Forwarded-Proto https;"
            print "        proxy_set_header Upgrade $http_upgrade;"
            print "        proxy_set_header Connection \"upgrade\";"
            print "    }"
            print ""
            done = 1
        }
        { print }
    ' "$NGINX_CONF" > "$tmp"
    cat "$tmp" > "$NGINX_CONF"
    rm -f "$tmp"
fi

rc-update add "$SERVICE_NAME" default 2>/dev/null || true
rc-service "$SERVICE_NAME" restart
nginx -t
rc-service nginx restart

lbu add /usr/local/bin/filebrowser "$AUTH_HOOK" "$CONFIG_DIR" "$DATA_DIR" "/etc/init.d/$SERVICE_NAME" "$NGINX_CONF" "/var/log/$SERVICE_NAME.log" 2>/dev/null || true
lbu commit -d 2>/dev/null || true

echo "Configured File Browser portal:"
echo "  url:  https://files.localboxmail.co.za$BASEURL/"
echo "  root: $FILES_ROOT"
echo "  auth: Dovecot hook"
