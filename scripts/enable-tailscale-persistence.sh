#!/bin/sh
set -eu

echo "================================================"
echo "   Enable Tailscale persistence"
echo "================================================"
echo ""
echo "This installs Tailscale if needed, enables its OpenRC service,"
echo "and persists package, service, and auth state with Alpine lbu."
echo "Set HEADSCALE_LOGIN_SERVER and optionally TAILSCALE_AUTHKEY"
echo "to join a Headscale network non-interactively."
echo "Set TAILSCALE_ADVERTISE_ROUTES to advertise subnet routes."
echo ""

ensure_apk_cache() {
    boot_media=/media/mmcblk0p1
    if [ -d "$boot_media" ]; then
        mount -o remount,rw "$boot_media" 2>/dev/null || true
        mkdir -p "$boot_media/cache"
        if [ ! -L /etc/apk/cache ] || [ "$(readlink /etc/apk/cache 2>/dev/null || true)" != "$boot_media/cache" ]; then
            rm -rf /etc/apk/cache
            ln -s "$boot_media/cache" /etc/apk/cache
        fi
    fi
}

echo ">> Installing Tailscale..."
ensure_apk_cache
if ! apk add -q tailscale tailscale-openrc 2>/dev/null; then
    apk add -q tailscale
fi
apk cache sync 2>/dev/null || true

SERVICE=""
if [ -f /etc/init.d/tailscale ]; then
    SERVICE="tailscale"
elif [ -f /etc/init.d/tailscaled ]; then
    SERVICE="tailscaled"
fi

if [ -z "$SERVICE" ]; then
    echo "ERROR: no Tailscale OpenRC service found after install"
    exit 1
fi

echo ">> Enabling $SERVICE service..."
mkdir -p /var/lib/tailscale
rc-update add "$SERVICE" default 2>/dev/null || true
rc-service "$SERVICE" restart || rc-service "$SERVICE" start

if ! tailscale status >/dev/null 2>&1; then
    echo ""
    echo "Tailscale is installed but not logged in yet."
    if [ -n "${HEADSCALE_LOGIN_SERVER:-}" ]; then
        echo ">> Joining Headscale network..."
        UP_ARGS="--login-server $HEADSCALE_LOGIN_SERVER"
        if [ -n "${TAILSCALE_AUTHKEY:-}" ]; then
            UP_ARGS="$UP_ARGS --authkey $TAILSCALE_AUTHKEY"
        fi
        [ -z "${TAILSCALE_HOSTNAME:-}" ] || UP_ARGS="$UP_ARGS --hostname $TAILSCALE_HOSTNAME"
        [ -z "${TAILSCALE_ADVERTISE_ROUTES:-}" ] || UP_ARGS="$UP_ARGS --advertise-routes $TAILSCALE_ADVERTISE_ROUTES"
        [ -z "${TAILSCALE_ACCEPT_ROUTES:-}" ] || UP_ARGS="$UP_ARGS --accept-routes=$TAILSCALE_ACCEPT_ROUTES"
        # shellcheck disable=SC2086
        tailscale up $UP_ARGS
    else
        echo "Run this on the Pi, then rerun this script:"
        echo ""
        echo "  tailscale up --login-server https://headscale.example.com"
        echo ""
    fi
else
    echo ">> Tailscale status OK"
    if [ -n "${TAILSCALE_ADVERTISE_ROUTES:-}" ] || [ -n "${TAILSCALE_ACCEPT_ROUTES:-}" ] || [ -n "${TAILSCALE_HOSTNAME:-}" ]; then
        echo ">> Updating Tailscale preferences..."
        UP_ARGS=""
        [ -z "${TAILSCALE_HOSTNAME:-}" ] || UP_ARGS="$UP_ARGS --hostname $TAILSCALE_HOSTNAME"
        [ -z "${TAILSCALE_ADVERTISE_ROUTES:-}" ] || UP_ARGS="$UP_ARGS --advertise-routes $TAILSCALE_ADVERTISE_ROUTES"
        [ -z "${TAILSCALE_ACCEPT_ROUTES:-}" ] || UP_ARGS="$UP_ARGS --accept-routes=$TAILSCALE_ACCEPT_ROUTES"
        # shellcheck disable=SC2086
        tailscale up $UP_ARGS
    fi
fi

echo ">> Persisting Alpine local backup state..."
lbu add /etc/apk/repositories 2>/dev/null || true
lbu add /etc/apk/world
lbu add /etc/apk/cache 2>/dev/null || true
lbu add "/etc/init.d/$SERVICE"
[ ! -f "/etc/conf.d/$SERVICE" ] || lbu add "/etc/conf.d/$SERVICE"
[ ! -e "/etc/runlevels/default/$SERVICE" ] || lbu add "/etc/runlevels/default/$SERVICE"
lbu add /var/lib/tailscale
lbu add /etc/group 2>/dev/null || true
lbu commit -d

cat << EOF

================================================
Tailscale persistence configured.

Verify after reboot:
  rc-service $SERVICE status
  tailscale status

If it asks you to log in again, run:
  tailscale up
  lbu add /var/lib/tailscale
  lbu commit -d
================================================
EOF
