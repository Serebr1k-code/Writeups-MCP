#!/usr/bin/env python3
import sys
import json
import sqlite3
import os

DB_PATH = os.getenv(
    "WRITEUPS_DB", "/home/Serebr1k/writeups-mcp-opencode/data/writeups_index.db"
)


def search_db(query, limit=10):
    if not os.path.exists(DB_PATH):
        return [{"error": "DB not found: " + DB_PATH}]
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute(
        "SELECT rowid, snippet(docs_fts, -1, '===', '===', '...', 10) as snippet, path FROM docs_fts WHERE docs_fts MATCH ? LIMIT ?",
        (query, limit),
    )
    rows = cur.fetchall()
    conn.close()
    return [{"id": r[0], "snippet": r[1], "path": r[2]} for r in rows]


def handle_request(req):
    method = req.get("method")
    params = req.get("params", {})
    req_id = req.get("id")

    if method == "initialize":
        return {
            "result": {"protocolVersion": "2024-11-05", "capabilities": {}},
            "id": req_id,
        }
    elif method == "tools/list":
        tools = [
            {
                "name": "search",
                "description": "Search writeups knowledge base",
                "inputSchema": {
                    "type": "object",
                    "properties": {
                        "q": {"type": "string"},
                        "limit": {"type": "integer"},
                    },
                },
            }
        ]
        return {"result": {"tools": tools}, "id": req_id}
    elif method == "tools/call":
        name = params.get("name")
        args = params.get("arguments", {})
        if name == "search":
            result = search_db(args.get("q", ""), args.get("limit", 10))
            return {"result": result, "id": req_id}
        return {"error": "Unknown tool: " + name}
    elif method == "ping":
        return {"result": "pong", "id": req_id}
    return {"error": "Unknown method"}


def main():
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
            resp = handle_request(req)
            print(json.dumps(resp), flush=True)
        except Exception as e:
            print(json.dumps({"error": str(e)}), flush=True)


if __name__ == "__main__":
    main()
