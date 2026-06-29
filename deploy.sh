#!/bin/sh
set -e

# ── Load settings ────────────────────────────────────────────────────────────
if [ -f "./settings.conf" ]; then
    . ./settings.conf
fi
PI_HOST=${PI_HOST:-192.168.0.5}
PI_USER=${PI_USER:-root}

echo "=== LocalBox Deploy ==="
echo "Target: $PI_USER@$PI_HOST"

# ── Build ────────────────────────────────────────────────────────────────────
echo ">> Building mail-ingest for ARM..."
GOOS=linux GOARCH=arm GOARM=7 go build -o mail-ingest .
echo "   Built successfully"

# ── Deploy binary ────────────────────────────────────────────────────────────
echo ">> Deploying binary..."
cat mail-ingest | ssh $PI_USER@$PI_HOST \
    'kill $(pgrep mail-ingest) 2>/dev/null; \
     rm -f /usr/local/bin/mail-ingest; \
     cat > /usr/local/bin/mail-ingest && \
     chmod +x /usr/local/bin/mail-ingest'
echo "   Binary deployed"

# ── Deploy templates ─────────────────────────────────────────────────────────
echo ">> Deploying templates..."
for f in templates/*.html; do
    fname=$(basename "$f")
    cat "$f" | ssh $PI_USER@$PI_HOST "cat > /etc/localbox/templates/$fname"
    echo "   Deployed $fname"
done

# ── Restart service ──────────────────────────────────────────────────────────
echo ">> Restarting service..."
ssh $PI_USER@$PI_HOST \
    'rm -f /run/mail-ingest.pid && \
     rc-service mail-ingest zap && \
     rc-service mail-ingest restart && \
     rc-service mail-ingest status'

# ── Commit to SD ─────────────────────────────────────────────────────────────
echo ">> Committing to SD card..."
ssh $PI_USER@$PI_HOST 'lbu commit -d'

echo ""
echo "=== Deploy complete ==="