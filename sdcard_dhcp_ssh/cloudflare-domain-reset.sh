#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
INVENTORY=${INVENTORY:-$SCRIPT_DIR/localbox-inventory.sh}
CLOUDFLARE_CONFIG=${CLOUDFLARE_CONFIG:-$PROJECT_DIR/cloudflare.conf}

ACTION=${1:-plan}
LOCALBOX_DOMAIN=${LOCALBOX_DOMAIN:-${DOMAIN:-}}
DELETE_DNS_RECORDS=${DELETE_DNS_RECORDS:-true}
RESET_EMAIL_ROUTING=${RESET_EMAIL_ROUTING:-true}

usage() {
    cat <<EOF
Usage:
  LOCALBOX_DOMAIN=gooffoody.co.za $0 plan
  LOCALBOX_DOMAIN=gooffoody.co.za CONFIRM_RESET_DOMAIN=gooffoody.co.za $0 apply

Plans or applies a Cloudflare cleanup for a domain before reusing it.

Default cleanup:
  - lists/deletes all editable DNS records in the zone
  - disables Cloudflare Email Routing catch-all
  - records the reset in the local inventory

It does not delete Cloudflare tunnels, Workers, the Cloudflare zone, registrar
settings, or nameservers.

Options:
  DELETE_DNS_RECORDS=false
  RESET_EMAIL_ROUTING=false
EOF
}

die() {
    echo "ERROR: $*" >&2
    exit 1
}

case "$ACTION" in
    plan|apply) ;;
    -h|--help|help)
        usage
        exit 0
        ;;
    *)
        usage >&2
        die "unknown action: $ACTION"
        ;;
esac

[ -n "$LOCALBOX_DOMAIN" ] || die "LOCALBOX_DOMAIN is required"
[ -f "$CLOUDFLARE_CONFIG" ] || die "Cloudflare config not found: $CLOUDFLARE_CONFIG"
for tool in curl jq; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
done

normalize_domain() {
    printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed 's#^https://##; s#^http://##; s#/.*##; s/[.]$//'
}

LOCALBOX_DOMAIN=$(normalize_domain "$LOCALBOX_DOMAIN")
case "$LOCALBOX_DOMAIN" in
    ""|.*|*..*|*_*|*-.*|*.-*|*[!a-z0-9.-]*)
        die "invalid domain: $LOCALBOX_DOMAIN"
        ;;
esac

if [ "$ACTION" = "apply" ] && [ "${CONFIRM_RESET_DOMAIN:-}" != "$LOCALBOX_DOMAIN" ]; then
    die "set CONFIRM_RESET_DOMAIN=$LOCALBOX_DOMAIN to apply this reset"
fi

# shellcheck disable=SC1090
. "$CLOUDFLARE_CONFIG"

API_URL=${api_url:-${url:-https://api.cloudflare.com/client/v4}}
API_URL=$(printf '%s' "$API_URL" | sed 's:/*$::')
ACCOUNT_ID=${account_id:-${account:-}}
API_TOKEN=${api_token:-${token:-${api_key:-${apikey:-${key:-}}}}}
ACCOUNT_ID=${CLOUDFLARE_ACCOUNT_ID:-$ACCOUNT_ID}
API_TOKEN=${CLOUDFLARE_API_TOKEN:-$API_TOKEN}
ZONE_ID=${CLOUDFLARE_ZONE_ID:-}

if [ "${enabled:-false}" != "true" ] && [ "${enabled:-false}" != "1" ] && [ "${enabled:-false}" != "yes" ]; then
    die "Cloudflare integration is disabled in $CLOUDFLARE_CONFIG"
fi
[ -n "$API_TOKEN" ] || die "api_token is required in $CLOUDFLARE_CONFIG"

api() {
    method=$1
    path=$2
    data=${3:-}
    if [ -n "$data" ]; then
        curl -fsS -X "$method" "$API_URL$path" \
            -H "Authorization: Bearer $API_TOKEN" \
            -H "Content-Type: application/json" \
            -H "Accept: application/json" \
            --data-binary "$data"
    else
        curl -fsS -X "$method" "$API_URL$path" \
            -H "Authorization: Bearer $API_TOKEN" \
            -H "Accept: application/json"
    fi
}

echo "Cloudflare domain reset"
echo "Action: $ACTION"
echo "Domain: $LOCALBOX_DOMAIN"
echo "Config: $CLOUDFLARE_CONFIG"
echo "Tunnels/workers/zones: preserved"

echo ">> Finding zone..."
if [ -z "$ZONE_ID" ]; then
    if [ -n "$ACCOUNT_ID" ]; then
        zone_json=$(api GET "/zones?name=$LOCALBOX_DOMAIN&account.id=$ACCOUNT_ID")
    else
        zone_json=$(api GET "/zones?name=$LOCALBOX_DOMAIN")
    fi
    ZONE_ID=$(printf '%s' "$zone_json" | jq -r '.result[0].id // empty')
    ZONE_STATUS=$(printf '%s' "$zone_json" | jq -r '.result[0].status // empty')
else
    zone_json=$(api GET "/zones/$ZONE_ID")
    zone_name=$(printf '%s' "$zone_json" | jq -r '.result.name // empty')
    ZONE_STATUS=$(printf '%s' "$zone_json" | jq -r '.result.status // empty')
    [ "$zone_name" = "$LOCALBOX_DOMAIN" ] || die "CLOUDFLARE_ZONE_ID does not belong to $LOCALBOX_DOMAIN"
fi
[ -n "$ZONE_ID" ] || die "Cloudflare zone not found for $LOCALBOX_DOMAIN"
echo "   $LOCALBOX_DOMAIN ($ZONE_STATUS)"

dns_json=$(api GET "/zones/$ZONE_ID/dns_records?per_page=500")
dns_count=$(printf '%s' "$dns_json" | jq '.result | length')
echo ">> DNS records: $dns_count"
printf '%s\n' "$dns_json" | jq -r '.result[]? | "   \(.type)\t\(.name)\t\(.content)\tproxied=\(.proxied // false)"'

if [ "$ACTION" = "apply" ] && [ "$DELETE_DNS_RECORDS" = "true" ]; then
    echo ">> Deleting DNS records..."
    printf '%s\n' "$dns_json" | jq -r '.result[]? | .id + "\t" + .type + "\t" + .name' |
    while IFS="$(printf '\t')" read -r record_id record_type record_name; do
        [ -n "$record_id" ] || continue
        api DELETE "/zones/$ZONE_ID/dns_records/$record_id" >/dev/null
        echo "   deleted $record_type $record_name"
    done
elif [ "$DELETE_DNS_RECORDS" = "true" ]; then
    echo ">> Plan: would delete all listed DNS records."
else
    echo ">> DNS deletion disabled."
fi

if [ "$RESET_EMAIL_ROUTING" = "true" ]; then
    echo ">> Cloudflare Email Routing catch-all..."
    catchall_json=$(api GET "/zones/$ZONE_ID/email/routing/rules/catch_all" 2>/tmp/localbox-email-routing.err || true)
    if [ -n "$catchall_json" ] && printf '%s' "$catchall_json" | jq -e '.success == true' >/dev/null 2>&1; then
        current_enabled=$(printf '%s' "$catchall_json" | jq -r '.result.enabled // false')
        current_actions=$(printf '%s' "$catchall_json" | jq -c '.result.actions // []')
        echo "   current enabled=$current_enabled actions=$current_actions"
        if [ "$ACTION" = "apply" ]; then
            payload=$(printf '%s' "$catchall_json" | jq -c '.result | .enabled = false')
            api PUT "/zones/$ZONE_ID/email/routing/rules/catch_all" "$payload" >/dev/null
            echo "   disabled catch-all"
        else
            echo "   Plan: would disable catch-all."
        fi
    else
        echo "   no readable catch-all, or Email Routing is not enabled"
        cat /tmp/localbox-email-routing.err 2>/dev/null || true
    fi
    rm -f /tmp/localbox-email-routing.err
else
    echo ">> Email Routing reset disabled."
fi

if [ "$ACTION" = "apply" ] && [ -x "$INVENTORY" ]; then
    "$INVENTORY" upsert-domain "$LOCALBOX_DOMAIN" zone_id="$ZONE_ID" zone_status="$ZONE_STATUS" status=reset notes=cloudflare-domain-reset
    "$INVENTORY" phase "" cloudflare-domain-reset complete detail="$LOCALBOX_DOMAIN"
fi

echo ">> Done."
