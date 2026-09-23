package lang

import "funroute/lang/internal/machine"

// The registry is the single authority on what exists and what it means.
type (
	Registry     = machine.Registry
	FunctionSpec = machine.FunctionSpec
	EvalFunc     = machine.EvalFunc
	Doc          = machine.Doc
	Form         = machine.Form
)

var (
	NewRegistry  = machine.NewRegistry
	CoreRegistry = machine.CoreRegistry
)

// The lazy forms a registry can enable. A registry is a console: what it
// enables is what its operators can write.
const (
	SwitchForm = machine.SwitchForm
	ForForm    = machine.ForForm
	ReduceForm = machine.ReduceForm
)

// A host function is registered by its Go signature, read once by reflection:
// Logic for business logic, Model for a model that also has a batch
// implementation. Both accept any arity, an optional leading context.Context,
// and Go containers nested to any depth.
var (
	Logic = machine.Logic
	Model = machine.Model
)

// The catalog is what a registry offers — its functions and its forms, with
// what the host and the language say about each — for a tool that lists
// them. Registry.Catalog builds it.
type (
	LanguageCatalog    = machine.LanguageCatalog
	FunctionDescriptor = machine.FunctionDescriptor
	FormDescriptor     = machine.FormDescriptor
)
