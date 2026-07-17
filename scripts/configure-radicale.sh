#!/bin/sh
set -eu

LOCALBOX_BASE=${LOCALBOX_BASE:-/mnt/data/localbox}
LOCALBOX_ETC=${LOCALBOX_ETC:-/etc/localbox}
RADICALE_DATA=${RADICALE_DATA:-$LOCALBOX_BASE/radicale}
RADICALE_PASSWORD=${RADICALE_PASSWORD:-password1}

apk add -q radicale radicale-openrc apache2-utils

mkdir -p /etc/radicale "$RADICALE_DATA/collections"
chown -R radicale:radicale "$RADICALE_DATA" 2>/dev/null || true

create_default_collections() {
    users_file=$LOCALBOX_ETC/users.conf
    [ -f "$users_file" ] || return 0

    while IFS=: read -r user domain hash rest; do
        [ -n "$user" ] || continue
        case "$user" in \#*) continue ;; esac
        [ -n "$domain" ] || continue

        user_root=$RADICALE_DATA/collections/collection-root/$user@$domain
        contacts=$user_root/contacts
        calendar=$user_root/calendar
        mkdir -p "$contacts" "$calendar"
        [ -f "$contacts/.Radicale.props" ] || printf '%s\n' '{"D:displayname":"Contacts","tag":"VADDRESSBOOK"}' > "$contacts/.Radicale.props"
        [ -f "$calendar/.Radicale.props" ] || printf '%s\n' '{"C:supported-calendar-component-set":"VEVENT,VTODO,VJOURNAL","D:displayname":"Calendar","tag":"VCALENDAR"}' > "$calendar/.Radicale.props"
    done < "$users_file"

    chown -R radicale:radicale "$RADICALE_DATA/collections" 2>/dev/null || true
}

create_default_collections

cat > /etc/radicale/config << EOF
[server]
hosts = 127.0.0.1:5232
max_content_length = 100000000

[auth]
type = htpasswd
htpasswd_filename = /etc/radicale/users
htpasswd_encryption = sha512
lc_username = True
urldecode_username = True

[rights]
type = owner_only

[storage]
filesystem_folder = $RADICALE_DATA/collections
EOF

touch /etc/radicale/users
cp -p /etc/radicale/users /etc/radicale/users.bak-$(date +%Y%m%d%H%M%S) 2>/dev/null || true
: > /etc/radicale/users
while IFS=: read -r user domain hash rest; do
    [ -n "$user" ] || continue
    case "$user" in \#*) continue ;; esac
    [ -n "$domain" ] || continue
    hash=$(openssl passwd -6 "$RADICALE_PASSWORD")
    printf '%s:%s\n' "$user@$domain" "$hash" >> /etc/radicale/users
done < "$LOCALBOX_ETC/users.conf"

chown root:radicale /etc/radicale/config /etc/radicale/rights /etc/radicale/users
chmod 640 /etc/radicale/config /etc/radicale/rights /etc/radicale/users

rc-update add radicale default 2>/dev/null || true
rc-service radicale restart
rc-service radicale status

lbu add /etc/radicale /etc/init.d/radicale /etc/runlevels/default/radicale /etc/apk/world 2>/dev/null || true
lbu commit -d 2>/dev/null || true

echo "Configured Radicale contacts/calendar on 127.0.0.1:5232."
