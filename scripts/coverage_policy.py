"""Shared source exclusions for coverage filtering and the full-diff gate."""

from fnmatch import fnmatchcase
from pathlib import Path, PurePosixPath
import sys


def patterns(text):
    return [line.strip() for line in text.splitlines()
            if line.strip() and not line.lstrip().startswith("#")]


def excluded(path, ignores):
    # Go's ./... package expansion excludes testdata fixtures. These are not
    # shipping packages, even when a fixture happens to contain valid Go code.
    return "testdata" in PurePosixPath(path).parts or any(
        fnmatchcase(path, pattern) for pattern in ignores
    )


def eligible(path, module, ignores):
    return (path.endswith(".go") and not path.endswith("_test.go")
            and not excluded(module + "/" + path, ignores))


def filter_profile(profile, ignores):
    return "".join(line for line in profile.splitlines(keepends=True)
                   if line.startswith("mode:") or not excluded(line.rsplit(":", 1)[0], ignores))


if __name__ == "__main__":
    if len(sys.argv) != 3:
        sys.exit("usage: coverage_policy.py <profile> <filtered-profile>")
    policy = patterns(Path(".coverageignore").read_text())
    Path(sys.argv[2]).write_text(filter_profile(Path(sys.argv[1]).read_text(), policy))
