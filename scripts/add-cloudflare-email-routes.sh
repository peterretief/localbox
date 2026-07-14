#!/bin/sh
set -eu

CONFIG=${1:-}
CLOUDFLARE_CONFIG=${CLOUDFLARE_CONFIG:-/etc/localbox/cloudflare.conf}

if [ -z "$CONFIG" ] || [ ! -f "$CONFIG" ]; then
    echo "Usage: localbox-add-email-routes /path/domain.conf" >&2
    exit 1
fi

if [ ! -f "$CLOUDFLARE_CONFIG" ]; then
    echo "ERROR: Cloudflare config not found: $CLOUDFLARE_CONFIG" >&2
    exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
    echo "ERROR: curl is required" >&2
    exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
    if command -v apk >/dev/null 2>&1 && [ "$(id -u)" -eq 0 ]; then
        apk add -q jq
    else
        echo "ERROR: jq is required. Install jq or run as root on Alpine so apk can install it." >&2
        exit 1
    fi
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
EMAIL_FORWARD_TO=""
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
        email_forward_to)
            EMAIL_FORWARD_TO=$(normalize_email "$value")
            ;;
        email_route)
            EMAIL_ROUTES="${EMAIL_ROUTES}${EMAIL_ROUTES:+
}$(normalize_email "$value")"
            ;;
    esac
done < "$CONFIG"

if [ -z "$DOMAIN" ]; then
    echo "ERROR: domain= is required in $CONFIG" >&2
    exit 1
fi
if [ -z "$EMAIL_ROUTES" ]; then
    echo "No email_route= entries found in $CONFIG"
    exit 0
fi
if [ -n "$EMAIL_WORKER" ] && [ -n "$EMAIL_FORWARD_TO" ]; then
    echo "ERROR: choose either email_worker= or email_forward_to=, not both" >&2
    exit 1
fi
if [ -z "$EMAIL_WORKER" ] && [ -z "$EMAIL_FORWARD_TO" ]; then
    echo "ERROR: set email_worker=WORKER_NAME or email_forward_to=address in $CONFIG" >&2
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

ZONE_ID=$(api GET "/zones?name=$DOMAIN&account.id=$ACCOUNT_ID" | jq -r '.result[0].id // empty')
if [ -z "$ZONE_ID" ]; then
    echo "ERROR: Cloudflare zone not found for $DOMAIN" >&2
    exit 1
fi

echo "Cloudflare zone: $DOMAIN ($ZONE_ID)"

if [ -n "$EMAIL_FORWARD_TO" ]; then
    echo "Ensuring destination address: $EMAIL_FORWARD_TO"
    existing_destination=$(api GET "/accounts/$ACCOUNT_ID/email/routing/addresses" | jq -r --arg email "$EMAIL_FORWARD_TO" '.result[]? | select(.email == $email) | .id' | head -1)
    if [ -z "$existing_destination" ]; then
        api POST "/accounts/$ACCOUNT_ID/email/routing/addresses" "$(jq -nc --arg email "$EMAIL_FORWARD_TO" '{email:$email}')" >/dev/null
        echo "Created destination address. Cloudflare may require email verification before forwarding works."
    fi
fi

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

    if [ -n "$EMAIL_WORKER" ]; then
        payload=$(jq -nc --arg name "LocalBox $address" --arg address "$address" --arg worker "$EMAIL_WORKER" '{
            name: $name,
            enabled: true,
            matchers: [{type:"literal", field:"to", value:$address}],
            actions: [{type:"worker", value:[$worker]}]
        }')
    else
        payload=$(jq -nc --arg name "LocalBox $address" --arg address "$address" --arg forward "$EMAIL_FORWARD_TO" '{
            name: $name,
            enabled: true,
            matchers: [{type:"literal", field:"to", value:$address}],
            actions: [{type:"forward", value:[$forward]}]
        }')
    fi

    if [ -n "$existing_id" ]; then
        echo "Updating email route: $address"
        api PUT "/zones/$ZONE_ID/email/routing/rules/$existing_id" "$payload" >/dev/null
    else
        echo "Creating email route: $address"
        api POST "/zones/$ZONE_ID/email/routing/rules" "$payload" >/dev/null
    fi
done

echo "Email routes configured."
