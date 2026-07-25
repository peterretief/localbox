#!/bin/sh
set -eu

PI_HOST=${PI_HOST:-192.168.0.7}
PI_USER=${PI_USER:-root}
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
INVENTORY=${INVENTORY:-$SCRIPT_DIR/localbox-inventory.sh}
DEVICE_NAME=${DEVICE_NAME:-$PI_HOST}
LOCALBOX_DOMAIN=${LOCALBOX_DOMAIN:-localboxtest.co.za}
LOCALBOX_ADMIN_USER=${LOCALBOX_ADMIN_USER:-admin}
LOCALBOX_ADMIN_EMAIL=${LOCALBOX_ADMIN_EMAIL:-$LOCALBOX_ADMIN_USER@$LOCALBOX_DOMAIN}
LOCALBOX_ADMIN_RECOVERY_EMAIL=${LOCALBOX_ADMIN_RECOVERY_EMAIL:-${RECOVERY_EMAIL:-}}
LOCALBOX_BASE=${LOCALBOX_BASE:-/mnt/data/localbox}
LOCALBOX_ETC=${LOCALBOX_ETC:-/etc/localbox}
MAIL_CLIENT_HOST=${MAIL_CLIENT_HOST:-*}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/localbox_phase8_known_hosts -o ConnectTimeout=10"}

if [ -z "${LOCALBOX_ADMIN_PASSWORD:-}" ]; then
    LOCALBOX_ADMIN_PASSWORD=$(openssl rand -base64 24 | tr -d '\n=')
    GENERATED_PASSWORD=1
else
    GENERATED_PASSWORD=0
fi

echo "Phase 8 target: $PI_USER@$PI_HOST"
echo "Goal: first LocalBox domain/mailbox, mail maps, services, lbu persist"
echo "Domain:        $LOCALBOX_DOMAIN"
echo "Admin mailbox: $LOCALBOX_ADMIN_EMAIL"
echo "Recovery:      ${LOCALBOX_ADMIN_RECOVERY_EMAIL:-required}"

ssh $SSH_OPTS "$PI_USER@$PI_HOST" \
    "LOCALBOX_DOMAIN='$LOCALBOX_DOMAIN' LOCALBOX_ADMIN_EMAIL='$LOCALBOX_ADMIN_EMAIL' LOCALBOX_ADMIN_RECOVERY_EMAIL='$LOCALBOX_ADMIN_RECOVERY_EMAIL' LOCALBOX_ADMIN_PASSWORD='$LOCALBOX_ADMIN_PASSWORD' LOCALBOX_BASE='$LOCALBOX_BASE' LOCALBOX_ETC='$LOCALBOX_ETC' MAIL_CLIENT_HOST='$MAIL_CLIENT_HOST' sh -s" <<'REMOTE'
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

normalize() {
    printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]'
}

valid_domain() {
    printf '%s' "$1" | grep -Eq '^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$'
}

valid_user() {
    printf '%s' "$1" | grep -Eq '^[a-z0-9._-]+$'
}

valid_recovery_email() {
    recovery=$1
    mailbox_domain=$2
    printf '%s' "$recovery" | grep -Eq '^[^:@[:space:]]+@[^:@[:space:]]+\.[^:@[:space:]]+$' || return 1
    recovery_domain=${recovery#*@}
    [ "$recovery_domain" != "$mailbox_domain" ] || return 1
    if [ -f "$LOCALBOX_ETC/domains.conf" ]; then
        ! grep -qxF "$recovery_domain" "$LOCALBOX_ETC/domains.conf"
    fi
}

ensure_line() {
    file=$1
    line=$2
    grep -qxF "$line" "$file" 2>/dev/null || printf '%s\n' "$line" >> "$file"
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

ensure_mailbox_dirs() {
    domain=$1
    user=$2
    for folder in "" ".Sent" ".Drafts" ".Trash"; do
        mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/$folder/new"
        mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/$folder/cur"
        mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/$folder/tmp"
    done
    mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/attachments"
}

rebuild_mail_maps() {
    WWW_UID=$(id -u www)
    MAIL_GID=$(getent group mail | cut -d: -f3)
    DOVECOT_USERS_FILE=/etc/dovecot/localbox/users

    tmp_dovecot_users=$(mktemp)
    tmp_virtual_domains=$(mktemp)
    tmp_virtual_mailboxes=$(mktemp)
    trap 'rm -f "$tmp_dovecot_users" "$tmp_virtual_domains" "$tmp_virtual_mailboxes"' EXIT

    while IFS=: read -r MAIL_USER MAIL_DOMAIN LOCALBOX_HASH rest; do
        [ -n "$MAIL_USER" ] || continue
        case "$MAIL_USER" in \#*) continue ;; esac
        [ -n "$MAIL_DOMAIN" ] || continue

        MAIL_ADDRESS="$MAIL_USER@$MAIL_DOMAIN"
        ensure_mailbox_dirs "$MAIL_DOMAIN" "$MAIL_USER"
        grep -qxF "$MAIL_DOMAIN OK" "$tmp_virtual_domains" 2>/dev/null || printf '%s OK\n' "$MAIL_DOMAIN" >> "$tmp_virtual_domains"
        printf '%s %s/mail/%s/Maildir/\n' "$MAIL_ADDRESS" "$MAIL_DOMAIN" "$MAIL_USER" >> "$tmp_virtual_mailboxes"

        if [ "$MAIL_ADDRESS" = "$LOCALBOX_ADMIN_EMAIL" ]; then
            DOVE_HASH=$(openssl passwd -6 "$LOCALBOX_ADMIN_PASSWORD")
            printf '%s:%s:%s:%s::%s/domains/%s/mail/%s::\n' "$MAIL_ADDRESS" "$DOVE_HASH" "$WWW_UID" "$MAIL_GID" "$LOCALBOX_BASE" "$MAIL_DOMAIN" "$MAIL_USER" >> "$tmp_dovecot_users"
        elif [ -f "$DOVECOT_USERS_FILE" ]; then
            awk -F: -v address="$MAIL_ADDRESS" 'tolower($1) == tolower(address) {print; exit}' "$DOVECOT_USERS_FILE" >> "$tmp_dovecot_users"
        fi
    done < "$LOCALBOX_ETC/users.conf"

    cp "$tmp_dovecot_users" "$DOVECOT_USERS_FILE"
    chmod 664 "$DOVECOT_USERS_FILE"
    chown root:mail "$DOVECOT_USERS_FILE"

    cp "$tmp_virtual_domains" /etc/postfix/localbox_virtual_domains
    cp "$tmp_virtual_mailboxes" /etc/postfix/localbox_virtual_mailboxes
    postmap lmdb:/etc/postfix/localbox_virtual_domains 2>/dev/null || postmap /etc/postfix/localbox_virtual_domains
    postmap lmdb:/etc/postfix/localbox_virtual_mailboxes 2>/dev/null || postmap /etc/postfix/localbox_virtual_mailboxes

    PRIMARY_DOMAIN=$(awk 'NR == 1 {print $1}' /etc/postfix/localbox_virtual_domains)
    PRIMARY_DOMAIN=${PRIMARY_DOMAIN:-$LOCALBOX_DOMAIN}
    POSTFIX_INET_INTERFACES=$MAIL_CLIENT_HOST
    if [ "$POSTFIX_INET_INTERFACES" = "*" ] || [ "$POSTFIX_INET_INTERFACES" = "0.0.0.0" ]; then
        POSTFIX_INET_INTERFACES=all
    fi
    postconf -e "inet_interfaces = $POSTFIX_INET_INTERFACES"
    postconf -e "myhostname = $PRIMARY_DOMAIN"
    postconf -e "myorigin = $PRIMARY_DOMAIN"
    postconf -e "virtual_mailbox_domains = lmdb:/etc/postfix/localbox_virtual_domains"
    postconf -e "virtual_mailbox_maps = lmdb:/etc/postfix/localbox_virtual_mailboxes"
}

mountpoint -q /mnt/data || {
    echo "ERROR: /mnt/data is not mounted. Finish Phase 4 first." >&2
    exit 1
}
[ -x /usr/local/bin/mail-ingest ] || {
    echo "ERROR: /usr/local/bin/mail-ingest is missing. Run Phase 6 first." >&2
    exit 1
}
[ -d /etc/dovecot ] && [ -d /etc/postfix ] || {
    echo "ERROR: Dovecot/Postfix are missing. Run Phase 7 first." >&2
    exit 1
}

LOCALBOX_DOMAIN=$(normalize "$LOCALBOX_DOMAIN")
LOCALBOX_ADMIN_EMAIL=$(normalize "$LOCALBOX_ADMIN_EMAIL")
LOCALBOX_ADMIN_RECOVERY_EMAIL=$(normalize "$LOCALBOX_ADMIN_RECOVERY_EMAIL")
LOCALBOX_ADMIN_USER=${LOCALBOX_ADMIN_EMAIL%@*}
LOCALBOX_ADMIN_DOMAIN=${LOCALBOX_ADMIN_EMAIL#*@}

valid_domain "$LOCALBOX_DOMAIN" || {
    echo "ERROR: invalid domain: $LOCALBOX_DOMAIN" >&2
    exit 1
}
[ "$LOCALBOX_ADMIN_DOMAIN" = "$LOCALBOX_DOMAIN" ] || {
    echo "ERROR: admin mailbox must belong to $LOCALBOX_DOMAIN" >&2
    exit 1
}
valid_user "$LOCALBOX_ADMIN_USER" || {
    echo "ERROR: invalid admin user: $LOCALBOX_ADMIN_USER" >&2
    exit 1
}
[ -n "$LOCALBOX_ADMIN_RECOVERY_EMAIL" ] || {
    echo "ERROR: LOCALBOX_ADMIN_RECOVERY_EMAIL is required" >&2
    exit 1
}
valid_recovery_email "$LOCALBOX_ADMIN_RECOVERY_EMAIL" "$LOCALBOX_DOMAIN" || {
    echo "ERROR: recovery email must be valid and outside this LocalBox: $LOCALBOX_ADMIN_RECOVERY_EMAIL" >&2
    exit 1
}

echo ">> Creating first LocalBox domain and mailbox..."
mkdir -p "$LOCALBOX_ETC" "$LOCALBOX_BASE/domains/$LOCALBOX_DOMAIN/site/assets/images" /etc/dovecot/localbox /etc/postfix
touch "$LOCALBOX_ETC/users.conf" "$LOCALBOX_ETC/domains.conf" "$LOCALBOX_ETC/admins.conf" "$LOCALBOX_ETC/box-admins.conf" "$LOCALBOX_ETC/password-reset-tokens.conf"

ensure_line "$LOCALBOX_ETC/domains.conf" "$LOCALBOX_DOMAIN"
if ! grep -q "^$LOCALBOX_ADMIN_USER:$LOCALBOX_DOMAIN:" "$LOCALBOX_ETC/users.conf"; then
    LOCALBOX_HASH=$(localbox_password_hash "$LOCALBOX_ADMIN_PASSWORD")
    printf '%s:%s:%s:%s\n' "$LOCALBOX_ADMIN_USER" "$LOCALBOX_DOMAIN" "$LOCALBOX_HASH" "$LOCALBOX_ADMIN_RECOVERY_EMAIL" >> "$LOCALBOX_ETC/users.conf"
else
    LOCALBOX_HASH=$(localbox_password_hash "$LOCALBOX_ADMIN_PASSWORD")
    tmp_users=$(mktemp)
    awk -F: -v user="$LOCALBOX_ADMIN_USER" -v domain="$LOCALBOX_DOMAIN" -v hash="$LOCALBOX_HASH" -v recovery="$LOCALBOX_ADMIN_RECOVERY_EMAIL" '
        BEGIN { OFS = ":" }
        $1 == user && $2 == domain {
            $3 = hash
            $4 = recovery
            print
            next
        }
        { print }
    ' "$LOCALBOX_ETC/users.conf" > "$tmp_users"
    cat "$tmp_users" > "$LOCALBOX_ETC/users.conf"
    rm -f "$tmp_users"
fi
ensure_line "$LOCALBOX_ETC/admins.conf" "$LOCALBOX_ADMIN_USER:$LOCALBOX_DOMAIN"
ensure_line "$LOCALBOX_ETC/box-admins.conf" "$LOCALBOX_ADMIN_USER:$LOCALBOX_DOMAIN"
ensure_mailbox_dirs "$LOCALBOX_DOMAIN" "$LOCALBOX_ADMIN_USER"

if [ ! -f "$LOCALBOX_BASE/domains/$LOCALBOX_DOMAIN/site/index.html" ]; then
    cat > "$LOCALBOX_BASE/domains/$LOCALBOX_DOMAIN/site/index.html" <<EOF
<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"><title>$LOCALBOX_DOMAIN</title></head>
<body><main><h1>$LOCALBOX_DOMAIN</h1><p>This LocalBox test site is ready.</p></main></body>
</html>
EOF
fi

if [ ! -f "$LOCALBOX_ETC/secret" ]; then
    openssl rand -hex 32 > "$LOCALBOX_ETC/secret"
fi
if [ ! -f "$LOCALBOX_ETC/sso-secret" ]; then
    openssl rand -hex 32 > "$LOCALBOX_ETC/sso-secret"
fi
cat > "$LOCALBOX_ETC/localbox.conf" <<EOF
domain=$LOCALBOX_DOMAIN
admin_email=$LOCALBOX_ADMIN_EMAIL
base=$LOCALBOX_BASE
webmail_url=https://mail.$LOCALBOX_DOMAIN
created_by=phase8-first-domain
EOF

chown -R www:mail "$LOCALBOX_BASE" /var/localbox 2>/dev/null || true
chmod -R g+rwX "$LOCALBOX_BASE" /var/localbox 2>/dev/null || true
find "$LOCALBOX_BASE" -type d -exec chmod 2775 {} \; 2>/dev/null || true
chown www:mail "$LOCALBOX_ETC"/*.conf "$LOCALBOX_ETC/password-reset-tokens.conf" 2>/dev/null || true
chmod 640 "$LOCALBOX_ETC"/*.conf "$LOCALBOX_ETC/password-reset-tokens.conf" 2>/dev/null || true
chown www:mail "$LOCALBOX_ETC/secret" "$LOCALBOX_ETC/sso-secret" 2>/dev/null || true
chmod 640 "$LOCALBOX_ETC/secret" "$LOCALBOX_ETC/sso-secret"

echo ">> Rebuilding Dovecot/Postfix maps..."
rebuild_mail_maps

echo ">> Restarting services..."
rc-service dovecot restart </dev/null >/tmp/phase8-dovecot.log 2>&1
rc-service postfix restart </dev/null >/tmp/phase8-postfix.log 2>&1
rc-service mail-ingest restart </dev/null >/tmp/phase8-mail-ingest.log 2>&1
rc-service contact-service restart </dev/null >/tmp/phase8-contact-service.log 2>&1
sleep 2
rc-service mail-ingest status >/tmp/phase8-mail-ingest-status.log 2>&1 || {
    echo "ERROR: mail-ingest did not stay started" >&2
    cat /tmp/phase8-mail-ingest.log /tmp/phase8-mail-ingest-status.log >&2
    exit 1
}
rc-service contact-service status >/tmp/phase8-contact-service-status.log 2>&1 || {
    echo "ERROR: contact-service did not stay started" >&2
    cat /tmp/phase8-contact-service.log /tmp/phase8-contact-service-status.log >&2
    exit 1
}

echo ">> Verifying LocalBox login hash..."
auth_check_status=0
if command -v timeout >/dev/null 2>&1; then
    printf '%s\n' "$LOCALBOX_ADMIN_PASSWORD" | timeout 30 /usr/local/bin/mail-ingest auth-check "$LOCALBOX_ADMIN_EMAIL" || auth_check_status=$?
else
    printf '%s\n' "$LOCALBOX_ADMIN_PASSWORD" | /usr/local/bin/mail-ingest auth-check "$LOCALBOX_ADMIN_EMAIL" || auth_check_status=$?
fi
if [ "$auth_check_status" -ne 0 ]; then
    echo "ERROR: LocalBox admin auth-check failed for $LOCALBOX_ADMIN_EMAIL status=$auth_check_status" >&2
    exit 1
fi

echo ">> Persisting first domain..."
ensure_lbu_media
mount -o remount,rw /media/mmcblk0p1 2>/dev/null || true
lbu add "$LOCALBOX_ETC"
lbu add /etc/dovecot
lbu add /etc/postfix
lbu add /etc/init.d/mail-ingest /etc/init.d/contact-service /etc/init.d/dovecot /etc/init.d/postfix 2>/dev/null || true
lbu add /etc/runlevels/default/mail-ingest /etc/runlevels/default/contact-service /etc/runlevels/default/dovecot /etc/runlevels/default/postfix 2>/dev/null || true
lbu add /var/localbox 2>/dev/null || true
lbu add /etc/lbu/lbu.conf
lbu commit -d
mount -o remount,ro /media/mmcblk0p1 2>/dev/null || true

echo ">> Phase 8 complete."
hostname
printf 'Domain: %s\n' "$LOCALBOX_DOMAIN"
printf 'Admin:  %s\n' "$LOCALBOX_ADMIN_EMAIL"
printf 'Recovery: %s\n' "$LOCALBOX_ADMIN_RECOVERY_EMAIL"
rc-service dovecot status || true
rc-service postfix status || true
rc-service mail-ingest status || true
rc-service contact-service status || true
postconf -n | grep -E '^(myhostname|myorigin|virtual_|smtpd_sasl|inet_interfaces)' || true
ls -la "$LOCALBOX_BASE/domains/$LOCALBOX_DOMAIN"
REMOTE

echo
echo "Phase 8 finished."
echo "Admin login: $LOCALBOX_ADMIN_EMAIL"
echo "Recovery email: $LOCALBOX_ADMIN_RECOVERY_EMAIL"
if [ "$GENERATED_PASSWORD" = "1" ]; then
    echo "Generated admin password: $LOCALBOX_ADMIN_PASSWORD"
fi

if [ -x "$INVENTORY" ]; then
    "$INVENTORY" upsert-device "$DEVICE_NAME" hostname="$PI_HOST" pi_host="$PI_HOST" ssh_user="$PI_USER" status=active notes=phase8
    "$INVENTORY" upsert-domain "$LOCALBOX_DOMAIN" device_name="$DEVICE_NAME" admin_email="$LOCALBOX_ADMIN_EMAIL" status=configured
    "$INVENTORY" phase "$DEVICE_NAME" phase8-first-domain complete detail="$LOCALBOX_DOMAIN $LOCALBOX_ADMIN_EMAIL"
fi
