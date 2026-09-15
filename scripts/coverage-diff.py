#!/usr/bin/env python3
"""Gate changed executable Go statements across a complete local Git diff."""

import argparse
import json
from bisect import bisect_right
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


def source_statements(sources):
    # Go's parser/scanner owns source syntax. Never execute source from the
    # compared commits, and keep comments/blank lines out of change credit.
    result = subprocess.run(
        ["go", "run", "./tools/coverage", "statements"],
        cwd=Path(__file__).resolve().parent.parent,
        input=json.dumps(sources), capture_output=True, text=True, check=True,
    )
    rows = json.loads(result.stdout)
    if not isinstance(rows, list) or len(rows) != len(sources):
        raise ValueError("source analyzer returned an incomplete file list")
    indexed = {row["path"]: row["statements"] or [] for row in rows}
    if len(indexed) != len(rows) or set(indexed) != {row["path"] for row in sources}:
        raise ValueError("source analyzer returned unexpected or duplicate files")
    return indexed


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
    additions = {}
    sources = []
    for path in changed:
        if eligible(path):
            added = added_lines(repo, base, head, path)
            if added:
                additions[path] = set(added)
                sources.append({"path": path, "source": git(repo, "show", head + ":" + path)})
    statements = source_statements(sources) if sources else {}
    rows, missing, declarations = [], {}, []
    for path, added in additions.items():
        logical = statements[path]
        if not logical:
            declarations.append(path)
            continue
        blocks = [block for block in files.get(path, []) if block[2]]
        starts = [block[0] for block in blocks]
        hits, total = 0, 0
        for statement in logical:
            position = (statement["line"], statement["column"])
            index = bisect_right(starts, position) - 1
            block = blocks[index] if index >= 0 and position < blocks[index][1] else None
            if block is None:
                missing.setdefault(path, []).append(position)
            if not added.intersection(statement["lines"]):
                continue
            total += 1
            hits += 1 if block is not None and block[3] else 0
        if total:
            rows.append((path, hits, total))
    return {
        "base": base, "head": head, "changed_files": len(changed),
        "eligible_files": len(additions), "added_lines": sum(map(len, additions.values())),
        "rows": rows, "missing": missing, "declarations": declarations,
        "covered": sum(row[1] for row in rows),
        "statements": sum(row[2] for row in rows),
    }


def render(result, floor):
    print("Full Git diff coverage: **changed executable Go statements**.\n")
    print(f"Base: `{result['base']}`. Head: `{result['head']}`.\n")
    print(
        f"Scope: {result['changed_files']} changed files, "
        f"{result['eligible_files']} eligible Go files with added lines, "
        f"{result['added_lines']} added eligible source lines. "
        "Git reads the full local comparison without an API file limit.\n"
    )
    print(
        "Count each logical executable statement once when an added line contains "
        "one of its own Go tokens. Nested bodies are measured separately; comments, "
        "blank lines, and unchanged source lines receive no credit. Statements sharing "
        "a changed physical line are counted together. Execution comes "
        "from the Go profile block containing its first token. This AST statement "
        "metric is distinct from Go NumStmt and Codecov line coverage. Generated "
        "protobuf and Go test files are excluded. Renames count as deletion plus addition.\n"
    )
    print("| File | Covered changed statements | Coverage |\n| --- | ---: | ---: |")
    for path, hits, total in result["rows"]:
        label = path.replace("&", "&amp;").replace("<", "&lt;").replace("|", "&#124;")
        label = label.replace("`", "&#96;").replace("\t", "&#9;").replace("\n", "&#10;")
        print(f"| {label} | {hits}/{total} | {100 * hits / total:.2f}% |")
    if result["declarations"]:
        print("\nChanged sources with no logical executable statements (declarations or empty bodies):")
        for path in result["declarations"]:
            print(f"\n- {path!r}")
    if result["missing"]:
        print("\n**FAIL: incomplete coverage evidence for executable source.** "
              "Every statement in each changed executable file needs a profile block. "
              "Collect its package/platform profile; omitted files are never waived automatically.")
        for path, positions in result["missing"].items():
            print(f"\n- {path!r}: {len(positions)} statement(s), first at {positions[0][0]}:{positions[0][1]}")
        return 1
    total, covered = result["statements"], result["covered"]
    if not total:
        print("\nChanged-statement coverage: **N/A — no executable statement tokens intersect added lines**. No percentage is claimed.")
        return 0
    print(f"\nChanged executable statements: **{covered}/{total} ({100 * covered / total:.2f}%)**, required: **{floor:g}%**.")
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
        if isinstance(error, subprocess.CalledProcessError) and error.stderr:
            print(error.stderr, file=sys.stderr)
        sys.exit(2)
