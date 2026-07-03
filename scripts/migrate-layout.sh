#!/bin/sh
set -e

DOMAIN=${1:-opamp.co.za}
OLD_MAIL="/mnt/data/mail/$DOMAIN"
OLD_SITE="/mnt/data/site/$DOMAIN"
NEW_DOMAIN="/mnt/data/localbox/domains/$DOMAIN"
NEW_MAIL="$NEW_DOMAIN/mail"
NEW_SITE="$NEW_DOMAIN/site"

echo "=== LocalBox layout migration ==="
echo "Domain: $DOMAIN"

echo ">> Stopping services..."
rc-service mail-ingest stop 2>/dev/null || true
rc-service contact-service stop 2>/dev/null || true

echo ">> Creating new layout..."
mkdir -p "$NEW_MAIL" "$NEW_SITE"

if [ -d "$OLD_MAIL" ]; then
    echo ">> Moving mail data..."
    for item in "$OLD_MAIL"/* "$OLD_MAIL"/.[!.]* "$OLD_MAIL"/..?*; do
        [ -e "$item" ] || continue
        mv "$item" "$NEW_MAIL"/
    done
fi

if [ -d "$OLD_SITE" ]; then
    echo ">> Moving site data..."
    for item in "$OLD_SITE"/* "$OLD_SITE"/.[!.]* "$OLD_SITE"/..?*; do
        [ -e "$item" ] || continue
        mv "$item" "$NEW_SITE"/
    done
fi

echo ">> Ensuring expected directories..."
mkdir -p "$NEW_SITE/assets/images"
mkdir -p "$NEW_DOMAIN/.archived"

echo ">> Fixing permissions..."
chown -R www:mail /mnt/data/localbox
chmod -R g+rwX /mnt/data/localbox
chmod 2775 "$NEW_DOMAIN" "$NEW_MAIL" "$NEW_SITE" "$NEW_SITE/assets" "$NEW_SITE/assets/images"

echo ">> Saving domain config..."
mkdir -p /etc/localbox
if [ ! -f /etc/localbox/domains.conf ] || ! grep -qx "$DOMAIN" /etc/localbox/domains.conf; then
    echo "$DOMAIN" >> /etc/localbox/domains.conf
fi
chown www:mail /etc/localbox/domains.conf
chmod 640 /etc/localbox/domains.conf

echo ">> Restarting services..."
rc-service mail-ingest start
rc-service contact-service start 2>/dev/null || true

echo ">> Persisting config..."
lbu add /etc/localbox/domains.conf
lbu commit -d

echo "=== Migration complete ==="
find "$NEW_DOMAIN" -maxdepth 3 -type d | sort
