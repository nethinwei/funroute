package machine

import (
	"context"
	"fmt"
	"unicode/utf8"
)

// registerContainers adds the three questions a container answers: what is at
// this position, is this item in there, and how many are there. An array and a
// dictionary share each name because it is the same question — only the key
// differs, and the compiler already knows which one it is holding.
//
// There is no missing-value result. `xs[9]` on a three-item array and `d["x"]`
// on a dictionary without that key are errors, the way division by zero is:
// the language has no null to hand back, and inventing one would put a silent
// wrong answer where a stopped rule belongs.
func registerContainers(registry *Registry) {
	t := TypeVar("T")
	registerAt(registry, t)
	registerMember(registry, t)
	registerLength(registry, t)
	registerBinary(registry, "mod", IntType, "取余",
		"两个整数相除的余数，符号跟随被除数；除数不能为零。写作 a % b。", evalIntMod)
}

func registerAt(registry *Registry, t Type) {
	mustRegister(registry, FunctionSpec{
		Name: "at", Params: []Type{ArrayOf(t), IntType}, Result: t, Eval: evalArrayAt,
		Doc: Doc{
			Cost: 2, Label: "取元素", Category: "容器",
			Description: "按下标取数组元素，下标从 0 开始；越界是错误，不会返回空值。写作 xs[i]。",
			Params:      []string{"数组", "下标"}, Result: "元素",
		},
	})
	mustRegister(registry, FunctionSpec{
		Name: "at", Params: []Type{DictOf(t), StringType}, Result: t, Eval: evalDictAt,
		Doc: Doc{
			Cost: 2, Label: "取值", Category: "容器",
			Description: `按键取字典的值；键不存在是错误，不会返回空值。写作 d["key"]。`,
			Params:      []string{"字典", "键"}, Result: "值",
		},
	})
}

func registerMember(registry *Registry, t Type) {
	mustRegister(registry, FunctionSpec{
		Name: "member", Params: []Type{t, ArrayOf(t)}, Result: BoolType, Eval: evalArrayMember,
		Doc: Doc{
			Cost: 3, Label: "是否在数组里", Category: "容器",
			Description: "数组里有没有这个元素，按相等判断逐个比较。写作 x in xs。",
			Params:      []string{"元素", "数组"}, Result: "是否命中",
		},
	})
	mustRegister(registry, FunctionSpec{
		Name: "member", Params: []Type{StringType, DictOf(t)}, Result: BoolType, Eval: evalDictMember,
		Doc: Doc{
			Cost: 2, Label: "是否有这个键", Category: "容器",
			Description: `字典里有没有这个键（不看值）。写作 "key" in d。`,
			Params:      []string{"键", "字典"}, Result: "是否存在",
		},
	})
}

func registerLength(registry *Registry, t Type) {
	container := Doc{
		Cost: 2, Label: "长度", Category: "容器",
		Description: "数组的元素个数、字典的键个数，或字符串的字符数（UTF-8 码点）。",
		Params:      []string{"容器"}, Result: "个数",
	}
	mustRegister(registry, FunctionSpec{Name: "len", Params: []Type{ArrayOf(t)}, Result: IntType, Eval: evalLength, Doc: container})
	mustRegister(registry, FunctionSpec{Name: "len", Params: []Type{DictOf(t)}, Result: IntType, Eval: evalLength, Doc: container})
	mustRegister(registry, FunctionSpec{Name: "len", Params: []Type{StringType}, Result: IntType, Eval: evalStringLength, Doc: container})
}

func evalArrayAt(_ context.Context, args []Value) (Value, error) {
	index, length := args[1].i, int64(args[0].length())
	if index < 0 || index >= length {
		return Value{}, fmt.Errorf("index %d is outside an array of %d items", index, length)
	}
	return args[0].at(int(index)), nil
}

func evalDictAt(_ context.Context, args []Value) (Value, error) {
	value, ok := args[0].lookup(args[1].s)
	if !ok {
		return Value{}, fmt.Errorf("the dictionary has no key %q", args[1].s)
	}
	return value, nil
}

func evalArrayMember(_ context.Context, args []Value) (Value, error) {
	item, container := args[0], args[1]
	for i := 0; i < container.length(); i++ {
		equal, err := compareEqual(container.at(i), item)
		if err != nil {
			return Value{}, err
		}
		if found, _ := equal.Bool(); found {
			return Bool(true), nil
		}
	}
	return Bool(false), nil
}

func evalDictMember(_ context.Context, args []Value) (Value, error) {
	_, ok := args[1].lookup(args[0].s)
	return Bool(ok), nil
}

func evalLength(_ context.Context, args []Value) (Value, error) {
	length, ok := args[0].Length()
	if !ok {
		return Value{}, fmt.Errorf("len needs an array or a dictionary")
	}
	return Int(int64(length)), nil
}

func evalStringLength(_ context.Context, args []Value) (Value, error) {
	return Int(int64(utf8.RuneCountInString(args[0].s))), nil
}

func evalIntMod(_ context.Context, args []Value) (Value, error) {
	dividend, divisor := args[0].i, args[1].i
	if divisor == 0 {
		return Value{}, fmt.Errorf("division by zero")
	}
	if divisor == -1 {
		return Int(0), nil // avoids the MinInt64 % -1 overflow trap
	}
	return Int(dividend % divisor), nil
}
