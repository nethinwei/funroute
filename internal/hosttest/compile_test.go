package hosttest

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nethinwei/funroute"
)

// hostRegistry is the console a host builds: the kernel, the forms it allows,
// and its own domain functions.
func hostRegistry(t *testing.T) *funroute.Registry {
	t.Helper()
	registry := funroute.CoreRegistry()
	if err := registry.EnableForm(funroute.SwitchForm); err != nil {
		t.Fatal(err)
	}
	err := funroute.Logic(registry, "route.is_healthy_v1", funroute.Doc{
		Label:       "渠道是否健康",
		Description: "UP 表示可用",
		Category:    "路由",
	}, func(status string) (bool, error) { return status == "UP", nil })
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

const hostSource = `switch(case route.is_healthy_v1(health) && doubled > 100 => "adyen", else => "stripe")`

// hostContract is what a console reads from its rule record: the arguments in
// ABI order, their types and the prose it shows operators.
func hostContract() funroute.CompileOptions {
	result := funroute.StringType
	return funroute.CompileOptions{
		Args: []funroute.ArgSpec{
			{Name: "health", Type: funroute.StringType, Doc: "渠道健康状态"},
			{Name: "doubled", Type: funroute.IntType, Doc: "订单金额的两倍，单位：分"},
		},
		Result:    &result,
		ResultDoc: "渠道号",
	}
}

func TestPhaseZeroPublicContract(t *testing.T) {
	t.Parallel()
	registry := funroute.CoreRegistry()
	if _, err := funroute.CompileExpr("if(", registry, funroute.CompileOptions{}); !errors.Is(err, funroute.ErrCompile) {
		t.Fatalf("CompileExpr(%q) error = %v, want ErrCompile", "if(", err)
	}
	if err := funroute.ValidateContract(funroute.CompileOptions{
		Args: []funroute.ArgSpec{{Name: "x", Type: funroute.IntType}, {Name: "x", Type: funroute.IntType}},
	}); !errors.Is(err, funroute.ErrContract) {
		t.Fatalf("ValidateContract(x declared twice) error = %v, want ErrContract", err)
	}
	if got := funroute.EnumOf("channel", "stripe", "adyen").String(); got != `enum<channel>{adyen,stripe}` {
		t.Fatalf("EnumOf(channel, stripe, adyen) = %s, want enum<channel>{adyen,stripe}", got)
	}
}

func TestHostCanCompileAndShipAnArtifact(t *testing.T) {
	t.Parallel()
	registry := hostRegistry(t)
	artifact, err := funroute.CompileExpr(hostSource, registry, hostContract())
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Args()) != 2 || artifact.Args()[0].Name() != "health" {
		t.Fatalf("artifact.Args() = %#v, want health then doubled", artifact.Args())
	}
	if !artifact.Result().Equal(funroute.StringType) || artifact.Digest() == "" {
		t.Fatalf("result = %s digest = %q, want string and a digest", artifact.Result(), artifact.Digest())
	}

	// An artifact is meant to be stored and shipped as JSON.
	encoded, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	var shipped funroute.Artifact
	if err := json.Unmarshal(encoded, &shipped); err != nil {
		t.Fatal(err)
	}
	runtime, err := funroute.Instantiate(&shipped, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(t.Context(), map[string]any{"health": "UP", "doubled": 500}, funroute.RunOptions{Fuel: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := result.String(); text != "adyen" {
		t.Fatalf("Run(health=UP, doubled=500) = %#v, want \"adyen\"", result.Any())
	}
}

// Programs are exchanged as ExprJSON: that is the contract with a front end,
// and it must be lossless down to the digest. The contract itself travels
// separately, in the host's own record.
func TestHostCanRoundTripAProgramThroughJSON(t *testing.T) {
	t.Parallel()
	registry := hostRegistry(t)
	artifact, err := funroute.CompileExpr(hostSource, registry, hostContract())
	if err != nil {
		t.Fatal(err)
	}
	document, err := funroute.ParseToJSON(hostSource)
	if err != nil {
		t.Fatal(err)
	}
	again, err := funroute.CompileJSON(document, registry, hostContract())
	if err != nil {
		t.Fatal(err)
	}
	if again.Digest() != artifact.Digest() {
		t.Fatalf("round trip changed the digest:\n%s\n%s", artifact.Digest(), again.Digest())
	}
	// The artifact carries the contract back, so a host that stored only the
	// artifact can still render the rule for a human.
	args, result, resultDoc := funroute.ContractFromArtifact(artifact)
	text := funroute.RenderWithContract(hostSource, args, result, resultDoc)
	if !strings.Contains(text, "health:") || !strings.Contains(text, "渠道健康状态") {
		t.Fatalf("rendered view = %s, want the argument health with its doc", text)
	}
	checkSameProgram(t, "the rendered view", text, registry, artifact.Digest())
	// Formatting keeps the contract comments and is still the same program.
	formatted, err := funroute.Format(text)
	if err != nil || !strings.HasPrefix(formatted, "// health:") {
		t.Fatalf("formatted view = %s (%v), want it to start with \"// health:\"", formatted, err)
	}
	checkSameProgram(t, "the formatted view", formatted, registry, artifact.Digest())
}

// checkSameProgram compiles text against the host contract and requires the
// digest of the original: comments and layout are not syntax.
func checkSameProgram(t *testing.T, what, text string, registry *funroute.Registry, digest string) {
	t.Helper()
	artifact, err := funroute.CompileExpr(text, registry, hostContract())
	if err != nil {
		t.Fatalf("compiling %s: %v", what, err)
	}
	if artifact.Digest() != digest {
		t.Fatalf("%s compiles to digest %s, want %s", what, artifact.Digest(), digest)
	}
}

// What a host exchanges besides text: the ExprJSON document names its
// version, a handle carries an engine's value by name, and a manifest lists
// each signature.
func TestTheDocumentsAHostExchanges(t *testing.T) {
	t.Parallel()
	encoded, err := funroute.ParseToJSON(`1 + 2`)
	if err != nil {
		t.Fatal(err)
	}
	var document struct{ Version int }
	if err := json.Unmarshal(encoded, &document); err != nil || document.Version != funroute.ExprJSONVersion {
		t.Fatalf("the document is version %d, want %d (%v)", document.Version, funroute.ExprJSONVersion, err)
	}
	if handle := funroute.NewHandle("onnx.tensor", []float32{1}); handle.Type().String() != "handle<onnx.tensor>" {
		t.Fatalf("NewHandle(\"onnx.tensor\").Type() = %s, want handle<onnx.tensor>", handle.Type())
	}
	manifest, err := json.Marshal(funroute.CoreRegistry().Manifest())
	if err != nil || !strings.Contains(string(manifest), `"name":"add"`) {
		t.Fatalf("the manifest is %s, %v, want the kernel's functions by name", manifest, err)
	}
}
