// Command perf measures how fast FunRoute is, through the public package the
// way a host uses it, and writes the report as Markdown on standard output:
// make perf puts it in docs/perf.md, committed when it is wanted. Timings
// depend on the machine, so they are measured here and never asserted; the
// limits a run decides are tests/limits', which go test holds to the docs.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func main() {
	var report strings.Builder
	header(&report)
	for _, section := range []func(*strings.Builder) error{singleRuns, largeInputs, compiling, languageService, artifacts} {
		if err := section(&report); err != nil {
			fmt.Fprintln(os.Stderr, "perf:", err)
			os.Exit(1)
		}
	}
	fmt.Print(report.String())
}

// header says where and when the numbers were taken.
func header(out *strings.Builder) {
	commit := command("git", "rev-parse", "--short", "HEAD")
	if command("git", "status", "--porcelain") != "" {
		commit += "（工作区有未提交的改动）"
	}
	fmt.Fprintf(out, "# 性能报告\n\n由 `make perf` 生成（`go run ./tests/perf`，与 expr 的对照来自 `tests/perf/expr`），不要手改。数字随机器变化，取的是多次运行里最快的一次；能力的边界见 [limits.md](limits.md)。\n\n")
	fmt.Fprintf(out, "- 日期：%s\n- 机器：%s，%s/%s\n- Go：%s\n- 提交：`%s`\n", time.Now().Format("2006-01-02"), cpuName(), runtime.GOOS, runtime.GOARCH, runtime.Version(), commit)
}

// cpuName is the processor's name, where the system tells it.
func cpuName() string {
	if name := command("sysctl", "-n", "machdep.cpu.brand_string"); name != "" {
		return name
	}
	if info, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for line := range strings.Lines(string(info)) {
			if key, value, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(key) == "model name" {
				return strings.TrimSpace(value)
			}
		}
	}
	return "未知处理器"
}

// command is what a command prints, trimmed, or "" when it cannot run.
func command(name string, args ...string) string {
	output, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// duration writes nanoseconds the way a person reads them.
func duration(nanoseconds float64) string {
	switch {
	case nanoseconds < 10:
		return fmt.Sprintf("%.2f ns", nanoseconds)
	case nanoseconds < 1e3:
		return fmt.Sprintf("%.0f ns", nanoseconds)
	case nanoseconds < 1e6:
		return fmt.Sprintf("%.1f µs", nanoseconds/1e3)
	case nanoseconds < 1e9:
		return fmt.Sprintf("%.1f ms", nanoseconds/1e6)
	}
	return fmt.Sprintf("%.2f s", nanoseconds/1e9)
}

// fastest is the least time run takes in three tries.
func fastest(run func() error) (time.Duration, error) {
	best := time.Duration(1 << 62)
	for range 3 {
		start := time.Now()
		if err := run(); err != nil {
			return 0, err
		}
		best = min(best, time.Since(start))
	}
	return best, nil
}
