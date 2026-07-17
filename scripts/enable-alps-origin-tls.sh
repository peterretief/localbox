#!/bin/sh
set -eu

ALPS_DIR=${ALPS_DIR:-/mnt/data/alps-test}
ALPS_CONFIG=${ALPS_CONFIG:-$ALPS_DIR/config.toml}
ALPS_HOST=${ALPS_HOST:-mail.opamp.co.za}
ALPS_ALT_HOST=${ALPS_ALT_HOST:-mymail.opamp.co.za}
ALPS_IP=${ALPS_IP:-100.64.0.8}

cd "$ALPS_DIR"
mkdir -p certs
if [ ! -f certs/alps.key ] || [ ! -f certs/alps.crt ]; then
    openssl req -x509 -newkey rsa:2048 -nodes -days 825 \
        -keyout certs/alps.key \
        -out certs/alps.crt \
        -subj "/CN=$ALPS_HOST" \
        -addext "subjectAltName=DNS:$ALPS_HOST,DNS:$ALPS_ALT_HOST,IP:$ALPS_IP"
    chown -R www:www certs 2>/dev/null || chown -R www certs
    chmod 600 certs/alps.key
    chmod 644 certs/alps.crt
fi

cp -p "$ALPS_CONFIG" "$ALPS_CONFIG.bak-tls-$(date +%Y%m%d%H%M%S)"
tmp=$(mktemp)
awk '
    /^\[server\.tls\]/ { skip = 1; next }
    /^\[tls\]/ { skip = 1; next }
    /^\[/ && skip { skip = 0 }
    !skip { print }
' "$ALPS_CONFIG" > "$tmp"
cat >> "$tmp" << EOF

[tls]
cert_file = "$ALPS_DIR/certs/alps.crt"
key_file = "$ALPS_DIR/certs/alps.key"
EOF
cat "$tmp" > "$ALPS_CONFIG"
rm -f "$tmp"

rc-service alps restart || true
sleep 1
rc-service alps status || true

echo
echo "HTTP probe:"
curl -sS -i --max-time 5 "http://$ALPS_IP:1323/" | sed -n '1,6p' || true

echo
echo "HTTPS probe:"
curl -k -sS -i --max-time 5 "https://$ALPS_IP:1323/" | sed -n '1,8p' || true

echo
echo "Config tail:"
tail -n 12 "$ALPS_CONFIG"
