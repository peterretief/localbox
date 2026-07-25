#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
INVENTORY=${INVENTORY:-$SCRIPT_DIR/localbox-inventory.sh}
DEVICE_NAME=${DEVICE_NAME:-}

LOCALBOX_DOMAIN=${LOCALBOX_DOMAIN:-localboxtest.co.za}
LOCALBOX_PUBLIC_HOST=${LOCALBOX_PUBLIC_HOST:-mail.$LOCALBOX_DOMAIN}
LOCALBOX_TUNNEL=${LOCALBOX_TUNNEL:-}
LOCALBOX_TUNNEL_SERVICE=${LOCALBOX_TUNNEL_SERVICE:-}
CLOUDFLARE_CONFIG=${CLOUDFLARE_CONFIG:-$PROJECT_DIR/cloudflare.conf}

if [ -z "$LOCALBOX_TUNNEL" ]; then
    echo "ERROR: LOCALBOX_TUNNEL is required." >&2
    echo "Set it to the tunnel name/id that is actually running on this test box." >&2
    exit 1
fi
if [ -z "$DEVICE_NAME" ]; then
    echo "ERROR: DEVICE_NAME is required." >&2
    echo "Every Cloudflare tunnel must be recorded in the local inventory against the owning Pi before routes are published." >&2
    exit 1
fi
if [ ! -x "$INVENTORY" ]; then
    echo "ERROR: inventory tool not found: $INVENTORY" >&2
    exit 1
fi

if [ ! -f "$CLOUDFLARE_CONFIG" ]; then
    echo "ERROR: Cloudflare config not found: $CLOUDFLARE_CONFIG" >&2
    echo "Copy cloudflare.conf.example to cloudflare.conf and set enabled=true, account_id, api_token." >&2
    exit 1
fi
if ! command -v curl >/dev/null 2>&1; then
    echo "ERROR: curl is required" >&2
    exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
    echo "ERROR: jq is required" >&2
    exit 1
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
    echo "ERROR: Cloudflare integration is disabled in $CLOUDFLARE_CONFIG" >&2
    exit 1
fi
if [ -z "$ACCOUNT_ID" ] || [ -z "$API_TOKEN" ]; then
    echo "ERROR: account_id and api_token are required in $CLOUDFLARE_CONFIG" >&2
    exit 1
fi

normalize_domain() {
    printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed 's#^https://##; s#^http://##; s#/.*##; s/[.]$//'
}

LOCALBOX_DOMAIN=$(normalize_domain "$LOCALBOX_DOMAIN")
LOCALBOX_PUBLIC_HOST=$(normalize_domain "$LOCALBOX_PUBLIC_HOST")

if [ -z "$LOCALBOX_TUNNEL_SERVICE" ]; then
    case "$LOCALBOX_PUBLIC_HOST" in
        mail."$LOCALBOX_DOMAIN") LOCALBOX_TUNNEL_SERVICE=http://localhost:1323 ;;
        files."$LOCALBOX_DOMAIN") LOCALBOX_TUNNEL_SERVICE=http://localhost:8082 ;;
        "$LOCALBOX_DOMAIN"|www."$LOCALBOX_DOMAIN") LOCALBOX_TUNNEL_SERVICE=http://localhost:80 ;;
        *) LOCALBOX_TUNNEL_SERVICE=http://localhost:80 ;;
    esac
fi

echo "Phase 9 target: Cloudflare"
echo "Goal: publish LocalBox hostname through an existing Cloudflare Tunnel"
echo "Domain:   $LOCALBOX_DOMAIN"
echo "Hostname: $LOCALBOX_PUBLIC_HOST"
echo "Tunnel:   $LOCALBOX_TUNNEL"
echo "Service:  $LOCALBOX_TUNNEL_SERVICE"
echo "Config:   $CLOUDFLARE_CONFIG"

case "$LOCALBOX_DOMAIN" in
    ""|.*|*..*|*_*|*-.*|*.-*|*[!a-z0-9.-]*)
        echo "ERROR: invalid domain: $LOCALBOX_DOMAIN" >&2
        exit 1
        ;;
esac
case "$LOCALBOX_PUBLIC_HOST" in
    "$LOCALBOX_DOMAIN"|*."$LOCALBOX_DOMAIN") ;;
    *)
        echo "ERROR: hostname must be under $LOCALBOX_DOMAIN: $LOCALBOX_PUBLIC_HOST" >&2
        exit 1
        ;;
esac

domain_owner=$("$INVENTORY" domain-owner "$LOCALBOX_DOMAIN")
if [ -n "$domain_owner" ] && [ "$domain_owner" != "$DEVICE_NAME" ]; then
    echo "ERROR: domain $LOCALBOX_DOMAIN belongs to $domain_owner, not $DEVICE_NAME" >&2
    exit 1
fi
if [ -z "$ZONE_ID" ]; then
    ZONE_ID=$("$INVENTORY" domain-field "$LOCALBOX_DOMAIN" zone_id)
fi

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

echo ">> Finding zone..."
if [ -z "$ZONE_ID" ]; then
    zone_json=$(api GET "/zones?name=$LOCALBOX_DOMAIN")
    ZONE_ID=$(printf '%s' "$zone_json" | jq -r '.result[0].id // empty')
    ZONE_STATUS=$(printf '%s' "$zone_json" | jq -r '.result[0].status // empty')
else
    zone_json=$(api GET "/zones/$ZONE_ID")
    LOCALBOX_DOMAIN_API=$(printf '%s' "$zone_json" | jq -r '.result.name // empty')
    ZONE_STATUS=$(printf '%s' "$zone_json" | jq -r '.result.status // empty')
    if [ "$LOCALBOX_DOMAIN_API" != "$LOCALBOX_DOMAIN" ]; then
        echo "ERROR: CLOUDFLARE_ZONE_ID does not belong to $LOCALBOX_DOMAIN" >&2
        exit 1
    fi
fi
if [ -z "$ZONE_ID" ]; then
    echo "ERROR: Cloudflare zone not found for $LOCALBOX_DOMAIN" >&2
    echo "The configured API token may not have access to this zone. Use a token scoped to this zone, or set CLOUDFLARE_API_TOKEN/CLOUDFLARE_ZONE_ID." >&2
    exit 1
fi
echo "   $LOCALBOX_DOMAIN ($ZONE_STATUS)"

echo ">> Finding tunnel..."
tunnel_json=$(api GET "/accounts/$ACCOUNT_ID/cfd_tunnel?is_deleted=false&per_page=100")
TUNNEL_ID=$(printf '%s' "$tunnel_json" | jq -r --arg tunnel "$LOCALBOX_TUNNEL" '.result[]? | select(.id == $tunnel or .name == $tunnel) | .id' | head -1)
TUNNEL_NAME=$(printf '%s' "$tunnel_json" | jq -r --arg id "$TUNNEL_ID" '.result[]? | select(.id == $id) | .name' | head -1)
TUNNEL_STATUS=$(printf '%s' "$tunnel_json" | jq -r --arg id "$TUNNEL_ID" '.result[]? | select(.id == $id) | .status' | head -1)
if [ -z "$TUNNEL_ID" ]; then
    echo "ERROR: Cloudflare tunnel not found: $LOCALBOX_TUNNEL" >&2
    exit 1
fi
echo "   $TUNNEL_NAME ($TUNNEL_STATUS)"

inventory_owner=$("$INVENTORY" tunnel-owner "$TUNNEL_ID")
if [ -z "$inventory_owner" ]; then
    inventory_owner=$("$INVENTORY" tunnel-owner "$TUNNEL_NAME")
fi
if [ -z "$inventory_owner" ]; then
    echo "ERROR: tunnel is not recorded in local inventory: $TUNNEL_NAME / $TUNNEL_ID" >&2
    echo "Record it first:" >&2
    echo "  ./localbox-inventory.sh upsert-tunnel $TUNNEL_NAME tunnel_id=$TUNNEL_ID device_name=$DEVICE_NAME status=$TUNNEL_STATUS" >&2
    exit 1
fi
if [ "$inventory_owner" != "$DEVICE_NAME" ]; then
    echo "ERROR: tunnel $TUNNEL_NAME belongs to $inventory_owner, not $DEVICE_NAME" >&2
    exit 1
fi
"$INVENTORY" validate-publish "$DEVICE_NAME" "$LOCALBOX_DOMAIN" "$TUNNEL_ID" >/dev/null

TUNNEL_TARGET="$TUNNEL_ID.cfargotunnel.com"

echo ">> Ensuring DNS CNAME..."
records_json=$(api GET "/zones/$ZONE_ID/dns_records?name=$LOCALBOX_PUBLIC_HOST&per_page=20")
record_id=$(printf '%s' "$records_json" | jq -r '.result[]? | select(.type == "CNAME") | .id' | head -1)
record_payload=$(jq -nc \
    --arg name "$LOCALBOX_PUBLIC_HOST" \
    --arg content "$TUNNEL_TARGET" \
    '{type:"CNAME", name:$name, content:$content, proxied:true, ttl:1}')
if [ -n "$record_id" ]; then
    api PUT "/zones/$ZONE_ID/dns_records/$record_id" "$record_payload" >/dev/null
    echo "   updated $LOCALBOX_PUBLIC_HOST -> $TUNNEL_TARGET"
else
    api POST "/zones/$ZONE_ID/dns_records" "$record_payload" >/dev/null
    echo "   created $LOCALBOX_PUBLIC_HOST -> $TUNNEL_TARGET"
fi

echo ">> Ensuring tunnel ingress route..."
config_json=$(api GET "/accounts/$ACCOUNT_ID/cfd_tunnel/$TUNNEL_ID/configurations")
new_config=$(printf '%s' "$config_json" | jq \
    --arg hostname "$LOCALBOX_PUBLIC_HOST" \
    --arg service "$LOCALBOX_TUNNEL_SERVICE" '
    .result.config as $cfg
    | ($cfg.ingress // []) as $ingress
    | if any($ingress[]?; .hostname == $hostname) then
        $cfg | .ingress = [ $ingress[] | if .hostname == $hostname then (.service = $service | .originRequest = (.originRequest // {})) else . end ]
      else
        ($ingress | map(select(.hostname != null))) as $routes
        | ($ingress | map(select(.hostname == null)) | first // {service:"http_status:404", originRequest:{}}) as $catchall
        | $cfg | .ingress = ($routes + [{hostname:$hostname, service:$service, originRequest:{}}] + [$catchall])
      end')
api PUT "/accounts/$ACCOUNT_ID/cfd_tunnel/$TUNNEL_ID/configurations" "$(jq -nc --argjson config "$new_config" '{config:$config}')" >/dev/null
echo "   $LOCALBOX_PUBLIC_HOST -> $LOCALBOX_TUNNEL_SERVICE"

echo ">> Phase 9 complete."
echo "Test:"
echo "  curl -I https://$LOCALBOX_PUBLIC_HOST"

if [ -x "$INVENTORY" ]; then
    "$INVENTORY" upsert-device "$DEVICE_NAME" status=active notes=phase9
    "$INVENTORY" upsert-tunnel "$TUNNEL_NAME" tunnel_id="$TUNNEL_ID" device_name="$DEVICE_NAME" status="$TUNNEL_STATUS"
    "$INVENTORY" upsert-domain "$LOCALBOX_DOMAIN" device_name="$DEVICE_NAME" zone_id="$ZONE_ID" zone_status="$ZONE_STATUS" status=published
    "$INVENTORY" phase "$DEVICE_NAME" phase9-cloudflare-route complete detail="$LOCALBOX_PUBLIC_HOST $TUNNEL_NAME $LOCALBOX_TUNNEL_SERVICE"
    "$INVENTORY" upsert-route "$LOCALBOX_PUBLIC_HOST" domain="$LOCALBOX_DOMAIN" tunnel_name="$TUNNEL_NAME" tunnel_id="$TUNNEL_ID" service="$LOCALBOX_TUNNEL_SERVICE" status=published
fi
