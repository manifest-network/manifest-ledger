package scripts_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const nodeScriptMock = `#!/usr/bin/env python3
import json, os, pathlib, sys
program = pathlib.Path(sys.argv[0]).name
args = sys.argv[1:]
with open(os.environ["NODE_FIXTURE_LOG"], "a") as log:
    log.write(json.dumps({"program": program, "args": args}) + "\n")
if program in ("make", "rm", "sleep"):
    sys.exit(0)  # Never build, delete, wait, or start a real node in this fixture.
if len(args) < 3 or args[0] != "--home":
    sys.exit("missing explicit --home")
home = pathlib.Path(args[1])
if not home.is_relative_to(pathlib.Path(os.environ["NODE_FIXTURE_ROOT"])):
    sys.exit("fixture refuses any write outside its private directory")
args = args[2:]
if args[:2] == ["keys", "add"]:
    sys.stdin.read()
elif args[0] == "init":
    config = home / "config"
    config.mkdir(parents=True, exist_ok=True)
    (config / "genesis.json").write_text("{}")
    (config / "config.toml").write_text(os.environ["NODE_FIXTURE_CONFIG"])
    (config / "app.toml").write_text(os.environ["NODE_FIXTURE_APP"])
elif args[0] in ("config", "genesis", "start"):
    pass
elif args[:2] == ["tx", "wasm"]:
    print(json.dumps({"txhash": "ABCDEF"}))
elif args[:2] == ["q", "tx"]:
    print(json.dumps({"events": [{"type": "instantiate", "attributes": [{"key": "_contract_address", "value": "manifest1contract"}]}]}))
elif args[:3] == ["q", "wasm", "contract"]:
    print(json.dumps({"address": args[3]}))
else:
    sys.exit("unexpected command: " + repr(args))
`

const nodeFixtureConfig = `laddr = "tcp://127.0.0.1:26657"
cors_allowed_origins = []
pprof_laddr = "localhost:6060"
p2p_laddr = "tcp://0.0.0.0:26656"
timeout_commit = "5s"
`

const nodeFixtureApp = `address = "tcp://localhost:1317"
enable = false
grpc_address = "localhost:9090"
grpc_web_address = "localhost:9091"
rosetta_address = ":8080"
`

type nodeScriptFixture struct {
	root, repo, binary, log string
	env                     []string
}

func newNodeScriptFixture(t *testing.T) nodeScriptFixture {
	t.Helper()
	for _, dependency := range []string{"bash", "jq", "python3", "realpath", "sed"} {
		_, err := exec.LookPath(dependency)
		require.NoError(t, err, "local node script fixtures require %s", dependency)
	}
	repo, err := filepath.Abs("..")
	require.NoError(t, err)
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	require.NoError(t, os.Mkdir(bin, 0o700))
	for _, name := range []string{"manifest binary", "make", "rm", "sleep"} {
		require.NoError(t, os.WriteFile(filepath.Join(bin, name), []byte(nodeScriptMock), 0o700)) //nolint:gosec // Owner execution is required for the isolated command fixture.
	}
	fixture := nodeScriptFixture{root: root, repo: repo, binary: filepath.Join(bin, "manifest binary"), log: filepath.Join(root, "calls.jsonl")}
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "HOME_DIR=") || strings.HasPrefix(value, "BINARY=") || strings.HasPrefix(value, "CLEAN=") {
			continue
		}
		fixture.env = append(fixture.env, value)
	}
	fixture.env = append(fixture.env,
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"BINARY="+fixture.binary, "NODE_FIXTURE_LOG="+fixture.log, "NODE_FIXTURE_ROOT="+root,
		"NODE_FIXTURE_CONFIG="+nodeFixtureConfig, "NODE_FIXTURE_APP="+nodeFixtureApp,
	)
	return fixture
}

func (f nodeScriptFixture) run(t *testing.T, script string, env ...string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command("bash", filepath.Join(f.repo, script)) //nolint:gosec // Test selects one of the repository's three helper scripts.
	cmd.Dir = f.root
	cmd.Env = append(append([]string(nil), f.env...), env...)
	return cmd.CombinedOutput()
}

func TestNodeHelpersTreatPathsAsLiteralArguments(t *testing.T) {
	for _, tc := range []struct {
		script, clean string
	}{
		{script: "scripts/test_node.sh", clean: "false"},
		{script: "scripts/test_node.sh", clean: "true"},
		{script: "scripts/upload_contract.sh", clean: "false"},
		{script: "network/manifest-1/set-genesis-params.sh", clean: "true"},
	} {
		t.Run(tc.script+"/clean="+tc.clean, func(t *testing.T) {
			f := newNodeScriptFixture(t)
			nodeHome := filepath.Join(f.root, "node $(touch INJECTED) `touch BACKTICK` * [abc]")
			configDir := filepath.Join(nodeHome, "config")
			require.NoError(t, os.MkdirAll(configDir, 0o700))
			for name, contents := range map[string]string{
				"genesis.json": "{}", "config.toml": nodeFixtureConfig, "app.toml": nodeFixtureApp,
			} {
				require.NoError(t, os.WriteFile(filepath.Join(configDir, name), []byte(contents), 0o600))
			}
			output, err := f.run(t, tc.script, "HOME_DIR="+nodeHome, "CLEAN="+tc.clean,
				"CHAIN_ID=chain value $(touch CHAIN_INJECTED)", "RPC=36657", "TIMEOUT_COMMIT=500ms")
			require.NoError(t, err, string(output))
			for _, marker := range []string{"INJECTED", "BACKTICK", "CHAIN_INJECTED"} {
				_, err := os.Stat(filepath.Join(f.root, marker))
				require.ErrorIs(t, err, os.ErrNotExist, "shell syntax in path/flag text must never execute")
			}
			log, err := os.ReadFile(f.log)
			require.NoError(t, err)
			var commands, resets int
			for line := range strings.SplitSeq(strings.TrimSpace(string(log)), "\n") {
				var call struct {
					Program string   `json:"program"`
					Args    []string `json:"args"`
				}
				require.NoError(t, json.Unmarshal([]byte(line), &call))
				switch call.Program {
				case "manifest binary":
					commands++
					require.GreaterOrEqual(t, len(call.Args), 3)
					require.Equal(t, []string{"--home", nodeHome}, call.Args[:2], "every node call must receive the complete literal home")
				case "rm":
					resets++
					require.Equal(t, []string{"-rf", "--", nodeHome}, call.Args, "reset must target exactly the validated directory")
				}
			}
			require.Positive(t, commands)
			if tc.clean == "true" {
				require.Equal(t, 1, resets)
			} else {
				require.Zero(t, resets)
			}
			if tc.script == "scripts/test_node.sh" {
				config, err := os.ReadFile(filepath.Join(configDir, "config.toml")) //nolint:gosec // Path is inside the test-owned temporary node home.
				require.NoError(t, err)
				require.Contains(t, string(config), `laddr = "tcp://0.0.0.0:36657"`)
				require.Contains(t, string(config), `timeout_commit = "500ms"`)
			}
		})
	}
}

func TestNodeHelpersRejectUnsafeHomesBeforeActions(t *testing.T) {
	for _, script := range []string{"scripts/test_node.sh", "scripts/upload_contract.sh", "network/manifest-1/set-genesis-params.sh"} {
		t.Run(script, func(t *testing.T) {
			f := newNodeScriptFixture(t)
			alias := filepath.Join(f.root, "home-alias")
			require.NoError(t, os.Symlink(os.Getenv("HOME"), alias))
			for _, nodeHome := range []string{"/", "/tmp", os.Getenv("HOME"), f.repo, f.root, alias} {
				output, err := f.run(t, script, "HOME_DIR="+nodeHome, "CLEAN=true")
				require.Error(t, err, nodeHome)
				require.Contains(t, string(output), "Unsafe HOME_DIR", nodeHome)
			}
			if script != "scripts/upload_contract.sh" {
				output, err := f.run(t, script, "CLEAN=true")
				require.Error(t, err)
				require.Contains(t, string(output), "Set HOME_DIR explicitly")
			}
			_, err := os.Stat(f.log)
			require.ErrorIs(t, err, os.ErrNotExist, "validation must run before binary calls, make, or deletion")
		})
	}
}

func TestNodeHelperRejectsSedSyntaxInConfigurationValues(t *testing.T) {
	f := newNodeScriptFixture(t)
	for _, value := range []string{"RPC=1|e touch SED_INJECTED", "RPC=99999999999999999999999", "TIMEOUT_COMMIT=5s|e touch SED_INJECTED"} {
		output, err := f.run(t, "scripts/test_node.sh", "HOME_DIR="+filepath.Join(f.root, "node"), "CLEAN=false", value)
		require.Error(t, err, string(output))
	}
	for _, path := range []string{f.log, filepath.Join(f.root, "SED_INJECTED")} {
		_, err := os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist, "malformed sed inputs must fail before any command/configuration write")
	}
}
