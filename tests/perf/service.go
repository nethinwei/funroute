package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nethinwei/funroute/lsp"
)

func backgroundContext() context.Context { return context.Background() }

// requests are what an editor asks of a document once it is open, in order.
var requests = []struct{ name, method string }{
	{"悬停", "textDocument/hover"}, {"补全", "textDocument/completion"},
	{"语义标记", "textDocument/semanticTokens/full"}, {"格式化", "textDocument/formatting"}, {"语法树", "funroute/syntaxTree"},
}

// languageService times each request on a short rule, a wide program and a
// long line of constants.
func languageService(out *strings.Builder) error {
	items, literals := make([]string, 1000), make([]string, 8000)
	for i := range items {
		items[i] = fmt.Sprintf("a * %d + b", i)
	}
	for i := range literals {
		literals[i] = strconv.Itoa(i)
	}
	documents := []struct{ name, text string }{
		{"一条规则", "let(fee = amount * 29 / 1000 + 30, if(fee > cap, cap, fee))"},
		{"1000 项数组，无契约", "[" + strings.Join(items, ", ") + "]"},
		{"8000 个常量写在一行", "sum([" + strings.Join(literals, ", ") + "])"},
	}
	out.WriteString("\n## 语言服务\n\n| 请求 |")
	for _, document := range documents {
		fmt.Fprintf(out, " %s（%s） |", document.name, size(len(document.text)))
	}
	out.WriteString("\n|---|---|---|---|\n")
	columns := make([][]string, len(documents))
	for i, document := range documents {
		column, err := serviceColumn(document.text)
		if err != nil {
			return err
		}
		columns[i] = column
	}
	for row, name := range append([]string{"打开文档并发布诊断"}, names()...) {
		fmt.Fprintf(out, "| %s | %s | %s | %s |\n", name, columns[0][row], columns[1][row], columns[2][row])
	}
	return nil
}

// size writes a length in bytes the way a person reads it.
func size(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d 字节", bytes)
	}
	return fmt.Sprintf("%.0f KB", float64(bytes)/1024)
}

func names() []string {
	out := make([]string, len(requests))
	for i, request := range requests {
		out[i] = request.name
	}
	return out
}

// serviceColumn is the fastest of three sessions for opening text and for
// each request on it.
func serviceColumn(text string) ([]string, error) {
	registry, err := newRegistry()
	if err != nil {
		return nil, err
	}
	best := make([]time.Duration, len(requests)+1)
	for i := range best {
		best[i] = time.Hour
	}
	for range 3 {
		timings := session(lsp.New(registry, func([]byte) {}), text)
		for i, took := range timings {
			best[i] = min(best[i], took)
		}
	}
	column := make([]string, len(best))
	for i, took := range best {
		column[i] = duration(float64(took.Nanoseconds()))
	}
	return column, nil
}

// session opens text on server and sends each request once, timing each.
func session(server *lsp.Server, text string) []time.Duration {
	send := func(method string, params any, request bool) time.Duration {
		message := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
		if request {
			message["id"] = 1
		}
		encoded, _ := json.Marshal(message)
		start := time.Now()
		server.Handle(encoded)
		return time.Since(start)
	}
	document := map[string]any{"uri": "file:///rule.fr"}
	send("initialize", map[string]any{}, true)
	timings := make([]time.Duration, 0, 1+len(requests))
	timings = append(timings, send("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": "file:///rule.fr", "version": 1, "text": text}}, false))
	at := map[string]any{"textDocument": document, "position": map[string]int{"line": 0, "character": len(text) / 2}}
	for _, request := range requests {
		params := map[string]any{"textDocument": document}
		if request.method == "textDocument/hover" || request.method == "textDocument/completion" {
			params = at
		}
		timings = append(timings, send(request.method, params, true))
	}
	return timings
}

// artifacts are the sizes of what the workbench ships, where make wasm and
// make web have built it.
func artifacts(out *strings.Builder) error {
	out.WriteString("\n## 产物\n\n")
	path := filepath.Join("web", "dist", "funroute.wasm")
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		out.WriteString("没有找到 `web/dist/funroute.wasm`：先 `make wasm`。\n")
		return nil
	}
	wasm, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var packed bytes.Buffer
	writer := gzip.NewWriter(&packed)
	if _, err := writer.Write(wasm); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	scripts, _ := filepath.Glob(filepath.Join("web", "dist", "*.js"))
	total := 0
	for _, script := range scripts {
		if info, err := os.Stat(script); err == nil {
			total += int(info.Size())
		}
	}
	fmt.Fprintf(out, "- 浏览器里的语言服务 `funroute.wasm`：%.2f MB（gzip 后 %.2f MB）\n- 前端 JS：%.0f KB\n", float64(len(wasm))/1e6, float64(packed.Len())/1e6, float64(total)/1e3)
	return nil
}
