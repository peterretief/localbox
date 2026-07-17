#!/bin/sh
set -eu

PASSWORD=${1:-}
LOCALBOX_BASE=${LOCALBOX_BASE:-/mnt/data/localbox}
LOCALBOX_ETC=${LOCALBOX_ETC:-/etc/localbox}
USERS_FILE="$LOCALBOX_ETC/users.conf"
DOVECOT_USERS_FILE=/etc/dovecot/localbox/users

if [ -z "$PASSWORD" ]; then
    echo "Usage: localbox-reset-passwords TEMPORARY_PASSWORD" >&2
    exit 1
fi
if [ ! -f "$USERS_FILE" ]; then
    echo "ERROR: $USERS_FILE does not exist" >&2
    exit 1
fi

timestamp=$(date +%Y%m%d%H%M%S)
cp -p "$USERS_FILE" "$USERS_FILE.bak-$timestamp"

localbox_password_hash() {
    password=$1
    salt=$(openssl rand -base64 18 | tr -dc 'A-Za-z0-9' | cut -c1-16)
    salt_b64=$(printf '%s' "$salt" | base64 | tr -d '\n=')
    if key=$(openssl kdf -binary -keylen 32 \
        -kdfopt digest:SHA256 \
        -kdfopt "pass:$password" \
        -kdfopt "salt:$salt" \
        -kdfopt iter:120000 \
        PBKDF2 2>/dev/null | base64 | tr -d '\n='); then
        printf 'pbkdf2-sha256$120000$%s$%s' "$salt_b64" "$key"
        return
    fi

    printf '%s' "$password" | openssl dgst -sha512 -binary | base64 | tr -d '\n'
}

web_hash=$(localbox_password_hash "$PASSWORD")
tmp_users=$(mktemp)
awk -F: -v hash="$web_hash" '
    BEGIN { OFS = ":" }
    /^[[:space:]]*$/ || /^[[:space:]]*#/ { print; next }
    NF >= 3 { $3 = hash; print; next }
    { print }
' "$USERS_FILE" > "$tmp_users"
cat "$tmp_users" > "$USERS_FILE"
rm -f "$tmp_users"
chown www:mail "$USERS_FILE" 2>/dev/null || true
chmod 640 "$USERS_FILE"

if command -v openssl >/dev/null 2>&1 && id www >/dev/null 2>&1 && getent group mail >/dev/null 2>&1; then
    mkdir -p /etc/dovecot/localbox
    if [ -f "$DOVECOT_USERS_FILE" ]; then
        cp -p "$DOVECOT_USERS_FILE" "$DOVECOT_USERS_FILE.bak-$timestamp"
    fi

    dove_hash=$(openssl passwd -6 "$PASSWORD")
    www_uid=$(id -u www)
    mail_gid=$(getent group mail | cut -d: -f3)
    tmp_dovecot=$(mktemp)
    awk -F: -v hash="$dove_hash" -v uid="$www_uid" -v gid="$mail_gid" -v base="$LOCALBOX_BASE" '
        /^[[:space:]]*$/ || /^[[:space:]]*#/ { next }
        NF >= 2 {
            user = $1
            domain = $2
            printf "%s@%s:%s:%s:%s::%s/domains/%s/mail/%s::\n", user, domain, hash, uid, gid, base, domain, user
        }
    ' "$USERS_FILE" > "$tmp_dovecot"
    cat "$tmp_dovecot" > "$DOVECOT_USERS_FILE"
    rm -f "$tmp_dovecot"
    chown root:mail "$DOVECOT_USERS_FILE" 2>/dev/null || true
    chmod 664 "$DOVECOT_USERS_FILE"
fi

if command -v postmap >/dev/null 2>&1 && [ -d /etc/postfix ]; then
    tmp_domains=$(mktemp)
    tmp_mailboxes=$(mktemp)
    awk -F: -v base="$LOCALBOX_BASE" '
        /^[[:space:]]*$/ || /^[[:space:]]*#/ { next }
        NF >= 2 {
            user = $1
            domain = $2
            domains[domain] = 1
            printf "%s@%s %s/mail/%s/Maildir/\n", user, domain, domain, user >> mailboxes
        }
        END {
            for (domain in domains) {
                printf "%s OK\n", domain
            }
        }
    ' mailboxes="$tmp_mailboxes" "$USERS_FILE" > "$tmp_domains"
    cp "$tmp_domains" /etc/postfix/localbox_virtual_domains
    cp "$tmp_mailboxes" /etc/postfix/localbox_virtual_mailboxes
    rm -f "$tmp_domains" "$tmp_mailboxes"
    postmap lmdb:/etc/postfix/localbox_virtual_domains 2>/dev/null || postmap /etc/postfix/localbox_virtual_domains 2>/dev/null || true
    postmap lmdb:/etc/postfix/localbox_virtual_mailboxes 2>/dev/null || postmap /etc/postfix/localbox_virtual_mailboxes 2>/dev/null || true
fi

if [ -f /etc/radicale/config ] && grep -Eq '^[[:space:]]*type[[:space:]]*=[[:space:]]*dovecot[[:space:]]*$' /etc/radicale/config; then
    :
elif command -v htpasswd >/dev/null 2>&1 && [ -f /etc/radicale/users ]; then
    cp -p /etc/radicale/users /etc/radicale/users.bak-$timestamp
    : > /etc/radicale/users
    while IFS=: read -r user domain hash rest; do
        [ -n "$user" ] || continue
        case "$user" in \#*) continue ;; esac
        [ -n "$domain" ] || continue
        hash=$(openssl passwd -6 "$PASSWORD")
        printf '%s:%s\n' "$user@$domain" "$hash" >> /etc/radicale/users
    done < "$USERS_FILE"
    chown root:radicale /etc/radicale/users 2>/dev/null || true
    chmod 640 /etc/radicale/users
fi

rc-service dovecot restart 2>/dev/null || true
rc-service postfix restart 2>/dev/null || true
rc-service radicale restart 2>/dev/null || true
rc-service mail-ingest restart 2>/dev/null || true

lbu add "$USERS_FILE" 2>/dev/null || true
lbu add /etc/dovecot 2>/dev/null || true
lbu add /etc/postfix 2>/dev/null || true
lbu add /etc/radicale 2>/dev/null || true
lbu commit -d 2>/dev/null || true

echo "Reset LocalBox mailbox passwords in $USERS_FILE."
if [ -f "$DOVECOT_USERS_FILE" ]; then
    echo "Reset Dovecot IMAP/SMTP passwords in $DOVECOT_USERS_FILE."
fi
if [ -f /etc/radicale/config ] && grep -Eq '^[[:space:]]*type[[:space:]]*=[[:space:]]*dovecot[[:space:]]*$' /etc/radicale/config; then
    echo "Radicale contacts/calendar uses Dovecot authentication."
elif [ -f /etc/radicale/users ]; then
    echo "Reset Radicale contacts/calendar passwords in /etc/radicale/users."
fi
