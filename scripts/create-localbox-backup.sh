#!/bin/sh
set -eu

LOCALBOX_BASE=${LOCALBOX_BASE:-/mnt/data/localbox}
BACKUP_DIR=${BACKUP_DIR:-$LOCALBOX_BASE/backups}
INCLUDE_MAIL_DATA=${INCLUDE_MAIL_DATA:-0}
INCLUDE_SITE_DATA=${INCLUDE_SITE_DATA:-0}
INCLUDE_FILE_DATA=${INCLUDE_FILE_DATA:-0}
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
copy_if_exists /etc/nginx/http.d/alps-webmail.conf
copy_if_exists /etc/nginx/http.d/localbox-webdav.conf
copy_if_exists /etc/nginx/localbox-webdav.htpasswd
copy_if_exists /etc/nginx/modules
copy_if_exists /etc/dovecot
copy_if_exists /etc/postfix
copy_if_exists /etc/radicale
copy_if_exists /etc/ssh
copy_if_exists /etc/init.d/mail-ingest
copy_if_exists /etc/init.d/contact-service
copy_if_exists /etc/init.d/dovecot
copy_if_exists /etc/init.d/postfix
copy_if_exists /etc/init.d/radicale
copy_if_exists /etc/init.d/alps
copy_if_exists /etc/init.d/filebrowser
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
copy_if_exists /usr/local/bin/filebrowser
copy_if_exists /usr/local/bin/localbox-filebrowser-auth
copy_if_exists /usr/local/sbin/localbox-enable-mail-client-access
copy_if_exists /usr/local/sbin/localbox-enable-tailscale-persistence
copy_if_exists /var/lib/tailscale
copy_if_exists /var/localbox/contact.log
copy_if_exists /var/localbox/send.log
copy_if_exists /mnt/data/alps-test/config.toml
copy_if_exists "$LOCALBOX_BASE/filebrowser"

include_domain_subdirs() {
    name=$1
    label=$2
    found=0

    if [ ! -d "$LOCALBOX_BASE/domains" ]; then
        return 0
    fi

    for domain_dir in "$LOCALBOX_BASE"/domains/*; do
        [ -d "$domain_dir/$name" ] || continue
        found=1
        domain=$(basename "$domain_dir")
        dst="$WORKDIR/rootfs$LOCALBOX_BASE/domains/$domain"
        mkdir -p "$dst"
        cp -a "$domain_dir/$name" "$dst/"
    done

    if [ "$found" = "1" ]; then
        echo ">> Included LocalBox $label data."
    fi
}

if [ "$INCLUDE_MAIL_DATA" = "1" ]; then
    echo ">> Including LocalBox mail data..."
    mkdir -p "$WORKDIR/rootfs$LOCALBOX_BASE"
    cp -a "$LOCALBOX_BASE/domains" "$WORKDIR/rootfs$LOCALBOX_BASE/" 2>/dev/null || true
else
    echo ">> Skipping mail data. Set INCLUDE_MAIL_DATA=1 to include it."
    if [ "$INCLUDE_SITE_DATA" = "1" ]; then
        echo ">> Including LocalBox website/contact-form data..."
        include_domain_subdirs site "website/contact-form"
    else
        echo ">> Skipping website/contact-form data. Set INCLUDE_SITE_DATA=1 to include it without mail."
    fi
fi

if [ "$INCLUDE_FILE_DATA" = "1" ]; then
    echo ">> Including LocalBox user files..."
    mkdir -p "$WORKDIR/rootfs$LOCALBOX_BASE"
    cp -a "$LOCALBOX_BASE/files" "$WORKDIR/rootfs$LOCALBOX_BASE/" 2>/dev/null || true
else
    echo ">> Skipping user files. Set INCLUDE_FILE_DATA=1 to include them."
fi

echo ">> Writing restore notes..."
cat > "$WORKDIR/RESTORE.md" << EOF
# LocalBox Backup

Created: $STAMP
Host: $HOST

This archive contains selected LocalBox configuration and service state.

Data switches used:

- INCLUDE_MAIL_DATA=$INCLUDE_MAIL_DATA
- INCLUDE_SITE_DATA=$INCLUDE_SITE_DATA
- INCLUDE_FILE_DATA=$INCLUDE_FILE_DATA

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
- Website/contact-form files under $LOCALBOX_BASE/domains/*/site when included
- Contact-form messages in maildirs when INCLUDE_MAIL_DATA=1

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
