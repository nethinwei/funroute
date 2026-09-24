package compile

// What the compiler does for money outside inference: the literals a context
// turned into rates or money, the registry's two enums, the contract's money
// types, and the stamp an artifact carries.

import (
	"fmt"
	"slices"
	"strconv"

	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
)

// literalValue is the value a literal compiles to: what it was written as,
// unless inference read a decimal or 0 as a rate, or 0 as money.
func (c *bytecodeCompiler) literalValue(node *syntax.LiteralExpr) (machine.Value, error) {
	typ, ok := c.inferred.NodeTypes[node.ID]
	if !ok || typ.Kind() == node.Value.Kind() {
		return node.Value, nil
	}
	switch typ.Kind() {
	case machine.RateKind:
		if node.Value.Kind() == machine.IntKind {
			return machine.RateValue(machine.Rate{}), nil // only 0 may be read as a rate
		}
		// Exact: a decimal literal is refused unless the float64 is what was
		// written (syntax.exactFloat), so its shortest text is the literal's.
		written, _ := node.Value.Float()
		rate, err := machine.ParseRate(strconv.FormatFloat(written, 'f', -1, 64))
		if err != nil {
			return machine.Value{}, syntax.Around(node, "type error: %v", err)
		}
		return machine.RateValue(rate), nil
	case machine.MoneyKind:
		return machine.MoneyValue(0, ""), nil
	default:
		return node.Value, nil
	}
}

// moneyLiteral is the value USD 1.70, 2.9% or 150 JPY / USD stands for,
// exactly: the amount in the currency's places, the rate in its unit, the
// exchange rate as written. Too many places for an amount or a rate is a
// compile error rather than a rounding.
func moneyLiteral(expr syntax.Expr, registry *machine.Registry) (machine.Value, error) {
	if _, declared := registry.Money(); !declared {
		return machine.Value{}, syntax.Around(expr, "type error: this registry declares no money")
	}
	switch node := expr.(type) {
	case *syntax.MoneyExpr:
		value, err := machine.ParseMoneyAmount(registry, node.Currency, node.Amount)
		if err != nil {
			return machine.Value{}, syntax.Around(node, "type error: %v", err)
		}
		return value, nil
	case *syntax.RateExpr:
		rate, err := machine.ParseRateIn(node.Value, node.Scale())
		if err != nil {
			return machine.Value{}, syntax.Around(node, "type error: %v", err)
		}
		return machine.RateValue(rate), nil
	case *syntax.FxRateExpr:
		value, err := machine.FxRateLiteral(registry, node.Rate, node.Quote, node.Base)
		if err != nil {
			return machine.Value{}, syntax.Around(node, "type error: %v", err)
		}
		return value, nil
	case *syntax.CurrencyExpr:
		table, _ := registry.Currencies()
		if _, err := table.Currency(node.Code); err != nil {
			return machine.Value{}, syntax.Around(node, "type error: %v", err)
		}
		return machine.CurrencyValue(node.Code), nil
	default:
		return machine.Value{}, fmt.Errorf("internal error: %T is not a money literal", expr)
	}
}

// inferMoneyLiteral types a money or rate literal by its value.
func inferMoneyLiteral(expr syntax.Expr, state *inferState, context inferContext) ([]inferResult, error) {
	value, err := moneyLiteral(expr, context.registry)
	if err != nil {
		return nil, err
	}
	return record(expr, []inferResult{{typ: state.concrete(value.Type()), state: state}}), nil
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
		if err := validateMoneyType(typ, registry, declared); err != nil {
			return contractErrorf("%v", err)
		}
	}
	if len(options.RateTables) > 0 && !declared {
		return contractErrorf("rate tables need a registry that declares money")
	}
	if options.Result == nil {
		return nil
	}
	params := make([]machine.Parameter, len(options.Args))
	for i, arg := range options.Args {
		params[i] = machine.NewParameter(arg.Name, arg.Type, "")
	}
	bound := machine.UnitVariables(params)
	for _, unit := range machine.UnitVariables([]machine.Parameter{machine.NewParameter("", *options.Result, "")}) {
		if !slices.Contains(bound, unit) {
			return contractErrorf("the result is in currency %s, which no argument binds", unit)
		}
	}
	return nil
}

func validateMoneyType(typ machine.Type, registry *machine.Registry, declared bool) error {
	return machine.WalkTypes(typ, func(inner machine.Type) error {
		if inner.Kind() == machine.EnumKind && isRegistryEnum(inner.Name()) && (declared || inner.Name() == machine.RateTableEnum) {
			return fmt.Errorf("enum<%s> is the registry's; name the contract's enum something else", inner.Name())
		}
		if !machine.IsMoneyKind(inner.Kind()) {
			return nil
		}
		if !declared {
			return fmt.Errorf("%s needs a registry that declares money", inner)
		}
		return declaredCodes(inner, registry)
	})
}

// declaredCodes checks that every code a type names is a declared currency.
func declaredCodes(typ machine.Type, registry *machine.Registry) error {
	for _, unit := range typ.Units() {
		if !machine.IsCurrencyCode(unit) {
			continue
		}
		if err := machine.DeclaredCurrency(registry, unit); err != nil {
			return err
		}
	}
	return nil
}
