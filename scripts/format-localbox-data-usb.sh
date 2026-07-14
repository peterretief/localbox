#!/bin/sh
set -eu

TARGET=${1:-}
LABEL=${LABEL:-LOCALBOXDATA}

if [ -z "$TARGET" ]; then
    echo "Usage: sudo CONFIRM_FORMAT_DEVICE=/dev/sdX $0 /dev/sdX"
    echo "WARNING: this erases and formats the whole target device as ext4."
    exit 1
fi

if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: run as root with sudo"
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
        echo "Examples: /dev/sdg, /dev/mmcblk0"
        exit 1
        ;;
esac

if [ "${CONFIRM_FORMAT_DEVICE:-}" != "$TARGET" ]; then
    echo "Refusing to format $TARGET without explicit confirmation."
    echo "Run:"
    echo "  sudo CONFIRM_FORMAT_DEVICE=$TARGET $0 $TARGET"
    exit 1
fi

echo "About to erase and format:"
lsblk "$TARGET"

echo ">> Unmounting mounted partitions..."
for partition in $(lsblk -ln -o PATH "$TARGET" | tail -n +2); do
    umount "$partition" 2>/dev/null || true
done

echo ">> Creating single ext4 partition..."
wipefs -a "$TARGET"
parted -s "$TARGET" mklabel gpt
parted -s "$TARGET" mkpart primary ext4 1MiB 100%
partprobe "$TARGET" 2>/dev/null || true
sleep 2

PARTITION="${TARGET}1"
case "$TARGET" in
    /dev/mmcblk*|/dev/nvme*) PARTITION="${TARGET}p1" ;;
esac

if [ ! -b "$PARTITION" ]; then
    echo "ERROR: expected partition not found after partitioning: $PARTITION"
    lsblk "$TARGET"
    exit 1
fi

echo ">> Formatting $PARTITION as ext4 label $LABEL..."
mkfs.ext4 -F -L "$LABEL" "$PARTITION"
sync

echo "Formatted LocalBox data USB:"
lsblk -f "$TARGET"
echo
echo "Use this fstab entry on the Pi after verifying UUID:"
UUID=$(blkid -s UUID -o value "$PARTITION")
echo "UUID=$UUID /mnt/data ext4 defaults 0 2"
