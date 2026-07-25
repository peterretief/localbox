#!/bin/sh
set -eu

TARBALL=${1:-}
TARGET=${2:-}
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
BASE_APKOVL=${BASE_APKOVL:-"$PROJECT_DIR/setup_new_pi/headless.apkovl.tar.gz"}
AUTHORIZED_KEYS=${AUTHORIZED_KEYS:-}
IFACES=${IFACES:-eth0 end0}
RESCUE_IP=${RESCUE_IP:-192.168.0.250/24}
MAX_CARD_BYTES=${MAX_CARD_BYTES:-137438953472}

usage() {
    cat <<EOF
Usage: CONFIRM_BOOT_DEVICE=/dev/sdX CONFIRM_BOOT_SERIAL=SERIAL $0 alpine-rpi-*.tar.gz /dev/sdX

Formats TARGET as a single FAT32 Alpine Raspberry Pi boot card with:
  - Alpine Raspberry Pi files from the tarball
  - headless.apkovl.tar.gz
  - wired DHCP config for: $IFACES
  - rescue static IP alias: $RESCUE_IP
  - optional SSH authorized_keys from AUTHORIZED_KEYS=/path/to/key.pub

This does not install LocalBox or first-boot provisioning.
EOF
}

die() {
    echo "ERROR: $*" >&2
    exit 1
}

if [ -z "$TARBALL" ] || [ -z "$TARGET" ]; then
    usage
    exit 1
fi

[ -f "$TARBALL" ] || die "Alpine tarball not found: $TARBALL"
[ -f "$BASE_APKOVL" ] || die "headless overlay not found: $BASE_APKOVL"
[ -z "$AUTHORIZED_KEYS" ] || [ -f "$AUTHORIZED_KEYS" ] || die "authorized_keys file not found: $AUTHORIZED_KEYS"
[ -b "$TARGET" ] || die "target is not a block device: $TARGET"

case "$TARGET" in
    /dev/sd[a-z]|/dev/mmcblk[0-9]|/dev/nvme[0-9]n[0-9]) ;;
    *) die "target must be a whole disk, not a partition: $TARGET" ;;
esac

TARGET_SIZE=$(lsblk -dn -b -o SIZE "$TARGET" | tr -d '[:space:]')
TARGET_MODEL=$(lsblk -dn -o MODEL "$TARGET" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
TARGET_SERIAL=$(lsblk -dn -o SERIAL "$TARGET" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
TARGET_TRAN=$(lsblk -dn -o TRAN "$TARGET" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
TARGET_RM=$(lsblk -dn -o RM "$TARGET" | tr -d '[:space:]')
TARGET_RO=$(lsblk -dn -o RO "$TARGET" | tr -d '[:space:]')
TARGET_MODEL=${TARGET_MODEL:-unknown}

[ "$TARGET_RO" = "0" ] || die "target is read-only: $TARGET"
[ "$TARGET_RM" = "1" ] || die "refusing non-removable disk: $TARGET model=$TARGET_MODEL serial=${TARGET_SERIAL:-unknown}"
[ "$TARGET_SIZE" -le "$MAX_CARD_BYTES" ] || die "refusing oversized target: $TARGET size=$TARGET_SIZE limit=$MAX_CARD_BYTES"
[ -n "$TARGET_SERIAL" ] || die "target has no reported serial; use a reader that reports device identity"

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
    sudo -v || die "sudo authentication failed; run from an interactive terminal"
fi

partition_for() {
    case "$1" in
        /dev/mmcblk*|/dev/nvme*) printf '%sp1\n' "$1" ;;
        *) printf '%s1\n' "$1" ;;
    esac
}

write_interfaces() {
    output=$1
    {
        printf '%s\n' 'auto lo'
        printf '%s\n\n' 'iface lo inet loopback'
        for iface in $IFACES; do
            [ -n "$iface" ] || continue
            printf 'auto %s\n' "$iface"
            printf 'iface %s inet dhcp\n\n' "$iface"
            if [ -n "$RESCUE_IP" ]; then
                printf 'iface %s inet static\n' "$iface"
                printf '    address %s\n\n' "$RESCUE_IP"
            fi
        done
    } > "$output"
}

BOOT_PART=$(partition_for "$TARGET")
MOUNT_DIR=$(mktemp -d /tmp/localbox-sdcard.XXXXXX)
MOUNTED=0

cleanup() {
    if [ "$MOUNTED" = "1" ] && mountpoint -q "$MOUNT_DIR"; then
        sudo umount "$MOUNT_DIR"
    fi
    rmdir "$MOUNT_DIR" 2>/dev/null || true
}
trap cleanup EXIT

echo "About to erase and write Raspberry Pi DHCP + SSH boot card:"
lsblk "$TARGET"
echo "Model:          $TARGET_MODEL"
echo "Serial:         $TARGET_SERIAL"
echo "Transport:      ${TARGET_TRAN:-unknown}"
echo "Alpine tarball: $TARBALL"
echo "Headless ovl:   $BASE_APKOVL"
echo "DHCP ifaces:    $IFACES"
echo "Rescue IP:      ${RESCUE_IP:-disabled}"

echo ">> Unmounting existing mounts..."
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

[ -b "$BOOT_PART" ] || die "expected boot partition was not created: $BOOT_PART"

echo ">> Formatting $BOOT_PART as FAT32..."
sudo mkfs.vfat -F 32 -n ALPINE "$BOOT_PART"

echo ">> Mounting boot partition..."
sudo mount -t vfat -o rw,uid="$(id -u)",gid="$(id -g)",flush "$BOOT_PART" "$MOUNT_DIR"
MOUNTED=1

echo ">> Extracting Alpine Raspberry Pi tarball..."
tar --no-same-owner -xzf "$TARBALL" -C "$MOUNT_DIR"

echo ">> Adding headless SSH bootstrap overlay..."
cp "$BASE_APKOVL" "$MOUNT_DIR/headless.apkovl.tar.gz"

echo ">> Writing DHCP interfaces file..."
write_interfaces "$MOUNT_DIR/interfaces"

echo ">> Disabling headless overlay update check..."
: > "$MOUNT_DIR/opt-out"

echo ">> Adding persistent boot SSH script..."
cat > "$MOUNT_DIR/unattended.sh" <<'EOF'
#!/bin/sh
set -eu

apk add openssh-server --force-non-repository
mkdir -p /etc/ssh/sshd_config.d
if [ -f /media/mmcblk0p1/authorized_keys ]; then
    mkdir -p /root/.ssh
    install -m600 /media/mmcblk0p1/authorized_keys /root/.ssh/authorized_keys
    cat > /etc/ssh/sshd_config.d/localbox-headless.conf <<'SSHD'
PermitRootLogin prohibit-password
PubkeyAuthentication yes
SSHD
else
    cat > /etc/ssh/sshd_config.d/localbox-headless.conf <<'SSHD'
PermitRootLogin yes
AuthenticationMethods none
PermitEmptyPasswords yes
SSHD
fi

ssh-keygen -A
rc-service sshd restart
EOF

if [ -n "$AUTHORIZED_KEYS" ]; then
    echo ">> Adding SSH authorized_keys..."
    cp "$AUTHORIZED_KEYS" "$MOUNT_DIR/authorized_keys"
fi

echo ">> Syncing and unmounting..."
sync
sudo umount "$MOUNT_DIR"
MOUNTED=0
rmdir "$MOUNT_DIR" 2>/dev/null || true

if command -v udisksctl >/dev/null 2>&1; then
    udisksctl power-off -b "$TARGET" >/dev/null 2>&1 || true
fi

echo "SD card prepared."
echo "Device:    $TARGET"
echo "Partition: $BOOT_PART"
echo "DHCP:      $IFACES"
echo "Rescue IP: ${RESCUE_IP:-disabled}"
if [ -n "$AUTHORIZED_KEYS" ]; then
    echo "SSH:       root login with authorized key"
else
    echo "SSH:       temporary root login with no authentication"
fi
