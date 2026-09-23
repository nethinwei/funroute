package compile

import (
	"errors"
	"testing"

	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
)

// A type error covers the expression it is about, not only the token its
// message points at: for 1 + "a" that is all of it, not just the "+".
func TestTypeErrorsCoverTheirExpression(t *testing.T) {
	order := machine.RecordOf(machine.Field{Name: "amount", Type: machine.IntType})
	options := CompileOptions{Args: []ArgSpec{{Name: "order", Type: order}, {Name: "n", Type: machine.IntType}}}
	cases := map[string]string{
		`n + (1 + "a")`:     `(1 + "a")`,
		`n + nope(n)`:       `nope(n)`,
		`order.missing + n`: `order.missing`,
		`[n, "a"]`:          `[n, "a"]`,
		`if(n > 0, 1, "a")`: `if(n > 0, 1, "a")`,
	}
	for source, want := range cases {
		_, err := CompileExpr(source, machine.CoreRegistry(), options)
		var positioned *syntax.PosError
		if !errors.As(err, &positioned) {
			t.Fatalf("%q: %v has no position", source, err)
		}
		if got := source[positioned.Start:positioned.End]; got != want {
			t.Errorf("%q: the error covers %q, want %q (%v)", source, got, want, err)
		}
	}
}

// Every kind of compile error carries where it is: the ones about enums,
// forms, switch branches and the declared result too, which once said
// "at byte N" in the message instead.
func TestEveryCompileErrorHasAPosition(t *testing.T) {
	status := machine.EnumOf("status", "ok", "failed")
	cases := []struct {
		source  string
		options CompileOptions
		want    string
	}{
		{`1 + @foo`, CompileOptions{}, `@foo`},
		{`@nope.ok`, CompileOptions{Args: []ArgSpec{{Name: "s", Type: status}}}, `@nope.ok`},
		{`switch(1, case 1 => 2, else 3)`, CompileOptions{}, `switch(1, case 1 => 2, else 3)`},
		{`1 + 2`, CompileOptions{Result: &machine.StringType}, `1 + 2`},
	}
	for _, item := range cases {
		_, err := CompileExpr(item.source, machine.CoreRegistry(), item.options)
		var positioned *syntax.PosError
		if !errors.As(err, &positioned) {
			t.Fatalf("%q: %v has no position", item.source, err)
		}
		if got := item.source[positioned.Start:positioned.End]; got != item.want {
			t.Errorf("%q: the error covers %q, want %q (%v)", item.source, got, item.want, err)
		}
	}
}
