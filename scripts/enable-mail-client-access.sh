#!/bin/sh
set -eu

LOCALBOX_BASE=${LOCALBOX_BASE:-/mnt/data/localbox}
MAIL_CLIENT_HOST=${MAIL_CLIENT_HOST:-*}
MAIL_DOMAIN=${MAIL_DOMAIN:-}
MAIL_USER=${MAIL_USER:-}

if [ -f "./settings.conf" ]; then
    . ./settings.conf
fi

if [ -z "$MAIL_DOMAIN" ]; then
    printf "Mail domain: "
    read MAIL_DOMAIN
fi
if [ -z "$MAIL_USER" ]; then
    printf "Mailbox user: "
    read MAIL_USER
fi
case "$MAIL_USER" in
    *@*)
        MAIL_DOMAIN=${MAIL_USER#*@}
        MAIL_USER=${MAIL_USER%@*}
        ;;
esac
if [ -z "$MAIL_DOMAIN" ] || [ -z "$MAIL_USER" ]; then
    echo "ERROR: MAIL_DOMAIN and MAIL_USER are required"
    exit 1
fi

echo "================================================"
echo "   Enable LocalBox mail-client access"
echo "================================================"
echo ""
echo "This enables IMAP and SMTP submission for VPN/local clients."
echo "Do not expose ports 143/993/587 publicly."
echo ""

printf "Mail-client password for $MAIL_USER@$MAIL_DOMAIN: "
stty -echo
read MAIL_PASS
stty echo
echo ""
if [ -z "$MAIL_PASS" ]; then
    echo "ERROR: password cannot be empty"
    exit 1
fi

echo ">> Installing Dovecot and Postfix..."
apk add -q dovecot dovecot-openrc postfix postfix-openrc cyrus-sasl cyrus-sasl-login

echo ">> Preparing mailbox folders..."
mkdir -p "$LOCALBOX_BASE/domains/$MAIL_DOMAIN/mail/$MAIL_USER/Maildir/new"
mkdir -p "$LOCALBOX_BASE/domains/$MAIL_DOMAIN/mail/$MAIL_USER/Maildir/cur"
mkdir -p "$LOCALBOX_BASE/domains/$MAIL_DOMAIN/mail/$MAIL_USER/Maildir/tmp"
mkdir -p "$LOCALBOX_BASE/domains/$MAIL_DOMAIN/mail/$MAIL_USER/Maildir/.Sent/new"
mkdir -p "$LOCALBOX_BASE/domains/$MAIL_DOMAIN/mail/$MAIL_USER/Maildir/.Sent/cur"
mkdir -p "$LOCALBOX_BASE/domains/$MAIL_DOMAIN/mail/$MAIL_USER/Maildir/.Sent/tmp"
mkdir -p "$LOCALBOX_BASE/domains/$MAIL_DOMAIN/mail/$MAIL_USER/Maildir/.Drafts/new"
mkdir -p "$LOCALBOX_BASE/domains/$MAIL_DOMAIN/mail/$MAIL_USER/Maildir/.Drafts/cur"
mkdir -p "$LOCALBOX_BASE/domains/$MAIL_DOMAIN/mail/$MAIL_USER/Maildir/.Drafts/tmp"
mkdir -p "$LOCALBOX_BASE/domains/$MAIL_DOMAIN/mail/$MAIL_USER/Maildir/.Trash/new"
mkdir -p "$LOCALBOX_BASE/domains/$MAIL_DOMAIN/mail/$MAIL_USER/Maildir/.Trash/cur"
mkdir -p "$LOCALBOX_BASE/domains/$MAIL_DOMAIN/mail/$MAIL_USER/Maildir/.Trash/tmp"
chown -R www:mail "$LOCALBOX_BASE/domains/$MAIL_DOMAIN/mail/$MAIL_USER"
chmod -R g+rwX "$LOCALBOX_BASE/domains/$MAIL_DOMAIN/mail/$MAIL_USER"

echo ">> Configuring Dovecot..."
mkdir -p /etc/dovecot/localbox
DOVE_HASH=$(openssl passwd -6 "$MAIL_PASS")
WWW_UID=$(id -u www)
MAIL_GID=$(getent group mail | cut -d: -f3)
cat > /etc/dovecot/localbox/users << EOF
$MAIL_USER@$MAIL_DOMAIN:$DOVE_HASH:$WWW_UID:$MAIL_GID::$LOCALBOX_BASE/domains/$MAIL_DOMAIN/mail/$MAIL_USER::
EOF
chmod 644 /etc/dovecot/localbox/users
chown root:root /etc/dovecot/localbox/users

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

cat > /etc/postfix/localbox_virtual_domains << EOF
$MAIL_DOMAIN OK
EOF
cat > /etc/postfix/localbox_virtual_mailboxes << EOF
$MAIL_USER@$MAIL_DOMAIN $MAIL_DOMAIN/mail/$MAIL_USER/Maildir/
EOF
postmap lmdb:/etc/postfix/localbox_virtual_domains
postmap lmdb:/etc/postfix/localbox_virtual_mailboxes

postconf -e "myhostname = $MAIL_DOMAIN"
postconf -e "myorigin = $MAIL_DOMAIN"
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
lbu add /etc/group
lbu commit -d

cat << EOF

================================================
Mail-client access enabled.

Outlook settings:
  Username: $MAIL_USER@$MAIL_DOMAIN
  Password: the mail-client password you entered
  IMAP host: this Pi's VPN IP or VPN hostname
  IMAP port: 143
  IMAP security: None/STARTTLS depending on client
  SMTP host: this Pi's VPN IP or VPN hostname
  SMTP port: 587
  SMTP security: STARTTLS if offered, otherwise None over VPN

Keep ports 143 and 587 reachable only over VPN/local network.
================================================
EOF
