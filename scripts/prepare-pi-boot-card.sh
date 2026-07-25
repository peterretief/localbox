#!/bin/sh
set -eu

BOOT_MOUNT=${BOOT_MOUNT:-${1:-/mnt/piboot}}
FIRSTBOOT_CONFIG=${FIRSTBOOT_CONFIG:-${2:-}}
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
FIRSTBOOT_SCRIPT=${FIRSTBOOT_SCRIPT:-"$SCRIPT_DIR/localbox-firstboot.sh"}
NETWORK_RESCUE_SCRIPT=${NETWORK_RESCUE_SCRIPT:-"$SCRIPT_DIR/localbox-network-rescue.sh"}

config_value() {
    key=$1
    file=$2
    [ -f "$file" ] || return 0
    awk -F= -v key="$key" '
        $0 ~ "^[[:space:]]*#" { next }
        $1 == key {
            sub(/^[^=]*=/, "")
            print
            exit
        }
    ' "$file"
}

wpa_escape() {
    printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

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

if [ ! -f "$NETWORK_RESCUE_SCRIPT" ]; then
    echo "ERROR: network rescue script not found: $NETWORK_RESCUE_SCRIPT"
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
mkdir -p "$WORK/etc/network" "$WORK/etc/runlevels/default" "$WORK/etc/ssh/sshd_config.d" "$WORK/etc/wpa_supplicant"
cat > "$WORK/etc/network/interfaces" << 'EOF'
auto lo
iface lo inet loopback

auto eth0
iface eth0 inet dhcp

auto end0
iface end0 inet dhcp

auto wlan0
iface wlan0 inet dhcp
    pre-up wpa_supplicant -B -i wlan0 -c /etc/wpa_supplicant/wpa_supplicant.conf || true
    post-down killall wpa_supplicant || true
EOF
cat > "$BOOT_MOUNT/interfaces" << 'EOF'
auto lo
iface lo inet loopback

auto eth0
iface eth0 inet dhcp

auto end0
iface end0 inet dhcp

auto wlan0
iface wlan0 inet dhcp
    pre-up wpa_supplicant -B -i wlan0 -c /etc/wpa_supplicant/wpa_supplicant.conf || true
    post-down killall wpa_supplicant || true
EOF
ln -sfn ../../init.d/networking "$WORK/etc/runlevels/default/networking"
ln -sfn ../../init.d/sshd "$WORK/etc/runlevels/default/sshd"
ln -sfn ../../init.d/chronyd "$WORK/etc/runlevels/default/chronyd"
cat > "$WORK/etc/ssh/sshd_config.d/10-localbox-hardening.conf" << 'EOF'
PermitEmptyPasswords no
PubkeyAuthentication yes
PermitRootLogin prohibit-password
EOF

if [ -n "$FIRSTBOOT_CONFIG" ] && [ -f "$FIRSTBOOT_CONFIG" ]; then
    wifi_ssid=$(config_value wifi_ssid "$FIRSTBOOT_CONFIG")
    wifi_password=$(config_value wifi_password "$FIRSTBOOT_CONFIG")
    wifi_country=$(config_value wifi_country "$FIRSTBOOT_CONFIG")
    wifi_country=${wifi_country:-ZA}
    if [ -n "$wifi_ssid" ] && [ -n "$wifi_password" ]; then
        echo ">> Adding Wi-Fi bootstrap config for SSID: $wifi_ssid"
        cat > "$BOOT_MOUNT/wpa_supplicant.conf" << EOF
country=$wifi_country
ctrl_interface=DIR=/var/run/wpa_supplicant GROUP=netdev
update_config=1

network={
    ssid="$(wpa_escape "$wifi_ssid")"
    psk="$(wpa_escape "$wifi_password")"
}
EOF
        cp "$BOOT_MOUNT/wpa_supplicant.conf" "$WORK/etc/wpa_supplicant/wpa_supplicant.conf"
    fi
fi

echo ">> Installing LocalBox first-boot script into overlay..."
mkdir -p "$WORK/usr/local/sbin" "$WORK/etc/init.d"
cp "$FIRSTBOOT_SCRIPT" "$WORK/usr/local/sbin/localbox-firstboot"
chmod +x "$WORK/usr/local/sbin/localbox-firstboot"
cp "$NETWORK_RESCUE_SCRIPT" "$WORK/usr/local/sbin/localbox-network-rescue"
chmod +x "$WORK/usr/local/sbin/localbox-network-rescue"
cat > "$WORK/etc/init.d/localbox-firstboot" << 'EOF'
#!/sbin/openrc-run
name="localbox-firstboot"
description="LocalBox first boot provisioning"
command="/usr/local/sbin/localbox-firstboot"
command_background=false
depend() {
    after networking localbox-network-rescue chronyd
}
EOF
chmod +x "$WORK/etc/init.d/localbox-firstboot"
ln -sfn ../../init.d/localbox-firstboot "$WORK/etc/runlevels/default/localbox-firstboot"
cat > "$WORK/etc/init.d/localbox-network-rescue" << 'EOF'
#!/sbin/openrc-run
name="localbox-network-rescue"
description="LocalBox network rescue DHCP fallback"
command="/usr/local/sbin/localbox-network-rescue"
command_background=false
depend() {
    provide net
    after networking
    before sshd localbox-firstboot
}
EOF
chmod +x "$WORK/etc/init.d/localbox-network-rescue"
ln -sfn ../../init.d/localbox-network-rescue "$WORK/etc/runlevels/default/localbox-network-rescue"

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
    tar --owner=0 --group=0 --numeric-owner -czf "$APKOVL.new" .
)
mv "$APKOVL.new" "$APKOVL"
sync

echo ">> Verifying overlay..."
tar -tzf "$APKOVL" | grep -E 'etc/network/interfaces|etc/runlevels/default/networking|usr/local/sbin/localbox-firstboot|etc/runlevels/default/localbox-firstboot|usr/local/sbin/localbox-network-rescue|etc/runlevels/default/localbox-network-rescue'

echo "Prepared Pi boot card."
echo "Overlay: $APKOVL"
echo "Backup:  $BACKUP"
