package machine

import (
	"bytes"
	"fmt"
	"io"

	"github.com/nethinwei/funroute/internal/kit"
)

// DecodeArgs reads a program's arguments from a JSON object. Numbers stay
// json.Number, so an integer keeps every digit on its way to the contract's
// type; anything after the object is refused rather than ignored.
func DecodeArgs(data []byte) (map[string]any, error) {
	args := map[string]any{}
	if len(bytes.TrimSpace(data)) == 0 {
		return args, nil
	}
	decoder := kit.NumberDecoder(data)
	if err := decoder.Decode(&args); err != nil {
		return nil, kit.Classify(ErrContract, "args: ", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("%w: args: trailing data after the object", ErrContract)
	}
	return args, nil
}
