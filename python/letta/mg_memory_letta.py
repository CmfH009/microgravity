#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Microgravity Letta Memory Module Adapter (mg_memory_letta.py)

Implements Letta (f.k.a. MemGPT) hierarchical agent memory architecture:
1. Core Memory: In-context, block-based editable working memory (human, persona, scratchpad).
2. Episodic / Recall Memory: Temporal, structured event and message logs with importance scoring,
   tag indexing, and semantic / keyword relevance retrieval.
3. Archival Memory: Long-term passage / knowledge storage with TF-IDF / vector search ranking.

Provides both a programmatic Python API and a JSON-RPC / CLI subprocess bridge for Microgravity.
"""

from __future__ import annotations

import argparse
import collections
import datetime
import json
import math
import os
import re
import sqlite3
import sys
import time
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple, Union


# ==============================================================================
# Helper Utilities: Text Processing & Similarity Scoring
# ==============================================================================

def tokenize(text: str) -> List[str]:
    """Tokenize and normalize text into lowercase word tokens."""
    if not text:
        return []
    return re.findall(r"\b\w+\b", text.lower())


def compute_bm25_similarity(query_tokens: List[str], doc_tokens: List[str], avg_doc_len: float = 50.0, k1: float = 1.5, b: float = 0.75) -> float:
    """Compute BM25-inspired relevance score between query tokens and document tokens."""
    if not query_tokens or not doc_tokens:
        return 0.0
    
    doc_len = len(doc_tokens)
    doc_counts = collections.Counter(doc_tokens)
    score = 0.0

    for token in query_tokens:
        if token in doc_counts:
            tf = doc_counts[token]
            num = tf * (k1 + 1)
            denom = tf + k1 * (1 - b + b * (doc_len / max(avg_doc_len, 1.0)))
            score += (num / denom)
    
    return score


# ==============================================================================
# 1. Core Memory: Block-Based In-Context Working Memory
# ==============================================================================

class CoreMemory:
    """
    Manages in-context, editable memory blocks (e.g. human, persona, scratchpad).
    Changes to core memory directly influence system prompt context.
    """
    DEFAULT_BLOCKS = {
        "persona": "Microgravity autonomous engineering agent equipped with Gemma inference, Letta hierarchical memory, and Browser-Use tools.",
        "human": "Primary developer and workspace operator.",
        "scratchpad": "Ready for active tasks.",
    }

    def __init__(self, conn: sqlite3.Connection):
        self.conn = conn
        self._ensure_schema()

    def _ensure_schema(self) -> None:
        with self.conn:
            self.conn.execute("""
                CREATE TABLE IF NOT EXISTS core_memory_blocks (
                    name TEXT PRIMARY KEY,
                    content TEXT NOT NULL,
                    char_limit INTEGER NOT NULL DEFAULT 2000,
                    updated_at REAL NOT NULL
                )
            """)
            # Initialize default blocks if empty
            cur = self.conn.execute("SELECT COUNT(*) FROM core_memory_blocks")
            if cur.fetchone()[0] == 0:
                now = time.time()
                for name, default_val in self.DEFAULT_BLOCKS.items():
                    self.conn.execute(
                        "INSERT INTO core_memory_blocks (name, content, char_limit, updated_at) VALUES (?, ?, ?, ?)",
                        (name, default_val, 2000, now),
                    )

    def get_block(self, name: str) -> Optional[Dict[str, Any]]:
        cur = self.conn.execute("SELECT name, content, char_limit, updated_at FROM core_memory_blocks WHERE name = ?", (name,))
        row = cur.fetchone()
        if not row:
            return None
        return {
            "name": row[0],
            "content": row[1],
            "char_limit": row[2],
            "updated_at": row[3],
        }

    def set_block(self, name: str, content: str, char_limit: int = 2000) -> Dict[str, Any]:
        if len(content) > char_limit:
            raise ValueError(f"Content exceeds block character limit ({len(content)} > {char_limit})")
        now = time.time()
        with self.conn:
            self.conn.execute("""
                INSERT INTO core_memory_blocks (name, content, char_limit, updated_at)
                VALUES (?, ?, ?, ?)
                ON CONFLICT(name) DO UPDATE SET
                    content = excluded.content,
                    char_limit = excluded.char_limit,
                    updated_at = excluded.updated_at
            """, (name, content, char_limit, now))
        return self.get_block(name)  # type: ignore

    def append_block(self, name: str, text_to_append: str) -> Dict[str, Any]:
        existing = self.get_block(name)
        char_limit = 2000
        if existing:
            new_content = existing["content"] + "\n" + text_to_append if existing["content"] else text_to_append
            char_limit = existing["char_limit"]
        else:
            new_content = text_to_append

        if len(new_content) > char_limit:
            raise ValueError(f"Appending exceeds block limit ({len(new_content)} > {char_limit})")
        return self.set_block(name, new_content, char_limit)

    def replace_in_block(self, name: str, old_str: str, new_str: str) -> Dict[str, Any]:
        existing = self.get_block(name)
        if not existing:
            raise KeyError(f"Core memory block '{name}' does not exist")
        
        if old_str not in existing["content"]:
            raise ValueError(f"Substring '{old_str}' not found in block '{name}'")
        
        updated_content = existing["content"].replace(old_str, new_str, 1)
        if len(updated_content) > existing["char_limit"]:
            raise ValueError(f"Replacement exceeds block limit ({len(updated_content)} > {existing['char_limit']})")
        
        return self.set_block(name, updated_content, existing["char_limit"])

    def list_blocks(self) -> Dict[str, str]:
        cur = self.conn.execute("SELECT name, content FROM core_memory_blocks ORDER BY name ASC")
        return {row[0]: row[1] for row in cur.fetchall()}

    def compile_prompt(self) -> str:
        """Renders core memory as XML blocks for system prompt injection."""
        blocks = self.list_blocks()
        lines = ["<core_memory>"]
        for name, content in sorted(blocks.items()):
            lines.append(f"  <{name}>")
            for cl in content.strip().splitlines():
                lines.append(f"    {cl}")
            lines.append(f"  </{name}>")
        lines.append("</core_memory>")
        return "\n".join(lines)


# ==============================================================================
# 2. Episodic / Recall Memory: Temporal Event & Message Logs
# ==============================================================================

class EpisodicMemory:
    """
    Manages episodic memory (interaction stream, user commands, agent actions,
    observations, thoughts, and checkpoints) with temporal, keyword, and importance filtering.
    """
    def __init__(self, conn: sqlite3.Connection):
        self.conn = conn
        self._ensure_schema()

    def _ensure_schema(self) -> None:
        with self.conn:
            self.conn.execute("""
                CREATE TABLE IF NOT EXISTS episodic_events (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    session_id TEXT NOT NULL,
                    timestamp REAL NOT NULL,
                    created_at TEXT NOT NULL,
                    role TEXT NOT NULL,
                    event_type TEXT NOT NULL,
                    content TEXT NOT NULL,
                    importance REAL NOT NULL DEFAULT 1.0,
                    tags TEXT NOT NULL DEFAULT '[]',
                    metadata TEXT NOT NULL DEFAULT '{}'
                )
            """)
            self.conn.execute("CREATE INDEX IF NOT EXISTS idx_ep_session ON episodic_events(session_id)")
            self.conn.execute("CREATE INDEX IF NOT EXISTS idx_ep_time ON episodic_events(timestamp)")
            self.conn.execute("CREATE INDEX IF NOT EXISTS idx_ep_role ON episodic_events(role)")

    def log_event(
        self,
        role: str,
        content: str,
        event_type: str = "message",
        session_id: str = "default",
        tags: Optional[List[str]] = None,
        importance: float = 1.0,
        metadata: Optional[Dict[str, Any]] = None,
        timestamp: Optional[float] = None,
    ) -> Dict[str, Any]:
        """Record an episodic event."""
        if not role or not role.strip():
            raise ValueError("Role cannot be empty")
        if content is None:
            content = ""

        ts = timestamp if timestamp is not None else time.time()
        created_at = datetime.datetime.fromtimestamp(ts, tz=datetime.timezone.utc).isoformat()
        tags_json = json.dumps(tags or [])
        meta_json = json.dumps(metadata or {})

        with self.conn:
            cur = self.conn.execute("""
                INSERT INTO episodic_events (
                    session_id, timestamp, created_at, role, event_type, content, importance, tags, metadata
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, (session_id, ts, created_at, role, event_type, content, float(importance), tags_json, meta_json))
            event_id = cur.lastrowid

        return {
            "id": event_id,
            "session_id": session_id,
            "timestamp": ts,
            "created_at": created_at,
            "role": role,
            "event_type": event_type,
            "content": content,
            "importance": importance,
            "tags": tags or [],
            "metadata": metadata or {},
        }

    def get_recent(self, session_id: Optional[str] = None, limit: int = 20) -> List[Dict[str, Any]]:
        """Retrieve recent episodic events."""
        if session_id:
            cur = self.conn.execute("""
                SELECT id, session_id, timestamp, created_at, role, event_type, content, importance, tags, metadata
                FROM episodic_events
                WHERE session_id = ?
                ORDER BY timestamp DESC
                LIMIT ?
            """, (session_id, limit))
        else:
            cur = self.conn.execute("""
                SELECT id, session_id, timestamp, created_at, role, event_type, content, importance, tags, metadata
                FROM episodic_events
                ORDER BY timestamp DESC
                LIMIT ?
            """, (limit,))
        
        rows = cur.fetchall()
        # Return in chronological order
        rows.reverse()
        return [self._row_to_event(r) for r in rows]

    def query_episodes(
        self,
        query: Optional[str] = None,
        session_id: Optional[str] = None,
        role: Optional[str] = None,
        event_type: Optional[str] = None,
        tags: Optional[List[str]] = None,
        start_time: Optional[float] = None,
        end_time: Optional[float] = None,
        min_importance: float = 0.0,
        limit: int = 50,
        offset: int = 0,
    ) -> List[Dict[str, Any]]:
        """Filter episodic memory with metadata and text query."""
        clauses = ["importance >= ?"]
        params: List[Any] = [min_importance]

        if session_id:
            clauses.append("session_id = ?")
            params.append(session_id)
        if role:
            clauses.append("role = ?")
            params.append(role)
        if event_type:
            clauses.append("event_type = ?")
            params.append(event_type)
        if start_time is not None:
            clauses.append("timestamp >= ?")
            params.append(start_time)
        if end_time is not None:
            clauses.append("timestamp <= ?")
            params.append(end_time)

        where_sql = " AND ".join(clauses)
        sql = f"""
            SELECT id, session_id, timestamp, created_at, role, event_type, content, importance, tags, metadata
            FROM episodic_events
            WHERE {where_sql}
            ORDER BY timestamp DESC
            LIMIT ? OFFSET ?
        """
        params.extend([limit * 2 if query else limit, offset])

        cur = self.conn.execute(sql, params)
        raw_events = [self._row_to_event(r) for r in cur.fetchall()]

        # Filter by tags if specified
        if tags:
            tag_set = set(tags)
            raw_events = [e for e in raw_events if tag_set.intersection(set(e["tags"]))]

        # Score & rank by text query if specified
        if query:
            q_tokens = tokenize(query)
            scored = []
            for ev in raw_events:
                d_tokens = tokenize(ev["content"])
                score = compute_bm25_similarity(q_tokens, d_tokens)
                if score > 0.05 or any(t in ev["content"].lower() for t in q_tokens):
                    ev_copy = dict(ev)
                    ev_copy["relevance_score"] = round(score, 4)
                    scored.append((score, ev_copy))
            scored.sort(key=lambda x: x[0], reverse=True)
            return [x[1] for x in scored[:limit]]

        raw_events.reverse()
        return raw_events[:limit]

    def compact_episodes(self, session_id: str, before_timestamp: float, summary_text: str) -> Dict[str, Any]:
        """
        Compacts older episodes in a session into a single summary checkpoint event,
        pruning individual messages to prevent unbounded token expansion.
        """
        with self.conn:
            cur = self.conn.execute("""
                SELECT COUNT(*) FROM episodic_events
                WHERE session_id = ? AND timestamp < ?
            """, (session_id, before_timestamp))
            count_pruned = cur.fetchone()[0]

            self.conn.execute("""
                DELETE FROM episodic_events
                WHERE session_id = ? AND timestamp < ?
            """, (session_id, before_timestamp))

        # Insert compacted summary checkpoint
        checkpoint = self.log_event(
            role="system",
            content=f"[Compacted History ({count_pruned} events)]: {summary_text}",
            event_type="checkpoint",
            session_id=session_id,
            importance=5.0,
            tags=["compacted_memory", "checkpoint"],
            metadata={"pruned_count": count_pruned, "before_timestamp": before_timestamp},
            timestamp=before_timestamp,
        )
        return {
            "checkpoint": checkpoint,
            "pruned_events": count_pruned,
        }

    def _row_to_event(self, row: Tuple[Any, ...]) -> Dict[str, Any]:
        tags = []
        try:
            tags = json.loads(row[8])
        except Exception:
            pass
        meta = {}
        try:
            meta = json.loads(row[9])
        except Exception:
            pass

        return {
            "id": row[0],
            "session_id": row[1],
            "timestamp": row[2],
            "created_at": row[3],
            "role": row[4],
            "event_type": row[5],
            "content": row[6],
            "importance": row[7],
            "tags": tags,
            "metadata": meta,
        }


# ==============================================================================
# 3. Archival Memory: Long-Term Knowledge & Semantic Store
# ==============================================================================

class ArchivalMemory:
    """
    Stores long-term facts, documentation snippets, and knowledge passages with
    tagging, source provenance, and TF-IDF / BM25 semantic retrieval.
    """
    def __init__(self, conn: sqlite3.Connection):
        self.conn = conn
        self._ensure_schema()

    def _ensure_schema(self) -> None:
        with self.conn:
            self.conn.execute("""
                CREATE TABLE IF NOT EXISTS archival_passages (
                    id TEXT PRIMARY KEY,
                    content TEXT NOT NULL,
                    tags TEXT NOT NULL DEFAULT '[]',
                    source TEXT NOT NULL DEFAULT 'user',
                    created_at REAL NOT NULL,
                    metadata TEXT NOT NULL DEFAULT '{}'
                )
            """)
            self.conn.execute("CREATE INDEX IF NOT EXISTS idx_arch_created ON archival_passages(created_at)")

    def insert_passage(
        self,
        content: str,
        tags: Optional[List[str]] = None,
        source: str = "agent",
        passage_id: Optional[str] = None,
        metadata: Optional[Dict[str, Any]] = None,
    ) -> Dict[str, Any]:
        if not content or not content.strip():
            raise ValueError("Passage content cannot be empty")

        pid = passage_id or f"arch_{int(time.time() * 1000)}_{abs(hash(content)) % 10000:04d}"
        now = time.time()
        tags_json = json.dumps(tags or [])
        meta_json = json.dumps(metadata or {})

        with self.conn:
            self.conn.execute("""
                INSERT INTO archival_passages (id, content, tags, source, created_at, metadata)
                VALUES (?, ?, ?, ?, ?, ?)
                ON CONFLICT(id) DO UPDATE SET
                    content = excluded.content,
                    tags = excluded.tags,
                    source = excluded.source,
                    created_at = excluded.created_at,
                    metadata = excluded.metadata
            """, (pid, content, tags_json, source, now, meta_json))

        return {
            "id": pid,
            "content": content,
            "tags": tags or [],
            "source": source,
            "created_at": now,
            "metadata": metadata or {},
        }

    def search_passages(self, query: str, top_k: int = 5, tag_filter: Optional[str] = None) -> List[Dict[str, Any]]:
        """Search archival memory passages using BM25 relevance and keyword matching."""
        if not query or not query.strip():
            return []

        cur = self.conn.execute("SELECT id, content, tags, source, created_at, metadata FROM archival_passages")
        rows = cur.fetchall()
        if not rows:
            return []

        q_tokens = tokenize(query)
        scored: List[Tuple[float, Dict[str, Any]]] = []

        for r in rows:
            tags = []
            try:
                tags = json.loads(r[2])
            except Exception:
                pass

            if tag_filter and tag_filter not in tags:
                continue

            content = r[1]
            doc_tokens = tokenize(content)
            score = compute_bm25_similarity(q_tokens, doc_tokens)

            # Boost if query appears as exact substring
            if query.lower() in content.lower():
                score += 2.0

            if score > 0.01:
                meta = {}
                try:
                    meta = json.loads(r[5])
                except Exception:
                    pass
                scored.append((
                    score,
                    {
                        "id": r[0],
                        "content": content,
                        "tags": tags,
                        "source": r[3],
                        "created_at": r[4],
                        "score": round(score, 4),
                        "metadata": meta,
                    }
                ))

        scored.sort(key=lambda x: x[0], reverse=True)
        return [item[1] for item in scored[:top_k]]

    def delete_passage(self, passage_id: str) -> bool:
        with self.conn:
            cur = self.conn.execute("DELETE FROM archival_passages WHERE id = ?", (passage_id,))
            return cur.rowcount > 0

    def list_passages(self, limit: int = 50) -> List[Dict[str, Any]]:
        cur = self.conn.execute("SELECT id, content, tags, source, created_at, metadata FROM archival_passages ORDER BY created_at DESC LIMIT ?", (limit,))
        results = []
        for r in cur.fetchall():
            tags = []
            try:
                tags = json.loads(r[2])
            except Exception:
                pass
            meta = {}
            try:
                meta = json.loads(r[5])
            except Exception:
                pass
            results.append({
                "id": r[0],
                "content": r[1],
                "tags": tags,
                "source": r[3],
                "created_at": r[4],
                "metadata": meta,
            })
        return results


# ==============================================================================
# 4. Master Letta Memory Adapter
# ==============================================================================

class LettaMemoryAdapter:
    """
    Unified Letta Memory Adapter combining Core, Episodic, and Archival memory.
    """
    def __init__(self, db_path: Optional[str] = None):
        if db_path is None or db_path == ":memory:":
            self.db_path = ":memory:"
            self.conn = sqlite3.connect(":memory:", check_same_thread=False)
        else:
            path = Path(db_path).expanduser().resolve()
            path.parent.mkdir(parents=True, exist_ok=True)
            self.db_path = str(path)
            self.conn = sqlite3.connect(self.db_path, check_same_thread=False)
            self.conn.execute("PRAGMA journal_mode=WAL;")
            self.conn.execute("PRAGMA synchronous=NORMAL;")

        self.core = CoreMemory(self.conn)
        self.episodic = EpisodicMemory(self.conn)
        self.archival = ArchivalMemory(self.conn)

    def close(self) -> None:
        if self.conn:
            self.conn.close()

    def get_stats(self) -> Dict[str, Any]:
        """Returns storage statistics across all memory tiers."""
        cur_core = self.conn.execute("SELECT COUNT(*), SUM(LENGTH(content)) FROM core_memory_blocks")
        core_count, core_bytes = cur_core.fetchone()
        
        cur_ep = self.conn.execute("SELECT COUNT(*), COUNT(DISTINCT session_id) FROM episodic_events")
        ep_count, session_count = cur_ep.fetchone()

        cur_arch = self.conn.execute("SELECT COUNT(*), SUM(LENGTH(content)) FROM archival_passages")
        arch_count, arch_bytes = cur_arch.fetchone()

        return {
            "database": self.db_path,
            "core_memory": {
                "blocks_count": core_count or 0,
                "total_bytes": core_bytes or 0,
                "blocks": list(self.core.list_blocks().keys()),
            },
            "episodic_memory": {
                "events_count": ep_count or 0,
                "sessions_count": session_count or 0,
            },
            "archival_memory": {
                "passages_count": arch_count or 0,
                "total_bytes": arch_bytes or 0,
            },
        }

    # High-level dispatch for CLI / Tool Bridge
    def handle_request(self, request: Dict[str, Any]) -> Dict[str, Any]:
        op = request.get("op")
        if not op:
            return {"status": "error", "error": "Missing 'op' operation field"}

        try:
            # ── Core Memory Ops ──
            if op == "core_get":
                block = request.get("block")
                if not block:
                    return {"status": "error", "error": "Missing 'block' parameter"}
                res = self.core.get_block(block)
                return {"status": "success", "block": res}

            elif op == "core_set":
                block = request.get("block")
                content = request.get("content", "")
                limit = int(request.get("char_limit", 2000))
                if not block:
                    return {"status": "error", "error": "Missing 'block' parameter"}
                res = self.core.set_block(block, content, limit)
                return {"status": "success", "block": res}

            elif op == "core_append":
                block = request.get("block")
                content = request.get("content", "")
                if not block or not content:
                    return {"status": "error", "error": "Both 'block' and 'content' are required"}
                res = self.core.append_block(block, content)
                return {"status": "success", "block": res}

            elif op == "core_replace":
                block = request.get("block")
                old_str = request.get("old_str")
                new_str = request.get("new_str", "")
                if not block or old_str is None:
                    return {"status": "error", "error": "'block' and 'old_str' are required"}
                res = self.core.replace_in_block(block, old_str, new_str)
                return {"status": "success", "block": res}

            elif op == "core_list":
                blocks = self.core.list_blocks()
                return {"status": "success", "blocks": blocks}

            elif op == "core_compile":
                compiled = self.core.compile_prompt()
                return {"status": "success", "compiled": compiled}

            # ── Episodic Memory Ops ──
            elif op == "episodic_log":
                role = request.get("role", "user")
                content = request.get("content", "")
                event_type = request.get("event_type", "message")
                session_id = request.get("session_id", "default")
                tags = request.get("tags", [])
                importance = float(request.get("importance", 1.0))
                metadata = request.get("metadata", {})
                res = self.episodic.log_event(
                    role=role,
                    content=content,
                    event_type=event_type,
                    session_id=session_id,
                    tags=tags,
                    importance=importance,
                    metadata=metadata,
                )
                return {"status": "success", "event": res}

            elif op == "episodic_recent":
                session_id = request.get("session_id")
                limit = int(request.get("limit", 20))
                res = self.episodic.get_recent(session_id=session_id, limit=limit)
                return {"status": "success", "events": res, "count": len(res)}

            elif op == "episodic_query":
                query = request.get("query")
                session_id = request.get("session_id")
                role = request.get("role")
                event_type = request.get("event_type")
                tags = request.get("tags")
                min_importance = float(request.get("min_importance", 0.0))
                limit = int(request.get("limit", 20))
                res = self.episodic.query_episodes(
                    query=query,
                    session_id=session_id,
                    role=role,
                    event_type=event_type,
                    tags=tags,
                    min_importance=min_importance,
                    limit=limit,
                )
                return {"status": "success", "events": res, "count": len(res)}

            elif op == "episodic_compact":
                session_id = request.get("session_id", "default")
                before_timestamp = float(request.get("before_timestamp", time.time()))
                summary_text = request.get("summary_text", "")
                res = self.episodic.compact_episodes(session_id, before_timestamp, summary_text)
                return {"status": "success", "compact_result": res}

            # ── Archival Memory Ops ──
            elif op == "archival_insert":
                content = request.get("content", "")
                tags = request.get("tags", [])
                source = request.get("source", "agent")
                passage_id = request.get("passage_id")
                metadata = request.get("metadata", {})
                res = self.archival.insert_passage(
                    content=content,
                    tags=tags,
                    source=source,
                    passage_id=passage_id,
                    metadata=metadata,
                )
                return {"status": "success", "passage": res}

            elif op == "archival_search":
                query = request.get("query", "")
                top_k = int(request.get("top_k", 5))
                tag_filter = request.get("tag_filter")
                res = self.archival.search_passages(query, top_k=top_k, tag_filter=tag_filter)
                return {"status": "success", "results": res, "count": len(res)}

            elif op == "archival_delete":
                passage_id = request.get("passage_id")
                if not passage_id:
                    return {"status": "error", "error": "Missing 'passage_id'"}
                ok = self.archival.delete_passage(passage_id)
                return {"status": "success", "deleted": ok}

            elif op == "archival_list":
                limit = int(request.get("limit", 50))
                res = self.archival.list_passages(limit=limit)
                return {"status": "success", "passages": res, "count": len(res)}

            # ── Meta / Stats Ops ──
            elif op == "stats":
                return {"status": "success", "stats": self.get_stats()}

            else:
                return {"status": "error", "error": f"Unknown memory operation '{op}'"}

        except Exception as e:
            return {"status": "error", "error": str(e), "op": op}


# ==============================================================================
# CLI / Stdin Subprocess Bridge Entrypoint
# ==============================================================================

def main():
    parser = argparse.ArgumentParser(description="Microgravity Letta Memory Subprocess Bridge")
    parser.add_argument("--db", type=str, default=None, help="Path to SQLite memory database")
    parser.add_argument("--op", type=str, default=None, help="Memory operation name")
    parser.add_argument("--json", type=str, default=None, help="Raw JSON payload for operation")
    args = parser.parse_args()

    default_db = args.db
    if not default_db:
        # Default to workspace memory location
        default_db = os.environ.get("MG_MEMORY_DB", os.path.expanduser("~/.microgravity/letta_memory.db"))

    adapter = LettaMemoryAdapter(default_db)

    # Read from CLI arg or stdin
    payload: Dict[str, Any] = {}
    if args.json:
        try:
            payload = json.loads(args.json)
        except json.JSONDecodeError as e:
            print(json.dumps({"status": "error", "error": f"Invalid CLI JSON: {e}"}))
            sys.exit(1)
    elif args.op:
        payload["op"] = args.op
    else:
        # Read from stdin
        raw = sys.stdin.read().strip()
        if raw:
            try:
                payload = json.loads(raw)
            except json.JSONDecodeError as e:
                print(json.dumps({"status": "error", "error": f"Invalid stdin JSON: {e}"}))
                sys.exit(1)
        else:
            payload["op"] = "stats"

    if args.op and "op" not in payload:
        payload["op"] = args.op

    res = adapter.handle_request(payload)
    print(json.dumps(res, indent=2 if sys.stdout.isatty() else None))
    adapter.close()


if __name__ == "__main__":
    main()
