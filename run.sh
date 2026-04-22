#!/usr/bin/env bash
set -euo pipefail

# Run the Writeups MCP FastAPI service
BASE_DIR="/home/Serebr1k/writeups-mcp-opencode"
DB_PATH="${WRITEUPS_DB:-$BASE_DIR/data/writeups_index.db}"

exec uvicorn service:app --host 127.0.0.1 --port 9001 --reload
