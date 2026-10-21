package money

import (
	"encoding/json"
	"errors"
	"testing"
)

// A currency's JSON is its code, and reads back to it; the zero Currency has
// no code, so it writes none rather than "", which would not read back.
func TestACurrencysJSONReadsBack(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(CurrencyOf("USD"))
	if err != nil || string(encoded) != `"USD"` {
		t.Fatalf("json.Marshal(USD) = %s, %v, want \"USD\"", encoded, err)
	}
	var back Currency
	if err := json.Unmarshal(encoded, &back); err != nil || back.Code() != "USD" {
		t.Fatalf("json.Unmarshal(%s) = %v, %v, want USD", encoded, back, err)
	}
	if encoded, err := json.Marshal(Currency{}); !errors.Is(err, ErrCurrency) {
		t.Errorf("json.Marshal(Currency{}) = %s, %v, want ErrCurrency", encoded, err)
	}
}
