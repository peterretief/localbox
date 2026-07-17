#!/bin/sh
set -eu

LOCALBOX_BASE=${LOCALBOX_BASE:-/mnt/data/localbox}
LOCALBOX_ETC=${LOCALBOX_ETC:-/etc/localbox}
RADICALE_DATA=${RADICALE_DATA:-$LOCALBOX_BASE/radicale}
DOVECOT_CONF=${DOVECOT_CONF:-/etc/dovecot/dovecot.conf}
RADICALE_CONF=${RADICALE_CONF:-/etc/radicale/config}
AUTH_SOCKET=${AUTH_SOCKET:-/run/dovecot/auth-radicale}
AUTH_LISTENER=${AUTH_SOCKET#/run/dovecot/}

mkdir -p "$RADICALE_DATA/collections"
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

if ! grep -q "$AUTH_SOCKET\\|unix_listener $AUTH_LISTENER" "$DOVECOT_CONF"; then
    cp -p "$DOVECOT_CONF" "$DOVECOT_CONF.bak-radicale-auth-$(date +%Y%m%d%H%M%S)"
    tmp=$(mktemp)
    awk -v socket="$AUTH_LISTENER" '
        /^service auth[[:space:]]*\{/ && !done {
            print
            print "  unix_listener " socket " {"
            print "    mode = 0660"
            print "    user = radicale"
            print "    group = radicale"
            print "  }"
            done = 1
            next
        }
        { print }
    ' "$DOVECOT_CONF" > "$tmp"
    cat "$tmp" > "$DOVECOT_CONF"
    rm -f "$tmp"
fi

tmp=$(mktemp)
sed "s|unix_listener $AUTH_SOCKET|unix_listener $AUTH_LISTENER|g" "$DOVECOT_CONF" > "$tmp"
cat "$tmp" > "$DOVECOT_CONF"
rm -f "$tmp"

cp -p "$RADICALE_CONF" "$RADICALE_CONF.bak-dovecot-auth-$(date +%Y%m%d%H%M%S)" 2>/dev/null || true
cat > "$RADICALE_CONF" << EOF
[server]
hosts = 127.0.0.1:5232
max_content_length = 100000000

[auth]
type = dovecot
dovecot_connection_type = AF_UNIX
dovecot_socket = $AUTH_SOCKET
lc_username = True
urldecode_username = True

[rights]
type = owner_only

[storage]
filesystem_folder = $RADICALE_DATA/collections
EOF

chown root:radicale "$RADICALE_CONF" /etc/radicale/rights 2>/dev/null || true
chmod 640 "$RADICALE_CONF" /etc/radicale/rights 2>/dev/null || true

rc-service dovecot restart
rc-service radicale restart
rc-service dovecot status
rc-service radicale status

lbu add "$DOVECOT_CONF" "$RADICALE_CONF" /etc/radicale /etc/dovecot 2>/dev/null || true
lbu commit -d 2>/dev/null || true

echo "Configured Radicale to authenticate through Dovecot at $AUTH_SOCKET."
