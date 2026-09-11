#!/usr/bin/env python3
"""Reproduce the three PR179 mutation checks without editing a checkout.

Usage: python3 reproduce.py (--bundled | REPOSITORY) NEW_WORK_DIRECTORY EVIDENCE_DIRECTORY
The work directory must not exist. The evidence directory receives logs,
patches, overlay maps, hashes, exact commands, and exit statuses.
"""

import datetime
import difflib
import hashlib
import gzip
import io
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys
import tarfile


BASELINE = "b4d8403977b8f18eeaad699239e9661efa2a4a8a"
PREVIOUS = "4b89fdacb8d840b074ffda3e64efb34f5a4b71b3"
WITHDRAWAL_FILE = "x/billing/keeper/provider_withdrawal.go"
EXPORT_FILE = "app/export.go"
WITHDRAWAL_FILTER = (
    "^TestProviderLeaseWithdrawal"
    "(LateFailurePreservesCaller|SuccessPreservesLifecycleAndCacheOwnership)$"
)
EXPORT_FILTER = "^TestZeroHeightExportRejectsStaleCheckTxTimeAfterRollback$"


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def utc_now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def require(condition, message):
    # Integrity and outcome checks must also run under python -O.
    if not condition:
        raise SystemExit(message)


def main():
    if len(sys.argv) != 4:
        raise SystemExit(__doc__)
    bundled = sys.argv[1] == "--bundled"
    repository = None if bundled else Path(sys.argv[1]).resolve()
    work, evidence = (Path(value).resolve() for value in sys.argv[2:])
    work.mkdir(parents=True, exist_ok=False)
    evidence.mkdir(parents=True, exist_ok=True)
    provenance = evidence / "mutation-provenance"
    provenance.mkdir(exist_ok=True)

    def git(*arguments):
        return subprocess.check_output(["git", "-C", str(repository), *arguments])

    if bundled:
        inputs = Path(__file__).resolve().parent / "inputs"
        input_manifest = json.loads((inputs / "manifest.json").read_text())
        require(input_manifest["baseline_commit"] == BASELINE,
                "Bundled baseline commit does not match the pinned commit")
        require(input_manifest["previous_commit"] == PREVIOUS,
                "Bundled previous commit does not match the pinned commit")
        archive_info = input_manifest["baseline_archive"]
        compressed = (inputs / archive_info["file"]).read_bytes()
        require(sha256(compressed) == archive_info["sha256"],
                "Bundled compressed archive hash mismatch")
        archive = gzip.decompress(compressed)
        require(sha256(archive) == archive_info["uncompressed_sha256"],
                "Bundled uncompressed archive hash mismatch")

        def previous_file(relative):
            entry = input_manifest["previous_files"][relative]
            data = (inputs / entry["file"]).read_bytes()
            require(sha256(data) == entry["sha256"],
                    f"Bundled historical source hash mismatch: {relative}")
            return data
    else:
        for commit in (BASELINE, PREVIOUS):
            require(git("rev-parse", commit).decode().strip() == commit,
                    f"Git revision does not match the pinned commit: {commit}")
        archive = git("archive", "--format=tar", BASELINE)

        def previous_file(relative):
            return git("show", f"{PREVIOUS}:{relative}")
    snapshot = work / "baseline"
    snapshot.mkdir()
    # Only the pinned Git archive or hash-checked bundled snapshot is extracted.
    with tarfile.open(fileobj=io.BytesIO(archive)) as source:
        source.extractall(snapshot, filter="data")
    original_hashes = {
        str(path.relative_to(snapshot)): sha256(path.read_bytes())
        for path in snapshot.rglob("*")
        if path.is_file()
    }
    # Validate every mutation input before invoking Go, including go version.
    baseline_withdrawal = (snapshot / WITHDRAWAL_FILE).read_bytes()
    clone_line = b"\tsettledLease.Reservation = cloneLeaseReservation(lease.Reservation)\n"
    require(baseline_withdrawal.count(clone_line) == 1,
            "Expected exactly one reservation clone assignment in the baseline")
    variants = [
        ("withdrawal-original", WITHDRAWAL_FILE,
         previous_file(WITHDRAWAL_FILE),
         f"entire source file from {PREVIOUS}",
         "./x/billing/keeper", WITHDRAWAL_FILTER),
        ("withdrawal-shallow", WITHDRAWAL_FILE,
         baseline_withdrawal.replace(clone_line, b""),
         "delete only the reservation clone assignment from the baseline",
         "./x/billing/keeper", WITHDRAWAL_FILTER),
        ("export-stale-header", EXPORT_FILE,
         previous_file(EXPORT_FILE),
         f"entire source file from {PREVIOUS}",
         "./app", EXPORT_FILTER),
    ]
    temporary = snapshot / ".review-tmp" / "build"
    temporary.mkdir(parents=True)
    raw_output = work / "raw-output"
    raw_output.mkdir()
    overrides = {
        "GOTOOLCHAIN": "go1.26.8",
        "GOMAXPROCS": "4",
        "TMPDIR": str(temporary),
        "GOWORK": "off",
        "GOFLAGS": "-mod=readonly",
    }
    environment = os.environ.copy()
    environment.update(overrides)
    toolchain = subprocess.check_output(
        ["go", "version"], cwd=snapshot, env=environment, text=True
    ).strip()
    go_paths = json.loads(subprocess.check_output(
        ["go", "env", "-json", "GOCACHE", "GOMODCACHE", "GOROOT"],
        cwd=snapshot, env=environment, text=True,
    ))

    manifest = {
        "started_at_utc": utc_now(),
        "baseline_commit": BASELINE,
        "previous_commit": PREVIOUS,
        "baseline_git_archive_sha256": sha256(archive),
        "snapshot_directory": str(snapshot),
        "source_repository": str(repository) if repository is not None else None,
        "source_mode": "bundled" if bundled else "git",
        "environment_overrides": overrides,
        "go_version": toolchain,
        "go_paths": go_paths,
        "driver_sha256": sha256(Path(__file__).read_bytes()),
        "driver_sys_argv": list(sys.argv),
        "driver_cwd": str(Path.cwd()),
        "python_executable": sys.executable,
        "python_version": sys.version,
        "python_optimize": sys.flags.optimize,
        "test_source_sha256": {
            name: original_hashes[name] for name in (
                "go.mod", "go.sum", "x/billing/keeper/export_test.go",
                "x/billing/keeper/provider_withdrawal_atomicity_test.go",
                "app/export_billing_test.go",
            )
        },
        "runs": [],
    }

    def persist():
        (provenance / "runs.json").write_text(json.dumps(manifest, indent=2) + "\n")

    def run(name, package, test_filter, overlay=None, mutation=None):
        command = ["go", "test", "-p", "2"]
        if overlay is not None:
            command += ["-overlay", str(overlay)]
        command += [package, "-run", test_filter, "-count=1", "-timeout=180s", "-v"]
        log = evidence / f"{name}.log"
        raw_log = raw_output / log.name
        raw_archive = provenance / f"{name}-raw.json"
        shell_command = "cd " + shlex.quote(str(snapshot)) + " && "
        shell_command += "env " + " ".join(
            shlex.quote(key + "=" + value) for key, value in overrides.items()
        )
        shell_command += " " + shlex.join(command)
        shell_command += " > " + shlex.quote(str(raw_log)) + " 2>&1"
        record = {
            "name": name, "cwd": str(snapshot), "argv": command,
            "exact_shell_command": shell_command,
            "log": log.name, "started_at_utc": utc_now(),
            "raw_stdout_stderr_path": str(raw_log),
            "raw_archive": str(raw_archive.relative_to(evidence)),
            "display_log_normalization": (
                "UTF-8 decode; splitlines(); expandtabs(8) then rstrip() per line; "
                "join with LF and append final LF"
            ),
            "expected_exit_status": 1 if overlay else 0,
        }
        if mutation is not None:
            record["mutation"] = mutation
        manifest["runs"].append(record)
        persist()
        print(shell_command, flush=True)
        with raw_log.open("wb") as output:
            completed = subprocess.run(
                command, cwd=snapshot, env=environment,
                stdout=output, stderr=subprocess.STDOUT, check=False,
            )
        raw_bytes = raw_log.read_bytes()
        raw_text = raw_bytes.decode()
        raw_archive.write_text(json.dumps({
            "encoding": "utf-8", "stdout_stderr": raw_text,
        }, indent=2) + "\n")
        log.write_text("\n".join(
            line.expandtabs(8).rstrip() for line in raw_text.splitlines()
        ) + "\n")
        record.update({
            "exit_status": completed.returncode,
            "finished_at_utc": utc_now(),
            "log_sha256": sha256(log.read_bytes()),
            "raw_stdout_stderr_sha256": sha256(raw_bytes),
            "raw_archive_sha256": sha256(raw_archive.read_bytes()),
        })
        log_text = log.read_text()
        record["tests_run"] = re.findall(r"^=== RUN\s+(\S+)", log_text, re.MULTILINE)
        record["tests_failed"] = re.findall(r"^\s*--- FAIL: (\S+)", log_text, re.MULTILINE)
        persist()
        print(f"{name}: exit {completed.returncode}", flush=True)
        if completed.returncode != record["expected_exit_status"]:
            raise SystemExit(f"Unexpected result; inspect {log}")
        if not record["tests_run"] or (overlay and not record["tests_failed"]):
            raise SystemExit(f"No expected test execution/failure evidence; inspect {log}")

    run("withdrawal-baseline-rerun", "./x/billing/keeper", WITHDRAWAL_FILTER)
    run("export-baseline-rerun", "./app", EXPORT_FILTER)
    for name, relative, mutated, description, package, test_filter in variants:
        baseline_bytes = (snapshot / relative).read_bytes()
        target = work / name / relative
        target.parent.mkdir(parents=True)
        target.write_bytes(mutated)
        patch = provenance / f"{name}.patch"
        patch.write_text("".join(difflib.unified_diff(
            baseline_bytes.decode().splitlines(keepends=True),
            mutated.decode().splitlines(keepends=True),
            fromfile="a/" + relative, tofile="b/" + relative,
            n=0,
        )))
        overlay = provenance / f"{name}-overlay.json"
        overlay.write_text(json.dumps({
            "Replace": {str(snapshot / relative): str(target)}
        }, indent=2) + "\n")
        mutation = {
            "description": description, "source_path": relative,
            "baseline_sha256": sha256(baseline_bytes),
            "mutant_sha256": sha256(mutated),
            "patch": patch.name, "patch_sha256": sha256(patch.read_bytes()),
            "overlay": overlay.name, "overlay_sha256": sha256(overlay.read_bytes()),
        }
        run(name + "-mutant", package, test_filter, overlay, mutation)

    changed = [
        name for name, digest in original_hashes.items()
        if sha256((snapshot / name).read_bytes()) != digest
    ]
    manifest["source_files_checked_unchanged"] = len(original_hashes)
    manifest["source_files_changed"] = changed
    manifest["finished_at_utc"] = utc_now()
    persist()
    if changed:
        raise SystemExit(f"Snapshot files unexpectedly changed: {changed}")
    require(sha256(Path(__file__).read_bytes()) == manifest["driver_sha256"],
            "Reproduction driver changed during execution")
    print(f"All {len(original_hashes)} archived source files remained unchanged.", flush=True)


if __name__ == "__main__":
    main()
