package machine_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

// What has no answer is an error of its class: a zone that is none or the
// machine's own, a day count past the years a time holds, text that is no
// time — RFC 3339's fraction is after a point and a time holds nanoseconds,
// not cut there — and nanoseconds past an int64.
func TestTimesFailWhereTheyHaveNoAnswer(t *testing.T) {
	t.Parallel()
	specs := []compile.ArgSpec{
		{Name: "t", Type: machine.TimeType}, {Name: "d", Type: machine.DurationType}, {Name: "s", Type: machine.StringType},
	}
	args := map[string]any{"t": "2262-01-01T00:00:00Z", "d": "2562047h", "s": "yesterday"}
	for source, want := range map[string]error{
		`hour(t, "Local")`:                        machine.ErrDomain,
		`hour(t, "")`:                             machine.ErrDomain,
		`weekday(t, "Mars/Olympus")`:              machine.ErrDomain,
		`add_days(t, 200000, "UTC")`:              machine.ErrDomain,
		`add_days(t, 365, "UTC")`:                 machine.ErrDomain,
		`time(s)`:                                 machine.ErrDomain,
		`time("2026-01-01T00:00:00,123Z")`:        machine.ErrDomain,
		`time("2026-01-01T00:00:00.1234567891Z")`: machine.ErrDomain,
		`t + d`: machine.ErrArithmetic,
		`d * 2`: machine.ErrArithmetic,
		`d / 0`: machine.ErrArithmetic,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := libRun(t, source, args, specs...); !errors.Is(err, want) {
				t.Fatalf("%s = %v, want %v", source, err, want)
			}
		})
	}
}

// A time zone's rules are the machine's tzdata, so a call that reads them is
// left to the run; reading a time from its text folds.
func TestZonedCallsAreNotFolded(t *testing.T) {
	t.Parallel()
	artifact, err := compile.CompileExpr(`hour(time("2026-09-28T18:30:00Z"), "Asia/Shanghai")`, machine.CoreRegistry(), compile.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(artifact)
	var called struct {
		Calls []machine.CallReference `json:"calls"`
	}
	if err := json.Unmarshal(encoded, &called); err != nil || len(called.Calls) != 1 || called.Calls[0].Name != "hour" {
		t.Fatalf("the artifact calls %v, want hour alone, time folded", called.Calls)
	}
	runtime, err := machine.Instantiate(artifact, machine.CoreRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if hour, err := runtime.Run(t.Context(), nil); err != nil || hour.Any() != int64(2) {
		t.Fatalf("the run answered %v, %v; want 2", hour.Any(), err)
	}
}

// A change of clock at midnight does not move a day into the one before: a
// day starts at its first instant, and a wall a change skips is read past the
// gap by as much as it is into it, one it repeats as the earlier instant.
func TestCalendarsReadAcrossChangesOfClock(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		// Santiago and Beirut skip their midnight, Havana repeats it.
		`start_of_day(time("2026-09-06T12:00:00Z"), "America/Santiago")`:    "2026-09-06T04:00:00Z",
		`start_of_day(time("2026-03-29T12:00:00Z"), "Asia/Beirut")`:         "2026-03-28T22:00:00Z",
		`start_of_day(time("2026-11-01T12:00:00Z"), "America/Havana")`:      "2026-11-01T04:00:00Z",
		`add_days(time("2026-03-07T05:30:00Z"), 1, "America/Havana")`:       "2026-03-08T05:30:00Z",
		`add_days(time("2026-03-07T07:30:00Z"), 1, "America/New_York")`:     "2026-03-08T07:30:00Z",
		`add_days(time("2026-10-31T05:30:00Z"), 1, "America/New_York")`:     "2026-11-01T05:30:00Z",
		`add_days(time("2026-11-01T06:30:00Z"), 1, "America/New_York")`:     "2026-11-02T06:30:00Z",
		`add_days(time("2026-11-01T12:34:56.789Z"), -400, "Asia/Shanghai")`: "2025-09-27T12:34:56.789Z",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			got, err := libRun(t, source, nil)
			wanted, _ := time.Parse(time.RFC3339Nano, want)
			if at, ok := got.(time.Time); err != nil || !ok || !at.Equal(wanted) {
				t.Fatalf("%s = %v, %v; want %s", source, got, err, want)
			}
		})
	}
}
