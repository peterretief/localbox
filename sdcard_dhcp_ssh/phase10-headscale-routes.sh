#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
ENV_FILE=${ENV_FILE:-$SCRIPT_DIR/.env}
INVENTORY=${INVENTORY:-$SCRIPT_DIR/localbox-inventory.sh}
HEADSCALE_API=${HEADSCALE_API:-$SCRIPT_DIR/headscale-api.sh}

PI_HOST=${PI_HOST:-}
PI_USER=${PI_USER:-root}
DEVICE_NAME=${DEVICE_NAME:-localbox-001}
TAILSCALE_HOSTNAME=${TAILSCALE_HOSTNAME:-$DEVICE_NAME}
TAILSCALE_ADVERTISE_ROUTES=${TAILSCALE_ADVERTISE_ROUTES:-}
TAILSCALE_ACCEPT_ROUTES=${TAILSCALE_ACCEPT_ROUTES:-false}
HEADSCALE_USER_ID=${HEADSCALE_USER_ID:-2}
HEADSCALE_CREATE_AUTHKEY=${HEADSCALE_CREATE_AUTHKEY:-false}
HEADSCALE_APPROVE_ROUTES=${HEADSCALE_APPROVE_ROUTES:-false}
HEADSCALE_PREAUTHKEY_METHOD=${HEADSCALE_PREAUTHKEY_METHOD:-ssh}
TAILSCALE_AUTHKEY=${TAILSCALE_AUTHKEY:-}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/localbox_phase10_known_hosts -o ConnectTimeout=10"}

if [ -f "$ENV_FILE" ]; then
    set -a
    # shellcheck disable=SC1090
    . "$ENV_FILE"
    set +a
fi

HEADSCALE_LOGIN_SERVER=${HEADSCALE_LOGIN_SERVER:-${HEADSCALE_URL:-https://headscale.withcare.co.za}}
if [ "$HEADSCALE_LOGIN_SERVER" = "https://headscale.example.com" ]; then
    HEADSCALE_LOGIN_SERVER=https://headscale.withcare.co.za
fi

if [ -z "$PI_HOST" ] && [ -x "$INVENTORY" ]; then
    PI_HOST=$("$INVENTORY" device-field "$DEVICE_NAME" pi_host 2>/dev/null || true)
fi
[ -n "$PI_HOST" ] || {
    echo "ERROR: PI_HOST is required for Phase 10." >&2
    exit 1
}

if [ -z "$TAILSCALE_ADVERTISE_ROUTES" ]; then
    TAILSCALE_ADVERTISE_ROUTES="$PI_HOST/32"
fi

echo "Phase 10 target: $PI_USER@$PI_HOST"
echo "Goal: Tailscale/Headscale join and explicit route advertisement"
echo "Device:     $DEVICE_NAME"
echo "TS name:    $TAILSCALE_HOSTNAME"
echo "Headscale:  $HEADSCALE_LOGIN_SERVER"
echo "Advertise:  ${TAILSCALE_ADVERTISE_ROUTES:-none}"
echo "Approve:    $HEADSCALE_APPROVE_ROUTES"
echo "Authkey via: $HEADSCALE_PREAUTHKEY_METHOD"

if [ -z "$TAILSCALE_AUTHKEY" ] && { [ "$HEADSCALE_CREATE_AUTHKEY" = "true" ] || [ "$HEADSCALE_CREATE_AUTHKEY" = "1" ] || [ "$HEADSCALE_CREATE_AUTHKEY" = "yes" ]; }; then
    echo ">> Creating one-use Headscale preauth key..."
    TAILSCALE_AUTHKEY=$(HEADSCALE_URL="$HEADSCALE_LOGIN_SERVER" HEADSCALE_PREAUTHKEY_METHOD="$HEADSCALE_PREAUTHKEY_METHOD" "$HEADSCALE_API" create-preauthkey user="$HEADSCALE_USER_ID" expiration=24h reusable=false | jq -r '.preAuthKey.key // .preauthkey.key // .key // empty')
    [ -n "$TAILSCALE_AUTHKEY" ] || {
        echo "ERROR: Headscale did not return a preauth key." >&2
        exit 1
    }
fi

echo ">> Installing/updating Tailscale persistence helper on target..."
ssh $SSH_OPTS "$PI_USER@$PI_HOST" 'mkdir -p /usr/local/sbin'
cat "$PROJECT_DIR/scripts/enable-tailscale-persistence.sh" | ssh $SSH_OPTS "$PI_USER@$PI_HOST" 'cat > /usr/local/sbin/localbox-enable-tailscale-persistence && chmod +x /usr/local/sbin/localbox-enable-tailscale-persistence'

echo ">> Enabling Tailscale and advertising routes..."
ssh $SSH_OPTS "$PI_USER@$PI_HOST" \
    "HEADSCALE_LOGIN_SERVER='$HEADSCALE_LOGIN_SERVER' TAILSCALE_AUTHKEY='$TAILSCALE_AUTHKEY' TAILSCALE_HOSTNAME='$TAILSCALE_HOSTNAME' TAILSCALE_ADVERTISE_ROUTES='$TAILSCALE_ADVERTISE_ROUTES' TAILSCALE_ACCEPT_ROUTES='$TAILSCALE_ACCEPT_ROUTES' /usr/local/sbin/localbox-enable-tailscale-persistence"

echo ">> Reading target Tailscale status..."
status_json=$(ssh $SSH_OPTS "$PI_USER@$PI_HOST" 'tailscale status --json 2>/dev/null || true')
tailscale_ips=$(printf '%s' "$status_json" | jq -r '(.Self.TailscaleIPs // []) | join(",")' 2>/dev/null || true)
tailscale_name=$(printf '%s' "$status_json" | jq -r '.Self.DNSName // .Self.HostName // empty' 2>/dev/null | sed 's/[.]$//' || true)
tailscale_name=${tailscale_name:-$TAILSCALE_HOSTNAME}

node_json=$(HEADSCALE_URL="$HEADSCALE_LOGIN_SERVER" "$HEADSCALE_API" raw GET /node)
node_id=$(printf '%s' "$node_json" | jq -r --arg name "$TAILSCALE_HOSTNAME" '
    .nodes[]?
    | select(.givenName == $name or .name == $name or (.name | startswith($name)))
    | .id
' | head -1)

if [ -n "$node_id" ]; then
    echo "Headscale node id: $node_id"
else
    echo "WARNING: could not find Headscale node for $TAILSCALE_HOSTNAME yet." >&2
fi

if [ -n "$node_id" ] && { [ "$HEADSCALE_APPROVE_ROUTES" = "true" ] || [ "$HEADSCALE_APPROVE_ROUTES" = "1" ] || [ "$HEADSCALE_APPROVE_ROUTES" = "yes" ]; }; then
    echo ">> Approving advertised routes in Headscale..."
    "$HEADSCALE_API" approve-routes node="$node_id" routes="$TAILSCALE_ADVERTISE_ROUTES" >/dev/null
fi

echo ">> Current Headscale routes..."
HEADSCALE_URL="$HEADSCALE_LOGIN_SERVER" "$HEADSCALE_API" routes || true

if [ -x "$INVENTORY" ]; then
    "$INVENTORY" upsert-device "$DEVICE_NAME" hostname="$PI_HOST" pi_host="$PI_HOST" ssh_user="$PI_USER" tailscale_name="$TAILSCALE_HOSTNAME" tailscale_ips="$tailscale_ips" advertised_routes="$TAILSCALE_ADVERTISE_ROUTES" status=active notes=phase10
    "$INVENTORY" phase "$DEVICE_NAME" phase10-headscale-routes complete detail="$TAILSCALE_HOSTNAME $TAILSCALE_ADVERTISE_ROUTES"
fi

echo
echo "Phase 10 finished."
echo "Tailscale IPs: ${tailscale_ips:-unknown}"
