#!/bin/sh
set -eu

CONFIG=${1:-}
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
CLOUDFLARE_CONFIG=${CLOUDFLARE_CONFIG:-"$PROJECT_DIR/cloudflare.conf"}
WORKER_SOURCE=${WORKER_SOURCE:-"$PROJECT_DIR/workers/localbox-email-worker.mjs"}
WORKER_DEPLOY_METHOD=${WORKER_DEPLOY_METHOD:-api}

if [ -z "$CONFIG" ] || [ ! -f "$CONFIG" ]; then
    echo "Usage: CLOUDFLARE_CONFIG=/path/cloudflare.conf $0 /path/domain.conf" >&2
    exit 1
fi
if [ ! -f "$CLOUDFLARE_CONFIG" ]; then
    echo "ERROR: Cloudflare config not found: $CLOUDFLARE_CONFIG" >&2
    exit 1
fi
if [ ! -f "$WORKER_SOURCE" ]; then
    echo "ERROR: worker source not found: $WORKER_SOURCE" >&2
    exit 1
fi
for tool in curl jq; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        echo "ERROR: $tool is required" >&2
        exit 1
    fi
done
if [ "$WORKER_DEPLOY_METHOD" = "wrangler" ] && ! command -v wrangler >/dev/null 2>&1 && ! command -v npx >/dev/null 2>&1; then
    echo "ERROR: WORKER_DEPLOY_METHOD=wrangler requires wrangler or npx on the dev machine" >&2
    exit 1
fi

# shellcheck disable=SC1090
. "$CLOUDFLARE_CONFIG"

API_URL=${api_url:-${url:-https://api.cloudflare.com/client/v4}}
API_URL=$(printf '%s' "$API_URL" | sed 's:/*$::')
ACCOUNT_ID=${account_id:-${account:-}}
API_TOKEN=${api_token:-${token:-${api_key:-${apikey:-${key:-}}}}}

if [ "${enabled:-false}" != "true" ] && [ "${enabled:-false}" != "1" ] && [ "${enabled:-false}" != "yes" ]; then
    echo "ERROR: Cloudflare integration is disabled in $CLOUDFLARE_CONFIG" >&2
    exit 1
fi
if [ -z "$ACCOUNT_ID" ] || [ -z "$API_TOKEN" ]; then
    echo "ERROR: account_id and api_token are required in $CLOUDFLARE_CONFIG" >&2
    exit 1
fi

DOMAIN=""
EMAIL_WORKER=""
INGEST_URL=""
INGEST_SECRET=""
BACKUP_INGEST_URL=""
BACKUP_INGEST_SECRET=""
INGEST_HOST=""
INGEST_CNAME_TARGET=""
EMAIL_ROUTES=""

normalize_email() {
    printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]'
}

while IFS= read -r line || [ -n "$line" ]; do
    line=${line%%#*}
    line=$(printf '%s' "$line" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    [ -n "$line" ] || continue
    key=${line%%=*}
    value=${line#*=}
    key=$(printf '%s' "$key" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')
    value=$(printf '%s' "$value" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    case "$key" in
        domain)
            [ -n "$DOMAIN" ] || DOMAIN=$(normalize_email "$value")
            ;;
        email_worker)
            EMAIL_WORKER=$value
            ;;
        email_ingest_url|ingest_url)
            INGEST_URL=$value
            ;;
        ingest_host|email_ingest_host)
            INGEST_HOST=$(normalize_email "$value")
            ;;
        ingest_cname_target|email_ingest_cname_target)
            INGEST_CNAME_TARGET=$(normalize_email "$value")
            ;;
        ingest_tunnel_id|email_ingest_tunnel_id)
            INGEST_CNAME_TARGET="$(normalize_email "$value").cfargotunnel.com"
            ;;
        email_ingest_secret|ingest_secret)
            INGEST_SECRET=$value
            ;;
        backup_ingest_url|email_backup_ingest_url)
            BACKUP_INGEST_URL=$value
            ;;
        backup_ingest_secret|email_backup_ingest_secret)
            BACKUP_INGEST_SECRET=$value
            ;;
        email_route)
            EMAIL_ROUTES="${EMAIL_ROUTES}${EMAIL_ROUTES:+
}$(normalize_email "$value")"
            ;;
    esac
done < "$CONFIG"

EMAIL_WORKER=${EMAIL_WORKER:-alpine}
if [ -z "$INGEST_URL" ] && [ -n "$INGEST_HOST" ]; then
    INGEST_URL="https://$INGEST_HOST/ingest"
fi
INGEST_URL=${INGEST_URL:-${LOCALBOX_INGEST_URL:-}}
INGEST_SECRET=${INGEST_SECRET:-${LOCALBOX_INGEST_SECRET:-}}

if [ -z "$DOMAIN" ]; then
    echo "ERROR: domain= is required in $CONFIG" >&2
    exit 1
fi
if [ -z "$EMAIL_ROUTES" ]; then
    echo "ERROR: at least one email_route= is required in $CONFIG" >&2
    exit 1
fi
if [ -z "$INGEST_URL" ]; then
    echo "ERROR: email_ingest_url= or LOCALBOX_INGEST_URL is required" >&2
    exit 1
fi
if [ -z "$INGEST_SECRET" ]; then
    echo "ERROR: email_ingest_secret= or LOCALBOX_INGEST_SECRET is required" >&2
    exit 1
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

find_zone_for_name() {
    name=$1
    candidate=$name
    while :; do
        zone_id=$(api GET "/zones?name=$candidate&account.id=$ACCOUNT_ID" | jq -r '.result[0].id // empty')
        if [ -n "$zone_id" ]; then
            printf '%s %s\n' "$zone_id" "$candidate"
            return 0
        fi
        case "$candidate" in
            *.*) candidate=${candidate#*.} ;;
            *) return 1 ;;
        esac
    done
}

if [ -n "$INGEST_HOST" ]; then
    if [ -z "$INGEST_CNAME_TARGET" ]; then
        echo "ERROR: ingest_host requires ingest_tunnel_id= or ingest_cname_target= so DNS can be created" >&2
        exit 1
    fi
    set -- $(find_zone_for_name "$INGEST_HOST" || true)
    INGEST_ZONE_ID=${1:-}
    INGEST_ZONE_NAME=${2:-}
    if [ -z "$INGEST_ZONE_ID" ]; then
        echo "ERROR: Cloudflare zone not found for ingest_host=$INGEST_HOST" >&2
        exit 1
    fi
    existing_dns_id=$(api GET "/zones/$INGEST_ZONE_ID/dns_records?type=CNAME&name=$INGEST_HOST" | jq -r '.result[0].id // empty')
    dns_payload=$(jq -nc --arg name "$INGEST_HOST" --arg content "$INGEST_CNAME_TARGET" '{
        type: "CNAME",
        name: $name,
        content: $content,
        proxied: true,
        ttl: 1
    }')
    if [ -n "$existing_dns_id" ]; then
        echo ">> Updating ingest DNS: $INGEST_HOST -> $INGEST_CNAME_TARGET"
        api PUT "/zones/$INGEST_ZONE_ID/dns_records/$existing_dns_id" "$dns_payload" >/dev/null
    else
        echo ">> Creating ingest DNS: $INGEST_HOST -> $INGEST_CNAME_TARGET"
        api POST "/zones/$INGEST_ZONE_ID/dns_records" "$dns_payload" >/dev/null
    fi
    echo "Ingest zone:    $INGEST_ZONE_NAME ($INGEST_ZONE_ID)"
fi

ZONE_ID=$(api GET "/zones?name=$DOMAIN&account.id=$ACCOUNT_ID" | jq -r '.result[0].id // empty')
if [ -z "$ZONE_ID" ]; then
    echo "ERROR: Cloudflare zone not found for $DOMAIN" >&2
    exit 1
fi

echo "Cloudflare zone: $DOMAIN ($ZONE_ID)"
echo "Worker:          $EMAIL_WORKER"
echo "Ingest URL:      $INGEST_URL"
if [ -n "$BACKUP_INGEST_URL" ]; then
    echo "Backup URL:      $BACKUP_INGEST_URL"
fi

ROUTE_MAP=""
if [ -n "$BACKUP_INGEST_URL" ] || [ -n "$BACKUP_INGEST_SECRET" ]; then
    if [ -z "$BACKUP_INGEST_URL" ] || [ -z "$BACKUP_INGEST_SECRET" ]; then
        echo "ERROR: backup_ingest_url and backup_ingest_secret must be set together" >&2
        exit 1
    fi
    ROUTE_MAP=$(jq -nc \
        --arg domain "$DOMAIN" \
        --arg home_url "$INGEST_URL" \
        --arg backup_url "$BACKUP_INGEST_URL" \
        '{($domain): {home: {url: $home_url}, backup: {url: $backup_url}}}')
fi

if [ "$WORKER_DEPLOY_METHOD" = "wrangler" ]; then
    tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/localbox-worker.XXXXXX")
    trap 'rm -rf "$tmpdir"' EXIT HUP INT TERM
    cp "$WORKER_SOURCE" "$tmpdir/localbox-email-worker.mjs"
    cat > "$tmpdir/wrangler.jsonc" << EOF
{
  "name": "$EMAIL_WORKER",
  "main": "localbox-email-worker.mjs",
  "compatibility_date": "2026-07-11",
  "vars": {
    "LOCALBOX_INGEST_URL": "$INGEST_URL",
    "LOCALBOX_BACKUP_INGEST_URL": "$BACKUP_INGEST_URL",
    "LOCALBOX_ROUTE_MAP": $(printf '%s' "${ROUTE_MAP:-{}}" | jq -Rs .)
  }
}
EOF
    printf '%s\n' "LOCALBOX_INGEST_SECRET=$INGEST_SECRET" > "$tmpdir/.localbox-secrets.env"
    if [ -n "$BACKUP_INGEST_SECRET" ]; then
        printf '%s\n' "LOCALBOX_BACKUP_INGEST_SECRET=$BACKUP_INGEST_SECRET" >> "$tmpdir/.localbox-secrets.env"
    fi
    echo ">> Deploying Worker module with Wrangler..."
    if command -v wrangler >/dev/null 2>&1; then
        (cd "$tmpdir" && CLOUDFLARE_API_TOKEN=$API_TOKEN wrangler deploy --config wrangler.jsonc --secrets-file .localbox-secrets.env)
    else
        (cd "$tmpdir" && CLOUDFLARE_API_TOKEN=$API_TOKEN npx wrangler deploy --config wrangler.jsonc --secrets-file .localbox-secrets.env)
    fi
else
    METADATA=$(jq -nc \
        --arg main "localbox-email-worker.mjs" \
        --arg ingest_url "$INGEST_URL" \
        --arg ingest_secret "$INGEST_SECRET" \
        --arg backup_ingest_url "$BACKUP_INGEST_URL" \
        --arg backup_ingest_secret "$BACKUP_INGEST_SECRET" \
        --arg route_map "${ROUTE_MAP:-{}}" \
        '{
          main_module: $main,
          bindings: [
            {type:"plain_text", name:"LOCALBOX_INGEST_URL", text:$ingest_url},
            {type:"plain_text", name:"LOCALBOX_ROUTE_MAP", text:$route_map},
            {type:"secret_text", name:"LOCALBOX_INGEST_SECRET", text:$ingest_secret}
          ] + (if $backup_ingest_url != "" then [
            {type:"plain_text", name:"LOCALBOX_BACKUP_INGEST_URL", text:$backup_ingest_url},
            {type:"secret_text", name:"LOCALBOX_BACKUP_INGEST_SECRET", text:$backup_ingest_secret}
          ] else [] end)
        }')

    echo ">> Uploading Worker module with Cloudflare API..."
    curl -fsS -X PUT "$API_URL/accounts/$ACCOUNT_ID/workers/scripts/$EMAIL_WORKER" \
        -H "Authorization: Bearer $API_TOKEN" \
        -F "metadata=$METADATA;type=application/json" \
        -F "localbox-email-worker.mjs=@$WORKER_SOURCE;type=application/javascript+module" >/dev/null
fi

echo ">> Enabling Email Routing DNS for $DOMAIN..."
api POST "/zones/$ZONE_ID/email/routing/dns" '{}' >/dev/null || true

existing_rules=$(api GET "/zones/$ZONE_ID/email/routing/rules")

printf '%s\n' "$EMAIL_ROUTES" | while IFS= read -r address; do
    [ -n "$address" ] || continue
    case "$address" in
        *"@$DOMAIN") ;;
        *)
            echo "ERROR: email_route=$address is not under domain $DOMAIN" >&2
            exit 1
            ;;
    esac

    existing_id=$(printf '%s' "$existing_rules" | jq -r --arg address "$address" '.result[]? | select((.matchers // [])[]? | .field == "to" and .value == $address) | .id' | head -1)
    payload=$(jq -nc --arg name "LocalBox $address" --arg address "$address" --arg worker "$EMAIL_WORKER" '{
        name: $name,
        enabled: true,
        matchers: [{type:"literal", field:"to", value:$address}],
        actions: [{type:"worker", value:[$worker]}]
    }')

    if [ -n "$existing_id" ]; then
        echo ">> Updating email route: $address"
        api PUT "/zones/$ZONE_ID/email/routing/rules/$existing_id" "$payload" >/dev/null
    else
        echo ">> Creating email route: $address"
        api POST "/zones/$ZONE_ID/email/routing/rules" "$payload" >/dev/null
    fi
done

echo "Cloudflare email worker and routes configured."
