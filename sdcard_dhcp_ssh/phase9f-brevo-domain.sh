#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
INVENTORY=${INVENTORY:-$SCRIPT_DIR/localbox-inventory.sh}
CLOUDFLARE_CONFIG=${CLOUDFLARE_CONFIG:-$PROJECT_DIR/cloudflare.conf}
BREVO_CONFIG=${BREVO_CONFIG:-$PROJECT_DIR/brevo.conf}
SMTP_CONFIG=${SMTP_CONFIG:-$PROJECT_DIR/smtp.conf}

DEVICE_NAME=${DEVICE_NAME:-}
LOCALBOX_DOMAIN=${LOCALBOX_DOMAIN:-localboxtest.co.za}
BREVO_AUTHENTICATE=${BREVO_AUTHENTICATE:-true}
BREVO_ENSURE_SPF=${BREVO_ENSURE_SPF:-true}
BREVO_ENSURE_PRIMARY_SENDER=${BREVO_ENSURE_PRIMARY_SENDER:-true}
BREVO_PRIMARY_SENDER_EMAIL=${BREVO_PRIMARY_SENDER_EMAIL:-}
BREVO_PRIMARY_SENDER_NAME=${BREVO_PRIMARY_SENDER_NAME:-}

echo "Phase 9f target: Brevo + Cloudflare"
echo "Goal: add sender domain to Brevo and publish Brevo DNS records in Cloudflare"
echo "Device:  ${DEVICE_NAME:-missing}"
echo "Domain:  $LOCALBOX_DOMAIN"
echo "Brevo:   $BREVO_CONFIG"
echo "DNS:     $CLOUDFLARE_CONFIG"

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
[ -n "$DEVICE_NAME" ] || {
    echo "ERROR: DEVICE_NAME is required" >&2
    exit 1
}
[ -f "$BREVO_CONFIG" ] || {
    echo "ERROR: Brevo config not found: $BREVO_CONFIG" >&2
    echo "Copy ../brevo.conf.example to ../brevo.conf and set enabled=true, password." >&2
    exit 1
}
[ -f "$CLOUDFLARE_CONFIG" ] || {
    echo "ERROR: Cloudflare config not found: $CLOUDFLARE_CONFIG" >&2
    exit 1
}

normalize_domain() {
    printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed 's#^https://##; s#^http://##; s#/.*##; s/[.]$//'
}

read_kv_config() {
    file=$1
    key_re=$2
    [ -f "$file" ] || return 0
    awk -F= -v key_re="$key_re" '
        /^[[:space:]]*($|#)/ { next }
        {
            k=tolower($1)
            gsub(/^[ \t]+|[ \t]+$/, "", k)
            if (k ~ key_re) {
                v=$0
                sub(/^[^=]*=/, "", v)
                gsub(/^[ \t]+|[ \t]+$/, "", v)
                gsub(/^["'\''"]|["'\''"]$/, "", v)
                print v
                exit
            }
        }
    ' "$file"
}

normalize_email() {
    value=$1
    value=$(printf '%s' "$value" | sed -n 's/.*<\([^>][^>]*\)>.*/\1/p; t; p' | tr '[:upper:]' '[:lower:]')
    printf '%s' "$value" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//'
}

LOCALBOX_DOMAIN=$(normalize_domain "$LOCALBOX_DOMAIN")
case "$LOCALBOX_DOMAIN" in
    ""|.*|*..*|*_*|*-.*|*.-*|*[!a-z0-9.-]*)
        echo "ERROR: invalid domain: $LOCALBOX_DOMAIN" >&2
        exit 1
        ;;
esac

"$INVENTORY" validate-device "$DEVICE_NAME" >/dev/null
domain_owner=$("$INVENTORY" domain-owner "$LOCALBOX_DOMAIN")
if [ -n "$domain_owner" ] && [ "$domain_owner" != "$DEVICE_NAME" ]; then
    echo "ERROR: domain $LOCALBOX_DOMAIN belongs to $domain_owner, not $DEVICE_NAME" >&2
    exit 1
fi

# shellcheck disable=SC1090
. "$BREVO_CONFIG"
BREVO_API_URL=${BREVO_API_URL:-${api_url:-https://api.brevo.com/v3}}
BREVO_API_URL=$(printf '%s' "$BREVO_API_URL" | sed 's:/*$::')
BREVO_API_KEY=${BREVO_API_KEY:-${brevo_api_key:-${api_key:-${apikey:-${key:-${password:-}}}}}}
if [ "${enabled:-false}" != "true" ] && [ "${enabled:-false}" != "1" ] && [ "${enabled:-false}" != "yes" ]; then
    echo "ERROR: Brevo integration is disabled in $BREVO_CONFIG" >&2
    exit 1
fi
[ -n "$BREVO_API_KEY" ] || {
    echo "ERROR: password or api_key is required in $BREVO_CONFIG" >&2
    exit 1
}

unset enabled api_url api_key apikey key password brevo_api_key
# shellcheck disable=SC1090
. "$CLOUDFLARE_CONFIG"
CLOUDFLARE_API_URL=${CLOUDFLARE_API_URL:-${api_url:-${url:-https://api.cloudflare.com/client/v4}}}
CLOUDFLARE_API_URL=$(printf '%s' "$CLOUDFLARE_API_URL" | sed 's:/*$::')
CLOUDFLARE_API_TOKEN=${CLOUDFLARE_API_TOKEN:-${api_token:-${token:-${api_key:-${apikey:-${key:-}}}}}}
CLOUDFLARE_ZONE_ID=${CLOUDFLARE_ZONE_ID:-}
if [ "${enabled:-false}" != "true" ] && [ "${enabled:-false}" != "1" ] && [ "${enabled:-false}" != "yes" ]; then
    echo "ERROR: Cloudflare integration is disabled in $CLOUDFLARE_CONFIG" >&2
    exit 1
fi
[ -n "$CLOUDFLARE_API_TOKEN" ] || {
    echo "ERROR: Cloudflare api_token is required in $CLOUDFLARE_CONFIG" >&2
    exit 1
}

brevo_api() {
    method=$1
    path=$2
    data=${3:-}
    if [ -n "$data" ]; then
        curl -fsS -X "$method" "$BREVO_API_URL$path" \
            -H "api-key: $BREVO_API_KEY" \
            -H "Accept: application/json" \
            -H "Content-Type: application/json" \
            --data-binary "$data"
    else
        curl -fsS -X "$method" "$BREVO_API_URL$path" \
            -H "api-key: $BREVO_API_KEY" \
            -H "Accept: application/json"
    fi
}

ensure_brevo_sender() {
    sender_email=$(normalize_email "$1")
    sender_name=$2

    [ -n "$sender_email" ] || return 0
    case "$sender_email" in
        *@*) ;;
        *) echo "ERROR: invalid Brevo sender email: $sender_email" >&2; exit 1 ;;
    esac
    sender_domain=${sender_email#*@}
    if [ "$sender_domain" != "$LOCALBOX_DOMAIN" ]; then
        echo "ERROR: Brevo sender $sender_email is not in $LOCALBOX_DOMAIN" >&2
        exit 1
    fi
    [ -n "$sender_name" ] || sender_name="LocalBox $LOCALBOX_DOMAIN"

    echo ">> Ensuring Brevo primary sender..."
    senders_json=$(brevo_api GET "/senders?domain=$LOCALBOX_DOMAIN")
    sender_id=$(printf '%s' "$senders_json" | jq -r --arg email "$sender_email" '
        .senders[]? |
        select((.email // "" | ascii_downcase) == ($email | ascii_downcase)) |
        .id
    ' | head -1)

    if [ -n "$sender_id" ]; then
        sender_active=$(printf '%s' "$senders_json" | jq -r --arg id "$sender_id" '
            .senders[]? |
            select((.id | tostring) == $id) |
            .active // false
        ' | head -1)
        echo "   $sender_email (existing, id=$sender_id, active=$sender_active)"
        brevo_sender_status=existing
        return 0
    fi

    payload=$(jq -nc --arg email "$sender_email" --arg name "$sender_name" '{email:$email, name:$name}')
    create_json=$(brevo_api POST "/senders" "$payload")
    sender_id=$(printf '%s' "$create_json" | jq -r '.id // empty')
    dkim_error=$(printf '%s' "$create_json" | jq -r '.dkimError // empty')
    spf_error=$(printf '%s' "$create_json" | jq -r '.spfError // empty')
    echo "   $sender_email (created, id=$sender_id, dkimError=$dkim_error, spfError=$spf_error)"
    brevo_sender_status=created
}

cf_api() {
    method=$1
    path=$2
    data=${3:-}
    if [ -n "$data" ]; then
        curl -fsS -X "$method" "$CLOUDFLARE_API_URL$path" \
            -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
            -H "Content-Type: application/json" \
            -H "Accept: application/json" \
            --data-binary "$data"
    else
        curl -fsS -X "$method" "$CLOUDFLARE_API_URL$path" \
            -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
            -H "Accept: application/json"
    fi
}

echo ">> Finding Cloudflare zone..."
if [ -z "$CLOUDFLARE_ZONE_ID" ]; then
    zone_json=$(cf_api GET "/zones?name=$LOCALBOX_DOMAIN")
    CLOUDFLARE_ZONE_ID=$(printf '%s' "$zone_json" | jq -r '.result[0].id // empty')
    ZONE_STATUS=$(printf '%s' "$zone_json" | jq -r '.result[0].status // empty')
else
    zone_json=$(cf_api GET "/zones/$CLOUDFLARE_ZONE_ID")
    zone_name=$(printf '%s' "$zone_json" | jq -r '.result.name // empty')
    ZONE_STATUS=$(printf '%s' "$zone_json" | jq -r '.result.status // empty')
    [ "$zone_name" = "$LOCALBOX_DOMAIN" ] || {
        echo "ERROR: CLOUDFLARE_ZONE_ID does not belong to $LOCALBOX_DOMAIN" >&2
        exit 1
    }
fi
[ -n "$CLOUDFLARE_ZONE_ID" ] || {
    echo "ERROR: Cloudflare zone not found for $LOCALBOX_DOMAIN" >&2
    exit 1
}
echo "   $LOCALBOX_DOMAIN ($ZONE_STATUS)"

echo ">> Ensuring Brevo sender domain..."
brevo_status=existing
http_code=$(curl -sS -o /tmp/localbox-brevo-domain.json -w '%{http_code}' \
    -H "api-key: $BREVO_API_KEY" \
    -H "Accept: application/json" \
    "$BREVO_API_URL/senders/domains/$LOCALBOX_DOMAIN")
case "$http_code" in
    200)
        domain_json=$(cat /tmp/localbox-brevo-domain.json)
        ;;
    404)
        payload=$(jq -nc --arg name "$LOCALBOX_DOMAIN" '{name:$name}')
        domain_json=$(brevo_api POST "/senders/domains" "$payload")
        brevo_status=created
        ;;
    *)
        cat /tmp/localbox-brevo-domain.json >&2 || true
        echo "ERROR: Brevo domain lookup failed with HTTP $http_code" >&2
        exit 1
        ;;
esac
rm -f /tmp/localbox-brevo-domain.json

verified=$(printf '%s' "$domain_json" | jq -r '.verified // false')
authenticated=$(printf '%s' "$domain_json" | jq -r '.authenticated // false')
echo "   $LOCALBOX_DOMAIN ($brevo_status, verified=$verified, authenticated=$authenticated)"

records=$(printf '%s' "$domain_json" | jq -c '.dns_records // {} | to_entries[]? | .value')
if [ -z "$records" ]; then
    echo "ERROR: Brevo returned no DNS records for $LOCALBOX_DOMAIN" >&2
    exit 1
fi

record_name() {
    host=$1
    case "$host" in
        @|"") printf '%s' "$LOCALBOX_DOMAIN" ;;
        *."$LOCALBOX_DOMAIN") printf '%s' "$host" ;;
        *) printf '%s.%s' "$host" "$LOCALBOX_DOMAIN" ;;
    esac
}

echo ">> Publishing Brevo DNS records in Cloudflare..."
printf '%s\n' "$records" | while IFS= read -r rec; do
    type=$(printf '%s' "$rec" | jq -r '.type // empty' | tr '[:lower:]' '[:upper:]')
    host=$(printf '%s' "$rec" | jq -r '.host_name // .hostname // .name // empty')
    value=$(printf '%s' "$rec" | jq -r '.value // empty')
    [ -n "$type" ] && [ -n "$host" ] && [ -n "$value" ] || continue

    name=$(record_name "$host")
    if [ "$type" = "TXT" ]; then
        value_unquoted=$(printf '%s' "$value" | sed 's/^"//; s/"$//')
        case "$value" in
            v=spf1*|V=SPF1*|\"v=spf1*|\"V=SPF1*)
                echo "   defer TXT $name SPF to merge step"
                continue
                ;;
        esac
        case "$value_unquoted" in
            v=spf1*|V=SPF1*)
                echo "   defer TXT $name SPF to merge step"
                continue
                ;;
        esac
    fi

    existing=$(cf_api GET "/zones/$CLOUDFLARE_ZONE_ID/dns_records?type=$type&name=$name&per_page=100")
    exact_id=$(printf '%s' "$existing" | jq -r --arg content "$value" '.result[]? | select(.content == $content) | .id' | head -1)
    first_id=$(printf '%s' "$existing" | jq -r '.result[0].id // empty')
    payload=$(jq -nc --arg type "$type" --arg name "$name" --arg content "$value" \
        '{type:$type, name:$name, content:$content, ttl:1, proxied:false}')

    if [ -n "$exact_id" ]; then
        echo "   ok $type $name"
    elif [ -n "$first_id" ] && [ "$type" != "TXT" ]; then
        cf_api PUT "/zones/$CLOUDFLARE_ZONE_ID/dns_records/$first_id" "$payload" >/dev/null
        echo "   updated $type $name"
    else
        cf_api POST "/zones/$CLOUDFLARE_ZONE_ID/dns_records" "$payload" >/dev/null
        echo "   created $type $name"
    fi
done

if [ "$BREVO_ENSURE_SPF" = "true" ] || [ "$BREVO_ENSURE_SPF" = "1" ] || [ "$BREVO_ENSURE_SPF" = "yes" ]; then
    echo ">> Ensuring SPF includes Brevo..."
    spf_json=$(cf_api GET "/zones/$CLOUDFLARE_ZONE_ID/dns_records?type=TXT&name=$LOCALBOX_DOMAIN&per_page=100")
    canonical_spf='v=spf1 include:spf.brevo.com mx include:_spf.mx.cloudflare.net ~all'
    spf_ids=$(printf '%s' "$spf_json" | jq -r '
        .result[]? |
        select(.content | gsub("^\"|\"$"; "") | test("^v=spf1"; "i")) |
        .id
    ')
    spf_id=$(printf '%s\n' "$spf_ids" | sed -n '1p')
    spf_payload=$(jq -nc --arg name "$LOCALBOX_DOMAIN" --arg content "$canonical_spf" \
        '{type:"TXT", name:$name, content:$content, ttl:1, proxied:false}')
    if [ -n "$spf_id" ]; then
        current_spf=$(printf '%s' "$spf_json" | jq -r --arg id "$spf_id" '
            .result[]? |
            select(.id == $id) |
            .content |
            gsub("^\"|\"$"; "")
        ')
        if [ "$current_spf" = "$canonical_spf" ]; then
            echo "   ok TXT $LOCALBOX_DOMAIN SPF"
        else
            cf_api PUT "/zones/$CLOUDFLARE_ZONE_ID/dns_records/$spf_id" "$spf_payload" >/dev/null
            echo "   updated TXT $LOCALBOX_DOMAIN SPF"
        fi
    else
        cf_api POST "/zones/$CLOUDFLARE_ZONE_ID/dns_records" "$spf_payload" >/dev/null
        echo "   created TXT $LOCALBOX_DOMAIN SPF"
    fi

    printf '%s\n' "$spf_ids" | sed '1d' |
    while IFS= read -r stale_id; do
        [ -n "$stale_id" ] || continue
        cf_api DELETE "/zones/$CLOUDFLARE_ZONE_ID/dns_records/$stale_id" >/dev/null
        echo "   deleted duplicate SPF TXT $stale_id"
    done
fi

if [ "$BREVO_AUTHENTICATE" = "true" ] || [ "$BREVO_AUTHENTICATE" = "1" ] || [ "$BREVO_AUTHENTICATE" = "yes" ]; then
    echo ">> Asking Brevo to authenticate domain..."
    if brevo_api PUT "/senders/domains/$LOCALBOX_DOMAIN/authenticate" '{}' >/tmp/localbox-brevo-auth.out 2>/tmp/localbox-brevo-auth.err; then
        cat /tmp/localbox-brevo-auth.out
    else
        echo "WARN: Brevo did not authenticate yet. DNS may still be propagating."
        cat /tmp/localbox-brevo-auth.err >&2 || true
    fi
    rm -f /tmp/localbox-brevo-auth.out /tmp/localbox-brevo-auth.err
fi

brevo_sender_status=skipped
primary_sender_email=$BREVO_PRIMARY_SENDER_EMAIL
if [ -z "$primary_sender_email" ]; then
    primary_sender_email=$(read_kv_config "$SMTP_CONFIG" '^(from|sender)$')
fi
primary_sender_name=$BREVO_PRIMARY_SENDER_NAME
if [ -z "$primary_sender_name" ]; then
    primary_sender_name=$(read_kv_config "$SMTP_CONFIG" '^(from_name|sender_name|name)$')
fi

if [ "$BREVO_ENSURE_PRIMARY_SENDER" = "true" ] || [ "$BREVO_ENSURE_PRIMARY_SENDER" = "1" ] || [ "$BREVO_ENSURE_PRIMARY_SENDER" = "yes" ]; then
    if [ -n "$primary_sender_email" ]; then
        ensure_brevo_sender "$primary_sender_email" "$primary_sender_name"
    else
        echo ">> Brevo primary sender skipped; set from= in $SMTP_CONFIG or BREVO_PRIMARY_SENDER_EMAIL."
    fi
fi

domain_json=$(brevo_api GET "/senders/domains/$LOCALBOX_DOMAIN")
verified=$(printf '%s' "$domain_json" | jq -r '.verified // false')
authenticated=$(printf '%s' "$domain_json" | jq -r '.authenticated // false')
if [ "$authenticated" = "true" ]; then
    brevo_status=authenticated
elif [ "$verified" = "true" ]; then
    brevo_status=verified
else
    brevo_status=dns-published
fi

"$INVENTORY" upsert-domain "$LOCALBOX_DOMAIN" \
    device_name="$DEVICE_NAME" \
    zone_id="$CLOUDFLARE_ZONE_ID" \
    zone_status="$ZONE_STATUS" \
    brevo_status="$brevo_status" \
    brevo_verified="$verified" \
    brevo_authenticated="$authenticated" \
    brevo_primary_sender="$(normalize_email "$primary_sender_email")" \
    brevo_sender_status="$brevo_sender_status" \
    status=published
"$INVENTORY" phase "$DEVICE_NAME" phase9f-brevo-domain complete detail="$LOCALBOX_DOMAIN $brevo_status sender=$brevo_sender_status"

echo ">> Phase 9f complete."
echo "Brevo domain: $LOCALBOX_DOMAIN"
echo "Status:       $brevo_status"
if [ -n "$primary_sender_email" ]; then
    echo "Sender:       $(normalize_email "$primary_sender_email") ($brevo_sender_status)"
fi
