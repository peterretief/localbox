#!/bin/sh
set -eu

BOOT_MOUNT=${BOOT_MOUNT:-${1:-/mnt/piboot}}
IFACES=${IFACES:-eth0 end0}

if [ ! -d "$BOOT_MOUNT" ]; then
    echo "Usage: BOOT_MOUNT=/mnt/piboot $0" >&2
    echo "ERROR: boot mount does not exist: $BOOT_MOUNT" >&2
    exit 1
fi

if [ ! -w "$BOOT_MOUNT" ]; then
    echo "ERROR: boot mount is not writable: $BOOT_MOUNT" >&2
    exit 1
fi

set -- "$BOOT_MOUNT"/*.apkovl.tar.gz
if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
    echo "ERROR: expected exactly one *.apkovl.tar.gz in $BOOT_MOUNT" >&2
    exit 1
fi
APKOVL=$1

STAMP=$(date +%Y%m%d-%H%M%S)
BACKUP="$APKOVL.bak-$STAMP"
WORK=$(mktemp -d /tmp/localbox-dhcp.XXXXXX)
cleanup() {
    rm -rf "$WORK"
}
trap cleanup EXIT

echo ">> Backing up overlay: $BACKUP"
cp -a "$APKOVL" "$BACKUP"

echo ">> Extracting overlay..."
tar -xzf "$APKOVL" -C "$WORK"

echo ">> Writing LAN DHCP config..."
mkdir -p "$WORK/etc/network" "$WORK/etc/runlevels/default"
{
    printf '%s\n' 'auto lo'
    printf '%s\n\n' 'iface lo inet loopback'

    for iface in $IFACES; do
        [ -n "$iface" ] || continue
        printf 'auto %s\n' "$iface"
        printf 'iface %s inet dhcp\n\n' "$iface"
    done
} > "$WORK/etc/network/interfaces"

ln -sfn ../../init.d/networking "$WORK/etc/runlevels/default/networking"

echo ">> Repacking overlay..."
(
    cd "$WORK"
    tar --owner=0 --group=0 --numeric-owner -czf "$APKOVL.new" .
)
mv "$APKOVL.new" "$APKOVL"
sync

echo ">> Done. LAN DHCP enabled for: $IFACES"
echo "Overlay: $APKOVL"
echo "Backup:  $BACKUP"
