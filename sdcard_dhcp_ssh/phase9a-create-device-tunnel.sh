#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
INVENTORY=${INVENTORY:-$SCRIPT_DIR/localbox-inventory.sh}
CLOUDFLARE_CONFIG=${CLOUDFLARE_CONFIG:-$PROJECT_DIR/cloudflare.conf}

PI_HOST=${PI_HOST:-192.168.0.7}
PI_USER=${PI_USER:-root}
DEVICE_NAME=${DEVICE_NAME:-localbox-001}
LOCALBOX_TUNNEL=${LOCALBOX_TUNNEL:-}
LOCALBOX_TUNNEL_SERVICE=${LOCALBOX_TUNNEL_SERVICE:-http://localhost:8080}
LOCALBOX_BIN_DIR=${LOCALBOX_BIN_DIR:-/mnt/data/localbox/bin}
CLOUDFLARED_URL=${CLOUDFLARED_URL:-https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-arm}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/localbox_phase9a_known_hosts -o ConnectTimeout=10"}

echo "Phase 9a target: $PI_USER@$PI_HOST"
echo "Goal: create dedicated Cloudflare Tunnel, install connector, record inventory"
echo "Device:  $DEVICE_NAME"
echo "Tunnel:  ${LOCALBOX_TUNNEL:-from inventory expected_tunnel}"
echo "Config:  $CLOUDFLARE_CONFIG"

if [ ! -x "$INVENTORY" ]; then
    echo "ERROR: inventory tool not found: $INVENTORY" >&2
    exit 1
fi
if [ ! -f "$CLOUDFLARE_CONFIG" ]; then
    echo "ERROR: Cloudflare config not found: $CLOUDFLARE_CONFIG" >&2
    echo "Copy cloudflare.conf.example to cloudflare.conf and set enabled=true, account_id, api_token." >&2
    exit 1
fi
if ! command -v curl >/dev/null 2>&1; then
    echo "ERROR: curl is required" >&2
    exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
    echo "ERROR: jq is required" >&2
    exit 1
fi

"$INVENTORY" validate-device "$DEVICE_NAME" >/dev/null
if [ -z "$LOCALBOX_TUNNEL" ]; then
    LOCALBOX_TUNNEL=$("$INVENTORY" device-field "$DEVICE_NAME" expected_tunnel)
fi
if [ -z "$LOCALBOX_TUNNEL" ]; then
    echo "ERROR: LOCALBOX_TUNNEL is required and expected_tunnel is missing from inventory." >&2
    exit 1
fi

# shellcheck disable=SC1090
. "$CLOUDFLARE_CONFIG"

API_URL=${api_url:-${url:-https://api.cloudflare.com/client/v4}}
API_URL=$(printf '%s' "$API_URL" | sed 's:/*$::')
ACCOUNT_ID=${account_id:-${account:-}}
API_TOKEN=${api_token:-${token:-${api_key:-${apikey:-${key:-}}}}}
ACCOUNT_ID=${CLOUDFLARE_ACCOUNT_ID:-$ACCOUNT_ID}
API_TOKEN=${CLOUDFLARE_API_TOKEN:-$API_TOKEN}

if [ "${enabled:-false}" != "true" ] && [ "${enabled:-false}" != "1" ] && [ "${enabled:-false}" != "yes" ]; then
    echo "ERROR: Cloudflare integration is disabled in $CLOUDFLARE_CONFIG" >&2
    exit 1
fi
if [ -z "$ACCOUNT_ID" ] || [ -z "$API_TOKEN" ]; then
    echo "ERROR: account_id and api_token are required in $CLOUDFLARE_CONFIG" >&2
    exit 1
fi

api() {
    method=$1
    path=$2
    data=${3:-}
    if [ -n "$data" ]; then
        curl -fsS -X "$method" "$API_URL$path" \
            -H "Authorization: Bearer $API_TOKEN" \
            -H "Content-Type: application/json" \
            -H "Accept: application/json" \
            --data-binary "$data"
    else
        curl -fsS -X "$method" "$API_URL$path" \
            -H "Authorization: Bearer $API_TOKEN" \
            -H "Accept: application/json"
    fi
}

echo ">> Checking for existing tunnel..."
tunnel_json=$(api GET "/accounts/$ACCOUNT_ID/cfd_tunnel?is_deleted=false&per_page=100")
TUNNEL_ID=$(printf '%s' "$tunnel_json" | jq -r --arg tunnel "$LOCALBOX_TUNNEL" '.result[]? | select(.id == $tunnel or .name == $tunnel) | .id' | head -1)
TUNNEL_STATUS=$(printf '%s' "$tunnel_json" | jq -r --arg id "$TUNNEL_ID" '.result[]? | select(.id == $id) | .status' | head -1)

if [ -z "$TUNNEL_ID" ]; then
    echo ">> Creating tunnel $LOCALBOX_TUNNEL..."
    create_payload=$(jq -nc --arg name "$LOCALBOX_TUNNEL" '{name:$name, config_src:"cloudflare"}')
    create_json=$(api POST "/accounts/$ACCOUNT_ID/cfd_tunnel" "$create_payload")
    TUNNEL_ID=$(printf '%s' "$create_json" | jq -r '.result.id // empty')
    TUNNEL_STATUS=$(printf '%s' "$create_json" | jq -r '.result.status // "inactive"')
fi
if [ -z "$TUNNEL_ID" ]; then
    echo "ERROR: failed to create/find tunnel $LOCALBOX_TUNNEL" >&2
    exit 1
fi
echo "   $LOCALBOX_TUNNEL ($TUNNEL_STATUS) $TUNNEL_ID"

echo ">> Fetching tunnel token..."
token_json=$(api GET "/accounts/$ACCOUNT_ID/cfd_tunnel/$TUNNEL_ID/token")
TUNNEL_TOKEN=$(printf '%s' "$token_json" | jq -r '.result // .token // empty')
if [ -z "$TUNNEL_TOKEN" ]; then
    echo "ERROR: failed to fetch tunnel token for $LOCALBOX_TUNNEL" >&2
    exit 1
fi

echo ">> Installing cloudflared connector on Pi..."
ssh $SSH_OPTS "$PI_USER@$PI_HOST" "LOCALBOX_TUNNEL='$LOCALBOX_TUNNEL' TUNNEL_TOKEN='$TUNNEL_TOKEN' CLOUDFLARED_URL='$CLOUDFLARED_URL' LOCALBOX_BIN_DIR='$LOCALBOX_BIN_DIR' sh -s" <<'REMOTE'
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

apk add -q ca-certificates curl
update-ca-certificates >/dev/null 2>&1 || true

mountpoint -q /mnt/data || {
    echo "ERROR: /mnt/data is not mounted. Finish Phase 4 first." >&2
    exit 1
}
mkdir -p "$LOCALBOX_BIN_DIR" /usr/local/bin

if [ ! -x "$LOCALBOX_BIN_DIR/cloudflared" ]; then
    echo ">> Downloading cloudflared..."
    curl -fsSL "$CLOUDFLARED_URL" -o "$LOCALBOX_BIN_DIR/cloudflared"
    chmod 755 "$LOCALBOX_BIN_DIR/cloudflared"
fi
ln -sf "$LOCALBOX_BIN_DIR/cloudflared" /usr/local/bin/cloudflared

mkdir -p /etc/cloudflared /var/log/cloudflared /run/cloudflared /usr/local/sbin
chmod 700 /etc/cloudflared
printf '%s\n' "$TUNNEL_TOKEN" > /etc/cloudflared/token
chmod 600 /etc/cloudflared/token

cat > /usr/local/sbin/localbox-cloudflared-run <<'EOF'
#!/bin/sh
set -eu
exec /usr/local/bin/cloudflared tunnel --no-autoupdate --loglevel info --logfile /var/log/cloudflared/cloudflared.log run --token "$(cat /etc/cloudflared/token)"
EOF
chmod 755 /usr/local/sbin/localbox-cloudflared-run

cat > /etc/init.d/cloudflared <<'EOF'
#!/sbin/openrc-run
name="cloudflared"
description="Cloudflare Tunnel connector"
command="/usr/local/sbin/localbox-cloudflared-run"
supervisor="supervise-daemon"
directory="/"
output_log="/var/log/cloudflared/cloudflared.log"
error_log="/var/log/cloudflared/cloudflared.log"

depend() {
    need localmount net
    after firewall
}

start_pre() {
    checkpath -d -m 0755 /run/cloudflared
    checkpath -d -m 0755 /var/log/cloudflared
}
EOF
chmod 755 /etc/init.d/cloudflared

rc-update add cloudflared default 2>/dev/null || true
rc-service cloudflared restart </dev/null >/tmp/phase9a-cloudflared.log 2>&1
sleep 3
rc-service cloudflared status >/tmp/phase9a-cloudflared-status.log 2>&1 || {
    echo "ERROR: cloudflared did not stay started" >&2
    cat /tmp/phase9a-cloudflared.log /tmp/phase9a-cloudflared-status.log >&2
    tail -n 80 /var/log/cloudflared/cloudflared.log >&2 2>/dev/null || true
    exit 1
}

ensure_lbu_media
mount -o remount,rw /media/mmcblk0p1 2>/dev/null || true
lbu add /etc/apk/world
lbu add /usr/local/bin/cloudflared
lbu add /usr/local/sbin/localbox-cloudflared-run
lbu add /etc/cloudflared
lbu add /var/log/cloudflared 2>/dev/null || true
lbu add /etc/init.d/cloudflared
lbu add /etc/runlevels/default/cloudflared 2>/dev/null || true
lbu add /etc/lbu/lbu.conf
lbu commit -d
mount -o remount,ro /media/mmcblk0p1 2>/dev/null || true

cloudflared --version || true
rc-service cloudflared status || true
REMOTE

"$INVENTORY" upsert-tunnel "$LOCALBOX_TUNNEL" \
    tunnel_id="$TUNNEL_ID" \
    device_name="$DEVICE_NAME" \
    status="$TUNNEL_STATUS" \
    origin_ip="$PI_HOST" \
    token_installed=1 \
    notes=phase9a-created
"$INVENTORY" phase "$DEVICE_NAME" phase9a-create-device-tunnel complete detail="$LOCALBOX_TUNNEL $TUNNEL_ID"

echo ">> Phase 9a complete."
"$INVENTORY" validate-publish "$DEVICE_NAME" "${LOCALBOX_DOMAIN:-localboxtest.co.za}" "$LOCALBOX_TUNNEL" || true

echo
echo "Next, publish the route:"
echo "  DEVICE_NAME=$DEVICE_NAME LOCALBOX_TUNNEL=$LOCALBOX_TUNNEL ./phase9-cloudflare-route.sh"
