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
echo ""

echo ">> Installing Tailscale..."
if ! apk add -q tailscale tailscale-openrc 2>/dev/null; then
    apk add -q tailscale
fi

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
        if [ -n "${TAILSCALE_AUTHKEY:-}" ]; then
            tailscale up --login-server "$HEADSCALE_LOGIN_SERVER" --authkey "$TAILSCALE_AUTHKEY"
        else
            tailscale up --login-server "$HEADSCALE_LOGIN_SERVER"
        fi
    else
        echo "Run this on the Pi, then rerun this script:"
        echo ""
        echo "  tailscale up --login-server https://headscale.example.com"
        echo ""
    fi
else
    echo ">> Tailscale status OK"
fi

echo ">> Persisting Alpine local backup state..."
lbu add /etc/apk/repositories 2>/dev/null || true
lbu add /etc/apk/world
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
