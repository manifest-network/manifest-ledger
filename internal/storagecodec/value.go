// Package storagecodec adapts public collection values to versioned disk
// representations while retaining support for legacy wire-format values.
package storagecodec

import (
	"bytes"
	"fmt"

	"cosmossdk.io/collections/codec"
)

// Value delegates the public JSON representation to Wire and stores binary
// values through EncodeStorage and DecodeStorage. Prefix must start with a
// zero byte, which cannot begin a valid legacy protobuf message. Module names
// and version prefixes are part of the existing storage/error contract.
// Version tags must be prefix-free: a future tag must not extend an existing
// tag (for example, v10 cannot follow v1 without a distinct version framing).
// All callbacks are required, including NormalizeLegacy for untagged values.
type Value[T any] struct {
	Wire            codec.ValueCodec[T]
	Prefix          string
	Module          string
	EncodeStorage   func(T) ([]byte, error)
	DecodeStorage   func([]byte) (T, error)
	NormalizeLegacy func(T) (T, error)
}

// Encode prefixes the module's binary storage representation with its version tag.
func (c Value[T]) Encode(value T) ([]byte, error) {
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
func (c Value[T]) Decode(encoded []byte) (T, error) {
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
func (c Value[T]) EncodeJSON(value T) ([]byte, error) { return c.Wire.EncodeJSON(value) }

// DecodeJSON reads the public wire representation without a storage version tag.
func (c Value[T]) DecodeJSON(encoded []byte) (T, error) { return c.Wire.DecodeJSON(encoded) }

// Stringify preserves the public wire codec's diagnostic representation.
func (c Value[T]) Stringify(value T) string { return c.Wire.Stringify(value) }

// ValueType returns the public wire codec's type identifier.
func (c Value[T]) ValueType() string { return c.Wire.ValueType() }
