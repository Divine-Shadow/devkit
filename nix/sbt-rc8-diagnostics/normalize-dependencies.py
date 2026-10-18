"""Normalize only build-local resolver caches for recursive hashing.

Artifact bytes are preserved. Do not point this at a consumer's live cache.
"""
import pathlib
import shutil
import sys


def normalize(root):
    # Same isolated-global cache exclusions as the existing immutable supplier.
    for name in ("cache", "staging"):
        path = root / "global" / name
        if path.is_symlink():
            raise ValueError(f"resolver cache symlink is outside this producer contract: {path}")
        if path.exists():
            shutil.rmtree(path)
    for path in sorted(root.rglob("*")):
        if path.is_symlink():
            raise ValueError(f"resolver cache symlink is outside this producer contract: {path}")
        if not path.is_file():
            continue
        if path.name.endswith((".lock", ".lck", ".lastUpdated", ".checked", ".computed", ".error")) or path.name in {
            "update.log", ".checked"
        }:
            path.unlink()
    # Global contains no user configuration: this HOME was made in the builder.
    # Keep plugin/artifact bytes; remove only known resolution/lock markers.


if __name__ == "__main__":
    normalize(pathlib.Path(sys.argv[1]))
