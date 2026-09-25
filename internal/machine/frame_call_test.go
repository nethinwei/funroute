package machine_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

// The request's budget reaches the extension; a function's own Timeout caps a
// single call; both surface as ErrDeadline, distinct from ErrExtension. The
// clock is synctest's, so the timeouts cost no real time.
func TestDeadlinesReachExtensionsAndAreTyped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		registry := slowRegistry(t)
		// The function's Timeout cuts a slow call short even with no request deadline.
		if err := runOneFloat(t, t.Context(), registry, `model.slow_v1(x)`); !errors.Is(err, machine.ErrDeadline) {
			t.Fatalf("timeout error = %v, want ErrDeadline", err)
		}
		// Without a Timeout the request's deadline does it.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)
		defer cancel()
		if err := runOneFloat(t, ctx, registry, `model.patient_v1(x)`); !errors.Is(err, machine.ErrDeadline) {
			t.Fatalf("request deadline error = %v, want ErrDeadline", err)
		}
		// An already-expired budget stops the program at the next call, before
		// the engine is asked anything.
		expired, cancelExpired := context.WithCancel(t.Context())
		cancelExpired()
		if err := runOneFloat(t, expired, registry, `model.patient_v1(x)`); !errors.Is(err, machine.ErrDeadline) {
			t.Fatalf("expired error = %v, want ErrDeadline", err)
		}
	})
}

// slowRegistry has one model that answers after 50ms unless its context ends
// first, registered twice: slow_v1 with a 5ms Timeout, patient_v1 without.
func slowRegistry(t *testing.T) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	slow := func(ctx context.Context, x float64) (float64, error) {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(50 * time.Millisecond):
			return x, nil
		}
	}
	if err := registry.Register(machine.FunctionSpec{
		Name: "model.slow_v1",
		Doc:  machine.Doc{Timeout: 5 * time.Millisecond},
		Go:   slow,
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(machine.FunctionSpec{Name: "model.patient_v1", Go: slow}); err != nil {
		t.Fatal(err)
	}
	return registry
}

// runOneFloat compiles source over one float argument x and runs it with x = 1
// under ctx, returning the run's error.
func runOneFloat(t *testing.T, ctx context.Context, registry *machine.Registry, source string) error {
	t.Helper()
	artifact, err := compile.CompileExpr(source, registry, compile.CompileOptions{Args: []compile.ArgSpec{{Name: "x", Type: machine.FloatType}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.RunValues(ctx, []machine.Value{machine.Float(1)})
	return err
}

// An extension's own failure is another kind, not mistaken for a deadline.
func TestExtensionErrorsAreTyped(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	failing := func(x float64) (float64, error) { return 0, errors.New("boom") }
	if err := registry.Register(machine.FunctionSpec{Name: "model.failing_v1", Go: failing}); err != nil {
		t.Fatal(err)
	}
	artifact, err := compile.CompileExpr(`model.failing_v1(x)`, registry, compile.CompileOptions{Args: []compile.ArgSpec{{Name: "x", Type: machine.FloatType}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	args := []machine.Value{machine.Float(1)}
	_, err = runtime.RunValues(t.Context(), args)
	if !errors.Is(err, machine.ErrExtension) || errors.Is(err, machine.ErrDeadline) {
		t.Fatalf("extension error = %v, want ErrExtension and not ErrDeadline", err)
	}
}

// A Detached function that ignores its context is abandoned at the deadline
// instead of holding the request. On synctest's clock the deadline is exact:
// the request returns when the 5ms Timeout has passed, and not later.
func TestDetachedCallsStopWaitingAtTheDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const timeout = 5 * time.Millisecond
		registry := machine.CoreRegistry()
		released := make(chan struct{})
		stubborn := func(x float64) (float64, error) {
			<-released
			return x, nil
		}
		if err := registry.Register(machine.FunctionSpec{
			Name: "engine.stubborn_v1",
			Doc:  machine.Doc{Timeout: timeout, Detached: true},
			Go:   stubborn,
		}); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		err := runOneFloat(t, t.Context(), registry, `engine.stubborn_v1(x)`)
		elapsed := time.Since(started)
		close(released)
		if !errors.Is(err, machine.ErrDeadline) || elapsed != timeout {
			t.Fatalf("detached error = %v after %s, want ErrDeadline after %s", err, elapsed, timeout)
		}
	})
}

func TestDetachedPanicsAreContainedAndTyped(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := registry.Register(machine.FunctionSpec{
		Name: "engine.panic_v1",
		Doc:  machine.Doc{Detached: true},
		Go: func(float64) (float64, error) {
			panic("detached exploded")
		},
	}); err != nil {
		t.Fatal(err)
	}
	artifact, err := compile.CompileExpr(`engine.panic_v1(x)`, registry, compile.CompileOptions{Args: []compile.ArgSpec{{Name: "x", Type: machine.FloatType}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.RunValues(t.Context(), []machine.Value{machine.Float(1)})
	if !errors.Is(err, machine.ErrExtension) || !strings.Contains(err.Error(), "detached exploded") {
		t.Fatalf("detached panic = %v, want ErrExtension naming the panic", err)
	}
}

func TestPanickingExtensionIsContained(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := registry.Register(machine.FunctionSpec{
		Name: "boom_v1", Params: []machine.Type{machine.IntType}, Result: machine.IntType,
		Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) { panic("extension exploded") },
		Doc:  machine.Doc{Label: "炸弹", Description: "总是 panic 的扩展", Category: "测试"},
	}); err != nil {
		t.Fatal(err)
	}
	artifact, err := compile.CompileExpr(`boom_v1(n)`, registry, compile.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Run(t.Context(), map[string]any{"n": 1})
	if !errors.Is(err, machine.ErrExtension) || !strings.Contains(err.Error(), "extension panicked") {
		t.Fatalf("panic was not contained: %v, want ErrExtension saying extension panicked", err)
	}
	// The runtime stays usable afterwards.
	if _, err := runtime.Run(t.Context(), map[string]any{"n": 2}); err == nil ||
		!strings.Contains(err.Error(), "extension panicked") {
		t.Fatalf("second run: %v, want extension panicked again", err)
	}
}

func TestPrefetchedCallStillHonorsRequestCancellation(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := registry.Register(machine.FunctionSpec{
		Name:    "model.score_v1",
		Go:      func(x float64) (float64, error) { return x, nil },
		GoBatch: func(xs []float64) ([]float64, error) { return xs, nil },
	}); err != nil {
		t.Fatal(err)
	}
	artifact, err := compile.CompileExpr(`model.score_v1(x)`, registry, compile.CompileOptions{Args: []compile.ArgSpec{{Name: "x", Type: machine.FloatType}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	sites := machine.PrefetchSites(artifact)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = machine.RunPrefetched(ctx, runtime, []machine.Value{machine.Float(1)},
		map[int]machine.Prefetched{sites[0].PC: {Value: machine.Float(1)}})
	if !errors.Is(err, machine.ErrDeadline) {
		t.Fatalf("canceled prefetched call = %v, want ErrDeadline", err)
	}
}

// lyingRegistry is the money registry with host functions whose results
// hold pounds, a currency the registry does not declare: an amount, one in
// an array, one in a record — and one that echoes its argument honestly.
func lyingRegistry(t *testing.T) *machine.Registry {
	t.Helper()
	registry := moneyRegistry(t)
	pounds := machine.MoneyValue(500, "GBP")
	list, err := machine.Array(machine.MoneyType, []machine.Value{pounds})
	if err != nil {
		t.Fatal(err)
	}
	record, err := machine.Record(machine.RecordOf(machine.FieldOf("fee", machine.MoneyType)), []machine.Value{pounds})
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []machine.FunctionSpec{
		{Name: "lies.same_v1", Params: []machine.Type{machine.MoneyType}, Result: machine.MoneyType, Eval: constantEval(pounds)},
		{Name: "lies.echo_v1", Params: []machine.Type{machine.MoneyType}, Result: machine.MoneyType, Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) { return args[0], nil }},
		{Name: "lies.list_v1", Params: []machine.Type{machine.IntType}, Result: machine.ArrayOf(machine.MoneyType), Eval: constantEval(list)},
		{Name: "lies.record_v1", Params: []machine.Type{machine.IntType}, Result: machine.RecordOf(machine.FieldOf("fee", machine.MoneyType)), Eval: constantEval(record)},
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	return registry
}

func constantEval(value machine.Value) machine.EvalFunc {
	return func(context.Context, []machine.Value) (machine.Value, error) { return value, nil }
}

// A host function's result may hold only currencies the registry declares,
// inside containers and records too: pounds are an ErrCurrency, the rule's
// error, which fallback leaves.
func TestAHostResultIsHeldToItsCurrenciesAllTheWayIn(t *testing.T) {
	t.Parallel()
	registry := lyingRegistry(t)
	for _, test := range []struct{ source, contract, result string }{
		{"lies.same_v1(a)", "a: money", "money"},
		{"lies.list_v1(n)", "n: int", "array<money>"},
		{"lies.record_v1(n)", "n: int", "record{fee: money}"},
		{"lies.record_v1(n).fee", "n: int", ""},
		{"fallback(lies.same_v1(a), a)", "a: money", "money"},
	} {
		t.Run(test.source+" as "+test.result, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileMoney(t, registry, test.source, test.contract, test.result)
			if err != nil {
				t.Fatalf("CompileExpr(%q) error = %v", test.source, err)
			}
			runtime, err := machine.Instantiate(artifact, registry)
			if err != nil {
				t.Fatal(err)
			}
			args := map[string]any{"n": 1}
			if strings.HasPrefix(test.contract, "a:") {
				args = map[string]any{"a": "USD 1.00"}
			}
			got, err := runtime.Run(t.Context(), args)
			if !errors.Is(err, machine.ErrCurrency) {
				t.Fatalf("%s = %v, %v, want ErrCurrency", test.source, got, err)
			}
		})
	}
}

// A host function returning what the signature says passes the same check.
func TestAHostResultInItsCurrencyPasses(t *testing.T) {
	t.Parallel()
	registry := lyingRegistry(t)
	artifact, err := compileMoney(t, registry, "lies.echo_v1(a)", "a: money", "money")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	got, err := runtime.Run(t.Context(), map[string]any{"a": "EUR 1.00"})
	if money, ok := got.Money(); err != nil || !ok || money.Currency() != "EUR" || money.Minor() != 100 {
		t.Fatalf("lies.echo_v1(EUR 1.00) = %v, %v, want EUR 1.00", got, err)
	}
}

// classedFailureRegistry has host functions that fail with a class of their own: an
// exchange rate they could not find, a currency mismatch.
func classedFailureRegistry(t *testing.T) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	for name, class := range map[string]error{"h.nofxrate_v1": machine.ErrNoFxRate, "h.currency_v1": machine.ErrCurrency} {
		err := registry.Register(machine.FunctionSpec{
			Name: name,
			Go:   func(float64) (float64, error) { return 0, fmt.Errorf("quote: %w", class) },
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return registry
}

// A host function's error keeps the class it has: a missing exchange rate is
// ErrNoFxRate, which fallback takes as data not yet at hand; a currency
// mismatch is ErrCurrency, which fallback leaves. Neither turns into an
// ErrExtension that would hide which it was.
func TestAHostFunctionsErrorKeepsItsClass(t *testing.T) {
	t.Parallel()
	registry := classedFailureRegistry(t)
	if err := runOneFloat(t, t.Context(), registry, "h.nofxrate_v1(x)"); !errors.Is(err, machine.ErrNoFxRate) {
		t.Fatalf("h.nofxrate_v1(x) error = %v, want ErrNoFxRate", err)
	}
	if err := runOneFloat(t, t.Context(), registry, "fallback(h.nofxrate_v1(x), 2.0)"); err != nil {
		t.Fatalf("fallback over ErrNoFxRate = %v, want the fallback", err)
	}
	for _, source := range []string{"h.currency_v1(x)", "fallback(h.currency_v1(x), 2.0)"} {
		if err := runOneFloat(t, t.Context(), registry, source); !errors.Is(err, machine.ErrCurrency) || errors.Is(err, machine.ErrExtension) {
			t.Fatalf("%s error = %v, want ErrCurrency and not ErrExtension", source, err)
		}
	}
}

// A currency mismatch a host reports after its budget ran out is still a
// currency mismatch: the deadline explains a failure without a class of its
// own, not one with.
func TestAClassifiedErrorOutlivesTheDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		registry := machine.CoreRegistry()
		late := func(ctx context.Context, x float64) (float64, error) {
			<-ctx.Done()
			return 0, fmt.Errorf("late: %w", machine.ErrCurrency)
		}
		if err := registry.Register(machine.FunctionSpec{
			Name: "h.late_v1",
			Doc:  machine.Doc{Timeout: 5 * time.Millisecond},
			Go:   late,
		}); err != nil {
			t.Fatal(err)
		}
		if err := runOneFloat(t, t.Context(), registry, "h.late_v1(x)"); !errors.Is(err, machine.ErrCurrency) || errors.Is(err, machine.ErrDeadline) {
			t.Fatalf("h.late_v1(x) error = %v, want ErrCurrency and not ErrDeadline", err)
		}
	})
}

// Whatever class a host function's error already has, it keeps, and it is not
// made an extension failure besides.
func TestAHostErrorKeepsAnyClassItHas(t *testing.T) {
	t.Parallel()
	for _, class := range []error{machine.ErrCompile, machine.ErrContract, machine.ErrDeadline, machine.ErrArithmetic} {
		t.Run(class.Error(), func(t *testing.T) {
			t.Parallel()
			registry := machine.CoreRegistry()
			if err := registry.Register(machine.FunctionSpec{
				Name: "h.classed_v1",
				Go:   func(int64) (int64, error) { return 0, fmt.Errorf("upstream: %w", class) },
			}); err != nil {
				t.Fatal(err)
			}
			artifact, err := compile.CompileExpr(`h.classed_v1(1)`, registry, compile.CompileOptions{})
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := machine.Instantiate(artifact, registry)
			if err != nil {
				t.Fatal(err)
			}
			_, err = runtime.Run(t.Context(), nil)
			if !errors.Is(err, class) || errors.Is(err, machine.ErrExtension) {
				t.Fatalf("a host error wrapping %v = %v, want %v and not ErrExtension", class, err, class)
			}
		})
	}
}
