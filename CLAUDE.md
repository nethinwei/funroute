# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 规格（薄）

FunRoute：面向支付路由的强类型纯表达式语言。module `funroute`，Go 1.26，**零第三方依赖**，当前目录**不是 git 仓库**。语法、类型与推导规则的完整说明在 `README.md`，改语义前先读它、改完同步它。

管线（单向，每步产物不可变）：

```text
Parse / ImportExprJSON → Expr AST → inferProgram → Artifact(+sha256 digest) → Instantiate → Run → Value
lang/parser.go           lang/ast.go  lang/infer*.go  lang/compiler.go        lang/vm.go   lang/frame.go
```

能力边界由**注册表**决定，不存在第二套 profile 机制：`CoreRegistry()` 是极简内核（10 个函数名、零惰性形式），`registry.EnableForm(SwitchForm/ForForm/ReduceForm/RecurForm)` 逐个打开惰性形式（`lang/forms.go` 做校验），`RegisterArrayPrimitives` 加列表原语。一个注册表 = 一个控制台；启用 `recur` 就是图灵完备层，靠 fuel 与 `MaxRecursion` 拦截。MVP 只持有一个注册表（`paymentdemo.NewRegistry()`，demo 里全开），CLI 的 `-profile` 只是“构造哪种注册表”的宿主概念——语言里没有 profile。

必须守住的不变量：

- `lang/registry.go` 的 Registry 是唯一类型权威；函数身份是**完整签名** `name(参数)->结果`，同名不同签名即重载。
- Artifact 冻结签名与 fuel 成本；`Instantiate` 校验版本、digest、签名存在性、cost 未变与每条指令，注册表漂移一律拒绝装载。
- digest = 清空 `Digest` 后 JSON 序列化再 sha256 ⇒ 改动既有字段、字段顺序或 JSON tag 都会让旧 Artifact 失效，必须 bump `ArtifactVersion`。
- 参数顺序 = 自由变量在规范化 AST 中首次出现顺序；dict key 排序保证 ExprJSON 往返后 args ABI 不变。
- 类型推导是多候选分叉 + `implicitTypeScore` 打分选最优，同分报歧义；调便利规则改打分函数，别在推导里加特判。混合数值签名（`(int,float)`）额外吃 `mixedPenalty`，所以 `risk < 0.5` 会把 `risk` 推成 float 而不是"int 提升为 float"这个更便宜的读法。
- `SwitchExpr.Value` 可为 nil（条件形态，`Match` 是 bool 条件），`SwitchCaseExpr.Match` 是列表（多值分支，任一命中）。推导上把条件形态当作“主体是 bool”，于是两种形态共用同一套统一逻辑；编译时 `compileMatch` 在无主体时不发 `OpEqual`。改这里要同时动 `lang/ast.go`、`json_ast.go`、`infer_expr.go`、`compiler.go`、`forms.go` 和 designer 的 switch 卡片。
- ExprJSON 是 v2（switch 的 `match` 列表化 + `value` 可选）。`web/funroute-designer.js` 的 `EXPR_JSON_VERSION` 必须与 `lang/json_ast.go` 的 `ExprJSONVersion` 同步，否则前端提交的文档会被后端拒绝。
- 中缀与关键字糖全部在 parser 层脱糖，**AST 不新增任何节点类型**：`a+b` 就是 `add(a,b)`，`a&&b` 就是 `if(a,b,false)`，`for(x in xs, e)` 就是位置形式的 `ForExpr`。新增糖时必须同时更新 `web/funroute-source.js` 的 `INFIX`/`sugarFromIf`/`forHead` 反向打印，否则源码→节点→源码会退化成函数形式。
- 比较 `lt/le/gt/ge` 是真函数（各 5 个签名）；`!=`/`&&`/`||`/`!` 是 `if` 的糖，靠 `if` 的惰性获得短路，不要为它们注册函数。
- `//` 注释、`1_000_000` 分隔符、尾随逗号是纯词法糖，不进 AST，ExprJSON 往返不保留。
- `if`/`switch`/`for`/`reduce` 在 parser 阶段即成专用节点，编译为跳转/循环指令、不走 `OpCall`；`for` 与 `reduce` 共用 `OpLoopInit/OpLoopCollect/OpLoopNext`，靠 `Instruction.C`（累加器槽，`noAccumulator` 表示映射）区分折叠与映射。
- 形式校验只有一处：`CompileAST` 里的 `validateForms`。parser 不做授权检查，所以源码与 ExprJSON 自动走同一条路径，不会出现“源码拒绝、JSON 放行”的后门。
- 扩展函数按不可信纯函数对待：`recover` 在激活层兜住 panic，不暴露宿主能力。但**值不再逐个深拷贝**——安全性来自 `Value` 的字段私有 + `Array()`/`Dict()` 返回副本，所以任何新增的包外取值入口都必须保持这个性质，否则内部零拷贝就漏了。
- 出栈返回的是栈上视窗，不是副本：`EvalFunc` 收到的 `[]Value` 只在调用期间有效。新增用到 `popN` 的 opcode 必须在下一次压栈前用完它。
- 尾位置的 `recur` 复用帧（编译期 `markTail` 标记，`Instruction.C = tailCall`），所以尾递归只受 fuel 约束；参数位置的 `recur` 才受 `MaxRecursion` 约束。改这块要同时改两处测试断言。
- `Kind`/`OpCode` 是 `uint8` 但 JSON 仍是名字（`MarshalText`/`UnmarshalText`）：加枚举值必须同时加进 `kindNames`/`opNames`，否则 JSON 往返会静默变成 invalid。

改动同步点：

- 新增 opcode → `lang/bytecode.go` 常量 + `lang/compiler.go` 发射 + `lang/vm.go` 的 `validateInstruction` + `lang/frame.go` 的 `step`。给 `Instruction` 增加 `omitempty` 字段不会改变既有 artifact 的 digest（零值不出现在 JSON 里），所以无需 bump `ArtifactVersion`；改已有字段则必须 bump。
- 新增惰性形式 → `lang/registry.go` 的 `knownForms` + `lang/forms.go` 的 `missingForm` + `lang/catalog.go` 的 `formDescriptors`，漏一处就等于默认放行或目录里查不到。
- 新增函数 → 只注册带 `Display` 的 `FunctionSpec`，目录与拖拽面板自动生效（`Display.Hidden` 保留不展示）；名字带 ABI 后缀 `@1`。
- 新增 ExprJSON 节点 → `lang/json_ast.go` 导入导出两侧 + `lang/ast.go` 的 `variableCollector`（局部绑定别漏）+ `web/funroute-source.js` 的 `expressionSource`/`splitNode` + `web/funroute-designer.js` 的 `cleanNode`/`SPECIAL_NODES`/卡片渲染。
- 新增前端文件 → 加进 `web/embed.go` 的 `//go:embed` 列表。前端分三层：`funroute-source.js`（纯函数：打印/格式化/词法，无 DOM）、`funroute-designer.js`（Web Component）、`app.js`（MVP 外壳）。
- 新增 API 字段 → 改 `mvp/server.go` 的 `expressionRequest`（`DisallowUnknownFields` 会拒绝未声明字段）；CSP 是 `script-src 'self'`，前端保持无框架无构建。

## 命令

```bash
make ci                                                # 提交前必须全过：check-fmt vet lint build test
make test | make lint | make vet | make fmt | make run
go test ./lang -run TestIfIsLazyAndFuelIsEnforced -v   # 单个测试
go test ./lang -bench . -benchtime 2000x               # VM 基准（lang/bench_test.go）
go run ./cmd/funroute inspect -expr 'if(a,b,add(1,1))'
go run ./cmd/funroute run -profile engineer -expr 'if(eq(n,0),acc,recur(sub(n,1),add(acc,n)))' -args '{"n":10,"acc":0}'
```

## 开发规范（`make lint` 强制，`tools/lint` 实现）

- 单个方法不超过 **50 行**（含签名与右大括号）。
- 嵌套不超过 **3 层**（if/for/switch/select/函数字面量各计一层，`else if` 不额外计）。
- 单个文件不超过 **800 行**（`.go` 与 `.js`）。
- 超限时拆函数或拆文件，不要放宽阈值；`tools/lint` 只用标准库，保持零依赖。
