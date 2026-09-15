package storagecodec

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
)

func stringConfig() Config[string] {
	return Config[string]{
		Wire:   collections.StringValue,
		Prefix: "\x00test/value/v1",
		Module: "test",
		EncodeStorage: func(value string) ([]byte, error) {
			return []byte("stored:" + value), nil
		},
		DecodeStorage: func(encoded []byte) (string, error) {
			return strings.TrimPrefix(string(encoded), "stored:"), nil
		},
		NormalizeLegacy: func(value string) (string, error) { return strings.ToUpper(value), nil },
	}
}

func TestValuePreservesStorageAndWireContracts(t *testing.T) {
	config := stringConfig()
	c := New(config)
	// Configuration is copied at construction; later caller changes cannot
	// remove required callbacks or change a collection's storage discriminator.
	config.Prefix = "changed"
	config.DecodeStorage = nil
	encoded, err := c.Encode("sample")
	require.NoError(t, err)
	require.Equal(t, []byte("\x00test/value/v1stored:sample"), encoded)
	decoded, err := c.Decode(encoded)
	require.NoError(t, err)
	require.Equal(t, "sample", decoded)
	decoded, err = c.Decode([]byte("legacy"))
	require.NoError(t, err)
	require.Equal(t, "LEGACY", decoded)
	decoded, err = c.Decode(nil)
	require.NoError(t, err)
	require.Empty(t, decoded)

	jsonBytes, err := c.EncodeJSON("sample")
	require.NoError(t, err)
	require.Equal(t, []byte(`"sample"`), jsonBytes)
	decoded, err = c.DecodeJSON(jsonBytes)
	require.NoError(t, err)
	require.Equal(t, "sample", decoded)
	require.Equal(t, collections.StringValue.Stringify("sample"), c.Stringify("sample"))
	require.Equal(t, collections.StringValue.ValueType(), c.ValueType())

	for _, prefix := range []string{"\x00test/value/v2", "\x00test/other/v1", "\x00other/value/v1", "\x00"} {
		_, err = c.Decode([]byte(prefix))
		require.EqualError(t, err, "unsupported test storage encoding")
	}
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	for _, prefix := range []string{
		"", "test/value/v1", "\x00", "\x00test/value", "\x00test/value/v0",
		"\x00test/value/v10", "\x00test/value/v1/v1", "\x00/value/v1", "\x00test//v1",
		"\x00test/value/vx", "\x00test/value\x00/v1",
	} {
		t.Run(prefix, func(t *testing.T) {
			config := stringConfig()
			config.Prefix = prefix
			require.Panics(t, func() { New(config) })
		})
	}
	for _, test := range []struct {
		name   string
		change func(*Config[string])
	}{
		{"wire", func(c *Config[string]) { c.Wire = nil }},
		{"typed nil wire", func(c *Config[string]) { c.Wire = (*nilStringCodec)(nil) }},
		{"module", func(c *Config[string]) { c.Module = "" }},
		{"encode", func(c *Config[string]) { c.EncodeStorage = nil }},
		{"decode", func(c *Config[string]) { c.DecodeStorage = nil }},
		{"normalize", func(c *Config[string]) { c.NormalizeLegacy = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := stringConfig()
			test.change(&config)
			require.Panics(t, func() { New(config) })
		})
	}
}

func TestAcceptedVersionPrefixesArePrefixFree(t *testing.T) {
	// Keep every existing tag byte-for-byte compatible. Single-digit versions
	// avoid the ambiguous v1/v10 extension without adding a new wire delimiter.
	var prefixes []string
	for _, entity := range []string{"billing/params", "billing/lease", "billing/credit-account", "sku/params", "sku/provider"} {
		for version := byte('1'); version <= '9'; version++ {
			prefix := "\x00" + entity + "/v" + string(version)
			config := stringConfig()
			config.Prefix = prefix
			require.NotPanics(t, func() { New(config) })
			prefixes = append(prefixes, prefix)
		}
	}
	for i, prefix := range prefixes {
		for j, other := range prefixes {
			if i != j {
				require.False(t, strings.HasPrefix(prefix, other), "%q extends %q", prefix, other)
			}
		}
	}
}

func TestValuePropagatesCallbackErrorsAndDoesNotAliasPayload(t *testing.T) {
	fault := errors.New("codec failed")
	for _, operation := range []string{"encode", "storage decode", "wire decode", "normalize"} {
		t.Run(operation, func(t *testing.T) {
			config := stringConfig()
			switch operation {
			case "encode":
				config.EncodeStorage = func(string) ([]byte, error) { return nil, fault }
			case "storage decode":
				config.DecodeStorage = func([]byte) (string, error) { return "", fault }
			case "wire decode":
				config.Wire = nilStringCodec{decodeErr: fault}
			case "normalize":
				config.NormalizeLegacy = func(string) (string, error) { return "", fault }
			}
			c := New(config)
			var err error
			switch operation {
			case "encode":
				_, err = c.Encode("sample")
			case "storage decode":
				_, err = c.Decode([]byte(config.Prefix))
			default:
				_, err = c.Decode([]byte("legacy"))
			}
			require.ErrorIs(t, err, fault)
		})
	}
	payload := []byte("stored:sample")
	config := stringConfig()
	config.EncodeStorage = func(string) ([]byte, error) { return payload, nil }
	encoded, err := New(config).Encode("sample")
	require.NoError(t, err)
	before := bytes.Clone(encoded)
	payload[0] = 'X'
	require.Equal(t, before, encoded)
}

type nilStringCodec struct{ decodeErr error }

func (nilStringCodec) Encode(value string) ([]byte, error) {
	return collections.StringValue.Encode(value)
}

func (c nilStringCodec) Decode(encoded []byte) (string, error) {
	if c.decodeErr != nil {
		return "", c.decodeErr
	}
	return collections.StringValue.Decode(encoded)
}

func (nilStringCodec) EncodeJSON(value string) ([]byte, error) {
	return collections.StringValue.EncodeJSON(value)
}

func (nilStringCodec) DecodeJSON(encoded []byte) (string, error) {
	return collections.StringValue.DecodeJSON(encoded)
}
func (nilStringCodec) Stringify(value string) string { return collections.StringValue.Stringify(value) }
func (nilStringCodec) ValueType() string             { return collections.StringValue.ValueType() }
