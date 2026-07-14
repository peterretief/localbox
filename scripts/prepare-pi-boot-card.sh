#!/bin/sh
set -eu

BOOT_MOUNT=${BOOT_MOUNT:-${1:-/mnt/piboot}}
FIRSTBOOT_CONFIG=${FIRSTBOOT_CONFIG:-${2:-}}
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
FIRSTBOOT_SCRIPT=${FIRSTBOOT_SCRIPT:-"$SCRIPT_DIR/localbox-firstboot.sh"}

if [ ! -d "$BOOT_MOUNT" ]; then
    echo "Usage: BOOT_MOUNT=/mnt/piboot FIRSTBOOT_CONFIG=/path/localbox-firstboot.conf $0"
    echo "ERROR: boot mount does not exist: $BOOT_MOUNT"
    exit 1
fi

if [ ! -w "$BOOT_MOUNT" ]; then
    echo "ERROR: boot mount is not writable: $BOOT_MOUNT"
    echo "Run fsck.vfat and mount the Pi boot partition read-write first."
    exit 1
fi

if [ ! -f "$FIRSTBOOT_SCRIPT" ]; then
    echo "ERROR: first-boot script not found: $FIRSTBOOT_SCRIPT"
    exit 1
fi

APKOVL=${APKOVL:-}
if [ -z "$APKOVL" ]; then
    set -- "$BOOT_MOUNT"/*.apkovl.tar.gz
    if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
        echo "ERROR: expected exactly one *.apkovl.tar.gz in $BOOT_MOUNT"
        exit 1
    fi
    APKOVL=$1
fi

STAMP=$(date +%Y%m%d-%H%M%S)
BACKUP="$APKOVL.bak-$STAMP"
WORK=$(mktemp -d /tmp/localbox-apkovl.XXXXXX)
cleanup() {
    rm -rf "$WORK"
}
trap cleanup EXIT

echo ">> Backing up overlay:"
echo "   $BACKUP"
cp -a "$APKOVL" "$BACKUP"

echo ">> Extracting overlay..."
tar -xzf "$APKOVL" -C "$WORK"

echo ">> Adding persistent DHCP networking..."
mkdir -p "$WORK/etc/network" "$WORK/etc/runlevels/default" "$WORK/etc/ssh/sshd_config.d"
cat > "$WORK/etc/network/interfaces" << 'EOF'
auto lo
iface lo inet loopback

auto eth0
iface eth0 inet dhcp

auto end0
iface end0 inet dhcp
EOF
ln -sfn ../../init.d/networking "$WORK/etc/runlevels/default/networking"
ln -sfn ../../init.d/sshd "$WORK/etc/runlevels/default/sshd"
ln -sfn ../../init.d/chronyd "$WORK/etc/runlevels/default/chronyd"
cat > "$WORK/etc/ssh/sshd_config.d/10-localbox-hardening.conf" << 'EOF'
PermitEmptyPasswords no
PubkeyAuthentication yes
PermitRootLogin prohibit-password
EOF

echo ">> Installing LocalBox first-boot script into overlay..."
mkdir -p "$WORK/usr/local/sbin" "$WORK/etc/init.d"
cp "$FIRSTBOOT_SCRIPT" "$WORK/usr/local/sbin/localbox-firstboot"
chmod +x "$WORK/usr/local/sbin/localbox-firstboot"
cat > "$WORK/etc/init.d/localbox-firstboot" << 'EOF'
#!/sbin/openrc-run
name="localbox-firstboot"
description="LocalBox first boot provisioning"
command="/usr/local/sbin/localbox-firstboot"
command_background=false
depend() {
    need net
    after chronyd
}
EOF
chmod +x "$WORK/etc/init.d/localbox-firstboot"
ln -sfn ../../init.d/localbox-firstboot "$WORK/etc/runlevels/default/localbox-firstboot"

if [ -n "$FIRSTBOOT_CONFIG" ]; then
    if [ ! -f "$FIRSTBOOT_CONFIG" ]; then
        echo "ERROR: first-boot config not found: $FIRSTBOOT_CONFIG"
        exit 1
    fi
    echo ">> Copying first-boot config to boot partition..."
    cp "$FIRSTBOOT_CONFIG" "$BOOT_MOUNT/localbox-firstboot.conf"
fi

echo ">> Repacking overlay..."
(
    cd "$WORK"
    tar -czf "$APKOVL.new" .
)
mv "$APKOVL.new" "$APKOVL"
sync

echo ">> Verifying overlay..."
tar -tzf "$APKOVL" | grep -E 'etc/network/interfaces|etc/runlevels/default/networking|usr/local/sbin/localbox-firstboot|etc/runlevels/default/localbox-firstboot'

echo "Prepared Pi boot card."
echo "Overlay: $APKOVL"
echo "Backup:  $BACKUP"
