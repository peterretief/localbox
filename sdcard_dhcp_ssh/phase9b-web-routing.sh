#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
INVENTORY=${INVENTORY:-$SCRIPT_DIR/localbox-inventory.sh}

PI_HOST=${PI_HOST:-192.168.0.7}
PI_USER=${PI_USER:-root}
DEVICE_NAME=${DEVICE_NAME:-localbox-001}
LOCALBOX_DOMAIN=${LOCALBOX_DOMAIN:-localboxtest.co.za}
LOCALBOX_BASE=${LOCALBOX_BASE:-/mnt/data/localbox}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/localbox_phase9b_known_hosts -o ConnectTimeout=10"}

echo "Phase 9b target: $PI_USER@$PI_HOST"
echo "Goal: nginx web routing for root/www public site and app paths"
echo "Device: $DEVICE_NAME"
echo "Domain: $LOCALBOX_DOMAIN"

if [ -x "$INVENTORY" ]; then
    "$INVENTORY" validate-device "$DEVICE_NAME" >/dev/null
fi

ssh $SSH_OPTS "$PI_USER@$PI_HOST" "LOCALBOX_DOMAIN='$LOCALBOX_DOMAIN' LOCALBOX_BASE='$LOCALBOX_BASE' sh -s" <<'REMOTE'
set -eu

ensure_lbu_media() {
    mkdir -p /etc/lbu
    [ -f /etc/lbu/lbu.conf ] || touch /etc/lbu/lbu.conf
    if grep -q '^# LBU_MEDIA=' /etc/lbu/lbu.conf; then
        sed -i 's/^# LBU_MEDIA=.*/LBU_MEDIA=mmcblk0p1/' /etc/lbu/lbu.conf
    elif grep -q '^LBU_MEDIA=' /etc/lbu/lbu.conf; then
        sed -i 's/^LBU_MEDIA=.*/LBU_MEDIA=mmcblk0p1/' /etc/lbu/lbu.conf
    else
        printf '%s\n' 'LBU_MEDIA=mmcblk0p1' >> /etc/lbu/lbu.conf
    fi
}

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

mountpoint -q /mnt/data || {
    echo "ERROR: /mnt/data is not mounted. Finish Phase 4 first." >&2
    exit 1
}
[ -d "$LOCALBOX_BASE/domains/$LOCALBOX_DOMAIN/site" ] || {
    echo "ERROR: site directory missing: $LOCALBOX_BASE/domains/$LOCALBOX_DOMAIN/site" >&2
    exit 1
}

ensure_apk_cache
apk add -q nginx nginx-openrc
apk cache sync 2>/dev/null || true
mkdir -p /etc/nginx/http.d/disabled
for config in /etc/nginx/http.d/default_server.conf /etc/nginx/http.d/localbox-sites.conf; do
    if [ -f "$config" ]; then
        mv "$config" "/etc/nginx/http.d/disabled/$(basename "$config").disabled"
    fi
done
if [ -f /etc/nginx/http.d/default.conf ] && ! grep -q 'LocalBox managed default site' /etc/nginx/http.d/default.conf; then
    cp /etc/nginx/http.d/default.conf /etc/nginx/http.d/disabled/default.conf.original
fi

cat > /etc/nginx/http.d/default.conf <<'NGINX'
# LocalBox managed default site.
map $host $localbox_domain {
    default $host;
    ~^www\.(?<bare>.+)$ $bare;
}

server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;

    root /mnt/data/localbox/domains/$localbox_domain/site;
    index index.html;

    location ~ ^/(login|logout|dashboard|mail|apps|files|admin|attachment|certificate|compose|forgot-password|health|ingest|message|orders|page|password|payment|public|register|reset-password|system)(/|$) {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location ~ ^/(api/domain|contact(?:\.html)?|order|waitlist)(/|$) {
        proxy_pass http://127.0.0.1:8081;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location / {
        try_files $uri $uri/ /index.html;
    }
}
NGINX

adduser nginx mail 2>/dev/null || true
rc-update add nginx default 2>/dev/null || true
nginx -t
rc-service nginx restart </dev/null >/tmp/phase9b-nginx.log 2>&1
sleep 1
rc-service nginx status >/tmp/phase9b-nginx-status.log 2>&1 || {
    echo "ERROR: nginx did not stay started" >&2
    cat /tmp/phase9b-nginx.log /tmp/phase9b-nginx-status.log >&2
    exit 1
}

ensure_lbu_media
mount -o remount,rw /media/mmcblk0p1 2>/dev/null || true
lbu add /etc/apk/world
lbu add /etc/nginx/http.d
lbu add /etc/init.d/nginx 2>/dev/null || true
lbu add /etc/runlevels/default/nginx 2>/dev/null || true
lbu add /etc/group /etc/passwd /etc/shadow 2>/dev/null || true
lbu add /etc/lbu/lbu.conf
lbu add /etc/apk/cache 2>/dev/null || true
lbu commit -d
mount -o remount,ro /media/mmcblk0p1 2>/dev/null || true

rc-service nginx status </dev/null
curl -I -sS -H "Host: $LOCALBOX_DOMAIN" http://127.0.0.1/ | sed -n '1,5p'
REMOTE

if [ -x "$INVENTORY" ]; then
    "$INVENTORY" phase "$DEVICE_NAME" phase9b-web-routing complete detail="$LOCALBOX_DOMAIN nginx:80"
fi

echo
echo "Phase 9b finished."
echo "Next publish root/www to LOCALBOX_TUNNEL_SERVICE=http://localhost:80."
