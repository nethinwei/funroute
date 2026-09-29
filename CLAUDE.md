# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

FunRoute：面向支付路由的强类型、纯表达式、必然终止的语言。module `github.com/nethinwei/funroute`，Go 1.26，**零第三方依赖**（`go.mod` 保持为空），主分支 `main`。

本文件只写**读代码看不出来、违反了会出事**的约束。其余去这些地方找：

- 语言用法与文法：`docs/language.md`、`docs/money.md`、`docs/contracts.md`、`docs/go.md`，文法在 `docs/grammar.md`；README 只是首页。改语义前先读，改完同步。
- 设计取舍与**否决过的方案**：`docs/roadmap.md` 的「关键设计决策」。改设计前先看，不要把否决过的东西加回来；新决策也记在那里。
- 终止性论证：`docs/termination.md`。新增形式或能凭空造容器的函数时同步它。
- 机制细节：代码与注释。本文件里的名字都是定位用的入口。

## 包与导入

```text
funroute.go          唯一的公开包，根目录唯一的 Go 文件：只有别名、转发、构造、访问与选项
lsp/                 语言服务（LSP + stdio）
extensions/std/      标准库，只用公开 API
internal/kit/        多个包共用的同一段逻辑：错误分类、名字与数字字符、切片投影与查重、JSON 数字 ← 只依赖标准库
internal/money/      金额、比例、汇率、币种表、舍入与分摊的 Go 运算   ← 只依赖 kit
internal/machine/    值、类型、字节码、VM、注册表、目录、清单、内核库  ← 只依赖 money
internal/syntax/     词法、语法、AST、ExprJSON、格式化、语法树       ← 只依赖 machine
internal/compile/    推导、编译、常量折叠、契约、Analyze             ← 依赖 syntax + machine
internal/demo/       演示控制台：宿主组装注册表的范例，工作台用它（宿主侧）
examples/            可运行的 Go 宿主程序，只用公开包；只放宿主程序，不放规则示例
tests/api/           公开面的测试与 Example*，按主题组织（宿主侧）
tests/conformance/   web/funroute-examples.json 的每个示例经公开 API 跑完整条流水线
tests/limits/        生成 docs/limits.md 的表格并守着它们（make limits）
tests/golden/        行为金库：随机程序在固定输入上经 Program 与 RunValues 的答案与失败，逐字节比对（make golden）
tests/perf/          性能报告程序，写进 docs/perf.md（make perf），不进 CI 的判定
tests/perf/expr/     与 expr 的对照：单独的 module，只有它依赖 expr，根 go.mod 保持为空
web/src/ web/wasm/   工作台前端（TS）与浏览器里的语言服务（js/wasm）
cmd/funroute cmd/playground CLI 与工作台静态服务
```

- 依赖严格单向（`go list -deps` 验证）；kit 在最底层，所有实现包与 `lsp/` 都可以用它，表里不再逐一写。
- **同一段逻辑只写一处**：两个包以上都要的放进 `internal/kit`，只有一个包要的留在那个包（奥卡姆剃刀：kit 不收"将来可能用到"的东西）。kit 不懂语言，不放任何带 FunRoute 语义的代码。
- **导入规则由 `tools/lint/imports.go` 强制**：实现包只有 `funroute.go` 与 `lsp/` 可以导入；`tests/`、`examples/` 与宿主侧包 `internal/demo` 只能用公开包，谁都可以导入 `internal/demo`。Go 的 internal 规则挡不住本模块自己的 `extensions/`、`examples/`、`tests/`、`cmd/`、`web/wasm`，所以需要这条检查。
- machine 的文件按领域加前缀：`money_*.go` 是金额，`host_*.go` 是宿主绑定，`lib_*.go` 是内核库（常用的聚合、数组、选择、字典、字符串、数值函数；低频的与金额重载在 std）。
- value/container/convert/vm/frame 必须同包：VM 直接操作 `Value` 的私有 backing，拆开就只能走公开 accessor。
- 跨层要用内部件时，导出一个**语义明确的入口**（如 `EvaluateClosed`、`Resolve`），不导出零件。

## 红线

**公开面**
- `funroute` 不导出任何宿主能写字段的数据类型：值只能经构造函数、`Parse`/JSON 或注册表得到，经访问方法读，JSON 形状由 `MarshalJSON` 固定。新增值类型必须同时给构造、访问与 JSON。
- machine 为 compile/syntax 准备的内部构造入口不在公开包转发。`Registry` 是类型别名，它的方法宿主都能调，所以这类入口写成以 `*Registry` 为参数的包级函数，不写成方法；money 给 machine 的入口同理（`money/machine.go`）。
- AST 不公开，程序一律用 ExprJSON 交换。`tests/api/value_test.go` 的类型断言块把每个公开类型写一遍。

**值的边界：零拷贝、零分配**
- 容器的 backing 就是原生 Go 值（`[]float64`、`map[string]int64`、`[]Money`），交给 `Value` 与从 `Value` 取出的都不复制，从那一刻起**只读**。新增取值入口必须保持"交出 backing、注释写明只读"。唯一的 backing 表是 `machine/container.go` 的 `natives`；读 backing 的 switch（容器操作、`fromGo`、`host_access.go`）为了热路径不改成间接调用，由 `TestEveryBackingIsHandledEverywhere` 逐项对照。
- float 按 IEEE 754（与 Go 一致）：NaN、±Inf 是合法的值，float 运算从不失败；`==`/`<` 与成员判断（`in`、`index_of`、`unique`）按 IEEE，排序按 Go 的全序 `cmp.Compare`（NaN 最前），`min`/`max` 与 `arg_min`/`arg_max` 遇 NaN 得 NaN 或它的下标；边界上不扫 float。int 照旧检查溢出。
- 不变量在值诞生处确立（币种已声明），使用处不复查；深度扫描会让大向量每次多花微秒级时间。金额容器在边界上做一次只读 O(n) 扫描，这是唯一的例外。
- 边界上不许分配：`TestArgumentChecksDoNotAllocate`、`TestProgramScalarsDoNotAllocate`、`BenchmarkVectorPassThrough`（耗时与长度无关）守着。`unsafe` 只在 `container.go`（原生数组的指针与长度）与 `host_*.go`（宿主内存）里用。
- 原生数组在 `Value` 里是首元素指针加长度：box 是 `*T`、`i` 是长度，切片装进接口要分配切片头，指针不用。只经 `container.go` 的 `arrayOf` 装入、`nativeItems`/`itemsAt` 取出，不再把切片本身放进 box。**每个数组 `Value` 的 `i` 都是它的长度**（嵌套数组用 `nestedOf`、视图用 `viewed`、arena 形式在建成时写入），`length()` 与 `len` 只读它。

**金额**（规则见 `docs/money.md`，取舍见 roadmap）
- 金额运算只写在 `internal/money` 的 Go 方法里，内核与 std 只做 Value 转换，所以宿主与规则永远同一个答案。语言能做而 Go 做不到的运算就是缺口。
- 一律 int64（比例、汇率、精确金额都是约分的分子/分母，中间积 128 位），不用 `math/big`；放不下是 `ErrArithmetic`，从不悄悄舍入。
- 金额类永不经过 float：小数字面量是 `money.Decimal`（`LiteralExpr.Decimal`），读作比例用 `Decimal.Ratio()`，读作 float 用 `LiteralExpr.Float()`（不精确就报错）；边界上 float64 进不了金额类（`money_coerce.go` 的 `exactInput`）。
- 没有默认舍入：落到最小单位之间的内核运算注册两次（`registerRounded`），不带模式的标 `exactStep`，只能写在 `round(…, @mode)` 里；精确值不出 `round`（`compile/exact.go`），不折叠进常量。
- 换汇只在 `using` 里；`using` 永远隔离、单跳、同一货币对后写的赢；找不到是 `ErrNoFxRate`。币种在运算处检查（`money` 的 `meet`），契约不约束币种。
- 不用金额的程序 digest 逐字节不变（`TestMoneyCapabilityKeepsDigests`）。

**终止性与运行时长**
- 只有调用了宿主函数（含纯函数，`regProgram.foreign`）的运行才设 `recover`：内核与内核库的代码不许 panic，`FuzzProgramsNeverPanic` 守着。新增内核函数时，所有失败都要走 error 返回。
- 语言不图灵完备：没有递归、没有无界循环，每次 `Run` 只有一个帧。任何能重入程序的构造都会推翻 `docs/termination.md` 的定理 A 与 B。
- 没有 fuel：运行多久由宿主的 `ctx` 决定，宿主调用前与循环每 `checkEvery` 轮（`machine/limit.go`，约 1 ms）查一次。新增会循环的执行路径（新的循环指令、向量的新走法）必须经 `turn` 计数，否则长循环停不下来。编译期折叠只按轮数（`foldTurns`）限制，不看时钟：同一份源码在哪台机器上都编出同一个 artifact。
- 凭空造容器的函数标 `Doc.BoundedArgs`，整数实参必须由输入规模界定；扩展函数的计算量只能随输入**规模**增长，不能随参数**数值**增长（设上限或换算法，如 `range`/`pad`/`allocate` 的 10000、`pow` 的平方求幂）。
- 常量折叠只碰 `Doc.Constexpr` 授权的函数：内核与 std 有，模型、时钟、远程调用**不标**。闭合表达式的失败是编译错误。

**Artifact 与 digest**
- digest 覆盖除 `Digest`、`Args[i].Doc`、`ResultDoc` 外的全部 JSON：改既有字段、字段顺序或 JSON tag，旧 artifact 就失效。ExprJSON 字段顺序就是节点 struct 的字段顺序。
- `ArtifactVersion`/`ExprJSONVersion`/`CatalogVersion` 只标识当前形状。还没有对外承诺兼容：形状要改就直接改，不留迁移台阶。
- 装载时不重新解析 ExprJSON，执行只依赖字节码。

**语法**
- 糖在 parser 里脱糖，不新增节点（`a+b` 即 `add(a,b)`，`->` 即 `convert`）。唯一的例外是选择器 `.fee`（`SelectorExpr`，格式化要读回它）：编译前由 `syntax.ExpandSelectors` 展开成推导式，推导、编译、折叠都看不见它，别给它写语义；节点只在 `syntax/ast.go` 定义，struct tag 驱动导入、导出、作用域与语法树，不按节点类型分派。
- **格式化结果必须解析回同一 ExprJSON**（`FuzzFormatRoundTrip` 守着）。新增糖要同时给打印器一条反向读法（`operators.go` 的 `operatorSpec.read`）。
- 嵌套至多 1000 层：Go 栈溢出是 `recover` 接不住的致命错误。ExprJSON 只解码一次，值槽位不接受 `null`。

**错误**
- 宿主只用 `errors.Is`；新增错误路径必须选一个类别。`fallback` 只接"数据暂不可得"（`ErrExtension`、`ErrDeadline`、`ErrNoFxRate`），规则或数据的错（`ErrArithmetic`、`ErrDomain`、`ErrCurrency`）不接，扩展函数里发生也保留原类别。
- 编译错误必须带位置，只经 `syntax.At`、`syntax.Around(node, …)` 或 parser 内部的 `over` 产生。

**执行性能**
- 执行的是装载时翻译出的寄存器形式（`lower*.go` → `regvm*.go`），栈字节码只是 Artifact 的格式：翻译不改 Artifact、不改 digest。
- 寄存器操作 `rinstr` 保持 16 字节、按指针读：操作数放进 a/b/c，放不下的进 `regProgram` 的旁表（`calls`/`loops`/`makes`），需要换算的在翻译时算好。
- **寄存器分三组、共用一套编号**（`regvm_banks.go`）：int 与 bool（0/1）在 `ints`，float 在 `floats`，其余在 `regs`（`Value`），由验证器证明的种类决定（`proof.kinds`）。翻译器的栈是 `slot{寄存器, 种类}`，按种类选操作（`move_i`/`add_i`/`field_b`/`at_d_i`/`collect_next_i`…），运行时不看 kind。`Value` 只在要值的地方就地做出：非直调的调用（`rcall.boxes`）、构造（`rmake.boxes`）、字典的条目、答案（`regProgram.result`），**不为此发操作**。新增操作要说清操作数与结果在哪组，并记下 `opKinds`：`fault` 与向量按它从各组取值。
- `frame.exec` 只放最常用的操作（50 行的上限也是它的上限），其余进 `cold`：`rCall` 之前的内核操作查 `coldKernels` 表（`TestEveryKernelOpHasAColdStep`），之后的进 `structure`。**热路径的操作码排在最前、连成一段**：取值范围超过 case 数的四倍，switch 就不再是跳转表。专用内核指令只写快路径，答不出就返回 false，由 `fault` 问函数本身要错误，文案一字不差。
- 形状常见的 Go 函数（`pureOf`）在候选分支外直接从各组调用（`rcall.banked`），纯的不看截止时间，宿主的照旧先看；在 fallback 的候选里一律按值调用，由翻译器静态决定。
- 改了翻译器、`rinstr` 或 `frame.exec`，与改动前交替跑 `BenchmarkDispatch`/`BenchmarkCall`/`BenchmarkRunPaths` 对照，再 `make perf`。
- 值流分析（`lower_escape.go`）决定数组建在哪：不逃出运行的建在帧的 arena 槽里，box 是指向槽的指针（`*[]T`），只有翻译器为它选的指令（循环、`at_a`）见得到；答案建在宿主借出的槽里（`RunInto`）。**指针形式的 box 绝不能流到宿主函数、结果或容器里**——新增会读或放出数组的指令，先在值流分析里给它规则。帧里不存指向宿主 struct 的指针（它可能在宿主的栈上），只拷切片头。
- 纯标量字段的 record 数组以视图（`recordsView`）为 backing：新增读数组的路径（`length`、`at`、`Slice`、`Array`、`elemType`、写回）都要认得它，派生视图（`perm`、子视图）不指向帧；只有 `rloop.itemsInPlace` 的循环复用 `f.items`，每项只装循环体读到的字段（值流分析的 `itemFields`：新增读循环项的栈指令要在那里记下它读的字段），并置 `frame.walked` 让收尾清掉。
- 帧的 `release` 只清这次运行写过的部分：清含指针的内存要走写屏障——多清三个空槽就让固定开销从 30 ns 变成 60 ns。
- `Session` 独占一个帧（`frame.owned`，不回池），给单个 goroutine 反复运行；还帧一律经 `putFrame`。`exec` 只交回答案所在的寄存器，调用方在收尾前取值一次。
- 帧池只用 `sync.Pool`，热路径上不许有跨 goroutine 共享的可写状态：一个用原子操作取还的共享空闲帧，曾让 10 核并行比单核还慢（`BenchmarkRunParallel` 用 `-cpu 1,10` 看扩展）。
- 向量（`lower_vector.go`、`regvm_vector*.go`）只跑逐列运算与折叠、收集、停止；遇到失败或停止，就把循环交回标量循环体，由它重跑那一项。所以向量只写快路径，`intOp`/`floatOp` 与内核指令必须逐项同义（`TestVectorOpsAnswerAsTheKernel`、`TestTheVectorAnswersAsTheBody` 守着）。少于 `vecShortest` 项的循环不进向量，对照测试要用更长的输入。
- 类型在装载时由 `machine/verify.go` 证明一次，执行路径不再检查内核运算的结果、容器的元素、循环收集的值与局部槽是否绑定；只检查宿主函数的回答。新增会产生值的执行路径，先让验证器能证明它的类型。

**Go 只做语言**
- Go 只输出语言事实（词法、语法、类型、诊断、格式化），不输出颜色、布局、控件、文案。前端只经 LSP 拿事实，不理解 ExprJSON、不写语法规则；结构视图的每次修改都是对原文区间的替换。

**record 与契约**
- 字段顺序就是类型。Go struct 只有带 `funroute:"name"` tag 的导出字段进 record，不从 Go 名推断（推断会让 Go 侧改名悄悄改掉契约）。
- 契约在宿主：程序文本只是表达式，参数、类型、返回类型由 `CompileOptions` 传入；`Args` 的顺序即 ABI。

## 改动同步点

| 改了什么 | 还要动哪里 |
|---|---|
| 新增 opcode | `machine/opcode.go` 表一行 + `verify.go` 的类型规则 + `lower_instr.go` 的翻译规则（`TestEveryOpcodeIsExecutableAndNamed` 兜底）；要新的寄存器操作就在 `regvm_ops.go` 加一行、在 `exec` 或 `cold` 里执行；会跳转的在 `leadersOf` 里另起一块 |
| 新增会产生、读或放出数组的栈指令 | `lower_escape.go` 的规则：它让数组留在运行里还是逃出（`TestValueFlowFindsWhereEachArrayGoes` 加一行） |
| 向量要跑新的运算 | `lower_vector.go` 的 `elementwise`、`regvm_vector_run.go` 的 `resultKind`、`regvm_vector_ops.go` 的列运算；失败条件与内核指令一字不差 |
| 内核函数要专用指令 | `lower_kernel.go` 的 `kernelOps` 一行（结果按种类分组的在 `lower_instr.go` 的 `kernel` 里选变体）+ `regvm_ops.go` 的操作（排在 `rCall` 之前）+ `regvm_kernel.go` 的快路径 + `regvm_cold.go` 的 `coldKernels` 一行（`TestKernelOpsAnswerAsTheirFunctions` 在边界值上对照函数） |
| 新增形式 | `machine/catalog.go` 的 `languageForms` 一行（可开关的写 `optional`，能包进表达式的写 `wrap` 模板）；节点的 kind tag 写 `,form`（可开关的再写 `,optional`，`TestTheOptionalFormsAreTheMachines` 对照）；termination.md |
| 新增 ExprJSON 节点 | `syntax/ast.go`（嵌入 `Node` 并写 `kind` tag，字段带 tag，加进 `nodeTypes`）；语义在 `compile/infer_expr.go`、`compiler.go`；打印在 `syntax/print.go`、`format.go`；按节点分派的还有 `compile/enum.go`、`fold.go`；示例要用到它（`lsp/funroute_test.go` 检查） |
| 改语法 | `docs/grammar.md` 的文法；跑 `go test ./internal/syntax -run XXX -fuzz FuzzFormatRoundTrip -fuzztime 60s` |
| 新增函数 | 只经 `Registry.Register` 注册 `FunctionSpec`（手写 `Params`/`Result`/`Eval`，或填 `Go`/`GoBatch` 按 Go 签名反射），目录与 LSP 自动生效；`Doc` 是唯一的函数元数据结构；ABI 版本写进名字（`route.score_v1`） |
| 新增官方函数或重载 | 常用的进内核库（`lib_*.go`，经 `librarySpecs` 注册，直接操作 backing），其余进 std；补案例（内核 `machine/examples.go`，std `extensions/std/examples.go`），同名的内核重载与 std 金额重载各挂各的案例，测试要求合起来选中每个重载；一个名字服务多种元素类型用 `libEach`/std 的 `eachType`，std 的在 `TestNamesCoverEveryElementTypeTheyClaim` 加一行 |
| 新增纯函数 | 标 `Doc.Constexpr` |
| 新增聚合函数 | 能用一个内核函数一步步折叠、或遇到某个 bool 就停的，声明 `FunctionSpec.Fold`；融合后的答案与失败与按原样编译的一致（`internal/compile/aggregate_test.go` 对照） |
| 按 Go 签名注册的新常见形状 | `host_direct.go` 一个 case，在 `TestDirectCallsAnswerAsReflection`（std 用的形状在 `TestTheStandardShapesAreCalledDirectly`）加一个该形状的函数；标量形状再在 `host_pure.go` 的 `pureOf` 加一个直接读写各组的，`TestPureCallsAnswerAsTheirEval` 加一个 |
| 新增凭空造容器的函数 | 标 `Doc.BoundedArgs`，理由写进 termination.md 定理 B 之后 |
| 新增读运行状态的内核函数 | `FunctionSpec.readsRun`（不折叠，要求写在 `using` 里） |
| 新增金额函数 | 先在 `internal/money` 的 Go 方法上实现；会舍入的内核运算用 `registerRounded`；非内核函数自己检查容器里币种一致（std 的 `amountsOf`）；宿主先 `DeclareMoney` 再注册 std |
| 改 `Doc` 里影响编译的字段 | `machine/manifest.go` 的 `ManifestFunction` |
| `Kind` 加值 | `kindNames`；有运行时表示的还有 `Value.Type()`/`Any()`/`Equal`、JSON（`jsonLeaf`、`coerceWith`）、`compile/infer_solve.go` 的 `implicitScores`、`namedTypes`（`ParseType`）、`lsp/sample.go`；Go 形式不是按形状读的（`time.Time` 是 struct），进 `ownGoKind` 与 `loadOwn`/`storeOwn`、`fromGo`/`FromValue` |
| 新增原生 backing | `machine/container.go` 的 `natives` 一行（装入取出只经 `arrayOf`/`nativeItems`）与 `host_plan.go` 的 `native` 常量；`TestEveryBackingIsHandledEverywhere` 指出每个还要补的 switch |
| `reflectType`/`fromGo` 新增非容器的 Go 类型 | `machine/host_plan.go` 的 `newCodecFor` 与 `host_access.go` |
| 新增公开 API | `funroute.go` 对应的一节；新类型进 `tests/api/value_test.go` 的断言块，并在 `tests/api` 以宿主视角用一次。能不加就不加 |
| 有意改变了某个答案或失败 | `make golden` 重写 `tests/golden/testdata/outcomes.jsonl`，逐条读 diff；只改执行方式时金库必须一字不差 |
| 改了 docs/limits.md 表格里的行为 | `make limits` 重新生成，读一遍 diff；新增边界就在 `tests/limits` 的用例表加一行，说明写在用例里 |
| 新增例子 | 只改 `web/funroute-examples.json`（源码 + 契约 + 入参 + 期望值）；示例合起来要覆盖演示注册表的全部函数、形式、运算符与节点 |
| 新增语言服务能力 | `lsp/`：标准方法优先，专有的用 `funroute/*` 或 `workspace/executeCommand`；`web/wasm` 只是传输 |
| 新增颜色 | 只在 `web/tokens.css`，用 `light-dark(浅, 深)`；组件只消费 token |
| 新增前端代码 | `web/src/*.ts`；组件经 `ui.ts` 的 `define` 注册并加进 `package.json` 入口；位置一律按 UTF-16 数（`projection.ts` 的 `offsetAt`）；运行时依赖只加实际导入的包 |
| 新增错误类别 | `machine/errors.go` 的 `errorClasses` 一行（最具体的在前，写明 `fallback` 接不接）与 `docs/go.md` 的错误表（`TestTheErrorTableIsTheErrorClasses` 对照） |
| 新的设计决策 | `docs/roadmap.md` 的「关键设计决策」 |

## 命令

```bash
make ci        # 提交前必须全过：格式、import、前端检查与构建、vet、staticcheck、modernize、golangci-lint（均含 js/wasm）、deadcode、lint、build、wasm、Go 与 JS 测试
make ci-linux  # 推送前：在 Docker 里按 GitHub Actions 的机器（linux/amd64）跑 make ci，看出架构带来的差别（如 arm64 的 FMA 融合）
make test | make lint | make vet | make fmt
make test-example  # 只跑读 web/funroute-examples.json 的测试：一致性流水线、LSP 运行与覆盖（每个重载、运算符、节点）、格式化往返
make wasm      # web/dist/funroute.wasm
make web       # web/dist/*.js（先在 web/ 里 npm install；产物不提交）
make run       # 构建并在 http://127.0.0.1:8080 服务工作台
make limits    # 重新生成 docs/limits.md 的表格（go test ./tests/limits -update）
make golden    # 重写行为金库（go test ./tests/golden -update），只在有意改变答案时
make perf      # 性能报告写进 docs/perf.md，需要时再提交
go run ./examples/routing                                          # 宿主程序示例：routing、money、batch
go test ./internal/compile -run TestIfIsLazy -v                    # 单个测试
go test ./internal/machine -bench . -benchtime 2000x              # VM 基准
go run ./cmd/funroute run -expr 'let(bps = 250, amount * bps / 10000)' -types 'amount=int' -args '{"amount":100000}'
```

检查工具（staticcheck、modernize、goimports、deadcode、golangci-lint）按 Makefile 顶部的固定版本装进 `.tools/`，不是依赖；golangci-lint 启用哪些检查见 `.golangci.yml`。

## 代码规范

- `make lint`（`tools/lint`，只用标准库）强制：函数 ≤ **50 行**，嵌套 ≤ **3 层**（函数字面量也算一层），文件 ≤ **800 行**。超限就拆，不放宽阈值。
- staticcheck 开全部检查：每个包有包注释（internal 包写在 `doc.go`），导出标识符的注释以名字开头。编译期断言用包级 `var _ T`。
- modernize：用当前 Go 更直接的写法。
- **检查工具报出的每一条都要处理，没有"有意为之"**：`make ci`（含 golangci-lint 与 `deadcode -test`）与 gopls/IDE 诊断都算。不用 `//lint:ignore`、`//nolint` 遮盖。处理必须**绝对无害**：行为、`errors.Is` 的结果、错误文案、输出（含 nil 与空切片的区别）、digest 都不变，分配与耗时不变差；对外能力（公开包走得到的 API）不因"仓库里没人调"而删，补测试。做不到无害就停下来说明，不为消一条告警改语义。
- import 两组：标准库、空行、本模块，组内按字母序（`goimports -local github.com/nethinwei/funroute`）。

## 测试规范

`tools/lint` 强制前五条：

- ctx 用 `t.Context()`（基准 `b.Context()`）。
- 不睡：依赖定时器的测试跑在 `testing/synctest` 上。
- 基准写 `for b.Loop()`。
- 测试辅助函数第一句 `t.Helper()`。
- **`foo_test.go` 对应同目录的 `foo.go`**。例外：`example_test.go`、`export_test.go`，以及只有 `doc.go` 的测试套件目录（`tests/` 下的 `api`、`conformance`、`limits`、`golden`）。没有 `helpers_test.go`。测 machine 行为又要先编译的测试写成 `package machine_test`，内部件经 `export_test.go` 暴露。

其余靠评审：表驱动测试每个用例一个 `t.Run`；独立的测试第一句 `t.Parallel()`（`testing.AllocsPerRun`、synctest、共享状态、计时敏感的除外）；失败信息写出输入、实际与期望。
