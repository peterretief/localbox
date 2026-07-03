#!/bin/sh
set -e

# ── Load settings ────────────────────────────────────────────────────────────
if [ -f "./settings.conf" ]; then
    . ./settings.conf
fi

# Required settings check
if [ -z "$CLOUDFLARE_TUNNEL_TOKEN" ]; then
    echo "ERROR: CLOUDFLARE_TUNNEL_TOKEN not set in settings.conf"
    exit 1
fi
if [ -z "$MAIL_DOMAIN" ]; then
    echo "ERROR: MAIL_DOMAIN not set in settings.conf"
    exit 1
fi
if [ -z "$INGEST_SECRET" ]; then
    echo "ERROR: INGEST_SECRET not set in settings.conf"
    exit 1
fi

echo "================================================"
echo "   LocalBox Mail Server Setup"
echo "   by Diode (diode.co.za)"
echo "================================================"
echo ""
echo "Domain: $MAIL_DOMAIN"
echo ""

# ── User setup wizard ────────────────────────────────────────────────────────
echo "=== Create your mail account ==="
printf "Username: "
read MAIL_USER
printf "Password: "
read -s MAIL_PASS
echo ""
printf "Confirm password: "
read -s MAIL_PASS2
echo ""

if [ "$MAIL_PASS" != "$MAIL_PASS2" ]; then
    echo "ERROR: Passwords do not match"
    exit 1
fi

if [ -z "$MAIL_USER" ] || [ -z "$MAIL_PASS" ]; then
    echo "ERROR: Username and password cannot be empty"
    exit 1
fi

echo ""
echo "Setting up LocalBox for $MAIL_USER@$MAIL_DOMAIN..."
echo ""

# ── 1. Repos ─────────────────────────────────────────────────────────────────
echo ">> Configuring Alpine repositories..."
cat > /etc/apk/repositories << 'EOF'
https://dl-cdn.alpinelinux.org/alpine/v3.24/main
https://dl-cdn.alpinelinux.org/alpine/v3.24/community
EOF
apk update -q

# ── 2. Packages ──────────────────────────────────────────────────────────────
echo ">> Installing packages..."
apk add -q \
    syncthing \
    syncthing-openrc \
    chrony \
    nginx \
    nginx-openrc \
    curl \
    wget \
    ca-certificates \
    openssl

# ── 3. Clock ─────────────────────────────────────────────────────────────────
echo ">> Syncing clock..."
chronyd -q 2>/dev/null || true

# ── 4. Users and groups ──────────────────────────────────────────────────────
echo ">> Creating system users..."
addgroup -S mail 2>/dev/null || true
adduser -D -H -s /sbin/nologin www 2>/dev/null || true
adduser www mail 2>/dev/null || true
adduser nginx mail 2>/dev/null || true
adduser syncthing mail 2>/dev/null || true

# ── 5. Mount USB storage ─────────────────────────────────────────────────────
echo ">> Mounting USB storage..."
mkdir -p /mnt/data
mount /mnt/data 2>/dev/null || true

# ── 6. Directory structure ───────────────────────────────────────────────────
echo ">> Creating directory structure..."
mkdir -p /mnt/data/localbox/domains/$MAIL_DOMAIN/mail/$MAIL_USER/Maildir/new
mkdir -p /mnt/data/localbox/domains/$MAIL_DOMAIN/mail/$MAIL_USER/Maildir/cur
mkdir -p /mnt/data/localbox/domains/$MAIL_DOMAIN/mail/$MAIL_USER/Maildir/tmp
mkdir -p /mnt/data/localbox/domains/$MAIL_DOMAIN/mail/$MAIL_USER/attachments
mkdir -p /mnt/data/localbox/domains/$MAIL_DOMAIN/site/assets/images
chown -R www:mail /mnt/data/localbox
chmod -R g+rwX /mnt/data/localbox
chmod 2775 /mnt/data/localbox/domains/$MAIL_DOMAIN /mnt/data/localbox/domains/$MAIL_DOMAIN/mail /mnt/data/localbox/domains/$MAIL_DOMAIN/site /mnt/data/localbox/domains/$MAIL_DOMAIN/site/assets /mnt/data/localbox/domains/$MAIL_DOMAIN/site/assets/images

# ── 7. LocalBox config ───────────────────────────────────────────────────────
echo ">> Creating LocalBox config..."
mkdir -p /etc/localbox/templates
chmod 755 /etc/localbox

# Secret
echo "$INGEST_SECRET" > /etc/localbox/secret
chmod 600 /etc/localbox/secret
chown www:mail /etc/localbox/secret

# Generate password hash
MAIL_HASH=$(echo -n "$MAIL_PASS" | sha512sum | awk '{print $1}' | xxd -r -p | base64 -w 0)

# Users file
echo "$MAIL_USER:$MAIL_DOMAIN:$MAIL_HASH" > /etc/localbox/users.conf
echo "$MAIL_DOMAIN" > /etc/localbox/domains.conf
echo "$MAIL_USER:$MAIL_DOMAIN" > /etc/localbox/admins.conf
echo "$MAIL_USER:$MAIL_DOMAIN" > /etc/localbox/box-admins.conf
chown www:mail /etc/localbox/users.conf
chown www:mail /etc/localbox/domains.conf
chown www:mail /etc/localbox/admins.conf
chown www:mail /etc/localbox/box-admins.conf
chmod 640 /etc/localbox/users.conf
chmod 640 /etc/localbox/domains.conf
chmod 640 /etc/localbox/admins.conf
chmod 640 /etc/localbox/box-admins.conf
echo "   Created account: $MAIL_USER@$MAIL_DOMAIN"

# ── 8. Log file ──────────────────────────────────────────────────────────────
echo ">> Creating log file..."
touch /var/log/mail-ingest.log
chown www:mail /var/log/mail-ingest.log
chmod 664 /var/log/mail-ingest.log

# ── 9. mail-ingest init script ───────────────────────────────────────────────
echo ">> Creating mail-ingest service..."
cat > /etc/init.d/mail-ingest << EOF
#!/sbin/openrc-run
name="mail-ingest"
description="LocalBox mail ingest daemon"
command="/usr/local/bin/mail-ingest"
command_user="www:mail"
directory="/etc/localbox"
pidfile="/run/mail-ingest.pid"
command_background=true
output_log="/var/log/mail-ingest.log"
error_log="/var/log/mail-ingest.log"
export LOCALBOX_SECRET="$INGEST_SECRET"
export LOCALBOX_BASE="/mnt/data/localbox"
depend() {
    need net
    after firewall
}
EOF
chmod +x /etc/init.d/mail-ingest

# ── 10. Nginx domain routing ─────────────────────────────────────────────────
echo ">> Configuring nginx domain routing..."
mkdir -p /etc/nginx/http.d
mkdir -p /etc/nginx/http.d/disabled
for config in /etc/nginx/http.d/default.conf /etc/nginx/http.d/default_server.conf; do
    if [ -f "$config" ]; then
        mv "$config" "/etc/nginx/http.d/disabled/$(basename "$config").disabled"
    fi
done
for config in /etc/nginx/http.d/disabled/*.conf; do
    if [ -f "$config" ]; then
        mv "$config" "$config.disabled"
    fi
done

cat > /etc/nginx/http.d/localbox-sites.conf << 'EOF'
map $host $localbox_domain {
    default $host;
    ~^www\.(?<bare>.+)$ $bare;
}

server {
    listen 80 default_server;
    server_name _;
    root /mnt/data/localbox/domains/$localbox_domain/site;
    index index.html;

    location = /contact {
        proxy_pass http://127.0.0.1:8081/contact;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location / {
        try_files $uri $uri/ /index.html =404;
    }
}
EOF

# ── 11. Cloudflared ──────────────────────────────────────────────────────────
echo ">> Installing cloudflared..."
wget -q --no-check-certificate \
    https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-arm \
    -O /usr/local/bin/cloudflared
chmod +x /usr/local/bin/cloudflared

echo ">> Configuring Cloudflare tunnel..."
cloudflared service uninstall 2>/dev/null || true
cloudflared service install $CLOUDFLARE_TUNNEL_TOKEN

# ── 12. Syncthing ────────────────────────────────────────────────────────────
echo ">> Configuring Syncthing..."
rc-update add syncthing default 2>/dev/null || true

# ── 13. fstab ────────────────────────────────────────────────────────────────
echo ">> Configuring fstab..."
USB_UUID=$(blkid /dev/sda1 | grep -o 'UUID="[^"]*"' | cut -d'"' -f2)
if [ -n "$USB_UUID" ]; then
    if ! grep -q "$USB_UUID" /etc/fstab; then
        echo "UUID=$USB_UUID /mnt/data ext4 defaults 0 2" >> /etc/fstab
        echo "   Added USB mount: $USB_UUID"
    fi
else
    echo "   WARNING: USB device not found - add fstab entry manually"
fi

# ── 14. Add services to boot ─────────────────────────────────────────────────
echo ">> Adding services to boot..."
rc-update add mail-ingest default 2>/dev/null || true
rc-update add contact-service default 2>/dev/null || true
rc-update add nginx default 2>/dev/null || true
rc-update add cloudflared default 2>/dev/null || true

# ── 15. Start services ───────────────────────────────────────────────────────
echo ">> Starting services..."
rm -f /run/mail-ingest.pid
rc-service mail-ingest zap 2>/dev/null || true
rc-service mail-ingest start
nginx -t && rc-service nginx restart
rc-service syncthing start
rc-service cloudflared start

# ── 16. LBU persistence ──────────────────────────────────────────────────────
echo ">> Saving configuration to SD card..."
sed -i 's/# LBU_MEDIA=usb/LBU_MEDIA=mmcblk0p1/' /etc/lbu/lbu.conf 2>/dev/null || true
lbu add /etc/apk/repositories
lbu add /usr/local/bin
lbu add /etc/init.d/mail-ingest
lbu add /etc/init.d/contact-service
lbu add /etc/init.d/cloudflared
lbu add /etc/nginx/http.d/localbox-sites.conf
lbu add /etc/localbox
lbu add /etc/fstab
lbu add /var/log/mail-ingest.log
lbu add /var/log/contact-service.log
lbu add /var/localbox/contact.log
lbu commit -d

echo ""
echo "================================================"
echo "   LocalBox setup complete!"
echo ""
echo "   Your mail address: $MAIL_USER@$MAIL_DOMAIN"
echo "   Web access: https://a.diode.co.za"
echo ""
echo "   Install Syncthing on your devices to"
echo "   sync your mail locally."
echo "================================================"
echo ""
