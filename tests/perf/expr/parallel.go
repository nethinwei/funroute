package main

import (
	"cmp"
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	"github.com/nethinwei/funroute"
)

// parallelSource is the rule every core runs at once.
const parallelSource = "a * b - a % b + 1"

// parallelWay is one way to run a rule on many goroutines at once: what it
// is called, and a run per goroutine it starts.
type parallelWay struct {
	name  string
	start func() func() error
}

// parallelSection measures one rule run on every core at once, the way a
// service runs it, beside running it on one core: FunRoute's Program, and
// expr's ways that are safe on many goroutines — a VM each, a pool of VMs,
// and expr.Run, which makes one per call.
func parallelSection(registry *funroute.Registry) (string, error) {
	ways, err := parallelWays(registry)
	if err != nil {
		return "", err
	}
	cores := runtime.GOMAXPROCS(0)
	var out strings.Builder
	fmt.Fprintf(&out, "\n### 并行：同一条规则在全部 %d 个核上同时跑\n\n`%s`，每个 goroutine 从自己的宿主 struct 读参数。数字是每次运行的时间（全部核的总耗时除以总次数），越小越好；上表 expr 用的复用 `vm.VM` 不能并发，这里是它能并发的三种用法。\n\n", cores, parallelSource)
	fmt.Fprintf(&out, "| 用法 | 1 核 | %d 核 | 分配 |\n|---|---|---|---|\n", cores)
	for _, way := range ways {
		one, all := parallel(way, 1), parallel(way, cores)
		if one.err != nil || all.err != nil {
			return "", fmt.Errorf("%s: %w", way.name, cmp.Or(one.err, all.err))
		}
		fmt.Fprintf(&out, "| %s | %s | %s | %d 次 |\n", way.name, duration(one.nanoseconds), duration(all.nanoseconds), all.allocations)
	}
	runtime.GOMAXPROCS(cores)
	return out.String(), nil
}

func parallelWays(registry *funroute.Registry) ([]parallelWay, error) {
	binding, err := funroute.Bind[scalarIn, int64](registry)
	if err != nil {
		return nil, err
	}
	program, err := binding.Compile(parallelSource)
	if err != nil {
		return nil, err
	}
	compiled, err := expr.Compile(parallelSource, expr.Env(scalarIn{}))
	if err != nil {
		return nil, err
	}
	pool := sync.Pool{New: func() any { return new(vm.VM) }}
	return []parallelWay{
		{"FunRoute `Program.Run`", func() func() error {
			in := newScalar()
			return func() error { _, err := program.Run(context.Background(), in); return err }
		}},
		{"expr：每个 goroutine 一个 `vm.VM`", func() func() error {
			in, machine := *newScalar(), new(vm.VM)
			return func() error { _, err := machine.Run(compiled, in); return err }
		}},
		{"expr：`sync.Pool` 管理 `vm.VM`", func() func() error {
			in := *newScalar()
			return func() error {
				machine, _ := pool.Get().(*vm.VM)
				_, err := machine.Run(compiled, in)
				pool.Put(machine)
				return err
			}
		}},
		{"expr：`expr.Run`（每次新建 `VM`）", func() func() error {
			in := *newScalar()
			return func() error { _, err := expr.Run(compiled, in); return err }
		}},
	}, nil
}

// parallel measures way on procs cores at once.
func parallel(way parallelWay, procs int) measured {
	runtime.GOMAXPROCS(procs)
	var failed error
	var mu sync.Mutex
	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			if err := drive(pb, way.start()); err != nil {
				mu.Lock()
				failed = cmp.Or(failed, err)
				mu.Unlock()
			}
		})
	})
	return measured{float64(result.T.Nanoseconds()) / float64(result.N), result.AllocsPerOp(), failed}
}

// drive runs run as long as pb says, and is the first error it met.
func drive(pb *testing.PB, run func() error) error {
	var failed error
	for pb.Next() {
		if err := run(); err != nil && failed == nil {
			failed = err
		}
	}
	return failed
}
