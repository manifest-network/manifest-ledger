"""Bounded CLI check using a disposable loopback-only single-validator fixture."""
from pathlib import Path
import hashlib
import json
import os
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request

binary = str(Path(sys.argv[1]).resolve())
evidence_file = Path(sys.argv[2]).resolve()
scratch = Path(os.environ.get("TMPDIR", tempfile.gettempdir()))
env = dict(os.environ, GOMAXPROCS="4")
evidence = []
with tempfile.TemporaryDirectory(dir=scratch, prefix="export-cli-fixture-") as folder:
    fixture = Path(folder)

    def run(args):
        return subprocess.run([binary, *args], capture_output=True, text=True, timeout=60, env=env)

    result = run(["testnet", "init-files", "--v", "1", "--output-dir", str(fixture / "network"),
                  "--node-daemon-home", "daemon", "--chain-id", "export-cli-fixture",
                  "--keyring-backend", "test", "--single-host", "--commit-timeout", "200ms",
                  "--home", str(fixture / "cli")])
    assert result.returncode == 0, result.stderr
    home = fixture / "network/node0/daemon"
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    rpc = f"http://127.0.0.1:{port}"
    with (fixture / "node.log").open("w") as log:
        node = subprocess.Popen([
            binary, "start", "--home", str(home), "--rpc.laddr", f"tcp://127.0.0.1:{port}",
            "--p2p.laddr", "tcp://127.0.0.1:0", "--p2p.seeds", "", "--p2p.persistent_peers", "",
            "--p2p.pex=false", "--grpc.enable=false", "--api.enable=false", "--pruning", "nothing",
            "--minimum-gas-prices", "0testtoken"], stdout=log, stderr=log, env=env)
        try:
            deadline = time.monotonic() + 60
            while True:
                if node.poll() is not None:
                    raise RuntimeError((fixture / "node.log").read_text()[-4000:])
                try:
                    with urllib.request.urlopen(rpc + "/status", timeout=1) as response:
                        status = json.load(response)
                    height = int(status["result"]["sync_info"]["latest_block_height"])
                    if height >= 3:
                        break
                except (OSError, ValueError):
                    pass
                if time.monotonic() > deadline:
                    raise RuntimeError("isolated node did not commit in time")
                time.sleep(0.25)
            with urllib.request.urlopen(rpc + f"/block?height={height}", timeout=2) as response:
                block = json.load(response)
            source_time = block["result"]["block"]["header"]["time"]
            genesis = json.loads((home / "config/genesis.json").read_text())
            address = genesis["app_state"]["auth"]["accounts"][0]["address"]
            query_flags = ["--height", str(height), "--output", "json", "--node", rpc, "--home", str(home)]
            listing = run(["query", "circuit", "accounts", "--page-limit", "100", *query_flags])
            assert listing.returncode == 0, listing.stderr
            inventory = json.loads(listing.stdout)
            assert not inventory.get("accounts") and not inventory.get("pagination", {}).get("next_key"), inventory
            single = run(["query", "circuit", "account", address, *query_flags])
            assert single.returncode != 0 and "InvalidArgument" in single.stderr, single.stderr
            evidence.extend([
                "Isolated single-validator fixture; no peers or public listener.",
                f"Selected committed height: {height}", f"Source block time: {source_time}",
                "Circuit inventory: " + listing.stdout.strip(),
                "Absent explicit account lookup exit: " + str(single.returncode), single.stderr.strip()])
        finally:
            if node.poll() is None:
                node.send_signal(signal.SIGTERM)
            try:
                node.wait(timeout=20)
            except subprocess.TimeoutExpired:
                node.kill()
                node.wait()

    base = ["export", "--home", str(home), "--height", str(height), "--for-zero-height"]
    redirected = run(base)
    assert redirected.returncode == 0, redirected.stderr
    contaminated = fixture / "redirected-genesis.json"
    contaminated.write_text(redirected.stdout)
    try:
        json.loads(redirected.stdout)
    except json.JSONDecodeError:
        pass
    else:
        raise AssertionError("expected default stdout logging before JSON")
    bad = run(["genesis", "validate", str(contaminated), "--home", str(fixture / "validate")])
    assert bad.returncode != 0, bad.stdout
    exported = fixture / "exported-genesis.json"
    clean = run([*base, "--output-document", str(exported)])
    assert clean.returncode == 0, clean.stderr
    state = json.loads(exported.read_text())
    assert state["initial_height"] in [0, "0"]
    assert state["genesis_time"] == genesis["genesis_time"]
    raw_hash = hashlib.sha256(exported.read_bytes()).hexdigest()
    state["genesis_time"] = source_time
    restart = fixture / "restart-genesis.json"
    restart.write_text(json.dumps(state) + "\n")
    valid = run(["genesis", "validate", str(restart), "--home", str(fixture / "validate")])
    assert valid.returncode == 0, valid.stderr
    assert hashlib.sha256(exported.read_bytes()).hexdigest() == raw_hash
    evidence.extend([
        "$ manifestd export --home /fixture/node --height SOURCE_HEIGHT --for-zero-height > /fixture/redirected-genesis.json",
        "export exit: 0", "stdout invariant log count: " + str(redirected.stdout.count("asserting crisis invariants")),
        "redirected file JSON decode: failed (expected)", "genesis validate redirected file exit: " + str(bad.returncode), bad.stderr.strip(),
        "$ manifestd export --home /fixture/node --height SOURCE_HEIGHT --for-zero-height --output-document /fixture/exported-genesis.json",
        "export exit: 0", "output document JSON decode: passed", "output document preserves original genesis_time; initial_height: 0",
        "stdout invariant log count: " + str(clean.stdout.count("asserting crisis invariants")),
        "Separate restart copy uses source block time; raw export hash unchanged.",
        "$ manifestd genesis validate /fixture/restart-genesis.json", "validation exit: 0", valid.stdout.strip(), valid.stderr.strip()])
    evidence_file.write_text("\n".join(evidence).replace(folder, "/fixture") + "\n")
    print("PASS: stdout redirection contaminates JSON; --output-document produces clean, validate-passing output.")
    print("PASS: absent circuit permission query errors while complete same-height inventory is empty.")
