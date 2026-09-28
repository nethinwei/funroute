package compile

import "github.com/nethinwei/funroute/internal/machine"

// A Binding is a compiler for one contract, and the contract is two Go types:
// the tagged fields of In are the arguments, in declaration order, and Out is
// the result. What it compiles — or loads — is a Program that runs on those
// types directly. A program that returns something else does not compile,
// because the result type takes part in inference. An artifact compiled
// elsewhere loads when In and Out can carry what it declares, matched by name.
type Binding[In, Out any] struct {
	registry *machine.Registry
	codec    *machine.Codec[In, Out]
	options  CompileOptions
}

func Bind[In, Out any](registry *machine.Registry) (*Binding[In, Out], error) {
	codec, err := machine.NewCodec[In, Out](registry)
	if err != nil {
		return nil, err
	}
	result := codec.Result()
	options := CompileOptions{Args: argSpecs(codec.Parameters()), Result: &result}
	options.hints = options.argTypes()
	if err := ValidateContract(options); err != nil {
		return nil, err
	}
	return &Binding[In, Out]{registry: registry, codec: codec, options: options}, nil
}

// Options is the contract as CompileOptions, for a console that shows it or a
// language server that checks against it. It is a copy.
func (b *Binding[In, Out]) Options() CompileOptions {
	options := b.options
	options.Args = append([]ArgSpec{}, b.options.Args...)
	result := *b.options.Result
	options.Result = &result
	return options
}

func (b *Binding[In, Out]) Compile(source string) (*machine.Program[In, Out], error) {
	// The compile only reads the contract, so it needs no copy of its own.
	options := b.options
	options.unsealed = true
	artifact, err := CompileExpr(source, b.registry, options)
	if err != nil {
		return nil, err
	}
	return b.codec.InstantiateCompiled(artifact)
}

func (b *Binding[In, Out]) CompileJSON(exprJSON []byte) (*machine.Program[In, Out], error) {
	// The compile only reads the contract, so it needs no copy of its own.
	options := b.options
	options.unsealed = true
	artifact, err := CompileJSON(exprJSON, b.registry, options)
	if err != nil {
		return nil, err
	}
	return b.codec.InstantiateCompiled(artifact)
}

// Load binds an artifact compiled elsewhere. Each argument it declares must be
// a field of In by name, each record field one of the struct's, each type the
// same; what In and Out carry beyond that is ignored. Otherwise the error is
// an ErrContract.
func (b *Binding[In, Out]) Load(artifact *machine.Artifact) (*machine.Program[In, Out], error) {
	return b.codec.Instantiate(artifact)
}
