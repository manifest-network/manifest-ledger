#!/usr/bin/env python3
"""Read-only, fail-closed audit of the repository's release authorization policy."""

import json
from pathlib import Path
import subprocess
import sys


def matches(actual, expected):
    """Allow API metadata, but require every configured value and exact list membership."""
    if isinstance(expected, dict):
        return isinstance(actual, dict) and all(
            key in actual and matches(actual[key], value)
            for key, value in expected.items()
        )
    if isinstance(expected, list):
        if not isinstance(actual, list) or len(actual) != len(expected):
            return False
        remaining = list(actual)
        for item in expected:
            for index, candidate in enumerate(remaining):
                if matches(candidate, item):
                    remaining.pop(index)
                    break
            else:
                return False
        return True
    return type(actual) is type(expected) and actual == expected


def main():
    policy = json.loads(
        (Path(__file__).resolve().parent.parent / ".github/release-controls.json").read_text()
    )
    failed = []
    for endpoint, expected in policy["requirements"].items():
        path = f"repos/{policy['repository']}" + (f"/{endpoint}" if endpoint else "")
        try:
            response = subprocess.run(
                ["gh", "api", path], check=True, text=True, capture_output=True, timeout=30
            )
            if not matches(json.loads(response.stdout), expected):
                failed.append(f"{endpoint or 'repository'}: policy drift")
        except (OSError, subprocess.SubprocessError, ValueError) as error:
            failed.append(f"{endpoint or 'repository'}: API read failed ({type(error).__name__})")
    if failed:
        print("Release controls are not verified:\n" + "\n".join(failed), file=sys.stderr)
        return 1
    print(f"Release controls verified for {policy['repository']}.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
