package kit

import (
	"bytes"
	"encoding/json"
)

// NumberDecoder reads JSON from data with every number kept as json.Number,
// the text it was written as, so none goes through a float64.
func NumberDecoder(data []byte) *json.Decoder {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder
}
