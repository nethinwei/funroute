package machine

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/nethinwei/funroute/internal/kit"
)

// The container half of the kernel's functions — the rest of CoreRegistry is in
// builtins.go, and both use its mustRegister. Not to be confused with
// container.go, which is how a container is *represented* (the native Go
// backing behind a Value); this file is what a program can *ask* one.
//
// registerContainers adds the three questions a container answers: what is at
// this position, is this item in there, and how many are there. An array, a
// dictionary and a string share each name because it is the same question —
// only the key differs, and the compiler already knows which one it is
// holding.
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
	mustRegister(registry, FunctionSpec{
		Name: "at", Params: []Type{StringType, IntType}, Result: StringType, Eval: evalStringAt,
		Doc: Doc{
			Cost: 3, Label: "取字符", Category: "容器",
			Description: "按位置取一个字符，从 0 开始，按 UTF-8 码点数而不是字节；越界是错误。结果仍是字符串，语言里没有单字符类型。写作 s[i]。",
			Params:      []string{"文本", "位置"}, Result: "那个字符",
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
	mustRegister(registry, FunctionSpec{
		Name: "member", Params: []Type{StringType, StringType}, Result: BoolType, Eval: evalStringMember,
		Doc: Doc{
			Cost: 3, Label: "是否含子串", Category: "容器",
			Description: `文本里有没有这段子串，和 Python 的 in 一样。写作 "b" in text；参数顺序相反的写法是 contains(text, "b")。`,
			Params:      []string{"子串", "文本"}, Result: "是否命中",
		},
	})
}

func registerLength(registry *Registry, t Type) {
	container := Doc{
		Cost: 2, Label: "长度", Category: "容器",
		Description: "数组的元素个数、字典的键个数，或字符串的字符数（UTF-8 码点）。",
		Params:      []string{"容器"}, Result: "个数",
	}
	// len of a comprehension counts what it would have yielded, with no
	// array built.
	mustRegister(registry, FunctionSpec{Name: "len", Params: []Type{ArrayOf(t)}, Result: IntType, Eval: evalLength, Doc: container, Fold: &Fold{Counts: true}})
	mustRegister(registry, FunctionSpec{Name: "len", Params: []Type{DictOf(t)}, Result: IntType, Eval: evalLength, Doc: container})
	mustRegister(registry, FunctionSpec{Name: "len", Params: []Type{StringType}, Result: IntType, Eval: evalStringLength, Doc: container})
}

func evalArrayAt(_ context.Context, args []Value) (Value, error) {
	index, length := args[1].i, int64(args[0].length())
	if index < 0 || index >= length {
		return Value{}, kit.Errorf(ErrDomain, "index %d is outside an array of %d items", index, length)
	}
	return args[0].at(int(index)), nil
}

func evalDictAt(_ context.Context, args []Value) (Value, error) {
	value, ok := args[0].lookup(args[1].s)
	if !ok {
		return Value{}, kit.Errorf(ErrDomain, "the dictionary has no key %q", args[1].s)
	}
	return value, nil
}

// evalStringAt counts in code points, the same unit len(string) reports, and
// walks to the index instead of building a []rune: taking one character out of
// a card number should not copy the card number. The character is the bytes
// the text has there, so a byte that is not UTF-8 comes out as itself.
func evalStringAt(_ context.Context, args []Value) (Value, error) {
	text, index := args[0].s, args[1].i
	rest := text
	for i := int64(0); i < index && rest != ""; i++ {
		_, size := utf8.DecodeRuneInString(rest)
		rest = rest[size:]
	}
	_, size := utf8.DecodeRuneInString(rest)
	if index < 0 || size == 0 {
		return Value{}, kit.Errorf(ErrDomain, "index %d is outside a string of %d characters", index, utf8.RuneCountInString(text))
	}
	return String(rest[:size]), nil
}

func evalStringMember(_ context.Context, args []Value) (Value, error) {
	return Bool(strings.Contains(args[1].s, args[0].s)), nil
}

func evalArrayMember(_ context.Context, args []Value) (Value, error) {
	item, container := args[0], args[1]
	if found, native := nativeMember(item, container.box); native {
		return Bool(found), nil
	}
	for i := range container.length() {
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

// nativeMember is whether a native array of scalars holds item, as the
// equality walk would find: native is false for any other array.
func nativeMember(item Value, backing any) (found, native bool) {
	switch items := backing.(type) {
	case []int64:
		return slices.Contains(items, item.i), true
	case []float64:
		return slices.Contains(items, item.f), true
	case []string:
		return slices.Contains(items, item.s), true
	case []bool:
		return slices.Contains(items, item.b), true
	}
	return false, false
}

func evalDictMember(_ context.Context, args []Value) (Value, error) {
	_, ok := args[1].lookup(args[0].s)
	return Bool(ok), nil
}

func evalLength(_ context.Context, args []Value) (Value, error) {
	length, ok := args[0].Length()
	if !ok {
		return Value{}, errors.New("len needs an array or a dictionary")
	}
	return Int(int64(length)), nil
}

func evalStringLength(_ context.Context, args []Value) (Value, error) {
	return Int(int64(utf8.RuneCountInString(args[0].s))), nil
}

func evalIntMod(_ context.Context, args []Value) (Value, error) {
	dividend, divisor := args[0].i, args[1].i
	if divisor == 0 {
		return Value{}, errDivisionByZero
	}
	if divisor == -1 {
		return Int(0), nil // avoids the MinInt64 % -1 overflow trap
	}
	return Int(dividend % divisor), nil
}
