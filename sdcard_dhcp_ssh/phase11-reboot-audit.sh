#!/bin/sh
set -eu

ACTION=${1:-audit}
PI_HOST=${PI_HOST:-192.168.0.7}
PI_USER=${PI_USER:-root}
DEVICE_NAME=${DEVICE_NAME:-localbox-001}
LOCALBOX_DOMAIN=${LOCALBOX_DOMAIN:-localboxtest.co.za}
LOCALBOX_TUNNEL=${LOCALBOX_TUNNEL:-}
LOCALBOX_BASE=${LOCALBOX_BASE:-/mnt/data/localbox}
LOCALBOX_BIN_DIR=${LOCALBOX_BIN_DIR:-$LOCALBOX_BASE/bin}
EXPECTED_WEB_SERVICE=${EXPECTED_WEB_SERVICE:-http://localhost:80}
EXPECTED_APP_SERVICE=${EXPECTED_APP_SERVICE:-http://localhost:8080}
EXPECTED_MAIL_SERVICE=${EXPECTED_MAIL_SERVICE:-http://localhost:1323}
EXPECTED_FILES_SERVICE=${EXPECTED_FILES_SERVICE:-http://localhost:8082}
INGEST_PROBE=${INGEST_PROBE:-false}
INVENTORY=${INVENTORY:-$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/localbox-inventory.sh}
SSH_KNOWN_HOSTS=${SSH_KNOWN_HOSTS:-/tmp/localbox_phase11_known_hosts}
SSH_OPTS=${SSH_OPTS:-"-o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=$SSH_KNOWN_HOSTS -o ConnectTimeout=10"}
LEGACY_SSH_OPTS=${LEGACY_SSH_OPTS:-"$SSH_OPTS -o HostKeyAlgorithms=+ssh-rsa,ssh-dss -o PubkeyAcceptedAlgorithms=+ssh-rsa"}

usage() {
    cat <<EOF
Usage:
  PI_HOST=192.168.0.7 DEVICE_NAME=localbox-001 LOCALBOX_DOMAIN=localboxtest.co.za $0 [audit|repair]

Audits whether a LocalBox Alpine Pi install survived reboot.

audit   Read-only checks. Default.
repair  Repair known safe local drift, then audit. Requires SSH to work.
EOF
}

case "$ACTION" in
    audit|repair) ;;
    -h|--help|help) usage; exit 0 ;;
    *) usage; exit 1 ;;
esac

quote_sh() {
    printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}

try_ssh() {
    opts=$1
    shift
    # shellcheck disable=SC2086
    ssh $opts "$PI_USER@$PI_HOST" "$@"
}

echo "Phase 11 target: $PI_USER@$PI_HOST"
echo "Goal: reboot-survival audit for LocalBox Pi install"
echo "Action: $ACTION"
echo "Device: $DEVICE_NAME"
echo "Domain: $LOCALBOX_DOMAIN"

if [ -x "$INVENTORY" ]; then
    "$INVENTORY" validate-device "$DEVICE_NAME" >/dev/null || true
    if [ -n "$LOCALBOX_TUNNEL" ]; then
        "$INVENTORY" validate-publish "$DEVICE_NAME" "$LOCALBOX_DOMAIN" "$LOCALBOX_TUNNEL" >/dev/null || true
    fi
fi

ssh_mode=normal
ssh_error=/tmp/localbox_phase11_ssh_error.$$
trap 'rm -f "$ssh_error"' EXIT HUP INT TERM
if ! try_ssh "$SSH_OPTS" 'true' 2>"$ssh_error"; then
    if try_ssh "$LEGACY_SSH_OPTS" 'true' >/dev/null 2>>"$ssh_error"; then
        ssh_mode=legacy
        echo "WARN ssh: reachable only with legacy host-key algorithms"
    else
        echo "FAIL ssh: cannot authenticate to $PI_USER@$PI_HOST after reboot"
        sed 's/^/  /' "$ssh_error" >&2
        echo
        echo "Recovery policy: use console or rewrite the card. Do not publish new Cloudflare routes while SSH is failed."
        exit 20
    fi
fi
if [ "$ssh_mode" = "legacy" ]; then
    SELECTED_SSH_OPTS=$LEGACY_SSH_OPTS
else
    SELECTED_SSH_OPTS=$SSH_OPTS
fi

remote_env="ACTION=$(quote_sh "$ACTION") DEVICE_NAME=$(quote_sh "$DEVICE_NAME") LOCALBOX_DOMAIN=$(quote_sh "$LOCALBOX_DOMAIN") LOCALBOX_BASE=$(quote_sh "$LOCALBOX_BASE") LOCALBOX_BIN_DIR=$(quote_sh "$LOCALBOX_BIN_DIR") EXPECTED_WEB_SERVICE=$(quote_sh "$EXPECTED_WEB_SERVICE") EXPECTED_APP_SERVICE=$(quote_sh "$EXPECTED_APP_SERVICE") EXPECTED_MAIL_SERVICE=$(quote_sh "$EXPECTED_MAIL_SERVICE") EXPECTED_FILES_SERVICE=$(quote_sh "$EXPECTED_FILES_SERVICE") INGEST_PROBE=$(quote_sh "$INGEST_PROBE")"

echo ">> Running remote audit..."
# shellcheck disable=SC2086
ssh $SELECTED_SSH_OPTS "$PI_USER@$PI_HOST" "$remote_env sh -s" <<'REMOTE'
set -eu

failures=0
warnings=0
repairs=0

pass() {
    printf 'PASS %s\n' "$1"
}

warn() {
    warnings=$((warnings + 1))
    printf 'WARN %s\n' "$1"
}

fail() {
    failures=$((failures + 1))
    printf 'FAIL %s\n' "$1"
}

fixed() {
    repairs=$((repairs + 1))
    printf 'FIXED %s\n' "$1"
}

repairing() {
    [ "$ACTION" = "repair" ]
}

service_exists() {
    [ -x "/etc/init.d/$1" ]
}

service_started() {
    rc-service "$1" status >/dev/null 2>&1
}

check_service() {
    service=$1
    required=$2
    if ! service_exists "$service"; then
        if [ "$required" = "required" ]; then
            fail "service:$service missing"
        else
            warn "service:$service not installed"
        fi
        return
    fi
    if service_started "$service"; then
        pass "service:$service started"
        return
    fi
    if repairing; then
        rc-service "$service" restart >/tmp/phase11-"$service".log 2>&1 && fixed "service:$service restarted" && return
    fi
    fail "service:$service not started"
}

check_runlevel() {
    service=$1
    required=$2
    if [ -e "/etc/runlevels/default/$service" ]; then
        pass "runlevel:$service default"
        return
    fi
    if repairing && service_exists "$service"; then
        rc-update add "$service" default >/dev/null 2>&1 || true
        [ -e "/etc/runlevels/default/$service" ] && fixed "runlevel:$service default" && return
    fi
    if [ "$required" = "required" ]; then
        fail "runlevel:$service missing from default"
    else
        warn "runlevel:$service missing from default"
    fi
}

ensure_symlink() {
    name=$1
    target=$LOCALBOX_BIN_DIR/$name
    link=/usr/local/bin/$name
    if [ -L "$link" ] && [ "$(readlink "$link")" = "$target" ] && [ -x "$target" ]; then
        pass "binary:$name symlinked"
        return
    fi
    if repairing && [ -x "$target" ]; then
        ln -sf "$target" "$link"
        fixed "binary:$name symlink repaired"
        return
    fi
    fail "binary:$name missing or not symlinked to $target"
}

check_file() {
    path=$1
    label=$2
    [ -e "$path" ] && pass "$label" || fail "$label missing"
}

check_localbox_user_recovery() {
    domain=$1
    if [ ! -f /etc/localbox/users.conf ]; then
        fail "users.conf missing"
        return
    fi
    missing=$(awk -F: -v domain="$domain" '$2 == domain && ($4 == "" || $4 ~ /^[[:space:]]*$/) { count++ } END { print count + 0 }' /etc/localbox/users.conf)
    if [ "$missing" -eq 0 ]; then
        pass "recovery:$domain all mailboxes have recovery email"
    else
        fail "recovery:$domain $missing mailbox(es) missing recovery email"
    fi
}

check_postfix_mailboxes() {
    domain=$1
    if [ ! -f /etc/postfix/localbox_virtual_mailboxes ]; then
        fail "postfix virtual mailboxes missing"
        return
    fi
    users=$(awk -F: -v domain="$domain" '$2 == domain { print $1 "@" $2 }' /etc/localbox/users.conf 2>/dev/null || true)
    [ -n "$users" ] || {
        warn "postfix:$domain no users to verify"
        return
    }
    for address in $users; do
        if postmap -q "$address" lmdb:/etc/postfix/localbox_virtual_mailboxes >/dev/null 2>&1; then
            pass "postfix:$address mapped"
        else
            fail "postfix:$address not mapped"
        fi
    done
}

check_ingest_secret() {
    if [ "$INGEST_PROBE" != "true" ]; then
        warn "ingest probe skipped"
        return
    fi
    if [ ! -r /etc/localbox/secret ]; then
        fail "ingest secret missing"
        return
    fi
    address=$(awk -F: -v domain="$LOCALBOX_DOMAIN" '$2 == domain { print $1 "@" $2; exit }' /etc/localbox/users.conf 2>/dev/null || true)
    [ -n "$address" ] || {
        fail "ingest probe no mailbox for $LOCALBOX_DOMAIN"
        return
    }
    raw_message=$(printf 'From: phase11@example.invalid\\r\\nTo: %s\\r\\nSubject: phase11 ingest probe\\r\\n\\r\\nprobe\\r\\n' "$address")
    payload=$(printf '{"to":"%s","from":"phase11@example.invalid","raw":"%s"}' "$address" "$raw_message")
    old_code=$(printf '%s' "$payload" | curl -sS -o /tmp/phase11-ingest-old.out -w '%{http_code}' -X POST http://127.0.0.1:8080/ingest -H 'Content-Type: application/json' -H 'X-Ingest-Secret: 220961' --data-binary @- 2>/dev/null || true)
    secret=$(cat /etc/localbox/secret)
    new_code=$(printf '%s' "$payload" | curl -sS -o /tmp/phase11-ingest-new.out -w '%{http_code}' -X POST http://127.0.0.1:8080/ingest -H 'Content-Type: application/json' -H "X-Ingest-Secret: $secret" --data-binary @- 2>/dev/null || true)
    [ "$old_code" = "401" ] && pass "ingest fallback secret rejected" || fail "ingest fallback secret returned $old_code"
    [ "$new_code" = "200" ] && pass "ingest configured secret accepted" || fail "ingest configured secret returned $new_code"
}

echo "Host: $(hostname)"
echo "Date: $(date)"

if mountpoint -q /mnt/data; then
    pass "mount:/mnt/data"
else
    if repairing; then
        mount /mnt/data >/tmp/phase11-mount-data.log 2>&1 || mount -a >/tmp/phase11-mount-a.log 2>&1 || true
        mountpoint -q /mnt/data && fixed "mount:/mnt/data mounted" || fail "mount:/mnt/data not mounted"
    else
        fail "mount:/mnt/data not mounted"
    fi
fi

grep -q '[[:space:]]/mnt/data[[:space:]]' /etc/fstab 2>/dev/null && pass "fstab:/mnt/data" || fail "fstab:/mnt/data missing"
grep -q '^LBU_MEDIA=mmcblk0p1' /etc/lbu/lbu.conf 2>/dev/null && pass "lbu media mmcblk0p1" || fail "lbu media not set to mmcblk0p1"

check_file /etc/hostname "hostname file"
check_file /etc/network/interfaces "network interfaces"
check_file /etc/ssh "ssh config dir"
check_file /root/.ssh/authorized_keys "root authorized_keys"
check_file /etc/localbox/secret "localbox ingest secret"
check_file /etc/localbox/sso-secret "localbox sso secret"
check_file /etc/localbox/users.conf "localbox users"
check_file /etc/localbox/domains.conf "localbox domains"

for service in networking sshd chronyd dovecot postfix nginx mail-ingest contact-service cloudflared; do
    check_runlevel "$service" required
    check_service "$service" required
done
for service in alps filebrowser; do
    check_runlevel "$service" optional
    check_service "$service" optional
done

for binary in mail-ingest contact-service cloudflared; do
    ensure_symlink "$binary"
done
[ -x "$LOCALBOX_BIN_DIR/alps" ] && ensure_symlink alps || warn "binary:alps not installed"
[ -x "$LOCALBOX_BIN_DIR/filebrowser" ] && ensure_symlink filebrowser || warn "binary:filebrowser not installed"

check_localbox_user_recovery "$LOCALBOX_DOMAIN"
if [ -x /usr/local/sbin/localbox-sync-mailmaps ]; then
    if repairing; then
        /usr/local/sbin/localbox-sync-mailmaps >/tmp/phase11-mailmaps.log 2>&1 && fixed "mail maps synced" || fail "mail maps sync failed"
    fi
    if command -v sudo >/dev/null 2>&1; then
        su www -s /bin/sh -c "sudo -n /usr/local/sbin/localbox-sync-mailmaps >/tmp/phase11-sudo-mailmaps.log 2>&1" && pass "sudo:www mailmaps" || fail "sudo:www mailmaps failed"
    else
        fail "sudo missing for web password/mailbox sync"
    fi
else
    fail "localbox-sync-mailmaps helper missing"
fi
check_postfix_mailboxes "$LOCALBOX_DOMAIN"
check_ingest_secret

if repairing; then
    mount -o remount,rw /media/mmcblk0p1 2>/dev/null || true
    lbu add /etc/hostname /etc/network/interfaces /etc/ssh /root/.ssh /etc/fstab /etc/lbu/lbu.conf 2>/dev/null || true
    lbu add /etc/localbox /etc/dovecot /etc/postfix /etc/nginx /etc/init.d /etc/runlevels/default 2>/dev/null || true
    lbu add /usr/local/bin /usr/local/sbin /etc/sudoers.d /etc/apk/world /etc/apk/cache 2>/dev/null || true
    lbu add /var/log/mail-ingest.log /var/log/contact-service.log /var/localbox 2>/dev/null || true
    lbu commit -d >/tmp/phase11-lbu.log 2>&1 && fixed "lbu committed repaired state" || fail "lbu commit failed"
    mount -o remount,ro /media/mmcblk0p1 2>/dev/null || true
fi

echo "SUMMARY failures=$failures warnings=$warnings fixed=$repairs"
[ "$failures" -eq 0 ] || exit 30
REMOTE

if [ "$ssh_mode" = "legacy" ]; then
    echo "FAIL ssh: legacy host-key mode is not an acceptable completed install"
    exit 22
fi

echo
echo "Phase 11 finished."
