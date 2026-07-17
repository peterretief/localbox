#!/bin/sh
set -eu

ALPS_HOST=${ALPS_HOST:-mail.opamp.co.za}
ALPS_ORIGIN=${ALPS_ORIGIN:-http://100.64.0.8:1323}
NGINX_CONF=${NGINX_CONF:-/etc/nginx/http.d/alps-webmail.conf}

cat > "$NGINX_CONF" << EOF
server {
    listen 80;
    server_name $ALPS_HOST;

    client_max_body_size 25m;

    location = /password/change {
        default_type application/json;
        add_header Cache-Control "no-store";
        return 403 '{"error":"Password changes are managed in the LocalBox admin portal. Please ask an administrator to change your mailbox password there."}';
    }

    location / {
        proxy_pass $ALPS_ORIGIN;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Forwarded-Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header X-Forwarded-Ssl on;
        proxy_set_header Origin http://\$host;
        proxy_set_header Referer http://\$host/;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
EOF

nginx -t
rc-service nginx restart

lbu add "$NGINX_CONF" 2>/dev/null || true
lbu commit -d 2>/dev/null || true

echo "Configured nginx proxy for $ALPS_HOST -> $ALPS_ORIGIN"
