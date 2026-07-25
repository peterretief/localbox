#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
INVENTORY=${INVENTORY:-$SCRIPT_DIR/localbox-inventory.sh}

BOOT_DEVICE=${BOOT_DEVICE:-${1:-}}
TARBALL=${TARBALL:-$PROJECT_DIR/setup_new_pi/alpine-rpi-3.24.1-armv7.tar.gz}
DEVICE_PREFIX=${DEVICE_PREFIX:-localbox}
ASSET_PREFIX=${ASSET_PREFIX:-LBX}
LOCALBOX_DOMAIN=${LOCALBOX_DOMAIN:-}
LOCALBOX_ADMIN_RECOVERY_EMAIL=${LOCALBOX_ADMIN_RECOVERY_EMAIL:-}
DATA_DEVICE=${DATA_DEVICE:-/dev/sda}
CONFIRM_DATA_DEVICE=${CONFIRM_DATA_DEVICE:-$DATA_DEVICE}
INITIAL_PI_HOST=${INITIAL_PI_HOST:-192.168.0.250}
PI_USER=${PI_USER:-root}
AUTHORIZED_KEYS=${AUTHORIZED_KEYS:-$HOME/.ssh/id_ed25519.pub}
INSTALL_PHASES=${INSTALL_PHASES:-phase2 phase3 phase4:format phase5 phase6 phase7 phase8 phase9a phase9b phase9c phase9d phase9e}
PUBLISH_ROUTES=${PUBLISH_ROUTES:-true}
BREVO_DOMAIN=${BREVO_DOMAIN:-true}
RUN_FINAL_AUDIT=${RUN_FINAL_AUDIT:-true}
POWER_TEST=${POWER_TEST:-false}

usage() {
    cat <<EOF
Usage:
  LOCALBOX_DOMAIN=example.co.za LOCALBOX_ADMIN_RECOVERY_EMAIL=you@example.com \\
  CONFIRM_BOOT_DEVICE=/dev/sdX CONFIRM_BOOT_SERIAL=SERIAL \\
  $0 /dev/sdX

Writes a fresh Alpine SD card, allocates the next LocalBox identity, waits for
the Pi on the rescue IP, runs the install phases, publishes standard
Cloudflare routes, configures Brevo domain DNS, and runs the final audit.

Required:
  BOOT_DEVICE or first argument
  CONFIRM_BOOT_DEVICE and CONFIRM_BOOT_SERIAL
  LOCALBOX_DOMAIN
  LOCALBOX_ADMIN_RECOVERY_EMAIL

Useful overrides:
  DEVICE_NAME=localbox-002
  DATA_DEVICE=/dev/sda CONFIRM_DATA_DEVICE=/dev/sda
  INITIAL_PI_HOST=192.168.0.250
  PUBLISH_ROUTES=false
  BREVO_DOMAIN=false
  INSTALL_PHASES="phase2 phase3 ..."
EOF
}

die() {
    echo "ERROR: $*" >&2
    exit 1
}

case "${1:-}" in
    -h|--help|help)
        usage
        exit 0
        ;;
esac

[ -n "$BOOT_DEVICE" ] || die "BOOT_DEVICE or first argument is required"
[ -n "$LOCALBOX_DOMAIN" ] || die "LOCALBOX_DOMAIN is required"
[ -n "$LOCALBOX_ADMIN_RECOVERY_EMAIL" ] || die "LOCALBOX_ADMIN_RECOVERY_EMAIL is required"
[ -f "$TARBALL" ] || die "Alpine tarball not found: $TARBALL"
[ -x "$INVENTORY" ] || die "inventory tool not found: $INVENTORY"

if [ "${CONFIRM_BOOT_DEVICE:-}" != "$BOOT_DEVICE" ]; then
    die "CONFIRM_BOOT_DEVICE must equal BOOT_DEVICE: $BOOT_DEVICE"
fi
[ -n "${CONFIRM_BOOT_SERIAL:-}" ] || die "CONFIRM_BOOT_SERIAL is required"

if [ -z "${DEVICE_NAME:-}" ]; then
    DEVICE_NAME=$("$INVENTORY" next-name device "$DEVICE_PREFIX")
fi

suffix=$(expr "$DEVICE_NAME" : '.*-\([0-9][0-9]*\)$' 2>/dev/null || true)
[ -n "$suffix" ] || die "DEVICE_NAME must end in a numeric suffix, e.g. localbox-002"
ASSET_TAG=${ASSET_TAG:-$(printf '%s-%04d' "$ASSET_PREFIX" "$suffix")}
LOCALBOX_TUNNEL=${LOCALBOX_TUNNEL:-$DEVICE_NAME-tunnel}
LOCALBOX_EMAIL_WORKER=${LOCALBOX_EMAIL_WORKER:-$DEVICE_NAME-mail-worker}
TAILSCALE_NAME=${TAILSCALE_NAME:-$DEVICE_NAME}

echo "LocalBox next install"
echo "Device:       $DEVICE_NAME"
echo "Asset tag:    $ASSET_TAG"
echo "Domain:       $LOCALBOX_DOMAIN"
echo "Tunnel:       $LOCALBOX_TUNNEL"
echo "Worker:       $LOCALBOX_EMAIL_WORKER"
echo "SD card:      $BOOT_DEVICE"
echo "Data device:  $DATA_DEVICE"

"$INVENTORY" upsert-device "$DEVICE_NAME" \
    asset_tag="$ASSET_TAG" \
    expected_tunnel="$LOCALBOX_TUNNEL" \
    expected_worker="$LOCALBOX_EMAIL_WORKER" \
    tailscale_name="$TAILSCALE_NAME" \
    status=installing \
    notes=localbox-install-next
"$SCRIPT_DIR/localbox-label.sh" "$DEVICE_NAME" >/dev/null
"$INVENTORY" upsert-domain "$LOCALBOX_DOMAIN" \
    device_name="$DEVICE_NAME" \
    admin_email="admin@$LOCALBOX_DOMAIN" \
    status=planned

echo
echo "== write sdcard =="
AUTHORIZED_KEYS="$AUTHORIZED_KEYS" \
CONFIRM_BOOT_DEVICE="$CONFIRM_BOOT_DEVICE" \
CONFIRM_BOOT_SERIAL="$CONFIRM_BOOT_SERIAL" \
"$SCRIPT_DIR/write-sdcard.sh" "$TARBALL" "$BOOT_DEVICE"

cat <<EOF

Move the SD card to the Raspberry Pi now, connect Ethernet and the USB data
stick, then press Enter. The script will wait for SSH on $INITIAL_PI_HOST.
EOF
read -r _

wait_for_ssh() {
    host=$1
    deadline=$(( $(date +%s) + ${BOOT_WAIT_SECONDS:-420} ))
    echo ">> Waiting for SSH on root@$host..."
    while [ "$(date +%s)" -lt "$deadline" ]; do
        if ssh -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/localbox_install_next_known_hosts -o ConnectTimeout=5 "$PI_USER@$host" 'true' >/dev/null 2>&1; then
            echo "   SSH is up."
            return 0
        fi
        sleep 5
    done
    return 1
}

wait_for_ssh "$INITIAL_PI_HOST" || die "SSH did not come up on $INITIAL_PI_HOST"

echo
echo "== pi phases =="
PI_HOST="$INITIAL_PI_HOST" \
DEVICE_NAME="$DEVICE_NAME" \
LOCALBOX_DOMAIN="$LOCALBOX_DOMAIN" \
LOCALBOX_ADMIN_RECOVERY_EMAIL="$LOCALBOX_ADMIN_RECOVERY_EMAIL" \
DATA_DEVICE="$DATA_DEVICE" \
CONFIRM_DATA_DEVICE="$CONFIRM_DATA_DEVICE" \
LOCALBOX_EMAIL_WORKER="$LOCALBOX_EMAIL_WORKER" \
"$SCRIPT_DIR/phase-reboot-test.sh" $INSTALL_PHASES

PI_HOST=$(ssh -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/localbox_install_next_known_hosts -o ConnectTimeout=10 "$PI_USER@$INITIAL_PI_HOST" \
    "ip -4 -o addr show scope global | awk '{split(\$4,a,\"/\"); if (a[1] != \"192.168.0.250\") {print a[1]; exit}}'" || true)
PI_HOST=${PI_HOST:-$INITIAL_PI_HOST}
"$INVENTORY" upsert-device "$DEVICE_NAME" pi_host="$PI_HOST" hostname=localbox-pi status=active notes=installed

if [ "$PUBLISH_ROUTES" = "true" ] || [ "$PUBLISH_ROUTES" = "1" ] || [ "$PUBLISH_ROUTES" = "yes" ]; then
    echo
    echo "== cloudflare routes =="
    for host in "$LOCALBOX_DOMAIN" "www.$LOCALBOX_DOMAIN" "mail.$LOCALBOX_DOMAIN" "files.$LOCALBOX_DOMAIN"; do
        DEVICE_NAME="$DEVICE_NAME" \
        LOCALBOX_DOMAIN="$LOCALBOX_DOMAIN" \
        LOCALBOX_PUBLIC_HOST="$host" \
        LOCALBOX_TUNNEL="$LOCALBOX_TUNNEL" \
        "$SCRIPT_DIR/phase9-cloudflare-route.sh"
    done
fi

if [ "$BREVO_DOMAIN" = "true" ] || [ "$BREVO_DOMAIN" = "1" ] || [ "$BREVO_DOMAIN" = "yes" ]; then
    echo
    echo "== brevo domain =="
    DEVICE_NAME="$DEVICE_NAME" \
    LOCALBOX_DOMAIN="$LOCALBOX_DOMAIN" \
    "$SCRIPT_DIR/phase9f-brevo-domain.sh"
fi

if [ -f "$PROJECT_DIR/smtp.conf" ]; then
    echo
    echo "== outbound smtp =="
    PI_HOST="$PI_HOST" \
    DEVICE_NAME="$DEVICE_NAME" \
    LOCALBOX_DOMAIN="$LOCALBOX_DOMAIN" \
    "$SCRIPT_DIR/phase-reboot-test.sh" phase9g
else
    echo
    echo "== outbound smtp skipped =="
    echo "No $PROJECT_DIR/smtp.conf found. Copy smtp.conf.example to smtp.conf to enable outbound relay."
fi

if [ "$RUN_FINAL_AUDIT" = "true" ] || [ "$RUN_FINAL_AUDIT" = "1" ] || [ "$RUN_FINAL_AUDIT" = "yes" ]; then
    echo
    echo "== final audit =="
    PI_HOST="$PI_HOST" \
    DEVICE_NAME="$DEVICE_NAME" \
    LOCALBOX_DOMAIN="$LOCALBOX_DOMAIN" \
    LOCALBOX_TUNNEL="$LOCALBOX_TUNNEL" \
    INGEST_PROBE=true \
    "$SCRIPT_DIR/phase11-reboot-audit.sh"
fi

echo
echo "Install complete."
echo "Device:  $DEVICE_NAME"
echo "Asset:   $ASSET_TAG"
echo "Pi host: $PI_HOST"
echo "Domain:  $LOCALBOX_DOMAIN"
echo "Admin:   admin@$LOCALBOX_DOMAIN"
echo
echo "Show record:"
echo "  ./localbox-inventory.sh show-device $DEVICE_NAME"
