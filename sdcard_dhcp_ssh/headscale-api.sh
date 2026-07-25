#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ENV_FILE=${ENV_FILE:-$SCRIPT_DIR/.env}
ACTION=${1:-help}

ENV_HEADSCALE_URL=${HEADSCALE_URL:-}
ENV_HEADSCALE_API_KEY=${HEADSCALE_API_KEY:-}
ENV_HEADSCALE_API_BASE=${HEADSCALE_API_BASE:-}
ENV_HEADSCALE_INSECURE=${HEADSCALE_INSECURE:-}
ENV_HEADSCALE_SSH_HOST=${HEADSCALE_SSH_HOST:-}

if [ -f "$ENV_FILE" ]; then
    set -a
    # shellcheck disable=SC1090
    . "$ENV_FILE"
    set +a
fi

HEADSCALE_URL=${ENV_HEADSCALE_URL:-${HEADSCALE_URL:-}}
HEADSCALE_API_KEY=${ENV_HEADSCALE_API_KEY:-${HEADSCALE_API_KEY:-}}
HEADSCALE_API_BASE=${ENV_HEADSCALE_API_BASE:-${HEADSCALE_API_BASE:-/api/v1}}
HEADSCALE_INSECURE=${ENV_HEADSCALE_INSECURE:-${HEADSCALE_INSECURE:-false}}
HEADSCALE_SSH_HOST=${ENV_HEADSCALE_SSH_HOST:-${HEADSCALE_SSH_HOST:-headscale}}

HEADSCALE_URL=${HEADSCALE_URL:-}
HEADSCALE_API_KEY=${HEADSCALE_API_KEY:-}

usage() {
    cat <<EOF
Usage:
  $0 health
  $0 version
  $0 raw METHOD PATH [JSON_BODY]
  $0 list-users
  $0 list-nodes
  $0 get-node NODE_ID_OR_NAME
  $0 routes
  $0 list-preauthkeys
  $0 list-apikeys
  $0 create-preauthkey user=USER_ID [expiration=1h] [reusable=true|false] [ephemeral=true|false] [tags=tag:a,tag:b]
  $0 ssh-create-preauthkey user=USER_ID [expiration=1h] [reusable=true|false] [ephemeral=true|false] [tags=tag:a,tag:b]
  $0 approve-routes node=NODE_ID routes=CIDR[,CIDR...]
  $0 ssh-routes [node=NODE_ID]

Environment:
  ENV_FILE=$ENV_FILE
  HEADSCALE_URL=https://headscale.example.com
  HEADSCALE_API_KEY=hskey-api-...
EOF
}

require_config() {
    if [ -z "$HEADSCALE_URL" ] || [ -z "$HEADSCALE_API_KEY" ]; then
        echo "ERROR: HEADSCALE_URL and HEADSCALE_API_KEY are required. Put them in $ENV_FILE." >&2
        exit 1
    fi
}

require_tools() {
    if ! command -v curl >/dev/null 2>&1; then
        echo "ERROR: curl is required" >&2
        exit 1
    fi
    if ! command -v jq >/dev/null 2>&1; then
        echo "ERROR: jq is required" >&2
        exit 1
    fi
}

curl_base() {
    if [ "$HEADSCALE_INSECURE" = "true" ] || [ "$HEADSCALE_INSECURE" = "1" ] || [ "$HEADSCALE_INSECURE" = "yes" ]; then
        printf '%s\n' "-k"
    fi
}

api_url() {
    path=$1
    case "$path" in
        http://*|https://*) printf '%s' "$path" ;;
        /api/*) printf '%s%s' "$(printf '%s' "$HEADSCALE_URL" | sed 's:/*$::')" "$path" ;;
        /*) printf '%s%s%s' "$(printf '%s' "$HEADSCALE_URL" | sed 's:/*$::')" "$HEADSCALE_API_BASE" "$path" ;;
        *) printf '%s%s/%s' "$(printf '%s' "$HEADSCALE_URL" | sed 's:/*$::')" "$HEADSCALE_API_BASE" "$path" ;;
    esac
}

api() {
    method=$1
    path=$2
    body=${3:-}
    require_config
    require_tools
    url=$(api_url "$path")
    insecure=$(curl_base)
    if [ -n "$body" ]; then
        # shellcheck disable=SC2086
        curl -fsS $insecure -X "$method" "$url" \
            -H "Authorization: Bearer $HEADSCALE_API_KEY" \
            -H "Content-Type: application/json" \
            -H "Accept: application/json" \
            --data-binary "$body"
    else
        # shellcheck disable=SC2086
        curl -fsS $insecure -X "$method" "$url" \
            -H "Authorization: Bearer $HEADSCALE_API_KEY" \
            -H "Accept: application/json"
    fi
}

kv_value() {
    key=$1
    shift
    for arg in "$@"; do
        case "$arg" in
            "$key="*) printf '%s' "${arg#*=}"; return 0 ;;
        esac
    done
    return 1
}

quote_sh() {
    printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}

duration_to_expiration() {
    duration=$1
    case "$duration" in
        *Z|*[0-9]+00:00|*[0-9]-[0-9]*) printf '%s' "$duration"; return 0 ;;
    esac
    if date -u -d "+$duration" '+%Y-%m-%dT%H:%M:%SZ' >/dev/null 2>&1; then
        date -u -d "+$duration" '+%Y-%m-%dT%H:%M:%SZ'
    else
        echo "ERROR: invalid expiration duration: $duration" >&2
        exit 1
    fi
}

list_nodes() {
    api GET /node | jq -r '
        ["id","name","givenName","user","online","ips","availableRoutes","approvedRoutes"],
        (.nodes[] | [
            .id,
            .name,
            .givenName,
            .user.name,
            (.online|tostring),
            (.ipAddresses // [] | join(",")),
            (.availableRoutes // [] | join(",")),
            (.approvedRoutes // [] | join(","))
        ])
        | @tsv' | column -t -s "$(printf '\t')"
}

get_node() {
    query=$1
    api GET /node | jq --arg query "$query" '
        .nodes[]
        | select((.id|tostring) == $query or .name == $query or .givenName == $query)
    '
}

routes() {
    api GET /node | jq -r '
        ["id","name","online","availableRoutes","approvedRoutes","subnetRoutes"],
        (.nodes[] | [
            .id,
            (.givenName // .name),
            (.online|tostring),
            (.availableRoutes // [] | join(",")),
            (.approvedRoutes // [] | join(",")),
            (.subnetRoutes // [] | join(","))
        ])
        | @tsv' | column -t -s "$(printf '\t')"
}

list_users() {
    api GET /user | jq -r '
        ["id","name","createdAt"],
        (.users[] | [.id, .name, .createdAt])
        | @tsv' | column -t -s "$(printf '\t')"
}

list_preauthkeys() {
    api GET /preauthkey | jq -r '
        ["id","user","key","reusable","ephemeral","used","expiration","tags"],
        (.preAuthKeys[] | [
            .id,
            (.user.name // ""),
            .key,
            (.reusable|tostring),
            (.ephemeral|tostring),
            (.used|tostring),
            .expiration,
            (.aclTags // [] | join(","))
        ])
        | @tsv' | column -t -s "$(printf '\t')"
}

list_apikeys() {
    api GET /apikey | jq -r '
        ["id","prefix","expiration","createdAt","lastSeen"],
        (.apiKeys[] | [.id, .prefix, .expiration, .createdAt, (.lastSeen // "")])
        | @tsv' | column -t -s "$(printf '\t')"
}

create_preauthkey() {
    shift
    user=$(kv_value user "$@" || true)
    expiration=$(kv_value expiration "$@" || true)
    reusable=$(kv_value reusable "$@" || true)
    ephemeral=$(kv_value ephemeral "$@" || true)
    tags=$(kv_value tags "$@" || true)
    expiration=${expiration:-1h}
    reusable=${reusable:-false}
    ephemeral=${ephemeral:-false}
    if [ -z "$user" ]; then
        echo "ERROR: user=USER_ID is required" >&2
        exit 1
    fi
    if [ "${HEADSCALE_PREAUTHKEY_METHOD:-api}" = "ssh" ] || { [ -z "$HEADSCALE_API_KEY" ] && [ -n "$HEADSCALE_SSH_HOST" ]; }; then
        ssh_create_preauthkey create-preauthkey "$@"
        return
    fi
    expires_at=$(duration_to_expiration "$expiration")
    tags_json=$(printf '%s' "$tags" | jq -R 'if . == "" then [] else split(",") end')
    body=$(jq -nc \
        --arg user "$user" \
        --arg expiration "$expires_at" \
        --argjson reusable "$reusable" \
        --argjson ephemeral "$ephemeral" \
        --argjson aclTags "$tags_json" \
        '{user:$user, reusable:$reusable, ephemeral:$ephemeral, expiration:$expiration, aclTags:$aclTags}')
    api POST /preauthkey "$body" | jq .
}

ssh_create_preauthkey() {
    shift
    user=$(kv_value user "$@" || true)
    expiration=$(kv_value expiration "$@" || true)
    reusable=$(kv_value reusable "$@" || true)
    ephemeral=$(kv_value ephemeral "$@" || true)
    tags=$(kv_value tags "$@" || true)
    expiration=${expiration:-1h}
    reusable=${reusable:-false}
    ephemeral=${ephemeral:-false}
    if [ -z "$user" ]; then
        echo "ERROR: user=USER_ID is required" >&2
        exit 1
    fi

    cmd="sudo -n headscale preauthkeys create --user $(quote_sh "$user") --expiration $(quote_sh "$expiration") --output json"
    case "$reusable" in true|1|yes) cmd="$cmd --reusable" ;; esac
    case "$ephemeral" in true|1|yes) cmd="$cmd --ephemeral" ;; esac
    if [ -n "$tags" ]; then
        cmd="$cmd --tags $(quote_sh "$tags")"
    fi
    ssh "$HEADSCALE_SSH_HOST" "$cmd" | jq .
}

ssh_routes() {
    shift
    node=$(kv_value node "$@" || true)
    if [ -n "$node" ]; then
        ssh "$HEADSCALE_SSH_HOST" "sudo -n headscale nodes list-routes --identifier '$node' -o json"
    else
        ssh "$HEADSCALE_SSH_HOST" "sudo -n headscale nodes list-routes -o json"
    fi
}

approve_routes() {
    shift
    node=$(kv_value node "$@" || true)
    route_list=$(kv_value routes "$@" || true)
    if [ -z "$node" ] || [ -z "$route_list" ]; then
        echo "ERROR: node=NODE_ID and routes=CIDR[,CIDR...] are required" >&2
        exit 1
    fi
    ssh "$HEADSCALE_SSH_HOST" "sudo -n headscale nodes approve-routes --identifier '$node' --routes '$route_list' -o json"
}

case "$ACTION" in
    health)
        api GET /health | jq .
        ;;
    version)
        api GET /version | jq . 2>/dev/null || api GET /version
        ;;
    raw)
        shift
        api "${1:-GET}" "${2:-/node}" "${3:-}" | jq . 2>/dev/null || true
        ;;
    list-users)
        list_users
        ;;
    list-nodes)
        list_nodes
        ;;
    get-node)
        shift
        get_node "${1:-}"
        ;;
    routes)
        routes
        ;;
    list-preauthkeys)
        list_preauthkeys
        ;;
    list-apikeys)
        list_apikeys
        ;;
    create-preauthkey)
        create_preauthkey "$@"
        ;;
    ssh-create-preauthkey)
        ssh_create_preauthkey "$@"
        ;;
    approve-routes)
        approve_routes "$@"
        ;;
    ssh-routes)
        ssh_routes "$@"
        ;;
    help|-h|--help)
        usage
        ;;
    *)
        echo "ERROR: unknown action: $ACTION" >&2
        usage >&2
        exit 1
        ;;
esac
