package storagecodec

import (
	"bytes"

	"github.com/cosmos/gogoproto/proto"
)

// Message is a generated storage protobuf with field-order marshaling.
type Message interface {
	proto.Message
	Marshal() ([]byte, error)
}

// Marshal uses generated field-order marshaling. Storage messages must contain
// no maps; their repeated fields are ordered slices, making the encoding
// deterministic. The returned bytes do not alias the marshaler's buffer.
func Marshal(message Message) ([]byte, error) {
	encoded, err := message.Marshal()
	if err != nil {
		return nil, err
	}
	return bytes.Clone(encoded), nil
}
