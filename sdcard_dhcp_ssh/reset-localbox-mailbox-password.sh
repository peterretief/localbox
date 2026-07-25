#!/bin/sh
set -eu

PI_HOST=${PI_HOST:-192.168.0.3}
PI_USER=${PI_USER:-root}
LOCALBOX_DOMAIN=${LOCALBOX_DOMAIN:-localboxtest.co.za}
LOCALBOX_USER=${LOCALBOX_USER:-admin}
LOCALBOX_EMAIL=${LOCALBOX_EMAIL:-$LOCALBOX_USER@$LOCALBOX_DOMAIN}
LOCALBOX_PASSWORD=${LOCALBOX_PASSWORD:-}
GENERATE_PASSWORD=${GENERATE_PASSWORD:-true}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/localbox_reset_password_known_hosts -o ConnectTimeout=10"}

usage() {
    cat <<EOF
Usage:
  PI_HOST=192.168.0.3 LOCALBOX_DOMAIN=localboxtest.co.za LOCALBOX_USER=admin $0
  PI_HOST=192.168.0.3 LOCALBOX_EMAIL=admin@localboxtest.co.za LOCALBOX_PASSWORD='new-password' $0

Resets both LocalBox app login and Dovecot/ALPS IMAP login for one mailbox,
then rebuilds mail maps and persists with lbu.
EOF
}

case "${1:-}" in
    -h|--help|help)
        usage
        exit 0
        ;;
esac

if [ -z "$LOCALBOX_PASSWORD" ]; then
    if [ "$GENERATE_PASSWORD" = "true" ] || [ "$GENERATE_PASSWORD" = "1" ] || [ "$GENERATE_PASSWORD" = "yes" ]; then
        LOCALBOX_PASSWORD=$(openssl rand -base64 24 | tr -d '\n=')
    else
        echo "ERROR: LOCALBOX_PASSWORD is required when GENERATE_PASSWORD=false" >&2
        exit 1
    fi
fi

case "$LOCALBOX_EMAIL" in
    *@*)
        LOCALBOX_USER=${LOCALBOX_EMAIL%@*}
        LOCALBOX_DOMAIN=${LOCALBOX_EMAIL#*@}
        ;;
    *)
        LOCALBOX_EMAIL=$LOCALBOX_USER@$LOCALBOX_DOMAIN
        ;;
esac

echo "Reset LocalBox mailbox password"
echo "Target:  $PI_USER@$PI_HOST"
echo "Mailbox: $LOCALBOX_EMAIL"

ssh $SSH_OPTS "$PI_USER@$PI_HOST" \
    "LOCALBOX_EMAIL='$LOCALBOX_EMAIL' LOCALBOX_USER='$LOCALBOX_USER' LOCALBOX_DOMAIN='$LOCALBOX_DOMAIN' LOCALBOX_PASSWORD='$LOCALBOX_PASSWORD' sh -s" <<'REMOTE'
set -eu

LOCALBOX_ETC=${LOCALBOX_ETC:-/etc/localbox}
LOCALBOX_BASE=${LOCALBOX_BASE:-/mnt/data/localbox}
USERS_FILE=$LOCALBOX_ETC/users.conf
DOVECOT_USERS_FILE=/etc/dovecot/localbox/users

[ -f "$USERS_FILE" ] || {
    echo "ERROR: users file not found: $USERS_FILE" >&2
    exit 1
}

localbox_password_hash() {
    password=$1
    salt=$(openssl rand -base64 18 | tr -dc 'A-Za-z0-9' | cut -c1-16)
    salt_b64=$(printf '%s' "$salt" | base64 | tr -d '\n=')
    key=$(openssl kdf -binary -keylen 32 \
        -kdfopt digest:SHA256 \
        -kdfopt "pass:$password" \
        -kdfopt "salt:$salt" \
        -kdfopt iter:120000 \
        PBKDF2 | base64 | tr -d '\n=')
    printf 'pbkdf2-sha256$120000$%s$%s' "$salt_b64" "$key"
}

LOCALBOX_HASH=$(localbox_password_hash "$LOCALBOX_PASSWORD")
tmp_users=$(mktemp)
found=0
awk -F: -v user="$LOCALBOX_USER" -v domain="$LOCALBOX_DOMAIN" -v hash="$LOCALBOX_HASH" '
    BEGIN { OFS = ":" }
    $1 == user && $2 == domain {
        $3 = hash
        print
        found = 1
        next
    }
    { print }
    END {
        if (!found) {
            exit 12
        }
    }
' "$USERS_FILE" > "$tmp_users" || {
    status=$?
    rm -f "$tmp_users"
    if [ "$status" -eq 12 ]; then
        echo "ERROR: mailbox not found in $USERS_FILE: $LOCALBOX_EMAIL" >&2
    fi
    exit "$status"
}
cat "$tmp_users" > "$USERS_FILE"
rm -f "$tmp_users"
chown www:mail "$USERS_FILE" 2>/dev/null || true
chmod 640 "$USERS_FILE"

WWW_UID=$(id -u www)
MAIL_GID=$(getent group mail | cut -d: -f3)
DOVE_HASH=$(printf '%s\n' "$LOCALBOX_PASSWORD" | openssl passwd -6 -stdin)
DOVE_LINE="$LOCALBOX_EMAIL:$DOVE_HASH:$WWW_UID:$MAIL_GID::$LOCALBOX_BASE/domains/$LOCALBOX_DOMAIN/mail/$LOCALBOX_USER::"
mkdir -p "$(dirname "$DOVECOT_USERS_FILE")"
touch "$DOVECOT_USERS_FILE"
tmp_dovecot=$(mktemp)
awk -F: -v address="$LOCALBOX_EMAIL" -v line="$DOVE_LINE" '
    BEGIN { found = 0 }
    tolower($1) == tolower(address) {
        print line
        found = 1
        next
    }
    NF { print }
    END {
        if (!found) {
            print line
        }
    }
' "$DOVECOT_USERS_FILE" > "$tmp_dovecot"
cat "$tmp_dovecot" > "$DOVECOT_USERS_FILE"
rm -f "$tmp_dovecot"
chown root:mail "$DOVECOT_USERS_FILE"
chmod 664 "$DOVECOT_USERS_FILE"

if [ -x /usr/local/sbin/localbox-sync-mailmaps ]; then
    /usr/local/sbin/localbox-sync-mailmaps
else
    echo "WARN: /usr/local/sbin/localbox-sync-mailmaps missing; restarting services only"
fi

rc-service dovecot restart </dev/null >/tmp/localbox-reset-dovecot.log 2>&1
rc-service postfix restart </dev/null >/tmp/localbox-reset-postfix.log 2>&1 || true
rc-service mail-ingest restart </dev/null >/tmp/localbox-reset-mail-ingest.log 2>&1 || true
rc-service contact-service restart </dev/null >/tmp/localbox-reset-contact-service.log 2>&1 || true
rc-service alps restart </dev/null >/tmp/localbox-reset-alps.log 2>&1 || true

if command -v /usr/local/bin/mail-ingest >/dev/null 2>&1; then
    printf '%s\n' "$LOCALBOX_PASSWORD" | /usr/local/bin/mail-ingest auth-check "$LOCALBOX_EMAIL"
fi

if command -v doveadm >/dev/null 2>&1; then
    printf '%s\n' "$LOCALBOX_PASSWORD" | doveadm auth test "$LOCALBOX_EMAIL" >/tmp/localbox-reset-doveadm.log 2>&1 || {
        cat /tmp/localbox-reset-doveadm.log >&2
        exit 1
    }
fi

mount -o remount,rw /media/mmcblk0p1 2>/dev/null || true
lbu add "$USERS_FILE" "$DOVECOT_USERS_FILE" "$LOCALBOX_ETC" 2>/dev/null || true
lbu commit -d
mount -o remount,ro /media/mmcblk0p1 2>/dev/null || true

echo "Password reset persisted."
REMOTE

echo
echo "Password reset complete."
echo "Mailbox: $LOCALBOX_EMAIL"
echo "Password: $LOCALBOX_PASSWORD"
