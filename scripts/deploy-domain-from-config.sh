#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")

ENV_PI_HOST=${PI_HOST:-}
ENV_PI_USER=${PI_USER:-}
if [ -f "$PROJECT_DIR/settings.conf" ]; then
    . "$PROJECT_DIR/settings.conf"
fi
if [ -n "$ENV_PI_HOST" ]; then
    PI_HOST=$ENV_PI_HOST
fi
if [ -n "$ENV_PI_USER" ]; then
    PI_USER=$ENV_PI_USER
fi

CONFIG=${1:-}
PI_HOST=${PI_HOST:-${2:-192.168.0.6}}
PI_USER=${PI_USER:-root}

if [ -z "$CONFIG" ] || [ ! -f "$CONFIG" ]; then
    echo "Usage: PI_HOST=192.168.0.6 $0 /path/domain.conf"
    exit 1
fi

REMOTE="/tmp/localbox-domain-$(date +%Y%m%d-%H%M%S).conf"

echo ">> Copying domain config to $PI_USER@$PI_HOST:$REMOTE"
scp "$CONFIG" "$PI_USER@$PI_HOST:$REMOTE"

echo ">> Provisioning domain..."
ssh "$PI_USER@$PI_HOST" "set -eu
if [ ! -x /usr/local/sbin/localbox-provision-template ]; then
    echo 'ERROR: /usr/local/sbin/localbox-provision-template is missing'
    exit 1
fi
/usr/local/sbin/localbox-provision-template '$REMOTE'
if [ -x /usr/local/sbin/localbox-add-email-routes ]; then
    /usr/local/sbin/localbox-add-email-routes '$REMOTE'
fi
chown -R www:mail /mnt/data/localbox 2>/dev/null || true
chmod -R g+rwX /mnt/data/localbox 2>/dev/null || true
rc-service mail-ingest restart 2>/dev/null || true
rc-service contact-service restart 2>/dev/null || true
rc-service nginx restart 2>/dev/null || true
lbu add /etc/localbox 2>/dev/null || true
lbu add /var/log/mail-ingest.log 2>/dev/null || true
lbu add /var/log/contact-service.log 2>/dev/null || true
lbu commit -d 2>/dev/null || true
rm -f '$REMOTE'
rc-status
"

echo "Domain deployment complete."
