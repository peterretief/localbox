#!/bin/sh
set -eu

usage() {
    cat >&2 <<'USAGE'
Usage:
  brevo-check-blocked-contact.sh [email]

Checks Brevo transactional blocked contacts.

Config:
  BREVO_API_KEY=...                         preferred
  BREVO_CONFIG=/path/to/brevo.conf          optional shell-style config

Config keys accepted:
  password=...
  api_key=...
  brevo_api_key=...
  BREVO_API_KEY=...

Examples:
  BREVO_API_KEY=... scripts/brevo-check-blocked-contact.sh dave@example.co.za
  BREVO_CONFIG=./brevo.conf scripts/brevo-check-blocked-contact.sh
USAGE
}

EMAIL=${1:-}
CONFIG=${BREVO_CONFIG:-}
API_URL=${BREVO_API_URL:-https://api.brevo.com/v3}
API_URL=$(printf '%s' "$API_URL" | sed 's:/*$::')

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
    usage
    exit 0
fi

if [ -z "$CONFIG" ]; then
    if [ -f ./brevo.conf ]; then
        CONFIG=./brevo.conf
    elif [ -f ../brevo.conf ]; then
        CONFIG=../brevo.conf
    fi
fi

if [ -n "$CONFIG" ]; then
    if [ ! -f "$CONFIG" ]; then
        echo "ERROR: Brevo config not found: $CONFIG" >&2
        exit 1
    fi
    # shellcheck disable=SC1090
    . "$CONFIG"
fi

API_KEY=${BREVO_API_KEY:-${brevo_api_key:-${api_key:-${apikey:-${key:-${password:-}}}}}}

if [ -z "$API_KEY" ]; then
    echo "ERROR: BREVO_API_KEY or brevo.conf password/api_key is required" >&2
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

LIMIT=${BREVO_BLOCKED_PAGE_SIZE:-100}
MAX_PAGES=${BREVO_BLOCKED_MAX_PAGES:-20}
OFFSET=0
PAGE=0
FOUND=0

EMAIL_LC=$(printf '%s' "$EMAIL" | tr '[:upper:]' '[:lower:]')
TMP=${TMPDIR:-/tmp}/brevo-blocked-contacts.$$
trap 'rm -f "$TMP"' EXIT HUP INT TERM
: > "$TMP"

while [ "$PAGE" -lt "$MAX_PAGES" ]; do
    RESPONSE=$(curl -fsS \
        -H "Accept: application/json" \
        -H "api-key: $API_KEY" \
        "$API_URL/smtp/blockedContacts?limit=$LIMIT&offset=$OFFSET&sort=desc")

    printf '%s\n' "$RESPONSE" | jq -r '.contacts[]? | @json' >> "$TMP"

    COUNT=$(printf '%s\n' "$RESPONSE" | jq -r '.count // 0')
    RETURNED=$(printf '%s\n' "$RESPONSE" | jq -r '.contacts | length')

    if [ "$RETURNED" -eq 0 ] || [ "$((OFFSET + RETURNED))" -ge "$COUNT" ]; then
        break
    fi

    PAGE=$((PAGE + 1))
    OFFSET=$((OFFSET + LIMIT))
done

if [ -n "$EMAIL" ]; then
    MATCH=$(jq -s --arg email "$EMAIL_LC" '[.[] | select((.email | ascii_downcase) == $email)]' "$TMP")
    if [ "$(printf '%s\n' "$MATCH" | jq 'length')" -gt 0 ]; then
        printf '%s\n' "$MATCH" | jq -r '.[] | "BLOCKED: \(.email)\nreason:  \(.reason.code // "unknown") - \(.reason.message // "")\nblocked: \(.blockedAt // "unknown")\nsender:  \(.senderEmail // "-")"'
        FOUND=1
    else
        echo "NOT BLOCKED: $EMAIL"
    fi
else
    jq -s -r '.[] | "\(.blockedAt // "-")  \(.email)  \(.reason.code // "unknown")  \(.senderEmail // "-")"' "$TMP"
fi

if [ "$FOUND" -eq 1 ]; then
    exit 10
fi
