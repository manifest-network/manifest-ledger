#!/usr/bin/env python3
"""Verify bundled mutation evidence without Go, Git, or historical objects.

Usage: python3 verify_artifacts.py EVIDENCE_DIRECTORY REPORT_JSON [--execution-files]
The optional flag also checks the recorded run's temporary source/overlay files;
it requires those absolute paths to remain available. Patch reconstruction is
recorded separately by verify_patch_reconstruction.py in the hygiene evidence.
"""

import datetime
import gzip
import hashlib
import io
import json
from pathlib import Path
import re
import shlex
import sys
import tarfile


def require(condition, message):
    if not condition:
        raise SystemExit(message)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def main():
    require(len(sys.argv) in (3, 4), __doc__)
    check_execution_files = len(sys.argv) == 4
    require(not check_execution_files or sys.argv[3] == "--execution-files", __doc__)
    evidence, report_path = (Path(value).resolve() for value in sys.argv[1:3])
    provenance = evidence / "mutation-provenance"
    runs_path = provenance / "runs.json"
    runs = json.loads(runs_path.read_text())
    inputs = provenance / "inputs"
    inputs_path = inputs / "manifest.json"
    input_manifest = json.loads(inputs_path.read_text())
    verified = {}

    def label(path):
        return str(path.relative_to(evidence)) if path.is_relative_to(evidence) else str(path)

    def record(path):
        value = digest(path.read_bytes())
        verified[label(path)] = value
        return value

    def verify(path, expected):
        require(record(path) == expected, f"Hash mismatch: {path}")

    record(runs_path)
    record(inputs_path)
    verify(provenance / "reproduce.py", runs["driver_sha256"])
    require(runs["source_mode"] == "bundled", "Expected bundled source mode")
    require(runs["source_repository"] is None, "Bundled run unexpectedly used a repository")
    require(runs["python_optimize"] == 1, "Expected the optimized-Python control run")
    require(runs["source_files_changed"] == [], "Snapshot changes were recorded")
    require(input_manifest["baseline_commit"] == runs["baseline_commit"], "Baseline commit mismatch")
    require(input_manifest["previous_commit"] == runs["previous_commit"], "Previous commit mismatch")
    archive_info = input_manifest["baseline_archive"]
    archive_path = inputs / archive_info["file"]
    verify(archive_path, archive_info["sha256"])
    archive_bytes = gzip.decompress(archive_path.read_bytes())
    require(digest(archive_bytes) == archive_info["uncompressed_sha256"]
            == runs["baseline_git_archive_sha256"], "Uncompressed archive hash mismatch")
    for entry in input_manifest["previous_files"].values():
        verify(inputs / entry["file"], entry["sha256"])
    with tarfile.open(fileobj=io.BytesIO(archive_bytes)) as archive:
        baseline = {
            member.name: archive.extractfile(member).read()
            for member in archive.getmembers() if member.isfile()
        }
    require(len(baseline) == runs["source_files_checked_unchanged"] == 483,
            "Baseline file count mismatch")
    snapshot = Path(runs["snapshot_directory"])
    if check_execution_files:
        for name, data in baseline.items():
            require((snapshot / name).read_bytes() == data, f"Snapshot source changed: {name}")
    for name, expected in runs["test_source_sha256"].items():
        require(digest(baseline[name]) == expected, f"Test/dependency hash mismatch: {name}")

    expected_runs = [
        ("withdrawal-baseline-rerun", 0), ("export-baseline-rerun", 0),
        ("withdrawal-original-mutant", 1), ("withdrawal-shallow-mutant", 1),
        ("export-stale-header-mutant", 1),
    ]
    require([(run["name"], run["exit_status"]) for run in runs["runs"]] == expected_runs,
            "Run sequence or outcome mismatch")
    for run in runs["runs"]:
        verify(evidence / run["log"], run["log_sha256"])
        raw_archive = evidence / run["raw_archive"]
        verify(raw_archive, run["raw_archive_sha256"])
        raw_record = json.loads(raw_archive.read_text())
        require(raw_record["encoding"] == "utf-8", "Unsupported raw output encoding")
        raw = raw_record["stdout_stderr"].encode()
        require(digest(raw) == run["raw_stdout_stderr_sha256"], "Raw output hash mismatch")
        if check_execution_files:
            require(Path(run["raw_stdout_stderr_path"]).read_bytes() == raw,
                    "Original raw output differs from its archive")
        display = "\n".join(line.expandtabs(8).rstrip() for line in raw.decode().splitlines()) + "\n"
        require((evidence / run["log"]).read_bytes() == display.encode(),
                "Display normalization mismatch")
        require(re.findall(r"^=== RUN\s+(\S+)", display, re.MULTILINE) == run["tests_run"],
                "Executed test names mismatch")
        require(re.findall(r"^\s*--- FAIL: (\S+)", display, re.MULTILINE) == run["tests_failed"],
                "Failed test names mismatch")
        require(run["exit_status"] == run["expected_exit_status"], "Unexpected run exit")
        require(run["tests_run"], "No tests executed")
        command = "cd " + shlex.quote(run["cwd"]) + " && env "
        command += " ".join(shlex.quote(key + "=" + value)
                            for key, value in runs["environment_overrides"].items())
        command += " " + shlex.join(run["argv"])
        command += " > " + shlex.quote(run["raw_stdout_stderr_path"]) + " 2>&1"
        require(command == run["exact_shell_command"], "Recorded shell command mismatch")
        if "mutation" not in run:
            require(run["exit_status"] == 0 and not run["tests_failed"], "Control failed")
            continue
        mutation = run["mutation"]
        require(run["exit_status"] == 1 and run["tests_failed"], "Missing mutant failure")
        source = mutation["source_path"]
        require(digest(baseline[source]) == mutation["baseline_sha256"], "Mutation baseline mismatch")
        verify(provenance / mutation["patch"], mutation["patch_sha256"])
        overlay = provenance / mutation["overlay"]
        verify(overlay, mutation["overlay_sha256"])
        replacements = json.loads(overlay.read_text())["Replace"]
        require(len(replacements) == 1, "Expected exactly one overlay replacement")
        source_path, mutant_path = next(iter(replacements.items()))
        require(source_path == str(snapshot / source), "Overlay source path mismatch")
        if check_execution_files:
            require(digest(Path(mutant_path).read_bytes()) == mutation["mutant_sha256"],
                    "Executed mutant source hash mismatch")

    report = {
        "verified_at_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "verifier_sha256": digest(Path(__file__).read_bytes()),
        "verifier_sys_argv": list(sys.argv), "verifier_cwd": str(Path.cwd()),
        "python_executable": sys.executable, "python_version": sys.version,
        "python_optimize": sys.flags.optimize,
        "runs": len(runs["runs"]), "baseline_archive_files": len(baseline),
        "checked_execution_files": check_execution_files,
        "compressed_archive_bytes": archive_path.stat().st_size,
        "uncompressed_archive_sha256": digest(archive_bytes), "hashes": verified,
        "checks": ["driver", "bundled inputs", "test/dependency sources", "display logs",
                   "raw JSON and encoded output", "recorded commands", "test names and outcomes",
                   "overlays and patches"],
        "patch_reconstruction": "verified separately by ../../2026-09-11-claude-hygiene-validation/verify_patch_reconstruction.py",
    }
    report_path.parent.mkdir(parents=True, exist_ok=True)
    report_path.write_text(json.dumps(report, indent=2) + "\n")
    print(f"Verified {len(runs['runs'])} runs and {len(verified)} artifact hashes.")
    print(f"Execution files checked: {check_execution_files}; baseline files: {len(baseline)}.")


if __name__ == "__main__":
    main()
