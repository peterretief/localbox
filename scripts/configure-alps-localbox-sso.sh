#!/bin/sh
set -eu

ALPS_DIR=${ALPS_DIR:-/mnt/data/alps-test}
ALPS_CONFIG=${ALPS_CONFIG:-$ALPS_DIR/config.toml}
DOVECOT_CONF=${DOVECOT_CONF:-/etc/dovecot/dovecot.conf}
SSO_SECRET_FILE=${SSO_SECRET_FILE:-/etc/localbox/sso-secret}
MASTER_USERNAME=${MASTER_USERNAME:-localbox-alps}
MASTER_SEPARATOR=${MASTER_SEPARATOR:-*}

random_secret() {
    if command -v openssl >/dev/null 2>&1; then
        openssl rand -base64 32
    else
        dd if=/dev/urandom bs=32 count=1 2>/dev/null | base64
    fi
}

if [ ! -f "$ALPS_CONFIG" ]; then
    echo "ERROR: ALPS config not found: $ALPS_CONFIG" >&2
    exit 1
fi
if [ ! -f "$DOVECOT_CONF" ]; then
    echo "ERROR: Dovecot config not found: $DOVECOT_CONF" >&2
    exit 1
fi

mkdir -p "$(dirname "$SSO_SECRET_FILE")"
if [ ! -s "$SSO_SECRET_FILE" ]; then
    random_secret > "$SSO_SECRET_FILE"
fi
chown www:mail "$SSO_SECRET_FILE" 2>/dev/null || chown www "$SSO_SECRET_FILE"
chmod 0640 "$SSO_SECRET_FILE"

MASTER_PASSWORD_FILE=${MASTER_PASSWORD_FILE:-/etc/localbox/alps-master-password}
if [ ! -s "$MASTER_PASSWORD_FILE" ]; then
    random_secret > "$MASTER_PASSWORD_FILE"
fi
chown root:root "$MASTER_PASSWORD_FILE"
chmod 0600 "$MASTER_PASSWORD_FILE"
MASTER_PASSWORD=$(cat "$MASTER_PASSWORD_FILE")

timestamp=$(date +%Y%m%d%H%M%S)
cp -p "$DOVECOT_CONF" "$DOVECOT_CONF.bak-alps-sso-$timestamp"
tmp_dovecot=$(mktemp)
awk -v user="$MASTER_USERNAME" -v pass="$MASTER_PASSWORD" -v sep="$MASTER_SEPARATOR" '
    /^# LocalBox ALPS SSO master user$/ { skip = 1; next }
    /^# End LocalBox ALPS SSO master user$/ { skip = 0; next }
    skip { next }
    /^auth_master_user_separator[[:space:]]*=/ { next }
    /^passdb passwd-file[[:space:]]*\{/ && !inserted {
        print "auth_master_user_separator = " sep
        print "# LocalBox ALPS SSO master user"
        print "passdb static {"
        print "  master = yes"
        print "  username_filter = " user
        print "  password = " pass
        print "}"
        print "# End LocalBox ALPS SSO master user"
        inserted = 1
    }
    { print }
    END {
        if (!inserted) {
            print "auth_master_user_separator = " sep
            print "# LocalBox ALPS SSO master user"
            print "passdb static {"
            print "  master = yes"
            print "  username_filter = " user
            print "  password = " pass
            print "}"
            print "# End LocalBox ALPS SSO master user"
        }
    }
' "$DOVECOT_CONF" > "$tmp_dovecot"

dovecot -c "$tmp_dovecot" -n >/dev/null
cat "$tmp_dovecot" > "$DOVECOT_CONF"
rm -f "$tmp_dovecot"

cp -p "$ALPS_CONFIG" "$ALPS_CONFIG.bak-localbox-sso-$timestamp"
tmp_alps=$(mktemp)
awk '
    /^\[localbox\.sso\]/ { skip = 1; next }
    /^\[/ && skip { skip = 0 }
    !skip { print }
' "$ALPS_CONFIG" > "$tmp_alps"
cat >> "$tmp_alps" << EOF

[localbox.sso]
enabled = true
secret_file = "$SSO_SECRET_FILE"
master_username = "$MASTER_USERNAME"
master_password = "$MASTER_PASSWORD"
master_separator = "$MASTER_SEPARATOR"
redirect_url = "/"
EOF
cat "$tmp_alps" > "$ALPS_CONFIG"
rm -f "$tmp_alps"

rc-service dovecot restart
rc-service alps restart

lbu add "$SSO_SECRET_FILE" "$MASTER_PASSWORD_FILE" "$DOVECOT_CONF" "$ALPS_CONFIG" 2>/dev/null || true
lbu commit -d 2>/dev/null || true

echo "Configured ALPS LocalBox SSO with Dovecot master user $MASTER_USERNAME."
