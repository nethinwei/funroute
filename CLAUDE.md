# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 规格（薄）

FunRoute：面向支付路由的强类型纯表达式语言。module `funroute`，Go 1.26，**零第三方依赖**，git 仓库，主分支 `main`。语法、类型与推导规则的完整说明在 `README.md`，改语义前先读它、改完同步它。

### 包边界（先看这条）

```text
lang/lang.go              唯一的公开接口：类型别名与转发，约 40 个标识符
lang/internal/machine/    值、类型、字节码、VM、帧、注册表、目录     ← 不依赖任何上层
lang/internal/syntax/     词法、语法、AST、ExprJSON                 ← 只依赖 machine
lang/internal/compile/    推导、编译、常量折叠、契约、导出视图        ← 依赖 syntax + machine
```

依赖严格单向，`go list -deps` 可验证。**硬约束**：`Value` 的容器 backing 是私有字段，VM 的循环/索引/构造直接在上面操作，所以 value/container/convert/vm/frame 必须同包 —— 拆开就只能走公开 accessor。跨层要用 machine 的内部件时，导出一个**语义明确的入口**（`EvaluateClosed`、`IsLazyIf`、`Resolve`、`FormOf`），不要导出零件。

AST 类型**故意不公开**：宿主通过 ExprJSON 交换程序。`lang/lang_test.go` 是 `package lang_test`，只能用公开 API，所以它同时守着这条边界。

### 值的容器是原生 Go 值，边界零转换零拷贝

`array<float>` 的 backing 就是 `[]float64`，`dict<int>` 就是 `map[string]int64`，只有容器的容器用 `[]Value`（`nestedArray`/`nestedDict`）。`machine/container.go` 是唯一知道这个映射的地方（`length`/`at`/`lookup`/`keys`/`tail`/`arrayBuilder`/`packDict`），`machine/convert.go` 的 `fromGo` 是唯一的 Go 类型清单：`ToValue`、`FromValue`、`Fn1/2/3` 的参数与结果转换、`Run(map)` 的 `coerce` 全走它。宿主 `[]float64` → `RunValues` → 扩展函数 → 返回，始终是同一个底层数组（`TestHostVectorsReachExtensionsWithoutCopying` 用指针相等断言，`BenchmarkVectorPassThrough` 证明耗时与长度无关）。

代价是**不可变靠约定**：交给 `Value` 的切片/映射与从 `Value` 取出的（`Array()`/`Dict()`/`Any()`/`FromValue`）从那一刻起只读，VM 自身从不原地修改。任何新增的取值入口都要保持"交出 backing，不复制"并在注释里说明只读。边界上不许分配：`fromGo` 失败返回哨兵 `errUnsupportedGoType`，标量在 `ToValue`/`FromValue` 里经 `any(&x)` 指针匹配，不装箱。

### 句柄与批处理：模型引擎的数据不进语言

`HandleKind` 是不透明宿主值：`Type.Name` 是宿主给的名字（`handle<onnx.tensor>`），`Value.s` 存名字、`box` 存 payload。语言对它只做一件事——传递：`compareEqual` 拒绝比较（`eq` 与 `switch` 共用），不能进容器字面量以外的任何运算，`Any()` 只给类型名。`Registry.handles` 把 Go 类型映射到句柄名（`DefineHandle[T]`），`Fn1/2/3`、`Model1/2` 的签名推导（`register.go` 的 `goType`）先查它再走 `fromGo`。**不要为张量设计值类型**：形状、dtype、批维度都归引擎，这里只有句柄。

批处理在 SDK 层（`machine/batch.go`），不在编译器：`PrefetchSites` 读字节码找可提升的调用，条件是参数全为 `OpLoadArg`/`OpConstant`、不在任何前向跳转或循环覆盖的区间内（`guardedInstructions`）。这三条保证提前算不改变可观察行为，`if` 的惰性对模型调用仍然成立。`Batch.execute` 对每个站点调一次 `EvalBatch`，结果按 pc 放进 `RunOptions.Prefetched`，`frame.evaluate` 命中即取，仍扣 fuel。没有 `EvalBatch` 的函数与不可提升的调用退回逐条 `Eval`。

### 预算：ctx 贯通，边界检查，错误类型化

`Run`/`RunValues`/`Batch.Run` 首参数是 `ctx`，帧持有它；`frame.invoke` 在**每次扩展调用前**检查 `ctx.Err()`，纯计算不打断。`Doc.Timeout` 用 `context.WithTimeout` 给单次调用加上限（只在设置了才分配），`Doc.Detached` 走 `callDetached`（独立 goroutine + select，参数先拷贝一份因为栈视窗会被复用）。内核函数没有这两个设置，走 `invoke` 的短路径，所以纯表达式基准不受影响。所有失败在 `classify` 里包成 `ErrDeadline` 或 `ErrExtension`，fuel 耗尽是 `ErrFuel`；新增错误路径必须选一个包，宿主只用 `errors.Is`。`Batch` 的引擎调用取批内最早 deadline（`earliestDeadline`），每个程序仍用自己的 ctx。

`Logic`/`Model` 是反射注册（`register.go` + `reflect.go`）：签名与两个方向的转换器在注册时解析一次，`intoGo` 对 backing 类型精确匹配的容器直接交出（零拷贝仍成立），其余逐层构造；调用走 `reflect.Call`，约 300 ns。不要为了省这点重新引入 `Fn1/Fn2/…` 这类按元数展开的泛型；要零开销就写 `FunctionSpec`。

### 语法节点只在一处定义

`syntax/ast.go` 的每个节点 struct 就是它的全部描述：`json:"..."` 是 ExprJSON 字段（`omitempty` = 可选），`role:"var|fn|local|text"` 决定名字校验规则，`binds:"a,b"` 说明这个局部名在哪些字段里可见（`@rest` = 同一列表里后面的项），`default`/`min` 给前端。`syntax/walk.go` 用反射把 tag 读成 plan，导入、导出、`FreeVariables`、`Children`、`FormOf`、`NodeSchemas` 全部由 plan 驱动，**不按节点类型分派**。节点自己的规则（重名、重复键）写在 `check()` 里，parser 的 `p.node()` 与导入器的 `finish()` 都调它 —— 源码和 JSON 一套规则。反射只在编译期跑，不在执行路径上。

前端拿到的目录（`lang.Catalog(registry)`，不是 `Registry.Catalog()`）带 `nodes`：`web/funroute-core.js` 的 `cleanNode`/`blankNode` 与 `web/funroute-designer.js` 的卡片布局都从它生成，JS 里没有节点字段清单，只有 `funroute-core.js` 的文案表 `FIELD_TEXT`。端到端验证：10 个例子经 `cleanNode` 得到的 JSON 与服务端 `/api/parse` 的规范 JSON 逐字节相同。

### 契约在宿主，不在语言里

程序文本**只是表达式**。参数名、类型、顺序、说明、返回类型由宿主通过 `CompileOptions{Args []ArgSpec, Result *Type, ResultDoc}` 传入。理由：控制台本来就存规则元数据（版本、生效窗口、审批人、灰度），参数类型是同类信息，放语言里就是两份平行元数据。曾经有过 `@arg/@let/@ret` 头部，删掉了（见 README「契约」章节的权衡）。

- `Args` 非空时，**顺序即 ABI**；为空则按自由变量首次出现顺序推导。
- 声明了可以不用（调用方 ABI 稳定），用了必须声明（`CompileOptions.validate`）。
- `Result` 参与 unify 而非事后比对，所以它能定死 `[]` 的元素类型、能在重载里选签名。
- `Doc`/`ResultDoc` 是唯一不进 digest 的东西（`ArtifactDigest` 清它们）。改文案不该让已部署的 artifact 失效。
- 函数名可以带点（命名空间与版本：`route.score_v1`），变量名不能带点，`.` 留给将来的字段访问（`IsValidVariableName`）。

### 管线（单向，每步产物不可变）

```text
Parse / ImportExprJSON → finish(check) → Expr AST → inferProgram → 常量折叠 → Artifact(+digest) → Instantiate → Run/RunValues → Value
syntax/parser.go syntax/json_ast.go  syntax/walk.go   compile/infer*.go  compile/fold.go  compile/compiler.go  machine/vm.go  machine/frame.go
```

能力边界由**注册表**决定：`CoreRegistry()` 是极简内核（14 个函数名、零惰性形式），`registry.EnableForm(SwitchForm/ForForm/ReduceForm)` 逐个打开（`compile/forms.go` 校验），`RegisterArrayPrimitives` 加列表原语。一个注册表 = 一个控制台。

**语言刻意不图灵完备**：没有无界循环，所有形式只遍历有限输入，每个程序都终止 —— `Fuel` 是成本上限而非安全兜底。想加回无界循环前先读 `docs/termination.md` 的附录。

### 必须守住的不变量

- `machine/registry.go` 的 Registry 是唯一类型权威；函数身份是**完整签名** `name(参数)->结果`，同名不同签名即重载。名字的形状与保留字只有一个权威（`IsValidFunctionName`/`IsValidVariableName`/`IsReservedName`），parser、导入器与 registry 都调它。
- Artifact 冻结签名与 fuel 成本；`Instantiate` 校验版本、digest、签名存在性、cost 未变与每条指令。装载时**不再重新解析 ExprJSON** —— 执行只依赖字节码，digest 已经保护了 ExprJSON。
- digest = 清空 `Digest`、`Args[i].Doc`、`ResultDoc` 后 JSON 序列化再 sha256 ⇒ 改动既有字段、字段顺序或 JSON tag 都会让旧 Artifact 失效，必须 bump `ArtifactVersion`。ExprJSON 的字段顺序就是节点 struct 的字段顺序，所以**给节点 struct 重排字段也算改 digest**。
- **opcode 的一切在一张表里**（`machine/opcode.go`）：名字、栈效应、校验规则，按 opcode 索引所以顺序不可能错位。加 opcode = 表里加一行 + `frame.step` 加一个 case，`TestEveryOpcodeIsExecutableAndNamed` 会抓住漏掉的那一半。
- **常量折叠**（`compile/fold.go`）：不读参数也不读循环变量的子表达式在编译期用真 VM 跑掉（`EvaluateClosed`），折叠失败就原样发指令 —— 所以语义绝不变，这正是它能安全穿过惰性 `if` 的原因。折叠是传递的（`let(a = 250, a * 4)` 整个折掉），折成常量的绑定**不占局部槽**。常量池只存标量，容器自然回退。
- `Artifact.MaxStack` 由编译期栈效应累加得出，帧据此一次预留，`push` 在预留内跳过上限检查；算错只影响优化不影响正确性（`push` 的回退路径仍检查）。`release` 只清用过的部分，靠 `reserved` 与溢出水位。
- 类型推导是多候选分叉 + `implicitTypeScore` 打分选最优，同分报歧义；混合数值签名额外吃 `mixedPenalty`，所以 `risk < 0.5` 会把 `risk` 推成 float。
- 枚举是 nominal 且只从契约进入程序：`EnumExpr`（`@member` / `@enum.member`）的所属枚举由 `compile/enum.go` 的 `collectEnums`/`resolveEnumReference` 在**契约的枚举命名空间**里解析，不靠上下文类型；`enum` 与 `string` 不 unify，编译成字符串常量，运行时值仍是成员名。
- `SwitchExpr.Value` 可为 nil（条件形态），`SwitchCaseExpr.Match` 是列表（多值分支）。改这里只动 `syntax/ast.go`（tag 决定 JSON、作用域、前端布局）加 `compile/infer_expr.go`、`compile/compiler.go` 的语义，以及 `web/funroute-source.js` 的打印。
- ExprJSON 文档只有 `{version, expr}`。`web/funroute-designer.js` 的 `EXPR_JSON_VERSION` 必须与 `syntax/json_ast.go` 的 `ExprJSONVersion` 同步。
- 中缀与关键字糖全部在 parser 层脱糖，**AST 不新增任何节点类型**：`a+b` 就是 `add(a,b)`，`a&&b` 就是 `if(a,b,false)`，`[e for x in xs if c]` 就是 `ForExpr`。新增糖必须同时更新 `web/funroute-source.js` 的 `INFIX`/`sugarFromIf`/`forHead` 反向打印。
- `and`/`or`/`not`/`ne` 是**派生形式**——展开为 `if`，不进注册表。三个同步点：`machine/catalog.go` 的 `derivedForms`、`web/funroute-source.js` 的 `sugarFromIf`/`logicalForm`、`web/funroute-designer.js` 的 `DERIVED_TEMPLATES`。
- 扩展函数按不可信纯函数对待：`recover` 在激活层兜住 panic。**值不拷贝**——容器 backing 直接交出，安全性来自只读约定（见上），任何新增的包外取值入口都必须保持"交出 backing、文档写明只读"。
- 出栈返回的是栈上视窗，不是副本：`EvalFunc` 收到的 `[]Value` 只在调用期间有效。
- 没有递归就没有嵌套激活：每次 `Run` 只建一个帧（来自 `sync.Pool`）。新增任何能重入程序的构造都会推翻 `docs/termination.md` 的定理 A 与 B。
- `Kind`/`OpCode` 是 `uint8` 但 JSON 是名字：`Kind` 加值要同步 `kindNames`，`OpCode` 加值要加表行。`HandleKind` 排在 `VarKind` 之后，既有 kind 的序号不变。

### 改动同步点

- 新增 opcode → `machine/opcode.go` 表 + `machine/frame.go` 的 `step`。仅此两处，测试兜底。给 `Instruction` 加 `omitempty` 字段不改变既有 digest（零值不出现在 JSON），无需 bump。
- 新增惰性形式 → `machine/registry.go` 的 `knownForms` + 节点的 `Form()` 方法（`compile/forms.go` 靠它和 `Children` 通用校验）+ `machine/catalog.go` 的 `formDescriptors`。
- 新增函数 → 只注册带 `Display` 的 `FunctionSpec`，目录与拖拽面板自动生效；要 ABI 版本就写进名字（`route.score_v1`）。按 Go 签名注册用 `Logic`（反射读签名，任意元数与嵌套，首参数可选 `context.Context`），模型函数用 `Model` 同时给单条与批量实现；引擎类型先 `DefineHandle[T]`。
- `Kind` 加值 → `kindNames` 同步；若它有运行时表示，`Value.hasType`/`Type()`/`Any()`、`compile/infer.go` 的 `typeTerm`（含 `name`）、`implicitTypeScore`、`ParseType`、`validateTypePattern` 与前端 `typeName` 都要认识它（`HandleKind` 是现成范例）。
- 新增 ExprJSON 节点 → 在 `syntax/ast.go` 定义带 tag 的 struct（含 `kind()`，需要时 `check()`/`Form()`）并加进 `nodeTypes`；导入、导出、作用域、schema、前端 `cleanNode`/空白模板/卡片全部自动生效。仍要手写的是语义：`compile/infer_expr.go`、`compile/compiler.go` 的 case，`web/funroute-core.js` 的 `expressionSource`/`_splitNode` 打印，以及可选的 `FIELD_TEXT` 文案。`walk_test.go` 的 schema 测试会要求列出新节点。
- 新增公开 API → `lang/lang.go` 加别名或转发，并在 `lang/lang_test.go` 以宿主视角用一次；能不加就不加。
- 新增前端例子 → 只改 `web/funroute-examples.js`（例子是**源码 + 契约**两部分，点按钮走 `/api/parse`，按钮由 `renderExamples` 生成）。别手写 ExprJSON 模板。
- 新增前端文件 → 加进 `web/embed.go` 的 `//go:embed` 列表。前端分层：`funroute-source.js`（纯函数：schema 驱动的 `cleanNode`/`blankNode`、打印/格式化/词法，无 DOM）、`funroute-contract.js`（契约面板，宿主数据）、`funroute-designer.js`（表达式画布，Web Component，卡片由 `catalog.nodes` 布局）、`funroute-examples.js`（例子）、`app.js`（MVP 外壳，把两部分组合起来提交）。**契约面板与画布之间没有同步点**：前者是宿主数据，后者是表达式，各自提交。
- 新增 API 字段 → 改 `mvp/server.go` 的 `expressionRequest`（`DisallowUnknownFields` 会拒绝未声明字段）；CSP 是 `script-src 'self'`，前端保持无框架无构建。

## 命令

```bash
make ci                                                # 提交前必须全过：check-fmt vet lint build test
make test | make lint | make vet | make fmt | make run
go test ./lang/internal/compile -run TestIfIsLazyAndFuelIsEnforced -v      # 单个测试
go test ./lang/internal/compile -bench . -benchtime 2000x                 # VM 基准
go test ./lang/internal/compile -bench RunPaths -cpuprofile /tmp/cpu.out  # 热路径 profile
go test ./lang/internal/compile -bench Vector -benchtime 1s                # 向量透传：耗时必须与 n 无关、0 allocs
go test ./lang/internal/compile -bench BatchVersus -benchtime 2000x        # 批处理对比：合批摊薄引擎开销
go list -deps ./lang/internal/machine | grep funroute                     # 验证依赖方向
go run ./cmd/funroute inspect -expr 'if(a,b,add(1,1))'
go run ./cmd/funroute run -expr 'reduce(x in items, total from 0, total + x)' -args '{"items":[1,2,3]}'
go run ./cmd/funroute run -expr 'let(bps = 250, amount * bps / 10000)' \
  -types 'amount=int' -args '{"amount":100000}'          # 契约由 -types 给出
```

## 开发规范（`make lint` 强制，`tools/lint` 实现）

- 单个方法不超过 **50 行**（含签名与右大括号）。
- 嵌套不超过 **3 层**（if/for/switch/select/函数字面量各计一层，`else if` 不额外计）。
- 单个文件不超过 **800 行**（`.go` 与 `.js`）。
- 超限时拆函数或拆文件，不要放宽阈值；`tools/lint` 只用标准库，保持零依赖。
