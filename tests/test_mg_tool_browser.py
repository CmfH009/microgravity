#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Unit tests for mg_tool_browser.py (Browser Action Tool Integration)."""

import asyncio
import os
import sys
import unittest
from pathlib import Path

# Add project root to sys.path
root_dir = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(root_dir))

from mg_tool_browser import BrowserSessionManager, FallbackHTMLParser


class TestFallbackHTMLParser(unittest.TestCase):
    def test_html_parsing_and_markdown(self):
        sample_html = """
        <!DOCTYPE html>
        <html>
        <head><title>Microgravity Test Page</title></head>
        <body>
            <h1>Welcome to Microgravity</h1>
            <p>This is a paragraph with a <a href="https://example.com/docs">documentation link</a>.</p>
            <form action="/search">
                <input type="text" name="q" placeholder="Search docs..." />
                <button type="submit" id="submit-btn">Search</button>
            </form>
            <img src="logo.png" alt="Microgravity Logo" />
        </body>
        </html>
        """
        parser = FallbackHTMLParser()
        parser.feed(sample_html)
        
        self.assertEqual(parser.title.strip(), "Microgravity Test Page")
        markdown = parser.get_markdown()
        self.assertIn("# Welcome to Microgravity", markdown)
        self.assertIn("documentation link", markdown)

        # Interactive elements
        self.assertGreaterEqual(len(parser.elements), 3)
        tags = [el.tag for el in parser.elements]
        self.assertIn("a", tags)
        self.assertIn("input", tags)
        self.assertIn("button", tags)


class TestBrowserSessionManager(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.session = BrowserSessionManager(headless=True)

    async def asyncTearDown(self):
        await self.session.close()

    async def test_navigate_and_extract(self):
        res = await self.session.navigate("https://example.com")
        self.assertEqual(res["status"], "success")
        self.assertIn("Example Domain", res["title"])

        extract_res = await self.session.extract(extract_links=True)
        self.assertEqual(extract_res["status"], "success")
        self.assertIn("Example Domain", extract_res["markdown"])
        self.assertGreaterEqual(len(extract_res["links"]), 1)

    async def test_search_page(self):
        await self.session.navigate("https://example.com")
        search_res = await self.session.search_page(pattern="documentation examples")
        self.assertEqual(search_res["status"], "success")
        self.assertGreaterEqual(search_res["count"], 1)

    async def test_find_elements(self):
        await self.session.navigate("https://example.com")
        find_res = await self.session.find_elements(selector="a")
        self.assertEqual(find_res["status"], "success")
        self.assertGreaterEqual(find_res["count"], 1)

    async def test_tabs_management(self):
        tabs_res = await self.session.get_tabs()
        self.assertEqual(tabs_res["status"], "success")
        self.assertEqual(len(tabs_res["tabs"]), 1)

        # Navigate with new tab
        nav2 = await self.session.navigate("https://example.com", new_tab=True)
        self.assertEqual(nav2["status"], "success")
        
        tabs_res2 = await self.session.get_tabs()
        self.assertEqual(len(tabs_res2["tabs"]), 2)

        # Switch tab
        sw = await self.session.switch_tab("tab1")
        self.assertEqual(sw["status"], "success")
        self.assertEqual(sw["active_tab_id"], "tab1")

        # Close tab
        cl = await self.session.close_tab("tab2")
        self.assertEqual(cl["status"], "success")
        self.assertNotIn("tab2", self.session.tabs)
        self.assertNotIn("tab2", self.session._playwright_pages)

        # Attempting to close only remaining tab returns error
        cl_only = await self.session.close_tab("tab1")
        self.assertEqual(cl_only["status"], "error")

    async def test_composite_action_plan(self):
        plan = [
            {"action": "navigate", "url": "https://example.com"},
            {"action": "search_page", "pattern": "Domain"},
            {"action": "find_elements", "selector": "a"},
        ]
        plan_res = await self.session.execute_plan(plan)
        self.assertEqual(plan_res["status"], "success")
        self.assertEqual(plan_res["steps_completed"], 3)


if __name__ == "__main__":
    unittest.main()
