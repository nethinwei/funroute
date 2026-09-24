package lang_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"funroute/lang"
)

// A rule is compiled against the contract the host declares, bound to the
// registry it may call, and then run: by name, the way a form or a JSON
// request arrives, or in ABI order on the hot path.
func ExampleCompileExpr() {
	registry := lang.CoreRegistry()
	artifact, err := lang.CompileExpr(`if(vip, amount * 15 / 10000, amount * 25 / 10000)`, registry, lang.CompileOptions{
		Args: []lang.ArgSpec{
			{Name: "vip", Type: lang.BoolType, Doc: "是否大客户"},
			{Name: "amount", Type: lang.IntType, Doc: "订单金额，单位：分"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	options := lang.RunOptions{Fuel: lang.DefaultFuel}

	fee, err := runtime.Run(ctx, map[string]any{"vip": false, "amount": 120000}, options)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("by name:", fee.Any())

	fee, err = runtime.RunValues(ctx, []lang.Value{lang.Bool(true), lang.Int(120000)}, options)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("in order:", fee.Any())
	// Output:
	// by name: 300
	// in order: 180
}

// A domain function is registered by its Go signature; the Doc says only what
// the signature cannot, and a rule calls the function by its name.
func ExampleLogic() {
	registry := lang.CoreRegistry()
	err := lang.Logic(registry, "risk.score_v1", lang.Doc{
		Label:  "风险评分",
		Cost:   25,
		Params: []string{"国家", "金额"},
	}, func(country string, amount int64) (float64, error) {
		if country == "SG" && amount < 1_000_000 {
			return 0.2, nil
		}
		return 0.9, nil
	})
	if err != nil {
		log.Fatal(err)
	}
	artifact, err := lang.CompileExpr(`if(risk.score_v1(country, amount) > 0.5, "review", "accept")`, registry, lang.CompileOptions{
		Args: []lang.ArgSpec{
			{Name: "country", Type: lang.StringType},
			{Name: "amount", Type: lang.IntType},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		log.Fatal(err)
	}
	for _, country := range []string{"SG", "BR"} {
		decision, err := runtime.Run(context.Background(), map[string]any{"country": country, "amount": 5000}, lang.RunOptions{})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(country, decision.Any())
	}
	// Output:
	// SG accept
	// BR review
}

// The contract is two Go types: the tagged fields of the request are the
// arguments, in their order, and the result is written straight into the
// response. RunBatch runs the requests a host already holds, and reports a
// failure by the index of the request that failed.
func ExampleBind() {
	type Request struct {
		Country string `funroute:"country"`
		Amount  int64  `funroute:"amount"`
	}
	type Route struct {
		Channel string `funroute:"channel"`
		Fee     int64  `funroute:"fee"`
	}
	binding, err := lang.Bind[Request, Route](lang.CoreRegistry())
	if err != nil {
		log.Fatal(err)
	}
	program, err := binding.Compile(`{channel: if(country == "SG", "adyen", "stripe"), fee: amount * 25 / 10000}`)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	route, err := program.Run(ctx, &Request{Country: "SG", Amount: 120000}, lang.RunOptions{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%+v\n", route)

	requests := []Request{{Country: "SG", Amount: 40000}, {Country: "BR", Amount: 80000}}
	routes := program.RunBatch(ctx, requests, lang.RunOptions{}, func(i int, err error) {
		log.Printf("request %d: %v", i, err)
	})
	for i, route := range routes {
		fmt.Printf("%d: %+v\n", i, route)
	}
	// Output:
	// {Channel:adyen Fee:300}
	// 0: {Channel:adyen Fee:100}
	// 1: {Channel:stripe Fee:200}
}

// A console compiles a rule against only what it reads. A binding loads that
// artifact by name: the request's other fields are ignored, and the response
// fields the rule does not write stay zero.
func ExampleBinding_Load() {
	type Request struct {
		Country string `funroute:"country"`
		Amount  int64  `funroute:"amount"`
	}
	type Route struct {
		Channel string `funroute:"channel"`
		Fee     int64  `funroute:"fee"`
	}
	registry := lang.CoreRegistry()
	stored, err := lang.CompileExpr(`{fee: amount * 25 / 10000}`, registry, lang.CompileOptions{
		Args: []lang.ArgSpec{{Name: "amount", Type: lang.IntType}},
	})
	if err != nil {
		log.Fatal(err)
	}
	binding, err := lang.Bind[Request, Route](registry)
	if err != nil {
		log.Fatal(err)
	}
	program, err := binding.Load(stored)
	if err != nil {
		log.Fatal(err)
	}
	route, err := program.Run(context.Background(), &Request{Country: "SG", Amount: 120000}, lang.RunOptions{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%+v\n", route)
	// Output:
	// {Channel: Fee:300}
}

// A record update gives back the record it was given with some fields
// replaced. The contract is written as text, the way a console stores it, and
// the complex type is declared once by name.
func ExampleCompileExpr_recordUpdate() {
	contract := lang.TextContract{
		Types:  map[string]string{"Order": "record{amount: int, currency: string}"},
		Args:   []lang.TextArg{{Name: "order", Type: "Order", Doc: "订单"}},
		Result: &lang.TextResult{Type: "Order", Doc: "扣掉优惠后的订单"},
	}
	options, err := contract.Options()
	if err != nil {
		log.Fatal(err)
	}
	registry := lang.CoreRegistry()
	artifact, err := lang.CompileExpr(`order with {amount: order.amount - 30}`, registry, options)
	if err != nil {
		log.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		log.Fatal(err)
	}
	order := map[string]any{"amount": 1200, "currency": "SGD"}
	discounted, err := runtime.Run(context.Background(), map[string]any{"order": order}, lang.RunOptions{})
	if err != nil {
		log.Fatal(err)
	}
	// A record marshals in its type's field order.
	encoded, err := json.Marshal(discounted)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(encoded))
	// Output:
	// {"amount":1170,"currency":"SGD"}
}

// Format lays a program out the way the language prints it; the result parses
// back to the same program.
func ExampleFormat() {
	formatted, err := lang.Format(`let(rate=250,cap=5000,min(amount*rate/10000,cap))`)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(formatted)
	// Output:
	// let(rate = 250, cap = 5000, min(amount * rate / 10000, cap))
}

// Money is declared once per console: the currencies and the rounding. A
// contract in one currency variable serves every currency, and the operators
// keep the currencies straight — the fee comes back in the currency the
// amount brought, rounded half up to its minor unit.
func ExampleRegistry_DeclareMoney() {
	registry := lang.CoreRegistry()
	err := registry.DeclareMoney(lang.MoneySpec{Rounding: lang.RoundHalfUp, Currencies: []lang.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "JPY", Digits: 0},
	}})
	if err != nil {
		log.Fatal(err)
	}
	artifact, err := lang.CompileExpr(`amount * 0.029 + like(amount, 30)`, registry, lang.CompileOptions{
		Args: []lang.ArgSpec{{Name: "amount", Type: lang.MoneyOf("c"), Doc: "交易金额"}},
	})
	if err != nil {
		log.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		log.Fatal(err)
	}
	for _, amount := range []string{"USD 12.34", "JPY 1234"} {
		fee, err := runtime.Run(context.Background(), map[string]any{"amount": amount}, lang.RunOptions{})
		if err != nil {
			log.Fatal(err)
		}
		text, err := registry.EncodeJSON(fee)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(amount, "->", string(text))
	}
	// Output:
	// USD 12.34 -> "USD 0.66"
	// JPY 1234 -> "JPY 66"
}

// A host works with money in Go the way a rule does: the same arithmetic,
// the same rounding, the same refusal to add dollars to euros. What needs a
// currency's decimal places — reading and writing an amount, converting it —
// goes through the table the registry declared.
func ExampleCurrencies() {
	registry := lang.CoreRegistry()
	err := registry.DeclareMoney(lang.MoneySpec{Rounding: lang.RoundHalfUp, Currencies: []lang.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "JPY", Digits: 0},
	}})
	if err != nil {
		log.Fatal(err)
	}
	table, _ := registry.Currencies()

	amount, err := table.Parse("USD 120.00")
	if err != nil {
		log.Fatal(err)
	}
	show := func(m lang.Money) string {
		text, err := table.Format(m)
		if err != nil {
			log.Fatal(err)
		}
		return text
	}
	rate, _ := lang.Percent("2.9")
	fee, _ := amount.MulRate(rate, table.Rounding())
	fixed, _ := table.Parse("USD 0.30")
	fee, _ = fee.Add(fixed)
	fmt.Println("fee:", show(fee))

	shares, _ := fee.Allocate(2, 1)
	fmt.Println("split:", show(shares[0]), show(shares[1]))

	fx, err := table.FxRate("USD", "JPY", "150")
	if err != nil {
		log.Fatal(err)
	}
	yen, _ := table.Convert(fee, fx, lang.RoundHalfUp)
	fmt.Println("in yen:", show(yen))

	_, err = fee.Add(yen)
	fmt.Println("dollars plus yen:", errors.Is(err, lang.ErrCurrency))
	// Output:
	// fee: USD 3.78
	// split: USD 2.52 USD 1.26
	// in yen: JPY 567
	// dollars plus yen: true
}

// A rate is read exactly in the unit it is written in — percent, basis
// points or a plain ratio — and refused rather than rounded when it has more
// than ten decimal places.
func ExampleParseRate() {
	for _, reading := range []struct {
		name string
		read func(string) (lang.Rate, error)
		text string
	}{
		{"Percent", lang.Percent, "2.9"}, {"BasisPoints", lang.BasisPoints, "25"}, {"BasisPoints", lang.BasisPoints, "0.5"},
		{"ParseRate", lang.ParseRate, "0.029"}, {"ParseRate", lang.ParseRate, "922337203.6854775807"},
	} {
		rate, err := reading.read(reading.text)
		fmt.Println(reading.name, reading.text, "=", rate, err)
	}
	for _, text := range []string{"0.00000000001", "1e3", "1_0", "922337203.6854775808"} {
		_, err := lang.ParseRate(text)
		fmt.Println("ParseRate", text, "refused:", err != nil)
	}
	_, err := lang.Percent("0.000000001")
	fmt.Println("Percent 0.000000001 refused:", err != nil)
	card, _ := lang.ParseRate("0.029")
	encoded, _ := json.Marshal(card)
	var decoded lang.Rate
	err = json.Unmarshal([]byte(`0.029`), &decoded)
	fmt.Println(string(encoded), decoded, decoded.Cmp(card), err)
	// Output:
	// Percent 2.9 = 0.029 <nil>
	// BasisPoints 25 = 0.0025 <nil>
	// BasisPoints 0.5 = 0.00005 <nil>
	// ParseRate 0.029 = 0.029 <nil>
	// ParseRate 922337203.6854775807 = 922337203.6854775807 <nil>
	// ParseRate 0.00000000001 refused: true
	// ParseRate 1e3 refused: true
	// ParseRate 1_0 refused: true
	// ParseRate 922337203.6854775808 refused: true
	// Percent 0.000000001 refused: true
	// "0.029" 0.029 0 <nil>
}

// Rates combine exactly, rounding half to even past ten places; exchange
// rates are exact, chain only through the currency they share, and invert.
func ExampleFxRate_Chain() {
	card, _ := lang.Percent("2.9")
	scheme, _ := lang.BasisPoints("25")
	sum, _ := card.Add(scheme)
	onFee, _ := card.Mul(card)
	one, _ := lang.ParseRate("1")
	three, _ := lang.ParseRate("3")
	third, _ := one.Div(three)
	fmt.Println(sum, onFee, third)
	_, err := one.Div(lang.Rate{})
	fmt.Println("divided by zero:", err != nil)

	table, _ := lang.NewCurrencies(lang.MoneySpec{Rounding: lang.RoundHalfEven, Currencies: []lang.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "EUR", Digits: 2}, {Code: "JPY", Digits: 0},
	}})
	usdEur, _ := table.FxRate("USD", "EUR", "0.9")
	eurJpy, _ := table.FxRate("EUR", "JPY", "160")
	usdJpy, _ := usdEur.Chain(eurJpy)
	fmt.Println(usdJpy)
	_, err = eurJpy.Chain(usdEur)
	fmt.Println("EUR→JPY then USD→EUR:", errors.Is(err, lang.ErrCurrency))
	usdEur, _ = table.FxRate("USD", "EUR", "0.7")
	back, _ := usdEur.Inverse()
	shown, _ := back.Decimal(4)
	fmt.Println(back, shown)
	// Output:
	// 0.0315 0.000841 0.3333333333
	// divided by zero: true
	// USD/JPY 144
	// EUR→JPY then USD→EUR: true
	// EUR/USD 10/7 1.4286
}

// An amount times a rate falls between two minor units and is rounded once,
// each mode on either side of zero as its name says: USD 0.05, USD -0.05 and
// USD 0.07 at 50% are 2.5, -2.5 and 3.5 cents.
func ExampleMoney_MulRate() {
	table, _ := lang.NewCurrencies(lang.MoneySpec{Rounding: lang.RoundHalfEven, Currencies: []lang.CurrencySpec{{Code: "USD", Digits: 2}}})
	half, _ := lang.Percent("50")
	for _, name := range []string{"half_even", "half_up", "half_down", "down", "up", "ceiling", "floor"} {
		mode, _ := lang.ParseRounding(name)
		fmt.Printf("%-9s", mode)
		for _, cents := range []int64{5, -5, 7} {
			amount, _ := table.Minor("USD", cents)
			share, _ := amount.MulRate(half, mode)
			fmt.Printf(" %2d", share.Minor())
		}
		fmt.Println()
	}
	// Output:
	// half_even  2 -2  4
	// half_up    3 -3  4
	// half_down  2 -2  3
	// down       2 -2  3
	// up         3 -3  4
	// ceiling    3 -2  4
	// floor      2 -3  3
}

// A rounding mode goes by one name everywhere — in Go, in a declaration's
// JSON and in round(expr, @mode) — and a name that is not one is refused.
func ExampleParseRounding() {
	fmt.Println(lang.RoundingEnumType())
	for _, name := range []string{"half_even", "HALF_UP", "banker"} {
		mode, err := lang.ParseRounding(name)
		fmt.Println(name, mode, err != nil)
	}
	encoded, _ := json.Marshal(lang.MoneySpec{Rounding: lang.RoundCeiling, Currencies: []lang.CurrencySpec{{Code: "JPY"}}})
	fmt.Println(string(encoded))
	_, err := json.Marshal(lang.Rounding(0))
	fmt.Println("an unset rounding cannot be written:", err != nil)
	// Output:
	// enum<rounding>{ceiling,down,floor,half_down,half_even,half_up,up}
	// half_even half_even false
	// HALF_UP invalid true
	// banker invalid true
	// {"rounding":"ceiling","currencies":[{"code":"JPY","digits":0}]}
	// an unset rounding cannot be written: true
}

// Converting rescales between the two currencies' places and rounds once;
// the inverse rate converts back and Implied recovers the rate from both
// amounts. Money in the wrong currency is an ErrCurrency, never a silent
// conversion, and a rate to a currency the table does not have cannot be made.
func ExampleCurrencies_Convert() {
	table, err := lang.NewCurrencies(lang.MoneySpec{Rounding: lang.RoundHalfEven, Currencies: []lang.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "JPY", Digits: 0}, {Code: "KWD", Digits: 3},
	}})
	if err != nil {
		log.Fatal(err)
	}
	show := func(m lang.Money) string {
		text, err := table.Format(m)
		if err != nil {
			log.Fatal(err)
		}
		return text
	}
	dollars := func(cents int64) lang.Money {
		m, _ := table.Minor("USD", cents)
		return m
	}
	yen, _ := table.FxRate("USD", "JPY", "150")
	converted, _ := table.Convert(dollars(378), yen, lang.RoundHalfUp)
	toDollars, _ := yen.Inverse()
	back, _ := table.Convert(converted, toDollars, lang.RoundHalfUp)
	paid, _ := table.Minor("JPY", 15_000)
	implied, _ := table.Implied(paid, dollars(10_000))
	fmt.Println(show(converted), show(back), implied)
	dinar, _ := table.FxRate("USD", "KWD", "0.307")
	cent, _ := table.Convert(dollars(1), dinar, lang.RoundHalfUp)
	zero, _ := table.Convert(lang.Money{}, yen, lang.RoundHalfUp)
	fmt.Println(show(cent), show(zero))
	oneYen, _ := table.Minor("JPY", 1)
	_, wrong := table.Convert(oneYen, yen, lang.RoundHalfUp)
	_, undeclared := table.FxRate("USD", "EUR", "1")
	_, backWrong := table.Convert(dollars(1), toDollars, lang.RoundHalfUp)
	_, noCurrency := table.Implied(lang.Money{}, dollars(1))
	for _, err := range []error{wrong, undeclared, backWrong, noCurrency} {
		fmt.Print(errors.Is(err, lang.ErrCurrency), " ")
	}
	fmt.Println()
	// Output:
	// JPY 567 USD 3.78 USD/JPY 150
	// KWD 0.003 JPY 0
	// true true true true
}

// A rate table holds the quotes a host has and converts through the shortest
// chain of them, rounding once. Given to a run as RunOptions.Rates, it is the
// table amount -> JPY converts through; using lays a rule's own quotes over
// it, and "..." marks where the table goes: what is written later wins.
func ExampleRates() {
	registry := lang.CoreRegistry()
	if err := registry.DeclareMoney(lang.MoneySpec{Rounding: lang.RoundHalfEven, Currencies: []lang.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "EUR", Digits: 2}, {Code: "JPY", Digits: 0},
	}}); err != nil {
		log.Fatal(err)
	}
	table, _ := registry.Currencies()
	rates := table.NewRates()
	_ = rates.Add("USD", "JPY", "150")
	_ = rates.Add("USD", "EUR", "0.92")
	// One hop at a time: euros to dollars at the inverse of USD/EUR, then
	// dollars to yen. The table never goes through a third currency itself.
	euros, _ := table.Parse("EUR 92.00")
	dollars, _ := rates.Convert(euros, "USD", lang.RoundHalfEven)
	yen, _ := rates.Convert(dollars, "JPY", lang.RoundHalfEven)
	_, direct := rates.Rate("EUR", "JPY")
	back, _ := rates.Rate("JPY", "USD")
	shown, _ := table.Format(yen)
	fmt.Println(shown, back, errors.Is(direct, lang.ErrNoRate))

	contract := lang.CompileOptions{Args: []lang.ArgSpec{{Name: "amount", Type: lang.MoneyOf("USD")}}}
	for _, source := range []string{"amount -> JPY", "using(160 JPY / USD, amount -> JPY)", "using(fx(USD, JPY) * 101%, amount -> JPY)"} {
		artifact, err := lang.CompileExpr(source, registry, contract)
		if err != nil {
			log.Fatal(err)
		}
		runtime, _ := lang.Instantiate(artifact, registry)
		value, err := runtime.Run(context.Background(), map[string]any{"amount": "USD 10.00"}, lang.RunOptions{Rates: rates})
		if err != nil {
			log.Fatal(err)
		}
		amount, _ := value.Money()
		text, _ := table.Format(amount)
		fmt.Println(source, "=", text)
	}
	// Output:
	// JPY 15000 JPY/USD 1/150 true
	// amount -> JPY = JPY 1500
	// using(160 JPY / USD, amount -> JPY) = JPY 1600
	// using(fx(USD, JPY) * 101%, amount -> JPY) = JPY 1515
}

// A quote keeps where it came from and when, for the host to read back; a
// rule converts through a named table the contract declares by naming it.
func ExampleRates_Quote() {
	registry := lang.CoreRegistry()
	if err := registry.DeclareMoney(lang.MoneySpec{Rounding: lang.RoundHalfUp, Currencies: []lang.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "JPY", Digits: 0},
	}}); err != nil {
		log.Fatal(err)
	}
	table, _ := registry.Currencies()
	settlement := table.NewRates()
	at := time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)
	_ = settlement.AddQuote(lang.QuoteSpec{Base: "USD", Quote: "JPY", Rate: "149.5", Source: "clearing", At: at})
	quote, _ := settlement.Quote("USD", "JPY")
	fmt.Println(quote.Rate(), quote.Source(), quote.At().Format(time.RFC3339))

	contract := lang.CompileOptions{Args: []lang.ArgSpec{{Name: "amount", Type: lang.MoneyOf("USD")}}, RateTables: []string{"settlement"}}
	artifact, err := lang.CompileExpr("using(@settlement, amount -> JPY)", registry, contract)
	if err != nil {
		log.Fatal(err)
	}
	runtime, _ := lang.Instantiate(artifact, registry)
	options := lang.RunOptions{RateTables: map[string]*lang.Rates{"settlement": settlement}}
	value, _ := runtime.Run(context.Background(), map[string]any{"amount": "USD 10.00"}, options)
	yen, _ := value.Money()
	text, _ := table.Format(yen)
	fmt.Println(text)
	// Output:
	// USD/JPY 149.5 clearing 2026-09-24T08:00:00Z
	// JPY 1495
}
