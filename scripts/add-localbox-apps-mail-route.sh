#!/bin/sh
set -eu

NGINX_CONF=${NGINX_CONF:-/etc/nginx/http.d/localbox-sites.conf}

if grep -q 'location = /apps/mail' "$NGINX_CONF" && grep -q 'location /apps/files/' "$NGINX_CONF" && grep -q 'location = /files' "$NGINX_CONF"; then
    echo "LocalBox app nginx routes already exist."
    exit 0
fi

cp -p "$NGINX_CONF" "$NGINX_CONF.bak-apps-mail-$(date +%Y%m%d%H%M%S)"
tmp=$(mktemp)
awk '
    /^    location = \/apps\/mail \{/ && !apps_files_done && !has_apps_files {
        if (!has_apps_files) {
            print "    location = /apps/files {"
            print "        proxy_pass http://127.0.0.1:8080/apps/files;"
            print "        proxy_set_header Host $host;"
            print "        proxy_set_header X-Forwarded-Host $host;"
            print "        proxy_set_header X-Real-IP $remote_addr;"
            print "        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;"
            print "        proxy_set_header X-Forwarded-Proto $scheme;"
            print "    }"
            print ""
            print "    location /apps/files/ {"
            print "        proxy_pass http://127.0.0.1:8080/apps/files/;"
            print "        proxy_set_header Host $host;"
            print "        proxy_set_header X-Forwarded-Host $host;"
            print "        proxy_set_header X-Real-IP $remote_addr;"
            print "        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;"
            print "        proxy_set_header X-Forwarded-Proto $scheme;"
            print "    }"
            print ""
        }
        apps_files_done = 1
    }
    /^    location = \/apps\/mail \{/ && !files_done && !has_files {
        print "    location = /files {"
        print "        proxy_pass http://127.0.0.1:8080/files;"
        print "        proxy_set_header Host $host;"
        print "        proxy_set_header X-Forwarded-Host $host;"
        print "        proxy_set_header X-Real-IP $remote_addr;"
        print "        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;"
        print "        proxy_set_header X-Forwarded-Proto $scheme;"
        print "    }"
        print ""
        print "    location = /files/download {"
        print "        proxy_pass http://127.0.0.1:8080/files/download;"
        print "        proxy_set_header Host $host;"
        print "        proxy_set_header X-Forwarded-Host $host;"
        print "        proxy_set_header X-Real-IP $remote_addr;"
        print "        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;"
        print "        proxy_set_header X-Forwarded-Proto $scheme;"
        print "    }"
        print ""
        print "    location = /files/delete {"
        print "        proxy_pass http://127.0.0.1:8080/files/delete;"
        print "        proxy_set_header Host $host;"
        print "        proxy_set_header X-Forwarded-Host $host;"
        print "        proxy_set_header X-Real-IP $remote_addr;"
        print "        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;"
        print "        proxy_set_header X-Forwarded-Proto $scheme;"
        print "    }"
        print ""
        files_done = 1
    }
    /location = \/files/ { has_files = 1 }
    /location \/apps\/files\// { has_apps_files = 1 }
    /^    location = \/mail \{/ && !done {
        if (!has_apps_mail) {
            print "    location = /apps/mail {"
            print "        proxy_pass http://127.0.0.1:8080/apps/mail;"
            print "        proxy_set_header Host $host;"
            print "        proxy_set_header X-Forwarded-Host $host;"
            print "        proxy_set_header X-Real-IP $remote_addr;"
            print "        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;"
            print "        proxy_set_header X-Forwarded-Proto $scheme;"
            print "    }"
            print ""
        }
        done = 1
    }
    /location = \/apps\/mail/ { has_apps_mail = 1 }
    { print }
' "$NGINX_CONF" > "$tmp"
cat "$tmp" > "$NGINX_CONF"
rm -f "$tmp"

nginx -t
rc-service nginx reload || rc-service nginx restart

lbu add "$NGINX_CONF" 2>/dev/null || true
lbu commit -d 2>/dev/null || true

echo "Added LocalBox app nginx routes."
