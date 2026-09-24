package machine_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

type benchOrder struct {
	Amount   int64   `funroute:"amount"`
	Currency string  `funroute:"currency"`
	Risk     float64 `funroute:"risk"`
	Country  string  `funroute:"country"`
}

type benchIn struct {
	Order benchOrder `funroute:"order"`
}

type benchDecision struct {
	Channel string `funroute:"channel"`
	Net     int64  `funroute:"net"`
}

// BenchmarkRecordBoundary is where the typed path pays off: a struct in and a
// struct out. The untyped path reflects over both structs on every call.
func BenchmarkRecordBoundary(b *testing.B) {
	registry := benchRegistry(b)
	binding, err := compile.Bind[benchIn, benchDecision](registry)
	if err != nil {
		b.Fatal(err)
	}
	program, err := binding.Compile(`{channel: if(order.risk < 0.5, order.currency, "manual"), net: order.amount - 30}`)
	if err != nil {
		b.Fatal(err)
	}
	in := benchIn{Order: benchOrder{Amount: 1000, Currency: "SGD", Risk: 0.2, Country: "SG"}}
	b.Run("RunValues+FromValue", func(b *testing.B) {
		ctx := b.Context()
		b.ReportAllocs()
		for b.Loop() {
			order, err := machine.ToValue(in.Order)
			if err != nil {
				b.Fatal(err)
			}
			value, err := program.Runtime().RunValues(ctx, []machine.Value{order}, machine.RunOptions{})
			if err != nil {
				b.Fatal(err)
			}
			if _, err := machine.FromValue[benchDecision](value); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Program", func(b *testing.B) {
		ctx := b.Context()
		b.ReportAllocs()
		for b.Loop() {
			if _, err := program.Run(ctx, &in, machine.RunOptions{}); err != nil {
				b.Fatal(err)
			}
		}
	})
}

type feeIn struct {
	Amount  money.Money            `funroute:"amount"`
	Fee     money.Ratio            `funroute:"fee"`
	Settle  money.Currency         `funroute:"settle"`
	History []money.Money          `funroute:"history"`
	Caps    map[string]money.Money `funroute:"caps"`
	Rates   []money.FxRate         `funroute:"rates"`
}

type feeOut struct {
	Fee     money.Money   `funroute:"fee"`
	Settled money.Money   `funroute:"settled"`
	Shares  []money.Money `funroute:"shares"`
	Settle  string        `funroute:"settle"`
}

// Every money type's Go form crosses a typed binding in both directions,
// the containers of money and of exchange rates included: the rates a rule
// converts at are an argument like any other.
func TestProgramCarriesEveryMoneyType(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	binding, err := compile.Bind[feeIn, feeOut](registry)
	if err != nil {
		t.Fatal(err)
	}
	options := machine.RunOptions{}
	program, err := binding.Compile(`let(fee = round(amount * fee, @half_even), cap = caps[currency(amount)], {
		fee: if(fee > cap, cap, fee),
		settled: using(rates, round(amount -> settle, @half_even)),
		shares: allocate(amount, len(history)),
		settle: string(settle),
	})`)
	if err != nil {
		t.Fatal(err)
	}
	table, _ := registry.Currencies()
	eurJPY, err := table.FxRate("EUR", "JPY", "160")
	if err != nil {
		t.Fatal(err)
	}
	in := feeIn{
		Amount: machine.NewMoney("EUR", 10_000), Fee: machine.NewRatio(29, 1000),
		Settle:  machine.NewCurrency("JPY"),
		History: []money.Money{machine.NewMoney("EUR", 1), machine.NewMoney("EUR", 2), machine.NewMoney("EUR", 3)},
		Caps:    map[string]money.Money{"EUR": machine.NewMoney("EUR", 250)},
		Rates:   []money.FxRate{eurJPY},
	}
	out, err := program.Run(t.Context(), &in, options)
	want := feeOut{
		Fee: machine.NewMoney("EUR", 250), Settled: machine.NewMoney("JPY", 16_000),
		Shares: []money.Money{machine.NewMoney("EUR", 3334), machine.NewMoney("EUR", 3333), machine.NewMoney("EUR", 3333)},
		Settle: "JPY",
	}
	if err != nil || fmt.Sprint(out) != fmt.Sprint(want) {
		t.Fatalf("Run(%+v) = %+v, %v, want %+v", in, out, err, want)
	}
	in.History[1] = machine.NewMoney("USD", in.History[1].Minor())
	if _, err := program.Run(t.Context(), &in, options); err != nil {
		t.Fatalf("history is only counted, so its currencies are not the program's: %v", err)
	}
	in.Caps["EUR"] = machine.NewMoney("USD", 250)
	if _, err := program.Run(t.Context(), &in, options); !errors.Is(err, machine.ErrCurrency) {
		t.Fatalf("a cap in dollars against euros: error = %v, want ErrCurrency", err)
	}
}

type ratioIn struct {
	Fee    money.Ratio    `funroute:"fee"`
	Settle money.Currency `funroute:"settle"`
}

// A ratio and a currency are scalars at the typed boundary: nothing to box.
func TestProgramRatiosAndCurrenciesDoNotAllocate(t *testing.T) {
	binding, err := compile.Bind[ratioIn, money.Ratio](moneyRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`if(settle == JPY, fee * fee, fee)`)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	allocs := testing.AllocsPerRun(1000, func() {
		in := ratioIn{Fee: machine.NewRatio(1, 5), Settle: machine.NewCurrency("JPY")}
		out, err := program.Run(ctx, &in, machine.RunOptions{})
		if err != nil || out != machine.NewRatio(1, 25) {
			t.Fatalf("Run(%+v) = %v, %v, want 0.04", in, out, err)
		}
	})
	if allocs != 0 {
		t.Fatalf("Run allocated %v times, want 0", allocs)
	}
}

type unreadIn struct {
	N      int64          `funroute:"n"`
	Settle money.Currency `funroute:"settle"`
	Amount money.Money    `funroute:"amount"`
}

// A binding declares every field, and a program may leave some unread: the
// arguments it does not read are not converted, so they cannot be wrong.
func TestProgramLeavesUnreadCurrenciesAlone(t *testing.T) {
	t.Parallel()
	binding, err := compile.Bind[unreadIn, int64](moneyRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`n + 1`)
	if err != nil {
		t.Fatal(err)
	}
	in := unreadIn{N: 1, Settle: machine.NewCurrency("USD"), Amount: machine.NewMoney("USD", 1)}
	out, err := program.Run(t.Context(), &in, machine.RunOptions{})
	if err != nil || out != 2 {
		t.Fatalf("n + 1 on %+v = %d, %v, want 2", in, out, err)
	}
}
