#!/bin/sh
set -eu

ACTION=${1:-inventory}
PI_HOST=${PI_HOST:-192.168.0.7}
PI_USER=${PI_USER:-root}
DATA_DEVICE=${DATA_DEVICE:-}
LABEL=${LABEL:-LOCALBOXDATA}
MOUNTPOINT=${MOUNTPOINT:-/mnt/data}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/localbox_phase4_known_hosts -o ConnectTimeout=10"}

usage() {
    cat <<EOF
Usage:
  PI_HOST=192.168.0.7 $0 inventory
  PI_HOST=192.168.0.7 DATA_DEVICE=/dev/sda $0 repair
  PI_HOST=192.168.0.7 DATA_DEVICE=/dev/sda CONFIRM_DATA_DEVICE=/dev/sda $0 format

Phase 4 formats and mounts the USB data drive at $MOUNTPOINT.
The format action erases DATA_DEVICE.
EOF
}

case "$ACTION" in
    inventory|repair|format) ;;
    -h|--help|help) usage; exit 0 ;;
    *) usage; exit 1 ;;
esac

quote_sh() {
    printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}

echo "Phase 4 target: $PI_USER@$PI_HOST"
echo "Action:         $ACTION"
[ -z "$DATA_DEVICE" ] || echo "Data device:    $DATA_DEVICE"

ssh $SSH_OPTS "$PI_USER@$PI_HOST" \
    "ACTION=$(quote_sh "$ACTION") DATA_DEVICE=$(quote_sh "$DATA_DEVICE") CONFIRM_DATA_DEVICE=$(quote_sh "${CONFIRM_DATA_DEVICE:-}") LABEL=$(quote_sh "$LABEL") MOUNTPOINT=$(quote_sh "$MOUNTPOINT") sh -s" <<'REMOTE'
set -eu

find_apk_cache() {
    for dir in /media/mmcblk0p1/apks /boot/apks /media/*/apks; do
        if [ -d "$dir/armv7" ]; then
            printf '%s\n' "$dir"
            return 0
        fi
    done
    return 1
}

install_local_apks() {
    cache=$1
    shift
    for pattern in "$@"; do
        set -- "$cache"/armv7/$pattern
        [ -f "$1" ] || continue
        apk add --force-non-repository "$@"
    done
}

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

show_inventory() {
    echo ">> Block devices"
    for b in /sys/block/*; do
        name=${b##*/}
        case "$name" in loop*|ram*) continue ;; esac
        sectors=$(cat "$b/size" 2>/dev/null || echo 0)
        bytes=$((sectors * 512))
        removable=$(cat "$b/removable" 2>/dev/null || echo "?")
        devpath=$(readlink -f "$b/device" 2>/dev/null || true)
        vendor=$(cat "$b/device/vendor" 2>/dev/null | sed 's/^[[:space:]]*//;s/[[:space:]]*$//' || true)
        model=$(cat "$b/device/model" 2>/dev/null | sed 's/^[[:space:]]*//;s/[[:space:]]*$//' || true)
        printf '/dev/%s bytes=%s removable=%s vendor="%s" model="%s" path="%s"\n' "$name" "$bytes" "$removable" "$vendor" "$model" "$devpath"
        for part in "$b"/"$name"*; do
            [ -e "$part" ] || continue
            [ "$part" = "$b/$name" ] && continue
            p=${part##*/}
            blkid "/dev/$p" 2>/dev/null || true
        done
    done
    echo ">> Current mounts"
    mount | grep -E ' /mnt/data | /media/|/dev/sd|/dev/mmc' || true
}

partition_for() {
    case "$1" in
        /dev/mmcblk*|/dev/nvme*) printf '%sp1\n' "$1" ;;
        *) printf '%s1\n' "$1" ;;
    esac
}

wait_for_partition() {
    partition=$1
    n=0
    while [ "$n" -lt 10 ]; do
        [ -b "$partition" ] && return 0
        partprobe "$DATA_DEVICE" 2>/dev/null || true
        partx -u "$DATA_DEVICE" 2>/dev/null || true
        mdev -s 2>/dev/null || true
        sleep 1
        n=$((n + 1))
    done
    return 1
}

if [ "$ACTION" = "inventory" ]; then
    show_inventory
    exit 0
fi

[ -n "$DATA_DEVICE" ] || {
    echo "ERROR: DATA_DEVICE is required for $ACTION" >&2
    exit 1
}
[ -b "$DATA_DEVICE" ] || {
    echo "ERROR: not a block device: $DATA_DEVICE" >&2
    exit 1
}
case "$DATA_DEVICE" in
    /dev/sd[a-z]) ;;
    *)
        echo "ERROR: data device must be a USB whole disk like /dev/sda" >&2
        exit 1
        ;;
esac

sys=/sys/block/${DATA_DEVICE##*/}
[ -d "$sys" ] || {
    echo "ERROR: missing sysfs entry for $DATA_DEVICE" >&2
    exit 1
}
removable=$(cat "$sys/removable" 2>/dev/null || echo 0)
devpath=$(readlink -f "$sys/device" 2>/dev/null || true)
case "$devpath" in
    *usb*) ;;
    *)
        echo "ERROR: refusing non-USB-looking device: $DATA_DEVICE path=$devpath" >&2
        exit 1
        ;;
esac
[ "$removable" = "1" ] || {
    echo "ERROR: refusing non-removable device: $DATA_DEVICE" >&2
    exit 1
}

mount_data_partition() {
    partition=$1
    uuid=$(blkid "$partition" | sed -n 's/.*UUID="\([^"]*\)".*/\1/p')
    [ -n "$uuid" ] || {
        echo "ERROR: could not read UUID for $partition" >&2
        exit 1
    }

    mkdir -p "$MOUNTPOINT"
    if grep -q "[[:space:]]$MOUNTPOINT[[:space:]]" /etc/fstab 2>/dev/null; then
        awk -v mp="$MOUNTPOINT" '$2 != mp { print }' /etc/fstab > /etc/fstab.new
        mv /etc/fstab.new /etc/fstab
    fi
    printf 'UUID=%s %s ext4 defaults 0 2\n' "$uuid" "$MOUNTPOINT" >> /etc/fstab

    mountpoint -q "$MOUNTPOINT" || mount "$partition" "$MOUNTPOINT"
    mkdir -p "$MOUNTPOINT/localbox" "$MOUNTPOINT/backups"

    echo ">> Persisting fstab and mountpoint..."
    ensure_lbu_media
    mount -o remount,rw /media/mmcblk0p1 2>/dev/null || true
    lbu add /etc/fstab
    lbu add /etc/lbu/lbu.conf
    lbu add /etc/apk/world 2>/dev/null || true
    lbu commit -d
    mount -o remount,ro /media/mmcblk0p1 2>/dev/null || true

    echo "Device:    $DATA_DEVICE"
    echo "Partition: $partition"
    echo "UUID:      $uuid"
    echo "Mount:     $MOUNTPOINT"
    df -h "$MOUNTPOINT"
}

if [ "$ACTION" = "repair" ]; then
    PARTITION=$(partition_for "$DATA_DEVICE")
    wait_for_partition "$PARTITION" || {
        echo "ERROR: expected partition not found: $PARTITION" >&2
        partx -l "$DATA_DEVICE" 2>/dev/null || true
        ls -l "$DATA_DEVICE"* 2>/dev/null || true
        exit 1
    }
    mount_data_partition "$PARTITION"
    echo ">> Phase 4 repair complete."
    exit 0
fi

[ "$CONFIRM_DATA_DEVICE" = "$DATA_DEVICE" ] || {
    echo "Refusing to erase $DATA_DEVICE without confirmation." >&2
    echo "Run with: DATA_DEVICE=$DATA_DEVICE CONFIRM_DATA_DEVICE=$DATA_DEVICE" >&2
    exit 1
}

echo ">> Installing disk tools if needed..."
APK_CACHE=$(find_apk_cache)
install_local_apks "$APK_CACHE" \
    'libuuid-*.apk' \
    'libblkid-*.apk' \
    'e2fsprogs-libs-*.apk' \
    'e2fsprogs-*.apk' \
    'e2fsprogs-extra-*.apk' \
    'sfdisk-*.apk' \
    'partx-*.apk'

command -v mkfs.ext4 >/dev/null 2>&1 || {
    echo "ERROR: mkfs.ext4 is still unavailable" >&2
    exit 1
}
command -v sfdisk >/dev/null 2>&1 || {
    echo "ERROR: sfdisk is still unavailable" >&2
    exit 1
}

echo ">> Formatting $DATA_DEVICE as $LABEL"
show_inventory
for mnt in $(grep "^$DATA_DEVICE" /proc/mounts | awk '{print $2}'); do
    umount "$mnt" 2>/dev/null || true
done

wipefs -a "$DATA_DEVICE" 2>/dev/null || true
dd if=/dev/zero of="$DATA_DEVICE" bs=1M count=8 conv=fsync 2>/dev/null || true
printf ',,L,*\n' | sfdisk "$DATA_DEVICE"
partprobe "$DATA_DEVICE" 2>/dev/null || true
partx -u "$DATA_DEVICE" 2>/dev/null || true
mdev -s 2>/dev/null || true
sleep 2

PARTITION=$(partition_for "$DATA_DEVICE")
wait_for_partition "$PARTITION" || {
    echo "ERROR: expected partition not found: $PARTITION" >&2
    partx -l "$DATA_DEVICE" 2>/dev/null || true
    ls -l "$DATA_DEVICE"* 2>/dev/null || true
    exit 1
}

wipefs -a "$PARTITION" 2>/dev/null || true
mkfs.ext4 -F -L "$LABEL" "$PARTITION"
mount_data_partition "$PARTITION"

echo ">> Phase 4 complete."
REMOTE

echo
echo "Phase 4 finished."
