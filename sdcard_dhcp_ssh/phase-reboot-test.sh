#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PI_HOST=${PI_HOST:-192.168.0.7}
PI_USER=${PI_USER:-root}
DEVICE_NAME=${DEVICE_NAME:-localbox-001}
LOCALBOX_DOMAIN=${LOCALBOX_DOMAIN:-localboxtest.co.za}
SSH_KNOWN_HOSTS=${SSH_KNOWN_HOSTS:-/tmp/localbox_phase_reboot_known_hosts}
SSH_OPTS=${SSH_OPTS:-"-o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=$SSH_KNOWN_HOSTS -o ConnectTimeout=5"}
REBOOT_WAIT_SECONDS=${REBOOT_WAIT_SECONDS:-360}
SSH_POLL_INTERVAL=${SSH_POLL_INTERVAL:-5}
SKIP_REBOOT=${SKIP_REBOOT:-false}
PHASE11_ACTION=${PHASE11_ACTION:-audit}
PHASE_TIMEOUT_SECONDS=${PHASE_TIMEOUT_SECONDS:-900}

usage() {
    cat <<EOF
Usage:
  PI_HOST=192.168.0.7 DEVICE_NAME=localbox-001 LOCALBOX_DOMAIN=localboxtest.co.za $0 phase2 phase3 phase4:format phase5 ...

Runs each selected Pi phase, reboots the Pi, waits for SSH, then runs a phase-specific persistence check.

Supported phase names:
  phase2
  phase3
  phase4:repair
  phase4:format       requires DATA_DEVICE and CONFIRM_DATA_DEVICE
  phase5
  phase6
  phase7
  phase8
  phase9a
  phase9b
  phase9c
  phase9d
  phase9e
  phase9g
  phase10
  phase11

Cloudflare route publishing is intentionally not included because phase9-cloudflare-route.sh
runs on the development machine and does not change the Pi.
EOF
}

case "${1:-}" in
    -h|--help|help)
        usage
        exit 0
        ;;
    "")
        usage
        exit 1
        ;;
esac

quote_sh() {
    printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}

ssh_pi() {
    # shellcheck disable=SC2086
    ssh $SSH_OPTS "$PI_USER@$PI_HOST" "$@"
}

wait_for_ssh() {
    deadline=$(( $(date +%s) + REBOOT_WAIT_SECONDS ))
    echo ">> Waiting for SSH on $PI_USER@$PI_HOST..."
    while [ "$(date +%s)" -lt "$deadline" ]; do
        if ssh_pi 'true' >/dev/null 2>&1; then
            echo "   SSH is back."
            return 0
        fi
        sleep "$SSH_POLL_INTERVAL"
    done
    echo "ERROR: SSH did not return within ${REBOOT_WAIT_SECONDS}s" >&2
    return 1
}

reboot_pi() {
    if [ "$SKIP_REBOOT" = "true" ]; then
        echo ">> SKIP_REBOOT=true, not rebooting."
        return 0
    fi
    echo ">> Rebooting $PI_USER@$PI_HOST..."
    ssh_pi 'sync; reboot' >/dev/null 2>&1 || true
    sleep 10
    wait_for_ssh
}

run_with_timeout() {
    if command -v timeout >/dev/null 2>&1; then
        timeout "$PHASE_TIMEOUT_SECONDS" "$@"
    else
        "$@"
    fi
}

run_remote_check() {
    phase=$1
    echo ">> Running post-reboot check for $phase..."
    ssh_pi "CHECK_PHASE=$(quote_sh "$phase") LOCALBOX_DOMAIN=$(quote_sh "$LOCALBOX_DOMAIN") sh -s" <<'REMOTE'
set -eu

failures=0

pass() {
    printf 'PASS %s\n' "$1"
}

fail() {
    failures=$((failures + 1))
    printf 'FAIL %s\n' "$1"
}

have_file() {
    [ -e "$1" ] && pass "$2" || fail "$2 missing"
}

service_started() {
    rc-service "$1" status >/dev/null 2>&1
}

runlevel_default() {
    [ -e "/etc/runlevels/default/$1" ]
}

check_service() {
    service=$1
    runlevel_default "$service" && pass "runlevel:$service" || fail "runlevel:$service missing"
    service_started "$service" && pass "service:$service started" || fail "service:$service not started"
}

check_lbu() {
    grep -q '^LBU_MEDIA=mmcblk0p1' /etc/lbu/lbu.conf 2>/dev/null && pass "lbu media" || fail "lbu media missing"
}

check_data_mount() {
    mountpoint -q /mnt/data && pass "mount:/mnt/data" || fail "mount:/mnt/data not mounted"
    grep -q '[[:space:]]/mnt/data[[:space:]]' /etc/fstab 2>/dev/null && pass "fstab:/mnt/data" || fail "fstab:/mnt/data missing"
}

echo "Host: $(hostname)"
echo "Date: $(date)"

case "$CHECK_PHASE" in
    phase2)
        check_lbu
        have_file /root/.ssh/authorized_keys "root authorized_keys"
        check_service sshd
        have_file /etc/network/interfaces "network interfaces"
        ;;
    phase3)
        check_lbu
        check_service sshd
        check_service chronyd
        apk update -q >/dev/null && pass "apk update" || fail "apk update failed"
        ;;
    phase4:repair|phase4:format)
        check_lbu
        check_service sshd
        check_data_mount
        ;;
    phase5)
        check_lbu
        check_data_mount
        have_file /etc/localbox "localbox config dir"
        have_file /mnt/data/localbox "localbox data dir"
        have_file /etc/runlevels/default/nginx "runlevel:nginx"
        ;;
    phase6)
        check_lbu
        check_data_mount
        have_file /usr/local/bin/mail-ingest "mail-ingest binary"
        have_file /usr/local/bin/contact-service "contact-service binary"
        have_file /etc/init.d/mail-ingest "mail-ingest service"
        have_file /etc/init.d/contact-service "contact-service service"
        command -v sudo >/dev/null 2>&1 && pass "sudo installed" || fail "sudo missing"
        have_file /etc/sudoers.d/localbox-mailmaps "sudoers mailmaps"
        have_file /usr/local/sbin/localbox-sync-mailmaps "mailmaps helper"
        ;;
    phase7)
        check_lbu
        check_data_mount
        check_service dovecot
        check_service postfix
        have_file /etc/postfix/localbox_virtual_domains "postfix virtual domains"
        have_file /etc/dovecot/localbox/users "dovecot users"
        if command -v sudo >/dev/null 2>&1 && [ -x /usr/local/sbin/localbox-sync-mailmaps ]; then
            su www -s /bin/sh -c "sudo -n /usr/local/sbin/localbox-sync-mailmaps >/tmp/phase-reboot-mailmaps.log 2>&1" && pass "sudo:www mailmaps" || fail "sudo:www mailmaps failed"
        else
            fail "sudo mailmaps prerequisites missing"
        fi
        ;;
    phase8)
        check_lbu
        check_data_mount
        check_service dovecot
        check_service postfix
        check_service nginx
        check_service mail-ingest
        check_service contact-service
        have_file /etc/localbox/secret "localbox ingest secret"
        have_file /etc/localbox/sso-secret "localbox sso secret"
        grep -qxF "$LOCALBOX_DOMAIN" /etc/localbox/domains.conf 2>/dev/null && pass "domain:$LOCALBOX_DOMAIN" || fail "domain:$LOCALBOX_DOMAIN missing"
        awk -F: -v domain="$LOCALBOX_DOMAIN" '$2 == domain && $4 != "" { found=1 } END { exit(found ? 0 : 1) }' /etc/localbox/users.conf 2>/dev/null && pass "domain users have recovery" || fail "domain users missing recovery"
        ;;
    phase9a)
        check_lbu
        check_data_mount
        check_service cloudflared
        if ! service_started cloudflared; then
            tail -n 40 /var/log/cloudflared/cloudflared.log 2>/dev/null || true
        fi
        command -v cloudflared >/dev/null 2>&1 && pass "cloudflared binary" || fail "cloudflared binary missing"
        ;;
    phase9b)
        check_lbu
        check_data_mount
        check_service nginx
        grep -q 'LocalBox managed default site' /etc/nginx/http.d/default.conf 2>/dev/null && pass "nginx localbox default config" || fail "nginx localbox default config missing"
        if curl -I -sS -H "Host: $LOCALBOX_DOMAIN" http://127.0.0.1/ | sed -n '1p' | grep -Eq ' 200 | 301 | 302 '; then
            pass "nginx:$LOCALBOX_DOMAIN local web"
        else
            fail "nginx:$LOCALBOX_DOMAIN local web failed"
        fi
        ;;
    phase9c)
        check_lbu
        check_data_mount
        check_service filebrowser
        ;;
    phase9d)
        check_lbu
        check_data_mount
        check_service alps
        ;;
    phase9e)
        check_lbu
        check_data_mount
        check_service mail-ingest
        ;;
    phase9g)
        check_lbu
        check_data_mount
        have_file /etc/localbox/smtp.conf "smtp config"
        have_file /etc/postfix/sasl_passwd.lmdb "postfix smtp credentials map"
        postconf -h relayhost 2>/dev/null | grep -q . && pass "postfix relayhost" || fail "postfix relayhost missing"
        check_service postfix
        check_service mail-ingest
        check_service contact-service
        ;;
    phase10)
        check_lbu
        check_service tailscale
        tailscale_ready=0
        n=0
        while [ "$n" -lt 12 ]; do
            if tailscale status --json >/tmp/phase-reboot-tailscale-status.json 2>/tmp/phase-reboot-tailscale-status.err &&
                grep -Eq '"BackendState"[[:space:]]*:[[:space:]]*"Running"' /tmp/phase-reboot-tailscale-status.json &&
                grep -Eq '"100\.[0-9]+\.[0-9]+\.[0-9]+"' /tmp/phase-reboot-tailscale-status.json; then
                tailscale_ready=1
                break
            fi
            sleep 5
            n=$((n + 1))
        done
        if [ "$tailscale_ready" -eq 1 ]; then
            pass "tailscale backend running"
            pass "tailscale IP assigned"
        else
            tailscale status --json >/tmp/phase-reboot-tailscale-status.json 2>/tmp/phase-reboot-tailscale-status.err || true
            fail "tailscale status failed"
            cat /tmp/phase-reboot-tailscale-status.err 2>/dev/null || true
            sed -n '1,40p' /tmp/phase-reboot-tailscale-status.json 2>/dev/null || true
        fi
        ;;
    *)
        fail "unknown check phase $CHECK_PHASE"
        ;;
esac

echo "SUMMARY failures=$failures"
[ "$failures" -eq 0 ]
REMOTE
}

run_phase() {
    phase=$1
    skip_reboot_check=0
    echo
    echo "== $phase =="
    set +e
    case "$phase" in
        phase2)
            run_with_timeout env PI_HOST="$PI_HOST" "$SCRIPT_DIR/phase2-persist-ssh.sh"
            ;;
        phase3)
            run_with_timeout env PI_HOST="$PI_HOST" "$SCRIPT_DIR/phase3-base-system.sh"
            ;;
        phase4:repair)
            run_with_timeout env PI_HOST="$PI_HOST" "$SCRIPT_DIR/phase4-data-usb.sh" repair
            ;;
        phase4:format)
            : "${DATA_DEVICE:?DATA_DEVICE is required for phase4:format}"
            : "${CONFIRM_DATA_DEVICE:?CONFIRM_DATA_DEVICE is required for phase4:format}"
            run_with_timeout env PI_HOST="$PI_HOST" DATA_DEVICE="$DATA_DEVICE" CONFIRM_DATA_DEVICE="$CONFIRM_DATA_DEVICE" "$SCRIPT_DIR/phase4-data-usb.sh" format
            ;;
        phase5)
            run_with_timeout env PI_HOST="$PI_HOST" "$SCRIPT_DIR/phase5-localbox-base.sh"
            ;;
        phase6)
            run_with_timeout env PI_HOST="$PI_HOST" "$SCRIPT_DIR/phase6-install-binaries.sh"
            ;;
        phase7)
            run_with_timeout env PI_HOST="$PI_HOST" "$SCRIPT_DIR/phase7-mail-stack.sh"
            ;;
        phase8)
            run_with_timeout env PI_HOST="$PI_HOST" DEVICE_NAME="$DEVICE_NAME" LOCALBOX_DOMAIN="$LOCALBOX_DOMAIN" "$SCRIPT_DIR/phase8-first-domain.sh"
            ;;
        phase9a)
            run_with_timeout env PI_HOST="$PI_HOST" DEVICE_NAME="$DEVICE_NAME" LOCALBOX_DOMAIN="$LOCALBOX_DOMAIN" "$SCRIPT_DIR/phase9a-create-device-tunnel.sh"
            ;;
        phase9b)
            run_with_timeout env PI_HOST="$PI_HOST" DEVICE_NAME="$DEVICE_NAME" LOCALBOX_DOMAIN="$LOCALBOX_DOMAIN" "$SCRIPT_DIR/phase9b-web-routing.sh"
            ;;
        phase9c)
            run_with_timeout env PI_HOST="$PI_HOST" DEVICE_NAME="$DEVICE_NAME" LOCALBOX_DOMAIN="$LOCALBOX_DOMAIN" "$SCRIPT_DIR/phase9c-files.sh"
            ;;
        phase9d)
            run_with_timeout env PI_HOST="$PI_HOST" DEVICE_NAME="$DEVICE_NAME" LOCALBOX_DOMAIN="$LOCALBOX_DOMAIN" "$SCRIPT_DIR/phase9d-alps-webmail.sh"
            ;;
        phase9e)
            run_with_timeout env PI_HOST="$PI_HOST" DEVICE_NAME="$DEVICE_NAME" LOCALBOX_DOMAIN="$LOCALBOX_DOMAIN" "$SCRIPT_DIR/phase9e-email-worker.sh"
            ;;
        phase9g)
            run_with_timeout env PI_HOST="$PI_HOST" DEVICE_NAME="$DEVICE_NAME" LOCALBOX_DOMAIN="$LOCALBOX_DOMAIN" RUN_SMTP_TEST="${RUN_SMTP_TEST:-auto}" "$SCRIPT_DIR/phase9g-outbound-smtp.sh"
            ;;
        phase10)
            run_with_timeout env \
                PI_HOST="$PI_HOST" \
                DEVICE_NAME="$DEVICE_NAME" \
                TAILSCALE_HOSTNAME="${TAILSCALE_HOSTNAME:-}" \
                TAILSCALE_ADVERTISE_ROUTES="${TAILSCALE_ADVERTISE_ROUTES:-}" \
                TAILSCALE_ACCEPT_ROUTES="${TAILSCALE_ACCEPT_ROUTES:-}" \
                TAILSCALE_AUTHKEY="${TAILSCALE_AUTHKEY:-}" \
                HEADSCALE_CREATE_AUTHKEY="${HEADSCALE_CREATE_AUTHKEY:-}" \
                HEADSCALE_APPROVE_ROUTES="${HEADSCALE_APPROVE_ROUTES:-}" \
                HEADSCALE_USER_ID="${HEADSCALE_USER_ID:-}" \
                HEADSCALE_LOGIN_SERVER="${HEADSCALE_LOGIN_SERVER:-}" \
                HEADSCALE_PREAUTHKEY_METHOD="${HEADSCALE_PREAUTHKEY_METHOD:-}" \
                HEADSCALE_SSH_HOST="${HEADSCALE_SSH_HOST:-}" \
                "$SCRIPT_DIR/phase10-headscale-routes.sh"
            ;;
        phase11)
            run_with_timeout env PI_HOST="$PI_HOST" DEVICE_NAME="$DEVICE_NAME" LOCALBOX_DOMAIN="$LOCALBOX_DOMAIN" "$SCRIPT_DIR/phase11-reboot-audit.sh" "$PHASE11_ACTION"
            skip_reboot_check=1
            ;;
        *)
            echo "ERROR: unsupported phase: $phase" >&2
            usage >&2
            exit 1
            ;;
    esac
    phase_status=$?
    set -e

    if [ "$phase_status" -ne 0 ]; then
        echo "FAIL phase:$phase command exited with status $phase_status"
        echo "Phase reboot test stopped before reboot for $phase."
        exit "$phase_status"
    fi

    if [ "$skip_reboot_check" = "1" ]; then
        return 0
    fi

    reboot_pi
    run_remote_check "$phase"
}

echo "Phase reboot test target: $PI_USER@$PI_HOST"
echo "Device: $DEVICE_NAME"
echo "Domain: $LOCALBOX_DOMAIN"

for phase in "$@"; do
    run_phase "$phase"
done

echo
echo "Phase reboot test finished."
