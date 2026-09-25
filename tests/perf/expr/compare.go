package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	"github.com/nethinwei/funroute"
	"github.com/nethinwei/funroute/extensions/std"
)

// row is one scenario: the two sources and their times.
type row struct {
	name, expr   string
	ours, theirs measured
}

// scenario compiles, checks and measures one row against the registry.
type scenario func(*funroute.Registry) (row, error)

// group is a titled list of scenarios, one table in the report.
type group struct {
	title     string
	scenarios []scenario
}

// on is the scenario that runs ours in FunRoute and theirs in expr, both on
// in, FunRoute answering an Out.
func on[In, Out any](in *In, ours, theirs string) scenario {
	return func(registry *funroute.Registry) (row, error) {
		return compare[In, Out](registry, in, ours, theirs)
	}
}

// compare compiles both sources against in, checks that they answer alike,
// and measures a run of each.
func compare[In, Out any](registry *funroute.Registry, in *In, ours, theirs string) (row, error) {
	binding, err := funroute.Bind[In, Out](registry)
	if err != nil {
		return row{}, err
	}
	program, err := binding.Compile(ours)
	if err != nil {
		return row{}, fmt.Errorf("%s: %w", ours, err)
	}
	compiled, err := expr.Compile(theirs, append([]expr.Option{expr.Env(*in)}, hostFunctions...)...)
	if err != nil {
		return row{}, fmt.Errorf("%s: %w", theirs, err)
	}
	var machine vm.VM
	if err := sameAnswer(program, compiled, &machine, in); err != nil {
		return row{}, fmt.Errorf("%s / %s: %w", ours, theirs, err)
	}
	result := row{name: "`" + ours + "`", expr: "`" + theirs + "`"}
	result.ours = bench(func(ctx context.Context) error {
		_, err := program.Run(ctx, in)
		return err
	})
	result.theirs = bench(func(context.Context) error {
		_, err := machine.Run(compiled, *in)
		return err
	})
	return result, cmp.Or(result.ours.err, result.theirs.err)
}

// sameAnswer runs each side once and compares the answers through JSON:
// expr answers in its own types (a float for a division or a ceiling, []any
// for a list, a map for a record), and JSON reads both the same way.
func sameAnswer[In, Out any](program *funroute.Program[In, Out], compiled *vm.Program, machine *vm.VM, in *In) error {
	ours, err := program.Run(context.Background(), in)
	if err != nil {
		return err
	}
	theirs, err := machine.Run(compiled, *in)
	if err != nil {
		return err
	}
	left, err := normalized(ours)
	if err != nil {
		return err
	}
	right, err := normalized(theirs)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(left, right) {
		return fmt.Errorf("FunRoute answers %v, expr %v", left, right)
	}
	return nil
}

// normalized is value as JSON reads it back. expr keys its maps by any
// (fromPairs, groupBy), which JSON cannot write, so those keys are printed.
func normalized(value any) (any, error) {
	if keyed := reflect.ValueOf(value); keyed.Kind() == reflect.Map && keyed.Type().Key().Kind() == reflect.Interface {
		printed := make(map[string]any, keyed.Len())
		for key, item := range keyed.Seq2() {
			printed[fmt.Sprint(key.Interface())] = item.Interface()
		}
		value = printed
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out any
	return out, json.Unmarshal(data, &out)
}

// compiling is the scenario that measures compiling the same source on each
// side against In.
func compiling[In, Out any](ours, theirs string) scenario {
	return func(registry *funroute.Registry) (row, error) {
		binding, err := funroute.Bind[In, Out](registry)
		if err != nil {
			return row{}, err
		}
		var in In
		result := row{name: "`" + ours + "`", expr: "`" + theirs + "`"}
		result.ours = bench(func(context.Context) error {
			_, err := binding.Compile(ours)
			return err
		})
		result.theirs = bench(func(context.Context) error {
			_, err := expr.Compile(theirs, append([]expr.Option{expr.Env(in)}, hostFunctions...)...)
			return err
		})
		return result, cmp.Or(result.ours.err, result.theirs.err)
	}
}

// measure runs every group's scenarios in order and stops at the first that
// fails.
func measure(groups []group) ([][]row, error) {
	registry, err := newRegistry()
	if err != nil {
		return nil, err
	}
	tables := make([][]row, 0, len(groups))
	for _, group := range groups {
		rows := make([]row, 0, len(group.scenarios))
		for _, scenario := range group.scenarios {
			row, err := scenario(registry)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", group.title, err)
			}
			rows = append(rows, row)
		}
		tables = append(tables, rows)
	}
	return tables, nil
}

// newRegistry is the kernel with its forms, the standard pack and the host
// functions.
func newRegistry() (*funroute.Registry, error) {
	registry := funroute.CoreRegistry()
	if err := registry.EnableForm(funroute.SwitchForm, funroute.ForForm, funroute.ReduceForm); err != nil {
		return nil, err
	}
	if err := std.Register(registry); err != nil {
		return nil, err
	}
	for _, spec := range hostSpecs() {
		if err := registry.Register(spec); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// hostSpecs are the host functions FunRoute calls, registered by their Go
// signatures; hostFunctions are the same for expr.
func hostSpecs() []funroute.FunctionSpec {
	return []funroute.FunctionSpec{
		{Name: "host.add_v1", Go: hostAdd},
		{Name: "host.scale_v1", Go: hostScale},
		{Name: "host.label_v1", Go: hostLabel},
		{Name: "host.total_v1", Go: hostTotal},
	}
}

func hostAdd(a, b int64) int64             { return a + b }
func hostScale(x float64, n int64) float64 { return x * float64(n) }
func hostLabel(s string) string            { return "id:" + s }

func hostTotal(xs []float64) float64 {
	total := 0.0
	for _, x := range xs {
		total += x
	}
	return total
}
