package machine

import (
	"testing"

	"github.com/nethinwei/funroute/internal/money"
)

// An amount between minor units has no money.Money to be: handed to the
// host's Go, it is refused rather than read as that many minor units.
func TestAnExactAmountHasNoGoForm(t *testing.T) {
	t.Parallel()
	plain, ok, err := intoOwnGo(MoneyValue(150, "USD"), moneyGoType)
	if !ok || err != nil || plain.Interface() != money.Make("USD", 150) {
		t.Fatalf("intoOwnGo(USD 1.50) = %v, %v, %v, want USD 1.50", plain, ok, err)
	}
	half := exactValue(money.ExactFrom("USD", ratio(1, 2)))
	if got, ok, err := intoOwnGo(half, moneyGoType); !ok || err == nil {
		t.Fatalf("intoOwnGo(half a cent) = %v, %v, %v, want a refusal", got, ok, err)
	}
}
