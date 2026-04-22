#!/usr/bin/env bash
set -euo pipefail

# Run the Writeups MCP FastAPI service with autoselect port.
# It tries ports from WRITEUPS_PORTS (space-separated) or defaults 9001..9005.
# Writes chosen port to $BASE_DIR/port.info and prints it to stdout.

BASE_DIR="/home/Serebr1k/writeups-mcp-opencode"
DB_PATH="${WRITEUPS_DB:-$BASE_DIR/data/writeups_index.db}"
PORTS_ENV="${WRITEUPS_PORTS:-9001 9002 9003 9004 9005}"

# find a free port from list
choose_port() {
  python3 - <<PY
import socket,sys
ports = sys.argv[1:]
for p in ports:
    s=socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    try:
        s.bind(('127.0.0.1', int(p)))
        s.close()
        print(p)
        sys.exit(0)
    except Exception:
        pass
sys.exit(1)
PY
}

IFS=' ' read -r -a PORTS_ARR <<< "$PORTS_ENV"
PORT_CHOSEN=""
for p in "${PORTS_ARR[@]}"; do
  if PORT_CAND=$(python3 - <<PY
import socket
p=int(${p})
try:
    s=socket.socket()
    s.bind(('127.0.0.1', p))
    s.close()
    print(p)
except:
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
  echo "No free port found in: $PORTS_ENV" >&2
  exit 1
fi

echo "Selected port: $PORT_CHOSEN"
mkdir -p "$BASE_DIR"
echo "$PORT_CHOSEN" > "$BASE_DIR/port.info"
export WRITEUPS_PORT="$PORT_CHOSEN"
export WRITEUPS_DB="$DB_PATH"

# Change to project directory so PYTHONPATH works
cd "$BASE_DIR"
export PYTHONPATH="$BASE_DIR"

# Detect uvicorn path
UVICORN_PATH="${UVICORN_PATH:-$(command -v uvicorn 2>/dev/null || echo /home/Serebr1k/.local/bin/uvicorn)}"

# Daemonize: run in background and exit immediately
nohup "$UVICORN_PATH" service:app --host 127.0.0.1 --port "$PORT_CHOSEN" > "$BASE_DIR/uvicorn.log" 2>&1 &
echo "Started uvicorn on port $PORT_CHOSEN (PID: $!)"
