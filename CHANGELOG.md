# 更新记录

版本号按[语义化版本](https://semver.org/lang/zh-CN/)。v0.x 期间，公开 API、语法、ExprJSON、Artifact 与签名清单的形状在两个次版本之间都可能不兼容地改变；每次改变写在这里。

## v0.1.2（未发布）

一次专门的修正与优化：外部评审报告逐条核实后的修复，以及有实测支撑的性能改进。新增[语义宪章](docs/semantics.md)，把"纯""强类型""必然终止""冻结""相等"的定义写在一处，下面不少修正就是让实现符合它。

### 修正

- **非原生元素的数组结果不再全为零**：`Out` 是 `[]int`、`[]int32`、`[]float32`、命名数值切片，或 record 里这类字段时，`Run`、`RunInto`、`Session` 原来静默得到全零；`[]int64` 等原生切片不受影响。
- **自定义折叠不再改掉被提升的字段**：注册了 `Fold` 且步骤不是内核专用指令的函数（如 `array<int> -> float` 按 `add` 折叠），与读 record 参数字段的表达式写在一起时，字段的值会被折叠的临时寄存器覆盖。
- **外来 artifact 不再能挂死或打崩宿主**：装载时要求去掉 `loop_next` 回边后控制流无环，`loop_init` 的项目、键与累加器槽互不相同。原来一条往回跳的 `jump` 能让运行无视 `ctx` 永不返回，槽位重合能在不设 recover 的程序里 panic。
- **句柄永不相等**：payload 是 map 的句柄原来能被 `==` 比较出 `true`。
- **金额只在结果放不下时才报 `ErrArithmetic`**：`Ratio.Div` 不再先求倒数，换汇不再先把汇率按位数缩放成一个比例，`round(prorate(m, part, whole), …)` 不再先构造 `part/whole`。`x / MinInt64`、极小汇率的换汇、`prorate(m, 1, MinInt64)` 现在有答案。
- **混合 int/float 比较遇 NaN 按 IEEE 754**：`NaN < 1` 与 `NaN < 1.0` 一样为 false，原来前者为 true。
- **`FromValue`**：目标是指向 struct 的指针时返回错误而不是 panic；目标是接口时给出 Go 形式（record 是 map），不再交出内部类型。
- **格式化**：`(true).x` 不再被打印成无法解析的 `true.x`；`funroute fmt` 不再多输出一个换行。
- **编译**：`if(true, 1, 1 / 0)` 与 `if(f, 1, 1 / 0)` 一样是编译错误（文档一直这样写）；`let(x = [], 1)` 的错误带位置；重载的候选全部失败时报出真正的原因，不再是 `ambiguous here ()`；ExprJSON 拒绝同时带 `flatten` 与 `yield_key` 的推导式；源码里以保留字为名的调用（`for(x)`）是语法错误，与 ExprJSON 一致。
- **标准库**：`rank` 遇 NaN 按排序的顺序给名次；`allocate(m, [权重])` 执行 10000 份的上限；`windows`、`chunk` 的尺寸在 32 位平台上不再截断；`cumsum` 溢出时报 `in cumsum`。
- **ISO 4217 表**：按 SIX 2026-09-17 的现行表，加入 `XCG`（加勒比盾）与 `VED`，去掉已退出的 `ANG` 与 `BGN`。
- **语言服务**：`signatureHelp` 的 `activeParameter` 不再超出所选签名的参数个数；`@` 后输入了几个字母时仍补全枚举成员，字符串里的 `@` 不再触发。
- 空的原生容器（宿主交来的 nil 切片或 map）的 `Any()` 给空切片或空 map，与 JSON 一致，不再是 nil。
- **相等只有一个定义**（宪章 S1）：`==`、`in`、`switch`、`index_of`、`unique`、`intersect`、`except` 原来对零与混合币种各有各的答案（`unique` 把 `USD 0` 与 `EUR 0` 当成一个，`==` 却报 `ErrCurrency`；`in` 的答案取决于先碰到哪个元素）。现在按类型里的位置检查币种，与顺序无关。
- **`RunInto` 不再在参数正读着的内存上建结果**：参数里有更深一层的原生数组（数组的数组、字典里的数组、record 里的 record）时，原来只比对顶层与一层字段，`out` 的切片与这类参数共用内存时会边读边写。
- **注册表的内省不再交出可写的内部状态**：`Overloads`、`Resolve` 返回快照，`Register` 先拷贝 `Doc`；宿主改返回的切片原来会改掉注册表，改变之后编译的含义。
- **签名清单带上 `Fold`**：按清单编译与按真实注册表编译，声明了 `Fold` 的函数原来得出不同的字节码与求值顺序；目录新增 `short_circuit`。
- **批处理**：共享的引擎调用原来取批内最早的截止时间、第一条请求取消就取消整批；`Run` 因 ctx 提前返回后，执行器还可能读它的参数。见下面的行为变化与宪章 S5。
- **float 字面量按值判断是否精确**：`1152921504606846976.0`（2^60）、`-9223372036854775808.0` 这类恰好是 float64 的整数字面量原来被当作不精确而拒绝。
- **金额只在结果放不下时才报 `ErrArithmetic`**（续）：`implied` 与带舍入的换汇原来在中间积超过 128 位时报错，现在按 192 位计算。
- **ExprJSON 与源码等价**（宪章 S7）：`flatten` 的推导式必须产出下一个 `for`，原来能导入一个源码写不出、格式化后变成另一个程序的形状；ExprJSON 的 fuzz 对导入成功的都做格式化往返。
- **多子句推导式的位置**：第二个及以后的 `for` 里的变量名，语法树的 `text_span`、悬停与作用域原来都指向整个推导式的开头。
- `repeat` 的上限按结果长度：`repeat("", 20000)` 是空串，原来报超出上限。

### 行为变化

- `if(true, 1, 1 / 0)` 这类整体可折叠、未走分支里有闭合失败的程序从能编译变为编译错误。
- `Run(map)` 收到宿主 struct 时按名字匹配：struct 多出的 tag 字段被忽略，与 `Load` 相同；原来报契约错误。
- 混合 int/float 比较遇 NaN 的结果见上。
- `cumsum` 溢出的错误文案从 `integer overflow in sum` 改为 `integer overflow in cumsum`。
- 能用 `-currencies iso` 写 `XCG 1`、`VED 1`，不能再写 `ANG 1`、`BGN 1`。
- **只有不带币种的零是多态的**：`USD 0 == EUR 0` 是 `ErrCurrency`（`0 == EUR 0` 仍是 `true`）；`unique([USD 0, EUR 0])`、`USD 1 in [EUR 1, USD 1]`、两种币种的 `index_of`/`intersect`/`except`、含金额的 record 比较同理。
- `intersect`、`except` 与金额键的 `sort_by`/`sort_by_desc`/`top_k`/`bottom_k` 由内核提供：不注册 std 也能用（金额键要先 `DeclareMoney`），目录里归入内核；`sort_by` 等候选与键长度不同时的文案是 `sort_by has 3 candidates and 2 keys`。
- 批处理共享的引擎调用：截止时间取批内最晚的（有请求没设就不设），只在批内所有请求都不再等待时取消；一条请求的 ctx 结束，它自己立即返回，同批其他请求照常完成。
- `RunBatch` 的 `n` 小于零时什么也不做，原来 panic。
- 签名清单与目录的 JSON 新增 `fold`、`short_circuit` 字段。

### 性能

- 金额键的 `top_k`/`bottom_k` 只在大小为 k 的堆里留候选：4096 个渠道取 10 个从 1.1 ms 到 0.18 ms。它们与 `sort_by`/`sort_by_desc` 的金额键改由内核按最小单位排序，不再先建出 n 个 record。
- `intersect`/`except` 按键查集合：4096 笔金额去掉前一半从 60.7 ms 到 0.14 ms；`in`、`index_of` 对金额与汇率数组直接比较最小单位与比值。
- std 两参数的金额 `min`/`max` 直接比较，不再先建一个数组。
- `Program.Run` 的入参 struct 里有 Go `int` 字段时，直线程序也直接从 struct 读参数（原来只认 `int64`、`float64`、`bool`、`string`，有 `int` 就走通用路径）。
- `unique` 对金额与字段都是标量或金额的 record 按键去重：4096 笔金额从 70.6 ms 到 0.13 ms，4096 个 record 从 728 ms 到 0.41 ms（含宪章 S1 按位置检查币种的约 10 ns 一条）。
- `first([…])` 对字段都有零值的 record 也融合：找到就停，只建出找到的那一个。
- `windows`、`chunk`、`take_while`、`drop_while` 共享原数组的内存：4096 笔金额的 `windows(…, 10)` 从 8194 次分配到 18 次。
- 编译时金额、比例、汇率、币种与枚举字面量不再先在嵌套编译器里折叠一遍：400 个金额分支的 `switch` 从 60 ms 到 3 ms。
- std 的 `pad_left`/`pad_right` 等标量形状直接从寄存器调用；直调表与按 Go 签名的直调表由测试保持一致。
- 帧交还后不再留着向量循环读过的宿主切片。
- 循环项不再建 record：遍历宿主 struct 切片、只读字段的循环把字段直接装进寄存器（500 个渠道的费用之和从 4.6 µs 到 3.5 µs）；按字段筛选后原样收集的渠道（`[c for c in channels if c.ok && c.fee < 50]`）只记下标，结果是宿主切片的视图（约 34 µs、504 次分配到 5.7 µs、4 次）；以循环项为基底的 `with` 从字段寄存器构造。
- 推导式里直接收集的 record（投影与 `with`）按块分配：500 个渠道的 `[c with {fee: 0} …]` 从 57 µs、1004 次分配到 38 µs、16 次，投影从 504 次分配到 16 次。
- `min`/`max`/`abs` 有专用指令，带它们的循环可以进向量：`[min(x, 10) for x in xs]`（500 个）从 5.4 µs 到 1.4 µs。

### 文档

- 新增 [`docs/semantics.md`](docs/semantics.md)（语义宪章）；README、language.md、money.md、go.md、contracts.md 改为引用它：首页的"强类型"改为"结构强类型"，"同一输入同一答案"限定为同一环境，Artifact 冻结的是签名绑定而不是行为，扩展函数的纯度是宿主的承诺。
- termination.md 的定理 B 改正：多项式的次数按语法归纳（循环相加、调用乘以函数的代价次数），原证明说内核函数都是线性的；新增内核与 std 的代价表。
- go.md 原来把 `any`/`all`/`first` 的融合写成"不改变结果的改写"，它们是短路形式（宪章 S4）。

### 工程

- 行为金库只记录 JSON 写不出的那个值，不记录 `encoding/json` 随 Go 版本变化的错误文案，Go 1.26 与 1.27 下结果相同。
- `make perf` 新增金额与 record 的选择场景，以及路由规则常见形状（字段求和、按字段筛选、投影、`with`、封顶、按下标遍历）。

## v0.1.1（2026-09-28）

### 修正

- **float 的答案与机器无关**：Go 在 arm64 上会把 `x * y + z` 融合成 FMA，`stddev` 与 `percentile` 的末位因此与 amd64 不同；两处先乘后加都改为先用 `float64(…)` 舍入乘积，处处同一个答案。arm64 上这两个函数的结果可能在最后一位与 v0.1.0 不同。
- **浮点 `pow` 不再在编译期算好**：指数不是整数时它就是 Go 的 `math.Pow`，amd64 上 `math.Exp` 按 CPU 是否有 FMA 走不同路径，末位随 CPU 而异；与读时区规则的日历函数一样不参与折叠，Artifact 因此与编译它的机器无关。整数版 `pow` 照旧折叠。
- 行为金库在 arm64 上生成、CI 在 amd64 上核对。

### 文档

- README 精简为首页：在线工作台的入口、一个可运行的 Go 示例、文档索引。原来的章节拆到 `docs/`：`language.md`、`money.md`、`contracts.md`、`go.md`、`tooling.md`（含命令行）、`grammar.md`、`development.md`。
- 在线工作台发布在 <https://nethinwei.github.io/funroute/>。

## v0.1.0（2026-09-28）

第一个对外发布的版本。相对 v0.0.2，语言、金额、SDK、执行层与工具都是重做或新增的；v0.0.2 编出的 Artifact 与 ExprJSON 不再能装载，用源码重新编译即可。

### 语言

- 契约归宿主：参数、类型与返回类型由 `CompileOptions` 或 `Bind[In, Out]` 给出，规则文本只是表达式；契约里可以写类型别名。
- 类型：`record`（字段顺序即类型，字段更新写作 `r with {a: 1}`）、nominal 枚举与 `@member`（`switch` 穷尽检查）、不透明句柄 `handle<name>`、`time` 与 `duration`（时长字面量 `2h30m`，按时区的日历函数，`now` 作参数）。
- 形式：`let`、`switch`（值匹配与条件链）、列表与字典推导（可连写多个 `for`）、`reduce`、`using`；`&&`/`||`/`!` 短路；可开关的形式由注册表启用。
- 字段选择器：`sort_by(channels, .fee)`、`min_by`/`max_by`。
- 字符串：`index_of`、`last_index_of`、`trim_prefix`、`trim_suffix`、`repeat`，编译时定下模式的 `matches`（RE2，文本至多 10000 字节）。
- 编译期求值：不读参数、每个调用都获授权（`Doc.Constexpr`）的子表达式在编译期算完，闭合表达式的失败是编译错误。
- float 按 IEEE 754，与 Go 一致：NaN 与 ±Inf 是合法的值，float 运算从不失败。

### 金额

- `money`、`ratio`、`fxrate`、`currency`：一律 int64 的精确分数，从不经过 float，放不下是 `ErrArithmetic`。
- 没有默认舍入：落在两个最小单位之间的运算要写在 `round(…, @mode)` 里（里面精确、只舍入一次）或当场写出舍入方式。
- 换汇只在 `using` 里，`amount -> JPY`；汇率加点、`implied`、`prorate`、`round_to`、按策略的 `allocate`。

### SDK

- 公开包 `github.com/nethinwei/funroute`；值层零拷贝：原生 Go 切片与映射作 backing，边界不转换。
- 类型化绑定：`Bind[In, Out]`、`Program.Run`/`RunInto`/`RunBatch`、`Session`；struct 以 `funroute:"name"` 标签映射为 record。
- 注册只有 `Registry.Register(FunctionSpec)`，`Go`/`GoBatch` 按 Go 签名反射；`Doc` 是唯一的函数元数据，`Fold` 声明可融合的聚合。
- 运行时长由宿主的 `ctx` 决定（删除了 fuel、`Doc.Cost`、`RunOptions`、`ErrFuel`），`Doc.Timeout` 与 `Doc.Detached` 约束单个函数。
- 类型化错误：`ErrCompile`、`ErrContract`、`ErrDeadline`、`ErrExtension`、`ErrCurrency`、`ErrArithmetic`、`ErrDomain`、`ErrNoFxRate`，一律 `errors.Is`；`fallback` 只接数据暂不可得的失败。
- 批处理：字节码证明可以提前算的模型调用按批合并。
- 内核库与标准库 `extensions/std`：聚合、序列、选择与分组、字典、字符串、数值、统计；每个函数带可运行的案例。

### 执行

- 装载时验证字节码，翻译成按种类分组的寄存器形式执行；专用内核指令与窥孔融合，值流分析让中间数组 0 分配，循环按列向量化。
- 与 expr v1.17.8 同机对照：执行的 100 行里 FunRoute 耗时更短的 92 行、持平 3 行；编译多做类型推导、常量折叠、字节码验证与 digest，耗时是 expr 的 1.0–2.0×（见 `docs/perf.md`）。
- 编译期折叠与格式化随程序规模线性增长。

### 工具

- 语言服务 `lsp`（stdio 与 WebAssembly）：诊断、补全、悬停、签名提示、语义标记、格式化、语法树。
- 策略工作台：编辑器加结构视图，结构视图的每处修改都是对原文区间的替换。
- 签名清单：没有实现的宿主函数只登记签名，语言服务照常检查。
- 文档：`docs/limits.md`（能力边界，实测）、`docs/termination.md`（终止性与成本上界）、`docs/perf.md`、`docs/roadmap.md`。
- 许可证：MIT。

## v0.0.2（2026-09-21）

布尔派生形式与列表推导式。

## v0.0.1

最初的原型。
