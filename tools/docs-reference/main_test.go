package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

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
