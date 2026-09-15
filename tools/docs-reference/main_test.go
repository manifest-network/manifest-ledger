package main

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/manifest-network/manifest-ledger/app"
)

func TestRunGeneratesActualReference(t *testing.T) {
	output := filepath.Join(t.TempDir(), "reference", "billing-sku.md")
	checkReferenceProcess(t, output, "generate")
	content, err := os.ReadFile(output) //nolint:gosec // Generated reference below t.TempDir.
	require.NoError(t, err)
	for _, name := range []string{"liftedinit.billing.v1.Query/CreditAccount", "liftedinit.sku.v1.Msg/CreateSKU", "--home"} {
		require.Contains(t, string(content), name, "missing generated surface: %s", name)
	}
	require.NotContains(t, string(content), "manifest-docs-reference-", "temporary paths must not leak into published defaults")

	// The production command seals the SDK configuration. Each further CLI
	// invocation therefore needs a fresh process, just as it does for users.
	checkReferenceProcess(t, output, "current")
	stale := []byte(strings.Replace(string(content), "liftedinit.billing.v1.Query/CreditAccount", "REMOVED-RPC", 1))
	require.NoError(t, os.WriteFile(output, stale, 0o600)) //nolint:gosec // The destination is fixed beneath t.TempDir, independent of generated content.
	checkReferenceProcess(t, output, "stale")
	afterCheck, err := os.ReadFile(output) //nolint:gosec // Generated reference below t.TempDir.
	require.NoError(t, err)
	require.Equal(t, stale, afterCheck, "check mode reports drift without repairing the evidence")
	require.NoError(t, os.Remove(output))
	checkReferenceProcess(t, output, "missing")
	blocked := filepath.Join(t.TempDir(), "blocked")
	require.NoError(t, os.WriteFile(blocked, []byte("keep"), 0o600))
	checkReferenceProcess(t, filepath.Join(blocked, "reference.md"), "blocked")
	unchanged, err := os.ReadFile(blocked) //nolint:gosec // Deliberate fixture below t.TempDir.
	require.NoError(t, err)
	require.Equal(t, "keep", string(unchanged))
}

func checkReferenceProcess(t *testing.T, output, scenario string) {
	t.Helper()
	executable, err := os.Executable()
	require.NoError(t, err)
	args := []string{"-test.run=^TestReferenceCommandProcess$"}
	// Go's test harness merges counter files from this same test binary.
	// Preserve subprocess coverage when the enclosing run is instrumented.
	if directory := flag.Lookup("test.gocoverdir"); directory != nil && directory.Value.String() != "" {
		args = append(args, "-test.gocoverdir="+directory.Value.String())
	}
	process := exec.CommandContext(t.Context(), executable, args...) //nolint:gosec // Relaunch this test binary, with fixed test selection.
	process.Env = append(os.Environ(), "MANIFEST_DOCS_TEST_SCENARIO="+scenario, "MANIFEST_DOCS_TEST_OUTPUT="+output)
	result, err := process.CombinedOutput()
	require.NoError(t, err, "%s: %s", scenario, result)
}

func TestReferenceCommandProcess(t *testing.T) {
	scenario := os.Getenv("MANIFEST_DOCS_TEST_SCENARIO")
	if scenario == "" {
		t.Skip("invoked by the reference command integration test")
	}
	originalHome := app.DefaultNodeHome
	err := run(os.Getenv("MANIFEST_DOCS_TEST_OUTPUT"), scenario != "blocked" && scenario != "generate")
	switch scenario {
	case "current", "generate":
		require.NoError(t, err)
	case "stale":
		require.ErrorContains(t, err, "reference is stale")
	case "missing":
		require.ErrorIs(t, err, os.ErrNotExist)
	case "blocked":
		require.Error(t, err)
	default:
		t.Fatalf("unknown command fixture %q", scenario)
	}
	require.Equal(t, originalHome, app.DefaultNodeHome, "successful and failed commands restore the application home")
}

func TestRenderIncludesReachableFieldsAndAllCommands(t *testing.T) {
	var file descriptorpb.FileDescriptorProto
	require.NoError(t, prototext.Unmarshal([]byte(`
name: "reference_test.proto"
package: "example"
syntax: "proto3"
message_type {
  name: "Input"
  oneof_decl { name: "selector" }
  field { name: "id" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING oneof_index: 0 }
  field { name: "name" number: 2 label: LABEL_OPTIONAL type: TYPE_STRING oneof_index: 0 }
}
message_type {
  name: "Node"
  field { name: "children" number: 1 label: LABEL_REPEATED type: TYPE_MESSAGE type_name: ".example.Node" }
  field { name: "state" number: 2 label: LABEL_OPTIONAL type: TYPE_ENUM type_name: ".example.State" }
  nested_type {
    name: "EntriesEntry"
    options { map_entry: true }
    field { name: "key" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING }
    field { name: "value" number: 2 label: LABEL_OPTIONAL type: TYPE_MESSAGE type_name: ".example.Node" }
  }
  field { name: "entries" number: 3 label: LABEL_REPEATED type: TYPE_MESSAGE type_name: ".example.Node.EntriesEntry" }
}
enum_type {
  name: "State"
  value { name: "UNKNOWN" number: 0 }
  value { name: "READY" number: 1 }
}
service {
  name: "Query"
  method { name: "Lookup" input_type: ".example.Input" output_type: ".example.Node" }
  method { name: "NewlyAdded" input_type: ".example.Input" output_type: ".example.Node" }
}
service {
  name: "Msg"
  method { name: "Change" input_type: ".example.Input" output_type: ".example.Node" }
}
`), &file))
	descriptor, err := protodesc.NewFile(&file, nil)
	require.NoError(t, err)
	registry := new(protoregistry.Files)
	require.NoError(t, registry.RegisterFile(descriptor))

	root := &cobra.Command{Use: "manifestd"}
	root.PersistentFlags().String("home", "/private/reference-home", "Node home")
	for _, category := range []string{"query", "tx"} {
		parent := &cobra.Command{Use: category}
		module := &cobra.Command{Use: "example"}
		root.AddCommand(parent)
		parent.AddCommand(module)
		for _, name := range []string{"zeta", "alpha"} {
			command := &cobra.Command{Use: name + " [id]", Short: "Look up a record", Run: func(*cobra.Command, []string) {}}
			if name == "alpha" {
				command.Flags().String("meta-hash", "", "Hash <hex> | empty clears\nReview first")
				require.NoError(t, command.MarkFlagRequired("meta-hash"))
			} else {
				command.Flags().Bool("inactive", false, "Include inactive records")
				command.Flags().Bool("old-flag", false, "Legacy spelling")
				require.NoError(t, command.Flags().MarkDeprecated("old-flag", "use --inactive"))
			}
			module.AddCommand(command)
		}
	}

	content, err := render(root, registry, []moduleSpec{{name: "example", namespace: "example"}}, "/private/reference-home")
	require.NoError(t, err)
	text := string(content)
	for _, fragment := range []string{
		"`example.Query/Lookup`", "`example.Query/NewlyAdded`", "`example.Msg/Change`",
		"| `id` | 1 | `string` | oneof selector |",
		"| `children` | 1 | [`example.Node`](#type-example-node) | repeated |",
		"| `entries` | 3 | map&lt;`string`, [`example.Node`](#type-example-node)&gt; | map |",
		"| `READY` | 1 |",
		"| `--meta-hash` | `string` | `\"\"` | yes | Hash &lt;hex&gt; \\| empty clears<br>Review first |",
		"(deprecated: use --inactive)",
		"| `--home` | `string` | `\"~/.manifest\"` | no | Node home |",
	} {
		require.Contains(t, text, fragment)
	}
	require.Equal(t, 1, strings.Count(text, "### `example.Node`"), "recursive message graphs must terminate and render each type once")
	require.NotContains(t, text, "### `example.Node.EntriesEntry`", "synthetic map entries are represented by their map field")
	require.NotContains(t, text, "/private/reference-home")
	require.Equal(t, 2, strings.Count(text, "| `--home` |"), "inherited flags appear once per command category, not once per command")
	for _, category := range []string{"query", "tx"} {
		alpha := strings.Index(text, "### `manifestd "+category+" example alpha [id]`")
		zeta := strings.Index(text, "### `manifestd "+category+" example zeta [id]`")
		require.GreaterOrEqual(t, alpha, 0)
		require.Greater(t, zeta, alpha, "command order must be deterministic")
	}

	_, err = render(root, registry, []moduleSpec{{name: "example", namespace: "missing"}}, "/private/reference-home")
	require.ErrorContains(t, err, "resolve service missing.Query")
	_, err = render(root, registry, []moduleSpec{{name: "missing", namespace: "example"}}, "/private/reference-home")
	require.ErrorContains(t, err, "module command query missing missing")
}
