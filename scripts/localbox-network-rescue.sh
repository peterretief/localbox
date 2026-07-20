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

candidate_ifaces() {
    for iface_path in /sys/class/net/*; do
        iface=$(basename "$iface_path")
        [ "$iface" != "lo" ] || continue
        [ -e "$iface_path" ] || continue
        case "$iface" in
            wlan*|wl*|tailscale*|docker*|br-*|veth*) continue ;;
        esac
        printf '%s\n' "$iface"
    done
}

ensure_dhcp_config() {
    iface=$1
    [ -n "$iface" ] || return 0
    mkdir -p /etc/network
    if [ -f /etc/network/interfaces ] && grep -q "iface $iface inet dhcp" /etc/network/interfaces; then
        return 0
    fi
    log "writing dhcp config for $iface"
    cat > /etc/network/interfaces << EOF
auto lo
iface lo inet loopback

auto $iface
iface $iface inet dhcp
EOF
}

ifaces=""
for _ in $(seq 1 20); do
    ifaces=$(candidate_ifaces)
    [ -n "$ifaces" ] && break
    log "waiting for ethernet interface"
    sleep 1
done

if [ -z "$ifaces" ]; then
    log "no ethernet interface found"
fi

printf '%s\n' "$ifaces" | while IFS= read -r iface; do
    [ -n "$iface" ] || continue
    iface_path="/sys/class/net/$iface"
    [ "$iface" != "lo" ] || continue
    [ -e "$iface_path" ] || continue

    operstate=$(cat "$iface_path/operstate" 2>/dev/null || echo unknown)
    carrier=$(cat "$iface_path/carrier" 2>/dev/null || echo unknown)
    address=$(cat "$iface_path/address" 2>/dev/null || echo unknown)
    log "found iface=$iface mac=$address state=$operstate carrier=$carrier"

    ensure_dhcp_config "$iface"
    ip link set "$iface" up 2>>"$LOG_FILE" || true
    for _ in $(seq 1 10); do
        carrier=$(cat "$iface_path/carrier" 2>/dev/null || echo unknown)
        [ "$carrier" = "1" ] && break
        log "waiting for carrier on $iface carrier=$carrier"
        sleep 1
    done
    if command -v udhcpc >/dev/null 2>&1; then
        log "requesting dhcp with udhcpc on $iface"
        udhcpc -i "$iface" -q -t 12 -T 4 2>>"$LOG_FILE" || true
    fi
done

ip -4 addr show 2>>"$LOG_FILE" >> "$LOG_FILE" || true
rc-service sshd start 2>>"$LOG_FILE" || true
log "network rescue complete"
