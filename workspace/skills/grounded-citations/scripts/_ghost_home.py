"""Resolve GHOST_HOME for standalone skill scripts.

Returns the Ghost home directory (default: ``~/.ghost``).
"""
import os
from pathlib import Path


def get_ghost_home() -> Path:
    val = os.environ.get("GHOST_HOME", "").strip()
    return Path(val) if val else Path.home() / ".ghost"
