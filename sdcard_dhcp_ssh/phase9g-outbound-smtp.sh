#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
INVENTORY=${INVENTORY:-$SCRIPT_DIR/localbox-inventory.sh}
SMTP_CONFIG=${SMTP_CONFIG:-$PROJECT_DIR/smtp.conf}

PI_HOST=${PI_HOST:-192.168.0.7}
PI_USER=${PI_USER:-root}
DEVICE_NAME=${DEVICE_NAME:-localbox-001}
LOCALBOX_DOMAIN=${LOCALBOX_DOMAIN:-localboxtest.co.za}
RUN_SMTP_TEST=${RUN_SMTP_TEST:-auto}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/localbox_phase9g_known_hosts -o ConnectTimeout=10"}

echo "Phase 9g target: $PI_USER@$PI_HOST"
echo "Goal: install outbound SMTP relay config for LocalBox app and ALPS/Postfix"
echo "Device: $DEVICE_NAME"
echo "Domain: $LOCALBOX_DOMAIN"
echo "SMTP:  $SMTP_CONFIG"

[ -f "$SMTP_CONFIG" ] || {
    echo "ERROR: SMTP config not found: $SMTP_CONFIG" >&2
    echo "Copy ../smtp.conf.example to ../smtp.conf and set Brevo SMTP username/password." >&2
    exit 1
}
if [ -x "$INVENTORY" ]; then
    "$INVENTORY" validate-device "$DEVICE_NAME" >/dev/null
fi

get_conf() {
    key_re=$1
    awk -F= -v key_re="$key_re" '
        /^[[:space:]]*($|#)/ { next }
        {
            k=tolower($1)
            gsub(/^[ \t]+|[ \t]+$/, "", k)
            if (k ~ key_re) {
                v=$0
                sub(/^[^=]*=/, "", v)
                gsub(/^[ \t]+|[ \t]+$/, "", v)
                gsub(/^["'\'']|["'\'']$/, "", v)
                print v
                exit
            }
        }
    ' "$SMTP_CONFIG"
}

enabled=$(get_conf '^enabled$')
host=$(get_conf '^(host|smtp_host)$')
port=$(get_conf '^(port|smtp_port)$')
username=$(get_conf '^(username|user|smtp_username)$')
password=$(get_conf '^(password|api_key|apikey|key|smtp_password)$')
from=$(get_conf '^(from|sender)$')
to=$(get_conf '^(to|recipient)$')
port=${port:-587}

case "$enabled" in
    true|1|yes|TRUE|YES) ;;
    *) echo "ERROR: enabled=true is required in $SMTP_CONFIG" >&2; exit 1 ;;
esac
[ -n "$host" ] || { echo "ERROR: host is required in $SMTP_CONFIG" >&2; exit 1; }
[ -n "$username" ] || { echo "ERROR: username is required in $SMTP_CONFIG" >&2; exit 1; }
[ -n "$password" ] || { echo "ERROR: password is required in $SMTP_CONFIG" >&2; exit 1; }
case "$port" in
    *[!0-9]*|"") echo "ERROR: port must be numeric in $SMTP_CONFIG" >&2; exit 1 ;;
esac
case "$from" in
    *@*) ;;
    *) echo "ERROR: from must be a valid sender address in $SMTP_CONFIG" >&2; exit 1 ;;
esac
from_domain=${from#*@}
[ "$from_domain" = "$LOCALBOX_DOMAIN" ] || {
    echo "ERROR: from=$from is not in $LOCALBOX_DOMAIN" >&2
    echo "Set from=admin@$LOCALBOX_DOMAIN in $SMTP_CONFIG for this install." >&2
    exit 1
}
if [ -n "$to" ]; then
    case "$to" in
        *@*) ;;
        *) echo "ERROR: to=$to is not a valid email address in $SMTP_CONFIG" >&2; exit 1 ;;
    esac
fi

echo ">> Installing SMTP config on Pi..."
ssh $SSH_OPTS "$PI_USER@$PI_HOST" 'mkdir -p /etc/localbox && cat > /tmp/localbox-smtp.conf.new' < "$SMTP_CONFIG"

echo ">> Configuring Postfix relay and restarting services..."
ssh $SSH_OPTS "$PI_USER@$PI_HOST" "LOCALBOX_DOMAIN='$LOCALBOX_DOMAIN' RUN_SMTP_TEST='$RUN_SMTP_TEST' sh -s" <<'REMOTE'
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

read_conf() {
    key_re=$1
    awk -F= -v key_re="$key_re" '
        /^[[:space:]]*($|#)/ { next }
        {
            k=tolower($1)
            gsub(/^[ \t]+|[ \t]+$/, "", k)
            if (k ~ key_re) {
                v=$0
                sub(/^[^=]*=/, "", v)
                gsub(/^[ \t]+|[ \t]+$/, "", v)
                gsub(/^["'\''"]|["'\''"]$/, "", v)
                print v
                exit
            }
        }
    ' /etc/localbox/smtp.conf
}

install -m 0640 -o root -g mail /tmp/localbox-smtp.conf.new /etc/localbox/smtp.conf
rm -f /tmp/localbox-smtp.conf.new

SMTP_HOST=$(read_conf '^(host|smtp_host)$')
SMTP_PORT=$(read_conf '^(port|smtp_port)$')
SMTP_USER=$(read_conf '^(username|user|smtp_username)$')
SMTP_PASS=$(read_conf '^(password|api_key|apikey|key|smtp_password)$')
SMTP_FROM=$(read_conf '^(from|sender)$')
SMTP_TO=$(read_conf '^(to|recipient)$')
SMTP_PORT=${SMTP_PORT:-587}

[ -n "$SMTP_HOST" ] && [ -n "$SMTP_USER" ] && [ -n "$SMTP_PASS" ] && [ -n "$SMTP_FROM" ] || {
    echo "ERROR: installed smtp.conf is incomplete" >&2
    exit 1
}

if ! command -v postconf >/dev/null 2>&1; then
    echo "ERROR: postfix is not installed. Run Phase 7 first." >&2
    exit 1
fi

printf '[%s]:%s %s:%s\n' "$SMTP_HOST" "$SMTP_PORT" "$SMTP_USER" "$SMTP_PASS" > /etc/postfix/sasl_passwd
chmod 600 /etc/postfix/sasl_passwd
postmap lmdb:/etc/postfix/sasl_passwd
[ -f /etc/postfix/aliases ] || printf 'postmaster: root\nroot: root\n' > /etc/postfix/aliases
postalias lmdb:/etc/postfix/aliases
postconf -e "relayhost = [$SMTP_HOST]:$SMTP_PORT"
postconf -e "smtp_sasl_auth_enable = yes"
postconf -e "smtp_sasl_password_maps = lmdb:/etc/postfix/sasl_passwd"
postconf -e "smtp_sasl_security_options = noanonymous"
postconf -e "smtp_tls_security_level = encrypt"
postconf -e "alias_maps = lmdb:/etc/postfix/aliases"
postconf -e "alias_database = lmdb:/etc/postfix/aliases"

rc-update add postfix default 2>/dev/null || true
rc-service postfix restart </dev/null >/tmp/phase9g-postfix.log 2>&1 || {
    echo "ERROR: postfix restart failed" >&2
    cat /tmp/phase9g-postfix.log >&2
    exit 1
}
for service in mail-ingest contact-service alps; do
    if [ -x "/etc/init.d/$service" ]; then
        if ! rc-service "$service" restart </dev/null >/tmp/phase9g-"$service".log 2>&1; then
            sleep 2
            if ! rc-service "$service" status >/dev/null 2>&1; then
                echo "ERROR: $service restart failed" >&2
                cat /tmp/phase9g-"$service".log >&2
                exit 1
            fi
            echo "WARN: $service restart reported an error but service is started"
        fi
    fi
done

if [ "$RUN_SMTP_TEST" = "true" ] || { [ "$RUN_SMTP_TEST" = "auto" ] && [ -n "$SMTP_TO" ]; }; then
    if command -v contact-service >/dev/null 2>&1; then
        echo ">> Sending contact-service SMTP test..."
        contact-service test-smtp "$LOCALBOX_DOMAIN" >/tmp/phase9g-smtp-test.log 2>&1 || {
            echo "ERROR: contact-service SMTP test failed" >&2
            cat /tmp/phase9g-smtp-test.log >&2
            exit 1
        }
        cat /tmp/phase9g-smtp-test.log
    else
        echo "WARN: contact-service binary not found; SMTP test skipped"
    fi
else
    echo ">> SMTP send test skipped; set to= in smtp.conf or RUN_SMTP_TEST=true."
fi

ensure_lbu_media
mount -o remount,rw /media/mmcblk0p1 2>/dev/null || true
lbu add /etc/localbox/smtp.conf
lbu add /etc/postfix/main.cf /etc/postfix/sasl_passwd /etc/postfix/sasl_passwd.lmdb /etc/postfix/aliases /etc/postfix/aliases.lmdb
lbu add /etc/lbu/lbu.conf
lbu commit -d
mount -o remount,ro /media/mmcblk0p1 2>/dev/null || true

echo ">> Phase 9g complete."
printf 'SMTP relay: %s:%s\n' "$SMTP_HOST" "$SMTP_PORT"
printf 'SMTP from:  %s\n' "$SMTP_FROM"
if [ -n "$SMTP_TO" ]; then
    printf 'Test/to:    %s\n' "$SMTP_TO"
fi
rc-service postfix status </dev/null
REMOTE

if [ -x "$INVENTORY" ]; then
    "$INVENTORY" phase "$DEVICE_NAME" phase9g-outbound-smtp complete detail="smtp relay $host:$port from $from"
fi

echo
echo "Phase 9g finished."
