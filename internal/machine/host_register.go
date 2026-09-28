package machine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Doc is what only a host can say about a function: what to call it in this
// console's language, what it means, what it costs. Everything a machine can
// work out is worked out — the signature comes from the Go types, the category
// defaults to the name's namespace, the order is the name's, and colour and
// icon are the console's decision. Hand-written metadata is metadata that can
// be wrong, so there is as little of it here as possible.
type Doc struct {
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Category    string   `json:"category"`
	Params      []string `json:"params,omitempty"` // parameter labels, in order
	Result      string   `json:"result,omitempty"` // result label
	// Examples show the function at work, one call each with the value it
	// gives. They are closed expressions — literals and the kernel, rates
	// through using where a conversion needs them — so anyone can run one
	// as written; the official functions' examples are run by a test, and
	// between them they choose every overload of their name.
	Examples []Example `json:"examples,omitempty"`
	// Constexpr says the compiler may call this function while folding, the
	// way C++ marks a function usable in a constant expression. The kernel's
	// functions all are. A host function is not unless it says so: folding
	// would otherwise reach an inference engine, a clock or a remote service
	// while a rule is compiled, and a rule that compiles differently at 3am is
	// worse than one that computes a little more at run time.
	Constexpr bool `json:"-"`
	// BoundedArgs requires every argument of a call to have a size the inputs
	// already bound: a literal, the length of a container, or those combined
	// by arithmetic. It is for a function whose result size follows from its
	// arguments — range is the one — because an arbitrary run-time scalar
	// would let one argument stand for an arbitrarily long array and break the
	// polynomial bound in docs/termination.md. Requiring a *constant* would be
	// stronger than that bound needs: range(len(fees)) is as safe as range(3).
	BoundedArgs bool `json:"-"`
	// Timeout and Detached are operational, so neither reaches a front end.
	// Timeout caps one call of this function: the deadline it receives is the
	// earlier of the request's and now+Timeout. Zero means the request's alone.
	Timeout time.Duration `json:"-"`
	// Detached runs the call on its own goroutine and stops waiting at the
	// deadline, for a binding that cannot honour a context. The abandoned call
	// keeps running until it returns on its own.
	Detached bool `json:"-"`
}

// Example is one use of a function: a program's text and the JSON of the
// value it gives, as Registry.EncodeJSON writes it.
type Example struct {
	Source string `json:"source"`
	Result string `json:"result"`
}

// reflectSpec fills Params, Result and Eval — and EvalBatch — from Go and
// GoBatch, the Go functions a spec may name in their place. Go is any func
// whose parameters are Go types the boundary knows — bool, the int and float
// kinds, string, slices and string-keyed maps of those at any depth, and types
// the registry has DefineHandle'd — optionally preceded by a context.Context,
// and whose results are R, or (R, error), for such an R. The signature and the
// conversions are derived by reflection once, here; a call of a common
// signature then calls Go as itself (host_direct.go), and any other goes
// through reflect.Call, which costs a few hundred nanoseconds. GoBatch takes every
// request's arguments as slices — one per parameter of Go, in order, after the
// optional context — and answers one result per request, the way an inference
// engine is called: a Batch uses it, a plain Run uses Go.
func (r *Registry) reflectSpec(spec *FunctionSpec) error {
	if spec.Go == nil {
		if spec.GoBatch != nil {
			return fmt.Errorf("function %s: GoBatch needs Go", spec.Name)
		}
		return nil
	}
	if spec.Params != nil || spec.Result.kind != InvalidKind || spec.Eval != nil || spec.EvalBatch != nil {
		return fmt.Errorf("function %s: Go takes the place of Params, Result, Eval and EvalBatch", spec.Name)
	}
	single, err := reflectSignature(r, spec.Go)
	if err != nil {
		return fmt.Errorf("function %s: %w", spec.Name, err)
	}
	spec.Params, spec.Result, spec.Eval = single.params, single.result, single.call
	if direct, ok := directEval(spec.Go); ok {
		spec.Eval, spec.madeResult = direct, true
	}
	// A Go function called as itself is called straight from the registers
	// when it is of a common shape; a pure one — a kernel function is pure
	// without saying so (IsConstexpr) — without a look at the deadline.
	if spec.Doc.Timeout == 0 && !spec.Doc.Detached && spec.GoBatch == nil {
		spec.banked = pureOf(spec.Go)
		if spec.Doc.Constexpr || spec.builtin {
			spec.pure = spec.banked
		}
	}
	if spec.GoBatch == nil {
		return nil
	}
	batched, err := reflectBatch(spec.GoBatch, single)
	if err != nil {
		return fmt.Errorf("function %s batch: %w", spec.Name, err)
	}
	spec.EvalBatch = batched.callBatch
	if direct, ok := directBatch(spec.GoBatch); ok {
		spec.EvalBatch = direct
	}
	return nil
}

// reflected is a Go function with its FunRoute signature and the converters
// for each argument and the result, all resolved at registration.
type reflected struct {
	fn      reflect.Value
	ctx     bool // whether the first Go parameter is a context.Context
	fails   bool // whether the Go function returns an error after its result
	params  []Type
	result  Type
	into    []func(Value) (reflect.Value, error)
	outOf   func(reflect.Value) (Value, error)
	goTypes []reflect.Type
}

var (
	contextType = reflect.TypeFor[context.Context]()
	errorType   = reflect.TypeFor[error]()
)

func reflectSignature(registry *Registry, fn any) (*reflected, error) {
	value := reflect.ValueOf(fn)
	typ := value.Type()
	if typ.Kind() != reflect.Func {
		return nil, fmt.Errorf("want a func, got %T", fn)
	}
	fails, ok := resultShape(typ)
	if !ok {
		return nil, errors.New("must return a result, or (result, error)")
	}
	out := &reflected{fn: value, fails: fails}
	var start int
	out.ctx, start = leadingContext(typ)
	for i := start; i < typ.NumIn(); i++ {
		param, err := reflectType(registry, typ.In(i))
		if err != nil {
			return nil, fmt.Errorf("parameter %d: %w", i-start+1, err)
		}
		out.params = append(out.params, param)
		out.goTypes = append(out.goTypes, typ.In(i))
		out.into = append(out.into, converterInto(registry, typ.In(i)))
	}
	result, err := reflectType(registry, typ.Out(0))
	if err != nil {
		return nil, fmt.Errorf("result: %w", err)
	}
	out.result = result
	out.outOf = converterOutOf(registry, result)
	return out, nil
}

// reflectBatch checks that batch is fn with every parameter and the result
// turned into a slice, and shares fn's converters element-wise. It needs no
// registry: those converters were resolved against it already, with fn.
func reflectBatch(batch any, single *reflected) (*reflected, error) {
	value := reflect.ValueOf(batch)
	typ := value.Type()
	fails, ok := resultShape(typ)
	if !ok {
		return nil, errors.New("must be a func returning results, or (results, error)")
	}
	out := &reflected{fn: value, fails: fails, params: single.params, result: single.result, into: single.into, outOf: single.outOf, goTypes: single.goTypes}
	var start int
	out.ctx, start = leadingContext(typ)
	if typ.NumIn()-start != len(single.goTypes) {
		return nil, fmt.Errorf("takes %d parameters, the single form takes %d", typ.NumIn()-start, len(single.goTypes))
	}
	for i, want := range single.goTypes {
		if got := typ.In(start + i); got.Kind() != reflect.Slice || got.Elem() != want {
			return nil, fmt.Errorf("parameter %d must be []%s, got %s", i+1, want, got)
		}
	}
	if got := typ.Out(0); got.Kind() != reflect.Slice || got.Elem() != single.fn.Type().Out(0) {
		return nil, fmt.Errorf("result must be []%s, got %s", single.fn.Type().Out(0), got)
	}
	return out, nil
}

// leadingContext reports whether a Go function's first parameter is a
// context.Context, and where the parameters the language passes start.
func leadingContext(typ reflect.Type) (bool, int) {
	if typ.NumIn() > 0 && typ.In(0) == contextType {
		return true, 1
	}
	return false, 0
}

// resultShape reads a Go function's results: R alone, or R and an error.
func resultShape(typ reflect.Type) (fails, ok bool) {
	switch {
	case typ.Kind() != reflect.Func:
		return false, false
	case typ.NumOut() == 1 && typ.Out(0) != errorType:
		return false, true
	case typ.NumOut() == 2 && typ.Out(0) != errorType && typ.Out(1) == errorType:
		return true, true
	}
	return false, false
}

// call is the EvalFunc: convert, reflect.Call, convert back.
func (r *reflected) call(ctx context.Context, args []Value) (Value, error) {
	in := make([]reflect.Value, 0, len(args)+1)
	if r.ctx {
		in = append(in, reflect.ValueOf(ctx))
	}
	for i, arg := range args {
		converted, err := r.into[i](arg)
		if err != nil {
			return Value{}, fmt.Errorf("argument %d: %w", i+1, err)
		}
		in = append(in, converted)
	}
	results := r.fn.Call(in)
	if err := r.failure(results); err != nil {
		return Value{}, err
	}
	return r.outOf(results[0])
}

// failure is the error a call returned, if the function returns one.
func (r *reflected) failure(results []reflect.Value) error {
	if !r.fails {
		return nil
	}
	err, _ := reflect.TypeAssert[error](results[1])
	return err
}

// callBatch is the BatchEvalFunc: one slice per parameter, one call.
func (r *reflected) callBatch(ctx context.Context, calls [][]Value) ([]Value, error) {
	in := make([]reflect.Value, 0, len(r.goTypes)+1)
	if r.ctx {
		in = append(in, reflect.ValueOf(ctx))
	}
	for p, goType := range r.goTypes {
		column := reflect.MakeSlice(reflect.SliceOf(goType), len(calls), len(calls))
		for i, call := range calls {
			converted, err := r.into[p](call[p])
			if err != nil {
				return nil, fmt.Errorf("request %d argument %d: %w", i, p+1, err)
			}
			column.Index(i).Set(converted)
		}
		in = append(in, column)
	}
	results := r.fn.Call(in)
	if err := r.failure(results); err != nil {
		return nil, err
	}
	list := results[0]
	out := make([]Value, list.Len())
	for i := range out {
		value, err := r.outOf(list.Index(i))
		if err != nil {
			return nil, fmt.Errorf("result %d: %w", i, err)
		}
		out[i] = value
	}
	return out, nil
}

// namespaceOf reads the category off a versioned name: route.score_v1 belongs
// with route. A name without a namespace has none to read.
func namespaceOf(name string) string {
	namespace, _, ok := strings.Cut(name, ".")
	if !ok {
		return "扩展"
	}
	return namespace
}
