package compile

import (
	"fmt"
	"strings"

	"funroute/lang/internal/machine"
)

// RenderWithContract writes the contract as comments above the expression.
// A contract lives in the host, so an expression on its own does not say what
// its inputs mean. That matters the moment a rule leaves the console — pasted
// into a ticket, an RFC or a chat, where "amount" could be cents or yuan.
//
// Comments are not syntax: the text parses to the same program with or without
// them, and re-parsing does not carry them back. The host record stays the
// single authority; this is a view of it.
func RenderWithContract(source string, args []ArgSpec, result *machine.Type, resultDoc string) string {
	lines := contractComments(args, result, resultDoc)
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
func ContractFromArtifact(artifact *machine.Artifact) ([]ArgSpec, *machine.Type, string) {
	if artifact == nil {
		return nil, nil, ""
	}
	params := artifact.Args()
	args := make([]ArgSpec, len(params))
	for i, param := range params {
		args[i] = ArgSpec{Name: param.Name(), Type: param.Type(), Doc: param.Doc()}
	}
	result := artifact.Result()
	return args, &result, artifact.ResultDoc()
}
