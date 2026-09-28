package machine

import (
	"cmp"
	"context"
	"fmt"
	"sync"
	"time"
)

// timeSpecs are what a rule does with times and durations: the arithmetic
// and the comparisons on their nanoseconds, reading a time from its text,
// and the calendar in a named zone. The current time is not among them: a
// rule that needs it is handed it, as an argument, so that a run answers the
// same every time.
func timeSpecs() []FunctionSpec {
	specs := timeArithmetic()
	for _, comparison := range comparisons {
		for _, typ := range []Type{TimeType, DurationType} {
			specs = append(specs, FunctionSpec{
				Name: comparison.name, Params: []Type{typ, typ}, Result: BoolType, Eval: orderTimes(comparison.accept),
				Doc: timeDoc(comparison.label, "比较两个时刻（早的小）或两个时长。", "比较结果", "左值", "右值"),
			})
		}
	}
	specs = append(specs, FunctionSpec{
		Name: "time", Params: []Type{StringType}, Result: TimeType, Eval: parseTime,
		Doc: timeDoc("读时刻", `把 RFC 3339 文本读成时刻：time("2026-09-28T10:00:00+08:00")。读不成是错误。`, "时刻", "文本"),
	})
	return append(specs, calendarSpecs()...)
}

// timeArithmetic is the integer operations on the nanoseconds, the answer
// of the kind the operands make: a time moved by a duration is a time, two
// times apart are a duration.
func timeArithmetic() []FunctionSpec {
	t, d, n := TimeType, DurationType, IntType
	specs := make([]FunctionSpec, 0, 9)
	for _, op := range []struct {
		name, label string
		params      [2]Type
		result      Type
		eval        EvalFunc
		left, right string
	}{
		{"add", "时刻加时长", [2]Type{t, d}, t, evalIntAdd, "时刻", "时长"},
		{"sub", "时刻减时长", [2]Type{t, d}, t, evalIntSub, "时刻", "时长"},
		{"sub", "两个时刻之差", [2]Type{t, t}, d, evalIntSub, "时刻", "时刻"},
		{"add", "时长相加", [2]Type{d, d}, d, evalIntAdd, "时长", "时长"},
		{"sub", "时长相减", [2]Type{d, d}, d, evalIntSub, "时长", "时长"},
		{"mul", "时长乘倍数", [2]Type{d, n}, d, evalIntMul, "时长", "倍数"},
		{"mul", "倍数乘时长", [2]Type{n, d}, d, evalIntMul, "倍数", "时长"},
		{"div", "时长除以份数", [2]Type{d, n}, d, evalIntDiv, "时长", "份数"},
		{"div", "时长之比", [2]Type{d, d}, n, evalIntDiv, "时长", "时长"},
	} {
		specs = append(specs, FunctionSpec{
			Name: op.name, Params: op.params[:], Result: op.result, Eval: asKind(op.result.kind, op.eval),
			Doc: timeDoc(op.label, "按纳秒做整数运算，溢出是错误；整数除法向零取整。", "结果", op.left, op.right),
		})
	}
	return specs
}

func timeDoc(label, description, result string, params ...string) Doc {
	return Doc{Label: label, Category: "时间", Description: description, Params: params, Result: result}
}

// asKind is an integer operation whose answer is of kind.
func asKind(kind Kind, eval EvalFunc) EvalFunc {
	return func(ctx context.Context, args []Value) (Value, error) {
		answer, err := eval(ctx, args)
		answer.kind = kind
		return answer, err
	}
}

func orderTimes(accept func(int) bool) EvalFunc {
	return func(_ context.Context, args []Value) (Value, error) {
		return Bool(accept(cmp.Compare(args[0].i, args[1].i))), nil
	}
}

func parseTime(_ context.Context, args []Value) (Value, error) {
	t, err := time.Parse(time.RFC3339Nano, args[0].s)
	if err != nil {
		return Value{}, fmt.Errorf("%w: time %q is not RFC 3339", ErrDomain, args[0].s)
	}
	return timeValue(t)
}

// calendarSpecs read a time in a zone, named as IANA names it: the hour and
// the day it is there, and the days counted there, which a zone's changes of
// clock make other than 24 hours.
func calendarSpecs() []FunctionSpec {
	zoned := "时区写 IANA 名字，如 \"Asia/Shanghai\"、\"UTC\"；不认识的时区是错误。"
	specs := make([]FunctionSpec, 0, 6)
	for _, part := range []struct {
		name, label, description string
		of                       func(time.Time) int
	}{
		{"hour", "小时", "时刻在该时区是几点，0 到 23。", time.Time.Hour},
		{"weekday", "星期", "时刻在该时区是星期几，1 是周一，7 是周日。", func(t time.Time) int { return (int(t.Weekday())+6)%7 + 1 }},
		{"day", "日", "时刻在该时区是几号，1 到 31。", time.Time.Day},
		{"month", "月", "时刻在该时区是几月，1 到 12。", func(t time.Time) int { return int(t.Month()) }},
	} {
		of := part.of
		specs = append(specs, FunctionSpec{
			Name: part.name, Params: []Type{TimeType, StringType}, Result: IntType, zoned: true,
			Eval: func(_ context.Context, args []Value) (Value, error) {
				local, err := inZone(args[0], args[1])
				return Int(int64(of(local))), err
			},
			Doc: timeDoc(part.label, part.description+zoned, part.label, "时刻", "时区"),
		})
	}
	return append(specs, FunctionSpec{
		Name: "start_of_day", Params: []Type{TimeType, StringType}, Result: TimeType, zoned: true, Eval: startOfDay,
		Doc: timeDoc("当天零点", "时刻在该时区当天的零点。"+zoned, "零点", "时刻", "时区"),
	}, FunctionSpec{
		Name: "add_days", Params: []Type{TimeType, IntType, StringType}, Result: TimeType, zoned: true, Eval: addDays,
		Doc: timeDoc("加天数", "按该时区的日历加 n 天，钟点不变；遇到夏令时切换，一天不是 24 小时。"+zoned, "时刻", "时刻", "天数", "时区"),
	})
}

func startOfDay(_ context.Context, args []Value) (Value, error) {
	local, err := inZone(args[0], args[1])
	if err != nil {
		return Value{}, err
	}
	year, month, day := local.Date()
	return timeValue(time.Date(year, month, day, 0, 0, 0, 0, local.Location()))
}

func addDays(_ context.Context, args []Value) (Value, error) {
	local, err := inZone(args[0], args[2])
	if err != nil {
		return Value{}, err
	}
	days := args[1].i
	// Past a time's span of about 106,000 days either way nothing is left
	// to count to; the bound keeps AddDate's int in range too.
	if days > 110_000 || days < -110_000 {
		return Value{}, fmt.Errorf("%w: %d days is past the years a time holds", ErrDomain, days)
	}
	return timeValue(local.AddDate(0, 0, int(days)))
}

// zones is every zone a run has named, loaded once: IANA names a few
// hundred, and a name that is none is not kept.
var zones sync.Map

// inZone is time t where zone name keeps its clocks.
func inZone(t, name Value) (time.Time, error) {
	loaded, ok := zones.Load(name.s)
	if !ok {
		// "" and "Local" are the machine's own zone, which a rule cannot
		// know; UTC is written as UTC.
		location, err := time.LoadLocation(name.s)
		if err != nil || name.s == "" || name.s == "Local" {
			return time.Time{}, fmt.Errorf("%w: unknown time zone %q", ErrDomain, name.s)
		}
		loaded, _ = zones.LoadOrStore(name.s, location)
	}
	location, _ := loaded.(*time.Location)
	return time.Unix(0, t.i).In(location), nil
}
