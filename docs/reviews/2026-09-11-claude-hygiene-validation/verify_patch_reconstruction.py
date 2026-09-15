#!/usr/bin/env python3
"""Seed archived baselines and replay recorded patch checks in fresh repositories.

replay TRANSCRIPT EVIDENCE_DIRECTORY NEW_WORK_DIRECTORY NEW_OUTPUT_JSON
accepts either a reconstruction report or terminal verification.json. It
executes each recorded portable_argv after binding only {python}, {verifier},
{evidence}, and {work}. No Go test or historical Git object is needed.
"""

import argparse
import base64
import datetime
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile


def require(condition, message):
    if not condition:
        raise SystemExit(message)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def checked_bytes(path, expected):
    data = Path(path).read_bytes()
    require(digest(data) == expected, f"SHA-256 mismatch: {path}")
    return data


def normalized(data):
    return "\n".join(line.expandtabs(8).rstrip() for line in data.decode().splitlines()).rstrip() + "\n"


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def recorded_steps(record):
    for name in (record["name"], record["source_path"], record["patch"], record["baseline"]["path"]):
        path = Path(name)
        require(not path.is_absolute() and ".." not in path.parts, "Transcript paths must stay inside their bound directories")
    require(len(Path(record["name"]).parts) == 1, "Fixture name must be a single directory component")
    fixture = "{work}/" + record["name"]
    target = fixture + "/" + record["source_path"]
    helper = ["{python}", "-I", "-O", "{verifier}"]
    baseline = record["baseline"]
    seed_argv = helper + ["seed"]
    if baseline["kind"] == "archive":
        seed_argv += ["--archive", "{evidence}/" + baseline["path"], "--archive-sha256", baseline["archive_sha256"], "--member", record["source_path"]]
    else:
        require(baseline["kind"] == "file", "Unknown baseline kind")
        seed_argv += ["--file", "{evidence}/" + baseline["path"]]
    seed_argv += ["--sha256", record["baseline_sha256"], "--destination", target]
    return [
        {"name": "init", "portable_argv": ["git", "init", "--quiet", fixture]},
        {"name": "seed-baseline", "portable_argv": seed_argv},
        {"name": "verify-patch", "portable_argv": helper + ["hash", "{evidence}/" + record["patch"], record["patch_sha256"]]},
        {"name": "apply-patch", "portable_argv": ["git", "-C", fixture, "apply", "--unidiff-zero", "{evidence}/" + record["patch"]]},
        {"name": "verify-result", "portable_argv": helper + ["hash", target, record["mutant_sha256"]]},
    ]


def seed(args):
    if args.archive:
        checked_bytes(args.archive, args.archive_sha256)
        with tarfile.open(args.archive, "r:gz") as archive:
            member = archive.getmember(args.member)
            require(member.isfile(), "Baseline archive member must be a regular file")
            data = archive.extractfile(member).read()
        require(digest(data) == args.sha256, "Baseline archive member SHA-256 mismatch")
    else:
        data = checked_bytes(args.file, args.sha256)
    target = Path(args.destination)
    target.parent.mkdir(parents=True, exist_ok=True)
    with target.open("xb") as output:
        output.write(data)
    require(digest(target.read_bytes()) == args.sha256, "Seeded baseline SHA-256 mismatch")
    print(json.dumps({"seeded_path": str(target), "baseline_sha256": args.sha256}))


def replay(args):
    transcript_path = Path(args.transcript).resolve()
    transcript_bytes = transcript_path.read_bytes()
    transcript = json.loads(transcript_bytes)
    records = transcript.get("patch_reconstruction_checks", transcript.get("records"))
    require(isinstance(records, list) and records, "Transcript must contain reconstruction records")
    evidence = Path(args.evidence).resolve()
    work = Path(args.work).resolve()
    output = Path(args.output).resolve()
    require(not work.exists(), "Reconstruction work directory must be new")
    require(not output.exists(), "Reconstruction output file must be new")
    driver = Path(__file__).resolve()
    bindings = {"python": sys.executable, "verifier": str(driver), "evidence": str(evidence), "work": str(work)}
    overrides = {"LC_ALL": "C", "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull}
    environment = os.environ.copy()
    environment.update(overrides)
    report = {
        "driver_argv": sys.orig_argv,
        "python_executable": sys.executable,
        "python_version": sys.version,
        "driver_sha256": digest(driver.read_bytes()),
        "driver_cwd": str(Path.cwd()),
        "replayed_transcript": str(transcript_path),
        "replayed_transcript_sha256": digest(transcript_bytes),
        "bindings": bindings,
        "environment_overrides": overrides,
        "environment_note": "Other host environment entries are inherited. Git system/global config is disabled; no credentials are archived.",
        "display_normalization": "splitlines + expandtabs(8) + per-line rstrip + overall rstrip + one LF",
        "started_at_utc": now(),
        "records": [],
    }
    for record in records:
        expected = recorded_steps(record)
        recorded = [{key: step[key] for key in ("name", "portable_argv")} for step in record["steps"]]
        require(recorded == expected, "Transcript must record all five canonical reconstruction steps")
        derived = {"steps", "reconstructed_sha256", "matches_recorded_mutant", "exit_status"}
        updated = {key: value for key, value in record.items() if key not in derived}
        updated["steps"] = []
        for step in record["steps"]:
            portable = step["portable_argv"]
            argv = [arg.format_map(bindings) for arg in portable]
            started = now()
            result = subprocess.run(argv, cwd=evidence, env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
            captured = {
                "name": step["name"], "portable_argv": portable, "argv": argv,
                "cwd": str(evidence), "started_at_utc": started,
                "finished_at_utc": now(), "exit_status": result.returncode,
            }
            for stream, data in [("stdout", result.stdout), ("stderr", result.stderr)]:
                display = normalized(data) if data else ""
                captured[stream] = {
                    "raw_encoding": "base64", "raw": base64.b64encode(data).decode(),
                    "raw_sha256": digest(data), "raw_bytes": len(data),
                    "display": display, "display_sha256": digest(display.encode()),
                }
            updated["steps"].append(captured)
            if result.returncode != 0:
                updated["exit_status"] = result.returncode
                updated["matches_recorded_mutant"] = False
                report["records"].append(updated)
                output.parent.mkdir(parents=True, exist_ok=True)
                output.write_text(json.dumps(report, indent=2) + "\n")
                raise SystemExit(f"Reconstruction failed: {record['name']} / {step['name']}")
        reconstructed = work / record["name"] / record["source_path"]
        checked_bytes(reconstructed, record["mutant_sha256"])
        updated["reconstructed_sha256"] = digest(reconstructed.read_bytes())
        updated["matches_recorded_mutant"] = True
        updated["exit_status"] = 0
        report["records"].append(updated)
    report["finished_at_utc"] = now()
    report["all_reconstructions_match"] = True
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, indent=2) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    seed_parser = commands.add_parser("seed")
    source = seed_parser.add_mutually_exclusive_group(required=True)
    source.add_argument("--file")
    source.add_argument("--archive")
    seed_parser.add_argument("--archive-sha256")
    seed_parser.add_argument("--member")
    seed_parser.add_argument("--sha256", required=True)
    seed_parser.add_argument("--destination", required=True)
    hash_parser = commands.add_parser("hash")
    hash_parser.add_argument("file")
    hash_parser.add_argument("sha256")
    replay_parser = commands.add_parser("replay")
    for name in ("transcript", "evidence", "work", "output"):
        replay_parser.add_argument(name)
    args = parser.parse_args()
    if args.command == "seed":
        require(not args.archive or (args.archive_sha256 and args.member), "Archive seeding requires an archive hash and member")
        seed(args)
    elif args.command == "hash":
        checked_bytes(args.file, args.sha256)
        print(json.dumps({"path": args.file, "sha256": args.sha256, "matches": True}))
    else:
        replay(args)


if __name__ == "__main__":
    main()
