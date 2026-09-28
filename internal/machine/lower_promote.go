package machine

// A record argument the program reads only field by field, each read
// straight off the argument, is never built: each field it reads has a
// register of its own, the field read becomes a read of that register, and a
// run loads the field there — a Program out of the host's struct, as it loads
// a plain argument, any other run out of the record it was given. The
// record's other fields are not loaded at all. Only a plain field is
// promoted: a bool, an int, a float or a string, which a run checks nothing
// of beyond what loading it does. Lowering does not change the artifact.

// promotion is one promoted field: argument arg's field field, in register
// reg — in the file of the field's kind.
type promotion struct {
	arg, field, reg int32
	kind            Kind
}

// promotionsOf finds the promoted fields of parts, in registers from base
// up: those of the record arguments whose every load is followed at once,
// inside its block, by a read of a plain field.
func promotionsOf(parts *ArtifactParts, leaders []bool, base int32) []promotion {
	code := parts.Instructions
	eligible := make([]bool, len(parts.Args))
	for i, param := range parts.Args {
		eligible[i] = param.typ.kind == RecordKind
	}
	for pc, in := range code {
		if in.Op == OpLoadArg && eligible[in.A] && !readsAPlainField(parts, leaders, pc) {
			eligible[in.A] = false
		}
	}
	var promotions []promotion
	for pc, in := range code {
		if in.Op != OpLoadArg || !eligible[in.A] || promoted(promotions, int32(in.A), int32(code[pc+1].A)) >= 0 {
			continue
		}
		field := code[pc+1].A
		promotions = append(promotions, promotion{
			arg: int32(in.A), field: int32(field), reg: base + int32(len(promotions)), kind: parts.Args[in.A].typ.fields[field].typ.kind,
		})
	}
	return promotions
}

// readsAPlainField reports whether the argument loaded at pc is read at once,
// in the same block, for a plain field.
func readsAPlainField(parts *ArtifactParts, leaders []bool, pc int) bool {
	code := parts.Instructions
	if pc+1 >= len(code) || leaders[pc+1] || code[pc+1].Op != OpField {
		return false
	}
	switch parts.Args[code[pc].A].typ.fields[code[pc+1].A].typ.kind {
	case BoolKind, IntKind, FloatKind, StringKind:
		return true
	}
	return false
}

// promoted is the register of argument arg's field, or -1 when it has none.
func promoted(promotions []promotion, arg, field int32) int32 {
	for _, p := range promotions {
		if p.arg == arg && p.field == field {
			return p.reg
		}
	}
	return -1
}

// field lowers a field read: of a promoted field, the read of its register.
func (l *lowerer) field(index int) {
	// An argument's register is between the constants and the stack.
	if held := l.stack[len(l.stack)-1].reg; held >= l.out.args && held < l.stackBase {
		if reg := promoted(l.out.promotions, held-l.out.args, int32(index)); reg >= 0 {
			l.pop()
			l.push(reg)
			return
		}
	}
	op := rField
	switch l.pushedKind() {
	case IntKind:
		op = rFieldI
	case FloatKind:
		op = rFieldF
	case BoolKind:
		op = rFieldB
	}
	l.unary(op, int32(index))
}

// promote loads each promoted field out of the record argument a run was
// given whole.
func (f *frame) promote(args []Value) {
	for _, p := range f.runtime.reg.promotions {
		f.setValue(p.reg, args[p.arg].Field(int(p.field)))
	}
}
