#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Unit tests for mg_memory_letta.py (Letta Hierarchical & Episodic Memory Adapter)."""

import os
import sys
import tempfile
import time
import unittest
from pathlib import Path

# Add python/letta to sys.path
root_dir = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(root_dir / "python" / "letta"))

from mg_memory_letta import LettaMemoryAdapter, CoreMemory, EpisodicMemory, ArchivalMemory


class TestCoreMemory(unittest.TestCase):
    def setUp(self):
        self.adapter = LettaMemoryAdapter(":memory:")
        self.core = self.adapter.core

    def tearDown(self):
        self.adapter.close()

    def test_default_blocks_initialized(self):
        blocks = self.core.list_blocks()
        self.assertIn("persona", blocks)
        self.assertIn("human", blocks)
        self.assertIn("scratchpad", blocks)

    def test_set_and_get_block(self):
        self.core.set_block("goals", "Complete Letta memory implementation.")
        block = self.core.get_block("goals")
        self.assertIsNotNone(block)
        self.assertEqual(block["name"], "goals")
        self.assertEqual(block["content"], "Complete Letta memory implementation.")

    def test_append_block(self):
        self.core.set_block("notes", "First point.")
        self.core.append_block("notes", "Second point.")
        block = self.core.get_block("notes")
        self.assertIn("First point.\nSecond point.", block["content"])

    def test_replace_in_block(self):
        self.core.set_block("facts", "Target OS is Linux.")
        self.core.replace_in_block("facts", "Linux", "Windows")
        block = self.core.get_block("facts")
        self.assertEqual(block["content"], "Target OS is Windows.")

    def test_replace_missing_substring_raises(self):
        self.core.set_block("facts", "Target OS is Windows.")
        with self.assertRaises(ValueError):
            self.core.replace_in_block("facts", "macOS", "Linux")

    def test_compile_prompt(self):
        compiled = self.core.compile_prompt()
        self.assertTrue(compiled.startswith("<core_memory>"))
        self.assertTrue(compiled.endswith("</core_memory>"))
        self.assertIn("<persona>", compiled)
        self.assertIn("<human>", compiled)


class TestEpisodicMemory(unittest.TestCase):
    def setUp(self):
        self.adapter = LettaMemoryAdapter(":memory:")
        self.episodic = self.adapter.episodic

    def tearDown(self):
        self.adapter.close()

    def test_log_and_recent(self):
        ev1 = self.episodic.log_event(
            role="user",
            content="Hello Microgravity agent.",
            event_type="message",
            tags=["greeting"],
        )
        self.assertEqual(ev1["role"], "user")
        self.assertEqual(ev1["content"], "Hello Microgravity agent.")

        recent = self.episodic.get_recent(limit=5)
        self.assertEqual(len(recent), 1)
        self.assertEqual(recent[0]["id"], ev1["id"])

    def test_query_filtering(self):
        self.episodic.log_event(role="user", content="Build database adapter", session_id="s1", importance=4.0)
        self.episodic.log_event(role="assistant", content="Database adapter ready", session_id="s1", importance=5.0)
        self.episodic.log_event(role="user", content="Deploy frontend", session_id="s2", importance=2.0)

        # Filter by session
        s1_events = self.episodic.query_episodes(session_id="s1")
        self.assertEqual(len(s1_events), 2)

        # Filter by text search
        db_events = self.episodic.query_episodes(query="database")
        self.assertEqual(len(db_events), 2)

        # Filter by importance
        high_imp = self.episodic.query_episodes(min_importance=4.5)
        self.assertEqual(len(high_imp), 1)
        self.assertEqual(high_imp[0]["role"], "assistant")

    def test_compaction(self):
        now = time.time()
        for i in range(5):
            self.episodic.log_event(
                role="user",
                content=f"Message step {i}",
                session_id="session_compaction",
                timestamp=now - (10 - i),
            )

        res = self.episodic.compact_episodes(
            session_id="session_compaction",
            before_timestamp=now - 2,
            summary_text="Summarized prior steps 0 through 3.",
        )
        self.assertGreaterEqual(res["pruned_events"], 3)
        self.assertIn("checkpoint", res)

        remaining = self.episodic.query_episodes(session_id="session_compaction")
        # Should contain checkpoint plus remaining recent messages
        self.assertTrue(any(e["event_type"] == "checkpoint" for e in remaining))


class TestArchivalMemory(unittest.TestCase):
    def setUp(self):
        self.adapter = LettaMemoryAdapter(":memory:")
        self.archival = self.adapter.archival

    def tearDown(self):
        self.adapter.close()

    def test_insert_and_search(self):
        p1 = self.archival.insert_passage(
            content="Gemma 2 9B is an open weights LLM developed by Google DeepMind.",
            tags=["models", "gemma"],
            source="docs",
        )
        p2 = self.archival.insert_passage(
            content="Letta is an OS-inspired memory architecture for autonomous AI agents.",
            tags=["memory", "architecture"],
            source="letta_spec",
        )

        # Search for Gemma
        results = self.archival.search_passages("DeepMind Gemma LLM")
        self.assertGreaterEqual(len(results), 1)
        self.assertEqual(results[0]["id"], p1["id"])

        # Tag filter
        mem_results = self.archival.search_passages("architecture", tag_filter="memory")
        self.assertEqual(len(mem_results), 1)
        self.assertEqual(mem_results[0]["id"], p2["id"])

    def test_delete_passage(self):
        p = self.archival.insert_passage("Temporary passage to delete.")
        self.assertTrue(self.archival.delete_passage(p["id"]))
        self.assertFalse(self.archival.delete_passage(p["id"]))


class TestLettaHandleRequest(unittest.TestCase):
    def setUp(self):
        self.adapter = LettaMemoryAdapter(":memory:")

    def tearDown(self):
        self.adapter.close()

    def test_dispatch_core_and_stats(self):
        res = self.adapter.handle_request({
            "op": "core_set",
            "block": "scratchpad",
            "content": "Updated via dispatch",
        })
        self.assertEqual(res["status"], "success")

        stats_res = self.adapter.handle_request({"op": "stats"})
        self.assertEqual(stats_res["status"], "success")
        self.assertIn("core_memory", stats_res["stats"])


if __name__ == "__main__":
    unittest.main()
