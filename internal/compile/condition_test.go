package compile

import (
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
)

// A condition tested by jumps answers, and fails, as the bool it stands for
// tested once: an operand is evaluated exactly when the ifs would, so a
// division by zero past a false && or a true || never happens.
func TestAConditionTestedByJumpsAnswersAsItsValue(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm, machine.SwitchForm); err != nil {
		t.Fatal(err)
	}
	options := CompileOptions{Args: []ArgSpec{
		{Name: "a", Type: machine.IntType}, {Name: "b", Type: machine.IntType}, {Name: "flag", Type: machine.BoolType},
		{Name: "xs", Type: machine.ArrayOf(machine.IntType)},
	}}
	atoms := []string{"a > 0", "10 / a > 2", "b == 3", "flag"}
	conditions := make([]string, 0, 5*len(atoms)*len(atoms))
	for _, x := range atoms {
		for _, y := range atoms {
			conditions = append(conditions, x+" && "+y, x+" || "+y, "!("+x+") && "+y, "!("+x+" || "+y+")", "("+x+" || "+y+") && !flag")
		}
	}
	for _, cond := range conditions {
		for jumps, value := range map[string]string{
			"if(" + cond + ", 1, 2)":                                     "let(v = " + cond + ", if(v, 1, 2))",
			"len([x for x in xs if " + cond + "])":                       "len([x for x in xs if let(v = " + cond + ", v)])",
			"switch(case " + cond + " => 1, case a > 1 => 2, else => 3)": "let(v = " + cond + ", switch(case v => 1, case a > 1 => 2, else => 3))",
		} {
			tested, valued := run(t, jumps, registry, options), run(t, value, registry, options)
			for _, args := range conditionInputs() {
				assertAnswersAlike(t, tested, valued, args)
			}
		}
	}
}

func conditionInputs() []map[string]any {
	inputs := make([]map[string]any, 0, 16)
	for _, a := range []int{-1, 0, 1, 5} {
		for _, b := range []int{3, 4} {
			for _, flag := range []bool{true, false} {
				inputs = append(inputs, map[string]any{"a": a, "b": b, "flag": flag, "xs": []any{1, 2}})
			}
		}
	}
	return inputs
}
