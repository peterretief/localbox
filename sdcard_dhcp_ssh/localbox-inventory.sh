#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
INVENTORY_DB=${INVENTORY_DB:-$SCRIPT_DIR/state/localbox-inventory.sqlite}
ACTION=${1:-help}

need_sqlite() {
    if ! command -v sqlite3 >/dev/null 2>&1; then
        echo "ERROR: sqlite3 is required" >&2
        exit 1
    fi
}

init_db() {
    need_sqlite
    mkdir -p "$(dirname "$INVENTORY_DB")"
    sqlite3 "$INVENTORY_DB" <<'SQL'
.timeout 5000
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS counters (
    name TEXT PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS devices (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_name TEXT NOT NULL UNIQUE,
    hostname TEXT,
    pi_host TEXT,
    ssh_user TEXT DEFAULT 'root',
    board_model TEXT,
    serial TEXT,
    sd_device TEXT,
    data_device TEXT,
    data_label TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    notes TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tunnels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tunnel_name TEXT NOT NULL UNIQUE,
    tunnel_id TEXT UNIQUE,
    device_name TEXT,
    status TEXT,
    connector_arch TEXT,
    connector_version TEXT,
    origin_ip TEXT,
    token_installed INTEGER NOT NULL DEFAULT 0,
    notes TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(device_name) REFERENCES devices(device_name) ON UPDATE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_tunnels_device_name_active
ON tunnels(device_name)
WHERE device_name IS NOT NULL AND status != 'deleted';

CREATE TABLE IF NOT EXISTS domains (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    domain TEXT NOT NULL UNIQUE,
    zone_id TEXT UNIQUE,
    zone_status TEXT,
    nameservers TEXT,
    device_name TEXT,
    admin_email TEXT,
    status TEXT NOT NULL DEFAULT 'planned',
    notes TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(device_name) REFERENCES devices(device_name) ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS routes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    hostname TEXT NOT NULL UNIQUE,
    domain TEXT NOT NULL,
    tunnel_name TEXT,
    tunnel_id TEXT,
    service TEXT,
    dns_record_id TEXT,
    status TEXT NOT NULL DEFAULT 'planned',
    notes TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(domain) REFERENCES domains(domain) ON UPDATE CASCADE,
    FOREIGN KEY(tunnel_name) REFERENCES tunnels(tunnel_name) ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS phase_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_name TEXT,
    phase TEXT NOT NULL,
    status TEXT NOT NULL,
    detail TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(device_name) REFERENCES devices(device_name) ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS schema_migrations (
    name TEXT PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO schema_migrations(name) VALUES('devices_tailscale_columns')
ON CONFLICT(name) DO NOTHING;
SQL
    for column in tailscale_name tailscale_ips advertised_routes approved_routes; do
        if ! sqlite3 "$INVENTORY_DB" "SELECT $column FROM devices LIMIT 0;" >/dev/null 2>&1; then
            sqlite3 "$INVENTORY_DB" "ALTER TABLE devices ADD COLUMN $column TEXT;"
        fi
    done
    if ! sqlite3 "$INVENTORY_DB" "SELECT asset_tag FROM devices LIMIT 0;" >/dev/null 2>&1; then
        sqlite3 "$INVENTORY_DB" "ALTER TABLE devices ADD COLUMN asset_tag TEXT;"
    fi
    for column in expected_tunnel expected_worker; do
        if ! sqlite3 "$INVENTORY_DB" "SELECT $column FROM devices LIMIT 0;" >/dev/null 2>&1; then
            sqlite3 "$INVENTORY_DB" "ALTER TABLE devices ADD COLUMN $column TEXT;"
        fi
    done
    for column in brevo_status brevo_verified brevo_authenticated; do
        if ! sqlite3 "$INVENTORY_DB" "SELECT $column FROM domains LIMIT 0;" >/dev/null 2>&1; then
            sqlite3 "$INVENTORY_DB" "ALTER TABLE domains ADD COLUMN $column TEXT;"
        fi
    done
    sqlite3 "$INVENTORY_DB" "CREATE UNIQUE INDEX IF NOT EXISTS idx_devices_asset_tag ON devices(asset_tag) WHERE asset_tag IS NOT NULL AND asset_tag != '';"
}

usage() {
    cat <<EOF
Usage:
  INVENTORY_DB=$INVENTORY_DB $0 init
  $0 next-name device|tunnel|worker [prefix]
  $0 reserve-devices COUNT [device_prefix=localbox] [asset_prefix=LBX]
  $0 upsert-device DEVICE_NAME [asset_tag=...] [expected_tunnel=...] [expected_worker=...] [hostname=...] [pi_host=...] [tailscale_name=...] [tailscale_ips=...] [advertised_routes=...] [approved_routes=...] [serial=...] [status=...] [notes=...]
  $0 upsert-domain DOMAIN [device_name=...] [zone_id=...] [zone_status=...] [nameservers=...] [admin_email=...] [brevo_status=...] [brevo_verified=...] [brevo_authenticated=...] [status=...]
  $0 upsert-tunnel TUNNEL_NAME [tunnel_id=...] [device_name=...] [status=...] [origin_ip=...] [connector_version=...]
  $0 upsert-route HOSTNAME domain=DOMAIN [tunnel_name=...] [tunnel_id=...] [service=...] [dns_record_id=...] [status=...]
  $0 phase DEVICE_NAME PHASE STATUS [detail=...]
  $0 reconcile-counters
  $0 list devices|domains|tunnels|routes|phases
  $0 show-device DEVICE_NAME
  $0 device-field DEVICE_NAME FIELD
  $0 lookup-tag ASSET_TAG
  $0 show-domain DOMAIN
  $0 domain-field DOMAIN FIELD
  $0 domain-owner DOMAIN
  $0 tunnel-owner TUNNEL_NAME_OR_ID
  $0 validate-device DEVICE_NAME
  $0 validate-publish DEVICE_NAME DOMAIN TUNNEL_NAME_OR_ID
EOF
}

sql_quote() {
    printf "'%s'" "$(printf '%s' "$1" | sed "s/'/''/g")"
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

set_sql() {
    col=$1
    val=$2
    if [ -n "$val" ]; then
        printf ', %s = %s' "$col" "$(sql_quote "$val")"
    fi
}

numeric_suffix() {
    expr "$1" : '.*-\([0-9][0-9]*\)$' 2>/dev/null || true
}

bump_counter_from_name() {
    kind=$1
    name=$2
    suffix=$(numeric_suffix "$name")
    [ -n "$suffix" ] || return 0
    sqlite3 "$INVENTORY_DB" "INSERT INTO counters(name, value) VALUES($(sql_quote "$kind"), $suffix)
ON CONFLICT(name) DO UPDATE SET value = CASE WHEN value < $suffix THEN $suffix ELSE value END;"
}

require_arg() {
    value=$1
    label=$2
    if [ -z "$value" ]; then
        echo "ERROR: missing $label" >&2
        exit 1
    fi
}

next_name() {
    kind=$1
    prefix=${2:-localbox}
    init_db
    sqlite3 "$INVENTORY_DB" "BEGIN;
INSERT INTO counters(name, value) VALUES($(sql_quote "$kind"), 0)
ON CONFLICT(name) DO NOTHING;
UPDATE counters SET value = value + 1 WHERE name = $(sql_quote "$kind");
SELECT printf('%s-%03d', $(sql_quote "$prefix"), value) FROM counters WHERE name = $(sql_quote "$kind");
COMMIT;"
}

upsert_device() {
    name=$1
    shift
    require_arg "$name" "device name"
    hostname=$(kv_value hostname "$@" || true)
    asset_tag=$(kv_value asset_tag "$@" || true)
    expected_tunnel=$(kv_value expected_tunnel "$@" || true)
    expected_worker=$(kv_value expected_worker "$@" || true)
    pi_host=$(kv_value pi_host "$@" || true)
    ssh_user=$(kv_value ssh_user "$@" || true)
    board_model=$(kv_value board_model "$@" || true)
    serial=$(kv_value serial "$@" || true)
    sd_device=$(kv_value sd_device "$@" || true)
    data_device=$(kv_value data_device "$@" || true)
    data_label=$(kv_value data_label "$@" || true)
    tailscale_name=$(kv_value tailscale_name "$@" || true)
    tailscale_ips=$(kv_value tailscale_ips "$@" || true)
    advertised_routes=$(kv_value advertised_routes "$@" || true)
    approved_routes=$(kv_value approved_routes "$@" || true)
    status=$(kv_value status "$@" || true)
    notes=$(kv_value notes "$@" || true)
    init_db
    sqlite3 "$INVENTORY_DB" "INSERT INTO devices(device_name) VALUES($(sql_quote "$name"))
ON CONFLICT(device_name) DO NOTHING;
UPDATE devices SET updated_at = CURRENT_TIMESTAMP$(set_sql asset_tag "$asset_tag")$(set_sql expected_tunnel "$expected_tunnel")$(set_sql expected_worker "$expected_worker")$(set_sql hostname "$hostname")$(set_sql pi_host "$pi_host")$(set_sql ssh_user "$ssh_user")$(set_sql board_model "$board_model")$(set_sql serial "$serial")$(set_sql sd_device "$sd_device")$(set_sql data_device "$data_device")$(set_sql data_label "$data_label")$(set_sql tailscale_name "$tailscale_name")$(set_sql tailscale_ips "$tailscale_ips")$(set_sql advertised_routes "$advertised_routes")$(set_sql approved_routes "$approved_routes")$(set_sql status "$status")$(set_sql notes "$notes")
WHERE device_name = $(sql_quote "$name");"
    bump_counter_from_name device "$name"
}

upsert_domain() {
    domain=$1
    shift
    require_arg "$domain" "domain"
    device_name=$(kv_value device_name "$@" || true)
    zone_id=$(kv_value zone_id "$@" || true)
    zone_status=$(kv_value zone_status "$@" || true)
    nameservers=$(kv_value nameservers "$@" || true)
    admin_email=$(kv_value admin_email "$@" || true)
    brevo_status=$(kv_value brevo_status "$@" || true)
    brevo_verified=$(kv_value brevo_verified "$@" || true)
    brevo_authenticated=$(kv_value brevo_authenticated "$@" || true)
    status=$(kv_value status "$@" || true)
    notes=$(kv_value notes "$@" || true)
    init_db
    sqlite3 "$INVENTORY_DB" "INSERT INTO domains(domain) VALUES($(sql_quote "$domain"))
ON CONFLICT(domain) DO NOTHING;
UPDATE domains SET updated_at = CURRENT_TIMESTAMP$(set_sql device_name "$device_name")$(set_sql zone_id "$zone_id")$(set_sql zone_status "$zone_status")$(set_sql nameservers "$nameservers")$(set_sql admin_email "$admin_email")$(set_sql brevo_status "$brevo_status")$(set_sql brevo_verified "$brevo_verified")$(set_sql brevo_authenticated "$brevo_authenticated")$(set_sql status "$status")$(set_sql notes "$notes")
WHERE domain = $(sql_quote "$domain");"
}

upsert_tunnel() {
    name=$1
    shift
    require_arg "$name" "tunnel name"
    tunnel_id=$(kv_value tunnel_id "$@" || true)
    device_name=$(kv_value device_name "$@" || true)
    status=$(kv_value status "$@" || true)
    connector_arch=$(kv_value connector_arch "$@" || true)
    connector_version=$(kv_value connector_version "$@" || true)
    origin_ip=$(kv_value origin_ip "$@" || true)
    token_installed=$(kv_value token_installed "$@" || true)
    notes=$(kv_value notes "$@" || true)
    init_db
    sqlite3 "$INVENTORY_DB" "INSERT INTO tunnels(tunnel_name) VALUES($(sql_quote "$name"))
ON CONFLICT(tunnel_name) DO NOTHING;
UPDATE tunnels SET updated_at = CURRENT_TIMESTAMP$(set_sql tunnel_id "$tunnel_id")$(set_sql device_name "$device_name")$(set_sql status "$status")$(set_sql connector_arch "$connector_arch")$(set_sql connector_version "$connector_version")$(set_sql origin_ip "$origin_ip")$(set_sql token_installed "$token_installed")$(set_sql notes "$notes")
WHERE tunnel_name = $(sql_quote "$name");"
    bump_counter_from_name tunnel "$name"
}

upsert_route() {
    hostname=$1
    shift
    require_arg "$hostname" "hostname"
    domain=$(kv_value domain "$@" || true)
    require_arg "$domain" "domain=DOMAIN"
    tunnel_name=$(kv_value tunnel_name "$@" || true)
    tunnel_id=$(kv_value tunnel_id "$@" || true)
    service=$(kv_value service "$@" || true)
    dns_record_id=$(kv_value dns_record_id "$@" || true)
    status=$(kv_value status "$@" || true)
    notes=$(kv_value notes "$@" || true)
    init_db
    sqlite3 "$INVENTORY_DB" "INSERT INTO routes(hostname, domain) VALUES($(sql_quote "$hostname"), $(sql_quote "$domain"))
ON CONFLICT(hostname) DO NOTHING;
UPDATE routes SET updated_at = CURRENT_TIMESTAMP$(set_sql domain "$domain")$(set_sql tunnel_name "$tunnel_name")$(set_sql tunnel_id "$tunnel_id")$(set_sql service "$service")$(set_sql dns_record_id "$dns_record_id")$(set_sql status "$status")$(set_sql notes "$notes")
WHERE hostname = $(sql_quote "$hostname");"
}

record_phase() {
    device_name=$1
    phase=$2
    status=$3
    shift 3
    detail=$(kv_value detail "$@" || true)
    init_db
    sqlite3 "$INVENTORY_DB" "INSERT INTO phase_runs(device_name, phase, status, detail) VALUES($(sql_quote "$device_name"), $(sql_quote "$phase"), $(sql_quote "$status"), $(sql_quote "$detail"));"
}

reconcile_counters() {
    init_db
    sqlite3 "$INVENTORY_DB" <<'SQL'
DELETE FROM counters WHERE name IN ('device', 'tunnel');
INSERT INTO counters(name, value)
SELECT 'device', COALESCE(MAX(CAST(substr(device_name, length(rtrim(device_name, '0123456789')) + 1) AS INTEGER)), 0)
FROM devices
WHERE device_name GLOB '*-[0-9]*';
INSERT INTO counters(name, value)
SELECT 'tunnel', COALESCE(MAX(CAST(substr(tunnel_name, length(rtrim(tunnel_name, '0123456789')) + 1) AS INTEGER)), 0)
FROM tunnels
WHERE tunnel_name GLOB '*-[0-9]*';
SQL
}

reserve_devices() {
    count=$1
    shift
    device_prefix=$(kv_value device_prefix "$@" || true)
    asset_prefix=$(kv_value asset_prefix "$@" || true)
    device_prefix=${device_prefix:-localbox}
    asset_prefix=${asset_prefix:-LBX}
    require_arg "$count" "count"
    case "$count" in
        *[!0-9]*|"")
            echo "ERROR: count must be a positive integer" >&2
            exit 1
            ;;
    esac
    init_db
    i=0
    while [ "$i" -lt "$count" ]; do
        device_name=$(next_name device "$device_prefix")
        suffix=$(numeric_suffix "$device_name")
        asset_tag=$(printf '%s-%04d' "$asset_prefix" "$suffix")
        upsert_device "$device_name" asset_tag="$asset_tag" status=reserved notes=batch-label
        printf '%s %s\n' "$device_name" "$asset_tag"
        i=$((i + 1))
    done
}

list_table() {
    table=$1
    init_db
    case "$table" in
        devices)
            sqlite3 -header -column "$INVENTORY_DB" "SELECT device_name, asset_tag, expected_tunnel, hostname, pi_host, tailscale_name, tailscale_ips, advertised_routes, approved_routes, status, updated_at FROM devices ORDER BY id;"
            ;;
        domains)
            sqlite3 -header -column "$INVENTORY_DB" "SELECT domain, zone_status, brevo_status, brevo_verified, brevo_authenticated, device_name, admin_email, status, updated_at FROM domains ORDER BY id;"
            ;;
        tunnels)
            sqlite3 -header -column "$INVENTORY_DB" "SELECT tunnel_name, tunnel_id, device_name, status, origin_ip, connector_version, updated_at FROM tunnels ORDER BY id;"
            ;;
        routes)
            sqlite3 -header -column "$INVENTORY_DB" "SELECT hostname, domain, tunnel_name, service, status, updated_at FROM routes ORDER BY id;"
            ;;
        phases)
            sqlite3 -header -column "$INVENTORY_DB" "SELECT device_name, phase, status, detail, created_at FROM phase_runs ORDER BY id DESC LIMIT 50;"
            ;;
        *)
            echo "ERROR: unknown list target: $table" >&2
            exit 1
            ;;
    esac
}

show_device() {
    init_db
    name=$1
    sqlite3 -header -column "$INVENTORY_DB" "SELECT * FROM devices WHERE device_name = $(sql_quote "$name");
SELECT '';
SELECT * FROM tunnels WHERE device_name = $(sql_quote "$name");
SELECT '';
SELECT * FROM domains WHERE device_name = $(sql_quote "$name");
SELECT '';
SELECT routes.* FROM routes JOIN domains ON domains.domain = routes.domain WHERE domains.device_name = $(sql_quote "$name");"
}

lookup_tag() {
    init_db
    tag=$1
    sqlite3 -header -column "$INVENTORY_DB" "SELECT * FROM devices WHERE asset_tag = $(sql_quote "$tag");
SELECT '';
SELECT tunnels.* FROM tunnels JOIN devices ON devices.device_name = tunnels.device_name WHERE devices.asset_tag = $(sql_quote "$tag");
SELECT '';
SELECT domains.* FROM domains JOIN devices ON devices.device_name = domains.device_name WHERE devices.asset_tag = $(sql_quote "$tag");"
}

show_domain() {
    init_db
    domain=$1
    sqlite3 -header -column "$INVENTORY_DB" "SELECT * FROM domains WHERE domain = $(sql_quote "$domain");
SELECT '';
SELECT * FROM routes WHERE domain = $(sql_quote "$domain");"
}

tunnel_owner() {
    init_db
    tunnel=$1
    sqlite3 -noheader "$INVENTORY_DB" "SELECT COALESCE(device_name, '') FROM tunnels WHERE tunnel_name = $(sql_quote "$tunnel") OR tunnel_id = $(sql_quote "$tunnel") LIMIT 1;"
}

domain_owner() {
    init_db
    domain=$1
    sqlite3 -noheader "$INVENTORY_DB" "SELECT COALESCE(device_name, '') FROM domains WHERE domain = $(sql_quote "$domain") LIMIT 1;"
}

value_for_domain() {
    init_db
    domain=$1
    column=$2
    case "$column" in
        domain|zone_id|zone_status|nameservers|device_name|admin_email|brevo_status|brevo_verified|brevo_authenticated|status|notes) ;;
        *)
            echo "ERROR: invalid domain field: $column" >&2
            exit 1
            ;;
    esac
    sqlite3 -noheader "$INVENTORY_DB" "SELECT COALESCE($column, '') FROM domains WHERE domain = $(sql_quote "$domain") LIMIT 1;"
}

value_for_device() {
    init_db
    device_name=$1
    column=$2
    case "$column" in
        device_name|hostname|pi_host|ssh_user|board_model|serial|sd_device|data_device|data_label|status|notes|tailscale_name|tailscale_ips|advertised_routes|approved_routes|asset_tag|expected_tunnel|expected_worker) ;;
        *)
            echo "ERROR: invalid device field: $column" >&2
            exit 1
            ;;
    esac
    sqlite3 -noheader "$INVENTORY_DB" "SELECT COALESCE($column, '') FROM devices WHERE device_name = $(sql_quote "$device_name") LIMIT 1;"
}

tunnel_name_for() {
    init_db
    tunnel=$1
    sqlite3 -noheader "$INVENTORY_DB" "SELECT COALESCE(tunnel_name, '') FROM tunnels WHERE tunnel_name = $(sql_quote "$tunnel") OR tunnel_id = $(sql_quote "$tunnel") LIMIT 1;"
}

validate_device() {
    init_db
    device_name=$1
    require_arg "$device_name" "device name"

    exists=$(sqlite3 -noheader "$INVENTORY_DB" "SELECT COUNT(*) FROM devices WHERE device_name = $(sql_quote "$device_name");")
    if [ "$exists" -eq 0 ]; then
        echo "ERROR: device not found in inventory: $device_name" >&2
        exit 1
    fi

    missing=""
    for column in asset_tag expected_tunnel expected_worker tailscale_name; do
        value=$(value_for_device "$device_name" "$column")
        if [ -z "$value" ]; then
            missing="$missing $column"
        fi
    done

    if [ -n "$missing" ]; then
        echo "ERROR: device $device_name is missing required inventory fields:$missing" >&2
        exit 1
    fi

    expected_tunnel=$(value_for_device "$device_name" expected_tunnel)
    tunnel_owner_value=$(tunnel_owner "$expected_tunnel")
    if [ -n "$tunnel_owner_value" ] && [ "$tunnel_owner_value" != "$device_name" ]; then
        echo "ERROR: expected tunnel $expected_tunnel belongs to $tunnel_owner_value, not $device_name" >&2
        exit 1
    fi

    echo "OK: device $device_name has identity fields"
    echo "    asset_tag=$(value_for_device "$device_name" asset_tag)"
    echo "    tailscale_name=$(value_for_device "$device_name" tailscale_name)"
    echo "    expected_tunnel=$expected_tunnel"
    echo "    expected_worker=$(value_for_device "$device_name" expected_worker)"
}

validate_publish() {
    init_db
    device_name=$1
    domain=$2
    tunnel=$3
    require_arg "$device_name" "device name"
    require_arg "$domain" "domain"
    require_arg "$tunnel" "tunnel name or id"

    validate_device "$device_name" >/dev/null

    owner=$(domain_owner "$domain")
    if [ -z "$owner" ]; then
        echo "ERROR: domain is not recorded in inventory: $domain" >&2
        exit 1
    fi
    if [ "$owner" != "$device_name" ]; then
        echo "ERROR: domain $domain belongs to $owner, not $device_name" >&2
        exit 1
    fi

    owner=$(tunnel_owner "$tunnel")
    if [ -z "$owner" ]; then
        echo "ERROR: tunnel is not recorded in inventory: $tunnel" >&2
        exit 1
    fi
    if [ "$owner" != "$device_name" ]; then
        echo "ERROR: tunnel $tunnel belongs to $owner, not $device_name" >&2
        exit 1
    fi

    expected_tunnel=$(value_for_device "$device_name" expected_tunnel)
    actual_tunnel=$(tunnel_name_for "$tunnel")
    if [ -n "$actual_tunnel" ] && [ "$actual_tunnel" != "$expected_tunnel" ]; then
        echo "ERROR: tunnel $actual_tunnel does not match expected tunnel $expected_tunnel for $device_name" >&2
        exit 1
    fi

    echo "OK: publish guard passed"
    echo "    device=$device_name"
    echo "    domain=$domain"
    echo "    tunnel=${actual_tunnel:-$tunnel}"
}

case "$ACTION" in
    init)
        init_db
        echo "$INVENTORY_DB"
        ;;
    next-name)
        shift
        next_name "${1:-}" "${2:-localbox}"
        ;;
    upsert-device)
        shift
        upsert_device "$@"
        ;;
    upsert-domain)
        shift
        upsert_domain "$@"
        ;;
    upsert-tunnel)
        shift
        upsert_tunnel "$@"
        ;;
    upsert-route)
        shift
        upsert_route "$@"
        ;;
    phase)
        shift
        record_phase "$@"
        ;;
    reconcile-counters)
        reconcile_counters
        ;;
    reserve-devices)
        shift
        reserve_devices "$@"
        ;;
    list)
        shift
        list_table "${1:-}"
        ;;
    show-device)
        shift
        show_device "${1:-}"
        ;;
    device-field)
        shift
        value_for_device "${1:-}" "${2:-}"
        ;;
    lookup-tag)
        shift
        lookup_tag "${1:-}"
        ;;
    show-domain)
        shift
        show_domain "${1:-}"
        ;;
    domain-field)
        shift
        value_for_domain "${1:-}" "${2:-}"
        ;;
    domain-owner)
        shift
        domain_owner "${1:-}"
        ;;
    tunnel-owner)
        shift
        tunnel_owner "${1:-}"
        ;;
    validate-device)
        shift
        validate_device "${1:-}"
        ;;
    validate-publish)
        shift
        validate_publish "${1:-}" "${2:-}" "${3:-}"
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
