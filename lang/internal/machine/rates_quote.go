package machine

import (
	"encoding/json"
	"time"
)

// QuoteSpec is a quote as a host adds it, with what it knows of it: where it
// came from, when it was quoted and until when it is meant to hold. It is an
// input, checked where AddQuote uses it.
type QuoteSpec struct {
	Base, Quote, Rate string
	Source            string
	At, Until         time.Time
}

// quoteMeta is what a table keeps of a QuoteSpec besides the rate.
type quoteMeta struct {
	source    string
	at, until time.Time
}

func (s QuoteSpec) meta() *quoteMeta {
	if s.Source == "" && s.At.IsZero() && s.Until.IsZero() {
		return nil
	}
	return &quoteMeta{source: s.Source, at: s.At, until: s.Until}
}

// Quote is a quote a rate table holds, as the host added it: the rate, and
// the source and times AddQuote recorded, zero where it was given none.
type Quote struct {
	rate      FxRate
	source    string
	at, until time.Time
}

// Rate is the quote's exchange rate, base to quote.
func (q Quote) Rate() FxRate { return q.rate }

// Source is where the host said the quote came from.
func (q Quote) Source() string { return q.source }

// At is when the host said the quote was made.
func (q Quote) At() time.Time { return q.at }

// Until is until when the host said the quote holds. The table does not
// act on it.
func (q Quote) Until() time.Time { return q.until }

func (q Quote) MarshalJSON() ([]byte, error) {
	if err := q.rate.checked(); err != nil {
		return nil, err
	}
	shape := struct {
		Base   string     `json:"base"`
		Quote  string     `json:"quote"`
		Rate   string     `json:"rate"`
		Source string     `json:"source,omitempty"`
		At     *time.Time `json:"at,omitempty"`
		Until  *time.Time `json:"until,omitempty"`
	}{Base: q.rate.base, Quote: q.rate.quote, Rate: rateText(q.rate.rate), Source: q.source}
	if !q.at.IsZero() {
		shape.At = &q.at
	}
	if !q.until.IsZero() {
		shape.Until = &q.until
	}
	return json.Marshal(shape)
}

// Quote is the quote the host added from base to quote in the table's
// current version, and false where it added none that way: an inverse the
// table works out, or a chain, is no quote.
func (r *Rates) Quote(base, quote string) (Quote, bool) {
	edge, ok := r.graph.Load().edges[base][quote]
	if !ok || !edge.quoted {
		return Quote{}, false
	}
	out := Quote{rate: FxRate{base: base, quote: quote, rate: edge.rate}}
	if edge.meta != nil {
		out.source, out.at, out.until = edge.meta.source, edge.meta.at, edge.meta.until
	}
	return out, true
}
