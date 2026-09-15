#!/usr/bin/env python3
"""Reproduce terminal-withdrawal controls and mutants without changing source files.

Usage: python3 reproduce.py SOURCE_TREE NEW_WORK_DIRECTORY EVIDENCE_DIRECTORY
The source tree must match inputs.json. Historical Git objects are not needed:
original.go.txt supplies the original implementation. Go uses its normal caches.
"""

import datetime
import difflib
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys

SOURCE = "x/billing/keeper/msg_server.go"
TEST = "x/billing/keeper/terminal_withdrawal_test.go"
FILTER = "^TestMsgWithdrawImportedClosedLease"


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def main():
    if len(sys.argv) != 4:
        raise SystemExit(__doc__)
    repository, work, evidence = (Path(arg).resolve() for arg in sys.argv[1:])
    driver = Path(__file__).resolve()
    inputs_path = driver.parent / "inputs.json"
    inputs = json.loads(inputs_path.read_text())
    for relative, expected in inputs["required_source_sha256"].items():
        if sha256((repository / relative).read_bytes()) != expected:
            raise SystemExit(f"Source does not match recorded input: {relative}")
    original_input = driver.parent / inputs["original"]["file"]
    original = original_input.read_bytes()
    if sha256(original) != inputs["original"]["sha256"]:
        raise SystemExit("Archived original implementation hash mismatch")
    fixed = (repository / SOURCE).read_bytes()
    fixed_text = fixed.decode()
    cursor = "\t\t} else {\n\t\t\tlease.LastSettledAt = settleTime\n\t\t}"
    if fixed_text.count(cursor) != 1:
        raise SystemExit("Expected exactly one terminal cursor assignment")
    retained = fixed_text.replace(cursor, "\t\t} else {\n\t\t\tif result.TransferAmounts.Equal(result.AccruedAmounts) {\n\t\t\t\tlease.LastSettledAt = settleTime\n\t\t\t}\n\t\t}", 1).encode()
    zero_guard = "if result.TransferAmounts.IsZero() && !finalizeClosedInterval {"
    if fixed_text.count(zero_guard) != 1:
        raise SystemExit("Expected exactly one terminal zero-payment guard")
    skipped = fixed_text.replace(zero_guard, "if result.TransferAmounts.IsZero() {", 1).encode()
    variants = [
        ("fixed", fixed, 0, "unmodified fixed implementation"),
        ("original", original, 1, "archived implementation before the F1 fix"),
        ("retained-cursor", retained, 1, "retain the CLOSED cursor unless all accrued amounts were paid"),
        ("skipped-zero", skipped, 1, "skip zero transfers before finalizing CLOSED intervals"),
    ]
    work.mkdir(parents=True, exist_ok=False)
    evidence.mkdir(parents=True, exist_ok=True)
    temporary = work / "build"
    temporary.mkdir()
    raw_directory = work / "raw-output"
    raw_directory.mkdir()
    source_hashes = {}
    for directory, names, files in os.walk(repository):
        names[:] = [name for name in names if not name.startswith(".") and name not in ("docs", "interchaintest", "node_modules")]
        for name in files:
            if name.endswith(".go"):
                path = Path(directory) / name
                source_hashes[str(path.relative_to(repository))] = sha256(path.read_bytes())
    overrides = {
        "GOTOOLCHAIN": "go1.26.8", "GOMAXPROCS": "4", "TMPDIR": str(temporary),
        "GOWORK": "off", "GOFLAGS": "-mod=readonly",
    }
    environment = os.environ.copy()
    environment.update(overrides)
    go_version_argv = ["go", "version"]
    go_env_argv = ["go", "env", "-json", "GOCACHE", "GOMODCACHE", "GOROOT"]
    dependency_argv = ["go", "list", "-m", "-json", "all"]
    go_version = subprocess.check_output(go_version_argv, cwd=repository, env=environment, text=True).strip()
    go_paths = json.loads(subprocess.check_output(go_env_argv, cwd=repository, env=environment, text=True))
    dependency_output = subprocess.check_output(dependency_argv, cwd=repository, env=environment, text=True)
    modules = []
    decoder = json.JSONDecoder()
    remaining = dependency_output
    while remaining.strip():
        remaining = remaining.lstrip()
        module, end = decoder.raw_decode(remaining)
        modules.append(module)
        remaining = remaining[end:]
    dependency_file = evidence / "dependency-modules.json"
    dependency_file.write_text(json.dumps({"argv": dependency_argv, "cwd": str(repository), "modules": modules}, indent=2) + "\n")
    manifest = {
        "started_at_utc": now(), "driver_sha256": sha256(driver.read_bytes()),
        "driver_sys_argv": list(sys.argv), "python_executable": sys.executable,
        "driver_cwd": str(Path.cwd()),
        "source_tree": str(repository), "work_directory": str(work),
        "inputs_manifest_sha256": sha256(inputs_path.read_bytes()),
        "original_input_sha256": sha256(original),
        "original_source_provenance": inputs["original"]["provenance"],
        "environment_overrides": overrides, "python_version": sys.version,
        "go_version": go_version, "go_version_argv": go_version_argv,
        "go_env_argv": go_env_argv, "go_paths": go_paths,
        "source_sha256": {SOURCE: sha256(fixed)},
        "test_source_sha256": {TEST: sha256((repository / TEST).read_bytes())},
        "dependency_source_sha256": {name: sha256((repository / name).read_bytes()) for name in ("go.mod", "go.sum")},
        "dependency_modules_file": dependency_file.name,
        "dependency_modules_sha256": sha256(dependency_file.read_bytes()),
        "go_source_sha256": dict(sorted(source_hashes.items())), "runs": [],
    }

    def persist():
        (evidence / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")

    for name, content, expected, description in variants:
        target = work / name / SOURCE
        target.parent.mkdir(parents=True)
        target.write_bytes(content)
        archived = evidence / (name + ".go.txt")
        archived.write_bytes(content)
        overlay = evidence / (name + "-overlay.json")
        overlay.write_text(json.dumps({"Replace": {str(repository / SOURCE): str(target)}}, indent=2) + "\n")
        mutation = {
            "description": description, "source_path": SOURCE,
            "baseline_sha256": sha256(fixed), "mutant_sha256": sha256(content),
            "archived_source": archived.name, "archived_source_sha256": sha256(archived.read_bytes()),
            "overlay": overlay.name, "overlay_sha256": sha256(overlay.read_bytes()),
        }
        if content != fixed:
            patch = evidence / (name + ".patch")
            patch.write_text("".join(difflib.unified_diff(
                fixed.decode().splitlines(keepends=True), content.decode().splitlines(keepends=True),
                fromfile="a/" + SOURCE, tofile="b/" + SOURCE, n=0,
            )))
            mutation.update(patch=patch.name, patch_sha256=sha256(patch.read_bytes()))
        argv = ["go", "test", "-p", "2", "-overlay", str(overlay), "./x/billing/keeper", "-run", FILTER, "-count=1", "-timeout=180s", "-v"]
        raw_log = raw_directory / (name + ".log")
        raw_archive = evidence / (name + "-raw.json")
        display = evidence / (name + ".log")
        shell_command = "cd " + shlex.quote(str(repository)) + " && env "
        shell_command += " ".join(shlex.quote(key + "=" + value) for key, value in overrides.items())
        shell_command += " " + shlex.join(argv) + " > " + shlex.quote(str(raw_log)) + " 2>&1"
        run = {
            "name": name, "description": description, "mutation": mutation,
            "argv": argv, "cwd": str(repository), "exact_shell_command": shell_command,
            "environment_overrides": overrides, "expected_exit_status": expected,
            "started_at_utc": now(), "raw_stdout_stderr_path": str(raw_log),
            "raw_archive": raw_archive.name, "log": display.name,
            "display_log_normalization": "UTF-8 decode; splitlines(); expandtabs(8) then rstrip() per line; join with LF and append final LF",
        }
        manifest["runs"].append(run)
        persist()
        print(shell_command, flush=True)
        with raw_log.open("wb") as output:
            result = subprocess.run(argv, cwd=repository, env=environment, stdout=output, stderr=subprocess.STDOUT, check=False)
        raw = raw_log.read_bytes()
        raw_text = raw.decode()
        raw_archive.write_text(json.dumps({"encoding": "utf-8", "stdout_stderr": raw_text}, indent=2) + "\n")
        display.write_text("\n".join(line.expandtabs(8).rstrip() for line in raw_text.splitlines()) + "\n")
        display_text = display.read_text()
        run.update({
            "exit_status": result.returncode, "finished_at_utc": now(),
            "log_sha256": sha256(display.read_bytes()),
            "raw_stdout_stderr_sha256": sha256(raw), "raw_archive_sha256": sha256(raw_archive.read_bytes()),
            "tests_run": re.findall(r"^=== RUN\s+(\S+)", display_text, re.MULTILINE),
            "tests_failed": re.findall(r"^\s*--- FAIL: (\S+)", display_text, re.MULTILINE),
        })
        persist()
        if result.returncode != expected or not run["tests_run"] or (expected and not run["tests_failed"]):
            raise SystemExit(f"Unexpected mutation result; inspect {display}")
        print(f"{name}: exit {result.returncode} (expected {expected})", flush=True)
    changed = [name for name, expected in source_hashes.items() if sha256((repository / name).read_bytes()) != expected]
    for relative, expected in inputs["required_source_sha256"].items():
        if sha256((repository / relative).read_bytes()) != expected:
            changed.append(relative)
    manifest.update(source_files_checked_unchanged=len(source_hashes), source_files_changed=sorted(set(changed)), finished_at_utc=now())
    persist()
    if changed:
        raise SystemExit("Source files changed during reproduction: " + ", ".join(changed))
    if sha256(driver.read_bytes()) != manifest["driver_sha256"] or sha256(inputs_path.read_bytes()) != manifest["inputs_manifest_sha256"]:
        raise SystemExit("Driver or input manifest changed during reproduction")
    print(f"All {len(source_hashes)} Go source files and required inputs remained unchanged.", flush=True)


if __name__ == "__main__":
    main()
