package machine

// A pure host function — one the compiler may fold (Doc.Constexpr), with no
// Timeout, not Detached, with no batch form — of a common scalar shape is
// called straight from the registers: its arguments read and its answer
// written with no Value slice and no closure between, and no look at the
// deadline first — a pure function is quick and waits on nothing, and a loop
// around it still looks. Inside a fallback's candidate it is called the
// ordinary way, where a panic becomes the candidate's failure. The answer and
// every failure are those of its ordinary call (TestPureCallsAnswerAsTheirEval).

// pureCall calls a pure function on the registers from args and writes its
// answer to dst.
type pureCall func(regs []Value, args, dst int32) error

// pureOf is the pureCall of fn, or nil for a shape it has none for.
func pureOf(fn any) pureCall {
	if call := pureUnary(fn); call != nil {
		return call
	}
	return pureBinary(fn)
}

func pureUnary(fn any) pureCall {
	switch fn := fn.(type) {
	case func(float64) (int64, error):
		return func(regs []Value, a, dst int32) error {
			r, err := fn(regs[a].f)
			if err != nil {
				return err
			}
			regs[dst] = Int(r)
			return nil
		}
	case func(float64) (float64, error):
		return func(regs []Value, a, dst int32) error {
			r, err := fn(regs[a].f)
			return floatInto(regs, dst, r, err)
		}
	case func(int64) (int64, error):
		return func(regs []Value, a, dst int32) error {
			r, err := fn(regs[a].i)
			if err != nil {
				return err
			}
			regs[dst] = Int(r)
			return nil
		}
	case func(string) (string, error):
		return func(regs []Value, a, dst int32) error {
			r, err := fn(regs[a].s)
			if err != nil {
				return err
			}
			regs[dst] = String(r)
			return nil
		}
	}
	return nil
}

func pureBinary(fn any) pureCall {
	switch fn := fn.(type) {
	case func(string, string) bool:
		return func(regs []Value, a, dst int32) error {
			regs[dst] = Bool(fn(regs[a].s, regs[a+1].s))
			return nil
		}
	case func(string, string) (string, error):
		return func(regs []Value, a, dst int32) error {
			r, err := fn(regs[a].s, regs[a+1].s)
			if err != nil {
				return err
			}
			regs[dst] = String(r)
			return nil
		}
	case func(int64, int64) (int64, error):
		return func(regs []Value, a, dst int32) error {
			r, err := fn(regs[a].i, regs[a+1].i)
			if err != nil {
				return err
			}
			regs[dst] = Int(r)
			return nil
		}
	case func(float64, float64) (float64, error):
		return func(regs []Value, a, dst int32) error {
			r, err := fn(regs[a].f, regs[a+1].f)
			return floatInto(regs, dst, r, err)
		}
	case func(string, int64, int64) (string, error):
		return func(regs []Value, a, dst int32) error {
			r, err := fn(regs[a].s, regs[a+1].i, regs[a+2].i)
			if err != nil {
				return err
			}
			regs[dst] = String(r)
			return nil
		}
	}
	return nil
}

// floatInto writes a float answer, refused when it is not finite as the
// ordinary call refuses it.
func floatInto(regs []Value, dst int32, r float64, err error) error {
	if err != nil {
		return err
	}
	value, err := CheckedFloat(r)
	if err != nil {
		return err
	}
	regs[dst] = value
	return nil
}
