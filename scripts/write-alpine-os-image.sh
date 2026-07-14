#!/bin/sh
set -eu

IMAGE=${1:-}
TARGET=${2:-}

if [ -z "$IMAGE" ] || [ -z "$TARGET" ]; then
    echo "Usage: CONFIRM_WRITE_DEVICE=/dev/sdX $0 /path/localbox-alpine-rpi.img.gz /dev/sdX"
    echo "WARNING: this overwrites the entire target device."
    exit 1
fi

if [ ! -f "$IMAGE" ]; then
    echo "ERROR: image not found: $IMAGE"
    exit 1
fi

if [ ! -b "$TARGET" ]; then
    echo "ERROR: target is not a block device: $TARGET"
    exit 1
fi

case "$TARGET" in
    /dev/sd[a-z]|/dev/mmcblk[0-9]|/dev/nvme[0-9]n[0-9]) ;;
    *)
        echo "ERROR: target must be a whole disk, not a partition: $TARGET"
        echo "Examples: /dev/sdf, /dev/mmcblk0"
        exit 1
        ;;
esac

if mount | grep -q "^$TARGET"; then
    echo "ERROR: target or one of its partitions is mounted:"
    mount | grep "^$TARGET" || true
    exit 1
fi

if lsblk -n -o MOUNTPOINT "$TARGET" | grep -q '/'; then
    echo "ERROR: target has mounted partitions:"
    lsblk "$TARGET"
    exit 1
fi

if [ "${CONFIRM_WRITE_DEVICE:-}" != "$TARGET" ]; then
    echo "Refusing to overwrite $TARGET without explicit confirmation."
    echo "Run:"
    echo "  CONFIRM_WRITE_DEVICE=$TARGET $0 $IMAGE $TARGET"
    exit 1
fi

echo "About to overwrite:"
lsblk "$TARGET"
echo "Image: $IMAGE"

if [ -f "$IMAGE.sha256" ]; then
    echo ">> Verifying checksum..."
    sha256sum -c "$IMAGE.sha256"
fi

echo ">> Writing image..."
case "$IMAGE" in
    *.gz) gzip -dc "$IMAGE" | dd of="$TARGET" bs=4M conv=fsync status=progress ;;
    *) dd if="$IMAGE" of="$TARGET" bs=4M conv=fsync status=progress ;;
esac

sync
echo "Wrote image to $TARGET"
echo "Unplug and replug the SD card before mounting or editing it."
