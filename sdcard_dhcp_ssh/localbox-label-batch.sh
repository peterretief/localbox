#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
COUNT=${1:-}
DEVICE_PREFIX=${DEVICE_PREFIX:-localbox}
ASSET_PREFIX=${ASSET_PREFIX:-LBX}
INVENTORY=${INVENTORY:-$SCRIPT_DIR/localbox-inventory.sh}
LABEL=${LABEL:-$SCRIPT_DIR/localbox-label.sh}
CREATE_DEVICE=${CREATE_DEVICE:-$SCRIPT_DIR/localbox-device-create.sh}

if [ -z "$COUNT" ]; then
    echo "Usage: $0 COUNT" >&2
    echo "Optional: DEVICE_PREFIX=localbox ASSET_PREFIX=LBX $0 COUNT" >&2
    exit 1
fi

case "$COUNT" in
    *[!0-9]*|"")
        echo "ERROR: count must be a positive integer" >&2
        exit 1
        ;;
esac

i=0
while [ "$i" -lt "$COUNT" ]; do
    output=$(DEVICE_PREFIX="$DEVICE_PREFIX" ASSET_PREFIX="$ASSET_PREFIX" STATUS=reserved "$CREATE_DEVICE")
    device=$(printf '%s\n' "$output" | awk -F: '/device_name:/ {gsub(/^[ \t]+|[ \t]+$/, "", $2); print $2; exit}')
    asset=$(printf '%s\n' "$output" | awk -F: '/asset_tag:/ {gsub(/^[ \t]+|[ \t]+$/, "", $2); print $2; exit}')
    label=$(printf '%s\n' "$output" | awk -F: '/label:/ {sub(/^[ \t]+/, "", $2); print $2; exit}')
    printf '%s %s %s\n' "$device" "$asset" "$label"
    i=$((i + 1))
done
