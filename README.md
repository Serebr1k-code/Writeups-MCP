# Writeups MCP for OpenCode

Go-based MCP server for searching and reading a local knowledge base with CTF writeups, HackTricks notes, exploit guides, cheat sheets, and other security documentation.

No JavaScript runtime is needed anymore. Main implementation is now fully on Go.

## What this project does

Server exposes three MCP tools:

- `search_writeups` - full-text search in the indexed SQLite database
- `read_writeup` - read a file by database id or by direct path
- `help` - quick built-in usage help

## Repository layout

```text
.
├── main.go                            # entrypoint, stdio and HTTP modes
├── go.mod
├── go.sum
├── internal/
│   ├── indexer/
│   │   └── indexer.go                 # optimized SQLite FTS index builder
│   ├── mcpserver/
│   │   └── server.go                  # MCP tool registration
│   └── writeups/
│       └── repository.go              # SQLite search and file reading logic
├── cmd/
│   └── build-index/
│       └── main.go                    # CLI for building the search index
├── scripts/
│   └── run-http.sh                    # helper that picks a free port and starts HTTP mode
├── deploy/
│   └── systemd/
│       └── writeups-mcp.service       # example systemd unit for HTTP mode
├── legacy/
│   └── python-http/                   # old experimental Python implementation
├── requirements.txt                   # only for Python index builder / legacy tools
└── README.md
```

## Requirements

- Go 1.26+
- SQLite database created by the Go index builder in `cmd/build-index`

## Environment variables

- `WRITEUPS_DB` - path to SQLite index file
- `PORT` - port for HTTP mode
- `WRITEUPS_HTTP_PORT` - fallback port for HTTP mode if `PORT` is not set
- `WRITEUPS_PORTS` - list of candidate ports for `scripts/run-http.sh`
- `HOST` - bind host for HTTP mode, default `127.0.0.1`

Default database path:

```text
~/writeups-mcp-opencode/data/writeups_index.db
```

## Build the search index

The index builder is now written in Go and is optimized for larger knowledge bases.

Basic usage:

```bash
go run ./cmd/build-index \
  --source /path/to/knowledge_base \
  --db /path/to/writeups_index.db
```

Build a standalone indexer binary:

```bash
go build -o writeups-indexer ./cmd/build-index
```

Then run it:

```bash
./writeups-indexer \
  --source /path/to/knowledge_base \
  --db /path/to/writeups_index.db
```

Useful flags:

- `--min-chars` - minimum cleaned text length, default `300`
- `--workers` - parallel file processing workers, default `runtime.NumCPU()`
- `--commit-every` - commit batch size, default `200`
- `--extensions` - file extensions to index, default `.md,.txt,.rst,.html`
- `--prune-deleted` - remove DB rows for files deleted from disk, default `true`

What is optimized:

- parallel file reading and preprocessing
- fast skip for unchanged files using stored size and mtime
- batched SQLite writes with WAL mode
- duplicate detection by content hash
- optional pruning of stale records

## Build the Go binary

```bash
go build -o writeups-mcp .
```

## Docker

Image uses Alpine for both build and runtime stages.

Build image:

```bash
docker build -t writeups-mcp .
```

Run HTTP mode with an existing database mounted into `/data`:

```bash
docker run --rm -p 9001:9001 \
  -e WRITEUPS_DB=/data/writeups_index.db \
  -v /absolute/path/to/data:/data \
  writeups-mcp
```

Container defaults:

- starts in HTTP mode
- binds to `0.0.0.0:9001`
- uses `/healthz` for Docker healthcheck

Healthcheck endpoint:

```text
http://127.0.0.1:9001/healthz
```

Inside the image:

- `writeups-mcp` - main MCP server binary
- `writeups-indexer` - index builder binary

Build an index inside the container:

```bash
docker run --rm \
  -v /absolute/path/to/knowledge_base:/kb:ro \
  -v /absolute/path/to/data:/data \
  --entrypoint /usr/local/bin/writeups-indexer \
  writeups-mcp \
  --source /kb \
  --db /data/writeups_index.db
```

## Run locally in stdio mode

This is the normal mode for OpenCode when MCP is launched as a local process.

```bash
WRITEUPS_DB=/absolute/path/to/writeups_index.db go run .
```

Or with explicit flag:

```bash
go run . -transport stdio -db /absolute/path/to/writeups_index.db
```

Using built binary:

```bash
WRITEUPS_DB=/absolute/path/to/writeups_index.db ./writeups-mcp -transport stdio
```

## Run in remote HTTP mode

Start the HTTP MCP server:

```bash
WRITEUPS_DB=/absolute/path/to/writeups_index.db go run . -transport http -host 127.0.0.1 -port 9001
```

Server endpoint:

```text
http://127.0.0.1:9001/mcp
```

Healthcheck endpoint:

```text
http://127.0.0.1:9001/healthz
```

Using built binary:

```bash
WRITEUPS_DB=/absolute/path/to/writeups_index.db ./writeups-mcp -transport http -port 9001
```

Helper script with automatic free-port selection:

```bash
WRITEUPS_DB=/absolute/path/to/writeups_index.db ./scripts/run-http.sh
```

Chosen port is written to `port.info`, logs go to `writeups-http.log`.

## OpenCode setup: local MCP

This is the recommended setup if OpenCode and the database are on the same machine.

Example config:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "writeups": {
      "type": "local",
      "enabled": true,
      "command": [
        "/absolute/path/to/Writeups-MCP/writeups-mcp",
        "-transport",
        "stdio"
      ],
      "environment": {
        "WRITEUPS_DB": "/absolute/path/to/writeups_index.db"
      },
      "timeout": 5000
    }
  }
}
```

If you do not want to prebuild the binary, you can run through Go directly:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "writeups": {
      "type": "local",
      "enabled": true,
      "command": [
        "go",
        "run",
        "/absolute/path/to/Writeups-MCP",
        "-transport",
        "stdio"
      ],
      "environment": {
        "WRITEUPS_DB": "/absolute/path/to/writeups_index.db"
      }
    }
  }
}
```

But compiled binary is better: faster startup, fewer moving parts.

## OpenCode setup: remote MCP

If you want OpenCode to connect to an already running HTTP server, first start remote mode and then add this config:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "writeups_remote": {
      "type": "remote",
      "enabled": true,
      "url": "http://127.0.0.1:9001/mcp",
      "timeout": 5000
    }
  }
}
```

If you later expose it behind HTTPS and auth, you can add headers:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "writeups_remote": {
      "type": "remote",
      "enabled": true,
      "url": "https://example.com/mcp",
      "headers": {
        "Authorization": "Bearer {env:WRITEUPS_MCP_TOKEN}"
      },
      "oauth": false
    }
  }
}
```

## What to use: local or remote

- use `local` if OpenCode and the database are on the same machine
- use `remote` if you want one shared MCP endpoint for several machines or users
- use compiled binary for local mode when possible
- use `remote` when you need a background service with central hosting

## Tool usage examples

Search:

```javascript
{ tool: "search_writeups", args: { query: "SQL injection", limit: 5 } }
```

Read by id:

```javascript
{ tool: "read_writeup", args: { id: 1, lines: "1-50" } }
```

Read by direct path:

```javascript
{ tool: "read_writeup", args: { path: "/path/to/file.md", lines: "120-180" } }
```

Help:

```javascript
{ tool: "help", args: { tool: "all" } }
```

## systemd example

Example unit file is in `deploy/systemd/writeups-mcp.service`.

Typical flow:

```bash
sudo cp deploy/systemd/writeups-mcp.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now writeups-mcp.service
```

Before enabling it, update paths inside the service file for your user, repository path, Go binary path or compiled binary path, and database path.

For production it is usually better to replace `go run . -transport http` with a compiled binary path.

## Legacy code

Old Python prototype files were preserved in `legacy/python-http` so they do not clutter the main implementation:

- `legacy/python-http/mcp_server.py`
- `legacy/python-http/service.py`
- `legacy/python-http/client.py`

They are not used by the current Go MCP server.

`requirements.txt` is now only relevant if you still want to keep old Python helper flow around locally.

## Quick troubleshooting

- `no such table: docs_fts` - database is missing or not built yet
- `writeup with id X not found` - database row does not exist
- local MCP does not start in OpenCode - verify absolute path to binary and `WRITEUPS_DB`
- remote MCP does not connect - verify server is running and `/mcp` is reachable
- `go run` is too slow in OpenCode - build the binary and point config to that binary instead

## Recommended minimal setup

For one workstation with OpenCode on the same machine:

1. build the SQLite index
2. build the Go binary with `go build -o writeups-mcp .`
3. configure OpenCode with `type: "local"`
4. point `WRITEUPS_DB` to the created database

That is the simplest and most reliable setup.
