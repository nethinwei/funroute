package machine

import (
	"encoding/json"
	"testing"
	"time"
)

// A quote keeps what the host said of it — its source, when it was made and
// until when — and hands it back as it was added: in the direction added,
// the metadata of the latest. The inverse the table works out is no quote.
func TestAQuoteKeepsWhatTheHostSaidOfIt(t *testing.T) {
	t.Parallel()
	rates := rateCurrencies(t).NewRates()
	at := time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)
	spec := QuoteSpec{Base: "USD", Quote: "JPY", Rate: "150.25", Source: "ecb", At: at, Until: at.Add(time.Minute)}
	if err := rates.AddQuote(spec); err != nil {
		t.Fatal(err)
	}
	quote, ok := rates.Quote("USD", "JPY")
	if !ok || quote.Rate().String() != "USD/JPY 150.25" || quote.Source() != "ecb" || !quote.At().Equal(at) || !quote.Until().Equal(at.Add(time.Minute)) {
		t.Fatalf("Quote(USD, JPY) = %+v, %v, want the quote as added", quote, ok)
	}
	if _, ok := rates.Quote("JPY", "USD"); ok {
		t.Fatal("Quote(JPY, USD) found the inverse, want no quote that way")
	}
	if err := rates.Add("USD", "JPY", "151"); err != nil {
		t.Fatal(err)
	}
	if again, _ := rates.Quote("USD", "JPY"); again.Source() != "" || again.Rate().String() != "USD/JPY 151" {
		t.Fatalf("after Add, Quote(USD, JPY) = %+v, want the new rate and no metadata", again)
	}
}

// A quote past its Until converts all the same: the table records the time,
// the host decides what is stale.
func TestAStaleQuoteStillConverts(t *testing.T) {
	t.Parallel()
	currencies := rateCurrencies(t)
	rates := currencies.NewRates()
	past := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := rates.AddQuote(QuoteSpec{Base: "USD", Quote: "JPY", Rate: "150", At: past, Until: past}); err != nil {
		t.Fatal(err)
	}
	got, err := rates.Convert(Money{currency: "USD", minor: 100}, "JPY", RoundHalfUp)
	if err != nil || got.minor != 150 {
		t.Fatalf("Convert(USD 1.00) past Until = %v, %v, want JPY 150", got, err)
	}
}

// A quote's JSON is the rate's with what the host said of it, the times
// left out where none were given.
func TestAQuotesJSON(t *testing.T) {
	t.Parallel()
	rates := rateCurrencies(t).NewRates()
	at := time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)
	if err := rates.AddQuote(QuoteSpec{Base: "USD", Quote: "JPY", Rate: "150", Source: "ecb", At: at}); err != nil {
		t.Fatal(err)
	}
	quote, _ := rates.Quote("USD", "JPY")
	encoded, err := json.Marshal(quote)
	if want := `{"base":"USD","quote":"JPY","rate":"150","source":"ecb","at":"2026-09-24T08:00:00Z"}`; err != nil || string(encoded) != want {
		t.Fatalf("json.Marshal(quote) = %s, %v, want %s", encoded, err, want)
	}
	if _, err := json.Marshal(Quote{}); err == nil {
		t.Fatal("json.Marshal(Quote{}) = nil error, want the zero quote refused")
	}
}
