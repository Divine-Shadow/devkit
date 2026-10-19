#!/usr/bin/env python3
"""Bind the declared immutable Go supplier for the complete source CI command."""
import os
from pathlib import Path
import shlex
import signal
import subprocess
import sys


def run(supplier, command, root):
    module = root / "cli/devctl"
    supplier = supplier.resolve(strict=True)
    for name in ("go.mod", "go.sum"):
        if (supplier / name).read_bytes() != (module / name).read_bytes():
            raise ValueError(f"immutable Go supplier does not match checkout {name}")
    payload = (supplier / "vendor").resolve(strict=True)
    if not payload.is_dir() or not (payload / "modules.txt").is_file():
        raise ValueError("immutable Go supplier has no vendor manifest")
    flags = os.environ.get("GOFLAGS", "")
    if any(flag.startswith("-modfile") for flag in shlex.split(flags)):
        raise ValueError("alternate module declarations are incompatible with this supplier")
    env = dict(os.environ, GOFLAGS=(flags + " -mod=vendor").strip())
    vendor = module / "vendor"
    # Atomic exclusive creation refuses files, directories and dangling links.
    vendor.symlink_to(payload, target_is_directory=True)
    owned = vendor.lstat()
    previous = {}
    child = None
    cancelled = []

    def forward(signum, _frame):
        cancelled.append(signum)
        if child is not None:
            try:
                os.killpg(child.pid, signum)
            except ProcessLookupError:
                pass

    try:
        for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
            previous[signum] = signal.signal(signum, forward)
        child = subprocess.Popen(command, cwd=root, env=env, start_new_session=True)
        # A signal handler can run while Popen is creating the process. It only
        # records cancellation until the process-group handle is available.
        if cancelled:
            try:
                os.killpg(child.pid, cancelled[-1])
            except ProcessLookupError:
                pass
        code = child.wait()
    finally:
        for signum, handler in previous.items():
            signal.signal(signum, handler)
        try:
            current = vendor.lstat()
        except FileNotFoundError:
            current = None
        if cancelled:
            # Waiting for make alone cannot prove every descendant has exited.
            # Retain the binding after interruption; a later run refuses it.
            print(f"interrupted CI: retained dependency binding for inspection: {vendor}", file=sys.stderr)
        elif current is not None and (current.st_dev, current.st_ino) == (owned.st_dev, owned.st_ino):
            vendor.unlink()
    if cancelled and code == 0:
        return 128 + cancelled[-1]
    return code if code >= 0 else 128 - code


def main():
    if len(sys.argv) < 4 or sys.argv[2] != "--":
        raise ValueError("usage: devctl-ci-dependencies.py SUPPLIER -- COMMAND [ARG ...]")
    root = Path(__file__).resolve().parent.parent
    return run(Path(sys.argv[1]), sys.argv[3:], root)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError) as error:
        print(f"devctl CI dependency binding refused: {error}", file=sys.stderr)
        sys.exit(1)
