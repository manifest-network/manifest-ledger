#!/usr/bin/env python3
"""Capture the SKU module gate control, coverage and isolated validation mutant.

Usage: python3 reproduce.py SOURCE_TREE NEW_WORK_DIRECTORY EVIDENCE_DIRECTORY
The archived inputs are immutable; each output directory must be new.
"""

import base64
import datetime
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys


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
    for name, expected in inputs["required_source_sha256"].items():
        if sha256((repository / name).read_bytes()) != expected:
            raise SystemExit(f"Source input mismatch: {name}")
    for name, expected in inputs["archived_sha256"].items():
        if sha256((driver.parent / name).read_bytes()) != expected:
            raise SystemExit(f"Archived input mismatch: {name}")
    work.mkdir(parents=True, exist_ok=False)
    evidence.mkdir(parents=True, exist_ok=False)
    temporary = work / "build"
    temporary.mkdir()
    overrides = {
        "GOTOOLCHAIN": "go1.26.8",
        "GOMAXPROCS": "4",
        "TMPDIR": str(temporary),
        "GOWORK": "off",
        "GOFLAGS": "-mod=readonly",
    }
    environment = os.environ.copy()
    environment.update(overrides)
    manifest = {
        "driver_argv": [sys.executable, str(driver), *map(str, (repository, work, evidence))],
        "driver_sha256": sha256(driver.read_bytes()),
        "inputs_manifest_sha256": sha256(inputs_path.read_bytes()),
        "cwd": str(repository),
        "environment_overrides": overrides,
        "environment_note": "Other normal host environment entries are inherited; credentials are not archived. Go workspace and module-write behavior are explicitly controlled.",
        "source_sha256": inputs["required_source_sha256"],
        "archived_sha256": inputs["archived_sha256"],
        "runs": [],
    }

    def persist():
        (evidence / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")

    def run(name, argv, expected):
        start = now()
        result = subprocess.run(argv, cwd=repository, env=environment, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=False)
        raw = evidence / (name + "-raw.json")
        raw.write_text(json.dumps({
            "encoding": "base64",
            "combined_stdout_stderr": base64.b64encode(result.stdout).decode(),
            "sha256": sha256(result.stdout),
            "bytes": len(result.stdout),
        }, indent=2) + "\n")
        output = evidence / (name + ".log")
        output.write_text("\n".join(line.expandtabs(8).rstrip() for line in result.stdout.decode().splitlines()).rstrip() + "\n")
        manifest["runs"].append({
            "name": name, "argv": argv, "cwd": str(repository),
            "environment_overrides": overrides, "started_at_utc": start,
            "completed_at_utc": now(), "exit_status": result.returncode,
            "expected_exit_status": expected, "raw_output": raw.name,
            "raw_output_file_sha256": sha256(raw.read_bytes()),
            "display_output": output.name,
            "display_output_sha256": sha256(output.read_bytes()),
            "display_normalization": "Tabs expanded at 8-column stops, line endings normalized, trailing whitespace and EOF blank lines removed, one final LF; exact captured bytes are retained in raw_output.",
            "combined_output_sha256": sha256(result.stdout),
            "combined_output_bytes": len(result.stdout),
        })
        persist()
        if result.returncode != expected:
            raise SystemExit(f"Unexpected status for {name}: {result.returncode}")
        return result.stdout

    run("go-version", ["go", "version"], 0)
    run("go-paths", ["go", "env", "-json", "GOCACHE", "GOMODCACHE", "GOROOT"], 0)
    for name, source, status in [
        ("fixed", "module-fixed.go.txt", 0),
        ("skip-validation", "module-skip-validation.go.txt", 1),
    ]:
        replacement = work / (name + ".go")
        replacement.write_bytes((driver.parent / source).read_bytes())
        overlay = evidence / (name + "-overlay.json")
        overlay.write_text(json.dumps({"Replace": {str(repository / "x/sku/module.go"): str(replacement)}}, indent=2) + "\n")
        run(name, [
            "go", "test", "-p", "2", "-overlay", str(overlay), "./x/sku",
            "-run", "^TestAppModuleBasicValidateGenesis", "-count=1", "-v",
            "-timeout=120s",
        ], status)
        manifest["runs"][-1].update({
            "overlay": overlay.name, "overlay_sha256": sha256(overlay.read_bytes()),
            "replacement_source": source, "replacement_sha256": sha256(replacement.read_bytes()),
        })
        persist()
    # Measure coverage in an independent, unmodified-source run. Keep the
    # mutation comparison free of coverage instrumentation.
    profile = evidence / "fixed-coverage.out"
    run("fixed-coverage", [
        "go", "test", "-p", "2", "./x/sku", "-run",
        "^TestAppModuleBasicValidateGenesis", "-count=1", "-v",
        "-coverprofile=" + str(profile), "-timeout=120s",
    ], 0)
    manifest["runs"][-1].update({
        "coverage_profile": profile.name,
        "coverage_profile_sha256": sha256(profile.read_bytes()),
    })
    persist()
    run("fixed-coverage-functions", ["go", "tool", "cover", "-func=" + str(evidence / "fixed-coverage.out")], 0)
    manifest["all_expected_statuses_observed"] = True
    persist()


if __name__ == "__main__":
    main()
