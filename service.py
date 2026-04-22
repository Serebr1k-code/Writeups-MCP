import os
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
import sqlite3
from typing import List

app = FastAPI(title="Writeups MCP")


class SearchRequest(BaseModel):
    q: str
    limit: int = 10


def conn(db_path: str = None):
    # allow configuration via env var WRITEUPS_DB, otherwise fallback to packaged path
    db = db_path or os.getenv(
        "WRITEUPS_DB", "/home/Serebr1k/writeups-mcp-opencode/data/writeups_index.db"
    )
    if not os.path.exists(db):
        raise FileNotFoundError(f"DB file not found: {db}")
    c = sqlite3.connect(db)
    # set row factory for convenience
    c.row_factory = sqlite3.Row
    return c


@app.post("/search")
def search(req: SearchRequest):
    if not req.q:
        raise HTTPException(status_code=400, detail="Empty query")
    try:
        c = conn()
    except FileNotFoundError as e:
        raise HTTPException(status_code=500, detail=str(e))
    cur = c.cursor()
    # use snippet to return short highlighted fragment
    cur.execute(
        "SELECT rowid, snippet(docs_fts, -1, '...', '...', '...', 10) as snippet, path FROM docs_fts WHERE docs_fts MATCH ? LIMIT ?",
        (req.q, req.limit),
    )
    rows = cur.fetchall()
    c.close()
    out = []
    for r in rows:
        out.append({"id": r["rowid"], "snippet": r["snippet"], "path": r["path"]})
    return {"hits": out}
