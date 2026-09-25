package limits

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nethinwei/funroute"
)

// probe is one row of a table: what the row says it is, the program that
// shows it, and what the reader should know that the result does not say.
type probe struct {
	shown    string
	source   string
	contract string // "a: int; b: float"
	args     string // JSON
	note     string
}

// probeTable runs each probe and writes the table: the case, what a run
// gives, and the note.
func probeTable(t *testing.T, registry *funroute.Registry, cases []probe) string {
	t.Helper()
	var out strings.Builder
	out.WriteString("| 情形 | 结果 | 说明 |\n|---|---|---|\n")
	for _, c := range cases {
		out.WriteString("| " + c.shown + " | " + outcome(t, registry, c.source, c.contract, c.args) + " | " + c.note + " |\n")
	}
	return out.String()
}

// outcome is what running source gives: its value, or when and with which
// class it fails.
func outcome(t *testing.T, registry *funroute.Registry, source, contract, args string) string {
	t.Helper()
	text, _ := result(t, registry, source, contract, args)
	return text
}

// result is outcome and whether the run gave a value.
func result(t *testing.T, registry *funroute.Registry, source, contract, args string) (string, bool) {
	t.Helper()
	artifact, err := funroute.CompileExpr(source, registry, options(t, contract))
	if err != nil {
		return "编译期 `" + errorName(err) + "`", false
	}
	value, err := run(t, registry, artifact, args)
	if err != nil {
		return "运行时 `" + errorName(err) + "`", false
	}
	encoded, err := registry.EncodeJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	if text := string(encoded); len(text) <= 60 {
		return "`" + text + "`", true
	}
	return "`" + string(encoded[:57]) + "…`", true
}

func run(t *testing.T, registry *funroute.Registry, artifact *funroute.Artifact, args string) (funroute.Value, error) {
	t.Helper()
	runtime, err := funroute.Instantiate(artifact, registry)
	if err != nil {
		t.Fatalf("an artifact the compiler made does not load: %v", err)
	}
	if args == "" {
		args = "{}"
	}
	decoded, err := funroute.DecodeArgs(json.RawMessage(args))
	if err != nil {
		return funroute.Value{}, err
	}
	return runtime.Run(t.Context(), decoded, funroute.RunOptions{})
}

// options reads a contract written "a: int; b: float".
func options(t *testing.T, contract string) funroute.CompileOptions {
	t.Helper()
	var text funroute.TextContract
	for part := range strings.SplitSeq(contract, ";") {
		if name, typ, ok := strings.Cut(strings.TrimSpace(part), ":"); ok {
			text.Args = append(text.Args, funroute.TextArg{Name: strings.TrimSpace(name), Type: strings.TrimSpace(typ)})
		}
	}
	options, err := text.Options()
	if err != nil {
		t.Fatalf("contract %q: %v", contract, err)
	}
	return options
}

// succeeds reports whether source runs on args.
func succeeds(t *testing.T, registry *funroute.Registry, source, contract, args string) bool {
	t.Helper()
	_, ok := result(t, registry, source, contract, args)
	return ok
}
