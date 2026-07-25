#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
INVENTORY=${INVENTORY:-$SCRIPT_DIR/localbox-inventory.sh}

DEVICE_NAME=${DEVICE_NAME:-}
LOCALBOX_TUNNEL=${LOCALBOX_TUNNEL:-}
LOCALBOX_TUNNEL_ID=${LOCALBOX_TUNNEL_ID:-}
LOCALBOX_TUNNEL_STATUS=${LOCALBOX_TUNNEL_STATUS:-planned}
LOCALBOX_TUNNEL_ORIGIN_IP=${LOCALBOX_TUNNEL_ORIGIN_IP:-}
LOCALBOX_TUNNEL_VERSION=${LOCALBOX_TUNNEL_VERSION:-}

if [ -z "$DEVICE_NAME" ] || [ -z "$LOCALBOX_TUNNEL" ]; then
    cat >&2 <<EOF
Usage:
  DEVICE_NAME=localbox-001 \\
  LOCALBOX_TUNNEL=localbox-tunnel-001 \\
  LOCALBOX_TUNNEL_ID=optional-cloudflare-uuid \\
  $0

Every Pi must have its own Cloudflare tunnel recorded in the local inventory.
EOF
    exit 1
fi

if [ ! -x "$INVENTORY" ]; then
    echo "ERROR: inventory tool not found: $INVENTORY" >&2
    exit 1
fi

"$INVENTORY" upsert-device "$DEVICE_NAME" status=active
"$INVENTORY" upsert-tunnel "$LOCALBOX_TUNNEL" \
    tunnel_id="$LOCALBOX_TUNNEL_ID" \
    device_name="$DEVICE_NAME" \
    status="$LOCALBOX_TUNNEL_STATUS" \
    origin_ip="$LOCALBOX_TUNNEL_ORIGIN_IP" \
    connector_version="$LOCALBOX_TUNNEL_VERSION"
"$INVENTORY" phase "$DEVICE_NAME" phase9a-record-device-tunnel complete detail="$LOCALBOX_TUNNEL $LOCALBOX_TUNNEL_ID"

echo "Recorded tunnel ownership:"
"$INVENTORY" list tunnels
