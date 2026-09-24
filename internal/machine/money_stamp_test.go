package machine

import (
	"slices"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/money"
)

// Any money-kind type an artifact states makes it an artifact that uses
// money, however deep in a container or record it is.
func TestAnArtifactUsesMoneyWhereverItStatesIt(t *testing.T) {
	t.Parallel()
	whole, minor, code := int64(5), int64(170), "USD"
	amount := Constant{Type: MoneyKind, Int: &minor, String: &code}
	arrayOfMoney := ArrayOf(MoneyType)
	order := RecordOf(Field{name: "fee", typ: RatioType})
	for name, test := range map[string]struct {
		artifact Artifact
		uses     bool
	}{
		"nothing but ints":        {Artifact{parts: ArtifactParts{Args: []Parameter{{name: "n", typ: IntType}}, Result: IntType}}, false},
		"a money argument":        {Artifact{parts: ArtifactParts{Args: []Parameter{{name: "m", typ: MoneyType}}, Result: IntType}}, true},
		"a ratio in an array":     {Artifact{parts: ArtifactParts{Args: []Parameter{{name: "r", typ: ArrayOf(RatioType)}}, Result: IntType}}, true},
		"a ratio in a record":     {Artifact{parts: ArtifactParts{Args: []Parameter{{name: "o", typ: order}}, Result: IntType}}, true},
		"money in a dict result":  {Artifact{parts: ArtifactParts{Result: DictOf(MoneyType)}}, true},
		"a currency result":       {Artifact{parts: ArtifactParts{Result: CurrencyType}}, true},
		"an instruction's type":   {Artifact{parts: ArtifactParts{Result: IntType, Instructions: []Instruction{{Op: OpMakeArray, Type: &arrayOfMoney}}}}, true},
		"an instruction's ints":   {Artifact{parts: ArtifactParts{Result: IntType, Instructions: []Instruction{{Op: OpMakeArray, Type: new(ArrayOf(IntType))}}}}, false},
		"a ratio constant":        {Artifact{parts: ArtifactParts{Result: IntType, Constants: []Constant{{Type: RatioKind, Int: &whole}}}}, true},
		"money deep in constants": {Artifact{parts: ArtifactParts{Result: IntType, Constants: []Constant{{Type: ArrayKind, Items: []Constant{{Type: RecordKind, Items: []Constant{amount}}}}}}}, true},
		"an empty money array":    {Artifact{parts: ArtifactParts{Result: IntType, Constants: []Constant{{Type: ArrayKind, Elem: &arrayOfMoney}}}}, true},
		"an int constant":         {Artifact{parts: ArtifactParts{Result: IntType, Constants: []Constant{{Type: IntKind, Int: &whole}}}}, false},
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
	usd, eur := money.CurrencySpec{Code: "USD", Digits: 2}, money.CurrencySpec{Code: "EUR", Digits: 2}
	registry := declared(t, usd)
	usdStamp := &MoneyStamp{Currencies: []money.CurrencySpec{usd}}
	plain := Artifact{parts: ArtifactParts{Result: IntType}}
	amount := Artifact{parts: ArtifactParts{Result: MoneyType}}
	for name, test := range map[string]struct {
		artifact Artifact
		stamp    *MoneyStamp
		registry *Registry
		want     string
	}{
		"no money and no stamp":            {plain, nil, registry, ""},
		"no money into no money":           {plain, nil, CoreRegistry(), ""},
		"money without a stamp":            {amount, nil, registry, "records no money stamp"},
		"a stamp into a registry without":  {amount, usdStamp, CoreRegistry(), "declares none"},
		"other places for a named code":    {amount, usdStamp, declared(t, money.CurrencySpec{Code: "USD", Digits: 3}), "decimal places"},
		"a named code no longer declared":  {amount, usdStamp, declared(t, eur), "does not declare"},
		"a registry declaring more":        {amount, usdStamp, declared(t, usd, eur), ""},
		"the registry's own facts":         {amount, usdStamp, registry, ""},
		"no code named, another table":     {amount, &MoneyStamp{}, declared(t, eur), ""},
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

// The codes an artifact names are gathered from its constants — an amount's,
// a currency's, an exchange rate's two, an array's items; a type names none.
func TestMoneyCodesAreEveryCodeTheArtifactNames(t *testing.T) {
	t.Parallel()
	jpy, usd, eur, gbp := "JPY", "USD", "EUR", "GBP"
	minor := int64(1)
	artifact := &Artifact{parts: ArtifactParts{
		Args:   []Parameter{{name: "m", typ: MoneyType}, {name: "r", typ: FxRateType}},
		Result: ArrayOf(MoneyType),
		Constants: []Constant{
			{Type: FxRateKind, String: &usd, Keys: []string{jpy, "150"}},
			{Type: ArrayKind, Items: []Constant{{Type: MoneyKind, Int: &minor, String: &eur}}},
			{Type: CurrencyKind, String: &gbp},
		},
	}}
	want := []string{"EUR", "GBP", "JPY", "USD"}
	if got := moneyCodes(artifact); !slices.Equal(got, want) {
		t.Fatalf("moneyCodes = %v, want %v", got, want)
	}
	stamp := stampFor(declared(t, money.CurrencySpec{Code: "USD", Digits: 2}, money.CurrencySpec{Code: "JPY", Digits: 0}).currencies(), want)
	if len(stamp.Currencies) != 2 || stamp.Currencies[0].Code != "JPY" || stamp.Currencies[1].Code != "USD" {
		t.Fatalf("stampFor = %+v, want the declared codes among them, JPY then USD", stamp)
	}
}
