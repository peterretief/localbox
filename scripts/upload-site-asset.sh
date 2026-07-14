#!/bin/sh
set -eu

if [ $# -lt 2 ] || [ $# -gt 3 ]; then
    echo "Usage: $0 domain local-file [remote-name]" >&2
    echo "Example: $0 localboxmail.co.za help-login.mp4" >&2
    exit 1
fi

DOMAIN=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')
LOCAL_FILE=$2
REMOTE_NAME=${3:-$(basename "$LOCAL_FILE")}
PI_HOST=${PI_HOST:-alpine-headless}
PI_USER=${PI_USER:-root}
SITE_DIR="/mnt/data/localbox/domains/$DOMAIN/site"
ASSET_DIR="$SITE_DIR/assets"
REMOTE_PATH="$ASSET_DIR/$REMOTE_NAME"

if [ -z "$DOMAIN" ]; then
    echo "Invalid domain: $DOMAIN" >&2
    exit 1
fi

OLD_IFS=$IFS
IFS=.
for label in $DOMAIN; do
    IFS=$OLD_IFS
    case "$label" in
        ""|-*|*-|*[!a-z0-9-]*)
            echo "Invalid domain: $DOMAIN" >&2
            exit 1
            ;;
    esac
    IFS=.
done
IFS=$OLD_IFS

case "$REMOTE_NAME" in
    ""|.*|*/*|*..*|*[!A-Za-z0-9._-]*)
        echo "Invalid remote file name: $REMOTE_NAME" >&2
        exit 1
        ;;
esac

if [ ! -f "$LOCAL_FILE" ]; then
    echo "Local file not found: $LOCAL_FILE" >&2
    exit 1
fi

ssh "$PI_USER@$PI_HOST" "mkdir -p '$ASSET_DIR'"
scp "$LOCAL_FILE" "$PI_USER@$PI_HOST:$REMOTE_PATH"
ssh "$PI_USER@$PI_HOST" "chown www:mail '$REMOTE_PATH' && chmod 664 '$REMOTE_PATH' && ls -lh '$REMOTE_PATH'"

echo "Uploaded:"
echo "  https://$DOMAIN/assets/$REMOTE_NAME"
