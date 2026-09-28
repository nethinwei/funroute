package api

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nethinwei/funroute"
)

type PaymentIn struct {
	PaidAt time.Time     `funroute:"paid_at"`
	Window time.Duration `funroute:"window"`
}

type PaymentOut struct {
	Due  time.Time `funroute:"due"`
	Hour int64     `funroute:"hour"`
	Late bool      `funroute:"late"`
}

// A host's time.Time and time.Duration are the language's time and duration,
// in a struct as on their own; the current time is an argument like any.
func TestAHostHandsOverTimes(t *testing.T) {
	t.Parallel()
	binding, err := funroute.Bind[PaymentIn, PaymentOut](funroute.CoreRegistry())
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`{due: paid_at + window, hour: hour(paid_at, "Asia/Shanghai"), late: window > 1h}`)
	if err != nil {
		t.Fatal(err)
	}
	paid := time.Date(2026, 9, 28, 10, 30, 0, 0, time.UTC)
	out, err := program.Run(t.Context(), &PaymentIn{PaidAt: paid, Window: 90 * time.Minute})
	if err != nil || !out.Due.Equal(paid.Add(90*time.Minute)) || out.Hour != 18 || !out.Late {
		t.Fatalf("Run = %+v, %v; want due 12:00 UTC, hour 18 and late", out, err)
	}
	if _, err := program.Run(t.Context(), &PaymentIn{PaidAt: time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)}); !errors.Is(err, funroute.ErrDomain) {
		t.Fatalf("a time past 2262 ran: %v, want ErrDomain", err)
	}
}

// Across JSON a time is RFC 3339 text and a duration Go's duration text, and
// ToValue and FromValue take Go's types.
func TestTimesCrossJSONAsText(t *testing.T) {
	t.Parallel()
	registry := funroute.CoreRegistry()
	artifact, err := funroute.CompileExpr(`[start + 2h30m, start - 1500ms]`, registry, funroute.CompileOptions{
		Args: []funroute.ArgSpec{{Name: "start", Type: funroute.TimeType}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := funroute.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := runtime.Run(t.Context(), map[string]any{"start": "2026-09-28T10:00:00+08:00"})
	if err != nil {
		t.Fatal(err)
	}
	if encoded, _ := json.Marshal(answer); string(encoded) != `["2026-09-28T04:30:00Z","2026-09-28T01:59:58.5Z"]` {
		t.Fatalf("the answer is %s", encoded)
	}
	window, err := funroute.ToValue(90 * time.Minute)
	if err != nil || !window.Type().Equal(funroute.DurationType) {
		t.Fatalf("ToValue(90m) = %v, %v", window.Type(), err)
	}
	if encoded, _ := json.Marshal(window); string(encoded) != `"1h30m0s"` {
		t.Fatalf("a duration's JSON is %s", encoded)
	}
	if back, err := funroute.FromValue[time.Duration](window); err != nil || back != 90*time.Minute {
		t.Fatalf("FromValue = %v, %v", back, err)
	}
	if length, ok := window.Duration(); !ok || length != 90*time.Minute {
		t.Fatalf("Duration() = %v, %v", length, ok)
	}
}
