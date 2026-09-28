package machine

import (
	"fmt"
	"reflect"
	"time"
)

// Time and duration. A time is an instant as nanoseconds since the Unix
// epoch, in no time zone; a duration is nanoseconds. Both are int64s, so a
// time spans the years 1678 to 2262, and both cross the boundary as Go's
// time.Time and time.Duration, and JSON as RFC 3339 text and Go's duration
// text. A zone is a question of the calendar, which a function answers for
// the zone it is named (lib_time.go).

var (
	timeGoType     = reflect.TypeFor[time.Time]()
	durationGoType = reflect.TypeFor[time.Duration]()
)

// timeValue is t as a time: false when it is outside the years a time spans.
func timeValue(t time.Time) (Value, error) {
	ns := t.UnixNano()
	if !time.Unix(0, ns).Equal(t) {
		return Value{}, fmt.Errorf("%w: %s is outside the years 1678 to 2262 a time holds", ErrDomain, t.Format(time.RFC3339))
	}
	return Value{kind: TimeKind, i: ns}, nil
}

// Duration is d as a duration.
func Duration(d time.Duration) Value { return Value{kind: DurationKind, i: int64(d)} }

// Time is a time's instant, in UTC.
func (v Value) Time() (time.Time, bool) { return time.Unix(0, v.i).UTC(), v.kind == TimeKind }

// Duration is a duration's length.
func (v Value) Duration() (time.Duration, bool) { return time.Duration(v.i), v.kind == DurationKind }

// coerceTimeKind reads what Run was handed by name for a time or a
// duration: the Go value, or the text JSON writes it as.
func coerceTimeKind(input any, expected Type) (Value, error) {
	text, ok := input.(string)
	switch {
	case !ok:
		return Value{}, fmt.Errorf("got %T, want %s text", input, expected)
	case expected.kind == TimeKind:
		t, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return Value{}, fmt.Errorf("time %q is not RFC 3339", text)
		}
		return timeValue(t)
	}
	d, err := time.ParseDuration(text)
	if err != nil {
		return Value{}, fmt.Errorf("duration %q is not a Go duration such as 1h30m", text)
	}
	return Duration(d), nil
}

// timeGoKind is the type of time.Time and time.Duration, which reflection
// would read as a record and an int.
func timeGoKind(typ reflect.Type) (Type, bool) {
	switch typ {
	case timeGoType:
		return TimeType, true
	case durationGoType:
		return DurationType, true
	}
	return Type{}, false
}
