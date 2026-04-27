#!/usr/bin/env bash
set -euo pipefail

BASE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PORTS_ENV="${WRITEUPS_PORTS:-9001 9002 9003 9004 9005}"
PORT_INFO_FILE="$BASE_DIR/port.info"
LOG_FILE="$BASE_DIR/writeups-http.log"

IFS=' ' read -r -a PORTS_ARR <<< "$PORTS_ENV"
PORT_CHOSEN=""

for p in "${PORTS_ARR[@]}"; do
  if PORT_CAND=$(python3 - <<PY
import socket
p = int(${p})
try:
    s = socket.socket()
    s.bind(('127.0.0.1', p))
    s.close()
    print(p)
except OSError:
    pass
PY
); then
    if [ -n "$PORT_CAND" ]; then
      PORT_CHOSEN="$PORT_CAND"
      break
    fi
  fi
done

if [ -z "$PORT_CHOSEN" ]; then
  printf 'No free port found in: %s\n' "$PORTS_ENV" >&2
  exit 1
fi

printf '%s\n' "$PORT_CHOSEN" > "$PORT_INFO_FILE"

nohup env PORT="$PORT_CHOSEN" go run . -transport http > "$LOG_FILE" 2>&1 &

printf 'Started Writeups MCP HTTP server on port %s (PID: %s)\n' "$PORT_CHOSEN" "$!"
