package compile

import (
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

func TestRenderWithContract(t *testing.T) {
	t.Parallel()
	args := []ArgSpec{
		{Name: "amount", Type: machine.IntType, Doc: "订单金额，单位：分"},
		{Name: "country", Type: machine.StringType},
		{Name: "weights", Type: machine.DictOf(machine.FloatType), Doc: "渠道权重"},
	}
	result := machine.IntType
	text := RenderWithContract("let(bps = 250, amount * bps / 10000)", CompileOptions{Args: args, Result: &result, ResultDoc: "应收总额，单位：分"})
	t.Logf("\n%s", text)
	// The rendered text must still parse to the same program.
	bare, err := syntax.Parse("let(bps = 250, amount * bps / 10000)")
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := syntax.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := syntax.ExportExprJSON(bare)
	b, _ := syntax.ExportExprJSON(rendered)
	if string(a) != string(b) {
		t.Fatalf("comments changed the program:\n%s\n%s", a, b)
	}
}
