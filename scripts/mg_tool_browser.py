#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Forwarder to python/browser/mg_tool_browser.py."""
import sys
from pathlib import Path

root_dir = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(root_dir / "python" / "browser"))

import mg_tool_browser

if __name__ == "__main__":
    mg_tool_browser.main()
