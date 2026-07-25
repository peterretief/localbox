#!/bin/sh
set -eu

PI_HOST=${PI_HOST:-192.168.0.7}
PI_USER=${PI_USER:-root}
LOCALBOX_BASE=${LOCALBOX_BASE:-/mnt/data/localbox}
LOCALBOX_ETC=${LOCALBOX_ETC:-/etc/localbox}
MAIL_CLIENT_HOST=${MAIL_CLIENT_HOST:-*}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/localbox_phase7_known_hosts -o ConnectTimeout=10"}

echo "Phase 7 target: $PI_USER@$PI_HOST"
echo "Goal: Dovecot/Postfix base mail stack, lbu persist"

ssh $SSH_OPTS "$PI_USER@$PI_HOST" \
    "LOCALBOX_BASE='$LOCALBOX_BASE' LOCALBOX_ETC='$LOCALBOX_ETC' MAIL_CLIENT_HOST='$MAIL_CLIENT_HOST' sh -s" <<'REMOTE'
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
[ -x /usr/local/bin/mail-ingest ] || {
    echo "ERROR: /usr/local/bin/mail-ingest is missing. Run Phase 6 first." >&2
    exit 1
}

echo ">> Installing Dovecot/Postfix packages..."
ensure_apk_cache
apk update -q
apk add -q dovecot dovecot-openrc postfix postfix-openrc cyrus-sasl cyrus-sasl-login
apk cache sync 2>/dev/null || true

echo ">> Ensuring users/groups and base files..."
addgroup -S mail 2>/dev/null || true
adduser -D -H -s /sbin/nologin www 2>/dev/null || true
adduser www mail 2>/dev/null || true
adduser nginx mail 2>/dev/null || true
mkdir -p "$LOCALBOX_ETC" "$LOCALBOX_BASE/domains" /etc/dovecot/localbox /etc/postfix
touch "$LOCALBOX_ETC/users.conf" "$LOCALBOX_ETC/domains.conf" "$LOCALBOX_ETC/admins.conf" "$LOCALBOX_ETC/box-admins.conf"
touch /etc/dovecot/localbox/users
chmod 664 /etc/dovecot/localbox/users
chown root:mail /etc/dovecot/localbox/users

WWW_UID=$(id -u www)
MAIL_GID=$(getent group mail | cut -d: -f3)

echo ">> Building initial Dovecot user file and Postfix maps..."
tmp_dovecot_users=$(mktemp)
tmp_virtual_domains=$(mktemp)
tmp_virtual_mailboxes=$(mktemp)
trap 'rm -f "$tmp_dovecot_users" "$tmp_virtual_domains" "$tmp_virtual_mailboxes"' EXIT

while IFS=: read -r MAIL_USER MAIL_DOMAIN LOCALBOX_HASH rest; do
    [ -n "$MAIL_USER" ] || continue
    case "$MAIL_USER" in \#*) continue ;; esac
    [ -n "$MAIL_DOMAIN" ] || continue

    MAIL_ADDRESS="$MAIL_USER@$MAIL_DOMAIN"
    grep -qxF "$MAIL_DOMAIN OK" "$tmp_virtual_domains" 2>/dev/null || printf '%s OK\n' "$MAIL_DOMAIN" >> "$tmp_virtual_domains"
    printf '%s %s/mail/%s/Maildir/\n' "$MAIL_ADDRESS" "$MAIL_DOMAIN" "$MAIL_USER" >> "$tmp_virtual_mailboxes"
    mkdir -p "$LOCALBOX_BASE/domains/$MAIL_DOMAIN/mail/$MAIL_USER/Maildir/new"
    mkdir -p "$LOCALBOX_BASE/domains/$MAIL_DOMAIN/mail/$MAIL_USER/Maildir/cur"
    mkdir -p "$LOCALBOX_BASE/domains/$MAIL_DOMAIN/mail/$MAIL_USER/Maildir/tmp"
done < "$LOCALBOX_ETC/users.conf"

cp "$tmp_dovecot_users" /etc/dovecot/localbox/users
chmod 664 /etc/dovecot/localbox/users
chown root:mail /etc/dovecot/localbox/users
cp "$tmp_virtual_domains" /etc/postfix/localbox_virtual_domains
cp "$tmp_virtual_mailboxes" /etc/postfix/localbox_virtual_mailboxes

postmap lmdb:/etc/postfix/localbox_virtual_domains 2>/dev/null || postmap /etc/postfix/localbox_virtual_domains 2>/dev/null || true
postmap lmdb:/etc/postfix/localbox_virtual_mailboxes 2>/dev/null || postmap /etc/postfix/localbox_virtual_mailboxes 2>/dev/null || true

echo ">> Configuring Dovecot..."
cat > /etc/dovecot/dovecot.conf <<EOF
dovecot_config_version = 2.4.0
dovecot_storage_version = 2.4.0

protocols = imap
listen = $MAIL_CLIENT_HOST
base_dir = /run/dovecot

auth_mechanisms = plain login
auth_allow_cleartext = yes
mail_privileged_group = mail

passdb passwd-file {
  default_password_scheme = SHA512-CRYPT
  auth_username_format = %{user}
  passwd_file_path = /etc/dovecot/localbox/users
}

userdb passwd-file {
  auth_username_format = %{user}
  passwd_file_path = /etc/dovecot/localbox/users
}

mail_driver = maildir
mail_path = ~/Maildir

namespace inbox {
  inbox = yes
  separator = /
  mailbox Sent {
    special_use = \Sent
    auto = subscribe
  }
  mailbox Drafts {
    special_use = \Drafts
    auto = subscribe
  }
  mailbox Trash {
    special_use = \Trash
    auto = subscribe
  }
}

service auth {
  unix_listener /var/spool/postfix/private/auth {
    mode = 0660
    user = postfix
    group = postfix
  }
}

ssl = no
EOF

echo ">> Configuring Postfix base..."
POSTFIX_INET_INTERFACES=$MAIL_CLIENT_HOST
if [ "$POSTFIX_INET_INTERFACES" = "*" ] || [ "$POSTFIX_INET_INTERFACES" = "0.0.0.0" ]; then
    POSTFIX_INET_INTERFACES=all
fi
PRIMARY_DOMAIN=$(awk 'NR == 1 {print $1}' /etc/postfix/localbox_virtual_domains)
PRIMARY_DOMAIN=${PRIMARY_DOMAIN:-localbox-pi.local}

postconf -e "inet_interfaces = $POSTFIX_INET_INTERFACES"
postconf -e "inet_protocols = ipv4"
postconf -e "myhostname = $PRIMARY_DOMAIN"
postconf -e "myorigin = $PRIMARY_DOMAIN"
postconf -e "mydestination = localhost"
postconf -e "virtual_mailbox_domains = lmdb:/etc/postfix/localbox_virtual_domains"
postconf -e "virtual_mailbox_maps = lmdb:/etc/postfix/localbox_virtual_mailboxes"
postconf -e "virtual_mailbox_base = $LOCALBOX_BASE/domains"
postconf -e "virtual_uid_maps = static:$WWW_UID"
postconf -e "virtual_gid_maps = static:$MAIL_GID"
postconf -e "smtpd_sasl_type = dovecot"
postconf -e "smtpd_sasl_path = private/auth"
postconf -e "smtpd_sasl_auth_enable = yes"
postconf -e "smtpd_recipient_restrictions = permit_sasl_authenticated,reject"
postconf -e "smtpd_relay_restrictions = permit_sasl_authenticated,reject_unauth_destination"
postconf -e "smtpd_tls_security_level = may"

if ! grep -q '^submission inet' /etc/postfix/master.cf; then
    cat >> /etc/postfix/master.cf <<'EOF'
submission inet n       -       n       -       -       smtpd
  -o syslog_name=postfix/submission
  -o smtpd_sasl_auth_enable=yes
  -o smtpd_recipient_restrictions=permit_sasl_authenticated,reject
EOF
fi

echo ">> Enabling and starting mail services..."
rc-update add dovecot default 2>/dev/null || true
rc-update add postfix default 2>/dev/null || true
rc-service dovecot restart </dev/null >/tmp/phase7-dovecot.log 2>&1
rc-service postfix restart </dev/null >/tmp/phase7-postfix.log 2>&1

echo ">> Persisting mail stack..."
ensure_lbu_media
mount -o remount,rw /media/mmcblk0p1 2>/dev/null || true
lbu add /etc/dovecot
lbu add /etc/postfix
lbu add /etc/init.d/dovecot
lbu add /etc/init.d/postfix
lbu add /etc/runlevels/default/dovecot 2>/dev/null || true
lbu add /etc/runlevels/default/postfix 2>/dev/null || true
lbu add /etc/apk/world
lbu add /etc/group /etc/passwd /etc/shadow 2>/dev/null || true
lbu add /etc/localbox
lbu add /etc/lbu/lbu.conf
lbu add /etc/apk/cache 2>/dev/null || true
lbu commit -d
mount -o remount,ro /media/mmcblk0p1 2>/dev/null || true

echo ">> Phase 7 complete."
rc-service dovecot status </dev/null || true
rc-service postfix status </dev/null || true
postconf -n | grep -E '^(inet_interfaces|myhostname|virtual_|smtpd_sasl)' || true
REMOTE

echo
echo "Phase 7 finished."
