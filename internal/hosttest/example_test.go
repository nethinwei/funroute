package hosttest_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/nethinwei/funroute"
)

// A rule is compiled against the contract the host declares, bound to the
// registry it may call, and then run: by name, the way a form or a JSON
// request arrives, or in ABI order on the hot path.
func ExampleCompileExpr() {
	registry := funroute.CoreRegistry()
	artifact, err := funroute.CompileExpr(`if(vip, amount * 15 / 10000, amount * 25 / 10000)`, registry, funroute.CompileOptions{
		Args: []funroute.ArgSpec{
			{Name: "vip", Type: funroute.BoolType, Doc: "是否大客户"},
			{Name: "amount", Type: funroute.IntType, Doc: "订单金额，单位：分"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	runtime, err := funroute.Instantiate(artifact, registry)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	options := funroute.RunOptions{Fuel: funroute.DefaultFuel}

	fee, err := runtime.Run(ctx, map[string]any{"vip": false, "amount": 120000}, options)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("by name:", fee.Any())

	fee, err = runtime.RunValues(ctx, []funroute.Value{funroute.Bool(true), funroute.Int(120000)}, options)
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
	registry := funroute.CoreRegistry()
	err := funroute.Logic(registry, "risk.score_v1", funroute.Doc{
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
	artifact, err := funroute.CompileExpr(`if(risk.score_v1(country, amount) > 0.5, "review", "accept")`, registry, funroute.CompileOptions{
		Args: []funroute.ArgSpec{
			{Name: "country", Type: funroute.StringType},
			{Name: "amount", Type: funroute.IntType},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	runtime, err := funroute.Instantiate(artifact, registry)
	if err != nil {
		log.Fatal(err)
	}
	for _, country := range []string{"SG", "BR"} {
		decision, err := runtime.Run(context.Background(), map[string]any{"country": country, "amount": 5000}, funroute.RunOptions{})
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
	binding, err := funroute.Bind[Request, Route](funroute.CoreRegistry())
	if err != nil {
		log.Fatal(err)
	}
	program, err := binding.Compile(`{channel: if(country == "SG", "adyen", "stripe"), fee: amount * 25 / 10000}`)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	route, err := program.Run(ctx, &Request{Country: "SG", Amount: 120000}, funroute.RunOptions{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%+v\n", route)

	requests := []Request{{Country: "SG", Amount: 40000}, {Country: "BR", Amount: 80000}}
	routes := program.RunBatch(ctx, requests, funroute.RunOptions{}, func(i int, err error) {
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
	registry := funroute.CoreRegistry()
	stored, err := funroute.CompileExpr(`{fee: amount * 25 / 10000}`, registry, funroute.CompileOptions{
		Args: []funroute.ArgSpec{{Name: "amount", Type: funroute.IntType}},
	})
	if err != nil {
		log.Fatal(err)
	}
	binding, err := funroute.Bind[Request, Route](registry)
	if err != nil {
		log.Fatal(err)
	}
	program, err := binding.Load(stored)
	if err != nil {
		log.Fatal(err)
	}
	route, err := program.Run(context.Background(), &Request{Country: "SG", Amount: 120000}, funroute.RunOptions{})
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
	contract := funroute.TextContract{
		Types:  map[string]string{"Order": "record{amount: int, currency: string}"},
		Args:   []funroute.TextArg{{Name: "order", Type: "Order", Doc: "订单"}},
		Result: &funroute.TextResult{Type: "Order", Doc: "扣掉优惠后的订单"},
	}
	options, err := contract.Options()
	if err != nil {
		log.Fatal(err)
	}
	registry := funroute.CoreRegistry()
	artifact, err := funroute.CompileExpr(`order with {amount: order.amount - 30}`, registry, options)
	if err != nil {
		log.Fatal(err)
	}
	runtime, err := funroute.Instantiate(artifact, registry)
	if err != nil {
		log.Fatal(err)
	}
	order := map[string]any{"amount": 1200, "currency": "SGD"}
	discounted, err := runtime.Run(context.Background(), map[string]any{"order": order}, funroute.RunOptions{})
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
	formatted, err := funroute.Format(`let(rate=250,cap=5000,min(amount*rate/10000,cap))`)
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
	registry := funroute.CoreRegistry()
	err := registry.DeclareMoney(funroute.MoneySpec{Currencies: []funroute.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "JPY", Digits: 0},
	}})
	if err != nil {
		log.Fatal(err)
	}
	artifact, err := funroute.CompileExpr(`round(amount * 0.029, @half_even) + money(30, currency(amount))`, registry, funroute.CompileOptions{
		Args: []funroute.ArgSpec{{Name: "amount", Type: funroute.MoneyType, Doc: "交易金额"}},
	})
	if err != nil {
		log.Fatal(err)
	}
	runtime, err := funroute.Instantiate(artifact, registry)
	if err != nil {
		log.Fatal(err)
	}
	for _, amount := range []string{"USD 12.34", "JPY 1234"} {
		fee, err := runtime.Run(context.Background(), map[string]any{"amount": amount}, funroute.RunOptions{})
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
	registry := funroute.CoreRegistry()
	err := registry.DeclareMoney(funroute.MoneySpec{Currencies: []funroute.CurrencySpec{
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
	show := func(m funroute.Money) string {
		text, err := table.Format(m)
		if err != nil {
			log.Fatal(err)
		}
		return text
	}
	ratio, _ := funroute.Percent("2.9")
	fee, _ := amount.MulRatio(ratio, funroute.RoundHalfUp)
	fixed, _ := table.Parse("USD 0.30")
	fee, _ = fee.Add(fixed)
	fmt.Println("fee:", show(fee))

	shares, _ := fee.Allocate(2, 1)
	fmt.Println("split:", show(shares[0]), show(shares[1]))

	fx, err := table.FxRate("USD", "JPY", "150")
	if err != nil {
		log.Fatal(err)
	}
	yen, _ := table.Convert(fee, fx, funroute.RoundHalfUp)
	fmt.Println("in yen:", show(yen))

	_, err = fee.Add(yen)
	fmt.Println("dollars plus yen:", errors.Is(err, funroute.ErrCurrency))
	// Output:
	// fee: USD 3.78
	// split: USD 2.52 USD 1.26
	// in yen: JPY 567
	// dollars plus yen: true
}

// A ratio is read exactly in the unit it is written in — percent, basis
// points or a plain ratio, however many places — and a ratio with no finite
// decimal is written as its fraction. Text that is neither is refused.
func ExampleParseRatio() {
	for _, reading := range []struct {
		name string
		read func(string) (funroute.Ratio, error)
		text string
	}{
		{"Percent", funroute.Percent, "2.9"}, {"BasisPoints", funroute.BasisPoints, "25"}, {"BasisPoints", funroute.BasisPoints, "0.5"},
		{"ParseRatio", funroute.ParseRatio, "0.029"}, {"ParseRatio", funroute.ParseRatio, "0.00000000001"},
		{"ParseRatio", funroute.ParseRatio, "1/3"}, {"Percent", funroute.Percent, "1/3"},
	} {
		ratio, err := reading.read(reading.text)
		fmt.Println(reading.name, reading.text, "=", ratio, err)
	}
	for _, text := range []string{"1e3", "1_0", "1/0", "2.9%"} {
		_, err := funroute.ParseRatio(text)
		fmt.Println("ParseRatio", text, "refused:", err != nil)
	}
	card, _ := funroute.ParseRatio("0.029")
	encoded, _ := json.Marshal(card)
	var decoded funroute.Ratio
	err := json.Unmarshal([]byte(`0.029`), &decoded)
	fmt.Println(string(encoded), decoded, decoded.Cmp(card), err)
	// Output:
	// Percent 2.9 = 0.029 <nil>
	// BasisPoints 25 = 0.0025 <nil>
	// BasisPoints 0.5 = 0.00005 <nil>
	// ParseRatio 0.029 = 0.029 <nil>
	// ParseRatio 0.00000000001 = 0.00000000001 <nil>
	// ParseRatio 1/3 = 1/3 <nil>
	// Percent 1/3 = 1/300 <nil>
	// ParseRatio 1e3 refused: true
	// ParseRatio 1_0 refused: true
	// ParseRatio 1/0 refused: true
	// ParseRatio 2.9% refused: true
	// "0.029" 0.029 0 <nil>
}

// Rates combine exactly — a third is a third; exchange rates are exact too,
// chain only through the currency they share, and invert.
func ExampleFxRate_Chain() {
	card, _ := funroute.Percent("2.9")
	scheme, _ := funroute.BasisPoints("25")
	sum, _ := card.Add(scheme)
	onFee, _ := card.Mul(card)
	one, _ := funroute.ParseRatio("1")
	three, _ := funroute.ParseRatio("3")
	third, _ := one.Div(three)
	fmt.Println(sum, onFee, third)
	_, err := one.Div(funroute.Ratio{})
	fmt.Println("divided by zero:", err != nil)

	table, _ := funroute.NewCurrencies(funroute.MoneySpec{Currencies: []funroute.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "EUR", Digits: 2}, {Code: "JPY", Digits: 0},
	}})
	usdEur, _ := table.FxRate("USD", "EUR", "0.9")
	eurJpy, _ := table.FxRate("EUR", "JPY", "160")
	usdJpy, _ := usdEur.Chain(eurJpy)
	fmt.Println(usdJpy)
	_, err = eurJpy.Chain(usdEur)
	fmt.Println("EUR→JPY then USD→EUR:", errors.Is(err, funroute.ErrCurrency))
	usdEur, _ = table.FxRate("USD", "EUR", "0.7")
	back, _ := usdEur.Inverse()
	shown, _ := back.Decimal(4)
	fmt.Println(back, shown)
	// Output:
	// 0.0315 0.000841 1/3
	// divided by zero: true
	// 144 JPY / USD
	// EUR→JPY then USD→EUR: true
	// 10/7 USD / EUR 1.4286
}

// An amount times a ratio falls between two minor units and is rounded once,
// each mode on either side of zero as its name says: USD 0.05, USD -0.05 and
// USD 0.07 at 50% are 2.5, -2.5 and 3.5 cents.
func ExampleMoney_MulRatio() {
	table, _ := funroute.NewCurrencies(funroute.MoneySpec{Currencies: []funroute.CurrencySpec{{Code: "USD", Digits: 2}}})
	half, _ := funroute.Percent("50")
	for _, name := range []string{"half_even", "half_up", "half_down", "down", "up", "ceiling", "floor"} {
		mode, _ := funroute.ParseRounding(name)
		fmt.Printf("%-9s", mode)
		for _, cents := range []int64{5, -5, 7} {
			amount, _ := table.Minor("USD", cents)
			share, _ := amount.MulRatio(half, mode)
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
	fmt.Println(funroute.RoundingEnumType())
	for _, name := range []string{"half_even", "HALF_UP", "banker"} {
		mode, err := funroute.ParseRounding(name)
		fmt.Println(name, mode, err != nil)
	}
	// Output:
	// enum<rounding>{ceiling,down,floor,half_down,half_even,half_up,up}
	// half_even half_even false
	// HALF_UP invalid true
	// banker invalid true
}

// Converting rescales between the two currencies' places and rounds once;
// the inverse rate converts back and Implied recovers the rate from both
// amounts. Money in the wrong currency is an ErrCurrency, never a silent
// conversion, and a rate to a currency the table does not have cannot be made.
func ExampleCurrencies_Convert() {
	table, err := funroute.NewCurrencies(funroute.MoneySpec{Currencies: []funroute.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "JPY", Digits: 0}, {Code: "KWD", Digits: 3},
	}})
	if err != nil {
		log.Fatal(err)
	}
	show := func(m funroute.Money) string {
		text, err := table.Format(m)
		if err != nil {
			log.Fatal(err)
		}
		return text
	}
	dollars := func(cents int64) funroute.Money {
		m, _ := table.Minor("USD", cents)
		return m
	}
	yen, _ := table.FxRate("USD", "JPY", "150")
	converted, _ := table.Convert(dollars(378), yen, funroute.RoundHalfUp)
	toDollars, _ := yen.Inverse()
	back, _ := table.Convert(converted, toDollars, funroute.RoundHalfUp)
	paid, _ := table.Minor("JPY", 15_000)
	implied, _ := table.Implied(paid, dollars(10_000))
	fmt.Println(show(converted), show(back), implied)
	dinar, _ := table.FxRate("USD", "KWD", "0.307")
	cent, _ := table.Convert(dollars(1), dinar, funroute.RoundHalfUp)
	zero, _ := table.Convert(funroute.Money{}, yen, funroute.RoundHalfUp)
	fmt.Println(show(cent), show(zero))
	oneYen, _ := table.Minor("JPY", 1)
	_, wrong := table.Convert(oneYen, yen, funroute.RoundHalfUp)
	_, undeclared := table.FxRate("USD", "EUR", "1")
	_, backWrong := table.Convert(dollars(1), toDollars, funroute.RoundHalfUp)
	_, noCurrency := table.Implied(funroute.Money{}, dollars(1))
	for _, err := range []error{wrong, undeclared, backWrong, noCurrency} {
		fmt.Print(errors.Is(err, funroute.ErrCurrency), " ")
	}
	fmt.Println()
	// Output:
	// JPY 567 USD 3.78 150 JPY / USD
	// KWD 0.003 JPY 0
	// true true true true
}

// The exchange rates a rule converts at are an argument like any other: the
// host's []FxRate crosses as it is, and using(market, …) converts at it —
// one hop, a quote the other way round by its inverse. A rule may write its
// own quotes, or read one from the using around it with fx and mark it up.
func ExampleFxRate() {
	registry := funroute.CoreRegistry()
	if err := registry.DeclareMoney(funroute.MoneySpec{Currencies: []funroute.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "EUR", Digits: 2}, {Code: "JPY", Digits: 0},
	}}); err != nil {
		log.Fatal(err)
	}
	table, _ := registry.Currencies()
	usdJPY, _ := table.FxRate("USD", "JPY", "150")
	usdEUR, _ := table.FxRate("USD", "EUR", "0.92")
	market := []funroute.FxRate{usdJPY, usdEUR}

	contract := funroute.CompileOptions{Args: []funroute.ArgSpec{{Name: "amount", Type: funroute.MoneyType}, {Name: "market", Type: funroute.ArrayOf(funroute.FxRateType)}}}
	for _, source := range []string{
		"using(market, round(amount -> JPY, @half_even))",
		"using(market, round(amount -> EUR -> USD, @half_even))",
		"using(160 JPY / USD, convert(amount, JPY, @half_up))",
		"using(market, using(fx(USD, JPY) * 101%, round(amount -> JPY, @half_even)))",
	} {
		artifact, err := funroute.CompileExpr(source, registry, contract)
		if err != nil {
			log.Fatal(err)
		}
		runtime, _ := funroute.Instantiate(artifact, registry)
		dollars, _ := table.Parse("USD 10.00")
		amount, _ := funroute.ToValue(dollars)
		quotes, _ := funroute.ToValue(market)
		value, err := runtime.RunValues(context.Background(), []funroute.Value{amount, quotes}, funroute.RunOptions{})
		if err != nil {
			log.Fatal(err)
		}
		result, _ := value.Money()
		text, _ := table.Format(result)
		fmt.Println(source, "=", text)
	}
	// Output:
	// using(market, round(amount -> JPY, @half_even)) = JPY 1500
	// using(market, round(amount -> EUR -> USD, @half_even)) = USD 10.00
	// using(160 JPY / USD, convert(amount, JPY, @half_up)) = JPY 1600
	// using(market, using(fx(USD, JPY) * 101%, round(amount -> JPY, @half_even))) = JPY 1515
}
