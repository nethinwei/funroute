package machine

import (
	"cmp"
	"context"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/nethinwei/funroute/internal/kit"
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
	registerContainers(r)
	return r
}

// comparisons are the four ordering predicates, here and in the money kernel.
// The remaining operators are pure sugar over these and if: a != b is
// if(eq(a,b),false,true), a && b is if(a,b,false), a || b is if(a,true,b), !a
// is if(a,false,true).
var comparisons = []struct {
	name, label string
	accept      func(int) bool
}{
	{"lt", "小于", func(order int) bool { return order < 0 }},
	{"le", "小于等于", func(order int) bool { return order <= 0 }},
	{"gt", "大于", func(order int) bool { return order > 0 }},
	{"ge", "大于等于", func(order int) bool { return order >= 0 }},
}

func registerComparisons(registry *Registry) {
	for _, comparison := range comparisons {
		registerComparison(registry, comparison.name, comparison.label, comparison.accept)
	}
}

func registerComparison(registry *Registry, name, label string, accept func(int) bool) {
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
			Name: name, Params: []Type{params[0], params[1]}, Result: BoolType, Eval: eval,
			Doc: Doc{
				Label:       label,
				Description: "比较两个数值或两个字符串；数值混合时整数会被安全提升。字符串按 UTF-8 字节序比较。",
				Category:    "比较",
				Params:      []string{"左值", "右值"},
				Result:      "比较结果",
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
		return cmp.Compare(left.i, right.i), nil
	}
	first, err := numericFloat(left)
	if err != nil {
		return 0, err
	}
	second, err := numericFloat(right)
	if err != nil {
		return 0, err
	}
	return cmp.Compare(first, second), nil
}

func registerControl(registry *Registry) {
	t := TypeVar("T")
	mustRegister(registry, FunctionSpec{
		Name: "if", Params: []Type{BoolType, t, t}, Result: t,
		special: specialIf,
		Doc: Doc{
			Label:       "条件选择",
			Description: "条件为真时只计算真分支，否则只计算假分支。两个分支必须返回同一类型。",
			Category:    "控制",
			Params:      []string{"条件", "为真", "为假"},
			Result:      "所选分支",
		},
	})
	registerFallback(registry, t)
	mustRegister(registry, FunctionSpec{
		Name: "eq", Params: []Type{t, t}, Result: BoolType, takesExact: true, Eval: func(_ context.Context, args []Value) (Value, error) { return compareEqual(args[0], args[1]) },
		Doc: Doc{
			Label:       "相等判断",
			Description: "比较两个同类型值是否完全相等。",
			Category:    "控制",
			Params:      []string{"左值", "右值"},
			Result:      "是否相等",
		},
	})
}

func registerFallback(registry *Registry, t Type) {
	mustRegister(registry, FunctionSpec{
		Name: "fallback", Params: []Type{t, t}, Result: t,
		special: specialFallback,
		Doc: Doc{
			Label:       "失败降级",
			Description: "按顺序尝试候选表达式；遇到扩展失败或超时才继续下一项，不会吞掉类型或内核运算错误。",
			Category:    "控制",
			Params:      []string{"首选候选", "后续候选"},
			Result:      "结果",
		},
	})
}

// arithmetic is the four operators on numbers, int with int and float with
// float, in registration order; add also joins strings.
var arithmetic = []struct {
	name, label, description string
	ints, floats             EvalFunc
	mixed                    func(a, b float64) (Value, error)
}{
	{"add", "加法 / 拼接", "数值相加或字符串拼接，具体类型由上下文自动推导。", evalIntAdd, evalFloatAdd,
		func(a, b float64) (Value, error) { return Float(a + b), nil }},
	{"sub", "减法", "两个同类型数值相减，具体类型由上下文自动推导。", evalIntSub, evalFloatSub,
		func(a, b float64) (Value, error) { return Float(a - b), nil }},
	{"mul", "乘法", "两个同类型数值相乘，具体类型由上下文自动推导。", evalIntMul, evalFloatMul,
		func(a, b float64) (Value, error) { return Float(a * b), nil }},
	{"div", "除法", "两个同类型数值相除；整数除数不能为零，浮点数按 IEEE 754：除以零得到无穷大，0.0 / 0.0 得到 NaN。", evalIntDiv, evalFloatDiv, floatDiv},
}

func registerArithmetic(registry *Registry) {
	for _, op := range arithmetic {
		registerBinary(registry, op.name, IntType, op.label, op.description, op.ints)
		registerBinary(registry, op.name, FloatType, op.label, op.description, op.floats)
		if op.name == "add" {
			registerBinary(registry, op.name, StringType, op.label, op.description, func(_ context.Context, args []Value) (Value, error) {
				return String(args[0].s + args[1].s), nil
			})
		}
	}
	for _, op := range arithmetic {
		registerMixedNumeric(registry, op.name, op.label, op.mixed)
	}
}

func registerMixedNumeric(registry *Registry, name, label string, eval func(float64, float64) (Value, error)) {
	description := "整数与浮点数混合时自动把整数安全提升为浮点数，结果为 float。"
	for _, params := range [][2]Type{{IntType, FloatType}, {FloatType, IntType}} {
		left, right := params[0], params[1]
		mustRegister(registry, FunctionSpec{
			Name: name, Params: []Type{left, right}, Result: FloatType,
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
			Doc: Doc{
				Label:       label,
				Description: description,
				Category:    "基础运算",
				Params:      []string{"左值", "右值"},
				Result:      "结果",
			},
		})
	}
}

func numericFloat(value Value) (float64, error) {
	if value.kind == IntKind {
		if value.i < -maxExactFloatInt || value.i > maxExactFloatInt {
			return 0, kit.Errorf(ErrArithmetic, "int %d cannot be represented exactly as float", value.i)
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
	registerConversion(registry, "int", IntType, IntType, "转为整数", "保持整数不变。", evalIdentity)
	registerConversion(registry, "int", FloatType, IntType, "转为整数", "只接受没有小数部分的浮点数，避免静默丢失精度。", func(_ context.Context, args []Value) (Value, error) {
		whole, ok := floatInt(args[0].f)
		if !ok {
			return Value{}, kit.Errorf(ErrArithmetic, "float %v cannot be converted to int without data loss", args[0].f)
		}
		return Int(whole), nil
	})
	registerConversion(registry, "int", StringType, IntType, "转为整数", "解析十进制整数字符串。", func(_ context.Context, args []Value) (Value, error) {
		value, err := strconv.ParseInt(strings.TrimSpace(args[0].s), 10, 64)
		if err != nil {
			return Value{}, kit.Errorf(ErrArithmetic, "cannot convert %q to int", args[0].s)
		}
		return Int(value), nil
	})
}

func registerFloatConversions(registry *Registry) {
	registerConversion(registry, "float", FloatType, FloatType, "转为浮点数", "保持浮点数不变。", evalIdentity)
	registerConversion(registry, "float", IntType, FloatType, "转为浮点数", "把可精确表示的整数转换为 float64。", func(_ context.Context, args []Value) (Value, error) {
		value, err := numericFloat(args[0])
		if err != nil {
			return Value{}, err
		}
		return Float(value), nil
	})
	registerConversion(registry, "float", StringType, FloatType, "转为浮点数", "解析浮点数字符串；NaN、Inf 照常读出，超出范围的得到无穷大。", func(_ context.Context, args []Value) (Value, error) {
		value, err := strconv.ParseFloat(strings.TrimSpace(args[0].s), 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return Value{}, kit.Errorf(ErrArithmetic, "cannot convert %q to float", args[0].s)
		}
		return Float(value), nil
	})
}

func registerStringConversions(registry *Registry) {
	registerConversion(registry, "string", StringType, StringType, "转为字符串", "保持字符串不变。", evalIdentity)
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
	registerConversion(registry, "bool", BoolType, BoolType, "转为布尔值", "保持布尔值不变。", evalIdentity)
	registerConversion(registry, "bool", StringType, BoolType, "转为布尔值", "解析 true 或 false，不接受模糊写法。", func(_ context.Context, args []Value) (Value, error) {
		switch strings.ToLower(strings.TrimSpace(args[0].s)) {
		case "true":
			return Bool(true), nil
		case "false":
			return Bool(false), nil
		default:
			return Value{}, kit.Errorf(ErrArithmetic, "cannot convert %q to bool", args[0].s)
		}
	})
}

// evalIdentity is a conversion to the type the value already has.
func evalIdentity(_ context.Context, args []Value) (Value, error) { return args[0], nil }

func registerConversion(registry *Registry, name string, from, to Type, label, description string, eval EvalFunc) {
	mustRegister(registry, FunctionSpec{
		Name: name, Params: []Type{from}, Result: to, Eval: eval,
		Doc: Doc{
			Label:       label,
			Description: description,
			Category:    "类型转换",
			Params:      []string{"值"},
			Result:      "转换结果",
		},
	})
}

func mustRegister(registry *Registry, spec FunctionSpec) {
	spec.builtin = true
	spec.Doc.Examples = kernelExamples[spec.Name]
	if err := registry.Register(spec); err != nil {
		panic(err)
	}
}

func registerBinary(registry *Registry, name string, typ Type, label, description string, eval EvalFunc) {
	mustRegister(registry, FunctionSpec{
		Name: name, Params: []Type{typ, typ}, Result: typ, Eval: eval,
		Doc: Doc{
			Label:       label,
			Description: description,
			Category:    "基础运算",
			Params:      []string{"左值", "右值"},
			Result:      "结果",
		},
	})
}

func evalIntAdd(_ context.Context, args []Value) (Value, error) {
	a, b := args[0].i, args[1].i
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return Value{}, overflowIn("add")
	}
	return Int(a + b), nil
}

func evalIntSub(_ context.Context, args []Value) (Value, error) {
	a, b := args[0].i, args[1].i
	if (b < 0 && a > math.MaxInt64+b) || (b > 0 && a < math.MinInt64+b) {
		return Value{}, overflowIn("sub")
	}
	return Int(a - b), nil
}

func evalIntMul(_ context.Context, args []Value) (Value, error) {
	a, b := args[0].i, args[1].i
	if a == 0 || b == 0 {
		return Int(0), nil
	}
	if (a == math.MinInt64 && b == -1) || (b == math.MinInt64 && a == -1) {
		return Value{}, overflowIn("mul")
	}
	result := a * b
	if result/b != a {
		return Value{}, overflowIn("mul")
	}
	return Int(result), nil
}

func evalIntDiv(_ context.Context, args []Value) (Value, error) {
	a, b := args[0].i, args[1].i
	if b == 0 {
		return Value{}, errDivisionByZero
	}
	if a == math.MinInt64 && b == -1 {
		return Value{}, overflowIn("div")
	}
	return Int(a / b), nil
}

func evalFloatAdd(_ context.Context, args []Value) (Value, error) {
	return Float(args[0].f + args[1].f), nil
}
func evalFloatSub(_ context.Context, args []Value) (Value, error) {
	return Float(args[0].f - args[1].f), nil
}
func evalFloatMul(_ context.Context, args []Value) (Value, error) {
	return Float(args[0].f * args[1].f), nil
}

func evalFloatDiv(_ context.Context, args []Value) (Value, error) {
	return floatDiv(args[0].f, args[1].f)
}

// floatDiv is a/b as IEEE 754 has it: a division by zero is an infinity,
// or NaN for 0/0.
func floatDiv(a, b float64) (Value, error) { return Float(a / b), nil }
