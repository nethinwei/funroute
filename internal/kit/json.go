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

// AppendJSONString appends text as json.Marshal writes it: between quotes as
// it is when nothing in it needs escaping, which a name and most text do not,
// and through json.Marshal otherwise.
func AppendJSONString(dst []byte, text string) []byte {
	for i := range len(text) {
		if b := text[i]; b < 0x20 || b >= 0x80 || b == '"' || b == '\\' || b == '<' || b == '>' || b == '&' {
			encoded, _ := json.Marshal(text)
			return append(dst, encoded...)
		}
	}
	dst = append(dst, '"')
	dst = append(dst, text...)
	return append(dst, '"')
}
