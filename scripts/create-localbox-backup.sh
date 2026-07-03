#!/bin/sh
set -eu

LOCALBOX_BASE=${LOCALBOX_BASE:-/mnt/data/localbox}
BACKUP_DIR=${BACKUP_DIR:-$LOCALBOX_BASE/backups}
INCLUDE_MAIL_DATA=${INCLUDE_MAIL_DATA:-0}
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
HOST=$(hostname 2>/dev/null || echo localbox)
WORKDIR=$(mktemp -d)
OUT="$BACKUP_DIR/localbox-$HOST-$STAMP.tar.gz"

cleanup() {
    rm -rf "$WORKDIR"
}
trap cleanup EXIT

mkdir -p "$BACKUP_DIR"

echo ">> Committing current Alpine lbu state..."
lbu commit -d

echo ">> Collecting system state..."
mkdir -p "$WORKDIR/rootfs"

copy_if_exists() {
    src=$1
    dst="$WORKDIR/rootfs$src"
    if [ -e "$src" ]; then
        mkdir -p "$(dirname "$dst")"
        cp -a "$src" "$dst"
    fi
}

copy_if_exists /etc/localbox
copy_if_exists /etc/nginx/http.d/localbox-sites.conf
copy_if_exists /etc/dovecot
copy_if_exists /etc/postfix
copy_if_exists /etc/ssh
copy_if_exists /etc/init.d/mail-ingest
copy_if_exists /etc/init.d/contact-service
copy_if_exists /etc/init.d/dovecot
copy_if_exists /etc/init.d/postfix
copy_if_exists /etc/init.d/sshd
copy_if_exists /etc/init.d/tailscale
copy_if_exists /etc/init.d/tailscaled
copy_if_exists /etc/runlevels/default
copy_if_exists /etc/apk/world
copy_if_exists /etc/apk/repositories
copy_if_exists /etc/passwd
copy_if_exists /etc/shadow
copy_if_exists /etc/group
copy_if_exists /etc/sudoers
copy_if_exists /usr/local/bin/mail-ingest
copy_if_exists /usr/local/bin/contact-service
copy_if_exists /usr/local/sbin/localbox-enable-mail-client-access
copy_if_exists /usr/local/sbin/localbox-enable-tailscale-persistence
copy_if_exists /var/lib/tailscale
copy_if_exists /var/localbox/contact.log
copy_if_exists /var/localbox/send.log

if [ "$INCLUDE_MAIL_DATA" = "1" ]; then
    echo ">> Including LocalBox mail/site data..."
    mkdir -p "$WORKDIR/rootfs$LOCALBOX_BASE"
    cp -a "$LOCALBOX_BASE/domains" "$WORKDIR/rootfs$LOCALBOX_BASE/" 2>/dev/null || true
else
    echo ">> Skipping mail/site data. Set INCLUDE_MAIL_DATA=1 to include it."
fi

echo ">> Writing restore notes..."
cat > "$WORKDIR/RESTORE.md" << EOF
# LocalBox Backup

Created: $STAMP
Host: $HOST

This archive contains selected LocalBox configuration and service state.

Restore outline on a fresh Alpine Pi:

1. Install base packages used by LocalBox.
2. Extract this archive at /.
3. Run \`lbu add\` for restored config paths.
4. Run \`lbu commit -d\`.
5. Restart services or reboot.

Sensitive contents may include:

- /etc/shadow
- /etc/localbox/smtp.conf
- /etc/postfix/sasl_passwd*
- /var/lib/tailscale
- SSH host keys in /etc/ssh

Treat this backup as secret.
EOF

echo ">> Creating backup archive..."
tar -C "$WORKDIR" -czf "$OUT" .
chmod 600 "$OUT"

echo ""
echo "Backup created:"
echo "$OUT"
echo ""
echo "Copy it off the Pi, for example:"
echo "scp root@$HOST:$OUT ."
