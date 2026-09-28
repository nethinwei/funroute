package machine

// A pure host function — one the compiler may fold (Doc.Constexpr), with no
// Timeout, not Detached, with no batch form — of a common scalar shape is
// called straight from the registers: its arguments read and its answer
// written with no Value slice and no closure between, and no look at the
// deadline first — a pure function is quick and waits on nothing, and a loop
// around it still looks. Inside a fallback's candidate it is called the
// ordinary way, where a panic becomes the candidate's failure. The answer and
// every failure are those of its ordinary call (TestPureCallsAnswerAsTheirEval).

// pureCall calls a pure function on the registers from args — each in the
// file of its kind — and writes its answer to dst, in the file of its kind.
type pureCall func(b *banks, args, dst int32) error

// pureOf is the pureCall of fn, or nil for a shape it has none for.
func pureOf(fn any) pureCall {
	for _, shapes := range []func(any) pureCall{pureUnary, pureBinary, plainUnary, plainBinary} {
		if call := shapes(fn); call != nil {
			return call
		}
	}
	return nil
}

// plainUnary and plainBinary are the shapes that return no error.
func plainUnary(fn any) pureCall {
	switch fn := fn.(type) {
	case func(int64) int64:
		return func(b *banks, a, dst int32) error { b.ints[dst] = fn(b.ints[a]); return nil }
	case func(float64) float64:
		return func(b *banks, a, dst int32) error { b.floats[dst] = fn(b.floats[a]); return nil }
	case func(string) string:
		return func(b *banks, a, dst int32) error { b.regs[dst] = String(fn(b.regs[a].s)); return nil }
	case func(string) bool:
		return func(b *banks, a, dst int32) error { b.ints[dst] = word(fn(b.regs[a].s)); return nil }
	case func(string) float64:
		return func(b *banks, a, dst int32) error { b.floats[dst] = fn(b.regs[a].s); return nil }
	}
	return nil
}

func plainBinary(fn any) pureCall {
	switch fn := fn.(type) {
	case func(int64, int64) int64:
		return func(b *banks, a, dst int32) error { b.ints[dst] = fn(b.ints[a], b.ints[a+1]); return nil }
	case func(float64, float64) float64:
		return func(b *banks, a, dst int32) error { b.floats[dst] = fn(b.floats[a], b.floats[a+1]); return nil }
	case func(float64, int64) float64:
		return func(b *banks, a, dst int32) error { b.floats[dst] = fn(b.floats[a], b.ints[a+1]); return nil }
	case func(int64, float64) float64:
		return func(b *banks, a, dst int32) error { b.floats[dst] = fn(b.ints[a], b.floats[a+1]); return nil }
	case func(string, float64) float64:
		return func(b *banks, a, dst int32) error { b.floats[dst] = fn(b.regs[a].s, b.floats[a+1]); return nil }
	case func(string, int64) int64:
		return func(b *banks, a, dst int32) error { b.ints[dst] = fn(b.regs[a].s, b.ints[a+1]); return nil }
	}
	return nil
}

func pureUnary(fn any) pureCall {
	switch fn := fn.(type) {
	case func(float64) (int64, error):
		return func(b *banks, a, dst int32) error {
			r, err := fn(b.floats[a])
			if err != nil {
				return err
			}
			b.ints[dst] = r
			return nil
		}
	case func(float64) (float64, error):
		return func(b *banks, a, dst int32) error {
			r, err := fn(b.floats[a])
			return floatInto(b, dst, r, err)
		}
	case func(int64) (int64, error):
		return func(b *banks, a, dst int32) error {
			r, err := fn(b.ints[a])
			if err != nil {
				return err
			}
			b.ints[dst] = r
			return nil
		}
	case func(string) (string, error):
		return func(b *banks, a, dst int32) error {
			r, err := fn(b.regs[a].s)
			if err != nil {
				return err
			}
			b.regs[dst] = String(r)
			return nil
		}
	}
	return nil
}

func pureBinary(fn any) pureCall {
	switch fn := fn.(type) {
	case func(string, string) bool:
		return func(b *banks, a, dst int32) error {
			b.ints[dst] = word(fn(b.regs[a].s, b.regs[a+1].s))
			return nil
		}
	case func(string, string) (string, error):
		return func(b *banks, a, dst int32) error {
			r, err := fn(b.regs[a].s, b.regs[a+1].s)
			if err != nil {
				return err
			}
			b.regs[dst] = String(r)
			return nil
		}
	case func(int64, int64) (int64, error):
		return func(b *banks, a, dst int32) error {
			r, err := fn(b.ints[a], b.ints[a+1])
			if err != nil {
				return err
			}
			b.ints[dst] = r
			return nil
		}
	case func(float64, float64) (float64, error):
		return func(b *banks, a, dst int32) error {
			r, err := fn(b.floats[a], b.floats[a+1])
			return floatInto(b, dst, r, err)
		}
	case func(string, int64, int64) (string, error):
		return func(b *banks, a, dst int32) error {
			r, err := fn(b.regs[a].s, b.ints[a+1], b.ints[a+2])
			if err != nil {
				return err
			}
			b.regs[dst] = String(r)
			return nil
		}
	}
	return nil
}

// floatInto writes a float answer.
func floatInto(b *banks, dst int32, r float64, err error) error {
	if err != nil {
		return err
	}
	b.floats[dst] = r
	return nil
}
