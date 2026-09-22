package compile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"funroute/lang/internal/machine"
)

func TestCompileAndContractErrorsAreTyped(t *testing.T) {
	registry := machine.CoreRegistry()
	if _, err := CompileExpr(`if(`, registry, CompileOptions{}); !errors.Is(err, machine.ErrCompile) {
		t.Fatalf("parse error = %v", err)
	}
	if _, err := CompileExpr(`x`, registry, CompileOptions{
		Args: []ArgSpec{{Name: "y", Type: machine.IntType}},
	}); !errors.Is(err, machine.ErrContract) || errors.Is(err, machine.ErrCompile) {
		t.Fatalf("contract error = %v", err)
	}
	if err := ValidateContract(CompileOptions{
		Args: []ArgSpec{{Name: "x", Type: machine.IntType}, {Name: "x", Type: machine.IntType}},
	}); !errors.Is(err, machine.ErrContract) {
		t.Fatalf("standalone contract error = %v", err)
	}
}

func TestFallbackCatchesOnlyExtensionAndDeadline(t *testing.T) {
	registry := machine.CoreRegistry()
	if err := machine.Logic(registry, "fail_v1", machine.Doc{}, func(value int64) (int64, error) {
		return 0, errors.New("engine unavailable")
	}); err != nil {
		t.Fatal(err)
	}
	if err := machine.Logic(registry, "panic_v1", machine.Doc{}, func(value int64) (int64, error) {
		panic("engine panic")
	}); err != nil {
		t.Fatal(err)
	}
	if err := machine.Logic(registry, "slow_v1", machine.Doc{Timeout: time.Millisecond}, func(ctx context.Context, value int64) (int64, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}

	for _, source := range []string{
		`fallback(fail_v1(x), 7)`,
		`fallback(panic_v1(x), 7)`,
		`fallback(slow_v1(x), x + 4)`,
	} {
		value, err := compileAndRunInt(t, source, registry, 100)
		if err != nil || value != 7 {
			t.Fatalf("%s = %d, %v", source, value, err)
		}
	}

	_, err := compileAndRunInt(t, `fallback(x / 0, 7)`, registry, 100)
	if err == nil || errors.Is(err, machine.ErrExtension) || errors.Is(err, machine.ErrDeadline) {
		t.Fatalf("kernel error was made catchable: %v", err)
	}
	_, err = compileAndRunInt(t, `fallback(fail_v1(x), x / 0, 7)`, registry, 100)
	if err == nil || errors.Is(err, machine.ErrExtension) || errors.Is(err, machine.ErrDeadline) {
		t.Fatalf("middle kernel error was made catchable: %v", err)
	}
	_, err = compileAndRunInt(t, `fallback(fail_v1(x), 7)`, registry, 1)
	if !errors.Is(err, machine.ErrFuel) {
		t.Fatalf("fuel error was caught: %v", err)
	}
}

func TestFallbackIsLazyOnSuccess(t *testing.T) {
	registry := machine.CoreRegistry()
	called := 0
	if err := machine.Logic(registry, "default_v1", machine.Doc{}, func(value int64) (int64, error) {
		called++
		return 9, nil
	}); err != nil {
		t.Fatal(err)
	}
	value, err := compileAndRunInt(t, `fallback(x, default_v1(x))`, registry, 100)
	if err != nil || value != 3 || called != 0 {
		t.Fatalf("result = %d, calls = %d, err = %v", value, called, err)
	}
}

func TestFallbackTriesAnyNumberOfCandidatesInOrder(t *testing.T) {
	registry := machine.CoreRegistry()
	var calls []string
	register := func(name string, succeed bool) {
		err := machine.Logic(registry, name, machine.Doc{}, func(value int64) (int64, error) {
			calls = append(calls, name)
			if !succeed {
				return 0, errors.New("provider unavailable")
			}
			return value + 10, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	register("first_v1", false)
	register("second_v1", false)
	register("third_v1", true)
	register("unused_v1", true)
	value, err := compileAndRunInt(t, `fallback(first_v1(x),second_v1(x),third_v1(x),unused_v1(x))`, registry, 100)
	if err != nil || value != 13 || strings.Join(calls, ",") != "first_v1,second_v1,third_v1" {
		t.Fatalf("result = %d, calls = %v, err = %v", value, calls, err)
	}
	calls = nil
	value, err = compileAndRunInt(t, `fallback(first_v1(x),second_v1(x),x + 9)`, registry, 100)
	if err != nil || value != 12 || strings.Join(calls, ",") != "first_v1,second_v1" {
		t.Fatalf("final result = %d, calls = %v, err = %v", value, calls, err)
	}
	if _, err := compileAndRunInt(t, `fallback(first_v1(x),second_v1(x))`, registry, 100); !errors.Is(err, machine.ErrExtension) {
		t.Fatalf("last candidate error = %v", err)
	}
	if _, err := CompileExpr(`fallback(x)`, registry, CompileOptions{}); err == nil || !strings.Contains(err.Error(), "at least 2") {
		t.Fatalf("single candidate error = %v", err)
	}
	if _, err := CompileExpr(`fallback(x,"bad",7)`, registry, CompileOptions{}); err == nil {
		t.Fatal("fallback accepted candidates with different types")
	}
}

func compileAndRunInt(t *testing.T, source string, registry *machine.Registry, fuel uint64) (int64, error) {
	t.Helper()
	artifact, err := CompileExpr(source, registry, CompileOptions{
		Args: []ArgSpec{{Name: "x", Type: machine.IntType}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(context.Background(), map[string]any{"x": int64(3)}, machine.RunOptions{Fuel: fuel})
	if err != nil {
		return 0, err
	}
	result, _ := value.Int()
	return result, nil
}

func TestEnumContractAndExhaustiveSwitch(t *testing.T) {
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
		t.Fatalf("artifact contract = %s -> %s", artifact.Args[0].Type, artifact.Result)
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
	value, err := runtime.Run(context.Background(), map[string]any{"channel": "adyen"}, machine.RunOptions{Fuel: 100})
	if text, _ := value.String(); err != nil || text != "stripe" {
		t.Fatalf("run = %q, %v", text, err)
	}
	if _, err := runtime.Run(context.Background(), map[string]any{"channel": "other"}, machine.RunOptions{Fuel: 100}); err == nil {
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
		{`reduce(item in channels, acc from channel, candidate)`, "accumulator type"},
	} {
		testOptions := options
		if strings.Contains(test.source, "candidate") {
			testOptions.Args = append(testOptions.Args, ArgSpec{Name: "candidate", Type: machine.StringType})
		}
		if strings.HasPrefix(test.source, "reduce") {
			testOptions.Args = append(testOptions.Args, ArgSpec{Name: "channels", Type: channels})
		}
		_, err := CompileExpr(test.source, registry, testOptions)
		if !errors.Is(err, machine.ErrCompile) || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: %v", test.source, err)
		}
	}
}

func enumFixture(t *testing.T) (machine.Type, *machine.Registry) {
	t.Helper()
	channel, err := machine.ParseType(`enum<channel>{stripe,adyen}`)
	if err != nil || channel.String() != `enum<channel>{adyen,stripe}` {
		t.Fatalf("enum = %s, %v", channel, err)
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
	value, err := runtime.Run(context.Background(), map[string]any{"channel": "adyen"}, machine.RunOptions{Fuel: 100})
	if got, _ := value.String(); err != nil || got != "switched" {
		t.Fatalf("run = %q, %v", got, err)
	}
	again, err := CompileJSON(artifact.ExprJSON, registry,
		CompileOptions{Args: []ArgSpec{{Name: "channel", Type: channel}}, Result: &text})
	if err != nil || again.Digest != artifact.Digest {
		t.Fatalf("enum member ExprJSON round trip = %q, %v", again.Digest, err)
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
		_, err := CompileExpr(test.source, registry, both)
		if !errors.Is(err, machine.ErrCompile) || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: %v", test.source, err)
		}
	}
	qualified, err := CompileExpr(`@channel.adyen`, registry, both)
	if err != nil || !qualified.Result.Equal(channel) {
		t.Fatalf("qualified member = %v, %v", qualified, err)
	}
}

// An enum is nominal: it never stands in for a string, so the only way across
// is the string(enum) conversion.
func TestEnumConvertsToStringOnlyExplicitly(t *testing.T) {
	channel, registry := enumFixture(t)
	text := machine.StringType
	options := CompileOptions{Args: []ArgSpec{{Name: "channel", Type: channel}}, Result: &text}
	if _, err := CompileExpr(`channel + ":settled"`, registry, options); !errors.Is(err, machine.ErrCompile) {
		t.Fatalf("implicit enum concatenation error = %v", err)
	}
	artifact, err := CompileExpr(`string(channel) + ":settled"`, registry, options)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(context.Background(), map[string]any{"channel": "adyen"}, machine.RunOptions{Fuel: 100})
	if got, _ := value.String(); err != nil || got != "adyen:settled" {
		t.Fatalf("run = %q, %v", got, err)
	}
}

// A contract may declare hundreds of members; an error message shows a sample
// and a count, while the type text itself stays complete and re-parsable.
func TestLargeEnumIsSummarizedInMessagesOnly(t *testing.T) {
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
	if summary != "enum<country>{c000,c001,c002,c003,c004,c005… 共 212 个}" {
		t.Fatalf("summary = %s", summary)
	}
	if nested := machine.ArrayOf(country).Summary(); nested != "array<"+summary+">" {
		t.Fatalf("nested summary = %s", nested)
	}
	registry := machine.CoreRegistry()
	_, err = CompileExpr(`@zz`, registry, CompileOptions{Args: []ArgSpec{{Name: "c", Type: country}}, Result: &country})
	if err == nil || strings.Contains(err.Error(), "c100") || !strings.Contains(err.Error(), "共 212 个") {
		t.Fatalf("unknown member error = %v", err)
	}
}

func TestEnumTypeRejectsMalformedDeclarations(t *testing.T) {
	for _, source := range []string{
		`enum<channel>{}`,
		`enum<channel>{a,a}`,
		`enum<channel>{"a"}`,
		`enum<channel>{1a}`,
		`enum<channel>{case}`,
	} {
		if _, err := machine.ParseType(source); err == nil {
			t.Fatalf("accepted %s", source)
		}
	}
}

func TestEnumMembersInsideContainersAreChecked(t *testing.T) {
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
	value, err := runtime.Run(context.Background(), nil, machine.RunOptions{Fuel: 100})
	items, ok := value.Array()
	if err != nil || !ok || len(items) != 2 {
		t.Fatalf("enum array = %#v, %v", value.Any(), err)
	}
	if _, err := CompileExpr(`[@adyen, @other]`, registry, CompileOptions{Result: &result}); !errors.Is(err, machine.ErrCompile) {
		t.Fatalf("invalid enum array error = %v", err)
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
	if _, err := runtime.Run(context.Background(), map[string]any{
		"channels": []string{"adyen", "stripe"},
	}, machine.RunOptions{Fuel: 100}); err != nil {
		t.Fatalf("native enum array input = %v", err)
	}
}
