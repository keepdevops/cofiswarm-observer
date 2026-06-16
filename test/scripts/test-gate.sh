#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
[[ -d "${ROOT}/plugins" ]] || exit 1
FHS_PLUGINS="${COFISWARM_VAR_LIB:-$HOME/cofiswarm/fhs/var/lib}/cofiswarm/observer/plugins"
[[ -d "$FHS_PLUGINS" ]] || { echo "missing $FHS_PLUGINS"; exit 1; }
BIN="${ROOT}/bin/cofiswarm-observer"
PORT=18016
"$BIN" -listen ":$PORT" &
PID=$!
trap 'kill $PID 2>/dev/null' EXIT
sleep 1
curl -sf "http://127.0.0.1:$PORT/healthz" >/dev/null
curl -s "http://127.0.0.1:$PORT/v1/plugins" | grep -q plugins
echo "ok: observer plugins + FHS"
