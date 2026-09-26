// Package std is the standard pack: the functions a payment rule writes
// less often than the kernel's library — sliding windows and batches, set
// differences, padding, grouping and ranking, cutting a list where a test
// stops holding — and the money overloads of the library's aggregates and
// selections, for a registry that declares money. It is written against the
// public package only, as any host's extension is.
package std

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/nethinwei/funroute"
)

var errIntegerOverflow = fmt.Errorf("%w: integer overflow in sum", funroute.ErrArithmetic)

// Register adds the pack to a registry. The order is the pack's: the order a
// name's overloads are registered in is the order the compiler tries them,
// and the money overloads, registered only when the registry declares money,
// come after all the others.
func Register(registry *funroute.Registry) error {
	if registry == nil {
		return errors.New("registry is required")
	}
	specs := slices.Concat(paddingSpecs(), sequenceSpecs(), whileSpecs(), groupSpecs())
	if _, declared := registry.Money(); declared {
		specs = append(specs, moneySpecs()...)
	}
	for _, spec := range specs {
		// Everything in the pack depends only on its arguments, so the
		// compiler may fold any call to it; a name's examples go with every
		// overload of it.
		spec.Doc.Constexpr = true
		spec.Doc.Examples = examples[spec.Name]
		if err := registry.Register(spec); err != nil {
			return err
		}
	}
	return nil
}

func sumInts(items []int64) (int64, error) {
	total := int64(0)
	for _, item := range items {
		if item > 0 && total > math.MaxInt64-item || item < 0 && total < math.MinInt64-item {
			return 0, errIntegerOverflow
		}
		total += item
	}
	return total, nil
}

// logic is a Go function as a spec.
func logic(name string, doc funroute.Doc, fn any) funroute.FunctionSpec {
	return funroute.FunctionSpec{Name: name, Doc: doc, Go: fn}
}

// eachType registers one name for every element type it serves. The language
// has no type classes, so a function that works on int, float and string is
// three registrations — that is the signature, not repetition. What it buys
// is a single place to read how many types a name covers.
func eachType(name string, doc funroute.Doc, implementations ...any) []funroute.FunctionSpec {
	specs := make([]funroute.FunctionSpec, len(implementations))
	for i, implementation := range implementations {
		specs[i] = logic(name, doc, implementation)
	}
	return specs
}
