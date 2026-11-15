package std

import (
	"maps"
	"slices"
)

// NamesWithExamples is the names examples gives examples to, for
// std_test.go: a name std does not register is an orphan.
func NamesWithExamples() []string { return slices.Sorted(maps.Keys(examples)) }

// RegisteredNames is every name std registers an overload of, money's
// included: a name it shares with the kernel is std's too when std adds an
// overload of it.
func RegisteredNames() map[string]bool {
	names := map[string]bool{}
	for _, spec := range slices.Concat(paddingSpecs(), sequenceSpecs(), whileSpecs(), groupSpecs(), moneySpecs()) {
		names[spec.Name] = true
	}
	return names
}
