#!/bin/sh
set -eu

BOOT_CONFIG=${BOOT_CONFIG:-/media/mmcblk0p1/localbox-firstboot.conf}
[ -f "$BOOT_CONFIG" ] || BOOT_CONFIG=/boot/localbox-firstboot.conf

if [ ! -f "$BOOT_CONFIG" ]; then
    echo "localbox-firstboot: no config found"
    exit 0
fi

BOOT_DIR=$(dirname "$BOOT_CONFIG")
LOG_FILE="$BOOT_DIR/localbox-firstboot.log"
if touch "$LOG_FILE" 2>/dev/null; then
    exec >> "$LOG_FILE" 2>&1
else
    exec >> /tmp/localbox-firstboot.log 2>&1
fi
echo "================================================"
echo "   LocalBox first boot: $(date)"
echo "================================================"

CONFIG_TMP=$(mktemp)
PROVISION_TMP=$(mktemp)
cleanup() {
    rm -f "$CONFIG_TMP" "$PROVISION_TMP"
}
trap cleanup EXIT

sed 's/\r$//' "$BOOT_CONFIG" > "$CONFIG_TMP"

hostname=
ssh_authorized_key=
root_password=
headscale_login_server=
tailscale_authkey=
ingest_secret=
cloudflare_tunnel_token=
wifi_ssid=
wifi_password=

read_firstboot_config() {
    while IFS= read -r line || [ -n "$line" ]; do
        case "$line" in
            ''|'#'*) continue ;;
            *=*) ;;
            *) continue ;;
        esac

        key=${line%%=*}
        value=${line#*=}
        key=$(printf '%s' "$key" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
        value=$(printf '%s' "$value" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
        case "$value" in
            \"*\") value=${value#\"}; value=${value%\"} ;;
            \'*\') value=${value#\'}; value=${value%\'} ;;
        esac

        case "$key" in
            hostname) hostname=$value ;;
            ssh_authorized_key) ssh_authorized_key=$value ;;
            root_password) root_password=$value ;;
            headscale_login_server) headscale_login_server=$value ;;
            tailscale_authkey) tailscale_authkey=$value ;;
            ingest_secret) ingest_secret=$value ;;
            cloudflare_tunnel_token) cloudflare_tunnel_token=$value ;;
            wifi_ssid) wifi_ssid=$value ;;
            wifi_password) wifi_password=$value ;;
        esac
    done < "$CONFIG_TMP"
}

read_firstboot_config

ensure_line() {
    file=$1
    line=$2
    grep -qxF "$line" "$file" 2>/dev/null || printf '%s\n' "$line" >> "$file"
}

persist_if_exists() {
    path=$1
    [ ! -e "$path" ] || lbu add "$path" 2>/dev/null || true
}

if [ -n "${hostname:-}" ]; then
    echo ">> Setting hostname: $hostname"
    printf '%s\n' "$hostname" > /etc/hostname
    hostname "$hostname" 2>/dev/null || true
fi

echo ">> Hardening SSH defaults..."
mkdir -p /etc/ssh/sshd_config.d
cat > /etc/ssh/sshd_config.d/10-localbox-hardening.conf << 'EOF'
PermitEmptyPasswords no
PubkeyAuthentication yes
PermitRootLogin prohibit-password
EOF
if [ -n "${root_password:-}" ]; then
    printf 'root:%s\n' "$root_password" | chpasswd 2>/dev/null || true
fi
if [ -n "${ssh_authorized_key:-}" ]; then
    mkdir -p /root/.ssh
    chmod 700 /root/.ssh
    printf '%s\n' "$ssh_authorized_key" >> /root/.ssh/authorized_keys
    sort -u /root/.ssh/authorized_keys -o /root/.ssh/authorized_keys
    chmod 600 /root/.ssh/authorized_keys
fi

echo ">> Ensuring base network config..."
mkdir -p /etc/network
cat > /etc/network/interfaces << 'EOF'
auto lo
iface lo inet loopback

auto eth0
iface eth0 inet dhcp

auto end0
iface end0 inet dhcp
EOF
if [ -n "${wifi_ssid:-}" ] && [ -n "${wifi_password:-}" ]; then
    cat >> /etc/network/interfaces << 'EOF'

auto wlan0
iface wlan0 inet dhcp
    pre-up wpa_supplicant -B -i wlan0 -c /etc/wpa_supplicant/wpa_supplicant.conf || true
    post-down killall wpa_supplicant || true
EOF
fi
rc-update add networking default 2>/dev/null || true
rc-service networking restart 2>/dev/null || rc-service networking start 2>/dev/null || true
/usr/local/sbin/localbox-network-rescue 2>/dev/null || true

echo ">> Configuring repositories..."
cat > /etc/apk/repositories << 'EOF'
https://dl-cdn.alpinelinux.org/alpine/v3.24/main
https://dl-cdn.alpinelinux.org/alpine/v3.24/community
EOF
apk update -q 2>/dev/null || true

echo ">> Installing required packages if missing..."
apk add -q chrony chrony-openrc nginx nginx-openrc ca-certificates openssl curl tailscale tailscale-openrc 2>/dev/null || true

echo ">> Configuring clock..."
rc-update add chronyd default 2>/dev/null || true
rc-service chronyd restart 2>/dev/null || rc-service chronyd start 2>/dev/null || true
chronyd -q 2>/dev/null || true

echo ">> Creating users and groups..."
addgroup -S mail 2>/dev/null || true
adduser -D -H -s /sbin/nologin www 2>/dev/null || true
adduser www mail 2>/dev/null || true
adduser nginx mail 2>/dev/null || true

echo ">> Preparing LocalBox directories..."
mkdir -p /mnt/data /mnt/data/localbox /etc/localbox/templates /var/localbox
mount /mnt/data 2>/dev/null || true
chmod 755 /etc/localbox
touch /var/localbox/contact.log /var/localbox/send.log /var/localbox/security.log /etc/localbox/password-reset-tokens.conf
chown www:mail /var/localbox/contact.log /var/localbox/send.log /var/localbox/security.log /etc/localbox/password-reset-tokens.conf 2>/dev/null || true
chmod 664 /var/localbox/contact.log /var/localbox/send.log /var/localbox/security.log /etc/localbox/password-reset-tokens.conf

if [ -n "${ingest_secret:-}" ]; then
    echo ">> Writing ingest secret..."
    printf '%s\n' "$ingest_secret" > /etc/localbox/secret
    chmod 600 /etc/localbox/secret
    chown www:mail /etc/localbox/secret 2>/dev/null || true
fi

if [ -n "${cloudflare_tunnel_token:-}" ]; then
    echo ">> Writing Cloudflare tunnel token..."
    cat > /etc/localbox/cloudflare-tunnel.conf << EOF
token=$cloudflare_tunnel_token
EOF
    chmod 600 /etc/localbox/cloudflare-tunnel.conf
fi

echo ">> Building provisioning config..."
while IFS= read -r line || [ -n "$line" ]; do
    line=${line%%#*}
    line=$(printf '%s' "$line" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    [ -n "$line" ] || continue
    key=${line%%=*}
    case "$key" in
        domain|mailbox|admin|box_admin)
            printf '%s\n' "$line" >> "$PROVISION_TMP"
            ;;
    esac
done < "$CONFIG_TMP"

if [ -s "$PROVISION_TMP" ] && [ -x /usr/local/sbin/localbox-provision-template ]; then
    echo ">> Provisioning domains and mailboxes..."
    /usr/local/sbin/localbox-provision-template "$PROVISION_TMP"
fi
if [ -x /usr/local/sbin/localbox-add-email-routes ]; then
    echo ">> Configuring Cloudflare email routes..."
    /usr/local/sbin/localbox-add-email-routes "$CONFIG_TMP" || true
fi

echo ">> Configuring service logs..."
install -o www -g mail -m 664 /dev/null /var/log/mail-ingest.log 2>/dev/null || {
    touch /var/log/mail-ingest.log
    chmod 664 /var/log/mail-ingest.log
}
install -o www -g mail -m 664 /dev/null /var/log/contact-service.log 2>/dev/null || {
    touch /var/log/contact-service.log
    chmod 664 /var/log/contact-service.log
}

echo ">> Configuring nginx..."
mkdir -p /etc/nginx/http.d/disabled
for config in /etc/nginx/http.d/default.conf /etc/nginx/http.d/default_server.conf; do
    if [ -f "$config" ]; then
        mv "$config" "/etc/nginx/http.d/disabled/$(basename "$config").disabled"
    fi
done
rc-update add nginx default 2>/dev/null || true
nginx -t >/dev/null 2>&1 && rc-service nginx restart 2>/dev/null || true

echo ">> Starting LocalBox services..."
rc-update add sshd default 2>/dev/null || true
rc-update add mail-ingest default 2>/dev/null || true
rc-update add contact-service default 2>/dev/null || true
rc-service sshd start 2>/dev/null || true
rc-service mail-ingest restart 2>/dev/null || true
rc-service contact-service restart 2>/dev/null || true

echo ">> Configuring Tailscale..."
rc-update add tailscale default 2>/dev/null || true
rc-service tailscale restart 2>/dev/null || rc-service tailscale start 2>/dev/null || true
if [ -n "${headscale_login_server:-}" ]; then
    if ! tailscale status >/dev/null 2>&1; then
        if [ -n "${tailscale_authkey:-}" ]; then
            tailscale up --login-server "$headscale_login_server" --authkey "$tailscale_authkey" 2>/dev/null || true
        else
            tailscale up --login-server "$headscale_login_server" 2>/dev/null || true
        fi
    fi
fi

echo ">> Persisting boot state..."
sed -i 's/# LBU_MEDIA=.*/LBU_MEDIA=mmcblk0p1/;s/^LBU_MEDIA=.*/LBU_MEDIA=mmcblk0p1/' /etc/lbu/lbu.conf 2>/dev/null || true
persist_if_exists /etc/hostname
persist_if_exists /etc/ssh/sshd_config.d/10-localbox-hardening.conf
persist_if_exists /root/.ssh/authorized_keys
persist_if_exists /etc/network/interfaces
persist_if_exists /etc/wpa_supplicant
persist_if_exists /etc/runlevels/default/networking
persist_if_exists /etc/apk/repositories
persist_if_exists /etc/apk/world
persist_if_exists /etc/localbox
persist_if_exists /usr/local/sbin/localbox-add-email-routes
persist_if_exists /usr/local/sbin/localbox-network-rescue
persist_if_exists /etc/nginx/http.d
persist_if_exists /etc/init.d/mail-ingest
persist_if_exists /etc/init.d/contact-service
persist_if_exists /etc/init.d/localbox-network-rescue
persist_if_exists /etc/init.d/nginx
persist_if_exists /etc/init.d/tailscale
persist_if_exists /etc/init.d/chronyd
persist_if_exists /etc/conf.d/tailscale
persist_if_exists /etc/runlevels/default
persist_if_exists /var/lib/tailscale
persist_if_exists /var/log/mail-ingest.log
persist_if_exists /var/log/contact-service.log
persist_if_exists /var/localbox/contact.log
persist_if_exists /var/localbox/send.log
persist_if_exists /var/localbox/security.log
persist_if_exists /etc/localbox/password-reset-tokens.conf
persist_if_exists /etc/group

rc-update del localbox-firstboot default 2>/dev/null || true
persist_if_exists /etc/runlevels/default
lbu commit -d 2>/dev/null || true

echo "LocalBox first boot complete."
