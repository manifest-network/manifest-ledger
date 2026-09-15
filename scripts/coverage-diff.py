#!/usr/bin/env python3
"""Gate Go statements in blocks intersecting added lines in a complete Git diff."""

import argparse
from bisect import bisect_left
from collections import defaultdict
from decimal import Decimal, InvalidOperation
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys


GENERATED_SUFFIXES = (".pb.go", ".pb.gw.go", ".pulsar.go")
BLOCK = re.compile(r"(.+):(\d+)\.(\d+),(\d+)\.(\d+) (\d+) (\d+)")
HUNK = re.compile(r"@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@(?:.*)")


def git(repo, *args):
    return subprocess.run(
        ["git", "--literal-pathspecs", "-C", str(repo), *args],
        check=True, capture_output=True,
    ).stdout.decode()


def revision(repo, value):
    resolved = git(repo, "rev-parse", "--verify", "--end-of-options", value + "^{commit}").strip()
    if not re.fullmatch(r"[0-9a-f]{40}|[0-9a-f]{64}", resolved):
        raise ValueError("revision did not resolve to one commit")
    return resolved


def eligible(path):
    return path.endswith(".go") and not path.endswith(("_test.go", *GENERATED_SUFFIXES))


def parse_profile(profile, module):
    lines = profile.splitlines()
    if not lines or lines[0] not in ("mode: set", "mode: count", "mode: atomic"):
        raise ValueError("invalid Go coverage profile mode")
    files = defaultdict(list)
    for number, line in enumerate(lines[1:], 2):
        match = BLOCK.fullmatch(line)
        if not match:
            raise ValueError(f"malformed Go coverage block on profile line {number}")
        name, start_line, start_column, end_line, end_column, statements, count = match.groups()
        if not name.startswith(module + "/"):
            raise ValueError(f"profile path is outside module {module}: {name}")
        path = name[len(module) + 1:]
        if (
            not path or PurePosixPath(path).is_absolute()
            or any(part in ("", ".", "..") for part in path.split("/"))
            or "\x00" in path or not path.endswith(".go")
        ):
            raise ValueError(f"invalid profile source path: {path!r}")
        start = (int(start_line), int(start_column))
        end = (int(end_line), int(end_column))
        statements, count = int(statements), int(count)
        # Go emits zero-length, zero-statement blocks for empty branches.
        if min(*start, *end) <= 0 or start > end or (start == end and statements):
            raise ValueError(f"invalid source range on profile line {number}")
        if lines[0] == "mode: set" and count > 1:
            raise ValueError("set-mode coverage count must be zero or one")
        files[path].append((start, end, statements, count))
    for path, blocks in files.items():
        blocks.sort()
        previous_end = None
        locations = set()
        for start, end, _, _ in blocks:
            if (start, end) in locations or (previous_end is not None and start < previous_end):
                raise ValueError(f"duplicate or overlapping blocks in merged profile: {path}")
            locations.add((start, end))
            previous_end = end
    if not any(block[2] for path, blocks in files.items() if eligible(path) for block in blocks):
        raise ValueError("profile contains no eligible Go statements")
    return files


def added_lines(repo, base, head, path):
    # Literal pathspecs and a separate argv element preserve spaces, tabs,
    # glob characters, and leading dashes in tracked filenames.
    patch = git(
        repo, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--text",
        "--no-color", "--inter-hunk-context=0", "--unified=0", base, head, "--", path,
    )
    added = set()
    for line in patch.split("\n"):
        match = HUNK.fullmatch(line)
        if match:
            start, length = int(match[1]), int(match[2] or 1)
            added.update(range(start, start + length))
    return sorted(added)


def analyze(profile, repo, base, head):
    base, head = revision(repo, base), revision(repo, head)
    go_mod = git(repo, "show", head + ":go.mod")
    module = re.search(r'^module\s+"?([^\s"]+)"?\s*$', go_mod, re.MULTILINE)
    if not module:
        raise ValueError("head go.mod has no module declaration")
    files = parse_profile(profile, module[1])
    changed = git(
        repo, "diff", "--no-ext-diff", "--no-renames", "--name-only", "-z", base, head,
    ).split("\x00")
    changed = sorted(path for path in changed if path)
    rows = []
    absent = []
    eligible_files = 0
    added_count = 0
    for path in changed:
        if not eligible(path):
            continue
        added = added_lines(repo, base, head, path)
        if not added:
            continue
        eligible_files += 1
        added_count += len(added)
        if path not in files:
            absent.append(path)
            continue
        hits, statements, blocks = 0, 0, 0
        for start, end, size, count in files[path]:
            index = bisect_left(added, start[0])
            if index < len(added) and added[index] <= end[0] and size:
                blocks += 1
                statements += size
                hits += size if count else 0
        if blocks:
            rows.append((path, hits, statements, blocks))
    return {
        "base": base, "head": head, "changed_files": len(changed),
        "eligible_files": eligible_files, "added_lines": added_count,
        "rows": rows, "absent": absent,
        "covered": sum(row[1] for row in rows),
        "statements": sum(row[2] for row in rows),
    }


def render(result, floor):
    print("Full Git diff coverage: **Go statements in changed blocks**.\n")
    print(f"Base: `{result['base']}`. Head: `{result['head']}`.\n")
    print(
        f"Scope: {result['changed_files']} changed files, "
        f"{result['eligible_files']} eligible Go files with added lines, "
        f"{result['added_lines']} added eligible source lines. "
        "Git reads the full local comparison without an API file limit.\n"
    )
    print(
        "Count each nonempty profile block once when its inclusive line range "
        "intersects an added line; weight it by its Go statement count. "
        "Renames count as deletion plus addition. Generated protobuf and Go test "
        "files are excluded. This is not Codecov line coverage; its separate "
        "project and patch requirements still apply.\n"
    )
    print("| File | Covered statements | Coverage | Changed blocks |\n| --- | ---: | ---: | ---: |")
    for path, hits, total, blocks in result["rows"]:
        label = path.replace("&", "&amp;").replace("<", "&lt;").replace("|", "&#124;")
        label = label.replace("`", "&#96;").replace("\t", "&#9;").replace("\n", "&#10;")
        print(f"| {label} | {hits}/{total} | {100 * hits / total:.2f}% | {blocks} |")
    if result["absent"]:
        print("\nChanged Go files with no blocks in the supplied profile. They contribute no statements to this metric; coverage-profile generation must establish source completeness:")
        for path in result["absent"]:
            print(f"\n- {path!r}")
    total, covered = result["statements"], result["covered"]
    if not total:
        print("\nChanged-block coverage: **N/A — no eligible statements intersect added lines**. No percentage is claimed.")
        return 0
    print(f"\nChanged-block statements: **{covered}/{total} ({100 * covered / total:.2f}%)**, required: **{floor:g}%**.")
    return 0 if Decimal(100 * covered) >= floor * total else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("profile", type=Path)
    parser.add_argument("base")
    parser.add_argument("head")
    parser.add_argument("floor")
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    args = parser.parse_args()
    floor = Decimal(args.floor)
    if not floor.is_finite() or not 0 <= floor <= 100:
        raise ValueError("coverage floor must be a finite percentage between zero and 100")
    result = analyze(args.profile.read_text(), args.repo, args.base, args.head)
    return render(result, floor)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, InvalidOperation, subprocess.CalledProcessError) as error:
        print(f"full-diff coverage validation failed: {error}", file=sys.stderr)
        sys.exit(2)
