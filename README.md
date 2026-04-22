Writeups MCP for OpenCode
=========================

This project provides:
- a cleaner indexer for the Kraber writeups knowledge base
- a FastAPI search service exposing endpoints agents can call
- a Python client wrapper + CLI

Build index:
  python3 build_index.py --source /home/Serebr1k/kraber/knowledge_base --db data/writeups_index.db

Run service:
  uvicorn service:app --host 0.0.0.0 --port 9001

Search from Python:
  from client import WriteupsClient
  c = WriteupsClient('http://localhost:9001')
  c.search('privilege escalation')

OpenCode integration
--------------------

Add the following block into your .config/opencode/config.json under the "mcp" object:

  "writeups-mcp": {
    "enabled": true,
    "type": "local",
    "command": [
      "/home/Serebr1k/venv-writeups-mcp/bin/uvicorn",
      "service:app",
      "--host",
      "127.0.0.1",
      "--port",
      "9001"
    ],
    "environment": {
      "WRITEUPS_DB": "/home/Serebr1k/writeups-mcp-opencode/data/writeups_index.db",
      "PYTHONPATH": "/home/Serebr1k/writeups-mcp-opencode"
    }
  }

This will start the FastAPI service as an MCP. Agents can then POST to http://127.0.0.1:9001/search with JSON {"q":"query","limit":10}.

Auto-port selection
-------------------

When launching via run.sh (recommended wrapper), the service will pick a free port from the list in the environment variable WRITEUPS_PORTS (space-separated) or the default range 9001..9005. The chosen port is written to /home/Serebr1k/writeups-mcp-opencode/port.info.

If you use the direct uvicorn command in OpenCode config.json, keep port in sync with WRITEUPS_DB and port chosen by run.sh (or call run.sh in command instead).
