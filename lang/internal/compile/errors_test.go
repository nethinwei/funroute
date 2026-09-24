package compile

import (
	"errors"
	"testing"

	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
)

func TestCompileAndContractErrorsAreTyped(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if _, err := CompileExpr(`if(`, registry, CompileOptions{}); !errors.Is(err, machine.ErrCompile) {
		t.Fatalf("CompileExpr(%q) error = %v, want ErrCompile", `if(`, err)
	}
	if _, err := CompileExpr(`x`, registry, CompileOptions{
		Args: []ArgSpec{{Name: "y", Type: machine.IntType}},
	}); !errors.Is(err, machine.ErrContract) || errors.Is(err, machine.ErrCompile) {
		t.Fatalf("CompileExpr(%q) with only y declared: error = %v, want ErrContract and not ErrCompile", `x`, err)
	}
	if err := ValidateContract(CompileOptions{
		Args: []ArgSpec{{Name: "x", Type: machine.IntType}, {Name: "x", Type: machine.IntType}},
	}); !errors.Is(err, machine.ErrContract) {
		t.Fatalf("ValidateContract(x declared twice) error = %v, want ErrContract", err)
	}
}

// A type error covers the expression it is about, not only the token its
// message points at: for 1 + "a" that is all of it, not just the "+".
func TestTypeErrorsCoverTheirExpression(t *testing.T) {
	t.Parallel()
	order := machine.RecordOf(machine.FieldOf("amount", machine.IntType))
	options := CompileOptions{Args: []ArgSpec{{Name: "order", Type: order}, {Name: "n", Type: machine.IntType}}}
	cases := map[string]string{
		`n + (1 + "a")`:     `(1 + "a")`,
		`n + nope(n)`:       `nope(n)`,
		`order.missing + n`: `order.missing`,
		`[n, "a"]`:          `[n, "a"]`,
		`if(n > 0, 1, "a")`: `if(n > 0, 1, "a")`,
	}
	for source, want := range cases {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			assertErrorCovers(t, source, options, want)
		})
	}
}

// Every kind of compile error carries where it is: the ones about enums,
// forms, switch branches and the declared result too, which once said
// "at byte N" in the message instead.
func TestEveryCompileErrorHasAPosition(t *testing.T) {
	t.Parallel()
	status := machine.EnumOf("status", "ok", "failed")
	cases := []struct {
		source  string
		options CompileOptions
		want    string
	}{
		{`1 + @foo`, CompileOptions{}, `@foo`},
		{`@nope.ok`, CompileOptions{Args: []ArgSpec{{Name: "s", Type: status}}}, `@nope.ok`},
		{`switch(1, case 1 => 2, else => 3)`, CompileOptions{}, `switch(1, case 1 => 2, else => 3)`},
		{`1 + 2`, CompileOptions{Result: &machine.StringType}, `1 + 2`},
	}
	for _, item := range cases {
		t.Run(item.source, func(t *testing.T) {
			t.Parallel()
			assertErrorCovers(t, item.source, item.options, item.want)
		})
	}
}

// assertErrorCovers compiles source against the kernel and checks that the
// error it fails with covers exactly want.
func assertErrorCovers(t *testing.T, source string, options CompileOptions, want string) {
	t.Helper()
	_, err := CompileExpr(source, machine.CoreRegistry(), options)
	var positioned *syntax.PosError
	if !errors.As(err, &positioned) {
		t.Fatalf("%q: %v has no position", source, err)
	}
	if start, end := positioned.Span(); source[start:end] != want {
		got := source[start:end]
		t.Errorf("%q: the error covers %q, want %q (%v)", source, got, want, err)
	}
}
