package syntax

import (
	"strings"
	"testing"
	"time"
)

// A string literal holds text, and text is UTF-8: a byte that is not would be
// replaced on the way through ExprJSON, and the program would not come back.
func TestStringsAreUTF8(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"\"\\200\"", "\"\x80\"", "concat(\"a\", \"\xff\")"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(source); err == nil || !strings.Contains(err.Error(), "not valid UTF-8") {
				t.Errorf("Parse(%q) error = %v, want a UTF-8 error", source, err)
			}
		})
	}
	if _, err := Parse(`"支付 ✓"`); err != nil {
		t.Errorf("Parse of UTF-8 text error = %v, want none", err)
	}
}

// Whitespace is ASCII whitespace. A lone byte is not a rune, so 0x85 or 0xA0
// on its own is a character the lexer cannot read, not a space.
func TestOnlyASCIIWhitespaceSeparates(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"a \x85 b", "a\xa0+ b"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(source); err == nil || !strings.Contains(err.Error(), "unexpected") {
				t.Errorf("Parse(%q) error = %v, want an unexpected character", source, err)
			}
		})
	}
	if _, err := Parse("a +\t\r\n\v\fb"); err != nil {
		t.Errorf("Parse with ASCII whitespace error = %v, want none", err)
	}
}

// A % against a number is a ratio, always: 10%-3 is 10% - 3. A remainder has
// a space before its %, or something other than a number in front of it.
func TestAPercentAgainstANumberIsAlwaysARatio(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"10 % 3":                 `{"node":"call","name":"mod"`,
		"x%3":                    `{"node":"call","name":"mod"`,
		"(10)%3":                 `{"node":"call","name":"mod"`,
		"10 %3":                  `{"node":"call","name":"mod"`,
		"2.9%":                   `{"node":"ratio","value":"2.9","unit":"%"}`,
		"2.9% - fee":             `{"node":"call","name":"sub","args":[{"node":"ratio"`,
		"[x * 2.9% for x in xs]": `{"node":"for"`,
		"25bps":                  `{"node":"ratio","value":"25","unit":"bps"}`,
		"7%-2":                   `{"node":"call","name":"sub","args":[{"node":"ratio","value":"7","unit":"%"},{"node":"int","int":2}]}`,
		"10%-3":                  `{"node":"call","name":"sub","args":[{"node":"ratio","value":"10","unit":"%"},{"node":"int","int":3}]}`,
		"10% -3":                 `{"node":"call","name":"sub","args":[{"node":"ratio","value":"10","unit":"%"},{"node":"int","int":3}]}`,
		"10%+3":                  `{"node":"call","name":"add","args":[{"node":"ratio","value":"10","unit":"%"},{"node":"int","int":3}]}`,
		"USD 1_000.50":           `{"node":"money","currency":"USD","amount":"1000.50"}`,
		"USD -1":                 `{"node":"money","currency":"USD","amount":"-1"}`,
		"-USD 1":                 `{"node":"call","name":"sub","args":[{"node":"int","int":0},{"node":"money","currency":"USD","amount":"1"}]}`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if got := mustExport(t, mustParse(t, source)); !strings.Contains(got, `"expr":`+want) {
				t.Fatalf("Parse(%q) = %s, want it to begin %s", source, got, want)
			}
		})
	}
	// A ratio written against an operand is an error that says why, never a
	// remainder read another way.
	for _, source := range []string{"10%3", "10%(x)", "10%x", `10%"a"`, "10%@x", "2.9%!x", "2.9%bps", "10%{a: 1}", "1%2.5", "1%2%"} {
		if _, err := Parse(source); err == nil || !strings.Contains(err.Error(), "is a ratio") {
			t.Fatalf("Parse(%q) error = %v, want one saying the %% makes a ratio", source, err)
		}
	}
	for _, source := range []string{"1e3%", "USD 1e3", "25bpsx", "USD // x\n1", "USD -0", "USD -0.00"} {
		if _, err := Parse(source); err == nil {
			t.Fatalf("Parse(%q) succeeded, want an error", source)
		}
	}
}

// exprOf is source's program as ExprJSON without the document around it.
func exprOf(t *testing.T, source string) string {
	t.Helper()
	document := mustExport(t, mustParse(t, source))
	return strings.TrimSuffix(strings.TrimPrefix(document, `{"version":1,"expr":`), "}")
}

// An amount keeps the decimal it was written as, without its separators. Its
// sign is written on the figure, against the digits: USD -1.70. A minus in
// front of the code is the negation of an amount, like any operand's.
func TestMoneyLiteralsKeepTheirDecimalText(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"USD 1.70":    `{"node":"money","currency":"USD","amount":"1.70"}`,
		"USD -1.70":   `{"node":"money","currency":"USD","amount":"-1.70"}`,
		"USD\t-0.05":  `{"node":"money","currency":"USD","amount":"-0.05"}`,
		"-USD 1.70":   `{"node":"call","name":"sub","args":[{"node":"int","int":0},{"node":"money","currency":"USD","amount":"1.70"}]}`,
		"JPY 1_000":   `{"node":"money","currency":"JPY","amount":"1000"}`,
		"USD 0":       `{"node":"money","currency":"USD","amount":"0"}`,
		"USD 01.70":   `{"node":"money","currency":"USD","amount":"01.70"}`,
		"KWD 0.001":   `{"node":"money","currency":"KWD","amount":"0.001"}`,
		"USD \t 1.70": `{"node":"money","currency":"USD","amount":"1.70"}`,
		// Three to eight capitals or digits after a capital are a code, even
		// one no registry declares: that is the compiler's to say.
		"ABCDEFGH 1": `{"node":"money","currency":"ABCDEFGH","amount":"1"}`,
		"U2D 1":      `{"node":"money","currency":"U2D","amount":"1"}`,
		"USD1 1":     `{"node":"money","currency":"USD1","amount":"1"}`,
		"USDT 0.5":   `{"node":"money","currency":"USDT","amount":"0.5"}`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if got := exprOf(t, source); got != want {
				t.Errorf("Parse(%q) = %s, want %s", source, got, want)
			}
		})
	}
}

// A name that is not a code's shape stays a name, and a name followed by a
// number is then two expressions side by side: an error, not an amount.
func TestOnlyACodeShapeBeforeANumberIsMoney(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"US 1", "ABCDEFGHI 1", "usd 1", "Usd 1", "uSD 1", "_USD 1", "X1 1", "1USD 1", "order.USD 1"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if expr, err := Parse(source); err == nil {
				t.Errorf("Parse(%q) = %s, want an error", source, mustExport(t, expr))
			}
		})
	}
}

// A code with no number after it is a currency: currencies are the
// language's own, so no variable is shaped like a code. Called, it is a
// function's name.
func TestACodeWithoutAnAmountIsACurrency(t *testing.T) {
	t.Parallel()
	usd := `{"node":"currency","code":"USD"}`
	for source, want := range map[string]string{
		"USD":      usd,
		"USD + 1":  `{"node":"call","name":"add","args":[` + usd + `,{"node":"int","int":1}]}`,
		"USD - 1":  `{"node":"call","name":"sub","args":[` + usd + `,{"node":"int","int":1}]}`,
		"USD-1":    `{"node":"call","name":"sub","args":[` + usd + `,{"node":"int","int":1}]}`,
		"USD -x":   `{"node":"call","name":"sub","args":[` + usd + `,{"node":"var","name":"x"}]}`,
		"f(USD)":   `{"node":"call","name":"f","args":[` + usd + `]}`,
		"[USD, 1]": `{"node":"array","items":[` + usd + `,{"node":"int","int":1}]}`,
		"USD(1)":   `{"node":"call","name":"USD","args":[{"node":"int","int":1}]}`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if got := exprOf(t, source); got != want {
				t.Errorf("Parse(%q) = %s, want %s", source, got, want)
			}
		})
	}
	for _, source := range []string{"USD.x", "let(USD = 1, USD)", "[x for USD in xs]"} {
		if _, err := Parse(source); err == nil {
			t.Errorf("Parse(%q) = nil error, want a currency refused as a name", source)
		}
	}
}

// An amount is a plain decimal: an exponent is refused with a message that
// says so, and a number that does not lex is refused as it always was.
func TestAnAmountIsAPlainDecimal(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"USD 1e3":   "not with an exponent",
		"USD -1e3":  "not with an exponent",
		"USD 1E-2":  "not with an exponent",
		"USD 1.5e2": "not with an exponent",
		"USD 1.":    "digits after '.'",
		"USD 1.70%": "unexpected token",
		"USD 25bps": "unexpected token",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(source); err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("Parse(%q) error = %v, want one containing %q", source, err, want)
			}
		})
	}
}

// A ratio is a ratio wherever it stands and whatever follows it.
func TestWhatFollowsAPercentDecidesItsMeaning(t *testing.T) {
	t.Parallel()
	rate := `{"node":"ratio","value":"2.9","unit":"%"}`
	for source, want := range map[string]string{
		"0.5bps":                               `{"node":"ratio","value":"0.5","unit":"bps"}`,
		"1_0%":                                 `{"node":"ratio","value":"10","unit":"%"}`,
		"f(2.9%)":                              `{"node":"call","name":"f","args":[` + rate + `]}`,
		"[2.9%, 1]":                            `{"node":"array","items":[` + rate + `,{"node":"int","int":1}]}`,
		"2.9% //注释":                            rate,
		"2.9%\n- fee":                          `{"node":"call","name":"sub","args":[` + rate + `,{"node":"var","name":"fee"}]}`,
		"2.9% in xs":                           `{"node":"call","name":"member","args":[` + rate + `,{"node":"var","name":"xs"}]}`,
		"2.9% == r":                            `{"node":"call","name":"eq","args":[` + rate + `,{"node":"var","name":"r"}]}`,
		"2.9% * x":                             `{"node":"call","name":"mul","args":[` + rate + `,{"node":"var","name":"x"}]}`,
		"2.9%.x":                               `{"node":"field","value":` + rate + `,"field":"x"}`,
		"1%%2":                                 `{"node":"call","name":"mod","args":[{"node":"ratio","value":"1","unit":"%"},{"node":"int","int":2}]}`,
		"7% - 2":                               `{"node":"call","name":"sub","args":[{"node":"ratio","value":"7","unit":"%"},{"node":"int","int":2}]}`,
		"10%[1]":                               `{"node":"call","name":"at","args":[{"node":"ratio","value":"10","unit":"%"},{"node":"int","int":1}]}`,
		"2.9%-fee":                             `{"node":"call","name":"sub","args":[` + rate + `,{"node":"var","name":"fee"}]}`,
		"2.9%!=r":                              `{"node":"call","name":"if","args":[{"node":"call","name":"eq","args":[` + rate + `,{"node":"var","name":"r"}]},{"node":"bool","bool":false},{"node":"bool","bool":true}]}`,
		"[x*2.9% for x in xs if x>0]":          `{"node":"for","source":{"node":"var","name":"xs"},"variable":"x","where":{"node":"call","name":"gt","args":[{"node":"var","name":"x"},{"node":"int","int":0}]},"yield":{"node":"call","name":"mul","args":[{"node":"var","name":"x"},` + rate + `]}}`,
		"switch(x, case 2.9% => 1, else => 2)": `{"node":"switch","value":{"node":"var","name":"x"},"cases":[{"match":[` + rate + `],"result":{"node":"int","int":1}}],"default":{"node":"int","int":2}}`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if got := exprOf(t, source); got != want {
				t.Errorf("Parse(%q) = %s, want %s", source, got, want)
			}
		})
	}
}

// A ratio's unit touches its number and ends where a name would.
func TestARatioUnitIsWrittenAgainstItsNumber(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"1e3bps", "1E3%", "25bps%", "2.9 %", "2.9%%", "25 bps", "0x1%"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if expr, err := Parse(source); err == nil {
				t.Errorf("Parse(%q) = %s, want an error", source, mustExport(t, expr))
			}
		})
	}
}

// `!=` does not start an operand, so a ratio before it is still a ratio: the
// roadmap's rule is "no operand follows", and the printer writes
// if(eq(2.9%, r), false, true) as 2.9% != r.
func TestARatioBeforeNotEqualIsARatio(t *testing.T) {
	t.Parallel()
	want := `{"node":"call","name":"if","args":[{"node":"call","name":"eq","args":[{"node":"ratio","value":"2.9","unit":"%"},{"node":"var","name":"r"}]},{"node":"bool","bool":false},{"node":"bool","bool":true}]}`
	if got := exprOf(t, "2.9% != r"); got != want {
		t.Errorf("Parse(%q) = %s, want %s", "2.9% != r", got, want)
	}
}

// There is no unary plus, so a + against what follows cannot start an
// operand; 2.9%+1 can only be a ratio plus one.
func TestARatioBeforeATouchingPlusIsARatio(t *testing.T) {
	t.Parallel()
	want := `{"node":"call","name":"add","args":[{"node":"ratio","value":"2.9","unit":"%"},{"node":"int","int":1}]}`
	if got := exprOf(t, "2.9%+1"); got != want {
		t.Errorf("Parse(%q) = %s, want %s", "2.9%+1", got, want)
	}
}

// What stops the lexer is named as it is: the character when the bytes are
// one, the byte when they are not UTF-8 — never a byte printed as though it
// were a character.
func TestTheLexerNamesWhatStoppedIt(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"x\u00a0+ 1": `unexpected '\u00a0'`,
		"x 好 1":      `unexpected '好'`,
		"x \x85 1":   "unexpected byte 0x85",
		"x # 1":      `unexpected '#'`,
		"1e3%3":      "1e3% is a ratio with an exponent",
	} {
		if _, err := Parse(source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) error = %v, want one saying %s", source, err, want)
		}
	}
}

// A duration is numbers each against its unit, the longest unit that fits
// first, so 1ms is a millisecond; a word after it, or a number without a
// unit, is no duration.
func TestADurationIsNumbersWithTheirUnits(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]time.Duration{
		"90s": 90 * time.Second, "2h30m": 150 * time.Minute, "1ms": time.Millisecond, "1m5s": 65 * time.Second,
		"1_500ms": 1500 * time.Millisecond, "-2h": -2 * time.Hour, "3us": 3 * time.Microsecond, "7ns": 7,
	} {
		expr, err := Parse(source)
		literal, ok := expr.(*LiteralExpr)
		if got, _ := literal.Value.Duration(); err != nil || !ok || got != want {
			t.Errorf("Parse(%q) = %v, %v, want %v", source, expr, err, want)
		}
	}
	for _, source := range []string{"5sec", "2h30", "1h.x", "3000000h"} {
		if _, err := Parse(source); err == nil {
			t.Errorf("Parse(%q) succeeded, want a syntax error", source)
		}
	}
}
