package machine

import (
	"context"
	"fmt"
	"reflect"
	"time"
)

// Doc is the presentation metadata of a registered function, plus the two
// operational settings a host sets per function. Every field has a usable
// default, so a host fills in only what it cares about.
type Doc struct {
	Label       string
	Description string
	Category    string
	Color       string
	Icon        string
	Cost        uint64
	Params      []string // parameter labels, in order
	Result      string   // result label
	Keywords    []string
	Examples    []FunctionExample
	Order       int
	Hidden      bool
	// Timeout caps one call of this function: the deadline it receives is the
	// earlier of the request's and now+Timeout. Zero means the request's alone.
	Timeout time.Duration
	// Detached runs the call on its own goroutine and stops waiting at the
	// deadline, for a binding that cannot honour a context. The abandoned call
	// keeps running until it returns on its own.
	Detached bool
}

// Logic registers a host function by its Go signature. fn is any func whose
// parameters are Go types the boundary knows — bool, the int and float kinds,
// string, slices and string-keyed maps of those at any depth, and types the
// registry has DefineHandle'd — optionally preceded by a context.Context, and
// whose results are (R, error) for such an R. The signature, the argument
// conversions and the result conversion are derived by reflection once, at
// registration; a call then goes through reflect.Call, which costs a few
// hundred nanoseconds. The kernel's own functions are FunctionSpecs and do not
// pay that; a host function that must not either can be one too.
//
//	lang.Logic(registry, "risk.score_v1", lang.Doc{Label: "风险评分", Cost: 25},
//	    func(country string, amount int64) (float64, error) { … })
func Logic(registry *Registry, name string, doc Doc, fn any) error {
	return Model(registry, name, doc, fn, nil)
}

// Model registers a function with a batch implementation as well. batch takes
// every request's arguments as slices — one per parameter of fn, in order,
// after the optional context — and answers one result per request, the way
// an inference engine is called. A Batch uses it; a plain Run uses fn.
//
//	lang.Model(registry, "model.fraud_v3", doc,
//	    func(ctx context.Context, emb *ort.Tensor) (float64, error) { … },
//	    func(ctx context.Context, embs []*ort.Tensor) ([]float64, error) { … })
func Model(registry *Registry, name string, doc Doc, fn, batch any) error {
	single, err := reflectSignature(registry, fn)
	if err != nil {
		return fmt.Errorf("function %s: %w", name, err)
	}
	spec := FunctionSpec{
		Name: name, Params: single.params, Result: single.result,
		Cost: doc.Cost, Timeout: doc.Timeout, Detached: doc.Detached,
		Eval:    single.call,
		Display: displayFor(name, doc, single),
	}
	if batch != nil {
		batched, err := reflectBatch(registry, batch, single)
		if err != nil {
			return fmt.Errorf("function %s batch: %w", name, err)
		}
		spec.EvalBatch = batched.callBatch
	}
	return registry.Register(spec)
}

// reflected is a Go function with its FunRoute signature and the converters
// for each argument and the result, all resolved at registration.
type reflected struct {
	fn      reflect.Value
	ctx     bool // whether the first Go parameter is a context.Context
	params  []Type
	result  Type
	into    []func(Value) (reflect.Value, error)
	outOf   func(reflect.Value) (Value, error)
	goTypes []reflect.Type
}

var (
	contextType = reflect.TypeOf((*context.Context)(nil)).Elem()
	errorType   = reflect.TypeOf((*error)(nil)).Elem()
)

func reflectSignature(registry *Registry, fn any) (*reflected, error) {
	value := reflect.ValueOf(fn)
	typ := value.Type()
	if typ.Kind() != reflect.Func {
		return nil, fmt.Errorf("want a func, got %T", fn)
	}
	if typ.NumOut() != 2 || typ.Out(1) != errorType {
		return nil, fmt.Errorf("must return (result, error)")
	}
	out := &reflected{fn: value}
	start := 0
	if typ.NumIn() > 0 && typ.In(0) == contextType {
		out.ctx = true
		start = 1
	}
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
// turned into a slice, and shares fn's converters element-wise.
func reflectBatch(registry *Registry, batch any, single *reflected) (*reflected, error) {
	value := reflect.ValueOf(batch)
	typ := value.Type()
	if typ.Kind() != reflect.Func || typ.NumOut() != 2 || typ.Out(1) != errorType {
		return nil, fmt.Errorf("must be a func returning (results, error)")
	}
	out := &reflected{fn: value, params: single.params, result: single.result, into: single.into, outOf: single.outOf, goTypes: single.goTypes}
	start := 0
	if typ.NumIn() > 0 && typ.In(0) == contextType {
		out.ctx = true
		start = 1
	}
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
	if err, _ := results[1].Interface().(error); err != nil {
		return Value{}, err
	}
	return r.outOf(results[0])
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
	if err, _ := results[1].Interface().(error); err != nil {
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

// displayFor fills in the presentation defaults.
func displayFor(name string, doc Doc, sig *reflected) FunctionDisplay {
	labels := make([]ParameterDisplay, len(sig.params))
	for i := range sig.params {
		label := fmt.Sprintf("参数 %d", i+1)
		if i < len(doc.Params) && doc.Params[i] != "" {
			label = doc.Params[i]
		}
		labels[i] = ParameterDisplay{Name: label, Label: label, Description: sig.params[i].String()}
	}
	return FunctionDisplay{
		Label:       orDefault(doc.Label, name),
		Description: orDefault(doc.Description, "宿主注册的扩展函数 "+name),
		Category:    orDefault(doc.Category, "扩展"),
		Color:       orDefault(doc.Color, "#475569"),
		Icon:        orDefault(doc.Icon, "ƒ"),
		Keywords:    doc.Keywords,
		Parameters:  labels,
		Result:      ResultDisplay{Label: orDefault(doc.Result, "结果"), Description: sig.result.String()},
		Examples:    doc.Examples,
		Order:       doc.Order,
		Hidden:      doc.Hidden,
	}
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
