# 开发

构建、检查与代码结构。改动时的同步点与必须守住的不变量记在 [CLAUDE.md](../CLAUDE.md)。

```bash
make ci        # 格式、import 分组、前端检查与构建、vet、staticcheck、modernize、golangci-lint（均含 js/wasm）、deadcode、lint、build、wasm、Go 与 JS 测试；提交前必须全过，GitHub Actions 在每次推送与 PR 上跑它
make test      # Go 测试
make wasm      # 浏览器用的语言服务：web/dist/funroute.wasm
make web       # 前端产物：web/dist/*.js（需要先在 web/ 里 npm install；不提交）
make test-js   # 前端纯逻辑与 wasm 会话测试（node --test）
make site      # 组装发布目录 site/（make run 与 Pages 都用它）
make run       # 启动工作台
go test ./internal/machine -bench . -benchtime 2000x   # VM 基准
```

`make lint` 强制三条预算：函数不超过 50 行、嵌套不超过 3 层、文件不超过 800 行。

代码结构：

```text
funroute.go            公开包 funroute：只有别名与转发，根目录唯一的 Go 文件
lsp/                   语言服务：协议、stdio 传输
extensions/std/        标准库，只用公开 API
internal/kit/          多个包共用的同一段逻辑：错误分类、名字与数字字符、切片投影与查重、JSON 数字（只依赖标准库）
internal/money/        金额、比例、汇率、币种表与舍入：纯 Go 运算（只依赖 kit）
internal/machine/      值、类型、字节码、VM、注册表、目录、签名清单、内核库（依赖 money）
internal/syntax/       词法、语法、AST、ExprJSON、词法段、格式化、语法树（只依赖 machine）
internal/compile/      推导、编译、常量折叠、契约、Analyze（依赖 syntax + machine）
internal/demo/         演示控制台：宿主组装注册表的范例，工作台用它（只用公开包）
examples/              可运行的 Go 宿主程序：路由、金额、批处理（go run ./examples/<名字>，只用公开包）
tests/api/             公开面的测试与 godoc 示例，按主题组织（只用公开包）
tests/conformance/     把 web/funroute-examples.json 的每个示例经公开 API 跑完整条流水线
tests/limits/          生成并守着 docs/limits.md 的表格（make limits）
tests/perf/            性能报告，写进 docs/perf.md（make perf）
tests/perf/expr/       与 expr 的对照（单独的 module，只有它依赖 expr）
web/src/               工作台前端（TypeScript），每个组件一个入口，打包到 web/dist/
web/wasm/              浏览器用的语言服务入口（js/wasm）
cmd/funroute  cmd/playground  CLI（含 fmt、lsp）与工作台静态服务
```

`internal/` 下的实现只有 `funroute.go` 与 `lsp/` 可以导入，由 `make lint` 检查。

改动时的同步点与必须守住的不变量记在 [`CLAUDE.md`](../CLAUDE.md)。
