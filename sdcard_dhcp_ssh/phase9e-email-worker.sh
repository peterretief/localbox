#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
INVENTORY=${INVENTORY:-$SCRIPT_DIR/localbox-inventory.sh}
CLOUDFLARE_CONFIG=${CLOUDFLARE_CONFIG:-$PROJECT_DIR/cloudflare.conf}
WORKER_SOURCE=${WORKER_SOURCE:-$PROJECT_DIR/workers/localbox-email-worker.mjs}

PI_HOST=${PI_HOST:-192.168.0.7}
PI_USER=${PI_USER:-root}
DEVICE_NAME=${DEVICE_NAME:-localbox-001}
LOCALBOX_DOMAIN=${LOCALBOX_DOMAIN:-localboxtest.co.za}
LOCALBOX_EMAIL_WORKER=${LOCALBOX_EMAIL_WORKER:-}
LOCALBOX_INGEST_URL=${LOCALBOX_INGEST_URL:-https://www.$LOCALBOX_DOMAIN/ingest}
LOCALBOX_INGEST_SECRET=${LOCALBOX_INGEST_SECRET:-}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/localbox_phase9e_known_hosts -o ConnectTimeout=10"}

echo "Phase 9e target: Cloudflare Email Routing"
echo "Goal: create/update per-device email Worker and domain catch-all"
echo "Device:  $DEVICE_NAME"
echo "Domain:  $LOCALBOX_DOMAIN"
echo "Ingest:  $LOCALBOX_INGEST_URL"
echo "Config:  $CLOUDFLARE_CONFIG"

for tool in curl jq; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        echo "ERROR: $tool is required" >&2
        exit 1
    fi
done
[ -x "$INVENTORY" ] || {
    echo "ERROR: inventory tool not found: $INVENTORY" >&2
    exit 1
}
[ -f "$CLOUDFLARE_CONFIG" ] || {
    echo "ERROR: Cloudflare config not found: $CLOUDFLARE_CONFIG" >&2
    exit 1
}
[ -f "$WORKER_SOURCE" ] || {
    echo "ERROR: worker source not found: $WORKER_SOURCE" >&2
    exit 1
}

"$INVENTORY" validate-device "$DEVICE_NAME" >/dev/null
if [ -z "$LOCALBOX_EMAIL_WORKER" ]; then
    LOCALBOX_EMAIL_WORKER=$("$INVENTORY" device-field "$DEVICE_NAME" expected_worker)
fi
[ -n "$LOCALBOX_EMAIL_WORKER" ] || {
    echo "ERROR: LOCALBOX_EMAIL_WORKER is required and expected_worker is missing from inventory." >&2
    exit 1
}

domain_owner=$("$INVENTORY" domain-owner "$LOCALBOX_DOMAIN")
if [ -n "$domain_owner" ] && [ "$domain_owner" != "$DEVICE_NAME" ]; then
    echo "ERROR: domain $LOCALBOX_DOMAIN belongs to $domain_owner, not $DEVICE_NAME" >&2
    exit 1
fi

if [ -z "$LOCALBOX_INGEST_SECRET" ]; then
    echo ">> Reading ingest secret from Pi..."
    LOCALBOX_INGEST_SECRET=$(ssh $SSH_OPTS "$PI_USER@$PI_HOST" 'cat /etc/localbox/secret')
fi
[ -n "$LOCALBOX_INGEST_SECRET" ] || {
    echo "ERROR: ingest secret is empty" >&2
    exit 1
}

# shellcheck disable=SC1090
. "$CLOUDFLARE_CONFIG"

API_URL=${api_url:-${url:-https://api.cloudflare.com/client/v4}}
API_URL=$(printf '%s' "$API_URL" | sed 's:/*$::')
ACCOUNT_ID=${account_id:-${account:-}}
API_TOKEN=${api_token:-${token:-${api_key:-${apikey:-${key:-}}}}}
ACCOUNT_ID=${CLOUDFLARE_ACCOUNT_ID:-$ACCOUNT_ID}
API_TOKEN=${CLOUDFLARE_API_TOKEN:-$API_TOKEN}
EMAIL_ROUTING_API_TOKEN=${CLOUDFLARE_EMAIL_ROUTING_API_TOKEN:-${email_routing_api_token:-$API_TOKEN}}
ZONE_ID=${CLOUDFLARE_ZONE_ID:-}

if [ "${enabled:-false}" != "true" ] && [ "${enabled:-false}" != "1" ] && [ "${enabled:-false}" != "yes" ]; then
    echo "ERROR: Cloudflare integration is disabled in $CLOUDFLARE_CONFIG" >&2
    exit 1
fi
[ -n "$ACCOUNT_ID" ] && [ -n "$API_TOKEN" ] || {
    echo "ERROR: account_id and api_token are required in $CLOUDFLARE_CONFIG" >&2
    exit 1
}

api() {
    method=$1
    path=$2
    data=${3:-}
    tmp_body=${TMPDIR:-/tmp}/localbox-cf-api.$$
    trap 'rm -f "$tmp_body"' EXIT HUP INT TERM
    : > "$tmp_body"
    if [ -n "$data" ]; then
        if ! curl -fsS -o "$tmp_body" -X "$method" "$API_URL$path" \
            -H "Authorization: Bearer $API_TOKEN" \
            -H "Content-Type: application/json" \
            -H "Accept: application/json" \
            --data-binary "$data"; then
            echo "ERROR: Cloudflare API failed: $method $path" >&2
            cat "$tmp_body" >&2 || true
            exit 1
        fi
    else
        if ! curl -fsS -o "$tmp_body" -X "$method" "$API_URL$path" \
            -H "Authorization: Bearer $API_TOKEN" \
            -H "Accept: application/json"; then
            echo "ERROR: Cloudflare API failed: $method $path" >&2
            cat "$tmp_body" >&2 || true
            exit 1
        fi
    fi
    cat "$tmp_body"
}

email_api() {
    method=$1
    path=$2
    data=${3:-}
    tmp_body=${TMPDIR:-/tmp}/localbox-cf-email-api.$$
    trap 'rm -f "$tmp_body"' EXIT HUP INT TERM
    : > "$tmp_body"
    if [ -n "$data" ]; then
        if ! curl -fsS -o "$tmp_body" -X "$method" "$API_URL$path" \
            -H "Authorization: Bearer $EMAIL_ROUTING_API_TOKEN" \
            -H "Content-Type: application/json" \
            -H "Accept: application/json" \
            --data-binary "$data"; then
            echo "ERROR: Cloudflare Email Routing API failed: $method $path" >&2
            cat "$tmp_body" >&2 || true
            echo >&2
            echo "The token used for this call must have Zone > Email Routing Rules > Edit for $LOCALBOX_DOMAIN." >&2
            echo "Set email_routing_api_token=... in $CLOUDFLARE_CONFIG if using a separate token." >&2
            exit 1
        fi
    else
        if ! curl -fsS -o "$tmp_body" -X "$method" "$API_URL$path" \
            -H "Authorization: Bearer $EMAIL_ROUTING_API_TOKEN" \
            -H "Accept: application/json"; then
            echo "ERROR: Cloudflare Email Routing API failed: $method $path" >&2
            cat "$tmp_body" >&2 || true
            echo >&2
            echo "The token used for this call must have Zone > Email Routing Rules > Read/Edit for $LOCALBOX_DOMAIN." >&2
            echo "Set email_routing_api_token=... in $CLOUDFLARE_CONFIG if using a separate token." >&2
            exit 1
        fi
    fi
    cat "$tmp_body"
}

echo ">> Finding zone..."
if [ -z "$ZONE_ID" ]; then
    zone_json=$(api GET "/zones?name=$LOCALBOX_DOMAIN&account.id=$ACCOUNT_ID")
    ZONE_ID=$(printf '%s' "$zone_json" | jq -r '.result[0].id // empty')
    ZONE_STATUS=$(printf '%s' "$zone_json" | jq -r '.result[0].status // empty')
else
    zone_json=$(api GET "/zones/$ZONE_ID")
    zone_name=$(printf '%s' "$zone_json" | jq -r '.result.name // empty')
    ZONE_STATUS=$(printf '%s' "$zone_json" | jq -r '.result.status // empty')
    [ "$zone_name" = "$LOCALBOX_DOMAIN" ] || {
        echo "ERROR: CLOUDFLARE_ZONE_ID does not belong to $LOCALBOX_DOMAIN" >&2
        exit 1
    }
fi
[ -n "$ZONE_ID" ] || {
    echo "ERROR: Cloudflare zone not found for $LOCALBOX_DOMAIN" >&2
    exit 1
}
echo "   $LOCALBOX_DOMAIN ($ZONE_STATUS)"

echo ">> Uploading Worker $LOCALBOX_EMAIL_WORKER..."
metadata=$(jq -nc \
    --arg main "localbox-email-worker.mjs" \
    --arg ingest_url "$LOCALBOX_INGEST_URL" \
    --arg ingest_secret "$LOCALBOX_INGEST_SECRET" \
    --arg route_map "{}" \
    '{
      main_module: $main,
      bindings: [
        {type:"plain_text", name:"LOCALBOX_INGEST_URL", text:$ingest_url},
        {type:"plain_text", name:"LOCALBOX_ROUTE_MAP", text:$route_map},
        {type:"secret_text", name:"LOCALBOX_INGEST_SECRET", text:$ingest_secret}
      ]
    }')
curl -fsS -X PUT "$API_URL/accounts/$ACCOUNT_ID/workers/scripts/$LOCALBOX_EMAIL_WORKER" \
    -H "Authorization: Bearer $API_TOKEN" \
    -F "metadata=$metadata;type=application/json" \
    -F "localbox-email-worker.mjs=@$WORKER_SOURCE;type=application/javascript+module" >/dev/null

echo ">> Enabling Email Routing DNS..."
api POST "/zones/$ZONE_ID/email/routing/dns" '{}' >/dev/null || true

echo ">> Ensuring catch-all routes to Worker..."
catchall_payload=$(jq -nc --arg worker "$LOCALBOX_EMAIL_WORKER" '{
    name: "LocalBox catch-all",
    enabled: true,
    matchers: [{type:"all"}],
    actions: [{type:"worker", value:[$worker]}]
}')
email_api PUT "/zones/$ZONE_ID/email/routing/rules/catch_all" "$catchall_payload" >/dev/null

if [ -x "$INVENTORY" ]; then
    "$INVENTORY" upsert-device "$DEVICE_NAME" expected_worker="$LOCALBOX_EMAIL_WORKER" status=active notes=phase9e
    "$INVENTORY" upsert-domain "$LOCALBOX_DOMAIN" device_name="$DEVICE_NAME" zone_id="$ZONE_ID" zone_status="$ZONE_STATUS" status=published
    "$INVENTORY" phase "$DEVICE_NAME" phase9e-email-worker complete detail="$LOCALBOX_EMAIL_WORKER catch-all $LOCALBOX_DOMAIN"
fi

echo ">> Phase 9e complete."
echo "Worker:    $LOCALBOX_EMAIL_WORKER"
echo "Catch-all: *@$LOCALBOX_DOMAIN -> $LOCALBOX_EMAIL_WORKER"
