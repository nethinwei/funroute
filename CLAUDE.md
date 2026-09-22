# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 规格（薄）

FunRoute：面向支付路由的强类型纯表达式语言。module `funroute`，Go 1.26，**零第三方依赖**，git 仓库，主分支 `main`。语法、类型与推导规则的完整说明在 `README.md`，改语义前先读它、改完同步它。

### 包边界（先看这条）

```text
lang/lang.go              唯一的公开接口：类型别名与转发，约 40 个标识符
extensions/std/           标准扩展包（聚合/字符串/数组/数值/选择/分组），只用公开 API
examples/payment/         示例宿主：领域函数与控制台的组装范例，不是交付物
lang/internal/machine/    值、类型、字节码、VM、帧、注册表、目录     ← 不依赖任何上层
lang/internal/syntax/     词法、语法、AST、ExprJSON                 ← 只依赖 machine
lang/internal/compile/    推导、编译、常量折叠、契约、导出视图        ← 依赖 syntax + machine
```

依赖严格单向，`go list -deps` 可验证。**硬约束**：`Value` 的容器 backing 是私有字段，VM 的循环/索引/构造直接在上面操作，所以 value/container/convert/vm/frame 必须同包 —— 拆开就只能走公开 accessor。跨层要用 machine 的内部件时，导出一个**语义明确的入口**（`EvaluateClosed`、`IsLazyIf`、`Resolve`、`FormOf`），不要导出零件。

AST 类型**故意不公开**：宿主通过 ExprJSON 交换程序。`lang/lang_test.go` 是 `package lang_test`，只能用公开 API，所以它同时守着这条边界。

### 值的容器是原生 Go 值，边界零转换零拷贝

`array<float>` 的 backing 就是 `[]float64`，`dict<int>` 就是 `map[string]int64`，只有容器的容器用 `[]Value`（`nestedArray`/`nestedDict`）。`machine/container.go` 是唯一知道这个映射的地方（`length`/`at`/`lookup`/`keys`/`tail`/`arrayBuilder`/`packDict`），`machine/convert.go` 的 `fromGo` 是唯一的 Go 类型清单：`ToValue`、`FromValue`、`Fn1/2/3` 的参数与结果转换、`Run(map)` 的 `coerce` 全走它。宿主 `[]float64` → `RunValues` → 扩展函数 → 返回，始终是同一个底层数组（`TestHostVectorsReachExtensionsWithoutCopying` 用指针相等断言，`BenchmarkVectorPassThrough` 证明耗时与长度无关）。

不变量在**值诞生处**确立、不在每次使用处复查：`CheckedFloat`（即公开的 `lang.Float`）、`ToValue`/`fromGo` 的 `checkFloats`、`Array`/`Dict`/`Record` 的 `validateInvariant` 挡住 NaN 与 Inf，所以 `RunValues` 拿到 `Value` 只校验类型，不再深度扫描 —— 那样做曾让 65536 元素的向量每次多花 16µs（`TestBoundaryKeepsTheInvariants` 是这条推理的守卫，`TestArgumentChecksDoNotAllocate` 守住零分配）。同理 `hasType` 对 record 直接比 `recordValue.typ`，不 `CloneType` 出一个 `Type` 来比。

代价是**不可变靠约定**：交给 `Value` 的切片/映射与从 `Value` 取出的（`Array()`/`Dict()`/`Any()`/`FromValue`）从那一刻起只读，VM 自身从不原地修改。任何新增的取值入口都要保持"交出 backing，不复制"并在注释里说明只读。边界上不许分配：`fromGo` 失败返回哨兵 `errUnsupportedGoType`，标量在 `ToValue`/`FromValue` 里经 `any(&x)` 指针匹配，不装箱。

### 句柄与批处理：模型引擎的数据不进语言

`HandleKind` 是不透明宿主值：`Type.Name` 是宿主给的名字（`handle<onnx.tensor>`），`Value.s` 存名字、`box` 存 payload。语言对它只做一件事——传递：`compareEqual` 拒绝比较（`eq` 与 `switch` 共用），不能进容器字面量以外的任何运算，`Any()` 只给类型名。`Registry.handles` 把 Go 类型映射到句柄名（`DefineHandle[T]`），`Fn1/2/3`、`Model1/2` 的签名推导（`register.go` 的 `goType`）先查它再走 `fromGo`。**不要为张量设计值类型**：形状、dtype、批维度都归引擎，这里只有句柄。

批处理在 SDK 层（`machine/batch.go`），不在编译器：`PrefetchSites` 读字节码找可提升的调用，条件是参数全为 `OpLoadArg`/`OpConstant`、不在任何前向跳转或循环覆盖的区间内（`guardedInstructions`）。这三条保证提前算不改变可观察行为，`if` 的惰性对模型调用仍然成立。`Batch.execute` 对每个站点调一次 `EvalBatch`，结果按 pc 放进 `RunOptions.Prefetched`，`frame.evaluate` 命中即取，仍扣 fuel。没有 `EvalBatch` 的函数与不可提升的调用退回逐条 `Eval`。

### 预算：ctx 贯通，边界检查，错误类型化

`Run`/`RunValues`/`Batch.Run` 首参数是 `ctx`，帧持有它；`frame.invoke` 在**每次扩展调用前**检查 `ctx.Err()`，纯计算不打断。`Doc.Timeout` 用 `context.WithTimeout` 给单次调用加上限（只在设置了才分配），`Doc.Detached` 走 `callDetached`（独立 goroutine + select，参数先拷贝一份因为栈视窗会被复用）。内核函数没有这两个设置，走 `invoke` 的短路径，所以纯表达式基准不受影响。所有失败在 `classify` 里包成 `ErrDeadline` 或 `ErrExtension`，fuel 耗尽是 `ErrFuel`；新增错误路径必须选一个包，宿主只用 `errors.Is`。`Batch` 的引擎调用取批内最早 deadline（`earliestDeadline`），每个程序仍用自己的 ctx。

`Logic`/`Model` 是反射注册（`register.go` + `reflect.go`）：签名与两个方向的转换器在注册时解析一次，`intoGo` 对 backing 类型精确匹配的容器直接交出（零拷贝仍成立），其余逐层构造；调用走 `reflect.Call`，约 300 ns。不要为了省这点重新引入 `Fn1/Fn2/…` 这类按元数展开的泛型；要零开销就写 `FunctionSpec`。

### 语法节点只在一处定义

`syntax/ast.go` 的每个节点 struct 就是它的全部描述：`json:"..."` 是 ExprJSON 字段（`omitempty` = 可选），字段类型决定它在 walk 里的角色（`Expr`/`[]Expr`/`string`/`[]struct`/`bool` —— 最后一种是节点的模式开关如 `ForExpr.Flatten`，没有子节点也不绑定名字），`role:"var|fn|local|text"` 决定名字校验规则，`binds:"a,b"` 说明这个局部名在哪些字段里可见（`@rest` = 同一列表里后面的项），`default`/`min` 给前端。`syntax/walk.go` 用反射把 tag 读成 plan，导入、导出、`FreeVariables`、`Children`、`FormOf`、`NodeSchemas` 全部由 plan 驱动，**不按节点类型分派**。节点自己的规则（重名、重复键）写在 `check()` 里，parser 的 `p.node()` 与导入器的 `finish()` 都调它 —— 源码和 JSON 一套规则。反射只在编译期跑，不在执行路径上。

前端拿到的目录（`lang.Catalog(registry)`，不是 `Registry.Catalog()`）带 `nodes`：`web/funroute-core.js` 的 `cleanNode`/`blankNode` 与 `web/funroute-designer.js` 的卡片布局都从它生成，JS 里没有节点字段清单，只有 `funroute-core.js` 的文案表 `FIELD_TEXT`。端到端验证：10 个例子经 `cleanNode` 得到的 JSON 与服务端 `/api/parse` 的规范 JSON 逐字节相同。

### 画布只有控制块与表达式

一条规则贯穿画布：**控制块是卡片，其余一切是一行文本**。控制块 = 有自己 ExprJSON 节点的特殊形式（`switch`/`for`/`reduce`/`let`）加惰性调用（`if`/`fallback`），这个集合由 `controlBlocksOf(catalog)` 从目录推导，前端不写死；`&&`/`||`/`!` 底层是 `if` 但按运算符处理，所以留在文本里。表达式槽就地编辑，提交时走 `/api/parse`，**通过才替换子树**，失败原地报错且文档不动。每个表达式位置都是放置目标：块放到已占位置时，原表达式收进新块的第一个空槽（`firstEmptySlot`），节点不能拖进自己的子树。语言层面任何位置都能放任何表达式（`switch` 主体放 `let`、`case` 匹配值放 `switch` 都合法），所以画布不设位置限制。

### 契约在宿主，不在语言里

程序文本**只是表达式**。参数名、类型、顺序、说明、返回类型由宿主通过 `CompileOptions{Args []ArgSpec, Result *Type, ResultDoc}` 传入。理由：控制台本来就存规则元数据（版本、生效窗口、审批人、灰度），参数类型是同类信息，放语言里就是两份平行元数据（权衡见 README「契约」章节）。

- `Args` 非空时，**顺序即 ABI**；为空则按自由变量首次出现顺序推导。
- 声明了可以不用（调用方 ABI 稳定），用了必须声明（`CompileOptions.validate`）。
- `Result` 参与 unify 而非事后比对，所以它能定死 `[]` 的元素类型、能在重载里选签名。
- `Doc`/`ResultDoc` 是唯一不进 digest 的东西（`ArtifactDigest` 清它们）。改文案不该让已部署的 artifact 失效。
- 类型别名只是**文本契约的拼写**：`ParseTypeWith(text, aliases)` 在解析时就地展开，`CompileOptions` 上没有别名字段，所以编译器与 digest 根本看不见它。别名不嵌套（一个声明不能引用另一个），入口三处：CLI `-alias`、API 契约的 `types`、契约面板的类型行。正因为展开在前，artifact 与运行结果只认识完整的 record —— 前端要显示写下的那个名字，就得拿 `/api/contract/check` 回传的 `types`（已解析的结构化类型）去反查（`aliasOf`），这是唯一的对应方式，拿操作员键入的文本去比是另一回事。复杂类型在契约里**一律声明成别名**：参数与返回处只写名字，完整结构在类型行里展开成多行（`formatTypeText`，parser 接受换行），例子数据也按这条组织。
- 函数名可以带点（命名空间与版本：`route.score_v1`），变量名不能带点，`.` 留给将来的字段访问（`IsValidVariableName`）。

### 管线（单向，每步产物不可变）

```text
Parse / ImportExprJSON → finish(check) → Expr AST → inferProgram → 常量折叠 → Artifact(+digest) → Instantiate → Run/RunValues → Value
syntax/parser.go syntax/json_ast.go  syntax/walk.go   compile/infer*.go  compile/fold.go  compile/compiler.go  machine/vm.go  machine/frame.go
```

能力边界由**注册表**决定：`CoreRegistry()` 是极简内核（15 个函数名，其中 `if`/`fallback` 惰性），`registry.EnableForm(SwitchForm/ForForm/ReduceForm)` 逐个打开（`compile/forms.go` 校验）。一个注册表 = 一个控制台。

**聚合不在语言里**：`sum`/`count`/`min`/`max`/`any`/`all` 是 `extensions/std` 注册的普通函数，`reduce` 留给自定义折叠。日常写法是"推导式映射 + 聚合函数"；`reduce` 的 `if` 子句负责折叠前的筛选，累加器写作 `acc = init`（与 `let` 同形）。

**语言刻意不图灵完备**：没有无界循环，所有形式只遍历有限输入，每个程序都终止 —— `Fuel` 是成本上限而非安全兜底。想加回无界循环前先读 `docs/termination.md` 的附录。

### 必须守住的不变量

- `machine/registry.go` 的 Registry 是唯一类型权威；函数身份是**完整签名** `name(参数)->结果`，同名不同签名即重载。名字的形状与保留字只有一个权威（`IsValidFunctionName`/`IsValidVariableName`/`IsReservedName`），parser、导入器与 registry 都调它。
- Artifact 冻结签名与 fuel 成本；`Instantiate` 校验版本、digest、签名存在性、cost 未变与每条指令。装载时**不再重新解析 ExprJSON** —— 执行只依赖字节码，digest 已经保护了 ExprJSON。
- digest = 清空 `Digest`、`Args[i].Doc`、`ResultDoc` 后 JSON 序列化再 sha256 ⇒ 改动既有字段、字段顺序或 JSON tag 都会让旧 Artifact 失效。ExprJSON 的字段顺序就是节点 struct 的字段顺序，所以**给节点 struct 重排字段也算改 digest**。`ArtifactVersion`/`ExprJSONVersion`/`CatalogVersion` 只标识**当前**形状，不是历史计数：还没有对外承诺兼容，形状要改就直接改，不必为迁移留台阶；等到有已部署的 artifact 时再让它们递增。
- **opcode 的一切在一张表里**（`machine/opcode.go`）：名字、栈效应、校验规则，按 opcode 索引所以顺序不可能错位。加 opcode = 表里加一行 + `frame.step` 加一个 case，`TestEveryOpcodeIsExecutableAndNamed` 会抓住漏掉的那一半。
- **常量折叠**（`compile/fold.go`）：不读参数也不读循环变量、且**每个调用都是 constexpr** 的子表达式在编译期用真 VM 跑掉（`EvaluateClosed`）。`Doc.Constexpr` 是这个授权（内核函数天然有，宿主函数不写就没有）—— 没有它，折叠会在编译规则时把推理引擎、时钟或远程服务调进去，而"3 点钟编译出来不一样"的规则比多算一点更糟。折叠是传递的（`let(a = 250, a * 4)` 整个折掉），折成常量的绑定**不占局部槽**，容器与记录也进常量池（`ConstantFromValue` 递归）。**闭合表达式的失败是编译错误**：只用内核函数且不读参数的东西每次算都一样，所以 `1 / 0`、`[1,2][5]`、`9223372036854775807 + 1` 编译期就报，即使写在惰性分支里（和 Go 对常量除零的处理一致）。两个例外：扩展函数的失败可能不在程序里（留作运行时的事），折叠预算 `ErrFuel` 耗尽只说明这一趟没算完。
- `Artifact.MaxStack` 由编译期栈效应累加得出，帧据此一次预留，`push` 在预留内跳过上限检查；算错只影响优化不影响正确性（`push` 的回退路径仍检查）。`release` 只清用过的部分，靠 `reserved` 与溢出水位。
- 类型推导是多候选分叉 + `implicitTypeScore` 打分选最优，同分报歧义；混合数值签名额外吃 `mixedPenalty`，所以 `risk < 0.5` 会把 `risk` 推成 float。
- 枚举是 nominal 且只从契约进入程序：`EnumExpr`（`@member` / `@enum.member`）的所属枚举由 `compile/enum.go` 的 `collectEnums`/`resolveEnumReference` 在**契约的枚举命名空间**里解析，不靠上下文类型；命名空间收的是契约类型里**任意深度**的枚举（元素、record 字段、字段的字段），走 `machine.WalkTypes`——「这个类型里有没有 X」只有这一个入口，手写的 `Elem` 递归会漏掉 record 字段，这正是它被建立的原因；`enum` 与 `string` 不 unify，编译成字符串常量，运行时值仍是成员名。
- **record 的字段顺序就是它的类型**：`RecordKind` 的 `Type.Fields` 有序，值（`recordValue`）按同一顺序紧凑存放，`FieldExpr` 在编译期解析成下标发 `OpField`，运行时不查名字。推导里 record 作为**整体** unify（`typeTerm.record`），因为它出现的地方字段都已具体 —— 来自契约或来自字段值有类型的字面量。边界按名字匹配：契约声明的字段**必须**都有（缺了就是另一个类型，没有 null 可以顶替，拼错的名字也落在这一侧），源数据多带的字段忽略（一个宿主对象服务多条规则），字段值按无损方向加宽（`1 → float` 可以，`1.7 → int` 不行），Go 与 JSON 两条路径同一套规则。**Go struct 就是 record**：`reflectType`/`intoGo`/`outOfGo`/`fromGo`/`FromValue` 都走 `machine/structs.go` 的一套映射 —— 导出字段按**声明顺序**（顺序即类型），**只有带 `funroute:"name"` tag 的导出字段在 record 里**，没有从 Go 名推断这回事（推断会让 Go 侧重命名悄悄改掉契约）；漏标不是静默的 —— 读它的表达式编译期就报 `has no field`。一个 tag 都没有的 struct 直接拒绝。字段名规则只有一条（`IsValidFieldName`），类型文本、源码与 ExprJSON 三个入口都用它。record 的 JSON 由 `Value.MarshalJSON` 按字段顺序写（`Any()` 交出的 Go map 不保序，要保序就 `json.Marshal(value)`）。还没有的是字段更新（`{...r, a: 1}`）。
- `SwitchExpr.Value` 可为 nil（条件形态），`SwitchCaseExpr.Match` 是列表（多值分支）。改这里只动 `syntax/ast.go`（tag 决定 JSON、作用域、前端布局）加 `compile/infer_expr.go`、`compile/compiler.go` 的语义，以及 `web/funroute-core.js` 的打印。
- ExprJSON 文档只有 `{version, expr}`。版本号前端不写死：`FunRouteLanguage` 从 `catalog.source.expr_json_version` 读，所以这里没有同步点。
- 编译错误带位置：`syntax.At(pos, …)` 是**唯一**的产生方式（lexer、parser、推导都用它），错误本身携带 byte offset，`lang.LineColumn(err, source)` 由宿主换算成行列 —— 语言层不持有源码文本，ExprJSON 编译的程序根本没有文本。新增错误路径必须走 `At`，否则位置就丢了；`compileError` 用两个 `%w` 包装，所以 `errors.Is` 找类别、`errors.As` 找位置都成立。
- 中缀与关键字糖全部在 parser 层脱糖，**AST 不新增任何节点类型**：`a+b` 就是 `add(a,b)`，`a&&b` 就是 `if(a,b,false)`，`[e for x in xs if c]` 就是 `ForExpr`。多层推导 `[e for x in xs for y in ys]` 也一样：`nestClauses` 把子句从内往外串成嵌套 `ForExpr`，除最内层外都置 `Flatten`，编译时发 `OpLoopSpread` 把内层产出的数组拼接进外层（`arrayBuilder.addAll`）。字典推导只接一个子句。新增糖必须同时更新 `web/funroute-core.js` 的 `sugarFromIf`/`forHead` 反向打印（优先级与模板从目录来，JS 不另存一份 `INFIX`）。运算符表（`syntax/operators.go`）支持三种 fixity：`infix`（符号 `%`，或单词 `in` —— 后者按 token 文本匹配，因为它的 kind 就是 identifier）、`prefix`、`index`（后缀 `xs[i]`，parser 在 primary 结束处读，前端按 fixity 打印回方括号）。
- `and`/`or`/`not`/`ne` 是**派生形式**——展开为 `if`，不进注册表。两个同步点：`machine/catalog.go` 的 `derivedForms`（目录描述）与 `web/funroute-core.js` 的 `sugarFromIf`/`logicalForm`（识别与打印）；卡片模板由 `createForm` 从目录的运算符模板实例化，前端不再存一份。
- 扩展函数按不可信纯函数对待：`recover` 在激活层兜住 panic。**值不拷贝**——容器 backing 直接交出，安全性来自只读约定（见上），任何新增的包外取值入口都必须保持"交出 backing、文档写明只读"。
- 出栈返回的是栈上视窗，不是副本：`EvalFunc` 收到的 `[]Value` 只在调用期间有效。
- 没有递归就没有嵌套激活：每次 `Run` 只建一个帧（来自 `sync.Pool`）。新增任何能重入程序的构造都会推翻 `docs/termination.md` 的定理 A 与 B。
- `Kind`/`OpCode` 是 `uint8` 但 JSON 是名字：`Kind` 加值要同步 `kindNames`，`OpCode` 加值要加表行。`HandleKind` 排在 `VarKind` 之后，既有 kind 的序号不变。

### 改动同步点

- 新增 opcode → `machine/opcode.go` 表 + `machine/frame.go` 的 `step`。仅此两处，测试兜底。给 `Instruction` 加 `omitempty` 字段不改变既有 digest（零值不出现在 JSON）。
- 新增惰性形式 → `machine/registry.go` 的 `knownForms` + 节点的 `Form()` 方法（`compile/forms.go` 靠它和 `Children` 通用校验）+ `machine/catalog.go` 的 `formDescriptors`。
- 新增**纯函数** → `Doc.Constexpr = true`，折叠就能在编译期算掉它（`std` 全包如此，`upper("adyen")`、`sum(range(4))` 都编译成一条载入指令）。模型、时钟、远程调用**不要**标。
- 新增**凭空造容器**的函数 → `Doc.BoundedArgs = true`，编译器的 `requireBoundedArgs` 要求每个实参的规模已被输入界定：字面量、`len(容器)`、或两者的算术组合（`boundedCall` 认这几种）。这正是 `docs/termination.md` 定理 B 需要的条件 —— 要求实参是**常量**比它更强，会把 `range(len(fees))` 这种安全写法一起误伤。只给"结果规模由参数决定"的函数用（`range` 是唯一一个），理由写在 `docs/termination.md` 定理 B 之后。
- 新增函数 → 只注册带 `Display` 的 `FunctionSpec`，目录与拖拽面板自动生效；要 ABI 版本就写进名字（`route.score_v1`）。`Doc` 是**唯一**的函数元数据结构：`FunctionSpec.Doc`、`Logic`/`Model` 的入参、目录 JSON 的 `doc` 字段都是它，没有平行的 Display/Parameter/Result 结构。只写机器算不出来的（标签、说明、成本、参数标签）：签名来自反射，分类缺省取命名空间，顺序按名字，**颜色/图标只在 `web/funroute-display.js`**。按 Go 签名注册用 `Logic`（反射读签名，任意元数与嵌套，首参数可选 `context.Context`），模型函数用 `Model` 同时给单条与批量实现；引擎类型先 `DefineHandle[T]`。一个名字要服务多种元素类型时（语言没有类型类，`int`/`float`/`string` 就是三次注册）走 `extensions/std` 的 `eachType`，并在 `TestNamesCoverEveryElementTypeTheyClaim` 加一行 —— 漏注册一个类型不会让任何东西失败，直到规则在生产里撞上那个类型。
- `Kind` 加值 → `kindNames` 同步；若它有运行时表示，`Value.hasType`/`Type()`/`Any()`、`compile/infer.go` 的 `typeTerm`（含 `name`）、`implicitTypeScore`、`ParseType`、`validateTypePattern` 与前端 `typeName` 都要认识它（`HandleKind` 是现成范例）。
- 新增 ExprJSON 节点 → 在 `syntax/ast.go` 定义带 tag 的 struct（含 `kind()`，需要时 `check()`/`Form()`）并加进 `nodeTypes`；导入、导出、作用域、schema、前端 `cleanNode`/空白模板/卡片全部自动生效。仍要手写的是语义：`compile/infer_expr.go`、`compile/compiler.go` 的 case，`web/funroute-core.js` 的 `expressionSource`/`_splitNode` 打印，以及可选的 `FIELD_TEXT` 文案。**按节点类型分派的第三处是 `compile/enum.go` 的 `validateConstrainedReturn`**（枚举出参要逐条返回路径证明）：漏了它不会不安全（default 走 `validateKnownType`，要求整体类型相等，是保守的），但能表达的程序会变少。`walk_test.go` 的 schema 测试会要求列出新节点。
- 新增公开 API → `lang/lang.go` 加别名或转发，并在 `lang/lang_test.go` 以宿主视角用一次；能不加就不加。
- 新增前端例子 → 只改 `web/funroute-examples.js`（例子是**源码 + 契约**两部分，点按钮走 `/api/parse`，按钮由 `renderExamples` 生成）。别手写 ExprJSON 模板。
- 新增颜色 → 不要写字面量。`web/styles.css` 的 `:root` 是**唯一**的调色板，每个 token 用 `light-dark(浅, 深)` 同时给出两套值，主题切换只改 `color-scheme`（`data-theme` 缺省即跟随系统）。`funroute-designer.css` 只消费这些 token，自己不定义颜色；彩底上的文字用 `--on-accent`，ink 填充按钮上的文字用 `--on-ink`，两者不会随主题翻转成不可读。
- 新增前端文件 → 加进 `web/embed.go` 的 `//go:embed` 列表（`make check-js` 按 `web/*.js` 通配，不用再列一遍）。前端依赖严格单向、`funroute-core.js` 零 import（`node --test` 就能跑它），分层每个文件只做一件事：`funroute-core.js`（**语言**，无 DOM 也无网络：schema 驱动的 `cleanNode`/`blankNode`、打印/格式化/词法、`FunRouteLanguage`，以及类型协议 `typeName`/`equalType`/`aliasOf`/`formatTypeText`/`formatValue`）、`funroute-workspace.js`（**一次会话**：契约模型 `emptyContract`/`contractPayload`/`typeRows`、`FunRouteClient`、`FunRouteWorkspace`；同样无 DOM，所以 `node --test` 能跑完整的编辑—编译—运行）、`funroute-display.js`（缩略类型文本与配色，**所有颜色图标只在这里**；`typeName`/`equalType` 是协议，留在 core）、`funroute-fields.js`（输入控件与“文本→值”解析：表达式行、枚举选择、值编辑、`parseInputValue`）、`funroute-dnd.js`（放置规则，注释里写明全部情形）、`funroute-palette.js`（控制块面板）、`funroute-reference.js`（语法说明，从目录生成）、`funroute-designer.js`（画布 Web Component）、`funroute-contract.js`（契约面板）、`funroute-examples.js`（例子加载）、`app.js`（MVP 外壳）。**契约面板与画布之间没有同步点**：前者是宿主数据，后者是表达式，各自提交。
- 新增 API 字段 → 改 `mvp/server.go` 的 `expressionRequest`（`DisallowUnknownFields` 会拒绝未声明字段）；CSP 是 `script-src 'self'`，前端保持无框架无构建。

## 命令

```bash
make ci                                                # 提交前必须全过：check-fmt vet lint build test
make test | make lint | make vet | make fmt | make run
go test ./lang/internal/compile -run TestIfIsLazyAndFuelIsEnforced -v      # 单个测试
go test ./lang/internal/compile -bench . -benchtime 2000x                 # VM 基准
go test ./lang/internal/compile -bench RunPaths -cpuprofile /tmp/cpu.out  # 热路径 profile
go test ./lang/internal/compile -bench Vector -benchtime 1s                # 向量透传：三个尺寸的 ns/op 必须相同（分配来自 Logic 的 reflect.Call，与 n 无关）
go test ./lang/internal/compile -bench BatchVersus -benchtime 2000x        # 批处理对比：合批摊薄引擎开销
go list -deps ./lang/internal/machine | grep funroute                     # 验证依赖方向
go run ./cmd/funroute inspect -expr 'if(a,b,add(1,1))'
go run ./cmd/funroute run -expr 'reduce(x in items, total = 0, total + x)' -args '{"items":[1,2,3]}'
go run ./cmd/funroute run -expr 'let(bps = 250, amount * bps / 10000)' \
  -types 'amount=int' -args '{"amount":100000}'          # 契约由 -types 给出
```

## 开发规范（`make lint` 强制，`tools/lint` 实现）

- 单个方法不超过 **50 行**（含签名与右大括号）。
- 嵌套不超过 **3 层**（if/for/switch/select/函数字面量各计一层，`else if` 不额外计）。
- 单个文件不超过 **800 行**（`.go` 与 `.js`）。
- 超限时拆函数或拆文件，不要放宽阈值；`tools/lint` 只用标准库，保持零依赖。
