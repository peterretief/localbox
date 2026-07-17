#!/bin/sh
set -eu

LOCALBOX_BASE=${LOCALBOX_BASE:-/mnt/data/localbox}
LOCALBOX_ETC=${LOCALBOX_ETC:-/etc/localbox}
MAIL_CLIENT_HOST=${MAIL_CLIENT_HOST:-*}

if [ -f "./settings.conf" ]; then
    . ./settings.conf
fi

USERS_FILE="$LOCALBOX_ETC/users.conf"
DOVECOT_USERS_FILE=/etc/dovecot/localbox/users

if [ ! -f "$USERS_FILE" ]; then
    echo "ERROR: $USERS_FILE does not exist"
    exit 1
fi

echo "================================================"
echo "   Enable LocalBox mail-client access for all users"
echo "================================================"
echo ""
echo "This enables IMAP and SMTP submission for VPN/local clients."
echo "Do not expose ports 143/993/587 publicly."
echo ""

echo ">> Installing Dovecot and Postfix..."
apk add -q dovecot dovecot-openrc postfix postfix-openrc cyrus-sasl cyrus-sasl-login alpine

echo ">> Preparing mailbox folders..."
mkdir -p /etc/dovecot/localbox
WWW_UID=$(id -u www)
MAIL_GID=$(getent group mail | cut -d: -f3)

tmp_dovecot_users=$(mktemp)
tmp_virtual_domains=$(mktemp)
tmp_virtual_mailboxes=$(mktemp)
trap 'rm -f "$tmp_dovecot_users" "$tmp_virtual_domains" "$tmp_virtual_mailboxes"' EXIT

existing_dovecot_line() {
    address=$1
    [ -f "$DOVECOT_USERS_FILE" ] || return 1
    awk -F: -v address="$address" 'tolower($1) == tolower(address) {print; exit}' "$DOVECOT_USERS_FILE"
}

ensure_mailbox_dirs() {
    domain=$1
    user=$2
    for folder in "" ".Sent" ".Drafts" ".Trash"; do
        mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/$folder/new"
        mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/$folder/cur"
        mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/$folder/tmp"
    done
    chown -R www:mail "$LOCALBOX_BASE/domains/$domain/mail/$user"
    chmod -R g+rwX "$LOCALBOX_BASE/domains/$domain/mail/$user"
}

prompt_mail_password() {
    address=$1
    if [ ! -r /dev/tty ]; then
        return 0
    fi
    printf "Current LocalBox password for %s (leave empty to skip IMAP/SMTP login for this user): " "$address" >&2
    stty -echo < /dev/tty
    read password < /dev/tty
    stty echo < /dev/tty
    echo "" >&2
    printf '%s' "$password"
}

while IFS=: read -r MAIL_USER MAIL_DOMAIN LOCALBOX_HASH rest; do
    [ -n "$MAIL_USER" ] || continue
    case "$MAIL_USER" in \#*) continue ;; esac
    [ -n "$MAIL_DOMAIN" ] || continue

    MAIL_ADDRESS="$MAIL_USER@$MAIL_DOMAIN"
    ensure_mailbox_dirs "$MAIL_DOMAIN" "$MAIL_USER"
    grep -qxF "$MAIL_DOMAIN OK" "$tmp_virtual_domains" 2>/dev/null || printf '%s OK\n' "$MAIL_DOMAIN" >> "$tmp_virtual_domains"
    printf '%s %s/mail/%s/Maildir/\n' "$MAIL_ADDRESS" "$MAIL_DOMAIN" "$MAIL_USER" >> "$tmp_virtual_mailboxes"

    line=$(existing_dovecot_line "$MAIL_ADDRESS" || true)
    if [ -n "$line" ]; then
        printf '%s\n' "$line" >> "$tmp_dovecot_users"
        continue
    fi

    MAIL_PASS=$(prompt_mail_password "$MAIL_ADDRESS")
    if [ -z "$MAIL_PASS" ]; then
        echo "   Skipped Dovecot password for $MAIL_ADDRESS. Reset/change this mailbox password in LocalBox later to enable IMAP/SMTP login."
        continue
    fi
    DOVE_HASH=$(openssl passwd -6 "$MAIL_PASS")
    printf '%s:%s:%s:%s::%s/domains/%s/mail/%s::\n' "$MAIL_ADDRESS" "$DOVE_HASH" "$WWW_UID" "$MAIL_GID" "$LOCALBOX_BASE" "$MAIL_DOMAIN" "$MAIL_USER" >> "$tmp_dovecot_users"
done < "$USERS_FILE"

cp "$tmp_dovecot_users" "$DOVECOT_USERS_FILE"
chmod 664 /etc/dovecot/localbox/users
chown root:mail /etc/dovecot/localbox/users

echo ">> Configuring Dovecot..."
cat > /etc/dovecot/dovecot.conf << EOF
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

echo ">> Configuring Postfix submission..."
mkdir -p /etc/postfix
POSTFIX_INET_INTERFACES=$MAIL_CLIENT_HOST
if [ "$POSTFIX_INET_INTERFACES" = "*" ] || [ "$POSTFIX_INET_INTERFACES" = "0.0.0.0" ]; then
    POSTFIX_INET_INTERFACES=all
fi
postconf -e "inet_interfaces = $POSTFIX_INET_INTERFACES"
postconf -e "inet_protocols = ipv4"

cp "$tmp_virtual_domains" /etc/postfix/localbox_virtual_domains
cp "$tmp_virtual_mailboxes" /etc/postfix/localbox_virtual_mailboxes
postmap lmdb:/etc/postfix/localbox_virtual_domains
postmap lmdb:/etc/postfix/localbox_virtual_mailboxes

PRIMARY_DOMAIN=$(awk 'NR == 1 {print $1}' /etc/postfix/localbox_virtual_domains)
PRIMARY_DOMAIN=${PRIMARY_DOMAIN:-localhost.localdomain}
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

if [ -f /etc/localbox/smtp.conf ]; then
    SMTP_HOST=$(awk -F= 'tolower($1) ~ /^(host|smtp_host)$/ {gsub(/^[ \t]+|[ \t]+$/, "", $2); print $2; exit}' /etc/localbox/smtp.conf)
    SMTP_PORT=$(awk -F= 'tolower($1) ~ /^(port|smtp_port)$/ {gsub(/^[ \t]+|[ \t]+$/, "", $2); print $2; exit}' /etc/localbox/smtp.conf)
    SMTP_USER=$(awk -F= 'tolower($1) ~ /^(username|user|smtp_username)$/ {gsub(/^[ \t]+|[ \t]+$/, "", $2); print $2; exit}' /etc/localbox/smtp.conf)
    SMTP_PASS=$(awk -F= 'tolower($1) ~ /^(password|api_key|apikey|key|smtp_password)$/ {gsub(/^[ \t]+|[ \t]+$/, "", $2); print $2; exit}' /etc/localbox/smtp.conf)
    SMTP_PORT=${SMTP_PORT:-587}
    if [ -n "$SMTP_HOST" ] && [ -n "$SMTP_USER" ] && [ -n "$SMTP_PASS" ]; then
        echo "[$SMTP_HOST]:$SMTP_PORT $SMTP_USER:$SMTP_PASS" > /etc/postfix/sasl_passwd
        chmod 600 /etc/postfix/sasl_passwd
        postmap lmdb:/etc/postfix/sasl_passwd
        postconf -e "relayhost = [$SMTP_HOST]:$SMTP_PORT"
        postconf -e "smtp_sasl_auth_enable = yes"
        postconf -e "smtp_sasl_password_maps = lmdb:/etc/postfix/sasl_passwd"
        postconf -e "smtp_sasl_security_options = noanonymous"
        postconf -e "smtp_tls_security_level = encrypt"
    fi
fi

if [ -f /etc/postfix/master.cf ]; then
    cp /etc/postfix/master.cf /etc/postfix/master.cf.localbox.bak
fi
if ! grep -q '^submission inet' /etc/postfix/master.cf; then
    cat >> /etc/postfix/master.cf << 'EOF'
submission inet n       -       n       -       -       smtpd
  -o syslog_name=postfix/submission
  -o smtpd_sasl_auth_enable=yes
  -o smtpd_recipient_restrictions=permit_sasl_authenticated,reject
EOF
fi

echo ">> Starting services..."
rc-update add dovecot default 2>/dev/null || true
rc-update add postfix default 2>/dev/null || true
rc-service dovecot restart
rc-service postfix restart

echo ">> Persisting Alpine local backup state..."
lbu add /etc/dovecot
lbu add /etc/postfix
lbu add /etc/init.d/dovecot
lbu add /etc/init.d/postfix
lbu add /etc/runlevels/default/dovecot 2>/dev/null || true
lbu add /etc/runlevels/default/postfix 2>/dev/null || true
lbu add /etc/apk/world
lbu add /etc/group
lbu add /etc/passwd
lbu commit -d

cat << EOF

================================================
Mail-client access enabled for configured LocalBox mailboxes.

Outlook settings:
  Username: the full email address
  Password: the LocalBox password for that mailbox
  IMAP host: this Pi's VPN IP or VPN hostname
  IMAP port: 143
  IMAP security: None/STARTTLS depending on client
  SMTP host: this Pi's VPN IP or VPN hostname
  SMTP port: 587
  SMTP security: STARTTLS if offered, otherwise None over VPN

Keep ports 143 and 587 reachable only over VPN/local network.
If a user was skipped because no password was entered, reset or change that
mailbox password in LocalBox once. Future LocalBox password changes update
the Dovecot IMAP/SMTP password file automatically.
================================================
EOF
