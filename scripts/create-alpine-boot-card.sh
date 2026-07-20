#!/bin/sh
set -eu

TARBALL=${1:-}
TARGET=${2:-}
FIRSTBOOT_CONFIG=${3:-}
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
BASE_APKOVL=${BASE_APKOVL:-"$PROJECT_DIR/setup_new_pi/headless.apkovl.tar.gz"}

usage() {
    cat <<EOF
Usage: CONFIRM_BOOT_DEVICE=/dev/sdX CONFIRM_BOOT_SERIAL=SERIAL $0 alpine-rpi-*.tar.gz /dev/sdX [localbox-firstboot.conf]

This erases TARGET, creates one FAT32 Alpine boot partition, extracts the
Alpine Raspberry Pi tarball, seeds the LocalBox headless overlay, and patches
the card for DHCP, SSH, and LocalBox first boot.

Safety defaults:
  - target must be removable media;
  - target must be 128GB or smaller;
  - CONFIRM_BOOT_SERIAL must match lsblk SERIAL.
EOF
}

if [ -z "$TARBALL" ] || [ -z "$TARGET" ]; then
    usage
    exit 1
fi

if [ ! -f "$TARBALL" ]; then
    echo "ERROR: Alpine tarball not found: $TARBALL"
    exit 1
fi

if [ ! -f "$BASE_APKOVL" ]; then
    echo "ERROR: base overlay not found: $BASE_APKOVL"
    exit 1
fi

if [ -n "$FIRSTBOOT_CONFIG" ] && [ ! -f "$FIRSTBOOT_CONFIG" ]; then
    echo "ERROR: first-boot config not found: $FIRSTBOOT_CONFIG"
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

TARGET_SIZE=$(lsblk -dn -b -o SIZE "$TARGET" | tr -d '[:space:]')
TARGET_MODEL=$(lsblk -dn -o MODEL "$TARGET" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
TARGET_SERIAL=$(lsblk -dn -o SERIAL "$TARGET" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
TARGET_TRAN=$(lsblk -dn -o TRAN "$TARGET" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
TARGET_RM=$(lsblk -dn -o RM "$TARGET" | tr -d '[:space:]')
TARGET_RO=$(lsblk -dn -o RO "$TARGET" | tr -d '[:space:]')
TARGET_MODEL=${TARGET_MODEL:-unknown}
MAX_BOOT_CARD_BYTES=${MAX_BOOT_CARD_BYTES:-137438953472}

if [ "$TARGET_RO" != "0" ]; then
    echo "ERROR: target is read-only: $TARGET"
    exit 1
fi

if [ "$TARGET_RM" != "1" ]; then
    echo "ERROR: refusing non-removable disk: $TARGET"
    echo "Model: $TARGET_MODEL"
    echo "Serial: ${TARGET_SERIAL:-unknown}"
    echo "This command is only for SD cards/removable media."
    exit 1
fi

if [ "$TARGET_SIZE" -gt "$MAX_BOOT_CARD_BYTES" ]; then
    echo "ERROR: refusing oversized boot-card target: $TARGET"
    echo "Size bytes: $TARGET_SIZE"
    echo "Limit bytes: $MAX_BOOT_CARD_BYTES"
    echo "This protects backup drives from being mistaken for SD cards."
    exit 1
fi

if [ "${CONFIRM_BOOT_DEVICE:-}" != "$TARGET" ]; then
    echo "Refusing to erase $TARGET without explicit confirmation."
    echo "Run:"
    echo "  CONFIRM_BOOT_DEVICE=$TARGET CONFIRM_BOOT_SERIAL=${TARGET_SERIAL:-unknown} $0 $TARBALL $TARGET ${FIRSTBOOT_CONFIG:-}"
    exit 1
fi

if [ -z "$TARGET_SERIAL" ]; then
    echo "ERROR: target has no reported serial; refusing destructive operation."
    echo "Use a card reader that reports serial/device identity."
    exit 1
fi

if [ "${CONFIRM_BOOT_SERIAL:-}" != "$TARGET_SERIAL" ]; then
    echo "Refusing to erase $TARGET without matching serial confirmation."
    echo "Device: $TARGET"
    echo "Model:  $TARGET_MODEL"
    echo "Serial: $TARGET_SERIAL"
    echo "Run:"
    echo "  CONFIRM_BOOT_DEVICE=$TARGET CONFIRM_BOOT_SERIAL=$TARGET_SERIAL $0 $TARBALL $TARGET ${FIRSTBOOT_CONFIG:-}"
    exit 1
fi

partition_for() {
    case "$1" in
        /dev/mmcblk*|/dev/nvme*) printf '%sp1\n' "$1" ;;
        *) printf '%s1\n' "$1" ;;
    esac
}

BOOT_PART=$(partition_for "$TARGET")
MOUNT_DIR=$(mktemp -d /tmp/localbox-piboot.XXXXXX)
cleanup() {
    if [ -n "${MOUNT_DIR:-}" ] && mountpoint -q "$MOUNT_DIR"; then
        sudo umount "$MOUNT_DIR"
    fi
    [ -z "${MOUNT_DIR:-}" ] || rmdir "$MOUNT_DIR" 2>/dev/null || true
}
trap cleanup EXIT

echo "About to erase and rebuild Alpine boot card:"
lsblk "$TARGET"
echo "Model:          $TARGET_MODEL"
echo "Serial:         $TARGET_SERIAL"
echo "Transport:      ${TARGET_TRAN:-unknown}"
echo "Removable:      $TARGET_RM"
echo "Alpine tarball: $TARBALL"
echo "Base overlay:    $BASE_APKOVL"

echo ">> Unmounting existing target mounts..."
for mountpoint in $(lsblk -nr -o MOUNTPOINT "$TARGET" | sed '/^$/d'); do
    sudo umount "$mountpoint"
done

echo ">> Partitioning $TARGET..."
sudo wipefs -a "$TARGET"
sudo parted -s "$TARGET" mklabel msdos
sudo parted -s "$TARGET" mkpart primary fat32 1MiB 100%
sudo parted -s "$TARGET" set 1 boot on
sudo partprobe "$TARGET" 2>/dev/null || true
sleep 2

if [ ! -b "$BOOT_PART" ]; then
    echo "ERROR: expected boot partition was not created: $BOOT_PART"
    lsblk "$TARGET"
    exit 1
fi

echo ">> Formatting $BOOT_PART as FAT32..."
sudo mkfs.vfat -F 32 -n ALPINE2 "$BOOT_PART"

echo ">> Mounting boot partition..."
sudo mount -t vfat -o rw,uid="$(id -u)",gid="$(id -g)",flush "$BOOT_PART" "$MOUNT_DIR"

echo ">> Extracting Alpine Raspberry Pi tarball..."
tar --no-same-owner -xzf "$TARBALL" -C "$MOUNT_DIR"

echo ">> Seeding LocalBox headless overlay..."
cp "$BASE_APKOVL" "$MOUNT_DIR/headless.apkovl.tar.gz"

echo ">> Patching boot card..."
BOOT_MOUNT="$MOUNT_DIR" FIRSTBOOT_CONFIG="$FIRSTBOOT_CONFIG" "$SCRIPT_DIR/prepare-pi-boot-card.sh"

echo ">> Syncing writes..."
sync
echo ">> Unmounting boot partition..."
sudo umount "$MOUNT_DIR"
rmdir "$MOUNT_DIR" 2>/dev/null || true
MOUNT_DIR=""

if command -v udisksctl >/dev/null 2>&1; then
    echo ">> Powering off $TARGET..."
    if udisksctl power-off -b "$TARGET" >/dev/null 2>&1; then
        echo "SD card reader powered off."
    else
        echo "Could not power off $TARGET automatically. The partition is unmounted; wait for activity to stop before removing it."
    fi
else
    echo "udisksctl not found. The partition is unmounted; wait for activity to stop before removing the card."
fi

echo "Fresh Alpine boot card prepared."
echo "Device:     $TARGET"
echo "Partition:  $BOOT_PART"
echo "Remove the SD card and boot it in the Raspberry Pi."
