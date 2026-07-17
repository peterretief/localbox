#!/bin/sh
set -eu

WEBMAIL_HOST=${1:-${WEBMAIL_HOST:-mail.localboxmail.co.za}}
NGINX_MAIN=${NGINX_MAIN:-/etc/nginx/nginx.conf}

if ! grep -q 'server_names_hash_bucket_size' "$NGINX_MAIN"; then
    cp -p "$NGINX_MAIN" "$NGINX_MAIN.bak-$(date +%Y%m%d%H%M%S)"
    tmp=$(mktemp)
    awk '
        /^http[[:space:]]*\{/ && !done {
            print
            print "    server_names_hash_bucket_size 64;"
            done = 1
            next
        }
        { print }
    ' "$NGINX_MAIN" > "$tmp"
    cat "$tmp" > "$NGINX_MAIN"
    rm -f "$tmp"
fi

ALPS_HOST="$WEBMAIL_HOST" sh /tmp/configure-alps-nginx-proxy.sh

lbu add "$NGINX_MAIN" /etc/nginx/http.d/alps-webmail.conf 2>/dev/null || true
lbu commit -d 2>/dev/null || true

echo "Configured webmail host: $WEBMAIL_HOST"
