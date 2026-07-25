#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
ALPS_PROJECT_DIR=${ALPS_PROJECT_DIR:-$(dirname "$PROJECT_DIR")/alps}
ALPS_BINARY=${ALPS_BINARY:-$ALPS_PROJECT_DIR/build/alps-linux-armv7}
ALPS_REBUILD=${ALPS_REBUILD:-auto}
INVENTORY=${INVENTORY:-$SCRIPT_DIR/localbox-inventory.sh}

PI_HOST=${PI_HOST:-192.168.0.7}
PI_USER=${PI_USER:-root}
DEVICE_NAME=${DEVICE_NAME:-localbox-001}
LOCALBOX_DOMAIN=${LOCALBOX_DOMAIN:-localboxtest.co.za}
LOCALBOX_BIN_DIR=${LOCALBOX_BIN_DIR:-/mnt/data/localbox/bin}
ALPS_ADDR=${ALPS_ADDR:-127.0.0.1:1323}
ALPS_CONFIG=${ALPS_CONFIG:-/etc/alps/config.toml}
ALPS_MASTER_USERNAME=${ALPS_MASTER_USERNAME:-localbox-alps}
ALPS_MASTER_SEPARATOR=${ALPS_MASTER_SEPARATOR:-*}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/localbox_phase9d_known_hosts -o ConnectTimeout=10"}

echo "Phase 9d target: $PI_USER@$PI_HOST"
echo "Goal: install patched ALPS webmail with LocalBox SSO"
echo "Device: $DEVICE_NAME"
echo "Domain: $LOCALBOX_DOMAIN"
echo "ALPS:   $ALPS_BINARY"

source_newer_than_binary() {
    [ -f "$ALPS_BINARY" ] || return 0
    find "$ALPS_PROJECT_DIR" \
        \( -path "$ALPS_PROJECT_DIR/.git" -o -path "$ALPS_PROJECT_DIR/build" -o -path "$ALPS_PROJECT_DIR/frontend/node_modules" \) -prune \
        -o \( -name '*.go' -o -path "$ALPS_PROJECT_DIR/frontend/src/*" -o -path "$ALPS_PROJECT_DIR/frontend/index.html" -o -path "$ALPS_PROJECT_DIR/frontend/package.json" \) \
        -newer "$ALPS_BINARY" -print -quit | grep -q .
}

if [ "$ALPS_REBUILD" = "true" ] || [ "$ALPS_REBUILD" = "1" ] || [ "$ALPS_REBUILD" = "yes" ] || { [ "$ALPS_REBUILD" = "auto" ] && source_newer_than_binary; }; then
    echo ">> Rebuilding ALPS for linux/armv7..."
    [ -d "$ALPS_PROJECT_DIR/frontend" ] || {
        echo "ERROR: ALPS frontend directory not found: $ALPS_PROJECT_DIR/frontend" >&2
        exit 1
    }
    (cd "$ALPS_PROJECT_DIR/frontend" && npm run build)
    mkdir -p "$(dirname "$ALPS_BINARY")"
    (cd "$ALPS_PROJECT_DIR" && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -o "$ALPS_BINARY" ./cmd/alps)
fi

if [ ! -f "$ALPS_BINARY" ]; then
    echo "ERROR: ALPS binary not found: $ALPS_BINARY" >&2
    echo "Build it first:" >&2
    echo "  cd $ALPS_PROJECT_DIR && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -o build/alps-linux-armv7 ./cmd/alps" >&2
    exit 1
fi
if [ -x "$INVENTORY" ]; then
    "$INVENTORY" validate-device "$DEVICE_NAME" >/dev/null
fi

echo ">> Installing ALPS binary..."
ssh $SSH_OPTS "$PI_USER@$PI_HOST" "LOCALBOX_BIN_DIR='$LOCALBOX_BIN_DIR' sh -s" <<'REMOTE'
set -eu
mountpoint -q /mnt/data || {
    echo "ERROR: /mnt/data is not mounted. Finish Phase 4 first." >&2
    exit 1
}
mkdir -p "$LOCALBOX_BIN_DIR" /usr/local/bin /etc/alps /var/log
REMOTE
cat "$ALPS_BINARY" | ssh $SSH_OPTS "$PI_USER@$PI_HOST" "cat > /tmp/alps.new && chmod 755 /tmp/alps.new && rc-service alps stop 2>/dev/null || true; mv /tmp/alps.new '$LOCALBOX_BIN_DIR/alps' && ln -sf '$LOCALBOX_BIN_DIR/alps' /usr/local/bin/alps"

echo ">> Configuring ALPS, Dovecot master auth, and OpenRC service..."
ssh $SSH_OPTS "$PI_USER@$PI_HOST" \
    "LOCALBOX_DOMAIN='$LOCALBOX_DOMAIN' ALPS_ADDR='$ALPS_ADDR' ALPS_CONFIG='$ALPS_CONFIG' ALPS_MASTER_USERNAME='$ALPS_MASTER_USERNAME' ALPS_MASTER_SEPARATOR='$ALPS_MASTER_SEPARATOR' sh -s" <<'REMOTE'
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

random_secret() {
    if command -v openssl >/dev/null 2>&1; then
        openssl rand -base64 32
    else
        dd if=/dev/urandom bs=32 count=1 2>/dev/null | base64
    fi
}

[ -f /etc/dovecot/dovecot.conf ] || {
    echo "ERROR: Dovecot is not configured. Run Phase 7 first." >&2
    exit 1
}
[ -f /etc/localbox/sso-secret ] || {
    echo "ERROR: LocalBox SSO secret missing. Run Phase 8 first." >&2
    exit 1
}

mkdir -p /etc/alps /var/log/alps /mnt/data/localbox/alps/tmp
chown -R www:mail /mnt/data/localbox/alps /var/log/alps 2>/dev/null || true
chmod 750 /mnt/data/localbox/alps /var/log/alps 2>/dev/null || true
chown www:mail /etc/localbox/sso-secret
chmod 640 /etc/localbox/sso-secret

MASTER_PASSWORD_FILE=/etc/localbox/alps-master-password
if [ ! -s "$MASTER_PASSWORD_FILE" ]; then
    random_secret > "$MASTER_PASSWORD_FILE"
fi
chown root:mail "$MASTER_PASSWORD_FILE"
chmod 640 "$MASTER_PASSWORD_FILE"
MASTER_PASSWORD=$(cat "$MASTER_PASSWORD_FILE")

# Keep transient backups out of /etc/dovecot because that directory is lbu-tracked.
mkdir -p /mnt/data/backups/alps
rm -f /etc/dovecot/dovecot.conf.bak-alps-*
timestamp=$(date +%Y%m%d%H%M%S)
cp -p /etc/dovecot/dovecot.conf "/mnt/data/backups/alps/dovecot.conf.bak-alps-$timestamp"
tmp_dovecot=$(mktemp)
awk -v user="$ALPS_MASTER_USERNAME" -v pass="$MASTER_PASSWORD" -v sep="$ALPS_MASTER_SEPARATOR" '
    /^# LocalBox ALPS SSO master user$/ { skip = 1; next }
    /^# End LocalBox ALPS SSO master user$/ { skip = 0; next }
    skip { next }
    /^auth_master_user_separator[[:space:]]*=/ { next }
    /^passdb passwd-file[[:space:]]*\{/ && !inserted {
        print "auth_master_user_separator = " sep
        print "# LocalBox ALPS SSO master user"
        print "passdb static {"
        print "  master = yes"
        print "  username_filter = " user
        print "  password = " pass
        print "}"
        print "# End LocalBox ALPS SSO master user"
        inserted = 1
    }
    { print }
    END {
        if (!inserted) {
            print "auth_master_user_separator = " sep
            print "# LocalBox ALPS SSO master user"
            print "passdb static {"
            print "  master = yes"
            print "  username_filter = " user
            print "  password = " pass
            print "}"
            print "# End LocalBox ALPS SSO master user"
        }
    }
' /etc/dovecot/dovecot.conf > "$tmp_dovecot"
dovecot -c "$tmp_dovecot" -n >/dev/null
cat "$tmp_dovecot" > /etc/dovecot/dovecot.conf
rm -f "$tmp_dovecot"

cat > "$ALPS_CONFIG" <<EOF
[server]
addr = "$ALPS_ADDR"
debug = false
trusted_proxies = ["127.0.0.1/32", "::1/128"]
temp_dir = "/mnt/data/localbox/alps/tmp"
session_minutes = 60
max_session_minutes = 1440
read_timeout_sec = 10
write_timeout_sec = 60
idle_timeout_sec = 120
imap_timeout_sec = 30
smtp_timeout_sec = 30

[server.rate_limit]
enabled = true
ip_requests_per_minute = 10
ip_requests_per_hour = 60
username_fails_per_quarter = 5
username_fails_per_hour = 10
global_requests_per_second = 100
lockout_minutes = 15

[cache]
ttl_minutes = 10
enabled = true

[logging]
output = "stderr"
format = "console"
level = "info"

[provider]
type = "imap"

[provider.imap]
server = "imap+insecure://127.0.0.1:143"
insecure = true

[smtp]
server = "smtp+insecure://127.0.0.1:587"
insecure = true

[localbox.sso]
enabled = true
secret_file = "/etc/localbox/sso-secret"
master_username = "$ALPS_MASTER_USERNAME"
master_password = "$MASTER_PASSWORD"
master_separator = "$ALPS_MASTER_SEPARATOR"
redirect_url = "/"

[plugin.base]
enabled = true

[plugin.caldav]
enabled = false

[plugin.carddav]
enabled = false

[plugin.managesieve]
enabled = false

[plugin.gpg]
enabled = false

[plugin.password]
enabled = false
EOF
chown root:mail "$ALPS_CONFIG"
chmod 640 "$ALPS_CONFIG"

cat > /etc/init.d/alps <<EOF
#!/sbin/openrc-run
name="alps"
description="LocalBox ALPS webmail"
command="/usr/local/bin/alps"
command_args="-config $ALPS_CONFIG"
command_user="www:mail"
directory="/mnt/data/localbox/alps"
supervisor="supervise-daemon"
output_log="/var/log/alps/alps.log"
error_log="/var/log/alps/alps.log"
depend() {
    need localmount net dovecot postfix
    after dovecot postfix
}
start_pre() {
    checkpath -d -m 0750 -o www:mail /mnt/data/localbox/alps
    checkpath -d -m 0750 -o www:mail /var/log/alps
    checkpath -f -m 0640 -o www:mail /var/log/alps/alps.log
}
EOF
chmod 755 /etc/init.d/alps

touch /var/log/alps/alps.log
chown www:mail /var/log/alps/alps.log
chmod 640 /var/log/alps/alps.log

rc-service dovecot restart </dev/null >/tmp/phase9d-dovecot.log 2>&1
rc-update add alps default 2>/dev/null || true
rc-service alps restart </dev/null >/tmp/phase9d-alps.log 2>&1
sleep 2
rc-service alps status >/tmp/phase9d-alps-status.log 2>&1 || {
    echo "ERROR: alps did not stay started" >&2
    cat /tmp/phase9d-alps.log /tmp/phase9d-alps-status.log >&2
    tail -n 80 /var/log/alps/alps.log >&2 2>/dev/null || true
    exit 1
}

ensure_lbu_media
mount -o remount,rw /media/mmcblk0p1 2>/dev/null || true
lbu include -r /etc/dovecot/dovecot.conf.bak-alps-* 2>/dev/null || true
lbu add /usr/local/bin/alps
lbu add /etc/alps
lbu add /etc/localbox/sso-secret /etc/localbox/alps-master-password
lbu add /etc/dovecot/dovecot.conf
lbu add /etc/init.d/alps
lbu add /etc/runlevels/default/alps 2>/dev/null || true
lbu add /var/log/alps 2>/dev/null || true
lbu add /etc/lbu/lbu.conf
lbu commit -d
mount -o remount,ro /media/mmcblk0p1 2>/dev/null || true

rc-service alps status </dev/null
curl --max-time 5 -I -sS "http://$ALPS_ADDR/" | sed -n '1,8p'
REMOTE

if [ -x "$INVENTORY" ]; then
    "$INVENTORY" phase "$DEVICE_NAME" phase9d-alps-webmail complete detail="alps $ALPS_ADDR mail.$LOCALBOX_DOMAIN"
fi

echo
echo "Phase 9d finished."
echo "Next route mail.$LOCALBOX_DOMAIN to LOCALBOX_TUNNEL_SERVICE=http://localhost:1323."
