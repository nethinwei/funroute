package conformance

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/nethinwei/funroute"
	"github.com/nethinwei/funroute/internal/demo"
)

// example is one entry of the workbench's examples: what the language takes
// and what it gives. Only what a run needs is read.
type example struct {
	Label    string                 `json:"label"`
	Source   string                 `json:"source"`
	Contract *funroute.TextContract `json:"contract"`
	Args     json.RawMessage        `json:"args"`
	Expected any                    `json:"expected"`
}

func readExamples(t *testing.T) []example {
	t.Helper()
	encoded, err := os.ReadFile("../../web/funroute-examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Version  int       `json:"version"`
		Examples []example `json:"examples"`
	}
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 1 || len(manifest.Examples) == 0 {
		t.Fatalf("the examples: version %d, %d of them", manifest.Version, len(manifest.Examples))
	}
	return manifest.Examples
}

func TestEveryExampleHoldsThroughThePipeline(t *testing.T) {
	t.Parallel()
	registry, err := demo.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range readExamples(t) {
		t.Run(item.Label, func(t *testing.T) {
			t.Parallel()
			options, err := item.Contract.Options()
			if err != nil {
				t.Fatalf("the contract: %v", err)
			}
			artifact := compiled(t, item.Source, registry, options)
			assertOneDigest(t, item, registry, options, artifact)
			if got := runJSON(t, reloaded(t, artifact), registry, item.Args); !reflect.DeepEqual(got, item.Expected) {
				t.Fatalf("%s with %s = %#v, want %#v", item.Source, item.Args, got, item.Expected)
			}
		})
	}
}

// assertOneDigest compiles the example's ExprJSON, its formatted text and the
// contract the artifact carries, and holds each to the artifact's digest.
func assertOneDigest(t *testing.T, item example, registry *funroute.Registry, options funroute.CompileOptions, artifact *funroute.Artifact) {
	t.Helper()
	exprJSON, err := funroute.ParseToJSON(item.Source)
	if err != nil {
		t.Fatal(err)
	}
	fromJSON, err := funroute.CompileJSON(exprJSON, registry, options)
	if err != nil {
		t.Fatalf("its ExprJSON does not compile: %v", err)
	}
	formatted, err := funroute.Format(item.Source)
	if err != nil {
		t.Fatalf("it does not format: %v", err)
	}
	for name, other := range map[string]*funroute.Artifact{
		"its ExprJSON":                    fromJSON,
		"its formatted text":              compiled(t, formatted, registry, options),
		"the contract its artifact holds": compiled(t, item.Source, registry, funroute.ContractFromArtifact(artifact)),
	} {
		if other.Digest() != artifact.Digest() {
			t.Errorf("%s compiles to %s, the source to %s", name, other.Digest(), artifact.Digest())
		}
	}
}

func compiled(t *testing.T, source string, registry *funroute.Registry, options funroute.CompileOptions) *funroute.Artifact {
	t.Helper()
	artifact, err := funroute.CompileExpr(source, registry, options)
	if err != nil {
		t.Fatalf("CompileExpr(%q): %v", source, err)
	}
	return artifact
}

// reloaded is the artifact written as JSON and read back, as a host that
// stores artifacts does.
func reloaded(t *testing.T, artifact *funroute.Artifact) *funroute.Artifact {
	t.Helper()
	encoded, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	var back funroute.Artifact
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatal(err)
	}
	return &back
}

// runJSON runs artifact on args and gives its value the way a person reads
// it — money as "USD 1.70" — decoded as encoding/json decodes the examples.
func runJSON(t *testing.T, artifact *funroute.Artifact, registry *funroute.Registry, args json.RawMessage) any {
	t.Helper()
	runtime, err := funroute.Instantiate(artifact, registry)
	if err != nil {
		t.Fatalf("the artifact read back does not load: %v", err)
	}
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	decoded, err := funroute.DecodeArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(), decoded)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	encoded, err := registry.EncodeJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	var got any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	return got
}
