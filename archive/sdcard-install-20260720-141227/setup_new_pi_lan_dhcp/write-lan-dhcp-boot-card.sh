#!/bin/sh
set -eu

TARBALL=${1:-}
TARGET=${2:-}
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
IFACES=${IFACES:-eth0 end0}
BASE_APKOVL=${BASE_APKOVL:-"$PROJECT_DIR/setup_new_pi/headless.apkovl.tar.gz"}
OVERLAY_NAME=${OVERLAY_NAME:-headless.apkovl.tar.gz}
MAX_BOOT_CARD_BYTES=${MAX_BOOT_CARD_BYTES:-137438953472}

usage() {
    cat <<EOF
Usage: CONFIRM_BOOT_DEVICE=/dev/sdX CONFIRM_BOOT_SERIAL=SERIAL $0 alpine-rpi-*.tar.gz /dev/sdX

This erases TARGET, creates one FAT32 Alpine boot partition, extracts the
Alpine Raspberry Pi tarball, seeds the headless overlay, and patches wired DHCP.

It seeds the known-working headless Alpine overlay and patches wired LAN DHCP.
It does not install LocalBox, first-boot provisioning, Wi-Fi, or rescue services.

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
    echo "ERROR: Alpine tarball not found: $TARBALL" >&2
    exit 1
fi

if [ ! -f "$BASE_APKOVL" ]; then
    echo "ERROR: base headless overlay not found: $BASE_APKOVL" >&2
    exit 1
fi

if [ ! -b "$TARGET" ]; then
    echo "ERROR: target is not a block device: $TARGET" >&2
    exit 1
fi

case "$TARGET" in
    /dev/sd[a-z]|/dev/mmcblk[0-9]|/dev/nvme[0-9]n[0-9]) ;;
    *)
        echo "ERROR: target must be a whole disk, not a partition: $TARGET" >&2
        echo "Examples: /dev/sdf, /dev/mmcblk0" >&2
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

if [ "$TARGET_RO" != "0" ]; then
    echo "ERROR: target is read-only: $TARGET" >&2
    exit 1
fi

if [ "$TARGET_RM" != "1" ]; then
    echo "ERROR: refusing non-removable disk: $TARGET" >&2
    echo "Model: $TARGET_MODEL" >&2
    echo "Serial: ${TARGET_SERIAL:-unknown}" >&2
    exit 1
fi

if [ "$TARGET_SIZE" -gt "$MAX_BOOT_CARD_BYTES" ]; then
    echo "ERROR: refusing oversized boot-card target: $TARGET" >&2
    echo "Size bytes: $TARGET_SIZE" >&2
    echo "Limit bytes: $MAX_BOOT_CARD_BYTES" >&2
    exit 1
fi

if [ -z "$TARGET_SERIAL" ]; then
    echo "ERROR: target has no reported serial; refusing destructive operation." >&2
    echo "Use a card reader that reports serial/device identity." >&2
    exit 1
fi

if [ "${CONFIRM_BOOT_DEVICE:-}" != "$TARGET" ] || [ "${CONFIRM_BOOT_SERIAL:-}" != "$TARGET_SERIAL" ]; then
    echo "Refusing to erase $TARGET without explicit device and serial confirmation."
    echo "Device:    $TARGET"
    echo "Model:     $TARGET_MODEL"
    echo "Serial:    $TARGET_SERIAL"
    echo "Transport: ${TARGET_TRAN:-unknown}"
    echo
    echo "Run:"
    echo "  CONFIRM_BOOT_DEVICE=$TARGET CONFIRM_BOOT_SERIAL=$TARGET_SERIAL $0 $TARBALL $TARGET"
    exit 1
fi

if [ "$(id -u)" -ne 0 ]; then
    if ! sudo -v; then
        echo "ERROR: sudo authentication failed; no changes were made to $TARGET." >&2
        echo "Run this from an interactive terminal so sudo can prompt for your password." >&2
        exit 1
    fi
fi

partition_for() {
    case "$1" in
        /dev/mmcblk*|/dev/nvme*) printf '%sp1\n' "$1" ;;
        *) printf '%s1\n' "$1" ;;
    esac
}

write_interfaces_file() {
    output=$1

    {
        printf '%s\n' 'auto lo'
        printf '%s\n\n' 'iface lo inet loopback'

        for iface in $IFACES; do
            [ -n "$iface" ] || continue
            printf 'auto %s\n' "$iface"
            printf 'iface %s inet dhcp\n\n' "$iface"
        done
    } > "$output"
}

patch_dhcp_overlay() {
    overlay_dir=$1
    overlay_path=$2

    tar -xzf "$overlay_path" -C "$overlay_dir"

    mkdir -p "$overlay_dir/etc/network" "$overlay_dir/etc/runlevels/default"
    write_interfaces_file "$overlay_dir/etc/network/interfaces"
    ln -sfn ../../init.d/networking "$overlay_dir/etc/runlevels/default/networking"

    (
        cd "$overlay_dir"
        tar --owner=0 --group=0 --numeric-owner -czf "$overlay_path" .
    )
}

BOOT_PART=$(partition_for "$TARGET")
MOUNT_DIR=$(mktemp -d /tmp/rpi-lan-dhcp-boot.XXXXXX)
OVERLAY_DIR=$(mktemp -d /tmp/rpi-lan-dhcp-overlay.XXXXXX)
MOUNTED=0

cleanup() {
    if [ "${MOUNTED:-0}" = "1" ] && [ -n "${MOUNT_DIR:-}" ] && mountpoint -q "$MOUNT_DIR"; then
        sudo umount "$MOUNT_DIR"
    fi
    [ -z "${MOUNT_DIR:-}" ] || rmdir "$MOUNT_DIR" 2>/dev/null || true
    [ -z "${OVERLAY_DIR:-}" ] || rm -rf "$OVERLAY_DIR"
}
trap cleanup EXIT

echo "About to erase and rebuild headless LAN DHCP Raspberry Pi boot card:"
lsblk "$TARGET"
echo "Model:          $TARGET_MODEL"
echo "Serial:         $TARGET_SERIAL"
echo "Transport:      ${TARGET_TRAN:-unknown}"
echo "Removable:      $TARGET_RM"
echo "Alpine tarball: $TARBALL"
echo "Base overlay:    $BASE_APKOVL"
echo "DHCP ifaces:    $IFACES"

echo ">> Unmounting existing target mounts..."
for target_mount in $(lsblk -nr -o MOUNTPOINT "$TARGET" | sed '/^$/d'); do
    sudo umount "$target_mount"
done

echo ">> Partitioning $TARGET..."
sudo wipefs -a "$TARGET"
sudo parted -s "$TARGET" mklabel msdos
sudo parted -s "$TARGET" mkpart primary fat32 1MiB 100%
sudo parted -s "$TARGET" set 1 boot on
sudo partprobe "$TARGET" 2>/dev/null || true
sleep 2

if [ ! -b "$BOOT_PART" ]; then
    echo "ERROR: expected boot partition was not created: $BOOT_PART" >&2
    lsblk "$TARGET"
    exit 1
fi

echo ">> Formatting $BOOT_PART as FAT32..."
sudo mkfs.vfat -F 32 -n ALPINE "$BOOT_PART"

echo ">> Mounting boot partition..."
sudo mount -t vfat -o rw,uid="$(id -u)",gid="$(id -g)",flush "$BOOT_PART" "$MOUNT_DIR"
MOUNTED=1

echo ">> Extracting Alpine Raspberry Pi tarball..."
tar --no-same-owner -xzf "$TARBALL" -C "$MOUNT_DIR"

echo ">> Seeding headless Alpine overlay..."
cp "$BASE_APKOVL" "$MOUNT_DIR/$OVERLAY_NAME"

echo ">> Patching overlay for LAN DHCP..."
patch_dhcp_overlay "$OVERLAY_DIR" "$MOUNT_DIR/$OVERLAY_NAME"

echo ">> Writing boot-partition DHCP interfaces file..."
write_interfaces_file "$MOUNT_DIR/interfaces"

echo ">> Syncing writes..."
sync
echo ">> Unmounting boot partition..."
sudo umount "$MOUNT_DIR"
MOUNTED=0
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

echo "Raspberry Pi headless LAN DHCP boot card prepared."
echo "Device:    $TARGET"
echo "Partition: $BOOT_PART"
echo "Overlay:   $OVERLAY_NAME"
