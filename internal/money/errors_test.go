package money

import (
	"context"
	"errors"
	"testing"
)

// A classified error is its class, keeps its cause behind it, and reads as
// the two joined.
func TestClassifyKeepsTheClassAndTheCause(t *testing.T) {
	t.Parallel()
	cause := context.DeadlineExceeded
	for name, test := range map[string]struct {
		detail, want string
	}{
		"no detail":   {"", "arithmetic failed: context deadline exceeded"},
		"with detail": {"args: ", "arithmetic failed: args: context deadline exceeded"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := Classify(ErrArithmetic, test.detail, cause)
			if got := err.Error(); got != test.want {
				t.Errorf("Classify(ErrArithmetic, %q, %v).Error() = %q, want %q", test.detail, cause, got, test.want)
			}
			if !errors.Is(err, ErrArithmetic) || !errors.Is(err, cause) {
				t.Errorf("Classify(ErrArithmetic, %q, %v) is not both its class and its cause", test.detail, cause)
			}
			if errors.Is(err, ErrCurrency) {
				t.Errorf("Classify(ErrArithmetic, %q, %v) is ErrCurrency, want only its own class", test.detail, cause)
			}
		})
	}
}
