package std_test

import (
	"testing"

	"funroute/lang"
)

// The statistics and formatting a rule reaches for when it judges an SLA or
// writes a reconciliation line.
func TestStatisticsAndFormatting(t *testing.T) {
	t.Parallel()
	latencies := lang.ArgSpec{Name: "latencies", Type: lang.ArrayOf(lang.IntType)}
	args := map[string]any{"latencies": []any{100, 200, 300, 400}}
	for _, test := range []struct {
		name, source string
		specs        []lang.ArgSpec
		args         map[string]any
		want         any
	}{
		{"中位分位", `percentile(latencies, 0.5)`, []lang.ArgSpec{latencies}, args, 250.0},
		{"最低分位", `percentile(latencies, 0.0)`, []lang.ArgSpec{latencies}, args, 100.0},
		{"最高分位", `percentile(latencies, 1.0)`, []lang.ArgSpec{latencies}, args, 400.0},
		{"标准差", `stddev([2, 4, 4, 4, 5, 5, 7, 9])`, nil, nil, 2.0},
		{"整数幂", `pow(2, 10)`, nil, nil, int64(1024)},
		{"浮点幂", `pow(9.0, 0.5)`, nil, nil, 3.0},
		{"指数退避", `pow(2, 3) * 200`, nil, nil, int64(1600)},
		{"左补零", `pad_left("1234", 8, "0")`, nil, nil, "00001234"},
		{"右补点", `pad_right("adyen", 7, ".")`, nil, nil, "adyen.."},
		{"已够长就原样", `pad_left("123456", 3, "0")`, nil, nil, "123456"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := run(t, test.source, test.args, test.specs...)
			if err != nil {
				t.Fatalf("run %s: %v", test.source, err)
			}
			if got != test.want {
				t.Fatalf("%s = %v (%T), want %v", test.source, got, got, test.want)
			}
		})
	}
	for _, source := range []string{
		`percentile(latencies, 1.5)`,
		`pow(2, 0 - 1)`,
		`pad_left("1", 4, "ab")`,
		`stddev([n for n in latencies if n > 999])`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := run(t, source, args, latencies); err == nil {
				t.Fatalf("%s must fail", source)
			}
		})
	}
}
