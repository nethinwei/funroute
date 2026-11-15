package machine_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

// runKernel runs source with one argument x of x's own type through the core
// registry.
func runKernel(t *testing.T, source string, x machine.Value) (machine.Value, error) {
	t.Helper()
	registry := machine.CoreRegistry()
	artifact, err := compile.CompileExpr(source, registry, compile.CompileOptions{Args: []compile.ArgSpec{{Name: "x", Type: x.Type()}}})
	if err != nil {
		t.Fatalf("CompileExpr(%q) error = %v", source, err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	return runtime.RunValues(t.Context(), []machine.Value{x})
}

// int(float) takes a whole float inside int64 and nothing else; 2^63 is
// outside, although math.MaxInt64 converts to it.
func TestIntOfAFloatStopsBelowTwoToTheSixtyThree(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		x    float64
		fits bool
	}{{0x1p63, false}, {-0x1p63, true}, {0x1p62, true}, {2.5, false}} {
		got, err := runKernel(t, "int(x)", machine.Float(test.x))
		if fits := err == nil; fits != test.fits {
			t.Errorf("int(%v) = %v, %v, want fits %v", test.x, got.Any(), err, test.fits)
		}
	}
}

// A conversion with no answer is ErrArithmetic, the rule's or the data's, as
// a division by zero is: text that is no number, a fraction taken as an int,
// an int past what a float holds exactly.
func TestConversionsWithNoAnswerAreArithmetic(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		source string
		x      machine.Value
	}{
		{"int(x)", machine.String("seven")},
		{"float(x)", machine.String("seven")},
		{"int(x)", machine.Float(2.5)},
		{"float(x)", machine.Int(1<<53 + 1)},
	} {
		if got, err := runKernel(t, test.source, test.x); !errors.Is(err, machine.ErrArithmetic) {
			t.Errorf("%s with x = %v: %v, %v, want ErrArithmetic", test.source, test.x.Any(), got.Any(), err)
		}
	}
}

// A comparison with NaN is false as IEEE 754 has it, whether the other side
// is a float or an int: 1 and 1.0 are the same number, so x < 1 is x < 1.0.
func TestNaNComparesFalseWithAnInt(t *testing.T) {
	t.Parallel()
	nan := machine.Float(math.NaN())
	for _, source := range []string{"x < 1", "x <= 1", "x > 1", "x >= 1", "1 < x", "1 >= x", "x < 1.0", "x >= 1.0"} {
		if got, err := runKernel(t, source, nan); err != nil || got.Any() != false {
			t.Errorf("%s with x = NaN: %v, %v, want false", source, got.Any(), err)
		}
	}
}

// A function declared Additive hands back no more than it is given, give or
// take a fixed amount, counting the items of every container and the
// characters of every string: every official one is called with arguments
// each from empty to hundreds of items, in every combination, and what
// comes back counted against what went in. One that doubled its arguments,
// made an item of every character or put a separator in for every item
// fails on the long ones. A reduce's accumulator is let through these
// functions on that promise (compile.requireBoundedFold).
func TestAnAdditiveFunctionHandsBackNoMoreThanItIsGiven(t *testing.T) {
	t.Parallel()
	registry := officialRegistry(t)
	checked := map[string]bool{}
	for _, function := range additiveSignatures(t, registry) {
		for _, elem := range []machine.Type{machine.StringType, machine.ArrayOf(machine.IntType)} {
			ran := sampled{t: t, function: function, elem: elem}.check(registry)
			checked[function.Name] = checked[function.Name] || ran
		}
	}
	for name, ran := range checked {
		if !ran {
			t.Errorf("%s is declared additive, and none of these samples runs a call of it", name)
		}
	}
	for _, name := range machine.KernelAdditive() {
		if _, registered := checked[name]; !registered {
			t.Errorf("kernelAdditive lists %q, which is no kernel function", name)
		}
	}
}

// builds is the most a function makes of its own, whatever it is given:
// the fixed amount of its promise. A padding and a repeat build up to
// std's maxPadWidth characters; the rest pick from what they are given or
// put it together, and make no more than a number written out.
func builds(name string) int {
	if name == "pad_left" || name == "pad_right" || name == "repeat" {
		return 10_000
	}
	return 32
}

type signature struct {
	Name     string         `json:"name"`
	Params   []machine.Type `json:"params"`
	Additive bool           `json:"additive"`
}

// additiveSignatures is the registry's functions declared Additive, as its
// manifest writes them.
func additiveSignatures(t *testing.T, registry *machine.Registry) []signature {
	t.Helper()
	encoded, err := json.Marshal(registry.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Functions []signature `json:"functions"`
	}
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	return slices.DeleteFunc(manifest.Functions, func(function signature) bool { return !function.Additive })
}

// sampled is one function called with sampled arguments, T read as elem.
type sampled struct {
	t        *testing.T
	function signature
	elem     machine.Type
}

// check calls the function with each parameter of every size, the ints of
// every number, and reports whether any call ran: one fails for what it is
// asked — first of nothing — and one of a parameter with no sample is not
// made.
func (s sampled) check(registry *machine.Registry) bool {
	s.t.Helper()
	contract := make([]compile.ArgSpec, len(s.function.Params))
	names := make([]string, len(contract))
	for i, param := range s.function.Params {
		typ, err := machine.ParseTypeWith(param.String(), map[string]machine.Type{"T": s.elem})
		if _, has := sample(typ, 0, 0); err != nil || !has {
			return false
		}
		names[i] = fmt.Sprintf("p%d", i)
		contract[i] = compile.ArgSpec{Name: names[i], Type: typ}
	}
	artifact, err := compile.CompileExpr(s.function.Name+"("+strings.Join(names, ", ")+")", registry, compile.CompileOptions{Args: contract})
	if err != nil {
		return false
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		s.t.Fatal(err)
	}
	sizes, ran, choices := []int{0, 1, 3, 300}, false, 1
	for range contract {
		choices *= 4
	}
	for choice := range choices {
		for _, number := range []int64{0, 1, 2, 40} {
			ran = s.call(runtime, contract, sizes, choice, number) || ran
		}
	}
	return ran
}

// call is one call: parameter i of the size choice's i-th digit picks.
func (s sampled) call(runtime *machine.Runtime, contract []compile.ArgSpec, sizes []int, choice int, number int64) bool {
	s.t.Helper()
	args, given, picked := map[string]any{}, 0, make([]int, len(contract))
	for i, param := range contract {
		picked[i] = sizes[choice%len(sizes)]
		choice /= len(sizes)
		args[param.Name], _ = sample(param.Type, picked[i], number+int64(i))
		given += count(args[param.Name])
	}
	result, err := runtime.Run(s.t.Context(), args)
	if err != nil {
		return false
	}
	if got := count(result.Any()); got > given+builds(s.function.Name) {
		s.t.Errorf("%s%v, T %s, sizes %v, numbers from %d: hands back %d, given %d", s.function.Name, s.function.Params, s.elem, picked, number, got, given)
	}
	return true
}

// sample is a Go value of typ: containers of size items, strings of size
// characters — some of them more than a byte, some that change case — and
// ints of number; false for a type with no sample here.
func sample(typ machine.Type, size int, number int64) (any, bool) {
	switch typ.Kind() {
	case machine.BoolKind:
		return number%2 == 0, true
	case machine.IntKind:
		return number, true
	case machine.FloatKind:
		return float64(number) / 2, true
	case machine.StringKind:
		// Each from another place in the pattern, so that they differ.
		return string([]rune(strings.Repeat("aß, Ǆ", size/5+2))[number%5:][:size]), true
	case machine.MoneyKind:
		return money.Make("USD", number), true
	case machine.ArrayKind:
		elem, _ := typ.Elem()
		items := make([]any, size)
		for i := range items {
			item, ok := sample(elem, 2, number+int64(i%7))
			if !ok {
				return nil, false
			}
			items[i] = item
		}
		return items, true
	case machine.DictKind:
		elem, _ := typ.Elem()
		entries := make(map[string]any, size)
		for i := range size {
			entry, ok := sample(elem, 2, number)
			if !ok {
				return nil, false
			}
			entries[fmt.Sprintf("k%d", i)] = entry
		}
		return entries, true
	}
	return nil, false
}

// count is how much a Go value holds: the items of its slices and maps, at
// any depth, and the characters of its strings and keys.
func count(value any) int {
	switch v := reflect.ValueOf(value); v.Kind() {
	case reflect.String:
		return utf8.RuneCountInString(v.String())
	case reflect.Slice:
		n := v.Len()
		for i := range v.Len() {
			n += count(v.Index(i).Interface())
		}
		return n
	case reflect.Map:
		n := v.Len()
		for entries := v.MapRange(); entries.Next(); {
			n += count(entries.Key().Interface()) + count(entries.Value().Interface())
		}
		return n
	}
	return 0
}
