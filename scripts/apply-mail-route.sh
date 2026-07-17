#!/bin/sh
set -eu

NGINX_CONF=${NGINX_CONF:-/etc/nginx/http.d/localbox-sites.conf}

if [ ! -f "$NGINX_CONF" ]; then
    echo "ERROR: $NGINX_CONF does not exist" >&2
    exit 1
fi

if ! grep -q 'location = /mail' "$NGINX_CONF"; then
    tmp=$(mktemp)
    awk '
        /location = \/public\/logout \{/ && !done {
            print "    location = /mail {"
            print "        proxy_pass http://127.0.0.1:8080/mail;"
            print "        proxy_set_header Host $host;"
            print "        proxy_set_header X-Forwarded-Host $host;"
            print "        proxy_set_header X-Real-IP $remote_addr;"
            print "        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;"
            print "        proxy_set_header X-Forwarded-Proto $scheme;"
            print "    }"
            print ""
            done = 1
        }
        { print }
    ' "$NGINX_CONF" > "$tmp"
    cat "$tmp" > "$NGINX_CONF"
    rm -f "$tmp"
fi

nginx -t
rc-service nginx restart
rc-service mail-ingest zap 2>/dev/null || true
rc-service mail-ingest restart

lbu add "$NGINX_CONF" 2>/dev/null || true
lbu add /usr/local/bin/mail-ingest 2>/dev/null || true
lbu commit -d 2>/dev/null || true
