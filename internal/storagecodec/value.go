// Package storagecodec adapts public collection values to versioned disk
// representations while retaining support for legacy wire-format values.
package storagecodec

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"

	"cosmossdk.io/collections/codec"
)

// Config describes a public value's wire and storage representations. Prefix
// must use the existing \x00namespace/entity/vN format, with N a single digit
// from 1 to 9. This makes all accepted prefixes prefix-free without changing
// stored bytes. Later versions must use distinct framing or a new namespace;
// extending v1 to v10 would be ambiguous with a v1 protobuf payload.
// All codecs and callbacks are required, including NormalizeLegacy.
type Config[T any] struct {
	Wire            codec.ValueCodec[T]
	Prefix          string
	Module          string
	EncodeStorage   func(T) ([]byte, error)
	DecodeStorage   func([]byte) (T, error)
	NormalizeLegacy func(T) (T, error)
}

// New validates the programmer-supplied configuration before a collection is
// built. Invalid configuration panics at keeper construction, rather than at
// the first consensus-state read or write. The returned codec keeps a private
// copy so callers cannot subsequently replace its required dependencies.
func New[T any](config Config[T]) codec.ValueCodec[T] {
	if config.Wire == nil || isNilCodec(config.Wire) {
		panic("storagecodec: Wire is required")
	}
	if config.EncodeStorage == nil || config.DecodeStorage == nil || config.NormalizeLegacy == nil {
		panic("storagecodec: all storage and legacy callbacks are required")
	}
	if config.Module == "" {
		panic("storagecodec: Module is required")
	}
	if len(config.Prefix) == 0 || config.Prefix[0] != 0 {
		panic("storagecodec: Prefix must begin with a zero byte")
	}
	parts := strings.Split(config.Prefix[1:], "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || strings.ContainsRune(config.Prefix[1:], 0) ||
		len(parts[2]) != 2 || parts[2][0] != 'v' || parts[2][1] < '1' || parts[2][1] > '9' {
		panic("storagecodec: Prefix must use namespace/entity/vN with a single version digit from 1 to 9")
	}
	return value[T]{config}
}

func isNilCodec(value any) bool {
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

type value[T any] struct{ Config[T] }

// Encode prefixes the module's binary storage representation with its version tag.
func (c value[T]) Encode(value T) ([]byte, error) {
	payload, err := c.EncodeStorage(value)
	if err != nil {
		return nil, err
	}
	encoded := make([]byte, len(c.Prefix)+len(payload))
	copy(encoded, c.Prefix)
	copy(encoded[len(c.Prefix):], payload)
	return encoded, nil
}

// Decode reads the current storage version or normalizes an untagged legacy
// protobuf value. Other tag-zero prefixes are rejected as unsupported versions.
func (c value[T]) Decode(encoded []byte) (T, error) {
	if bytes.HasPrefix(encoded, []byte(c.Prefix)) {
		return c.DecodeStorage(encoded[len(c.Prefix):])
	}
	if len(encoded) > 0 && encoded[0] == 0 {
		var zero T
		return zero, fmt.Errorf("unsupported %s storage encoding", c.Module)
	}
	value, err := c.Wire.Decode(encoded)
	if err != nil {
		var zero T
		return zero, err
	}
	return c.NormalizeLegacy(value)
}

// EncodeJSON preserves the public wire codec's JSON representation.
func (c value[T]) EncodeJSON(value T) ([]byte, error) { return c.Wire.EncodeJSON(value) }

// DecodeJSON reads the public wire representation without a storage version tag.
func (c value[T]) DecodeJSON(encoded []byte) (T, error) { return c.Wire.DecodeJSON(encoded) }

// Stringify preserves the public wire codec's diagnostic representation.
func (c value[T]) Stringify(value T) string { return c.Wire.Stringify(value) }

// ValueType returns the public wire codec's type identifier.
func (c value[T]) ValueType() string { return c.Wire.ValueType() }
