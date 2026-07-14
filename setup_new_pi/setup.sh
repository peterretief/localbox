#!/bin/sh
set -eu

SETUP_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SETUP_DIR")

usage() {
    cat <<EOF
Usage: ./setup.sh COMMAND [args]

Commands:
  inventory                         Show attached disks and mounts.
  prepare-boot                      Patch mounted Alpine boot card.
  save-image                        Save known-good SD card image.
  write-image IMAGE DEVICE           Write saved image to a whole SD device.
  format-usb DEVICE                 Format a whole USB device as LocalBox data disk.
  cloudflare [DOMAIN_CONF]          Create/update Cloudflare Worker and Email Routing.
  deploy-domain [DOMAIN_CONF]       Provision domain on reachable Pi.

Common environment:
  BOOT_MOUNT=/mnt/piboot
  FIRSTBOOT_CONFIG=$SETUP_DIR/localbox-firstboot.conf
  CLOUDFLARE_CONFIG=$PROJECT_DIR/cloudflare.conf
  WORKER_DEPLOY_METHOD=wrangler
  PI_HOST=192.168.0.6

Examples:
  ./setup.sh inventory
  BOOT_MOUNT=/mnt/piboot FIRSTBOOT_CONFIG=./localbox-firstboot.conf ./setup.sh prepare-boot
  WORKER_DEPLOY_METHOD=wrangler ./setup.sh cloudflare ./domain.conf
  PI_HOST=192.168.0.6 ./setup.sh deploy-domain ./domain.conf

Cloudflare domain config can include:
  email_worker=alpine-n
  ingest_host=n.diode.co.za
  ingest_tunnel_id=your-cloudflare-tunnel-uuid
EOF
}

command=${1:-}
[ -n "$command" ] || {
    usage
    exit 1
}
shift || true

case "$command" in
    inventory)
        exec "$PROJECT_DIR/scripts/device-inventory.sh" "$@"
        ;;
    prepare-boot)
        BOOT_MOUNT=${BOOT_MOUNT:-/mnt/piboot}
        FIRSTBOOT_CONFIG=${FIRSTBOOT_CONFIG:-"$SETUP_DIR/localbox-firstboot.conf"}
        export BOOT_MOUNT FIRSTBOOT_CONFIG
        exec "$PROJECT_DIR/scripts/prepare-pi-boot-card.sh" "$@"
        ;;
    save-image)
        BOOT_MOUNT=${BOOT_MOUNT:-/mnt/piboot}
        OUT_DIR=${OUT_DIR:-"$PROJECT_DIR/images"}
        export BOOT_MOUNT OUT_DIR
        exec "$PROJECT_DIR/scripts/save-alpine-os-image.sh" "$@"
        ;;
    write-image)
        exec "$PROJECT_DIR/scripts/write-alpine-os-image.sh" "$@"
        ;;
    format-usb)
        exec "$PROJECT_DIR/scripts/format-localbox-data-usb.sh" "$@"
        ;;
    cloudflare)
        config=${1:-"$SETUP_DIR/domain.conf"}
        exec "$PROJECT_DIR/scripts/setup-cloudflare-email-worker.sh" "$config"
        ;;
    deploy-domain)
        config=${1:-"$SETUP_DIR/domain.conf"}
        exec "$PROJECT_DIR/scripts/deploy-domain-from-config.sh" "$config"
        ;;
    help|-h|--help)
        usage
        ;;
    *)
        echo "ERROR: unknown command: $command" >&2
        usage >&2
        exit 1
        ;;
esac
