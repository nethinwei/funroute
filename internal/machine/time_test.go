package machine

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// A time or a duration arrives by name as the text JSON writes it, and goes
// out the same way, inside containers too; a time past the years an int64
// of nanoseconds spans is refused.
func TestTimesCrossTheBoundaryAsText(t *testing.T) {
	t.Parallel()
	moment, err := coerce("2026-09-28T10:00:00.5+08:00", TimeType)
	if at, _ := moment.Time(); err != nil || !at.Equal(time.Date(2026, 9, 28, 2, 0, 0, 5e8, time.UTC)) {
		t.Fatalf("coerce time = %v, %v", at, err)
	}
	lengths, err := coerce([]any{"1h30m", "1.5s"}, ArrayOf(DurationType))
	if encoded, _ := json.Marshal(lengths); err != nil || string(encoded) != `["1h30m0s","1.5s"]` {
		t.Fatalf("coerce durations = %s, %v", encoded, err)
	}
	for _, bad := range []struct {
		input any
		typ   Type
	}{{"yesterday", TimeType}, {1.5, TimeType}, {"soon", DurationType}, {"3000-01-01T00:00:00Z", TimeType}} {
		if _, err := coerce(bad.input, bad.typ); err == nil {
			t.Errorf("coerce(%v, %s) succeeded", bad.input, bad.typ)
		}
	}
	if _, err := ToValue(time.Date(1600, 1, 1, 0, 0, 0, 0, time.UTC)); !errors.Is(err, ErrDomain) {
		t.Fatalf("ToValue of the year 1600 = %v, want ErrDomain", err)
	}
}
