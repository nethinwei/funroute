package compile

import (
	"fmt"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// A record update, order with {amount: 1}, has its base's type: it replaces
// fields and never adds, removes or retypes one. That keeps the result the
// type it came from — an Order in, an Order out — so it can go back into the
// contract that declared it.

// inferRecordUpdate needs the base's type the way a field read does, and then
// holds each new value to the type of the field it replaces.
func inferRecordUpdate(node *syntax.RecordUpdateExpr, state *inferState, context inferContext) ([]inferResult, error) {
	bases, err := inferExpr(node.Base, state, context)
	if err != nil {
		return nil, err
	}
	var out []inferResult
	for _, base := range bases {
		typ, ok := base.state.publicType(base.typ)
		if !ok || typ.Kind() != machine.RecordKind {
			continue
		}
		states, err := inferUpdatedFields(node, typ, base.state, context)
		if err != nil {
			return nil, err
		}
		for _, updated := range states {
			out = append(out, inferResult{typ: recordTerm(typ), state: updated})
		}
	}
	if len(out) == 0 {
		return nil, syntax.Around(node.Base, "type error: with {…} updates something that is not a record with a known type")
	}
	return record(node, out), nil
}

// inferUpdatedFields threads the candidates through every new value in turn.
func inferUpdatedFields(node *syntax.RecordUpdateExpr, typ machine.Type, state *inferState, context inferContext) ([]*inferState, error) {
	states := []*inferState{state}
	for _, field := range node.Fields {
		index := typ.FieldIndex(field.Name)
		if index < 0 {
			return nil, syntax.Around(field.Value, "type error: %s has no field %q to update", typ.Summary(), field.Name)
		}
		next, err := inferUpdatedField(field, typ.Fields()[index].Type(), states, context)
		if err != nil {
			return nil, err
		}
		states = next
	}
	return states, nil
}

func inferUpdatedField(field syntax.RecordFieldExpr, want machine.Type, states []*inferState, context inferContext) ([]*inferState, error) {
	var out []*inferState
	for _, state := range states {
		values, err := inferExpr(field.Value, state, context)
		if err != nil {
			return nil, err
		}
		for _, value := range values {
			candidate := value.state.clone()
			if candidate.unify(value.typ, candidate.concrete(want)) == nil {
				out = append(out, candidate)
			}
		}
	}
	if len(out) == 0 {
		return nil, syntax.Around(field.Value, "type error: field %q is %s, and an update keeps its type", field.Name, want.Summary())
	}
	return out, nil
}

// compileRecordUpdate pushes the base, then the new values in the order they
// were written, and replaces them in one instruction.
func (c *bytecodeCompiler) compileRecordUpdate(node *syntax.RecordUpdateExpr) error {
	resultType, ok := c.inferred.NodeTypes[node.ID]
	if !ok || resultType.Kind() != machine.RecordKind {
		return fmt.Errorf("internal error: record update with unresolved type")
	}
	if err := c.compile(node.Base); err != nil {
		return err
	}
	names := make([]string, len(node.Fields))
	for i, field := range node.Fields {
		names[i] = field.Name
		if err := c.compile(field.Value); err != nil {
			return err
		}
	}
	c.emit(machine.Instruction{Op: machine.OpRecordWith, Type: &resultType, Keys: names})
	return nil
}
