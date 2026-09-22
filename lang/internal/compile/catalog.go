package compile

import (
	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
)

// Catalog is everything a front end renders for one registry: its functions,
// forms and value types, plus the shape of every ExprJSON node. The node
// schemas come from the syntax layer, which machine cannot see, so this is the
// entry a host calls rather than Registry.Catalog.
func Catalog(registry *machine.Registry) machine.LanguageCatalog {
	catalog := registry.Catalog()
	catalog.Nodes = syntax.NodeSchemas()
	catalog.Source = syntax.SourceSyntax()
	return catalog
}
