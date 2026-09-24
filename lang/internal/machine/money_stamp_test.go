package machine

import (
	"slices"
	"strings"
	"testing"
)

// Any money-kind type an artifact states makes it an artifact that uses
// money, however deep in a container or record it is.
func TestAnArtifactUsesMoneyWhereverItStatesIt(t *testing.T) {
	t.Parallel()
	rate, minor, code := int64(5), int64(170), "USD"
	money := Constant{Type: MoneyKind, Int: &minor, String: &code}
	arrayOfMoney := ArrayOf(MoneyOf(""))
	order := RecordOf(Field{name: "fee", typ: RateType})
	for name, test := range map[string]struct {
		artifact Artifact
		uses     bool
	}{
		"nothing but ints":        {Artifact{parts: ArtifactParts{Args: []Parameter{{name: "n", typ: IntType}}, Result: IntType}}, false},
		"a money argument":        {Artifact{parts: ArtifactParts{Args: []Parameter{{name: "m", typ: MoneyOf("c")}}, Result: IntType}}, true},
		"a rate in an array":      {Artifact{parts: ArtifactParts{Args: []Parameter{{name: "r", typ: ArrayOf(RateType)}}, Result: IntType}}, true},
		"a rate in a record":      {Artifact{parts: ArtifactParts{Args: []Parameter{{name: "o", typ: order}}, Result: IntType}}, true},
		"money in a dict result":  {Artifact{parts: ArtifactParts{Result: DictOf(MoneyOf("USD"))}}, true},
		"a currency result":       {Artifact{parts: ArtifactParts{Result: CurrencyOf("")}}, true},
		"an instruction's type":   {Artifact{parts: ArtifactParts{Result: IntType, Instructions: []Instruction{{Op: OpMakeArray, Type: &arrayOfMoney}}}}, true},
		"an instruction's ints":   {Artifact{parts: ArtifactParts{Result: IntType, Instructions: []Instruction{{Op: OpMakeArray, Type: new(ArrayOf(IntType))}}}}, false},
		"a rate constant":         {Artifact{parts: ArtifactParts{Result: IntType, Constants: []Constant{{Type: RateKind, Int: &rate}}}}, true},
		"money deep in constants": {Artifact{parts: ArtifactParts{Result: IntType, Constants: []Constant{{Type: ArrayKind, Items: []Constant{{Type: RecordKind, Items: []Constant{money}}}}}}}, true},
		"an empty money array":    {Artifact{parts: ArtifactParts{Result: IntType, Constants: []Constant{{Type: ArrayKind, Elem: &arrayOfMoney}}}}, true},
		"an int constant":         {Artifact{parts: ArtifactParts{Result: IntType, Constants: []Constant{{Type: IntKind, Int: &rate}}}}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := ArtifactUsesMoney(&test.artifact); got != test.uses {
				t.Fatalf("ArtifactUsesMoney(%+v) = %v, want %v", test.artifact, got, test.uses)
			}
		})
	}
}

// An artifact is held to the money facts it was compiled on: the default
// rounding, and the places of the currencies it names. A registry that
// declares more currencies, or changes one the artifact does not name, still
// loads it; one that changed what it names does not.
func TestTheMoneyStampIsCheckedOnLoad(t *testing.T) {
	t.Parallel()
	usd, eur := CurrencySpec{Code: "USD", Digits: 2}, CurrencySpec{Code: "EUR", Digits: 2}
	registry := declared(t, RoundHalfUp, usd)
	usdStamp := &MoneyStamp{Rounding: RoundHalfUp, Currencies: []CurrencySpec{usd}}
	plain := Artifact{parts: ArtifactParts{Result: IntType}}
	money := Artifact{parts: ArtifactParts{Result: MoneyOf("USD")}}
	for name, test := range map[string]struct {
		artifact Artifact
		stamp    *MoneyStamp
		registry *Registry
		want     string
	}{
		"no money and no stamp":            {plain, nil, registry, ""},
		"no money into no money":           {plain, nil, CoreRegistry(), ""},
		"money without a stamp":            {money, nil, registry, "records no money stamp"},
		"a stamp into a registry without":  {money, usdStamp, CoreRegistry(), "declares none"},
		"other places for a named code":    {money, usdStamp, declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 3}), "decimal places"},
		"a named code no longer declared":  {money, usdStamp, declared(t, RoundHalfUp, eur), "does not declare"},
		"another default rounding":         {money, usdStamp, declared(t, RoundDown, usd), "rounds"},
		"a registry declaring more":        {money, usdStamp, declared(t, RoundHalfUp, usd, eur), ""},
		"the registry's own facts":         {money, usdStamp, registry, ""},
		"no code named, another table":     {money, &MoneyStamp{Rounding: RoundHalfUp}, declared(t, RoundHalfUp, eur), ""},
		"a stamp with no money, elsewhere": {plain, usdStamp, CoreRegistry(), "declares none"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			artifact := test.artifact
			artifact.parts.Money = test.stamp
			err := checkMoneyStamp(&artifact, test.registry)
			if (test.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("checkMoneyStamp error = %v, want %q", err, test.want)
			}
		})
	}
}

// The codes an artifact names are gathered from everywhere it states one:
// types, constants — an exchange rate's two, an array's items — and currency
// checks; variables and empty units are no codes.
func TestMoneyCodesAreEveryCodeTheArtifactNames(t *testing.T) {
	t.Parallel()
	jpy, usd, eur, gbp := "JPY", "USD", "EUR", "GBP"
	minor := int64(1)
	artifact := &Artifact{parts: ArtifactParts{
		Args:   []Parameter{{name: "m", typ: MoneyOf("c")}, {name: "r", typ: FxRateOf("CHF", "")}},
		Result: ArrayOf(MoneyOf("KWD")),
		Constants: []Constant{
			{Type: FxRateKind, String: &usd, Keys: []string{jpy, "150"}},
			{Type: ArrayKind, Items: []Constant{{Type: MoneyKind, Int: &minor, String: &eur}}},
			{Type: CurrencyKind, String: &gbp},
		},
		Instructions: []Instruction{{Op: OpCurrencyCheck, C: CheckCode, Keys: []string{"SEK"}}},
	}}
	want := []string{"CHF", "EUR", "GBP", "JPY", "KWD", "SEK", "USD"}
	if got := moneyCodes(artifact); !slices.Equal(got, want) {
		t.Fatalf("moneyCodes = %v, want %v", got, want)
	}
	stamp := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2}, CurrencySpec{Code: "JPY", Digits: 0}).currencies().stampFor(want)
	if len(stamp.Currencies) != 2 || stamp.Currencies[0].Code != "JPY" || stamp.Currencies[1].Code != "USD" {
		t.Fatalf("stampFor = %+v, want the declared codes among them, JPY then USD", stamp)
	}
}
