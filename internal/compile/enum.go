package compile

import (
	"fmt"
	"slices"
	"strings"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
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
// program under it resolves in. ValidateContract does not look for two
// different enums under one name; collectEnums refuses them when a program
// compiles. Enums reports no such conflict: it stops at the first one it
// meets, and holds only the enums collected before it.
func (o CompileOptions) Enums() map[string]machine.Type {
	enums := map[string]machine.Type{}
	_ = collectEnums(enums, o.argTypes(), o.Result)
	return enums
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
		dst[inner.Name()] = inner
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
// brings. A contract's own enum member comes first.
func isRegistryEnum(name string) bool {
	return name == machine.RoundingEnum || name == machine.AllocationEnum
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
