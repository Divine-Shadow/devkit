#!/usr/bin/env python3
import json
import sys
from pathlib import Path


def overlay_local_flake(overlay: str) -> str:
    return f"./overlays/{overlay}#default"


def accepted_flakes(overlay: str) -> list[str]:
    return [overlay_local_flake(overlay)]


def clean_scalar(value: str) -> str:
    value = value.strip()
    if len(value) >= 2 and value[0] == value[-1] and value[0] in {"'", '"'}:
        return value[1:-1]
    return value


def parse_top_level_mapping(text: str, section_name: str) -> dict[str, str]:
    section = None
    values: dict[str, str] = {}
    lines = text.splitlines()
    index = 0
    while index < len(lines):
        raw_line = lines[index]
        index += 1
        if not raw_line.strip() or raw_line.lstrip().startswith("#"):
            continue
        indent = len(raw_line) - len(raw_line.lstrip(" "))
        stripped = raw_line.strip()
        if indent == 0 and stripped.endswith(":"):
            section = stripped[:-1]
            continue
        if section != section_name or indent == 0 or ":" not in stripped:
            continue
        key, value = stripped.split(":", 1)
        key = key.strip()
        value = value.strip()
        if value in {"|", ">"}:
            block_lines = []
            while index < len(lines):
                next_line = lines[index]
                next_indent = len(next_line) - len(next_line.lstrip(" "))
                if next_line.strip() and next_indent <= indent:
                    break
                block_lines.append(next_line[indent + 2 :] if len(next_line) >= indent + 2 else "")
                index += 1
            values[key] = "\n".join(block_lines).strip()
            continue
        values[key] = clean_scalar(value)
    return values


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: validate-overlay-runtimes.py <overlays-dir>", file=sys.stderr)
        return 2

    overlays_dir = Path(sys.argv[1])
    problems = []
    entries = []

    for devkit_yaml in sorted(overlays_dir.glob("*/devkit.yaml")):
        overlay = devkit_yaml.parent.name
        runtime = parse_top_level_mapping(devkit_yaml.read_text(), "runtime")
        flake = runtime.get("flake", "").strip()
        image = runtime.get("image", "").strip()
        core_check = runtime.get("core_check", "").strip()
        codex_version = runtime.get("codex_version", "").strip()
        runtime_nix = devkit_yaml.parent / "runtime.nix"
        overlay_flake = devkit_yaml.parent / "flake.nix"
        retired_overlay_file = devkit_yaml.parent / ("compose." + "override.yml")
        accepted = accepted_flakes(overlay)

        if not flake:
            problems.append(f"{overlay}: runtime.flake is required")
        elif flake not in accepted:
            problems.append(f"{overlay}: runtime.flake {flake!r} is not an accepted ref ({' or '.join(accepted)})")
        if image:
            problems.append(f"{overlay}: runtime.image is retired metadata; use runtime.flake")
        if not runtime_nix.exists():
            problems.append(f"{overlay}: missing per-overlay runtime.nix")
        if not overlay_flake.exists():
            problems.append(f"{overlay}: missing per-overlay flake.nix")
        if retired_overlay_file.exists():
            problems.append(f"{overlay}: retired overlay runtime file remains")
        if not core_check:
            problems.append(f"{overlay}: runtime.core_check is required")
        if not codex_version:
            problems.append(f"{overlay}: runtime.codex_version is required")

        entries.append(
            {
                "overlay": overlay,
                "flake": flake,
                "runtime_nix": str(runtime_nix.relative_to(overlays_dir.parent)),
                "overlay_flake": str(overlay_flake.relative_to(overlays_dir.parent)),
                "core_check": core_check,
                "codex_version": codex_version,
            }
        )

    if problems:
        for problem in problems:
            print(problem, file=sys.stderr)
        return 1

    print(json.dumps({"overlays": entries}, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
