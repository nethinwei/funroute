package kit

import (
	"context"
	"errors"
	"testing"
)

var errClass, errOther = errors.New("arithmetic failed"), errors.New("currency mismatch")

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
			err := Classify(errClass, test.detail, cause)
			if got := err.Error(); got != test.want {
				t.Errorf("Classify(errClass, %q, %v).Error() = %q, want %q", test.detail, cause, got, test.want)
			}
			if !errors.Is(err, errClass) || !errors.Is(err, cause) {
				t.Errorf("Classify(errClass, %q, %v) is not both its class and its cause", test.detail, cause)
			}
			if errors.Is(err, errOther) {
				t.Errorf("Classify(errClass, %q, %v) is errOther, want only its own class", test.detail, cause)
			}
		})
	}
}

// Errorf reads as its class and the message, and is its class.
func TestErrorfIsItsClass(t *testing.T) {
	t.Parallel()
	err := Errorf(errClass, "int %d cannot be %s", 3, "x")
	if got, want := err.Error(), "arithmetic failed: int 3 cannot be x"; got != want {
		t.Errorf("Errorf(errClass, …).Error() = %q, want %q", got, want)
	}
	if !errors.Is(err, errClass) {
		t.Errorf("Errorf(errClass, …) is not errClass")
	}
}
