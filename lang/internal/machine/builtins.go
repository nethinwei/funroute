package machine

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// CoreRegistry is the minimal computational kernel: operator-facing functions
// plus the lazy if and fallback calls. Optional structural forms are turned on
// with EnableForm, list primitives with RegisterArrayPrimitives, and everything
// domain specific is registered by the host.
func CoreRegistry() *Registry {
	r := NewRegistry()
	registerControl(r)
	registerArithmetic(r)
	registerComparisons(r)
	registerConversions(r)
	return r
}

// registerComparisons adds the four ordering predicates. The remaining
// operators are pure sugar over these and if: a != b is if(eq(a,b),false,true),
// a && b is if(a,b,false), a || b is if(a,true,b), !a is if(a,false,true).
func registerComparisons(registry *Registry) {
	for _, comparison := range []struct {
		name, label, icon string
		accept            func(int) bool
	}{
		{"lt", "小于", "<", func(order int) bool { return order < 0 }},
		{"le", "小于等于", "≤", func(order int) bool { return order <= 0 }},
		{"gt", "大于", ">", func(order int) bool { return order > 0 }},
		{"ge", "大于等于", "≥", func(order int) bool { return order >= 0 }},
	} {
		registerComparison(registry, comparison.name, comparison.label, comparison.icon, comparison.accept)
	}
}

func registerComparison(registry *Registry, name, label, icon string, accept func(int) bool) {
	eval := func(_ context.Context, args []Value) (Value, error) {
		order, err := compareValues(args[0], args[1])
		if err != nil {
			return Value{}, err
		}
		return Bool(accept(order)), nil
	}
	for _, params := range [][2]Type{
		{IntType, IntType}, {FloatType, FloatType}, {StringType, StringType},
		{IntType, FloatType}, {FloatType, IntType},
	} {
		mustRegister(registry, FunctionSpec{
			Name: name, Params: []Type{params[0], params[1]}, Result: BoolType, Cost: 2, Eval: eval,
			Display: FunctionDisplay{
				Label:       label,
				Description: "比较两个数值或两个字符串；数值混合时整数会被安全提升。字符串按 UTF-8 字节序比较。",
				Category:    "比较",
				Color:       "#0E7490",
				Icon:        icon,
				Parameters: []ParameterDisplay{
					{Name: "left", Label: "左值", Description: params[0].String()},
					{Name: "right", Label: "右值", Description: params[1].String()},
				},
				Result: ResultDisplay{Label: "比较结果", Description: "bool"},
				Order:  30,
			},
		})
	}
}

// compareValues orders two values of the same domain. Integers are compared as
// integers, so values beyond float64's exact range still order correctly.
func compareValues(left, right Value) (int, error) {
	switch {
	case left.kind == StringKind && right.kind == StringKind:
		return strings.Compare(left.s, right.s), nil
	case left.kind == IntKind && right.kind == IntKind:
		return compareOrdered(left.i, right.i), nil
	}
	first, err := numericFloat(left)
	if err != nil {
		return 0, err
	}
	second, err := numericFloat(right)
	if err != nil {
		return 0, err
	}
	return compareOrdered(first, second), nil
}

func compareOrdered[T int64 | float64](left, right T) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func registerControl(registry *Registry) {
	t := TypeVar("T")
	mustRegister(registry, FunctionSpec{
		Name: "if", Params: []Type{BoolType, t, t}, Result: t,
		Cost: 1, special: specialIf,
		Display: FunctionDisplay{
			Label:       "条件选择",
			Description: "条件为真时只计算真分支，否则只计算假分支。两个分支必须返回同一类型。",
			Category:    "控制",
			Color:       "#7C3AED",
			Icon:        "◇",
			Parameters: []ParameterDisplay{
				{Name: "condition", Label: "条件", Description: "布尔表达式"},
				{Name: "whenTrue", Label: "为真", Description: "条件为真时求值"},
				{Name: "whenFalse", Label: "为假", Description: "条件为假时求值"},
			},
			Result:   ResultDisplay{Label: "所选分支", Description: "类型 T"},
			Examples: []FunctionExample{{Title: "选择结果", Expression: "if(enabled,primary,fallback)"}},
			Order:    10,
		},
	})
	registerFallback(registry, t)
	mustRegister(registry, FunctionSpec{
		Name: "eq", Params: []Type{t, t}, Result: BoolType, Cost: 2,
		Eval: func(_ context.Context, args []Value) (Value, error) { return compareEqual(args[0], args[1]) },
		Display: FunctionDisplay{
			Label:       "相等判断",
			Description: "比较两个同类型值是否完全相等。",
			Category:    "控制",
			Color:       "#7C3AED",
			Icon:        "=",
			Parameters: []ParameterDisplay{
				{Name: "left", Label: "左值"},
				{Name: "right", Label: "右值"},
			},
			Result: ResultDisplay{Label: "是否相等"},
			Order:  20,
		},
	})
}

func registerFallback(registry *Registry, t Type) {
	mustRegister(registry, FunctionSpec{
		Name: "fallback", Params: []Type{t, t}, Result: t,
		Cost: 1, special: specialFallback,
		Display: FunctionDisplay{
			Label:       "失败降级",
			Description: "按顺序尝试候选表达式；遇到扩展失败或超时才继续下一项，不会吞掉 fuel、类型或内核运算错误。",
			Category:    "控制",
			Color:       "#7C3AED",
			Icon:        "↘",
			Parameters: []ParameterDisplay{
				{Name: "candidate", Label: "首选候选", Description: "优先求值"},
				{Name: "next", Label: "后续候选", Description: "前一项可降级失败时才求值；可继续添加"},
			},
			Result:   ResultDisplay{Label: "结果", Description: "类型 T"},
			Examples: []FunctionExample{{Title: "渠道级联", Expression: "fallback(primary_quote,secondary_quote,default_quote)"}},
			Order:    15,
		},
	})
}

func registerArithmetic(registry *Registry) {
	registerBinary(registry, "add", IntType, "加法 / 拼接", "数值相加或字符串拼接，具体类型由上下文自动推导。", "+", evalIntAdd)
	registerBinary(registry, "add", FloatType, "加法 / 拼接", "数值相加或字符串拼接，具体类型由上下文自动推导。", "+", evalFloatAdd)
	registerBinary(registry, "add", StringType, "加法 / 拼接", "数值相加或字符串拼接，具体类型由上下文自动推导。", "+", func(_ context.Context, args []Value) (Value, error) {
		return String(args[0].s + args[1].s), nil
	})
	registerBinary(registry, "sub", IntType, "减法", "两个同类型数值相减，具体类型由上下文自动推导。", "−", evalIntSub)
	registerBinary(registry, "sub", FloatType, "减法", "两个同类型数值相减，具体类型由上下文自动推导。", "−", evalFloatSub)
	registerBinary(registry, "mul", IntType, "乘法", "两个同类型数值相乘，具体类型由上下文自动推导。", "×", evalIntMul)
	registerBinary(registry, "mul", FloatType, "乘法", "两个同类型数值相乘，具体类型由上下文自动推导。", "×", evalFloatMul)
	registerBinary(registry, "div", IntType, "除法", "两个同类型数值相除；除数不能为零。", "÷", evalIntDiv)
	registerBinary(registry, "div", FloatType, "除法", "两个同类型数值相除；除数不能为零。", "÷", evalFloatDiv)
	registerMixedNumeric(registry, "add", "加法 / 拼接", "+", func(a, b float64) (Value, error) { return finiteResult(a+b, "add") })
	registerMixedNumeric(registry, "sub", "减法", "−", func(a, b float64) (Value, error) { return finiteResult(a-b, "sub") })
	registerMixedNumeric(registry, "mul", "乘法", "×", func(a, b float64) (Value, error) { return finiteResult(a*b, "mul") })
	registerMixedNumeric(registry, "div", "除法", "÷", func(a, b float64) (Value, error) {
		if b == 0 {
			return Value{}, fmt.Errorf("division by zero")
		}
		return finiteResult(a/b, "div")
	})
}

func registerMixedNumeric(registry *Registry, name, label, icon string, eval func(float64, float64) (Value, error)) {
	description := "整数与浮点数混合时自动把整数安全提升为浮点数，结果为 float。"
	for _, params := range [][2]Type{{IntType, FloatType}, {FloatType, IntType}} {
		left, right := params[0], params[1]
		mustRegister(registry, FunctionSpec{
			Name: name, Params: []Type{left, right}, Result: FloatType, Cost: 2,
			Eval: func(_ context.Context, args []Value) (Value, error) {
				a, err := numericFloat(args[0])
				if err != nil {
					return Value{}, err
				}
				b, err := numericFloat(args[1])
				if err != nil {
					return Value{}, err
				}
				return eval(a, b)
			},
			Display: FunctionDisplay{
				Label:       label,
				Description: description,
				Category:    "基础运算",
				Color:       "#2563EB",
				Icon:        icon,
				Parameters: []ParameterDisplay{
					{Name: "left", Label: "左值", Description: "int 或 float"},
					{Name: "right", Label: "右值", Description: "int 或 float"},
				},
				Result: ResultDisplay{Label: "结果", Description: "float"},
				Order:  100,
			},
		})
	}
}

func numericFloat(value Value) (float64, error) {
	if value.kind == IntKind {
		const maxExactFloatInt = int64(1 << 53)
		if value.i < -maxExactFloatInt || value.i > maxExactFloatInt {
			return 0, fmt.Errorf("int %d cannot be represented exactly as float", value.i)
		}
		return float64(value.i), nil
	}
	return value.f, nil
}

func registerConversions(registry *Registry) {
	registerIntConversions(registry)
	registerFloatConversions(registry)
	registerStringConversions(registry)
	registerBoolConversions(registry)
}

func registerIntConversions(registry *Registry) {
	registerConversion(registry, "int", IntType, IntType, "转为整数", "保持整数不变。", func(_ context.Context, args []Value) (Value, error) { return args[0], nil })
	registerConversion(registry, "int", FloatType, IntType, "转为整数", "只接受没有小数部分的浮点数，避免静默丢失精度。", func(_ context.Context, args []Value) (Value, error) {
		if args[0].f < math.MinInt64 || args[0].f > math.MaxInt64 || math.Trunc(args[0].f) != args[0].f {
			return Value{}, fmt.Errorf("float %v cannot be converted to int without data loss", args[0].f)
		}
		return Int(int64(args[0].f)), nil
	})
	registerConversion(registry, "int", StringType, IntType, "转为整数", "解析十进制整数字符串。", func(_ context.Context, args []Value) (Value, error) {
		value, err := strconv.ParseInt(strings.TrimSpace(args[0].s), 10, 64)
		if err != nil {
			return Value{}, fmt.Errorf("cannot convert %q to int", args[0].s)
		}
		return Int(value), nil
	})
}

func registerFloatConversions(registry *Registry) {
	registerConversion(registry, "float", FloatType, FloatType, "转为浮点数", "保持浮点数不变。", func(_ context.Context, args []Value) (Value, error) { return args[0], nil })
	registerConversion(registry, "float", IntType, FloatType, "转为浮点数", "把可精确表示的整数转换为 float64。", func(_ context.Context, args []Value) (Value, error) {
		value, err := numericFloat(args[0])
		if err != nil {
			return Value{}, err
		}
		return Float(value), nil
	})
	registerConversion(registry, "float", StringType, FloatType, "转为浮点数", "解析有限浮点数字符串。", func(_ context.Context, args []Value) (Value, error) {
		value, err := strconv.ParseFloat(strings.TrimSpace(args[0].s), 64)
		if err != nil {
			return Value{}, fmt.Errorf("cannot convert %q to float", args[0].s)
		}
		return CheckedFloat(value)
	})
}

func registerStringConversions(registry *Registry) {
	registerConversion(registry, "string", StringType, StringType, "转为字符串", "保持字符串不变。", func(_ context.Context, args []Value) (Value, error) { return args[0], nil })
	registerConversion(registry, "string", AnyEnumType, StringType, "转为字符串", "取枚举成员的名字。枚举是 nominal 类型，当字符串用必须显式转换。", func(_ context.Context, args []Value) (Value, error) {
		return String(args[0].s), nil
	})
	registerConversion(registry, "string", IntType, StringType, "转为字符串", "把整数格式化为十进制字符串。", func(_ context.Context, args []Value) (Value, error) {
		return String(strconv.FormatInt(args[0].i, 10)), nil
	})
	registerConversion(registry, "string", FloatType, StringType, "转为字符串", "用稳定格式输出浮点数。", func(_ context.Context, args []Value) (Value, error) {
		return String(strconv.FormatFloat(args[0].f, 'g', -1, 64)), nil
	})
	registerConversion(registry, "string", BoolType, StringType, "转为字符串", "把布尔值格式化为 true 或 false。", func(_ context.Context, args []Value) (Value, error) {
		return String(strconv.FormatBool(args[0].b)), nil
	})
}

func registerBoolConversions(registry *Registry) {
	registerConversion(registry, "bool", BoolType, BoolType, "转为布尔值", "保持布尔值不变。", func(_ context.Context, args []Value) (Value, error) { return args[0], nil })
	registerConversion(registry, "bool", StringType, BoolType, "转为布尔值", "解析 true 或 false，不接受模糊写法。", func(_ context.Context, args []Value) (Value, error) {
		switch strings.ToLower(strings.TrimSpace(args[0].s)) {
		case "true":
			return Bool(true), nil
		case "false":
			return Bool(false), nil
		default:
			return Value{}, fmt.Errorf("cannot convert %q to bool", args[0].s)
		}
	})
}

func registerConversion(registry *Registry, name string, from, to Type, label, description string, eval EvalFunc) {
	mustRegister(registry, FunctionSpec{
		Name: name, Params: []Type{from}, Result: to, Cost: 2, Eval: eval,
		Display: FunctionDisplay{
			Label:       label,
			Description: description,
			Category:    "类型转换",
			Color:       "#0F766E",
			Icon:        "↪",
			Parameters:  []ParameterDisplay{{Name: "value", Label: "值", Description: "自动选择匹配的转换"}},
			Result:      ResultDisplay{Label: "转换结果", Description: to.String()},
			Order:       10,
		},
	})
}

func mustRegister(registry *Registry, spec FunctionSpec) {
	spec.builtin = true
	if err := registry.Register(spec); err != nil {
		panic(err)
	}
}

func registerBinary(registry *Registry, name string, typ Type, label, description, icon string, eval EvalFunc) {
	mustRegister(registry, FunctionSpec{
		Name: name, Params: []Type{typ, typ}, Result: typ, Cost: 2, Eval: eval,
		Display: FunctionDisplay{
			Label:       label,
			Description: description,
			Category:    "基础运算",
			Color:       "#2563EB",
			Icon:        icon,
			Parameters: []ParameterDisplay{
				{Name: "left", Label: "左值", Description: typ.String()},
				{Name: "right", Label: "右值", Description: typ.String()},
			},
			Result: ResultDisplay{Label: "结果", Description: typ.String()},
			Order:  100,
		},
	})
}

// RegisterArrayPrimitives adds is_empty/prepend/head/tail: the list algebra
// that reduce and the comprehensions build on. docs/termination.md shows what
// adding an unbounded recursion form to them would make expressible.
func RegisterArrayPrimitives(registry *Registry) error {
	for _, spec := range []FunctionSpec{arrayIsEmptySpec(), arrayPrependSpec(), arrayHeadSpec(), arrayTailSpec()} {
		spec.builtin = true
		if err := registry.Register(spec); err != nil {
			return err
		}
	}
	return nil
}

func arrayIsEmptySpec() FunctionSpec {
	t := TypeVar("T")
	arrayT := ArrayOf(t)
	return FunctionSpec{
		Name: "array.is_empty", Params: []Type{arrayT}, Result: BoolType, Cost: 1,
		Eval: func(_ context.Context, args []Value) (Value, error) { return Bool(args[0].length() == 0), nil },
		Display: FunctionDisplay{
			Label:       "数组是否为空",
			Description: "判断同型数组是否没有元素；可与 head、tail、prepend 组合处理列表。",
			Category:    "最小列表",
			Color:       "#D97706",
			Icon:        "∅",
			Parameters:  []ParameterDisplay{{Name: "array", Label: "数组", Description: "array<T>"}},
			Result:      ResultDisplay{Label: "是否为空", Description: "bool"},
			Order:       10,
		},
	}
}

func arrayPrependSpec() FunctionSpec {
	t := TypeVar("T")
	arrayT := ArrayOf(t)
	return FunctionSpec{
		Name: "array.prepend", Params: []Type{t, arrayT}, Result: arrayT, Cost: 2,
		Eval: func(_ context.Context, args []Value) (Value, error) {
			array := args[1]
			builder := newArrayBuilder(array.elemType(), array.length()+1)
			builder.add(args[0])
			for i := 0; i < array.length(); i++ {
				builder.add(array.at(i))
			}
			return builder.finish(), nil
		},
		Display: FunctionDisplay{
			Label:       "数组头部插入",
			Description: "返回在数组头部加入一个同类型元素的新数组，不修改原数组。",
			Category:    "最小列表",
			Color:       "#D97706",
			Icon:        "+[",
			Parameters: []ParameterDisplay{
				{Name: "value", Label: "新元素", Description: "T"},
				{Name: "array", Label: "原数组", Description: "array<T>"},
			},
			Result: ResultDisplay{Label: "新数组", Description: "array<T>"},
			Order:  20,
		},
	}
}

func arrayHeadSpec() FunctionSpec {
	t := TypeVar("T")
	arrayT := ArrayOf(t)
	return FunctionSpec{
		Name: "array.head", Params: []Type{arrayT}, Result: t, Cost: 1,
		Eval: func(_ context.Context, args []Value) (Value, error) {
			if args[0].length() == 0 {
				return Value{}, fmt.Errorf("array.head requires a non-empty array")
			}
			return args[0].at(0), nil
		},
		Display: FunctionDisplay{
			Label:       "数组首元素",
			Description: "返回非空数组的第一个元素；空数组会返回运行错误。",
			Category:    "最小列表",
			Color:       "#D97706",
			Icon:        "[0]",
			Parameters:  []ParameterDisplay{{Name: "array", Label: "非空数组", Description: "array<T>"}},
			Result:      ResultDisplay{Label: "首元素", Description: "T"},
			Order:       30,
		},
	}
}

func arrayTailSpec() FunctionSpec {
	t := TypeVar("T")
	arrayT := ArrayOf(t)
	return FunctionSpec{
		Name: "array.tail", Params: []Type{arrayT}, Result: arrayT, Cost: 1,
		Eval: func(_ context.Context, args []Value) (Value, error) {
			if args[0].length() == 0 {
				return Value{}, fmt.Errorf("array.tail requires a non-empty array")
			}
			return args[0].tail(), nil
		},
		Display: FunctionDisplay{
			Label:       "移除数组首元素",
			Description: "返回移除首元素后的新数组；空数组会返回运行错误。",
			Category:    "最小列表",
			Color:       "#D97706",
			Icon:        "]",
			Parameters:  []ParameterDisplay{{Name: "array", Label: "非空数组", Description: "array<T>"}},
			Result:      ResultDisplay{Label: "剩余数组", Description: "array<T>"},
			Order:       40,
		},
	}
}

func evalIntAdd(_ context.Context, args []Value) (Value, error) {
	a, b := args[0].i, args[1].i
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return Value{}, fmt.Errorf("integer overflow in add")
	}
	return Int(a + b), nil
}

func evalIntSub(_ context.Context, args []Value) (Value, error) {
	a, b := args[0].i, args[1].i
	if (b < 0 && a > math.MaxInt64+b) || (b > 0 && a < math.MinInt64+b) {
		return Value{}, fmt.Errorf("integer overflow in sub")
	}
	return Int(a - b), nil
}

func evalIntMul(_ context.Context, args []Value) (Value, error) {
	a, b := args[0].i, args[1].i
	if a == 0 || b == 0 {
		return Int(0), nil
	}
	if (a == math.MinInt64 && b == -1) || (b == math.MinInt64 && a == -1) {
		return Value{}, fmt.Errorf("integer overflow in mul")
	}
	result := a * b
	if result/b != a {
		return Value{}, fmt.Errorf("integer overflow in mul")
	}
	return Int(result), nil
}

func evalIntDiv(_ context.Context, args []Value) (Value, error) {
	a, b := args[0].i, args[1].i
	if b == 0 {
		return Value{}, fmt.Errorf("division by zero")
	}
	if a == math.MinInt64 && b == -1 {
		return Value{}, fmt.Errorf("integer overflow in div")
	}
	return Int(a / b), nil
}

func evalFloatAdd(_ context.Context, args []Value) (Value, error) {
	return finiteResult(args[0].f+args[1].f, "add")
}
func evalFloatSub(_ context.Context, args []Value) (Value, error) {
	return finiteResult(args[0].f-args[1].f, "sub")
}
func evalFloatMul(_ context.Context, args []Value) (Value, error) {
	return finiteResult(args[0].f*args[1].f, "mul")
}

func evalFloatDiv(_ context.Context, args []Value) (Value, error) {
	if args[1].f == 0 {
		return Value{}, fmt.Errorf("division by zero")
	}
	return finiteResult(args[0].f/args[1].f, "div")
}

func finiteResult(value float64, operation string) (Value, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return Value{}, fmt.Errorf("non-finite float result in %s", operation)
	}
	return Float(value), nil
}
