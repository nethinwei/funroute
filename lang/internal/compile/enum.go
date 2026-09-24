package compile

import (
	"fmt"
	"slices"
	"strings"

	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
)

// collectEnums builds the contract's enum namespace. Enums only ever enter a
// program through the host contract, so this map is the whole set a bare
// @member can refer to — which is why resolution needs no type context.
func collectEnums(dst map[string]machine.Type, hints map[string]machine.Type, ret *machine.Type) error {
	for _, hint := range hints {
		if err := collectEnum(dst, hint); err != nil {
			return err
		}
	}
	if ret == nil {
		return nil
	}
	return collectEnum(dst, *ret)
}

// Enums is the contract's enum namespace, by name: what an @member in a
// program under it resolves in. A contract ValidateContract accepted has no
// two enums under one name, so there is no conflict to report here.
func (o CompileOptions) Enums() map[string]machine.Type {
	enums := map[string]machine.Type{}
	_ = collectEnums(enums, o.argTypes(), o.Result)
	addTableEnum(enums, o.RateTables)
	return enums
}

// addTableEnum puts the contract's rate tables into the namespace @member
// resolves in, as the members of rate_table.
func addTableEnum(enums map[string]machine.Type, tables []string) {
	if len(tables) > 0 {
		enums[machine.RateTableEnum] = machine.EnumOf(machine.RateTableEnum, tables...)
	}
}

// collectEnum registers every enum the type holds, at any depth — an element,
// a record field, a field of a field. A host that passes the whole order in
// declares its channel enum there, not as a separate argument.
func collectEnum(dst map[string]machine.Type, typ machine.Type) error {
	return machine.WalkTypes(typ, func(inner machine.Type) error {
		if inner.Kind() != machine.EnumKind {
			return nil
		}
		if existing, ok := dst[inner.Name()]; ok && !existing.Equal(inner) {
			return fmt.Errorf("the contract declares %s and %s under the same name", existing.Summary(), inner.Summary())
		}
		dst[inner.Name()] = machine.CloneType(inner)
		return nil
	})
}

// resolveEnumReference decides which enum @member belongs to. A qualified
// @channel.adyen names its enum; a bare @adyen must match exactly one member
// set in the contract, and an ambiguous one asks for the qualified form.
func resolveEnumReference(node *syntax.EnumExpr, enums map[string]machine.Type) (machine.Type, error) {
	if node.Enum != "" {
		return resolveQualifiedEnum(node, enums)
	}
	var found []string
	for name, typ := range enums {
		if slices.Contains(typ.Values(), node.Member) {
			found = append(found, name)
		}
	}
	slices.Sort(found)
	// The contract's own enums come first: declaring money must not make a
	// rule that already wrote @last for a contract member ambiguous. The
	// registry's strategy is then @allocation.last.
	if contract := slices.DeleteFunc(slices.Clone(found), isRegistryEnum); len(contract) > 0 {
		found = contract
	}
	switch len(found) {
	case 1:
		return enums[found[0]], nil
	case 0:
		return machine.Type{}, syntax.Around(node, "type error: %s is not a member of any enum in this contract%s",
			node.Source(), declaredEnums(enums))
	default:
		return machine.Type{}, syntax.Around(node, "type error: %s is ambiguous; it is a member of %s, so write @%s.%s",
			node.Source(), strings.Join(found, " and "), found[0], node.Member)
	}
}

// isRegistryEnum reports an enum the language brings rather than a type of
// the contract: the rounding modes and the allocation strategies money
// brings, and the rate tables. A contract's own enum member comes first.
func isRegistryEnum(name string) bool {
	return name == machine.RoundingEnum || name == machine.AllocationEnum || name == machine.RateTableEnum
}

func resolveQualifiedEnum(node *syntax.EnumExpr, enums map[string]machine.Type) (machine.Type, error) {
	typ, ok := enums[node.Enum]
	if !ok {
		return machine.Type{}, syntax.Around(node, "type error: the contract declares no enum named %q%s",
			node.Enum, declaredEnums(enums))
	}
	if !slices.Contains(typ.Values(), node.Member) {
		return machine.Type{}, syntax.Around(node, "type error: %q is not a member of %s", node.Member, typ.Summary())
	}
	return typ, nil
}

func declaredEnums(enums map[string]machine.Type) string {
	if len(enums) == 0 {
		return "; the contract declares no enum"
	}
	names := make([]string, 0, len(enums))
	for _, typ := range enums {
		names = append(names, typ.Summary())
	}
	slices.Sort(names)
	return "; the contract declares " + strings.Join(names, ", ")
}

// validateEnumResult proves every enum-bearing part of the declared result.
// It also pushes the declared type into container-producing nodes, so their
// bytecode builds array<enum>/dict<enum> rather than a plain string container.
func validateEnumResult(expr syntax.Expr, inferred *inference, registry *machine.Registry) error {
	if !containsEnum(inferred.Result) {
		return nil
	}
	if err := validateConstrainedReturn(expr, inferred.Result, inferred, registry); err != nil {
		return err
	}
	inferred.NodeTypes[expr.NodeID()] = machine.CloneType(inferred.Result)
	return nil
}

func containsEnum(typ machine.Type) bool {
	return machine.TypeContains(typ, machine.EnumKind)
}

func validateConstrainedReturn(expr syntax.Expr, expected machine.Type, inferred *inference, registry *machine.Registry) error {
	switch node := expr.(type) {
	case *syntax.CallExpr:
		return validateConstrainedCall(node, expected, inferred, registry)
	case *syntax.SwitchExpr:
		return validateConstrainedSwitch(node, expected, inferred, registry)
	case *syntax.LetExpr:
		return validateAndSet(node, node.Body, expected, inferred, registry)
	case *syntax.UsingExpr:
		return validateAndSet(node, node.Body, expected, inferred, registry)
	case *syntax.ReduceExpr:
		if err := validateConstrainedReturn(node.Init, expected, inferred, registry); err != nil {
			return err
		}
		return validateAndSet(node, node.Body, expected, inferred, registry)
	case *syntax.ArrayExpr:
		return validateConstrainedArray(node, expected, inferred, registry)
	case *syntax.DictExpr:
		return validateConstrainedDict(node, expected, inferred, registry)
	case *syntax.ForExpr:
		return validateConstrainedFor(node, expected, inferred, registry)
	default:
		// A node with no case here is checked whole: its inferred type has to
		// be the expected one. That is the conservative answer — it rejects a
		// program this walk could have proven rather than letting one through
		// — so a new node type costs precision here, never soundness.
		return validateKnownType(expr, expected, inferred)
	}
}

func validateConstrainedCall(node *syntax.CallExpr, expected machine.Type, inferred *inference, registry *machine.Registry) error {
	key, ok := inferred.Selections[node.ID]
	function, resolved := registry.Resolve(key)
	if ok && resolved && function.IsLazyIf() && len(node.Args) == 3 {
		return validatePair(node, node.Args[1], node.Args[2], expected, inferred, registry)
	}
	if ok && resolved && function.IsLazyFallback() && len(node.Args) >= 2 {
		for _, candidate := range node.Args {
			if err := validateConstrainedReturn(candidate, expected, inferred, registry); err != nil {
				return err
			}
		}
		inferred.NodeTypes[node.ID] = machine.CloneType(expected)
		return nil
	}
	return validateKnownType(node, expected, inferred)
}

func validatePair(parent syntax.Expr, left, right syntax.Expr, expected machine.Type, inferred *inference, registry *machine.Registry) error {
	if err := validateConstrainedReturn(left, expected, inferred, registry); err != nil {
		return err
	}
	return validateAndSet(parent, right, expected, inferred, registry)
}

func validateConstrainedSwitch(node *syntax.SwitchExpr, expected machine.Type, inferred *inference, registry *machine.Registry) error {
	for _, item := range node.Cases {
		if err := validateConstrainedReturn(item.Result, expected, inferred, registry); err != nil {
			return err
		}
	}
	if node.Default != nil {
		if err := validateConstrainedReturn(node.Default, expected, inferred, registry); err != nil {
			return err
		}
	}
	inferred.NodeTypes[node.ID] = machine.CloneType(expected)
	return nil
}

func validateConstrainedArray(node *syntax.ArrayExpr, expected machine.Type, inferred *inference, registry *machine.Registry) error {
	if expected.Kind() != machine.ArrayKind || !hasElem(expected) {
		return enumReturnError(node, expected)
	}
	for _, item := range node.Items {
		if err := validateConstrainedReturn(item, elemOf(expected), inferred, registry); err != nil {
			return err
		}
	}
	inferred.NodeTypes[node.ID] = machine.CloneType(expected)
	return nil
}

func validateConstrainedDict(node *syntax.DictExpr, expected machine.Type, inferred *inference, registry *machine.Registry) error {
	if expected.Kind() != machine.DictKind || !hasElem(expected) {
		return enumReturnError(node, expected)
	}
	for _, entry := range node.Entries {
		if err := validateConstrainedReturn(entry.Value, elemOf(expected), inferred, registry); err != nil {
			return err
		}
	}
	inferred.NodeTypes[node.ID] = machine.CloneType(expected)
	return nil
}

// validateConstrainedFor proves each shape a comprehension takes: a list
// yields elements, a dictionary comprehension yields values under its keys,
// and an outer clause of [e for x in xs for y in ys] yields the inner list,
// which is spliced in — so that one is held to the whole expected type. The
// comprehension itself has the container type, not its element's.
func validateConstrainedFor(node *syntax.ForExpr, expected machine.Type, inferred *inference, registry *machine.Registry) error {
	want := machine.ArrayKind
	if node.YieldKey != nil {
		want = machine.DictKind
	}
	if expected.Kind() != want || !hasElem(expected) {
		return enumReturnError(node, expected)
	}
	yield := elemOf(expected)
	if node.Flatten {
		yield = expected
	}
	if err := validateConstrainedReturn(node.Yield, yield, inferred, registry); err != nil {
		return err
	}
	inferred.NodeTypes[node.ID] = machine.CloneType(expected)
	return nil
}

func validateAndSet(parent, child syntax.Expr, expected machine.Type, inferred *inference, registry *machine.Registry) error {
	if err := validateConstrainedReturn(child, expected, inferred, registry); err != nil {
		return err
	}
	inferred.NodeTypes[parent.NodeID()] = machine.CloneType(expected)
	return nil
}

func validateKnownType(expr syntax.Expr, expected machine.Type, inferred *inference) error {
	if typ, ok := inferred.NodeTypes[expr.NodeID()]; ok && typ.Equal(expected) {
		return nil
	}
	return enumReturnError(expr, expected)
}

func enumReturnError(expr syntax.Expr, expected machine.Type) error {
	return syntax.Around(expr, "type error: cannot prove the expression returns %s", expected.Summary())
}
