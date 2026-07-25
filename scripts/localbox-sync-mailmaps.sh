#!/bin/sh
set -eu

LOCALBOX_BASE=${LOCALBOX_BASE:-/mnt/data/localbox}
LOCALBOX_ETC=${LOCALBOX_ETC:-/etc/localbox}
USERS_FILE=${USERS_FILE:-$LOCALBOX_ETC/users.conf}
DOVECOT_USERS_FILE=${DOVECOT_USERS_FILE:-/etc/dovecot/localbox/users}
POSTFIX_DOMAINS=${POSTFIX_DOMAINS:-/etc/postfix/localbox_virtual_domains}
POSTFIX_MAILBOXES=${POSTFIX_MAILBOXES:-/etc/postfix/localbox_virtual_mailboxes}

[ "$(id -u)" -eq 0 ] || {
    echo "ERROR: localbox-sync-mailmaps must run as root" >&2
    exit 1
}
[ -f "$USERS_FILE" ] || {
    echo "ERROR: users file not found: $USERS_FILE" >&2
    exit 1
}
[ -d /etc/postfix ] || {
    echo "ERROR: Postfix is not installed" >&2
    exit 1
}

tmp_domains=$(mktemp)
tmp_mailboxes=$(mktemp)
tmp_dovecot=$(mktemp)
trap 'rm -f "$tmp_domains" "$tmp_mailboxes" "$tmp_dovecot"' EXIT

mkdir -p /etc/dovecot/localbox /etc/postfix

awk -F: -v mailboxes="$tmp_mailboxes" '
    NF >= 3 && $1 !~ /^#/ && $1 != "" && $2 != "" {
        domains[$2] = 1
        printf "%s@%s %s/mail/%s/Maildir/\n", $1, $2, $2, $1 >> mailboxes
    }
    END {
        for (domain in domains) {
            print domain " OK"
        }
    }
' "$USERS_FILE" > "$tmp_domains"

if [ -f "$DOVECOT_USERS_FILE" ]; then
    awk -F: -v users="$USERS_FILE" '
        BEGIN {
            while ((getline line < users) > 0) {
                split(line, parts, ":")
                if (parts[1] != "" && parts[1] !~ /^#/ && parts[2] != "") {
                    keep[tolower(parts[1] "@" parts[2])] = 1
                }
            }
        }
        keep[tolower($1)] { print }
    ' "$DOVECOT_USERS_FILE" > "$tmp_dovecot"
    cp "$tmp_dovecot" "$DOVECOT_USERS_FILE"
else
    : > "$DOVECOT_USERS_FILE"
fi
chown root:mail "$DOVECOT_USERS_FILE"
chmod 664 "$DOVECOT_USERS_FILE"

cp "$tmp_domains" "$POSTFIX_DOMAINS"
cp "$tmp_mailboxes" "$POSTFIX_MAILBOXES"
chmod 600 "$POSTFIX_DOMAINS" "$POSTFIX_MAILBOXES"
chown root:root "$POSTFIX_DOMAINS" "$POSTFIX_MAILBOXES"

postmap lmdb:"$POSTFIX_DOMAINS" 2>/dev/null || postmap "$POSTFIX_DOMAINS"
postmap lmdb:"$POSTFIX_MAILBOXES" 2>/dev/null || postmap "$POSTFIX_MAILBOXES"

while IFS=: read -r user domain rest; do
    [ -n "$user" ] || continue
    case "$user" in \#*) continue ;; esac
    [ -n "$domain" ] || continue
    for folder in "" ".Sent" ".Drafts" ".Trash"; do
        mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/$folder/new"
        mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/$folder/cur"
        mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/$folder/tmp"
    done
    mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/attachments"
done < "$USERS_FILE"
chown -R www:mail "$LOCALBOX_BASE/domains" 2>/dev/null || true
chmod -R g+rwX "$LOCALBOX_BASE/domains" 2>/dev/null || true

rc-service dovecot reload 2>/dev/null || rc-service dovecot restart 2>/dev/null || true
rc-service postfix reload 2>/dev/null || rc-service postfix restart 2>/dev/null || true

if command -v lbu >/dev/null 2>&1; then
    mount -o remount,rw /media/mmcblk0p1 2>/dev/null || true
    lbu add "$USERS_FILE" "$DOVECOT_USERS_FILE" "$POSTFIX_DOMAINS" "$POSTFIX_MAILBOXES"
    lbu add "$POSTFIX_DOMAINS.lmdb" "$POSTFIX_MAILBOXES.lmdb" 2>/dev/null || true
    lbu add /etc/lbu/lbu.conf 2>/dev/null || true
    lbu commit -d
    mount -o remount,ro /media/mmcblk0p1 2>/dev/null || true
fi

echo "LocalBox mail maps synced."
