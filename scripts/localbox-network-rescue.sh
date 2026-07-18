#!/bin/sh
set -eu

LOG_FILE=${LOG_FILE:-/media/mmcblk0p1/localbox-network.log}
if ! touch "$LOG_FILE" 2>/dev/null; then
    LOG_FILE=/tmp/localbox-network.log
    touch "$LOG_FILE" 2>/dev/null || true
fi

log() {
    printf '%s %s\n' "$(date '+%Y-%m-%d %H:%M:%S' 2>/dev/null || echo boot)" "$*" >> "$LOG_FILE"
}

log "network rescue starting"
log "cmdline: $(cat /proc/cmdline 2>/dev/null || true)"

for iface_path in /sys/class/net/*; do
    iface=$(basename "$iface_path")
    [ "$iface" != "lo" ] || continue
    [ -e "$iface_path" ] || continue

    operstate=$(cat "$iface_path/operstate" 2>/dev/null || echo unknown)
    carrier=$(cat "$iface_path/carrier" 2>/dev/null || echo unknown)
    address=$(cat "$iface_path/address" 2>/dev/null || echo unknown)
    log "found iface=$iface mac=$address state=$operstate carrier=$carrier"

    ip link set "$iface" up 2>>"$LOG_FILE" || true
    if command -v udhcpc >/dev/null 2>&1; then
        log "requesting dhcp with udhcpc on $iface"
        udhcpc -i "$iface" -q -t 8 -T 3 2>>"$LOG_FILE" || true
    fi
done

ip -4 addr show 2>>"$LOG_FILE" >> "$LOG_FILE" || true
rc-service sshd start 2>>"$LOG_FILE" || true
log "network rescue complete"
