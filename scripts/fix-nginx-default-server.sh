#!/bin/sh
set -eu

adduser nginx mail 2>/dev/null || true

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

    location = /public/logout {
        proxy_pass http://127.0.0.1:8080/public/logout;
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

    location / {
        try_files $uri $uri/ /index.html =404;
    }
}
NGINX

nginx -t
rc-service nginx restart
lbu add /etc/nginx/http.d/localbox-sites.conf
lbu add /etc/group
lbu commit -d
