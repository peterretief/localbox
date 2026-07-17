#!/bin/sh
set -eu

MODE=${1:-}
DOMAIN=${2:-}
PEER=${3:-}
LOCALBOX_BASE=${LOCALBOX_BASE:-/mnt/data/localbox}

usage() {
    cat <<EOF
Usage:
  $0 push DOMAIN PEER
  $0 pull DOMAIN PEER

Examples:
  $0 push localboxmail.co.za pi-b
  $0 pull localboxmail.co.za pi-b

Modes:
  push  Copy local mail for DOMAIN to PEER without deleting remote files.
  pull  Copy PEER mail for DOMAIN back to this box without deleting local files.
EOF
}

if [ -z "$MODE" ] || [ -z "$DOMAIN" ] || [ -z "$PEER" ]; then
    usage >&2
    exit 1
fi

case "$MODE" in
    push|pull) ;;
    *)
        echo "ERROR: mode must be push or pull" >&2
        usage >&2
        exit 1
        ;;
esac

case "$DOMAIN" in
    *[!a-zA-Z0-9._-]*|"")
        echo "ERROR: invalid domain: $DOMAIN" >&2
        exit 1
        ;;
esac

local_mail="$LOCALBOX_BASE/domains/$DOMAIN/mail/"
remote_mail="$PEER:$LOCALBOX_BASE/domains/$DOMAIN/mail/"

if [ "$MODE" = "push" ]; then
    if [ ! -d "$local_mail" ]; then
        echo "ERROR: local mail path not found: $local_mail" >&2
        exit 1
    fi
    exec rsync -aH --numeric-ids --ignore-existing "$local_mail" "$remote_mail"
fi

mkdir -p "$local_mail"
exec rsync -aH --numeric-ids --ignore-existing "$remote_mail" "$local_mail"
