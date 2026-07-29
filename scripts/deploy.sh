#!/bin/sh
set -e

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
cd "$PROJECT_DIR"

# ── Load settings ────────────────────────────────────────────────────────────
ENV_PI_HOST=${PI_HOST:-}
ENV_PI_USER=${PI_USER:-}
ENV_GOCACHE=${GOCACHE:-}
if [ -f "./settings.conf" ]; then
    . ./settings.conf
fi
if [ -n "$ENV_PI_HOST" ]; then
    PI_HOST=$ENV_PI_HOST
fi
if [ -n "$ENV_PI_USER" ]; then
    PI_USER=$ENV_PI_USER
fi
if [ -n "$ENV_GOCACHE" ]; then
    GOCACHE=$ENV_GOCACHE
fi
PI_HOST=${PI_HOST:-192.168.0.6}
PI_USER=${PI_USER:-root}
GOCACHE=${GOCACHE:-/tmp/localbox-go-build}
export GOCACHE

echo "=== LocalBox Deploy ==="
echo "Target: $PI_USER@$PI_HOST"

# ── Build ────────────────────────────────────────────────────────────────────
echo ">> Building mail-ingest for ARM..."
GOOS=linux GOARCH=arm GOARM=7 go build -o mail-ingest .
echo "   Built successfully"

echo ">> Building contact service for ARM..."
GOOS=linux GOARCH=arm GOARM=7 go build -o contact-service ./cmd/contact
echo "   Built successfully"

# ── Deploy binary ────────────────────────────────────────────────────────────
echo ">> Deploying binary..."
cat mail-ingest | ssh $PI_USER@$PI_HOST \
    'kill $(pgrep mail-ingest) 2>/dev/null; \
     rm -f /usr/local/bin/mail-ingest; \
     cat > /usr/local/bin/mail-ingest && \
     chmod +x /usr/local/bin/mail-ingest'
echo "   Binary deployed"

echo ">> Deploying optional admin tools..."
cat scripts/enable-mail-client-access.sh | ssh $PI_USER@$PI_HOST \
    'mkdir -p /usr/local/sbin && \
     cat > /usr/local/sbin/localbox-enable-mail-client-access && \
     chmod +x /usr/local/sbin/localbox-enable-mail-client-access'
cat scripts/enable-tailscale-persistence.sh | ssh $PI_USER@$PI_HOST \
    'mkdir -p /usr/local/sbin && \
     cat > /usr/local/sbin/localbox-enable-tailscale-persistence && \
     chmod +x /usr/local/sbin/localbox-enable-tailscale-persistence'
cat scripts/create-localbox-backup.sh | ssh $PI_USER@$PI_HOST \
    'mkdir -p /usr/local/sbin && \
     cat > /usr/local/sbin/localbox-create-backup && \
     chmod +x /usr/local/sbin/localbox-create-backup'
cat scripts/sync-surrogate-mail.sh | ssh $PI_USER@$PI_HOST \
    'mkdir -p /usr/local/sbin && \
     cat > /usr/local/sbin/localbox-sync-surrogate-mail && \
     chmod +x /usr/local/sbin/localbox-sync-surrogate-mail'
cat scripts/provision-template.sh | ssh $PI_USER@$PI_HOST \
    'mkdir -p /usr/local/sbin && \
     cat > /usr/local/sbin/localbox-provision-template && \
     chmod +x /usr/local/sbin/localbox-provision-template'
cat scripts/add-cloudflare-tunnel-route.sh | ssh $PI_USER@$PI_HOST \
    'mkdir -p /usr/local/sbin && \
     cat > /usr/local/sbin/localbox-add-tunnel-route && \
     chmod +x /usr/local/sbin/localbox-add-tunnel-route'
cat scripts/add-cloudflare-email-routes.sh | ssh $PI_USER@$PI_HOST \
    'mkdir -p /usr/local/sbin && \
     cat > /usr/local/sbin/localbox-add-email-routes && \
     chmod +x /usr/local/sbin/localbox-add-email-routes'
cat scripts/localbox-firstboot.sh | ssh $PI_USER@$PI_HOST \
    'mkdir -p /usr/local/sbin && \
     cat > /usr/local/sbin/localbox-firstboot && \
     chmod +x /usr/local/sbin/localbox-firstboot'
cat localbox-template.conf.example | ssh $PI_USER@$PI_HOST \
    'mkdir -p /etc/localbox/examples && \
     cat > /etc/localbox/examples/localbox-template.conf.example'
cat domainscoza.conf.example | ssh $PI_USER@$PI_HOST \
    'mkdir -p /etc/localbox/examples && \
     cat > /etc/localbox/examples/domainscoza.conf.example'
ssh $PI_USER@$PI_HOST \
    'if [ -f /etc/localbox/domainscoza.conf ]; then \
         grep -q "^ns1=" /etc/localbox/domainscoza.conf || printf "\nns1=sam.ns.cloudflare.com\n" >> /etc/localbox/domainscoza.conf; \
         grep -q "^ns2=" /etc/localbox/domainscoza.conf || printf "ns2=marissa.ns.cloudflare.com\n" >> /etc/localbox/domainscoza.conf; \
     fi'
cat cloudflare.conf.example | ssh $PI_USER@$PI_HOST \
    'mkdir -p /etc/localbox/examples && \
     cat > /etc/localbox/examples/cloudflare.conf.example'
cat cloudflare-tunnel.conf.example | ssh $PI_USER@$PI_HOST \
    'mkdir -p /etc/localbox/examples && \
     cat > /etc/localbox/examples/cloudflare-tunnel.conf.example'
cat paystack.conf.example | ssh $PI_USER@$PI_HOST \
    'mkdir -p /etc/localbox/examples && \
     cat > /etc/localbox/examples/paystack.conf.example'
echo "   Optional admin tools deployed"

# ── Deploy contact service ───────────────────────────────────────────────────
echo ">> Deploying contact service..."
cat contact-service | ssh $PI_USER@$PI_HOST \
    'kill $(pgrep contact-service) 2>/dev/null; \
     rm -f /usr/local/bin/contact-service; \
     cat > /usr/local/bin/contact-service && \
     chmod +x /usr/local/bin/contact-service && \
     mkdir -p /var/localbox && \
     touch /var/localbox/contact.log && \
     touch /var/localbox/send.log && \
     chmod 664 /var/localbox/contact.log'

ssh $PI_USER@$PI_HOST 'cat > /etc/init.d/contact-service << '"'"'EOF'"'"'
#!/sbin/openrc-run
name="contact-service"
description="LocalBox contact form service"
command="/usr/local/bin/contact-service"
command_user="www:mail"
pidfile="/run/contact-service.pid"
command_background=true
output_log="/var/log/contact-service.log"
error_log="/var/log/contact-service.log"
depend() {
    need net
    after firewall
}
EOF
chmod +x /etc/init.d/contact-service
touch /var/log/contact-service.log
touch /var/localbox/send.log
touch /var/localbox/security.log
touch /etc/localbox/password-reset-tokens.conf
chown www:mail /var/log/contact-service.log /var/localbox/contact.log /var/localbox/send.log /var/localbox/security.log /etc/localbox/password-reset-tokens.conf 2>/dev/null || true
chmod 664 /var/log/contact-service.log /var/localbox/contact.log /var/localbox/send.log /var/localbox/security.log /etc/localbox/password-reset-tokens.conf
rc-update add contact-service default 2>/dev/null || true'
echo "   Contact service deployed"

# ── Deploy templates ─────────────────────────────────────────────────────────
echo ">> Deploying templates..."
for f in templates/*.html; do
    fname=$(basename "$f")
    cat "$f" | ssh $PI_USER@$PI_HOST "cat > /etc/localbox/templates/$fname"
    echo "   Deployed $fname"
done

echo ">> Ensuring domain site pages..."
ssh $PI_USER@$PI_HOST 'sh -s' << 'EOF'
set -e
adduser nginx mail 2>/dev/null || true
for domain_dir in /mnt/data/localbox/domains/*; do
    [ -d "$domain_dir" ] || continue
    site_dir="$domain_dir/site"
    mkdir -p "$site_dir/assets/images"
    [ -f "$site_dir/contact.html" ] || cp /etc/localbox/templates/contact.html "$site_dir/contact.html"
    [ -f "$site_dir/thankyou.html" ] || cp /etc/localbox/templates/thankyou.html "$site_dir/thankyou.html"
    if [ ! -f "$site_dir/index.html" ]; then
        cat > "$site_dir/index.html" << 'HTML'
<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"><title>LocalBox</title></head>
<body><main><h1>Welcome</h1><p>This LocalBox site is ready.</p></main></body>
</html>
HTML
    fi
done
chown -R www:mail /mnt/data/localbox
chmod -R g+rwX /mnt/data/localbox
find /mnt/data/localbox/domains -type d -path '*/site*' -exec chmod 2775 {} \;
EOF

echo ">> Migrating Sent folders..."
ssh $PI_USER@$PI_HOST 'sh -s' << 'EOF'
set -e
for mailbox in /mnt/data/localbox/domains/*/mail/*; do
    [ -d "$mailbox/Sent" ] || continue
    [ -d "$mailbox/Maildir" ] || continue
    mkdir -p "$mailbox/Maildir/.Sent/new" "$mailbox/Maildir/.Sent/cur" "$mailbox/Maildir/.Sent/tmp"
    for dir in new cur tmp; do
        [ -d "$mailbox/Sent/$dir" ] || continue
        for file in "$mailbox/Sent/$dir"/*; do
            [ -f "$file" ] || continue
            mv "$file" "$mailbox/Maildir/.Sent/$dir/"
        done
    done
    rmdir "$mailbox/Sent/new" "$mailbox/Sent/cur" "$mailbox/Sent/tmp" "$mailbox/Sent" 2>/dev/null || true
done
chown -R www:mail /mnt/data/localbox
chmod -R g+rwX /mnt/data/localbox
EOF

echo ">> Marking existing uploaded HTML files..."
ssh $PI_USER@$PI_HOST 'sh -s' << 'EOF'
set -e
for site_dir in /mnt/data/localbox/domains/*/site; do
    [ -d "$site_dir" ] || continue
    uploads="$site_dir/html-uploads.conf"
    touch "$uploads"
    for file in "$site_dir"/index-*.html; do
        [ -f "$file" ] || continue
        name=$(basename "$file")
        size=$(wc -c < "$file" | tr -d " ")
        case "$name" in
            index-[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]T[0-9][0-9][0-9][0-9][0-9][0-9]Z.html)
                if [ "$size" -gt 1200 ] && ! grep -qxF "$name" "$uploads"; then
                    echo "$name" >> "$uploads"
                fi
                ;;
        esac
    done
done
chown -R www:mail /mnt/data/localbox
chmod -R g+rwX /mnt/data/localbox
EOF

# ── Configure nginx domain routing ───────────────────────────────────────────
echo ">> Configuring nginx domain routing..."
ssh $PI_USER@$PI_HOST 'sh -s' << 'EOF'
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

cat > /etc/nginx/http.d/localbox-sites.conf << 'NGINX'
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

    location = /waitlist {
        proxy_pass http://127.0.0.1:8081/waitlist;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /register {
        proxy_pass http://127.0.0.1:8080/register;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /register/verify {
        proxy_pass http://127.0.0.1:8080/register/verify;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /login {
        proxy_pass http://127.0.0.1:8080/login;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /forgot-password {
        proxy_pass http://127.0.0.1:8080/forgot-password;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /reset-password {
        proxy_pass http://127.0.0.1:8080/reset-password;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /dashboard {
        proxy_pass http://127.0.0.1:8080/dashboard;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /apps/mail {
        proxy_pass http://127.0.0.1:8080/apps/mail;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /apps/files {
        proxy_pass http://127.0.0.1:8080/apps/files;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location /apps/files/ {
        proxy_pass http://127.0.0.1:8080/apps/files/;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /files {
        proxy_pass http://127.0.0.1:8080/files;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /files/download {
        proxy_pass http://127.0.0.1:8080/files/download;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /files/delete {
        proxy_pass http://127.0.0.1:8080/files/delete;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /mail {
        proxy_pass http://127.0.0.1:8080/mail;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /public/logout {
        proxy_pass http://127.0.0.1:8080/public/logout;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /admin {
        proxy_pass http://127.0.0.1:8080/admin;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /attachment {
        proxy_pass http://127.0.0.1:8080/attachment;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /certificate {
        proxy_pass http://127.0.0.1:8080/certificate;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /compose {
        proxy_pass http://127.0.0.1:8080/compose;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /logout {
        proxy_pass http://127.0.0.1:8080/logout;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /message {
        proxy_pass http://127.0.0.1:8080/message;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /message/action {
        proxy_pass http://127.0.0.1:8080/message/action;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /orders {
        proxy_pass http://127.0.0.1:8080/orders;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /page {
        client_max_body_size 25m;
        proxy_pass http://127.0.0.1:8080/page;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /page/html {
        client_max_body_size 25m;
        proxy_pass http://127.0.0.1:8080/page/html;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /page/images {
        client_max_body_size 25m;
        proxy_pass http://127.0.0.1:8080/page/images;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /password {
        proxy_pass http://127.0.0.1:8080/password;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /system {
        proxy_pass http://127.0.0.1:8080/system;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /system/certificate {
        proxy_pass http://127.0.0.1:8080/system/certificate;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /health {
        proxy_pass http://127.0.0.1:8080/health;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /ingest {
        proxy_pass http://127.0.0.1:8080/ingest;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /payment/start {
        proxy_pass http://127.0.0.1:8080/payment/start;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /payment/paystack/return {
        proxy_pass http://127.0.0.1:8080/payment/paystack/return;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /payment/paystack/webhook {
        proxy_pass http://127.0.0.1:8080/payment/paystack/webhook;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /account/domains {
        proxy_pass http://127.0.0.1:8080/account/domains;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /account/order {
        proxy_pass http://127.0.0.1:8080/account/order;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /order {
        proxy_pass http://127.0.0.1:8081/order;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /order/login {
        proxy_pass http://127.0.0.1:8081/order/login;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /order/progress {
        proxy_pass http://127.0.0.1:8081/order/progress;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /order/logout {
        proxy_pass http://127.0.0.1:8081/order/logout;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /api/domain/check {
        proxy_pass http://127.0.0.1:8081/api/domain/check;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /api/domain/search {
        proxy_pass http://127.0.0.1:8081/api/domain/search;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = / {
        proxy_pass http://127.0.0.1:8080/index.html;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location ~ ^/[A-Za-z0-9][A-Za-z0-9-]*\.html$ {
        proxy_pass http://127.0.0.1:8080;
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
NGINX
adduser nginx mail 2>/dev/null || true
nginx -t && rc-service nginx restart
EOF

# ── Restart service ──────────────────────────────────────────────────────────
echo ">> Restarting service..."
ssh $PI_USER@$PI_HOST \
    'rm -f /run/mail-ingest.pid && \
     rc-service mail-ingest zap && \
     rc-service mail-ingest restart && \
     rc-service mail-ingest status && \
     rm -f /run/contact-service.pid && \
     rc-service contact-service zap && \
     rc-service contact-service restart && \
     rc-service contact-service status'

# ── Commit to SD ─────────────────────────────────────────────────────────────
echo ">> Committing to SD card..."
ssh $PI_USER@$PI_HOST \
	 'lbu add /usr/local/bin && \
     lbu add /usr/local/sbin/localbox-enable-mail-client-access && \
     lbu add /usr/local/sbin/localbox-enable-tailscale-persistence && \
     lbu add /usr/local/sbin/localbox-create-backup && \
     lbu add /usr/local/sbin/localbox-provision-template && \
     lbu add /usr/local/sbin/localbox-add-tunnel-route && \
     lbu add /usr/local/sbin/localbox-add-email-routes && \
     lbu add /usr/local/sbin/localbox-firstboot && \
     lbu add /etc/localbox/examples && \
	 lbu add /etc/init.d/contact-service && \
	 lbu add /etc/group && \
     [ ! -f /etc/network/interfaces ] || lbu add /etc/network/interfaces && \
     [ ! -e /etc/runlevels/default/networking ] || lbu add /etc/runlevels/default/networking && \
	 lbu add /etc/nginx/http.d && \
	 lbu add /etc/localbox/templates && \
	 lbu add /etc/localbox/domains.conf && \
     lbu add /etc/localbox/admins.conf && \
     lbu add /etc/localbox/box-admins.conf && \
     [ ! -f /etc/localbox/smtp.conf ] || lbu add /etc/localbox/smtp.conf && \
     [ ! -f /etc/localbox/domainscoza.conf ] || lbu add /etc/localbox/domainscoza.conf && \
     [ ! -f /etc/localbox/cloudflare.conf ] || lbu add /etc/localbox/cloudflare.conf && \
     [ ! -f /etc/localbox/cloudflare-tunnel.conf ] || lbu add /etc/localbox/cloudflare-tunnel.conf && \
     [ ! -f /etc/localbox/paystack.conf ] || lbu add /etc/localbox/paystack.conf && \
     [ ! -f /etc/localbox/password-reset-tokens.conf ] || lbu add /etc/localbox/password-reset-tokens.conf && \
     [ ! -f /var/log/mail-ingest.log ] || lbu add /var/log/mail-ingest.log && \
     lbu add /var/log/contact-service.log && \
     lbu add /var/localbox/contact.log && \
     lbu add /var/localbox/send.log && \
     lbu add /var/localbox/security.log && \
     lbu commit -d'

echo ""
echo "=== Deploy complete ==="
