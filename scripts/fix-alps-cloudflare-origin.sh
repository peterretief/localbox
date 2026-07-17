#!/bin/sh
set -eu

ALPS_DIR=${ALPS_DIR:-/mnt/data/alps-test}
ALPS_CONFIG=${ALPS_CONFIG:-$ALPS_DIR/config.toml}
ALPS_ADDR=${ALPS_ADDR:-100.64.0.8:1323}

if [ ! -f "$ALPS_CONFIG" ]; then
    echo "ERROR: $ALPS_CONFIG does not exist" >&2
    exit 1
fi

cp -p "$ALPS_CONFIG" "$ALPS_CONFIG.bak-$(date +%Y%m%d%H%M%S)"
tmp=$(mktemp)
awk '
    /^trusted_proxies =/ {
        print "trusted_proxies = [\"127.0.0.1/32\", \"100.64.0.8/32\", \"100.64.0.0/10\"]"
        next
    }
    { print }
' "$ALPS_CONFIG" > "$tmp"
cat "$tmp" > "$ALPS_CONFIG"
rm -f "$tmp"

old=$(pgrep -f "$ALPS_DIR/alps -config $ALPS_CONFIG" || true)
if [ -n "$old" ]; then
    kill $old
    sleep 1
fi

cd "$ALPS_DIR"
su -s /bin/sh www -c "nohup $ALPS_DIR/alps -config $ALPS_CONFIG >> $ALPS_DIR/stdout.log 2>> $ALPS_DIR/stderr.log &"
sleep 1

if ! pgrep -f "$ALPS_DIR/alps -config $ALPS_CONFIG" >/dev/null; then
    echo "ERROR: ALPS did not start" >&2
    exit 1
fi

curl -sS -i --max-time 5 \
    -H "Host: mail.opamp.co.za" \
    -H "X-Forwarded-Proto: https" \
    "http://$ALPS_ADDR/" | sed -n '1,8p'

echo "ALPS Cloudflare origin settings updated."
