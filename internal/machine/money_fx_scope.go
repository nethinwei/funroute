package machine

import (
	"context"
	"fmt"

	"github.com/nethinwei/funroute/internal/money"
)

// A using runs part of a program with the exchange rates it names — its
// quotes, each an fxrate or an array<fxrate> — and no others: rates outside
// the using take no part. OpFxPush moves the quotes off the stack onto the
// frame; OpFxPop drops them. A conversion inside reads the innermost using's
// quotes, a later one over an earlier, and a quote the other way round by
// its inverse. Nothing is built: the quotes are the values the rule gave,
// the host's []FxRate among them, so opening a using allocates nothing.

func validateFxPush(in Instruction, fail failFunc) error {
	if in.A != 0 || in.C != 0 || in.B < 1 || len(in.Keys) != 0 {
		return fail("fx_push takes B quotes, at least one, and nothing else, not A=%d B=%d C=%d", in.A, in.B, in.C)
	}
	return nil
}

// pushScope opens a using: its quotes are on the stack.
func (f *frame) pushScope(in Instruction) error {
	quotes, err := f.popN(in.B)
	if err != nil {
		return err
	}
	f.fxMarks = append(f.fxMarks, len(f.fxQuotes))
	f.fxQuotes = append(f.fxQuotes, quotes...)
	return nil
}

// popScope closes the innermost using.
func (f *frame) popScope() { f.dropScopes(len(f.fxMarks) - 1) }

// dropScopes closes every using past the first depth ones.
func (f *frame) dropScopes(depth int) {
	if depth >= len(f.fxMarks) {
		return
	}
	mark := f.fxMarks[depth]
	clear(f.fxQuotes[mark:])
	f.fxQuotes = f.fxQuotes[:mark]
	f.fxMarks = f.fxMarks[:depth]
}

// scopeQuotes is the innermost using's quotes; ok is false outside every
// using.
func (f *frame) scopeQuotes() ([]Value, bool) {
	if len(f.fxMarks) == 0 {
		return nil, false
	}
	return f.fxQuotes[f.fxMarks[len(f.fxMarks)-1]:], true
}

// quoteBetween is the rate from one currency to another among quotes: the
// last quote of the pair either way, a quote the other way by its inverse.
// found is false when no quote names the pair.
func quoteBetween(quotes []Value, from, to string) (money.FxRate, bool) {
	// Last first, by index: an iterator's closure is not free in every build,
	// and a conversion allocates nothing.
	for i := range quotes {
		quote := quotes[len(quotes)-1-i]
		if rates, ok := quote.box.([]money.FxRate); ok {
			if rate, found := lastBetween(rates, from, to); found {
				return rate, true
			}
			continue
		}
		if rate, _ := quote.FxRate(); matches(rate, from, to) {
			return rate, true
		}
	}
	return money.FxRate{}, false
}

// lastBetween is the last of rates that quotes the pair, either way.
func lastBetween(rates []money.FxRate, from, to string) (money.FxRate, bool) {
	for i := range rates {
		if rate := rates[len(rates)-1-i]; matches(rate, from, to) {
			return rate, true
		}
	}
	return money.FxRate{}, false
}

// matches reports a quote of the pair from→to, either way round.
func matches(rate money.FxRate, from, to string) bool {
	base, quote := rate.Base(), rate.Quote()
	if base == "" {
		return false
	}
	return base == from && quote == to || base == to && quote == from
}

// rateBetween is the exchange rate from→to the frame's innermost using has:
// the identity for one currency, otherwise its quote of the pair. Outside a
// using, or without a quote of the pair, it is ErrNoFxRate.
func (f *frame) rateBetween(table *money.Currencies, from, to string) (money.FxRate, error) {
	if from == to {
		pair, err := money.PairOf(table, from, to)
		return money.FxRateFrom(pair, money.RatioFromParts(1, 1)), err
	}
	quotes, _ := f.scopeQuotes()
	rate, found := quoteBetween(quotes, from, to)
	if !found {
		return money.FxRate{}, fmt.Errorf("%w: the using has no quote between %s and %s, either way", ErrNoFxRate, from, to)
	}
	if rate.Base() == from {
		return rate, nil
	}
	pair, err := money.PairOf(table, from, to)
	if err != nil {
		return money.FxRate{}, err
	}
	_, ratio := money.FxRateParts(rate)
	inverse, err := money.RatioFromParts(1, 1).Div(ratio)
	return money.FxRateFrom(pair, inverse), err
}

// fxScopeKey is the context key a conversion finds the frame under.
type fxScopeKey struct{}

// fxContext is the context a kernel function that reads exchange rates is
// called with: the run's, and the frame for the using it is inside. The
// frame holds one, so the call allocates nothing; nothing else is handed it,
// so no host code keeps it past the call.
type fxContext struct {
	context.Context
	frame *frame
}

func (c *fxContext) Value(key any) any {
	if _, ok := key.(fxScopeKey); ok {
		return c.frame
	}
	return c.Context.Value(key)
}

// scopeFrame is the frame a conversion runs in.
func scopeFrame(ctx context.Context) (*frame, error) {
	f, ok := ctx.Value(fxScopeKey{}).(*frame)
	if !ok {
		return nil, fmt.Errorf("%w: a conversion outside a using has no exchange rates", ErrNoFxRate)
	}
	return f, nil
}
