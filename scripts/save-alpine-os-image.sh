#!/bin/sh
set -eu

BOOT_MOUNT=${BOOT_MOUNT:-${1:-/mnt/piboot}}
OUT_DIR=${OUT_DIR:-${2:-./images}}
NAME=${NAME:-localbox-alpine-rpi}

if [ ! -d "$BOOT_MOUNT" ]; then
    echo "Usage: BOOT_MOUNT=/mnt/piboot OUT_DIR=./images $0"
    echo "ERROR: boot mount does not exist: $BOOT_MOUNT"
    exit 1
fi

if [ ! -f "$BOOT_MOUNT/.alpine-release" ] || ! ls "$BOOT_MOUNT"/*.apkovl.tar.gz >/dev/null 2>&1; then
    echo "ERROR: $BOOT_MOUNT does not look like an Alpine diskless Pi boot partition"
    exit 1
fi

SOURCE=$(findmnt -n -o SOURCE --target "$BOOT_MOUNT")
if [ -z "$SOURCE" ] || [ ! -b "$SOURCE" ]; then
    echo "ERROR: could not identify block device for $BOOT_MOUNT"
    exit 1
fi

PKNAME=$(lsblk -n -o PKNAME "$SOURCE" 2>/dev/null | head -1 | tr -d '[:space:]')
if [ -n "$PKNAME" ]; then
    DEVICE="/dev/$PKNAME"
else
    DEVICE="$SOURCE"
fi

if [ ! -b "$DEVICE" ]; then
    echo "ERROR: source device is not a block device: $DEVICE"
    exit 1
fi

mkdir -p "$OUT_DIR"
STAMP=$(date +%Y%m%d-%H%M%S)
IMAGE="$OUT_DIR/$NAME-$STAMP.img.gz"
META="$OUT_DIR/$NAME-$STAMP.txt"

echo "Saving full Alpine SD image"
echo "Boot mount: $BOOT_MOUNT"
echo "Source:     $DEVICE"
echo "Image:      $IMAGE"

{
    echo "name=$NAME"
    echo "created_at=$STAMP"
    echo "source_device=$DEVICE"
    echo "boot_mount=$BOOT_MOUNT"
    echo "alpine_release=$(cat "$BOOT_MOUNT/.alpine-release" 2>/dev/null || true)"
    echo "apkovl=$(basename "$(ls "$BOOT_MOUNT"/*.apkovl.tar.gz | head -1)")"
    echo
    lsblk -f "$DEVICE"
} > "$META"

dd if="$DEVICE" bs=4M status=progress | gzip -c > "$IMAGE"
sha256sum "$IMAGE" > "$IMAGE.sha256"
sync

echo "Saved:"
echo "  $IMAGE"
echo "  $IMAGE.sha256"
echo "  $META"
