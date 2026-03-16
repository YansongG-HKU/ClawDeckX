#!/bin/bash
set -e

# Start OpenClaw Gateway in background if installed
if command -v openclaw &>/dev/null; then
    # Resolve config path (same logic as Go code)
    OPENCLAW_STATE_DIR="${XDG_STATE_HOME:-$HOME/.local/state}/openclaw"
    OPENCLAW_CONFIG="$OPENCLAW_STATE_DIR/openclaw.json"

    if [ -f "$OPENCLAW_CONFIG" ]; then
        echo "[docker-entrypoint] OpenClaw detected, starting gateway..."
        nohup openclaw gateway run --port 18789 > /tmp/openclaw-gateway.log 2>&1 &
        # Wait briefly for gateway to be ready
        for i in $(seq 1 10); do
            if curl -sf http://127.0.0.1:18789/health &>/dev/null; then
                echo "[docker-entrypoint] OpenClaw gateway started successfully"
                break
            fi
            sleep 1
        done
    else
        echo "[docker-entrypoint] OpenClaw installed but not configured, skipping gateway auto-start"
    fi
fi

# Start ClawDeckX (exec replaces shell so tini can manage signals)
exec /app/clawdeckx serve "$@"
