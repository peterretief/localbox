#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
INVENTORY=${INVENTORY:-$SCRIPT_DIR/localbox-inventory.sh}
LABEL=${LABEL:-$SCRIPT_DIR/localbox-label.sh}

DEVICE_NAME=${DEVICE_NAME:-}
DEVICE_PREFIX=${DEVICE_PREFIX:-localbox}
ASSET_PREFIX=${ASSET_PREFIX:-LBX}
STATUS=${STATUS:-}
HOSTNAME=${HOSTNAME:-}
PI_HOST=${PI_HOST:-}

if [ -z "$DEVICE_NAME" ]; then
    DEVICE_NAME=$("$INVENTORY" next-name device "$DEVICE_PREFIX")
    STATUS=${STATUS:-reserved}
fi

suffix=$(expr "$DEVICE_NAME" : '.*-\([0-9][0-9]*\)$' 2>/dev/null || true)
if [ -z "$suffix" ]; then
    echo "ERROR: DEVICE_NAME must end in a numeric suffix, e.g. localbox-001" >&2
    exit 1
fi

ASSET_TAG=${ASSET_TAG:-$(printf '%s-%04d' "$ASSET_PREFIX" "$suffix")}
EXPECTED_TUNNEL=${EXPECTED_TUNNEL:-$DEVICE_NAME-tunnel}
EXPECTED_WORKER=${EXPECTED_WORKER:-$DEVICE_NAME-mail-worker}
TAILSCALE_NAME=${TAILSCALE_NAME:-$DEVICE_NAME}

"$INVENTORY" upsert-device "$DEVICE_NAME" \
    asset_tag="$ASSET_TAG" \
    expected_tunnel="$EXPECTED_TUNNEL" \
    expected_worker="$EXPECTED_WORKER" \
    hostname="$HOSTNAME" \
    pi_host="$PI_HOST" \
    tailscale_name="$TAILSCALE_NAME" \
    status="$STATUS" \
    notes=device-created

label_path=$("$LABEL" "$DEVICE_NAME")

cat <<EOF
Device identity created:
  device_name:     $DEVICE_NAME
  asset_tag:       $ASSET_TAG
  tailscale_name:  $TAILSCALE_NAME
  tunnel_name:     $EXPECTED_TUNNEL
  worker_name:     $EXPECTED_WORKER
  label:           $label_path

Lookup:
  ./localbox-inventory.sh show-device $DEVICE_NAME
  ./localbox-inventory.sh lookup-tag $ASSET_TAG
EOF
