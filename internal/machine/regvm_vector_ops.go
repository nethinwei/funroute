package machine

// The vector's operations over a column. Each answers what the kernel
// operation answers for every item that reaches it, and is the first such
// item it fails on — where the kernel's function fails — or limit when there
// is none; an item that does not reach it is computed or not, and never
// fails. TestVectorOpsAnswerAsTheKernel holds intOp and floatOp to the
// kernel's functions on the edges.

// intOp is the int operation op of a and b.
func intOp(op rop, a, b int64) (int64, bool) {
	switch op {
	case rAddI:
		return addInt(a, b)
	case rSubI:
		return subInt(a, b)
	case rMulI:
		return mulInt(a, b)
	case rDivI:
		return divInt(a, b)
	case rModI:
		return modInt(a, b)
	}
	return 0, false
}

// floatOp is the float operation op of a and b, which always answers, as
// setFloat does.
func floatOp(op rop, a, b float64) float64 {
	switch op {
	case rAddF:
		return a + b
	case rSubF:
		return a - b
	case rMulF:
		return a * b
	}
	return a / b
}

// eval runs the operation over the first limit items, those alive, and is
// the new limit.
func (o *vecOp) eval(limit int, alive []bool) int {
	switch o.outKind {
	case vecInt:
		if o.op == rMove {
			return o.moveInts(limit)
		}
		return o.ints(limit, alive)
	case vecFloat:
		switch o.op {
		case rMove:
			return o.moveFloats(limit)
		case rIntToF:
			return o.toFloats(limit, alive)
		}
		return o.floats(limit)
	}
	return o.bools(limit)
}

func (o *vecOp) ints(limit int, alive []bool) int {
	a, b, dst := o.a, o.b, o.dst.ints
	switch o.op {
	case rAddI:
		for i := range limit {
			sum, ok := addInt(a.int(i), b.int(i))
			if !ok && alive[i] {
				return i
			}
			dst[i] = sum
		}
		return limit
	case rMulI:
		for i := range limit {
			product, ok := mulInt(a.int(i), b.int(i))
			if !ok && alive[i] {
				return i
			}
			dst[i] = product
		}
		return limit
	case rModI:
		return o.mods(limit, alive)
	}
	for i := range limit {
		value, ok := intOp(o.op, a.int(i), b.int(i))
		if !ok && alive[i] {
			return i
		}
		dst[i] = value
	}
	return limit
}

// mods is a column of remainders; by one divisor for all, the common case,
// the checks are made once.
func (o *vecOp) mods(limit int, alive []bool) int {
	a, b, dst := o.a, o.b, o.dst.ints
	if b.ints == nil && b.i != 0 && b.i != -1 {
		divisor := b.i
		for i := range limit {
			dst[i] = a.int(i) % divisor
		}
		return limit
	}
	for i := range limit {
		value, ok := intOp(rModI, a.int(i), b.int(i))
		if !ok && alive[i] {
			return i
		}
		dst[i] = value
	}
	return limit
}

// floats runs a float operation, which never fails.
func (o *vecOp) floats(limit int) int {
	a, b, dst := o.a, o.b, o.dst.floats
	for i := range limit {
		dst[i] = floatOp(o.op, a.float(i), b.float(i))
	}
	return limit
}

// bools runs a comparison, or a move of bools; neither fails.
func (o *vecOp) bools(limit int) int {
	a, b, dst := o.a, o.b, o.dst.bools
	switch {
	case o.op == rMove:
		for i := range limit {
			dst[i] = a.bool(i)
		}
	case o.op == rLtI:
		for i := range limit {
			dst[i] = a.int(i) < b.int(i)
		}
	case o.op == rLeI:
		for i := range limit {
			dst[i] = a.int(i) <= b.int(i)
		}
	case o.op == rLtF:
		for i := range limit {
			dst[i] = a.float(i) < b.float(i)
		}
	case o.op == rLeF:
		for i := range limit {
			dst[i] = a.float(i) <= b.float(i)
		}
	default:
		o.equal(limit)
	}
	return limit
}

// equal is == of two ints, floats or bools.
func (o *vecOp) equal(limit int) {
	a, b, dst := o.a, o.b, o.dst.bools
	switch a.kind {
	case vecInt:
		if b.ints == nil {
			for i := range limit {
				dst[i] = a.int(i) == b.i
			}
			return
		}
		for i := range limit {
			dst[i] = a.int(i) == b.int(i)
		}
	case vecFloat:
		for i := range limit {
			dst[i] = a.float(i) == b.float(i)
		}
	default:
		for i := range limit {
			dst[i] = a.bool(i) == b.bool(i)
		}
	}
}

func (o *vecOp) toFloats(limit int, alive []bool) int {
	for i := range limit {
		value, ok := intFloat(o.a.int(i))
		if !ok && alive[i] {
			return i
		}
		o.dst.floats[i] = value
	}
	return limit
}

func (o *vecOp) moveInts(limit int) int {
	for i := range limit {
		o.dst.ints[i] = o.a.int(i)
	}
	return limit
}

func (o *vecOp) moveFloats(limit int) int {
	for i := range limit {
		o.dst.floats[i] = o.a.float(i)
	}
	return limit
}
