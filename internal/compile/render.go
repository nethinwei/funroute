package compile

import (
	"fmt"
	"strings"

	"github.com/nethinwei/funroute/internal/kit"
	"github.com/nethinwei/funroute/internal/machine"
)

// RenderWithContract writes the contract as comments above the expression.
// A contract lives in the host, so an expression on its own does not say what
// its inputs mean. That matters the moment a rule leaves the console — pasted
// into a ticket, an RFC or a chat, where "amount" could be cents or yuan.
//
// Comments are not syntax: the text parses to the same program with or without
// them, and re-parsing does not carry them back. The host record stays the
// single authority; this is a view of it.
func RenderWithContract(source string, contract CompileOptions) string {
	lines := contractComments(contract.Args, contract.Result, contract.ResultDoc)
	if len(lines) == 0 {
		return source
	}
	return strings.Join(lines, "\n") + "\n\n" + source
}

func contractComments(args []ArgSpec, result *machine.Type, resultDoc string) []string {
	if len(args) == 0 && result == nil {
		return nil
	}
	width := 0
	for _, arg := range args {
		if len(arg.Name)+1 > width {
			width = len(arg.Name) + 1
		}
	}
	if result != nil && width < 2 {
		width = 2
	}
	lines := make([]string, 0, len(args)+1)
	for _, arg := range args {
		lines = append(lines, comment(arg.Name+":", width, arg.Type.String(), arg.Doc))
	}
	if result != nil {
		lines = append(lines, comment("→", width, result.String(), resultDoc))
	}
	return lines
}

func comment(label string, width int, typeName, doc string) string {
	line := fmt.Sprintf("// %-*s %s", width, label, typeName)
	if doc == "" {
		return line
	}
	return fmt.Sprintf("%-28s %s", line, doc)
}

// ContractFromArtifact recovers the contract an artifact was compiled with, so
// a host that only stored the artifact can still render or re-check it.
// argSpecs is the contract's arguments as parameters declare them.
func argSpecs(params []machine.Parameter) []ArgSpec {
	return kit.Map(params, func(param machine.Parameter) ArgSpec {
		return ArgSpec{Name: param.Name(), Type: param.Type(), Doc: param.Doc()}
	})
}

func ContractFromArtifact(artifact *machine.Artifact) CompileOptions {
	if artifact == nil {
		return CompileOptions{}
	}
	result := artifact.Result()
	return CompileOptions{Args: argSpecs(artifact.Args()), Result: &result, ResultDoc: artifact.ResultDoc()}
}
