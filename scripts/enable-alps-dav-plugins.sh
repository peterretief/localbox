#!/bin/sh
set -eu

ALPS_CONFIG=${ALPS_CONFIG:-/mnt/data/alps-test/config.toml}
RADICALE_URL=${RADICALE_URL:-http://127.0.0.1:5232}

cp -p "$ALPS_CONFIG" "$ALPS_CONFIG.bak-dav-$(date +%Y%m%d%H%M%S)"
tmp=$(mktemp)
awk -v server="$RADICALE_URL" '
    function flush_plugin() {
        if (section == "caldav") {
            print "[plugin.caldav]"
            print "enabled = true"
            print "server = \"" server "\""
        } else if (section == "carddav") {
            print "[plugin.carddav]"
            print "enabled = true"
            print "server = \"" server "\""
        }
    }
    /^\[plugin\.caldav\]/ {
        flush_plugin()
        section = "caldav"
        next
    }
    /^\[plugin\.carddav\]/ {
        flush_plugin()
        section = "carddav"
        next
    }
    /^\[/ {
        flush_plugin()
        section = ""
        print
        next
    }
    (section == "caldav" || section == "carddav") { next }
    { print }
    END {
        flush_plugin()
    }
' "$ALPS_CONFIG" > "$tmp"
cat "$tmp" > "$ALPS_CONFIG"
rm -f "$tmp"

rc-service alps restart
sleep 1
rc-service alps status

echo "Enabled ALPS CalDAV/CardDAV plugins against $RADICALE_URL."
