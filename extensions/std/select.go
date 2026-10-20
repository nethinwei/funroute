package std

import (
	"context"
	"fmt"

	"github.com/nethinwei/funroute"
)

// whileSpecs cut a list where a run of trues ends. The test is the second
// array, same as every other pair here — "every item until the running total
// passes the cap" is take_while(amounts, [t <= cap for t in cumsum(amounts)]).
func whileSpecs() []funroute.FunctionSpec {
	list := funroute.ArrayOf(funroute.TypeVar("T"))
	specs := make([]funroute.FunctionSpec, 0, 2)
	for _, side := range []struct {
		name, label, description string
		prefix                   bool
	}{
		{"take_while", "取到不满足为止", "从头取，遇到第一个 false 就停，后面还有 true 也不要。判断数组必须与候选等长。", true},
		{"drop_while", "跳过开头满足的", "从头跳过 true，遇到第一个 false 就把剩下的全部返回。判断数组必须与候选等长。", false},
	} {
		prefix, name := side.prefix, side.name
		specs = append(specs, funroute.FunctionSpec{
			Name: name, Params: []funroute.Type{list, funroute.ArrayOf(funroute.BoolType)}, Result: list,
			Eval: func(ctx context.Context, args []funroute.Value) (funroute.Value, error) {
				return cutWhile(name, args, prefix)
			},
			Doc: funroute.Doc{
				Label: side.label, Category: "选择",
				Description: side.description,
				Params:      []string{"候选", "逐项判断"}, Result: "截取后的候选",
			},
		})
	}
	return specs
}

func cutWhile(name string, args []funroute.Value, prefix bool) (funroute.Value, error) {
	count, _ := args[0].Length()
	tests, _ := args[1].Length()
	if count != tests {
		return funroute.Value{}, fmt.Errorf("%w: %s has %d candidates and %d tests", funroute.ErrDomain, name, count, tests)
	}
	cut := count
	for i := range tests {
		test, _ := args[1].At(i)
		if keep, _ := test.Bool(); !keep {
			cut = i
			break
		}
	}
	if prefix {
		return part(args[0], 0, cut)
	}
	return part(args[0], cut, count)
}

// errNoExtreme is an arg_min or arg_max, or a money min or max, of nothing.
var errNoExtreme = fmt.Errorf("%w: an empty array has no extreme", funroute.ErrDomain)
