#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
INVENTORY_DB=${INVENTORY_DB:-$SCRIPT_DIR/state/localbox-inventory.sqlite}
OUT_DIR=${OUT_DIR:-$SCRIPT_DIR/state/labels}
DEVICE_NAME=${1:-}

if [ -z "$DEVICE_NAME" ]; then
    echo "Usage: $0 DEVICE_NAME" >&2
    exit 1
fi
if [ ! -f "$INVENTORY_DB" ]; then
    echo "ERROR: inventory DB not found: $INVENTORY_DB" >&2
    exit 1
fi

mkdir -p "$OUT_DIR"

python3 - "$INVENTORY_DB" "$DEVICE_NAME" "$OUT_DIR" <<'PY'
import html
import os
import sqlite3
import sys

db_path, device_name, out_dir = sys.argv[1:4]

patterns = {
    "0": "nnnwwnwnn", "1": "wnnwnnnnw", "2": "nnwwnnnnw", "3": "wnwwnnnnn",
    "4": "nnnwwnnnw", "5": "wnnwwnnnn", "6": "nnwwwnnnn", "7": "nnnwnnwnw",
    "8": "wnnwnnwnn", "9": "nnwwnnwnn", "A": "wnnnnwnnw", "B": "nnwnnwnnw",
    "C": "wnwnnwnnn", "D": "nnnnwwnnw", "E": "wnnnwwnnn", "F": "nnwnwwnnn",
    "G": "nnnnnwwnw", "H": "wnnnnwwnn", "I": "nnwnnwwnn", "J": "nnnnwwwnn",
    "K": "wnnnnnnww", "L": "nnwnnnnww", "M": "wnwnnnnwn", "N": "nnnnwnnww",
    "O": "wnnnwnnwn", "P": "nnwnwnnwn", "Q": "nnnnnnwww", "R": "wnnnnnwwn",
    "S": "nnwnnnwwn", "T": "nnnnwnwwn", "U": "wwnnnnnnw", "V": "nwwnnnnnw",
    "W": "wwwnnnnnn", "X": "nwnnwnnnw", "Y": "wwnnwnnnn", "Z": "nwwnwnnnn",
    "-": "nwnnnnwnw", ".": "wwnnnnwnn", " ": "nwwnnnwnn", "$": "nwnwnwnnn",
    "/": "nwnwnnnwn", "+": "nwnnnwnwn", "%": "nnnwnwnwn", "*": "nwnnwnwnn",
}

def code39_svg(value, x, y, narrow=2, height=70):
    encoded = "*" + value.upper() + "*"
    bad = [c for c in encoded if c not in patterns]
    if bad:
        raise SystemExit(f"Unsupported Code39 characters: {bad!r}")
    parts = []
    cursor = x
    for char in encoded:
        pattern = patterns[char]
        for i, width_code in enumerate(pattern):
            width = narrow * (3 if width_code == "w" else 1)
            if i % 2 == 0:
                parts.append(f'<rect x="{cursor}" y="{y}" width="{width}" height="{height}" fill="#000"/>')
            cursor += width
        cursor += narrow
    return "\n".join(parts), cursor - x

con = sqlite3.connect(db_path)
con.row_factory = sqlite3.Row
row = con.execute("SELECT * FROM devices WHERE device_name = ?", (device_name,)).fetchone()
if row is None:
    raise SystemExit(f"Device not found: {device_name}")

asset_tag = row["asset_tag"] or device_name.upper()
barcode_y = 118
barcode_height = 62
barcode, barcode_width = code39_svg(asset_tag, 24, barcode_y, height=barcode_height)
width = max(420, int(barcode_width + 48))
height = 250
lines = [
    ("Device", row["device_name"] or ""),
    ("Asset", asset_tag),
    ("Host", row["hostname"] or row["pi_host"] or ""),
    ("Tailnet", row["tailscale_name"] or ""),
]

text = []
y = 28
text.append(f'<text x="24" y="{y}" font-size="20" font-family="Arial, sans-serif" font-weight="700">LocalBox</text>')
y += 22
for label, value in lines:
    if value:
        text.append(f'<text x="24" y="{y}" font-size="13" font-family="Arial, sans-serif"><tspan font-weight="700">{html.escape(label)}:</tspan> {html.escape(value)}</text>')
        y += 16
text.append(f'<text x="24" y="206" font-size="18" font-family="Arial, sans-serif" font-weight="700">{html.escape(asset_tag)}</text>')
text.append(f'<text x="24" y="228" font-size="11" font-family="Arial, sans-serif">Lookup: ./localbox-inventory.sh lookup-tag {html.escape(asset_tag)}</text>')

svg = f'''<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="{width}" height="{height}" viewBox="0 0 {width} {height}">
  <rect width="100%" height="100%" fill="#fff"/>
  <rect x="8" y="8" width="{width-16}" height="{height-16}" fill="none" stroke="#111" stroke-width="1"/>
  {"".join(text)}
  {barcode}
</svg>
'''

safe = "".join(c if c.isalnum() or c in "-_" else "_" for c in asset_tag)
out = os.path.join(out_dir, f"{safe}.svg")
with open(out, "w", encoding="utf-8") as f:
    f.write(svg)
print(out)
PY
