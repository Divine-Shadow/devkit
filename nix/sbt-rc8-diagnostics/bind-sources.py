"""Bind public patched/formatter-transformed inputs without build-local paths."""
import argparse
import datetime
import hashlib
import json
import pathlib
import shutil

CONTROL_FILES = [
    "build.sbt", "project/Utils.scala", "project/plugins.sbt",
    "project/build.properties", ".scalafmt.conf",
]
SOURCE_ROOTS = ["main-actions/src/main", "main-actions/src/test"]


def inputs(root):
    paths = [root / name for name in CONTROL_FILES]
    source_roots = [root / name for name in SOURCE_ROOTS]
    entries = [*source_roots, *(p for source in source_roots for p in source.rglob("*"))]
    # Source is an exact public Git/NAR input; reject links before payload reads.
    # Inspect directories too: a suffix-filtered traversal can miss a linked
    # directory that the compiler's source scanner might follow.
    ancestry = {ancestor for path in [*paths, *entries] for ancestor in [path, *path.parents]}
    for path in ancestry:
        if path.is_symlink():
            raise ValueError(f"source symlink is outside the producer contract: {path}")
    paths.extend(p for p in entries if p.is_file() and p.suffix in {".scala", ".java"})
    rows = []
    for path in sorted(paths):
        data = path.read_bytes()
        rows.append({"path": str(path.relative_to(root)), "bytes": len(data),
                     "executable": bool(path.stat().st_mode & 0o111),
                     "sha256": hashlib.sha256(data).hexdigest()})
    return rows


def capture(root, output):
    rows = inputs(root)
    output.mkdir(parents=True, exist_ok=False)
    for item in rows:
        destination = output / "sources" / item["path"]
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(root / item["path"], destination)
        shutil.copymode(root / item["path"], destination)
    (output / "manifest.json").write_text(json.dumps({"files": rows}, indent=2) + "\n")


def check(root, output):
    expected = json.loads((output / "manifest.json").read_text())["files"]
    if inputs(root) != expected:
        raise ValueError("effective source changed after formatting/before artifact publication")


def check_generated(root, epoch, output):
    expected = datetime.datetime.fromtimestamp(epoch, datetime.timezone.utc).strftime("%Y%m%dT%H%M%S")
    paths = sorted(root.rglob("xsbt.version.properties"))
    if not paths:
        raise ValueError("no generated version resources found after source compilation")
    rows = []
    for path in paths:
        if any(p.is_symlink() for p in [path, *path.parents]):
            raise ValueError("generated version resource must not traverse a symlink")
        values = dict(line.split("=", 1) for line in path.read_text().splitlines() if "=" in line)
        if values.get("timestamp") != expected:
            raise ValueError(f"generated version timestamp differs from fixed source epoch: {path}")
        rows.append({"path": str(path.relative_to(root)), "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
                     "timestamp": expected})
    output.write_text(json.dumps({"sourceEpoch": epoch, "versionResources": rows}, indent=2) + "\n")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("command", choices=["capture", "check", "check-generated"])
    parser.add_argument("root", type=pathlib.Path)
    parser.add_argument("output", type=pathlib.Path)
    parser.add_argument("--epoch", type=int)
    args = parser.parse_args()
    root = args.root.absolute()
    if args.command == "capture":
        capture(root, args.output)
    elif args.command == "check":
        check(root, args.output)
    else:
        if args.epoch is None:
            parser.error("check-generated requires --epoch")
        check_generated(root, args.epoch, args.output)
