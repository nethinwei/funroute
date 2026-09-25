package compile

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
)

func TestArrayDictionaryAndGenericFunctions(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	registerCollectionTestExtensions(t, registry)
	artifact, err := CompileExpr(
		`if(has(weights,key),get(weights,key),get([0.25,0.5],fallback))`,
		registry,
		CompileOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []machine.Parameter{
		machine.NewParameter("weights", machine.DictOf(machine.FloatType), ""),
		machine.NewParameter("key", machine.StringType, ""),
		machine.NewParameter("fallback", machine.IntType, ""),
	}
	if len(artifact.Args()) != len(want) {
		t.Fatalf("args = %#v, want %#v", artifact.Args(), want)
	}
	for i := range want {
		if artifact.Args()[i].Name() != want[i].Name() || !artifact.Args()[i].Type().Equal(want[i].Type()) {
			t.Fatalf("arg %d = %#v, want %#v", i, artifact.Args()[i], want[i])
		}
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(t.Context(), map[string]any{
		"weights":  map[string]any{"primary": 0.9},
		"key":      "missing",
		"fallback": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Float(); !ok || value != 0.5 {
		t.Fatalf("result = %#v, want 0.5", result.Any())
	}
}

func TestExtensionSignatureDrivesInference(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	err := registry.Register(machine.FunctionSpec{
		Name:   "risk.approved_v1",
		Params: []machine.Type{machine.StringType, machine.IntType},
		Result: machine.BoolType,
		// An extension sees values the way a host does: through the public
		// accessors, never the private fields.
		Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) {
			country, _ := args[0].String()
			amount, _ := args[1].Int()
			return machine.Bool(country == "US" && amount > 100), nil
		},
		Doc: machine.Doc{Label: "风险通过", Description: "演示扩展函数", Category: "风控"},
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := CompileExpr(`risk.approved_v1(country,amount)`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := artifact.Args()[0].Type(); !got.Equal(machine.StringType) {
		t.Fatalf("country type = %s, want string", got)
	}
	if got := artifact.Args()[1].Type(); !got.Equal(machine.IntType) {
		t.Fatalf("amount type = %s, want int", got)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(t.Context(), map[string]any{"country": "US", "amount": 200})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Bool(); !ok || !value {
		t.Fatalf("Run(country=US, amount=200) = %#v, want true", result.Any())
	}
}

func registerCollectionTestExtensions(t *testing.T, registry *machine.Registry) {
	t.Helper()
	typeT := machine.TypeVar("T")
	for _, spec := range []machine.FunctionSpec{
		{
			Name: "has", Params: []machine.Type{machine.DictOf(typeT), machine.StringType}, Result: machine.BoolType,
			Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) {
				entries, _ := args[0].Dict()
				key, _ := args[1].String()
				_, ok := entries[key]
				return machine.Bool(ok), nil
			},
		},
		{
			Name: "get", Params: []machine.Type{machine.DictOf(typeT), machine.StringType}, Result: typeT,
			Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) {
				entries, _ := args[0].Dict()
				key, _ := args[1].String()
				value, ok := entries[key]
				if !ok {
					return machine.Value{}, errors.New("missing key")
				}
				return value, nil
			},
		},
		{
			Name: "get", Params: []machine.Type{machine.ArrayOf(typeT), machine.IntType}, Result: typeT,
			Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) {
				items, _ := args[0].Array()
				index, _ := args[1].Int()
				if index < 0 || index >= int64(len(items)) {
					return machine.Value{}, errors.New("bad index")
				}
				return items[index], nil
			},
		},
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
}

// Money meeting a plain number the wrong way says where to go instead.
func TestMoneyMistakesAreExplained(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"amount + 5":    "money(n, 币种)",
		"amount / 3":    "allocate",
		"amount * risk": "2.9% 这样的字面量",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			contract := moneyContract(t, "amount: money; risk: float; euros: money")
			_, err := CompileExpr(source, moneyRegistry(t), CompileOptions{Args: contract})
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("CompileExpr(%q) error = %v, want a hint containing %q", source, err, want)
			}
		})
	}
}

// hintContract is where money meets everything a hint knows about.
const hintContract = "amount: money; euros: money; a: money; b: money; u: money; risk: float; k: int; f: bool; fee: ratio"

// hintOf compiles source under hintContract and returns what follows the
// no-overload message's "；", or "" when there is no hint.
func hintOf(t *testing.T, source string) string {
	t.Helper()
	_, err := CompileExpr(source, moneyRegistry(t), CompileOptions{Args: moneyContract(t, hintContract)})
	if err == nil {
		t.Fatalf("CompileExpr(%q) compiled, want a type error", source)
	}
	_, hint, _ := strings.Cut(err.Error(), "；")
	return hint
}

// Every way money meets a plain number or other money wrongly has its hint.
func TestMoneyHintsCoverEveryBranch(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"amount * euros": "两笔金额不能相乘",
		"1 - fee":        "100% - fee",
		"fee * 2":        "100% - fee",
		"k * fee":        "n * fee 先把 n 用在金额上",
		"fee / k":        "100% - fee",
		"risk + fee":     "100% - fee",
		"fee < 1":        "100% - fee",
		"risk * amount":  "2.9% 这样的字面量",
		"amount + risk":  "2.9% 这样的字面量",
		"amount < risk":  "2.9% 这样的字面量",
		"risk / u":       "2.9% 这样的字面量",
		"amount / k":     "allocate(m, n)",
		"u / 3":          "allocate(m, n)",
		"5 + amount":     "money(n, currency(同币种金额))",
		"amount - 5":     "money(n, 币种)",
		"5 - amount":     "money(n, 币种)",
		"u + k":          "money(n, 币种)",
		// Two arguments, one money and one float, whatever the function.
		"fallback(amount, risk)": "2.9% 这样的字面量",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if got := hintOf(t, source); !strings.Contains(got, want) {
				t.Fatalf("CompileExpr(%q) hint = %q, want one containing %q", source, got, want)
			}
		})
	}
}

// Where no hint would be true, none is given.
func TestMoneyHintsStaySilentElsewhere(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"3 / amount", "minor(k)", `"x" + amount`, "if(f, amount, risk)", "fallback(amount, 5)",
		"if(f, {a: 1}, {b: 2})", "if(f, {fee: amount}, {cost: euros})", "{fee: amount} + {fee: amount}", "{fee: amount} + 1",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if got := hintOf(t, source); got != "" {
				t.Fatalf("CompileExpr(%q) hint = %q, want none", source, got)
			}
		})
	}
}

// The container hints still speak for containers of money.
func TestContainerHintsHoldForMoney(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"[amount] + [amount]":           "concat(a, b)",
		`{"x": amount} + {"y": amount}`: "merge(a, b)",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if got := hintOf(t, source); !strings.Contains(got, want) {
				t.Fatalf("CompileExpr(%q) hint = %q, want one containing %q", source, got, want)
			}
		})
	}
}

// The README promises money × money a pointer too; it has none.
func TestMoneyTimesMoneyIsExplained(t *testing.T) {
	t.Parallel()
	got := hintOf(t, "amount * amount")
	if got == "" {
		t.Fatal("amount * amount has no hint")
	}
}
