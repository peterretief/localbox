#!/bin/sh
set -eu

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
    echo "Usage: localbox-add-tunnel-route domain [tunnel-name-or-id]" >&2
    exit 1
fi

DOMAIN=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed 's#^https://##; s#^http://##; s#^www\.##; s#/.*##; s/[.]$//')
TUNNEL=${2:-${LOCALBOX_TUNNEL:-}}

case "$DOMAIN" in
    ""|.*|*..*|*_*|*-.*|*.-*|*[^a-z0-9.-]*)
        echo "Invalid domain: $DOMAIN" >&2
        exit 1
        ;;
esac

if [ -z "$TUNNEL" ] && [ -f /etc/localbox/cloudflare-tunnel.conf ]; then
    # shellcheck disable=SC1091
    . /etc/localbox/cloudflare-tunnel.conf
    TUNNEL=${tunnel:-${TUNNEL:-}}
fi

if [ -z "$TUNNEL" ]; then
    echo "Tunnel name/id required. Pass it as the second argument or set tunnel= in /etc/localbox/cloudflare-tunnel.conf." >&2
    exit 1
fi

echo "Adding Cloudflare Tunnel DNS route:"
echo "  tunnel: $TUNNEL"
echo "  host:   $DOMAIN"
cloudflared tunnel route dns "$TUNNEL" "$DOMAIN"

echo "Adding www route:"
cloudflared tunnel route dns "$TUNNEL" "www.$DOMAIN" || true

echo "Done."
