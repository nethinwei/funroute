package syntax

import (
	"strconv"
	"strings"
	"testing"
)

// Import applies the same rules as the parser, from the same definitions.
func TestImportEnforcesTheNodeDefinitions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ document, want string }{
		{`{"version":1,"expr":{"node":"for","source":{"node":"var","name":"xs"},"yield":{"node":"var","name":"x"}}}`, "for node is missing variable"},
		{`{"version":1,"expr":{"node":"let","bindings":[],"body":{"node":"int","int":1}}}`, "needs at least 1 item"},
		{`{"version":1,"expr":{"node":"var","name":"x","extra":1}}`, `unknown field "extra"`},
		{`{"version":1,"expr":{"node":"switch","cases":[{"match":[{"node":"bool","bool":true}],"result":{"node":"int","int":1}}],"default":{"node":"int","int":2},"value":null}}`, "null node"},
		{`{"version":1,"expr":{"node":"dict","entries":[{"key":"a","value":{"node":"int","int":1}},{"key":"a","value":{"node":"int","int":2}}]}}`, `duplicate dictionary key "a"`},
		{`{"version":1,"expr":{"node":"reduce","source":{"node":"var","name":"xs"},"variable":"x","accumulator":"x","init":{"node":"int","int":0},"body":{"node":"var","name":"x"}}}`, `"x" is bound twice`},
		{`{"version":1,"expr":{"node":"for","source":{"node":"var","name":"xs"},"variable":"in","yield":{"node":"var","name":"x"}}}`, `invalid local variable name "in"`},
		{`{"version":1,"expr":{"node":"float","float":"nan"}}`, `"nan" is not a decimal`},
		{`{"version":1,"expr":{"node":"float","float":"1e400"}}`, "out of float64's range"},
		// A reserved word would print as syntax and read back as something else.
		{`{"version":1,"expr":{"node":"var","name":"case"}}`, `invalid variable name "case"`},
		{`{"version":1,"expr":{"node":"call","name":"let","args":[]}}`, `invalid function name "let"`},
		{`{"version":1,"expr":{"node":"loop"}}`, `unknown expression node "loop"`},
		// The document's own keys are as exact as a node's.
		{`{"VERSION":1,"expr":{"node":"var","name":"x"}}`, `unknown field "VERSION"`},
		{`{"version":1,"eXpr":{"node":"var","name":"x"}}`, `unknown field "eXpr"`},
		{`{"version":"1","expr":{"node":"var","name":"x"}}`, "version must be an integer"},
		// A value slot takes its own kind of value, never null: null is no 0,
		// no false, no empty name and no empty list.
		{`{"version":1,"expr":{"node":"int","int":null}}`, "malformed int"},
		{`{"version":1,"expr":{"node":"int","int":1.5}}`, "malformed int"},
		{`{"version":1,"expr":{"node":"int","int":"1"}}`, "malformed int"},
		{`{"version":1,"expr":{"node":"bool","bool":null}}`, "malformed bool"},
		{`{"version":1,"expr":{"node":"var","name":null}}`, "must be a string"},
		{`{"version":1,"expr":{"node":"call","name":"f","args":null}}`, "must be a list"},
		{`{"version":1,"expr":null}`, "null node"},
	} {
		t.Run(test.want, func(t *testing.T) {
			t.Parallel()
			_, err := ImportExprJSON([]byte(test.document))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("%s\nerror = %v, want %q", test.document, err, test.want)
			}
		})
	}
}

// FuzzImportExprJSON holds the importer to its rules on any bytes: it never
// panics, and what it accepts exports to a document that imports back to the
// same program — and prints as source that parses back to it too.
func FuzzImportExprJSON(f *testing.F) {
	for _, source := range fuzzSources(f) {
		if expr, err := Parse(source); err == nil {
			f.Add([]byte(mustExport(f, expr)))
		}
	}
	for _, document := range []string{
		``, `{}`, `null`, `{"version":1}`, `{"version":2,"expr":{"node":"int","int":1}}`,
		`{"version":1,"expr":{"node":"float","float":"nan"}}`,
		`{"version":1,"expr":{"node":"var","name":"case"}}`,
		`{"version":1,"expr":{"node":"record_update","base":{"node":"var","name":"r"},"fields":[]}}`,
		`{"version":1,"expr":{"node":"money","currency":"USD","amount":"-0.5"}}`,
		`{"version":1,"expr":{"node":"money","currency":"usd","amount":"1"}}`,
		`{"version":1,"expr":{"node":"ratio","value":"-2.9","unit":"%"}}`,
		`{"version":1,"expr":{"node":"ratio","value":"2.9","unit":"pct"}}`,
		`{"version":1,"expr":{"node":"field","value":{"node":"ratio","value":"25","unit":"bps"},"field":"x"}}`,
		`{"version":1,"expr":{"node":"fxrate","rate":".5","quote":"JPY","base":"USD"}}`,
		`{"version":1,"expr":{"node":"fxrate","rate":"1.","quote":"JPY","base":"USD"}}`,
		`{"version":1,"expr":{"node":"fxrate","rate":"150","quote":"jpy","base":"USD"}}`,
		`{"version":1,"expr":{"node":"currency","code":"U"}}`,
		`{"version":1,"expr":{"node":"using","quotes":[],"body":{"node":"var","name":"a"}}}`,
		`{"version":1,"expr":{"node":"using","outer":true,"quotes":[{"node":"fxrate","rate":"150","quote":"JPY","base":"USD"}],"body":{"node":"var","name":"a"}}}`,
	} {
		f.Add([]byte(document))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		expr, err := ImportExprJSON(data)
		if err != nil {
			return
		}
		want := exportFuzzed(t, string(data), expr)
		again, err := ImportExprJSON([]byte(want))
		if err != nil {
			t.Fatalf("ImportExprJSON(ExportExprJSON(ImportExprJSON(%q))) error = %v, want nil", data, err)
		}
		if got := exportFuzzed(t, want, again); got != want {
			t.Fatalf("ImportExprJSON(%q) exports to %s after another round trip, want %s", data, got, want)
		}
		checkReparses(t, "Inline", string(data), Inline(expr), want)
	})
}

func mustExport(t testing.TB, expr Expr) string {
	t.Helper()
	encoded, err := ExportExprJSON(expr)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestSwitchSurvivesExprJSONRoundTrip(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`switch(country, case "MY", "TH" => "asia", case "SG" => "sg", else => "global")`,
		`switch(case amount > 100 => "big", case risk > 0.5, country == "SG" => "check", else => "ok")`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			assertExprJSONIsCanonical(t, source)
		})
	}
}

func TestDictionaryWalkSurvivesExprJSONRoundTrip(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`[add(k, string(v)) for k, v in weights if v > 1.0]`,
		`reduce(k, v in weights, total = 0.0, total + v)`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if first := assertExprJSONIsCanonical(t, source); !strings.Contains(string(first), `"key_variable":"k"`) {
				t.Fatalf("the key variable is missing from %s", first)
			}
		})
	}
}

func TestLetSurvivesExprJSONRoundTrip(t *testing.T) {
	t.Parallel()
	source := `let(base = amount * 2, fee = base / 10, base + fee)`
	expr, err := Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	first, err := ExportExprJSON(expr)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := ImportExprJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExportExprJSON(imported)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("%s is not canonical:\n%s\n%s", source, first, second)
	}
}

// assertExprJSONIsCanonical parses source, and checks that exporting,
// importing and exporting again gives the same ExprJSON. It returns it.
func assertExprJSONIsCanonical(t *testing.T, source string) []byte {
	t.Helper()
	expr, err := Parse(source)
	if err != nil {
		t.Fatalf("parse %s: %v", source, err)
	}
	first, err := ExportExprJSON(expr)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := ImportExprJSON(first)
	if err != nil {
		t.Fatalf("import %s: %v", source, err)
	}
	second, err := ExportExprJSON(imported)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("%s is not canonical:\n%s\n%s", source, first, second)
	}
	return first
}

// A money or ratio node imported from ExprJSON meets the rules the parser's
// own literals meet: a code's shape, a plain unsigned decimal (an amount may
// carry one minus), and a unit that is % or bps.
func TestImportChecksMoneyAndRatioNodes(t *testing.T) {
	t.Parallel()
	money := func(currency, amount string) string {
		return `{"version":1,"expr":{"node":"money","currency":` + currency + `,"amount":` + amount + `}}`
	}
	ratio := func(value, unit string) string {
		return `{"version":1,"expr":{"node":"ratio","value":` + value + `,"unit":` + unit + `}}`
	}
	for name, test := range map[string]struct{ document, want string }{
		"lower-case code":         {money(`"usd"`, `"1"`), `invalid currency code "usd"`},
		"two-letter code":         {money(`"US"`, `"1"`), `invalid currency code "US"`},
		"nine-letter code":        {money(`"ABCDEFGHI"`, `"1"`), `invalid currency code "ABCDEFGHI"`},
		"code with a digit first": {money(`"1SD"`, `"1"`), `invalid currency code "1SD"`},
		"empty code":              {money(`""`, `"1"`), `invalid currency code ""`},
		"exponent":                {money(`"USD"`, `"1e3"`), `invalid amount "1e3"`},
		"two points":              {money(`"USD"`, `"1.2.3"`), `invalid amount "1.2.3"`},
		"empty amount":            {money(`"USD"`, `""`), `invalid amount ""`},
		"bare minus":              {money(`"USD"`, `"-"`), `invalid amount "-"`},
		"two minuses":             {money(`"USD"`, `"--1"`), `invalid amount "--1"`},
		"negative zero":           {money(`"USD"`, `"-0.00"`), `no negative zero`},
		"plus":                    {money(`"USD"`, `"+1"`), `invalid amount "+1"`},
		"trailing point":          {money(`"USD"`, `"1."`), `invalid amount "1."`},
		"leading point":           {money(`"USD"`, `".5"`), `invalid amount ".5"`},
		"separator":               {money(`"USD"`, `"1_000"`), `invalid amount "1_000"`},
		"space":                   {money(`"USD"`, `" 1"`), `invalid amount " 1"`},
		"amount as a number":      {money(`"USD"`, `1.7`), `money amount: must be a string`},
		"missing amount":          {`{"version":1,"expr":{"node":"money","currency":"USD"}}`, `money node is missing amount`},
		"unit pct":                {ratio(`"2.9"`, `"pct"`), `invalid ratio unit "pct"`},
		"empty unit":              {ratio(`"2.9"`, `""`), `invalid ratio unit ""`},
		"upper-case unit":         {ratio(`"2.9"`, `"BPS"`), `invalid ratio unit "BPS"`},
		"negative rate":           {ratio(`"-2.9"`, `"%"`), `invalid ratio "-2.9"`},
		"empty rate":              {ratio(`""`, `"%"`), `invalid ratio ""`},
		"rate with two points":    {ratio(`"1.2.3"`, `"bps"`), `invalid ratio "1.2.3"`},
		"rate exponent":           {ratio(`"1e3"`, `"%"`), `invalid ratio "1e3"`},
		"unknown field":           {`{"version":1,"expr":{"node":"ratio","value":"1","unit":"%","scale":2}}`, `unknown field "scale"`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ImportExprJSON([]byte(test.document))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("ImportExprJSON(%s) error = %v, want %q", test.document, err, test.want)
			}
		})
	}
}

// What the checks accept imports, exports unchanged and prints as source
// that reads back to it — including amounts the parser never writes itself.
func TestImportedMoneyAndRatiosPrintBack(t *testing.T) {
	t.Parallel()
	for document, source := range map[string]string{
		`{"node":"money","currency":"USD","amount":"-0.5"}`:                                                     `USD -0.5`,
		`{"node":"money","currency":"JPY","amount":"007"}`:                                                      `JPY 007`,
		`{"node":"money","currency":"ABCDEFGH","amount":"1"}`:                                                   `ABCDEFGH 1`,
		`{"node":"ratio","value":"007","unit":"%"}`:                                                             `007%`,
		`{"node":"ratio","value":"0.25","unit":"bps"}`:                                                          `0.25bps`,
		`{"node":"field","value":{"node":"money","currency":"USD","amount":"-1"},"field":"x"}`:                  `(USD -1).x`,
		`{"node":"call","name":"at","args":[{"node":"ratio","value":"1","unit":"bps"},{"node":"int","int":0}]}`: `(1bps)[0]`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			wrapped := `{"version":1,"expr":` + document + `}`
			expr, err := ImportExprJSON([]byte(wrapped))
			if err != nil {
				t.Fatalf("ImportExprJSON(%s) error = %v, want nil", wrapped, err)
			}
			if got := mustExport(t, expr); got != wrapped {
				t.Errorf("ImportExprJSON(%s) exports as %s, want it unchanged", wrapped, got)
			}
			if got := Inline(expr); got != source {
				t.Errorf("Inline(ImportExprJSON(%s)) = %q, want %q", wrapped, got, source)
			}
			checkReparses(t, "Inline", wrapped, Inline(expr), wrapped)
		})
	}
}

func TestMoneySurvivesExprJSONRoundTrip(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`let(fee = amount * 2.9% + USD 0.30, cap = USD -25.00, if(fee > cap, cap, fee))`,
		`[x * 0.5bps + JPY 1_000 for x in xs if x > 1%]`,
		`{a: (USD 1).b, r: (25bps)[0]}`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			assertExprJSONIsCanonical(t, source)
		})
	}
}

// A duration is its literal's text in ExprJSON, and reads back as the same
// length; text that is no duration is refused.
func TestDurationsSurviveExprJSONRoundTrip(t *testing.T) {
	t.Parallel()
	for _, source := range []string{`t + 2h30m - 1s500ms`, `[-90s, 0s, 1ns]`} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			assertExprJSONIsCanonical(t, source)
		})
	}
	if _, err := ImportExprJSON([]byte(`{"version":1,"expr":{"node":"duration","duration":"soon"}}`)); err == nil {
		t.Fatal("a duration of text that is none was imported")
	}
}

// An exchange rate literal's figure is read as the parser reads one — digits
// on both sides of a point — so a document the importer takes prints back
// as source that parses: ".5" and "1." are no figures.
func TestAnExchangeRateFigureIsAPlainDecimal(t *testing.T) {
	t.Parallel()
	for _, figure := range []string{".5", "1.", "1.2.3", "", "-1", "1e3", "1_000"} {
		document := `{"version":1,"expr":{"node":"fxrate","rate":"` + figure + `","quote":"JPY","base":"USD"}}`
		if _, err := ImportExprJSON([]byte(document)); err == nil {
			t.Errorf("ImportExprJSON(fxrate %q) = nil error, want the figure refused", figure)
		}
	}
	expr, err := ImportExprJSON([]byte(`{"version":1,"expr":{"node":"fxrate","rate":"150.25","quote":"JPY","base":"USD"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := Inline(expr); got != "150.25 JPY / USD" {
		t.Fatalf("Inline(fxrate 150.25) = %q, want 150.25 JPY / USD", got)
	}
}

// BenchmarkImportDeepExprJSON imports documents of growing depth whose size
// is mostly one leaf: the time follows the size, not depth × size, because
// the document is decoded once and no node reads the bytes under it again.
func BenchmarkImportDeepExprJSON(b *testing.B) {
	leaf := `{"node":"string","string":"` + strings.Repeat("x", 20_000) + `"}`
	for _, depth := range []int{100, 400, 800} {
		document := []byte(`{"version":1,"expr":` + strings.Repeat(`{"node":"call","name":"f","args":[`, depth) +
			leaf + strings.Repeat(`]}`, depth) + `}`)
		b.Run(strconv.Itoa(depth), func(b *testing.B) {
			for b.Loop() {
				mustImport(b, document)
			}
		})
	}
}

func mustImport(b *testing.B, document []byte) {
	b.Helper()
	if _, err := ImportExprJSON(document); err != nil {
		b.Fatal(err)
	}
}

// ExprJSON nests no deeper than source does.
func TestImportedNestingIsBounded(t *testing.T) {
	t.Parallel()
	nested := func(depth int) string {
		return `{"version":1,"expr":` + strings.Repeat(`{"node":"call","name":"f","args":[`, depth-1) +
			`{"node":"int","int":1}` + strings.Repeat(`]}`, depth-1) + `}`
	}
	if _, err := ImportExprJSON([]byte(nested(maxNesting))); err != nil {
		t.Fatalf("ImportExprJSON(%d levels) error = %v, want it read", maxNesting, err)
	}
	// The error says it once, not once for every level above it.
	if _, err := ImportExprJSON([]byte(nested(maxNesting + 1))); err == nil || err.Error() != "expression JSON nests deeper than 1000 levels" {
		t.Fatalf("ImportExprJSON(%d levels) error = %v, want a nesting error", maxNesting+1, err)
	}
}

// A tree as deep as the limit goes both ways: source that deep has ExprJSON
// that reads back, and ExprJSON that deep formats as source that parses —
// even where every level needs parentheses.
func TestTheNestingLimitIsTheSameBothWays(t *testing.T) {
	t.Parallel()
	additions := "x" + strings.Repeat(" + x", maxNesting-1)
	expr, err := Parse(additions)
	if err != nil {
		t.Fatalf("Parse(%d levels of +) error = %v", maxNesting, err)
	}
	document, err := ExportExprJSON(expr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ImportExprJSON(document); err != nil {
		t.Fatalf("ImportExprJSON(the ExprJSON of %d levels of +) error = %v, want it read", maxNesting, err)
	}
	subtractions := strings.Repeat("x - (", maxNesting-2) + "x - x" + strings.Repeat(")", maxNesting-2)
	for _, source := range []string{additions, subtractions} {
		expr, err := Parse(source)
		if err != nil {
			t.Fatalf("Parse(%.20s…) error = %v", source, err)
		}
		if _, err := Parse(Format(expr)); err != nil {
			t.Fatalf("Parse(Format(%.20s…)) error = %v, want the formatted text read", source, err)
		}
	}
}

// Of several unknown fields, the first in key order is the one named, so
// the message is the same every time.
func TestTheUnknownFieldNamedIsTheFirstByKey(t *testing.T) {
	t.Parallel()
	for range 20 {
		_, err := ImportExprJSON([]byte(`{"version":1,"expr":{"node":"int","int":1,"zz":1,"mm":2,"aa":3}}`))
		if err == nil || !strings.Contains(err.Error(), `unknown field "aa"`) {
			t.Fatalf("import = %v, want the unknown field \"aa\"", err)
		}
	}
}
