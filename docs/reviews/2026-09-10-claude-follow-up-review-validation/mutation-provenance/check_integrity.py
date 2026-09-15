#!/usr/bin/env python3
"""Check reproduction-driver validation with normal and optimized Python.

Usage: python3 check_integrity.py NEW_WORK_DIRECTORY EVIDENCE_DIRECTORY
Go is replaced with a local marker/stub. This checks driver control flow; the
separate bundled reproduction runs the actual Go controls and mutants.
"""

import datetime
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile


def require(condition, message):
    if not condition:
        raise SystemExit(message)


def sha256(data):
    return hashlib.sha256(data).hexdigest()


GO_STUB = '''#!{python}
import json, os, sys
from pathlib import Path
args=sys.argv[1:]
with Path(os.environ["GO_CALL_LOG"]).open("a") as log:
    log.write(json.dumps(args)+"\\n")
if args == ["version"]:
    print("go version DRIVER-TEST-STUB")
    raise SystemExit(0)
if args and args[0] == "env":
    print(json.dumps({{"GOCACHE":"stub","GOMODCACHE":"stub","GOROOT":"stub"}}))
    raise SystemExit(0)
mode=os.environ["STUB_MODE"]
mutant="-overlay" in args
if mode == "bad_exit":
    print("=== RUN TestDriverStub")
    raise SystemExit(22)
if mode == "no_tests":
    print("stub output without test execution")
    raise SystemExit(0)
if mode == "source_changed":
    source=Path.cwd()/"README.md"
    source.write_bytes(source.read_bytes()+b"\\ncontrolled driver-test mutation\\n")
if mode == "driver_changed":
    driver=Path(os.environ["TARGET_DRIVER"])
    driver.write_bytes(driver.read_bytes()+b"\\n# controlled driver-test mutation\\n")
print("=== RUN TestDriverStub")
if not (mode == "no_mutant_failure" and mutant):
    print("--- FAIL: TestDriverStub (0.00s)" if mutant else "--- PASS: TestDriverStub (0.00s)")
raise SystemExit(1 if mutant else 0)
'''


def main():
    if len(sys.argv) != 3:
        raise SystemExit(__doc__)
    work, evidence = (Path(arg).resolve() for arg in sys.argv[1:])
    work.mkdir(parents=True, exist_ok=False)
    evidence.mkdir(parents=True, exist_ok=True)
    origin = Path(__file__).resolve().parent
    driver_source = (origin / "reproduce.py").read_bytes()
    cases = [
        ("baseline_commit", "Bundled baseline commit", False),
        ("previous_commit", "Bundled previous commit", False),
        ("compressed_archive", "Bundled compressed archive hash mismatch", False),
        ("uncompressed_archive", "Bundled uncompressed archive hash mismatch", False),
        ("previous_withdrawal", "Bundled historical source hash mismatch: x/billing/keeper/provider_withdrawal.go", False),
        ("previous_export", "Bundled historical source hash mismatch: app/export.go", False),
        ("clone_assignment", "Expected exactly one reservation clone assignment", False),
        ("bad_exit", "Unexpected result", True),
        ("no_tests", "No expected test execution/failure evidence", True),
        ("no_mutant_failure", "No expected test execution/failure evidence", True),
        ("source_changed", "Snapshot files unexpectedly changed", True),
        ("driver_changed", "Reproduction driver changed during execution", True),
    ]
    report = {
        "started_at_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "driver_sha256": sha256(driver_source),
        "check_driver_sha256": sha256(Path(__file__).read_bytes()),
        "check_driver_sys_argv": list(sys.argv), "cwd": str(Path.cwd()),
        "python_executable": sys.executable, "python_version": sys.version,
        "go_is_stubbed": True, "runs": [],
    }
    for optimized in (False, True):
        for name, diagnostic, runtime in cases:
            with tempfile.TemporaryDirectory(prefix=name+"-", dir=work) as temporary:
                fixture = Path(temporary)
                driver = fixture / "reproduce.py"
                driver.write_bytes(driver_source)
                shutil.copytree(origin / "inputs", fixture / "inputs")
                manifest_path = fixture / "inputs/manifest.json"
                manifest = json.loads(manifest_path.read_text())
                archive_path = fixture / "inputs" / manifest["baseline_archive"]["file"]
                if name in ("baseline_commit", "previous_commit"):
                    manifest[name] = "0" * 40
                elif name == "compressed_archive":
                    archive_path.write_bytes(archive_path.read_bytes() + b"tampered")
                elif name == "uncompressed_archive":
                    manifest["baseline_archive"]["uncompressed_sha256"] = "0" * 64
                elif name in ("previous_withdrawal", "previous_export"):
                    relative = "app/export.go" if name == "previous_export" else "x/billing/keeper/provider_withdrawal.go"
                    path = fixture / "inputs" / manifest["previous_files"][relative]["file"]
                    path.write_bytes(path.read_bytes() + b"\n// controlled integrity-test mutation\n")
                elif name == "clone_assignment":
                    original_archive = gzip.decompress(archive_path.read_bytes())
                    target = io.BytesIO()
                    with tarfile.open(fileobj=io.BytesIO(original_archive)) as original, tarfile.open(fileobj=target, mode="w") as changed:
                        for member in original.getmembers():
                            data = original.extractfile(member).read() if member.isfile() else None
                            if member.name == "x/billing/keeper/provider_withdrawal.go":
                                line = b"\tsettledLease.Reservation = cloneLeaseReservation(lease.Reservation)\n"
                                require(data.count(line) == 1, "Fixture must contain exactly one clone assignment")
                                data = data.replace(line, b"")
                                member.size = len(data)
                            changed.addfile(member, io.BytesIO(data) if data is not None else None)
                    archive = target.getvalue()
                    compressed = gzip.compress(archive, mtime=0)
                    archive_path.write_bytes(compressed)
                    manifest["baseline_archive"].update(sha256=sha256(compressed), uncompressed_sha256=sha256(archive))
                manifest_path.write_text(json.dumps(manifest, indent=2) + "\n")
                binaries = fixture / "bin"
                binaries.mkdir()
                go = binaries / "go"
                go.write_text(GO_STUB.format(python=sys.executable))
                go.chmod(0o700)
                marker = fixture / "go-calls.jsonl"
                overrides = {"PATH": str(binaries) + os.pathsep + os.environ.get("PATH", ""), "GO_CALL_LOG": str(marker), "STUB_MODE": name, "TARGET_DRIVER": str(driver)}
                environment = os.environ.copy()
                environment.pop("PYTHONOPTIMIZE", None)
                environment.update(overrides)
                argv = [sys.executable] + (["-O"] if optimized else []) + [str(driver), "--bundled", str(fixture / "work"), str(fixture / "evidence")]
                result = subprocess.run(argv, cwd=fixture, env=environment, capture_output=True, text=True)
                calls = [json.loads(line) for line in marker.read_text().splitlines()] if marker.exists() else []
                require(result.returncode != 0, f"{name}: tampered input or invalid outcome was accepted")
                require(diagnostic in result.stderr, f"{name}: wrong diagnostic: {result.stderr}")
                require(bool(calls) == runtime, f"{name}: unexpected Go invocation count {len(calls)}")
                report["runs"].append({
                    "case": name, "optimized": optimized, "argv": argv, "cwd": str(fixture),
                    "environment_overrides": overrides, "environment_removed": ["PYTHONOPTIMIZE"],
                    "exit_status": result.returncode, "stdout": result.stdout, "stderr": result.stderr,
                    "expected_diagnostic": diagnostic, "go_invocations": calls,
                    "rejected_before_go": not calls,
                    "tampered_input_manifest_sha256": sha256(manifest_path.read_bytes()),
                })
                print(f"{name} ({'-O' if optimized else 'plain'}): rejected; Go calls={len(calls)}", flush=True)
                (evidence / "checks.json").write_text(json.dumps(report, indent=2) + "\n")
    require((origin / "reproduce.py").read_bytes() == driver_source, "Shared driver changed during checks")
    report["finished_at_utc"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    (evidence / "checks.json").write_text(json.dumps(report, indent=2) + "\n")
    print("All 24 plain/optimized driver checks passed; no real Go command was executed.", flush=True)


if __name__ == "__main__":
    main()
