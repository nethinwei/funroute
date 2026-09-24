package compile

// What the compiler does for money outside inference: the literals a context
// turned into ratios or money, the registry's two enums, the contract's money
// types, and the stamp an artifact carries.

import (
	"fmt"
	"slices"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
	"github.com/nethinwei/funroute/internal/syntax"
)

// literalValue is the value a literal compiles to: what it was written as,
// unless inference read a decimal or 0 as a ratio, or 0 as money. A decimal
// becomes a ratio from its digits, never through a float64.
func (c *bytecodeCompiler) literalValue(node *syntax.LiteralExpr) (machine.Value, error) {
	written := node.Value.Kind()
	kind := written
	if typ, ok := c.inferred.NodeTypes[node.ID]; ok {
		kind = typ.Kind()
	}
	switch {
	case kind == machine.MoneyKind:
		return machine.MoneyValue(0, ""), nil
	case kind == machine.RatioKind && written == machine.IntKind:
		return machine.RatioValue(money.Ratio{}), nil // only 0 may be read as a rate
	case kind == machine.RatioKind:
		ratio, err := node.Decimal.Ratio()
		if err != nil {
			return machine.Value{}, syntax.Around(node, "type error: %v", err)
		}
		return machine.RatioValue(ratio), nil
	case written == machine.FloatKind:
		return node.Float()
	}
	return node.Value, nil
}

// moneyLiteral is the value USD 1.70, 2.9% or 150 JPY / USD stands for,
// exactly: the amount in the currency's places, the ratio in its unit, the
// exchange rate as written. Too many places for an amount or a rate is a
// compile error rather than a rounding.
func moneyLiteral(expr syntax.Expr, registry *machine.Registry) (machine.Value, error) {
	if _, declared := registry.Money(); !declared {
		return machine.Value{}, syntax.Around(expr, "type error: this registry declares no money")
	}
	var value machine.Value
	var err error
	switch node := expr.(type) {
	case *syntax.MoneyExpr:
		value, err = machine.ParseMoneyAmount(registry, node.Currency, node.Amount)
	case *syntax.RatioExpr:
		var ratio money.Ratio
		ratio, err = money.ParseRatioIn(node.Value, node.Scale())
		value = machine.RatioValue(ratio)
	case *syntax.FxRateExpr:
		value, err = machine.FxRateLiteral(registry, node.Rate, node.Quote, node.Base)
	case *syntax.CurrencyExpr:
		table, _ := registry.Currencies()
		_, err = table.Currency(node.Code)
		value = machine.CurrencyValue(node.Code)
	default:
		return machine.Value{}, fmt.Errorf("internal error: %T is not a money literal", expr)
	}
	if err != nil {
		return machine.Value{}, syntax.Around(expr, "type error: %v", err)
	}
	return value, nil
}

// inferMoneyLiteral types a money or ratio literal by its value.
func inferMoneyLiteral(expr syntax.Expr, state *inferState, context inferContext) (typeTerm, error) {
	value, err := moneyLiteral(expr, context.registry)
	if err != nil {
		return typeTerm{}, err
	}
	return record(expr, state, state.concrete(value.Type())), nil
}

// enumValue is what @member compiles to: an enum's run-time value is its
// member.
func (c *bytecodeCompiler) enumValue(node *syntax.EnumExpr) machine.Value {
	return machine.String(node.Member)
}

// addRegistryEnums puts the registry's enums into the namespace @member
// resolves in, where money is declared: the rounding modes (@half_up) and
// the allocation strategies (@last).
func addRegistryEnums(enums map[string]machine.Type, registry *machine.Registry) {
	if _, ok := registry.Money(); ok {
		enums[machine.RoundingEnum] = machine.RoundingEnumType()
		enums[machine.AllocationEnum] = machine.AllocationEnumType()
	}
}

// EnumNamespace is everything @member resolves in for a program under the
// contract on the registry: the contract's enums and, with money declared,
// the rounding modes.
func EnumNamespace(options CompileOptions, registry *machine.Registry) map[string]machine.Type {
	enums := options.Enums()
	addRegistryEnums(enums, registry)
	return enums
}

// validateMoneyContract checks the contract's money types against the
// registry: money must be declared, every code it names too, the registry's
// enum names are not the contract's to take, and a currency variable in the
// result must be bound by an argument — or the run could not say which
// currency the result is in.
func validateMoneyContract(options CompileOptions, registry *machine.Registry) error {
	types := slices.Collect(func(yield func(machine.Type) bool) {
		for _, arg := range options.Args {
			yield(arg.Type)
		}
		if options.Result != nil {
			yield(*options.Result)
		}
	})
	_, declared := registry.Money()
	for _, typ := range types {
		if err := validateMoneyType(typ, declared); err != nil {
			return contractErrorf("%v", err)
		}
	}
	return nil
}

func validateMoneyType(typ machine.Type, declared bool) error {
	return machine.WalkTypes(typ, func(inner machine.Type) error {
		if inner.Kind() == machine.EnumKind && isRegistryEnum(inner.Name()) && declared {
			return fmt.Errorf("enum<%s> is the registry's; name the contract's enum something else", inner.Name())
		}
		if !machine.IsMoneyKind(inner.Kind()) {
			return nil
		}
		if !declared {
			return fmt.Errorf("%s needs a registry that declares money", inner)
		}
		return nil
	})
}
