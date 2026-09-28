# 更新记录

版本号按[语义化版本](https://semver.org/lang/zh-CN/)。v0.x 期间，公开 API、语法、ExprJSON、Artifact 与签名清单的形状在两个次版本之间都可能不兼容地改变；每次改变写在这里。

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
