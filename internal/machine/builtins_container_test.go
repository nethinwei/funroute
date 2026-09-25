package machine_test

import (
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

func compileAndRun(t *testing.T, source string, registry *machine.Registry, args map[string]any) (machine.Value, *machine.Runtime) {
	t.Helper()
	artifact, err := compile.CompileExpr(source, registry, compile.CompileOptions{})
	if err != nil {
		t.Fatalf("compile %s: %v", source, err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatalf("instantiate %s: %v", source, err)
	}
	value, err := runtime.Run(t.Context(), args)
	if err != nil {
		t.Fatalf("run %s: %v", source, err)
	}
	return value, runtime
}

// A string is a whole container or none of one. len() always counted its
// characters, but at() and in refused it — so "the first digit of the BIN" had
// to go through slice(), and "does this reason code mention timeout" through
// contains(), while the operators that mean exactly those things were errors.
func TestStringIsAContainerLikeTheOthers(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	for _, test := range []struct {
		name, source, arg, input string
		want                     any
	}{
		{"取字符", `card[0]`, "card", "4111111111111111", "4"},
		{"按码点而不是字节", `note[1]`, "note", "银行卡", "行"},
		{"长度也按码点", `len(note)`, "note", "银行卡", int64(3)},
		{"不是 UTF-8 的字节原样取出", `note[1]`, "note", "1\xff", "\xff"},
		{"含子串", `"timeout" in reason`, "reason", "gateway_timeout", true},
		{"不含子串", `"fraud" in reason`, "reason", "gateway_timeout", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value, _ := compileAndRun(t, test.source, registry,
				map[string]any{test.arg: test.input})
			if value.Any() != test.want {
				t.Fatalf("%s = %#v, want %#v", test.source, value.Any(), test.want)
			}
		})
	}
	// Out of range is an error, the same as it is for an array.
	artifact, err := compile.CompileExpr(`card[9]`, registry, compile.CompileOptions{
		Args: []compile.ArgSpec{{Name: "card", Type: machine.StringType}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Run(t.Context(), map[string]any{"card": "411"}); err == nil {
		t.Fatal("an index past the end of a string was accepted")
	}
}
