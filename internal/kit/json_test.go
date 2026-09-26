package kit

import (
	"bytes"
	"encoding/json"
	"testing"
)

// A string is written as json.Marshal writes it, whichever way it is.
func FuzzAppendJSONStringIsMarshal(f *testing.F) {
	for _, seed := range []string{"", "adyen", "<a&b>", "é", "\x00\n\t", "\xff", " ", `"\`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		want, err := json.Marshal(text)
		if err != nil {
			t.Fatal(err)
		}
		if got := AppendJSONString(nil, text); !bytes.Equal(got, want) {
			t.Fatalf("AppendJSONString(%q) = %s, want %s", text, got, want)
		}
	})
}
