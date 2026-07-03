#!/bin/sh
set -eu

LOCALBOX_BASE=${LOCALBOX_BASE:-/mnt/data/localbox}
LOCALBOX_ETC=${LOCALBOX_ETC:-/etc/localbox}
TEMPLATE=${1:-}

if [ -z "$TEMPLATE" ] || [ ! -f "$TEMPLATE" ]; then
    echo "Usage: localbox-provision-template /path/to/template.conf"
    exit 1
fi

mkdir -p "$LOCALBOX_ETC/templates"
touch "$LOCALBOX_ETC/users.conf" "$LOCALBOX_ETC/domains.conf" "$LOCALBOX_ETC/admins.conf" "$LOCALBOX_ETC/box-admins.conf"
chmod 755 "$LOCALBOX_ETC"

normalize_email() {
    printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]'
}

valid_domain() {
    printf '%s' "$1" | grep -Eq '^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$'
}

valid_user() {
    printf '%s' "$1" | grep -Eq '^[a-z0-9._-]+$'
}

ensure_line() {
    file=$1
    line=$2
    grep -qxF "$line" "$file" 2>/dev/null || printf '%s\n' "$line" >> "$file"
}

password_hash() {
    password=$1
    salt=$(openssl rand -base64 18 | tr -dc 'A-Za-z0-9' | cut -c1-16)
    key=$(printf '%s' "$password" | openssl dgst -sha512 -binary | base64 | tr -d '\n')
    printf '%s' "$key"
}

ensure_domain() {
    domain=$1
    valid_domain "$domain" || {
        echo "Invalid domain: $domain"
        exit 1
    }
    ensure_line "$LOCALBOX_ETC/domains.conf" "$domain"
    mkdir -p "$LOCALBOX_BASE/domains/$domain/mail"
    mkdir -p "$LOCALBOX_BASE/domains/$domain/site/assets/images"
    if [ ! -f "$LOCALBOX_BASE/domains/$domain/site/index.html" ]; then
        cat > "$LOCALBOX_BASE/domains/$domain/site/index.html" << 'EOF'
<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"><title>LocalBox</title></head>
<body><main><h1>Welcome</h1><p>This LocalBox site is ready.</p></main></body>
</html>
EOF
    fi
    for name in contact.html thankyou.html; do
        if [ ! -f "$LOCALBOX_BASE/domains/$domain/site/$name" ] && [ -f "$LOCALBOX_ETC/templates/$name" ]; then
            cp "$LOCALBOX_ETC/templates/$name" "$LOCALBOX_BASE/domains/$domain/site/$name"
        fi
    done
}

ensure_mailbox() {
    email=$(normalize_email "$1")
    password=$2
    user=${email%@*}
    domain=${email#*@}
    if [ "$user" = "$email" ] || [ -z "$user" ] || [ -z "$domain" ]; then
        echo "Invalid mailbox email: $email"
        exit 1
    fi
    valid_user "$user" || {
        echo "Invalid mailbox user: $user"
        exit 1
    }
    ensure_domain "$domain"
    if ! grep -q "^$user:$domain:" "$LOCALBOX_ETC/users.conf"; then
        hash=$(password_hash "$password")
        printf '%s:%s:%s\n' "$user" "$domain" "$hash" >> "$LOCALBOX_ETC/users.conf"
    fi
    mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/new"
    mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/cur"
    mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/tmp"
    mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/.Sent/new"
    mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/.Sent/cur"
    mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/.Sent/tmp"
    mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/.Drafts/new"
    mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/.Drafts/cur"
    mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/.Drafts/tmp"
    mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/.Trash/new"
    mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/.Trash/cur"
    mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/Maildir/.Trash/tmp"
    mkdir -p "$LOCALBOX_BASE/domains/$domain/mail/$user/attachments"
}

ensure_role() {
    file=$1
    email=$(normalize_email "$2")
    user=${email%@*}
    domain=${email#*@}
    if [ "$user" = "$email" ] || [ -z "$user" ] || [ -z "$domain" ]; then
        echo "Invalid role email: $email"
        exit 1
    fi
    ensure_line "$file" "$user:$domain"
}

while IFS= read -r line || [ -n "$line" ]; do
    line=${line%%#*}
    line=$(printf '%s' "$line" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    [ -n "$line" ] || continue
    key=${line%%=*}
    value=${line#*=}
    key=$(printf '%s' "$key" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')
    value=$(printf '%s' "$value" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')

    case "$key" in
        domain)
            ensure_domain "$(normalize_email "$value")"
            ;;
        mailbox)
            email=${value%%:*}
            password=${value#*:}
            if [ "$email" = "$password" ] || [ -z "$password" ]; then
                echo "Mailbox needs password: $value"
                exit 1
            fi
            ensure_mailbox "$email" "$password"
            ;;
        admin)
            ensure_role "$LOCALBOX_ETC/admins.conf" "$value"
            ;;
        box_admin)
            ensure_role "$LOCALBOX_ETC/box-admins.conf" "$value"
            ;;
        *)
            echo "Unknown template key: $key"
            exit 1
            ;;
    esac
done < "$TEMPLATE"

chown -R www:mail "$LOCALBOX_BASE" 2>/dev/null || true
chmod -R g+rwX "$LOCALBOX_BASE" 2>/dev/null || true
chmod 640 "$LOCALBOX_ETC/users.conf" "$LOCALBOX_ETC/domains.conf" "$LOCALBOX_ETC/admins.conf" "$LOCALBOX_ETC/box-admins.conf"
chown www:mail "$LOCALBOX_ETC/users.conf" "$LOCALBOX_ETC/domains.conf" "$LOCALBOX_ETC/admins.conf" "$LOCALBOX_ETC/box-admins.conf" 2>/dev/null || true

lbu add "$LOCALBOX_ETC" 2>/dev/null || true
lbu commit -d 2>/dev/null || true

echo "Provisioning complete."
