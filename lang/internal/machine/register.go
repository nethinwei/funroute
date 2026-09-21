package machine

import "fmt"

// Doc is the presentation metadata of a registered function. Every field has a
// usable default, so a host fills in only what it cares about.
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
}

// Fn1, Fn2 and Fn3 register a host function by its Go signature: the parameter
// and result types are derived from the type arguments, and the value
// conversions and their error handling are generated. Compare with writing a
// FunctionSpec by hand — the Go compiler now checks that the implementation
// matches the declared ABI.
//
//	lang.Fn2(registry, "risk.score_v1", lang.Doc{Label: "风险评分", Cost: 25},
//	    func(country string, amount int64) (float64, error) { … })
//
// Supported Go types are bool, int64, float64, string, their slices, [][]float64
// and map[string]T for those scalars. Anything else needs a FunctionSpec.
func Fn1[A, R any](registry *Registry, name string, doc Doc, fn func(A) (R, error)) error {
	params, result, err := signature1[A, R]()
	if err != nil {
		return fmt.Errorf("function %s: %w", name, err)
	}
	return registerFn(registry, name, doc, params, result, func(args []Value) (Value, error) {
		first, err := FromValue[A](args[0])
		if err != nil {
			return Value{}, err
		}
		return resultValue(fn(first))
	})
}

func Fn2[A, B, R any](registry *Registry, name string, doc Doc, fn func(A, B) (R, error)) error {
	params, result, err := signature2[A, B, R]()
	if err != nil {
		return fmt.Errorf("function %s: %w", name, err)
	}
	return registerFn(registry, name, doc, params, result, func(args []Value) (Value, error) {
		first, err := FromValue[A](args[0])
		if err != nil {
			return Value{}, err
		}
		second, err := FromValue[B](args[1])
		if err != nil {
			return Value{}, err
		}
		return resultValue(fn(first, second))
	})
}

func Fn3[A, B, C, R any](registry *Registry, name string, doc Doc, fn func(A, B, C) (R, error)) error {
	params, result, err := signature3[A, B, C, R]()
	if err != nil {
		return fmt.Errorf("function %s: %w", name, err)
	}
	return registerFn(registry, name, doc, params, result, func(args []Value) (Value, error) {
		first, err := FromValue[A](args[0])
		if err != nil {
			return Value{}, err
		}
		second, err := FromValue[B](args[1])
		if err != nil {
			return Value{}, err
		}
		third, err := FromValue[C](args[2])
		if err != nil {
			return Value{}, err
		}
		return resultValue(fn(first, second, third))
	})
}

func signature1[A, R any]() ([]Type, Type, error) {
	first, err := goType[A]()
	if err != nil {
		return nil, Type{}, err
	}
	result, err := goType[R]()
	return []Type{first}, result, err
}

func signature2[A, B, R any]() ([]Type, Type, error) {
	first, err := goType[A]()
	if err != nil {
		return nil, Type{}, err
	}
	rest, result, err := signature1[B, R]()
	return append([]Type{first}, rest...), result, err
}

func signature3[A, B, C, R any]() ([]Type, Type, error) {
	first, err := goType[A]()
	if err != nil {
		return nil, Type{}, err
	}
	rest, result, err := signature2[B, C, R]()
	return append([]Type{first}, rest...), result, err
}

func resultValue[R any](result R, err error) (Value, error) {
	if err != nil {
		return Value{}, err
	}
	return ToValue(result)
}

// registerFn fills in the display defaults and registers the spec.
func registerFn(registry *Registry, name string, doc Doc, params []Type, result Type, eval EvalFunc) error {
	labels := make([]ParameterDisplay, len(params))
	for i := range params {
		label := fmt.Sprintf("参数 %d", i+1)
		if i < len(doc.Params) && doc.Params[i] != "" {
			label = doc.Params[i]
		}
		labels[i] = ParameterDisplay{Name: label, Label: label, Description: params[i].String()}
	}
	return registry.Register(FunctionSpec{
		Name: name, Params: params, Result: result, Cost: doc.Cost, Eval: eval,
		Display: FunctionDisplay{
			Label:       orDefault(doc.Label, name),
			Description: orDefault(doc.Description, "宿主注册的扩展函数 "+name),
			Category:    orDefault(doc.Category, "扩展"),
			Color:       orDefault(doc.Color, "#475569"),
			Icon:        orDefault(doc.Icon, "ƒ"),
			Keywords:    doc.Keywords,
			Parameters:  labels,
			Result:      ResultDisplay{Label: orDefault(doc.Result, "结果"), Description: result.String()},
			Examples:    doc.Examples,
			Order:       doc.Order,
			Hidden:      doc.Hidden,
		},
	})
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
