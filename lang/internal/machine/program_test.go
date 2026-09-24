package machine_test

import (
	"errors"
	"fmt"
	"testing"

	"funroute/lang/internal/compile"
	"funroute/lang/internal/machine"
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
	Amount  machine.Money            `funroute:"amount"`
	Fee     machine.Rate             `funroute:"fee"`
	Settle  machine.Currency         `funroute:"settle"`
	History []machine.Money          `funroute:"history"`
	Caps    map[string]machine.Money `funroute:"caps"`
}

type feeOut struct {
	Fee     machine.Money   `funroute:"fee"`
	Settled machine.Money   `funroute:"settled"`
	Shares  []machine.Money `funroute:"shares"`
	Settle  string          `funroute:"settle"`
}

// Every money type's Go form crosses a typed binding in both directions,
// the containers of money included.
func TestProgramCarriesEveryMoneyType(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	binding, err := compile.Bind[feeIn, feeOut](registry)
	if err != nil {
		t.Fatal(err)
	}
	options := machine.RunOptions{Rates: moneyRates(t, registry, "EUR", "JPY", "160")}
	program, err := binding.Compile(`let(fee = amount * fee, cap = caps[currency(amount)], {
		fee: if(fee > cap, cap, fee),
		settled: amount -> settle,
		shares: allocate(amount, len(history)),
		settle: string(settle),
	})`)
	if err != nil {
		t.Fatal(err)
	}
	in := feeIn{
		Amount: machine.NewMoney("EUR", 10_000), Fee: machine.NewRate(290_000_000),
		Settle:  machine.NewCurrency("JPY"),
		History: []machine.Money{machine.NewMoney("EUR", 1), machine.NewMoney("EUR", 2), machine.NewMoney("EUR", 3)},
		Caps:    map[string]machine.Money{"EUR": machine.NewMoney("EUR", 250)},
	}
	out, err := program.Run(t.Context(), &in, options)
	want := feeOut{
		Fee: machine.NewMoney("EUR", 250), Settled: machine.NewMoney("JPY", 16_000),
		Shares: []machine.Money{machine.NewMoney("EUR", 3334), machine.NewMoney("EUR", 3333), machine.NewMoney("EUR", 3333)},
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

type rateIn struct {
	Fee    machine.Rate     `funroute:"fee"`
	Settle machine.Currency `funroute:"settle"`
}

// A rate and a currency are scalars at the typed boundary: nothing to box.
func TestProgramRatesAndCurrenciesDoNotAllocate(t *testing.T) {
	binding, err := compile.Bind[rateIn, machine.Rate](moneyRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`if(settle == JPY, fee * fee, fee)`)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	allocs := testing.AllocsPerRun(1000, func() {
		in := rateIn{Fee: machine.NewRate(2_000_000_000), Settle: machine.NewCurrency("JPY")}
		out, err := program.Run(ctx, &in, machine.RunOptions{})
		if err != nil || out != machine.NewRate(400_000_000) {
			t.Fatalf("Run(%+v) = %v, %v, want 0.04", in, out, err)
		}
	})
	if allocs != 0 {
		t.Fatalf("Run allocated %v times, want 0", allocs)
	}
}

type unreadIn struct {
	N      int64            `funroute:"n"`
	Settle machine.Currency `funroute:"settle"`
	Amount machine.Money    `funroute:"amount"`
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
