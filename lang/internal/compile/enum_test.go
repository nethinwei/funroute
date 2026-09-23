package compile

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"funroute/lang/internal/machine"
)

func TestEnumContractAndExhaustiveSwitch(t *testing.T) {
	t.Parallel()
	channel, registry := enumFixture(t)
	result := channel
	options := CompileOptions{
		Args:   []ArgSpec{{Name: "channel", Type: channel}},
		Result: &result,
	}
	artifact, err := CompileExpr(`switch(channel, case @adyen => @stripe, case @stripe => @adyen)`, registry, options)
	if err != nil {
		t.Fatal(err)
	}
	if !artifact.Result.Equal(channel) || !artifact.Args[0].Type.Equal(channel) {
		t.Fatalf("artifact contract = %s -> %s, want %s -> %s", artifact.Args[0].Type, artifact.Result, channel, channel)
	}
	again, err := CompileJSON(artifact.ExprJSON, registry, options)
	if err != nil {
		t.Fatal(err)
	}
	if again.Digest != artifact.Digest {
		t.Fatalf("enum switch ExprJSON digest = %q, want %q", again.Digest, artifact.Digest)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(), map[string]any{"channel": "adyen"}, machine.RunOptions{Fuel: 100})
	if text, _ := value.String(); err != nil || text != "stripe" {
		t.Fatalf("Run(channel=adyen) = %q, %v, want \"stripe\"", text, err)
	}
	if _, err := runtime.Run(t.Context(), map[string]any{"channel": "other"}, machine.RunOptions{Fuel: 100}); err == nil {
		t.Fatal("runtime accepted a string outside the enum")
	} else if !errors.Is(err, machine.ErrContract) {
		t.Fatalf("runtime enum error is not a contract error: %v", err)
	}

	assertEnumCompileErrors(t, registry, options)
}

func assertEnumCompileErrors(t *testing.T, registry *machine.Registry, options CompileOptions) {
	t.Helper()
	channels := machine.ArrayOf(options.Args[0].Type)
	if err := registry.EnableForm(machine.ReduceForm); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		source string
		want   string
	}{
		{`switch(channel, case @adyen => @adyen)`, "missing stripe"},
		{`switch(channel, case @adyen => @adyen, case @other => @stripe, else @adyen)`, "not a member"},
		{`switch(channel, case "adyen" => @adyen, case "stripe" => @stripe)`, `"adyen" is a member of enum<channel>{adyen,stripe}, written @adyen`},
		{`switch(channel, case @adyen => "adyen", case @stripe => "stripe")`, "returns string"},
		{`fallback(channel, candidate)`, "no overload"},
		{`candidate`, "returns string"},
		{`reduce(item in channels, acc = channel, candidate)`, "accumulator type"},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			testOptions := options
			if strings.Contains(test.source, "candidate") {
				testOptions.Args = append(testOptions.Args, ArgSpec{Name: "candidate", Type: machine.StringType})
			}
			if strings.HasPrefix(test.source, "reduce") {
				testOptions.Args = append(testOptions.Args, ArgSpec{Name: "channels", Type: channels})
			}
			assertCompileErrorContains(t, registry, test.source, testOptions, test.want)
		})
	}
}

// assertCompileErrorContains checks that source fails to compile with an
// ErrCompile whose message contains want.
func assertCompileErrorContains(t *testing.T, registry *machine.Registry, source string, options CompileOptions, want string) {
	t.Helper()
	_, err := CompileExpr(source, registry, options)
	if !errors.Is(err, machine.ErrCompile) || !strings.Contains(err.Error(), want) {
		t.Errorf("CompileExpr(%q) error = %v, want ErrCompile containing %q", source, err, want)
	}
}

func enumFixture(t *testing.T) (machine.Type, *machine.Registry) {
	t.Helper()
	channel, err := machine.ParseType(`enum<channel>{stripe,adyen}`)
	if err != nil || channel.String() != `enum<channel>{adyen,stripe}` {
		t.Fatalf("ParseType(enum<channel>{stripe,adyen}) = %s, %v, want enum<channel>{adyen,stripe}", channel, err)
	}
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.SwitchForm); err != nil {
		t.Fatal(err)
	}
	return channel, registry
}

// A member reference is resolved in the contract's enum namespace, so it is
// typed wherever it appears — no surrounding type context needed.
func TestEnumMembersResolveThroughTheContract(t *testing.T) {
	t.Parallel()
	channel, registry := enumFixture(t)
	backup, err := machine.ParseType(`enum<backup>{adyen,paypal}`)
	if err != nil {
		t.Fatal(err)
	}
	text := machine.StringType
	artifact, err := CompileExpr(`let(preferred = @stripe, if(channel == preferred, "same", "switched"))`,
		registry, CompileOptions{Args: []ArgSpec{{Name: "channel", Type: channel}}, Result: &text})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(), map[string]any{"channel": "adyen"}, machine.RunOptions{Fuel: 100})
	if got, _ := value.String(); err != nil || got != "switched" {
		t.Fatalf("Run(channel=adyen) = %q, %v, want \"switched\"", got, err)
	}
	again, err := CompileJSON(artifact.ExprJSON, registry,
		CompileOptions{Args: []ArgSpec{{Name: "channel", Type: channel}}, Result: &text})
	if err != nil || again.Digest != artifact.Digest {
		t.Fatalf("enum member ExprJSON round trip = %q, %v, want %q", again.Digest, err, artifact.Digest)
	}
	assertEnumReferenceErrors(t, registry, channel, backup)
}

func assertEnumReferenceErrors(t *testing.T, registry *machine.Registry, channel, backup machine.Type) {
	t.Helper()
	both := CompileOptions{
		Args:   []ArgSpec{{Name: "channel", Type: channel}, {Name: "backup", Type: backup}},
		Result: &channel,
	}
	for _, test := range []struct{ source, want string }{
		{`@adyen`, "ambiguous; it is a member of backup and channel, so write @backup.adyen"},
		{`@channel.paypal`, `"paypal" is not a member of enum<channel>{adyen,stripe}`},
		{`@tier.gold`, `the contract declares no enum named "tier"`},
		{`@unknown`, "is not a member of any enum in this contract"},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			assertCompileErrorContains(t, registry, test.source, both, test.want)
		})
	}
	qualified, err := CompileExpr(`@channel.adyen`, registry, both)
	if err != nil || !qualified.Result.Equal(channel) {
		t.Fatalf("CompileExpr(@channel.adyen) = %v, %v, want a program returning %s", qualified, err, channel)
	}
}

// An enum is nominal: it never stands in for a string, so the only way across
// is the string(enum) conversion.
func TestEnumConvertsToStringOnlyExplicitly(t *testing.T) {
	t.Parallel()
	channel, registry := enumFixture(t)
	text := machine.StringType
	options := CompileOptions{Args: []ArgSpec{{Name: "channel", Type: channel}}, Result: &text}
	if _, err := CompileExpr(`channel + ":settled"`, registry, options); !errors.Is(err, machine.ErrCompile) {
		t.Fatalf("implicit enum concatenation error = %v, want ErrCompile", err)
	}
	artifact, err := CompileExpr(`string(channel) + ":settled"`, registry, options)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(), map[string]any{"channel": "adyen"}, machine.RunOptions{Fuel: 100})
	if got, _ := value.String(); err != nil || got != "adyen:settled" {
		t.Fatalf("Run(channel=adyen) = %q, %v, want \"adyen:settled\"", got, err)
	}
}

// A contract may declare hundreds of members; an error message shows a sample
// and a count, while the type text itself stays complete and re-parsable.
func TestLargeEnumIsSummarizedInMessagesOnly(t *testing.T) {
	t.Parallel()
	members := make([]string, 212)
	for i := range members {
		members[i] = fmt.Sprintf("c%03d", i)
	}
	country := machine.EnumOf("country", members...)
	if text := country.String(); len(text) < 1000 {
		t.Fatalf("type text should stay complete, got %d bytes", len(text))
	}
	again, err := machine.ParseType(country.String())
	if err != nil || !again.Equal(country) {
		t.Fatalf("type text must parse back: %v", err)
	}
	summary := country.Summary()
	if want := "enum<country>{c000,c001,c002,c003,c004,c005… 共 212 个}"; summary != want {
		t.Fatalf("summary = %s, want %s", summary, want)
	}
	if nested := machine.ArrayOf(country).Summary(); nested != "array<"+summary+">" {
		t.Fatalf("nested summary = %s, want array<%s>", nested, summary)
	}
	registry := machine.CoreRegistry()
	_, err = CompileExpr(`@zz`, registry, CompileOptions{Args: []ArgSpec{{Name: "c", Type: country}}, Result: &country})
	if err == nil || strings.Contains(err.Error(), "c100") || !strings.Contains(err.Error(), "共 212 个") {
		t.Fatalf("unknown member error = %v, want a summary with 共 212 个 and no c100", err)
	}
}

func TestEnumTypeRejectsMalformedDeclarations(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`enum<channel>{}`,
		`enum<channel>{a,a}`,
		`enum<channel>{"a"}`,
		`enum<channel>{1a}`,
		`enum<channel>{case}`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if got, err := machine.ParseType(source); err == nil {
				t.Fatalf("ParseType(%q) = %s, want an error", source, got)
			}
		})
	}
}

func TestEnumMembersInsideContainersAreChecked(t *testing.T) {
	t.Parallel()
	channel, registry := enumFixture(t)
	result := machine.ArrayOf(channel)
	artifact, err := CompileExpr(`[@adyen, @stripe]`, registry, CompileOptions{Result: &result})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(), nil, machine.RunOptions{Fuel: 100})
	items, ok := value.Array()
	if err != nil || !ok || len(items) != 2 {
		t.Fatalf("enum array = %#v, %v, want two members", value.Any(), err)
	}
	if _, err := CompileExpr(`[@adyen, @other]`, registry, CompileOptions{Result: &result}); !errors.Is(err, machine.ErrCompile) {
		t.Fatalf("invalid enum array error = %v, want ErrCompile", err)
	}
	artifact, err = CompileExpr(`channels`, registry, CompileOptions{
		Args:   []ArgSpec{{Name: "channels", Type: result}},
		Result: &result,
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err = machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Run(t.Context(), map[string]any{
		"channels": []string{"adyen", "stripe"},
	}, machine.RunOptions{Fuel: 100}); err != nil {
		t.Fatalf("native enum array input = %v", err)
	}
}

// A comprehension can return enums in each of its shapes: a dictionary
// comprehension yields them as values, and a multi-clause one splices the
// inner list into the outer.
func TestComprehensionsCanReturnEnums(t *testing.T) {
	t.Parallel()
	channel, registry := enumFixture(t)
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	args := []ArgSpec{{Name: "channels", Type: machine.ArrayOf(channel)}, {Name: "keys", Type: machine.ArrayOf(machine.StringType)}}
	for source, result := range map[string]machine.Type{
		`{k: @adyen for k in keys}`:                 machine.DictOf(channel),
		`[c for c in channels for k in keys]`:       machine.ArrayOf(channel),
		`[@stripe for c in channels for k in keys]`: machine.ArrayOf(channel),
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := CompileExpr(source, registry, CompileOptions{Args: args, Result: &result}); err != nil {
				t.Errorf("%s -> %s: %v", source, result, err)
			}
		})
	}
}

// An enum reaches the contract's namespace from wherever it sits in a type,
// not just from the top of an argument. A host that passes the whole order in
// declares its channel enum as a field of that record — which is the shape a
// routing rule actually gets — and `order.channel == @adyen` has to resolve
// the member from it. Following Type.Elem alone used to miss every one of
// these, so the namespace saw an enum in array<enum> but not in
// record{channel: enum}.
func TestEnumsAreCollectedFromAnyDepthOfTheContract(t *testing.T) {
	t.Parallel()
	channel, registry := enumFixture(t)
	nested := machine.RecordOf(machine.Field{
		Name: "inner",
		Type: machine.RecordOf(machine.Field{Name: "channel", Type: channel}),
	})
	for _, shape := range []struct {
		name   string
		typ    machine.Type
		source string
	}{
		{"字段", machine.RecordOf(machine.Field{Name: "channel", Type: channel}), `order.channel == @adyen`},
		{"字段的字段", nested, `order.inner.channel == @adyen`},
		{"record 数组的字段", machine.ArrayOf(machine.RecordOf(machine.Field{Name: "channel", Type: channel})), `order[0].channel == @adyen`},
		{"record 里的枚举数组", machine.RecordOf(machine.Field{Name: "channels", Type: machine.ArrayOf(channel)}), `@adyen in order.channels`},
	} {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			if _, err := CompileExpr(shape.source, registry, CompileOptions{
				Args: []ArgSpec{{Name: "order", Type: shape.typ}},
			}); err != nil {
				t.Fatalf("%s: %v", shape.source, err)
			}
		})
	}

	// The exhaustiveness check reaches it too: a switch on a record's enum
	// field must still name every member.
	order := machine.RecordOf(machine.Field{Name: "channel", Type: channel})
	args := []ArgSpec{{Name: "order", Type: order}}
	if _, err := CompileExpr(`switch(order.channel, case @adyen => 1, case @stripe => 2)`, registry, CompileOptions{Args: args}); err != nil {
		t.Fatalf("exhaustive switch on a field: %v", err)
	}
	_, err := CompileExpr(`switch(order.channel, case @adyen => 1)`, registry, CompileOptions{Args: args})
	if err == nil || !strings.Contains(err.Error(), "missing stripe") {
		t.Fatalf("a switch missing a member compiled: %v, want an error naming missing stripe", err)
	}
}
