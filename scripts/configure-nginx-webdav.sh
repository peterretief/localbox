#!/bin/sh
set -eu

WEBDAV_HOST=${WEBDAV_HOST:-files.localboxmail.co.za}
LOCALBOX_BASE=${LOCALBOX_BASE:-/mnt/data/localbox}
FILES_ROOT=${FILES_ROOT:-$LOCALBOX_BASE/files}
NGINX_CONF=${NGINX_CONF:-/etc/nginx/http.d/localbox-webdav.conf}
WEBDAV_USERS=${WEBDAV_USERS:-/etc/nginx/localbox-webdav.htpasswd}
DOVECOT_USERS=${DOVECOT_USERS:-/etc/dovecot/localbox/users}
CLIENT_BODY_TEMP=${CLIENT_BODY_TEMP:-$LOCALBOX_BASE/tmp/nginx-client-body}
CLIENT_MAX_BODY_SIZE=${CLIENT_MAX_BODY_SIZE:-100m}

if [ ! -f "$DOVECOT_USERS" ]; then
    echo "ERROR: Dovecot users file not found: $DOVECOT_USERS" >&2
    exit 1
fi

if ! apk info -e nginx-mod-http-dav-ext >/dev/null 2>&1; then
    apk add -q nginx-mod-http-dav-ext
fi

mkdir -p "$FILES_ROOT" "$CLIENT_BODY_TEMP" "$(dirname "$WEBDAV_USERS")"

tmp_users=$(mktemp)
awk -F: '
    $1 != "" && $2 != "" {
        print $1 ":" $2
    }
' "$DOVECOT_USERS" > "$tmp_users"
install -m 640 -o root -g nginx "$tmp_users" "$WEBDAV_USERS"
rm -f "$tmp_users"

awk -F: '$1 != "" { print $1 }' "$DOVECOT_USERS" | while IFS= read -r user; do
    [ -n "$user" ] || continue
    mkdir -p "$FILES_ROOT/$user"
    chown nginx:nginx "$FILES_ROOT/$user"
    chmod 750 "$FILES_ROOT/$user"
done

chown -R nginx:nginx "$FILES_ROOT" "$CLIENT_BODY_TEMP"
chmod 750 "$FILES_ROOT" "$CLIENT_BODY_TEMP"

if [ -f "$NGINX_CONF" ]; then
    cp -p "$NGINX_CONF" "$NGINX_CONF.bak-$(date +%Y%m%d%H%M%S)"
fi

cat > "$NGINX_CONF" <<EOF
server {
    listen 80;
    server_name $WEBDAV_HOST;

    client_max_body_size $CLIENT_MAX_BODY_SIZE;
    client_body_temp_path $CLIENT_BODY_TEMP;

    auth_basic "LocalBox Files";
    auth_basic_user_file $WEBDAV_USERS;

    location / {
        alias $FILES_ROOT/\$remote_user/;
        autoindex on;
        autoindex_exact_size off;
        autoindex_localtime on;

        dav_methods PUT DELETE MKCOL COPY MOVE;
        dav_ext_methods PROPFIND OPTIONS;
        create_full_put_path on;
        dav_access user:rw group:rw all:r;
    }
}
EOF

nginx -t
rc-service nginx restart

lbu add "$NGINX_CONF" "$WEBDAV_USERS" /etc/nginx/modules /etc/apk/world 2>/dev/null || true
lbu commit -d 2>/dev/null || true

echo "Configured WebDAV:"
echo "  host: https://$WEBDAV_HOST/"
echo "  root: $FILES_ROOT"
echo "  users: mirrored from $DOVECOT_USERS"
