#!/usr/bin/env python3
"""Report statement-weighted package coverage from a merged Go profile."""

from collections import defaultdict
from pathlib import Path
import sys


def summarize(profile):
    lines = profile.splitlines()
    if not lines or lines[0] not in ("mode: set", "mode: count", "mode: atomic"):
        raise ValueError("invalid Go coverage profile mode")
    packages = defaultdict(lambda: [0, 0])
    seen = set()
    for line in lines[1:]:
        location, statements, count = line.split()
        statements, count = int(statements), int(count)
        if statements < 0 or count < 0 or location in seen:
            raise ValueError("negative counts or duplicate blocks in merged profile")
        seen.add(location)
        package = location.rsplit(":", 1)[0].rsplit("/", 1)[0]
        packages[package][0] += statements if count else 0
        packages[package][1] += statements
    return packages


def main():
    if len(sys.argv) != 3:
        raise ValueError("usage: coverage-summary.py <merged-profile> <project-floor-percent>")
    packages = summarize(Path(sys.argv[1]).read_text())
    covered = sum(pair[0] for pair in packages.values())
    total = sum(pair[1] for pair in packages.values())
    floor = float(sys.argv[2])
    if not total or not 0 <= floor <= 100:
        raise ValueError("empty profile or invalid coverage floor")
    print("| Package | Covered statements | Coverage |\n| --- | ---: | ---: |")
    for package, (hits, size) in sorted(packages.items()):
        if size:
            print(f"| `{package}` | {hits}/{size} | {100 * hits / size:.2f}% |")
    print(f"\nProject: **{100 * covered / total:.2f}%**, required: **{floor:g}%**.")
    return 0 if 100 * covered / total >= floor else 1


if __name__ == "__main__":
    sys.exit(main())
