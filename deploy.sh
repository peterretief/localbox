#!/bin/sh
set -e

# ── Load settings ────────────────────────────────────────────────────────────
if [ -f "./settings.conf" ]; then
    . ./settings.conf
fi
PI_HOST=${PI_HOST:-192.168.0.5}
PI_USER=${PI_USER:-root}
SITE_DIR=${SITE_DIR:-/mnt/data/site/$MAIL_DOMAIN}
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

# ── Deploy contact service ───────────────────────────────────────────────────
echo ">> Deploying contact service..."
cat contact-service | ssh $PI_USER@$PI_HOST \
    'kill $(pgrep contact-service) 2>/dev/null; \
     rm -f /usr/local/bin/contact-service; \
     cat > /usr/local/bin/contact-service && \
     chmod +x /usr/local/bin/contact-service && \
     mkdir -p /var/localbox && \
     touch /var/localbox/contact.log && \
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
chown www:mail /var/log/contact-service.log /var/localbox/contact.log 2>/dev/null || true
chmod 664 /var/log/contact-service.log /var/localbox/contact.log
rc-update add contact-service default 2>/dev/null || true'
echo "   Contact service deployed"

# ── Deploy templates ─────────────────────────────────────────────────────────
echo ">> Deploying templates..."
for f in templates/*.html; do
    fname=$(basename "$f")
    cat "$f" | ssh $PI_USER@$PI_HOST "cat > /etc/localbox/templates/$fname"
    echo "   Deployed $fname"
done

echo ">> Deploying site pages..."
ssh $PI_USER@$PI_HOST "mkdir -p $SITE_DIR"
for f in templates/contact.html templates/thankyou.html; do
    fname=$(basename "$f")
    cat "$f" | ssh $PI_USER@$PI_HOST "cat > $SITE_DIR/$fname"
    echo "   Deployed $SITE_DIR/$fname"
done

# ── Configure nginx contact route ────────────────────────────────────────────
echo ">> Configuring nginx contact route..."
ssh $PI_USER@$PI_HOST "cat > /etc/nginx/http.d/localbox-sites.conf << EOF
server {
    listen 80;
    server_name rpi.diode.co.za;
    root $SITE_DIR;
    index index.html;

    location = /contact {
        proxy_pass http://127.0.0.1:8081/contact;
        proxy_set_header Host \\\$host;
        proxy_set_header X-Real-IP \\\$remote_addr;
        proxy_set_header X-Forwarded-For \\\$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \\\$scheme;
    }

    location / {
        try_files \\\$uri \\\$uri/ =404;
    }
}
EOF
nginx -t && rc-service nginx reload"

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
     lbu add /etc/init.d/contact-service && \
     lbu add /etc/nginx/http.d/localbox-sites.conf && \
     lbu add /var/log/contact-service.log && \
     lbu add /var/localbox/contact.log && \
     lbu commit -d'

echo ""
echo "=== Deploy complete ==="
