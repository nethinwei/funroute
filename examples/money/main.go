// Command money is a host that charges and settles in several currencies. A
// rule computes the fee, the split and the conversion; the host computes the
// same with the methods of funroute.Money beside it, and the two agree to the
// minor unit, because the rule's arithmetic is those methods.
//
//	go run ./examples/money
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/nethinwei/funroute"
	"github.com/nethinwei/funroute/extensions/std"
)

func main() {
	spec := funroute.MoneySpec{Currencies: std.ISO4217()}
	registry := funroute.CoreRegistry()
	if err := registry.DeclareMoney(spec); err != nil {
		log.Fatal(err)
	}
	if err := std.Register(registry); err != nil {
		log.Fatal(err)
	}
	table, err := funroute.NewCurrencies(spec)
	if err != nil {
		log.Fatal(err)
	}
	amount := must(table.Parse("USD 1234.57"))
	rate := must(funroute.Percent("2.9"))
	fee(registry, table, amount, rate)
	split(registry, table, amount)
	convert(registry, table, amount)
}

// fee is 2.9% of the amount, rounded once: in the rule and in Go.
func fee(registry *funroute.Registry, table *funroute.Currencies, amount funroute.Money, rate funroute.Ratio) {
	byRule := run(registry, "round(amount * rate, @half_even)", map[string]any{"amount": amount, "rate": rate},
		funroute.ArgSpec{Name: "amount", Type: funroute.MoneyType}, funroute.ArgSpec{Name: "rate", Type: funroute.RatioType})
	byGo := must(amount.MulRatio(rate, funroute.RoundHalfEven))
	fmt.Printf("fee:     rule %s, Go %s\n", byRule, format(table, byGo))
}

// split shares the amount 1 : 1 : 1 without losing a cent: the remainder
// goes by the largest remainder, the default strategy.
func split(registry *funroute.Registry, table *funroute.Currencies, amount funroute.Money) {
	byRule := run(registry, "allocate(amount, [1, 1, 1])", map[string]any{"amount": amount}, funroute.ArgSpec{Name: "amount", Type: funroute.MoneyType})
	shares := must(amount.Allocate(1, 1, 1))
	texts := make([]string, len(shares))
	for i, share := range shares {
		texts[i] = format(table, share)
	}
	fmt.Printf("split:   rule %s, Go %v\n", byRule, texts)
}

// convert changes the amount into yen at the host's quote, which the rule
// takes as an argument: a rate is data, never a table inside the language.
func convert(registry *funroute.Registry, table *funroute.Currencies, amount funroute.Money) {
	quote := must(table.FxRate("USD", "JPY", "150.25"))
	byRule := run(registry, "using(quote, round(amount -> JPY, @half_even))", map[string]any{"amount": amount, "quote": quote},
		funroute.ArgSpec{Name: "amount", Type: funroute.MoneyType}, funroute.ArgSpec{Name: "quote", Type: funroute.FxRateType})
	byGo := must(table.Convert(amount, quote, funroute.RoundHalfEven))
	fmt.Printf("convert: rule %s, Go %s (at %s)\n", byRule, format(table, byGo), quote)
}

// run compiles source against args and runs it, and writes the value the
// way a person reads money.
func run(registry *funroute.Registry, source string, args map[string]any, specs ...funroute.ArgSpec) string {
	artifact := must(funroute.CompileExpr(source, registry, funroute.CompileOptions{Args: specs}))
	runtime := must(funroute.Instantiate(artifact, registry))
	value := must(runtime.Run(context.Background(), args, funroute.RunOptions{}))
	return string(must(registry.EncodeJSON(value)))
}

func format(table *funroute.Currencies, amount funroute.Money) string {
	return `"` + must(table.Format(amount)) + `"`
}

func must[T any](value T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return value
}
