package limits

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/nethinwei/funroute"
)

// computedTables are the tables whose cells are found rather than run once:
// by a binary search, by a count, by checking an edge from both sides.
var computedTables = map[string]func(*testing.T, *funroute.Registry) string{
	"one-step": oneStepTable,
	"round":    roundTable,
	"program":  programTable,
	"catalog":  catalogTable,
}

// usd writes minor units of USD as an amount.
func usd(minor int64) string { return fmt.Sprintf("USD %d.%02d", minor/100, minor%100) }

// scientific writes x as 3.18×10¹⁵.
func scientific(x float64) string {
	exponent := int(math.Floor(math.Log10(x)))
	superscript := strings.NewReplacer("0", "⁰", "1", "¹", "2", "²", "3", "³", "4", "⁴", "5", "⁵", "6", "⁶", "7", "⁷", "8", "⁸", "9", "⁹", "-", "⁻")
	return fmt.Sprintf("%.2f×10%s", x/math.Pow(10, float64(exponent)), superscript.Replace(strconv.Itoa(exponent)))
}

// largest is the largest amount in USD minor units source takes as m. The
// ones measured here only fail past a point, so a binary search finds it.
func largest(t *testing.T, registry *funroute.Registry, source string) int64 {
	t.Helper()
	artifact, err := funroute.CompileExpr(source, registry, options(t, "m: money"))
	if err != nil {
		t.Fatal(err)
	}
	low, high := int64(1), int64(math.MaxInt64)
	for low < high {
		middle := low + (high-low+1)/2
		if _, err := run(t, registry, artifact, `{"m": "`+usd(middle)+`"}`); err == nil {
			low = middle
		} else {
			high = middle - 1
		}
	}
	return low
}

func oneStepTable(t *testing.T, registry *funroute.Registry) string {
	t.Helper()
	var out strings.Builder
	out.WriteString("| 写法 | 最大的 USD 金额 | 约 |\n|---|---|---|\n")
	for _, source := range []string{
		"mul(m, 2.9%, @half_even)",
		"using(150.25 JPY / USD, convert(m, JPY, @half_even))",
		"m + m",
	} {
		minor := largest(t, registry, source)
		fmt.Fprintf(&out, "| `%s` | %s | USD %s |\n", source, usd(minor), scientific(float64(minor)/100))
	}
	return out.String()
}

// roundTable states the bound an exact step in round is sure to take — the
// largest int64 over the reduced numerator of what it multiplies by — and
// checks the bound and the amounts just under it.
func roundTable(t *testing.T, registry *funroute.Registry) string {
	t.Helper()
	var out strings.Builder
	out.WriteString("| 表达式 | 约分后的分子 | 保证可用到 |\n|---|---|---|\n")
	for _, c := range []struct {
		source, factor string
		numerator      int64
	}{
		{"round(m * 2.9%, @half_even)", "29/1000", 29},
		{"using(150.25 JPY / USD, round(m -> JPY, @half_even))", "601/400（150.25，JPY 没有小数位）", 601},
		{"using(0.92 EUR / USD, round(m -> EUR, @half_even))", "23/25", 23},
	} {
		bound := math.MaxInt64 / c.numerator
		for _, under := range []int64{0, 1, 2, 3, 5, 7, 11, 13, 1000} {
			if !succeeds(t, registry, c.source, "m: money", `{"m": "`+usd(bound-under)+`"}`) {
				t.Fatalf("%s fails on %s, under the bound it is sure to take", c.source, usd(bound-under))
			}
		}
		fmt.Fprintf(&out, "| `%s` | %s | USD %s |\n", c.source, c.factor, scientific(float64(bound)/100))
	}
	return out.String()
}
