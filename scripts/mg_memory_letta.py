#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Forwarder to python/letta/mg_memory_letta.py."""
import sys
from pathlib import Path

root_dir = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(root_dir / "python" / "letta"))

import mg_memory_letta

if __name__ == "__main__":
    mg_memory_letta.main()
