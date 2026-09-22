package compile

import (
	"context"
	"testing"

	"funroute/lang/internal/machine"
)

// A string is a whole container or none of one. len() always counted its
// characters, but at() and in refused it — so "the first digit of the BIN" had
// to go through slice(), and "does this reason code mention timeout" through
// contains(), while the operators that mean exactly those things were errors.
func TestStringIsAContainerLikeTheOthers(t *testing.T) {
	registry := machine.CoreRegistry()
	for _, test := range []struct {
		name, source, arg, input string
		want                     any
	}{
		{"取字符", `card[0]`, "card", "4111111111111111", "4"},
		{"按码点而不是字节", `note[1]`, "note", "银行卡", "行"},
		{"长度也按码点", `len(note)`, "note", "银行卡", int64(3)},
		{"含子串", `"timeout" in reason`, "reason", "gateway_timeout", true},
		{"不含子串", `"fraud" in reason`, "reason", "gateway_timeout", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, _ := compileAndRun(t, test.source, registry,
				map[string]any{test.arg: test.input}, machine.RunOptions{Fuel: 1000})
			if value.Any() != test.want {
				t.Fatalf("%s = %#v, want %#v", test.source, value.Any(), test.want)
			}
		})
	}
	// Out of range is an error, the same as it is for an array.
	artifact, err := CompileExpr(`card[9]`, registry, CompileOptions{
		Args: []ArgSpec{{Name: "card", Type: machine.StringType}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Run(context.Background(), map[string]any{"card": "411"}, machine.RunOptions{Fuel: 100}); err == nil {
		t.Fatal("an index past the end of a string was accepted")
	}
}
