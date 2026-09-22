# 改进计划与决策记录

本文记录 FunRoute 从"路由表达式"走向"受治理的金融决策语言"的阶段计划，以及每个阶段已经做出的取舍。原则只有一条，所有取舍都从它推出：**性能让位于适用性，正确性不让位于任何东西。**

定位：相对于通用表达式求值器（如 `expr`），FunRoute 的价值不在求值本身，而在它周围的治理层 —— 规则是带 digest 与冻结 ABI 的 Artifact、能力边界由注册表定义且默认关闭、契约归宿主并参与推导、成本有界且必终止、文本与画布无损互转、模型接入是一等公民。每一项改进都应在这几条之内。

## 已完成

- 容器零拷贝值层：原生 Go 切片/映射作 backing，`Value` 56 字节，边界零转换。
- 不透明句柄 `handle<name>`：引擎数据穿过表达式，只能传递，不能比较。
- 批处理第一级：字节码证明可提升的模型调用按批合并，惰性 `if` 不受影响。
- 语法节点单一权威：struct tag 驱动导入、导出、作用域、schema 与前端卡片。
- 契约归宿主；变量名禁点（`.` 留给字段访问）。
- 目录只承载含义：`Doc` 是宿主写的唯一结构（注册入参、存储、目录输出同一个），签名来自反射、分类缺省取命名空间、颜色与图标只在前端；`Keywords`/`Examples`/`Order`/`FunctionDisplay`/`ParameterDisplay` 等平行结构删除。
- 聚合与遍历分工：`reduce` 保留为自定义折叠的逃生口（对照 expr —— 它有 70 个内建，仍然保留 `reduce`），日常聚合由可选包 `extensions/std` 提供（`sum`/`count`/`min`/`max`/`any`/`all`/`range`），内核仍是 15 个函数名。`reduce` 有推导式同款的 `if` 子句，累加器初值写作 `acc = init`（与 `let` 同形）。`range` 的参数必须编译期已知（`FunctionSpec.ConstantArgs`），否则一个标量就能代表任意长的数组，定理 B 失效。
- 前端与 SDK 的边界清理：契约面板的样式随库走（此前 MVP 外壳的 `.fr-muted` 在全局覆盖画布自己的字号）、控制块面板去掉空转的搜索框（面板里最多六个块）、`funroute-core.js` 只导出外部真正 import 的符号；`lang` 包的每个公开类型都在 `lang_test.go` 被宿主视角显式命名一次，少一个别名就编译不过。
- 策略工作台的编辑模型：控制块是卡片、其余是一行就地编辑的源码；控制块集合、类型建议、值节点文案都由目录推导，前端不再有硬编码清单。

## 阶段 0：地基收口（已完成，2026-09-22）

本阶段已全部落地并由 Go/JS 测试覆盖。`fallback` 使用专用字节码错误边界并支持同类型变参候选，按顺序惰性求值，批处理不会越过边界预取；内核运算错误与宿主扩展错误已分开。枚举保留为字符串运行表示，闭集由契约携带，因此不增加第二套值表示。

| 状态 | 项 | 决策 | 取舍 |
|---|---|---|---|
| ✅ | 反射注册 `Logic` / `Model`，删除 `Fn1/2/3`、`Model1/2` | 两个入口，名字同长；`FunctionSpec` 仍是手写逃生口 | 扩展调用每次约 +200 ns、几次分配；内核函数不走反射，纯表达式基准不变。换来任意元数、任意嵌套、Go 常见标量类型 |
| ✅ | `ctx` 贯通：`Run(ctx, …)`、`Batch.Run(ctx, …)`，扩展函数可选首参数 `context.Context`，`Doc.Timeout` 为函数级上限 | 实际 deadline = min(请求剩余预算, 函数上限)；VM 只在扩展调用前检查 | API 破坏性改动，趁未发布做 |
| ✅ | 超时执行方式 | **默认信任 ctx；注册时可标 `Detached`**，VM 放独立 goroutine 等，到点放弃 | `Detached` 每次约 1 µs，被放弃的调用继续占资源直到自行结束；只给无取消能力的引擎绑定用 |
| ✅ | `fallback(primary, secondary, ..., final)` 变参惰性形式 | 同类型候选按顺序尝试；只接扩展错误与超时，**不接** fuel 耗尽、类型错误与内核函数错误 | 规则不能吞掉自身 bug |
| ✅ | 时间预算耗尽且无 `fallback` | 直接返回 `ErrDeadline`，宿主兜底 | 不加"规则级默认值"，避免同一件事两层机制 |
| ✅ | 类型化错误 | `ErrCompile`、`ErrContract`、`ErrFuel`、`ErrDeadline`、`ErrExtension`，`errors.Is` 可分；MVP 映射稳定错误码 | — |
| ✅ | 枚举类型 + 返回类型穷尽检查 | 契约声明 `enum<channel>{adyen,stripe}`，表达式里成员写作 `@adyen`（歧义时 `@channel.adyen`）；枚举是 nominal 类型，不与 `string` 互换；枚举入/出参做边界校验；无 `else` 的 `switch` 必须完整覆盖，契约变化会触发重新编译失败 | 一个 kind；运行值仍是 string |

## 与通用求值器的差距核对（2026-09-22，对照 expr v1.17）

expr 的内建是 70 个函数 + 20 个运算符，本语言内核是 15 个函数名 + 7 档运算符。逐项核对后差距分两类：

- **该补的**（已排进阶段 1）：索引与切片、`%`、`len`、`in`、数值与字符串库、数组与字典库、record 字段访问、错误行列。其中**字段访问与索引是唯一的真实接入劣势** —— 没有它们，宿主要把一个订单对象拍平成几十个扁平入参，而且宿主每次改结构都要改契约。其余多是标准库空缺，属于注册什么的问题，不是语言能不能的问题。
- **不补的**（见「明确不做」）：lambda 谓词、`nil` 与 `??` / `?.`、`$env` 与未定义变量、正则 `matches`、JSON/base64/bytes/位运算、pipe 与方法链。它们要么与冻结 ABI 冲突，要么与成本可静态估计冲突（正则的 ReDoS 直接击穿定理 B），要么只是写法偏好 —— 画布已经解决同一个问题。

反过来，digest 冻结 ABI、必终止与多项式延迟上界、fuel 成本上限、nominal 枚举与穷尽检查、契约参与 unify（而非事后比对）、文本与画布无损互转、零拷贝容器、模型批处理、能力由注册表开关，通用求值器都没有。常量折叠也更强：expr 要手工标 `ConstExpr`，这里是「不读参数就折」由编译器自动判定。


### 执行性能基线（2026-09-22，darwin/arm64，Go 1.26，对照 expr v1.17.8）

同机同表达式，各跑 1 秒：

| 场景 | FunRoute | expr | |
|---|---|---|---|
| `amount * bps / 10000 + fixed` | 168 ns · **0 alloc** | 48 ns · 3 alloc | expr 快 3.5× |
| 三路条件分支 | 187 ns · **0 alloc** | 31 ns · 0 alloc | expr 快 6× |
| 64 元素 filter + sum | 5810 ns · 7 alloc / 680 B | 3102 ns · 174 alloc / 2296 B | expr 快 1.9× |
| 64 元素折叠 | 7218 ns · **0 alloc** | 3380 ns · 174 alloc / 2296 B | expr 快 2.1× |

**理论上该我们快**：值不装箱（`Value` 是结构体，标量直接放在里面；expr 的栈是 `[]any`，中间结果要装箱）、类型编译期定死（我们直接选中 `evalIntAdd`，expr 的 `Add(any, any)` 是 345 行的 12×12 双层 type switch）、参数是数组不是 map。三条优势都真实存在——实测的 alloc 列就是证据。

**实际慢在调用协议**。算术场景 profile（3.7 s 采样）：`push` 12.5%（56 字节 `Value` 拷贝）、`step` 11.9%、`call` 11%、`RunValues` 9.1%（帧准备）、`hasType` 6% 加 `validateInvariant` 3.4%（运行时重做编译期已证明的类型检查）、`result`/`reset`/`release` 合计 10%，而真正在算的 `evalIntMul` + `evalIntDiv` **只有 3%**。一句话：expr 把 `a * b` 编译成一条 VM 内联指令，我们编译成一次通用函数调用。零拷贝值层省下的钱，在调用协议上花掉了。

**但 0 分配是真的**：单次延迟我们慢 2–3 倍，expr 每次调用制造 174 次分配的垃圾，高 QPS 下会转成 GC 压力与尾延迟。另外 fuel 计费、错误分类、digest 校验、类型安全这些治理功能 expr 一样没有——profile 说明它们很便宜，95% 的开销是实现选择，不是治理税。

对比用的 benchmark 依赖 expr，不进这个零依赖仓库：复现方式是另建一个 module，`replace funroute => ../funroute` 并 require expr，对同一组表达式各写一份。

## 阶段 1：结构与语言补齐（未开始）

顺序按**返工成本**排，不按影响面排：`record` 贯穿推导、ExprJSON、digest 与前端，越晚做返工越大；`decimal` 只影响示例里的 float 字面量，是小时级的改写。所以原先「先 `decimal`」的顺序改为下表 —— `decimal` 退到 `record` 之后、`money`（阶段 2B）之前，前面先补三项低风险的地基。

| 顺序 | 项 | 决策 | 取舍 |
|---|---|---|---|
| 1 | 索引 `xs[i]`、`d[k]`，`%`，`in`，`len` | 内核函数 + 糖，不加节点，不动 digest | 现在 dict 取一个键都做不到、只能整表遍历，补上后它才是可用类型 |
| 2 | record 类型 | struct 映射、字段访问 `r.field`、契约按字段声明 | 贯穿推导、ExprJSON、常量、前端的最大改动；路由、对账、模型多输出都依赖它。一次性 bump ExprJSON 与 Artifact 版本 |
| 3 | 标准库扩展包 `extensions/std`（**聚合部分已完成**，字符串与数值库待补） | 不进内核，由注册表开关；`any` / `all` / `sum` 接推导式现推的数组，`sort_by` / `top_k` 将接平行 key 数组 | 不需要 lambda，终止性不变。已落地：`sum` `count` `min` `max` `any` `all` `range`。待补：`upper`/`lower`/`trim`/`split`/`join`/`contains`、`abs`/`round`/`ceil`/`floor`，以及 `sort_by`/`top_k`（排在阶段 2A 的路由包里） |
| 4 | 错误带行列与节点路径 | 编译错误附行列与 ExprJSON 路径，画布高亮 | 现在报的是 `at byte 7`，运营读不懂 |
| 5 | `decimal` 类型（定点 int64 系数 + 指数，中间运算 128 位） | **不带后缀的小数字面量默认 decimal**，`1.7f` 显式 float | 用户决策。写钱的人不会误用 float。代价：模型分数比较 `risk > 0.5` 需要字面量按上下文定型（见下），现有示例中的 float 字面量要复核 |
| 5（同批） | 数值字面量定型 | 小数字面量是**未定型常量**：默认 decimal，与 float 上下文 unify 时成为 float（同 Go 的 untyped constant）；`1.7f` 强制 float、`1.7d` 强制 decimal | 这是让「默认 decimal」与模型分数共存的唯一不引入大量重载的方式；`implicitTypeScore` 与 `mixedPenalty` 相应调整 |
| 6 | 多层与字典推导、`else =>`、尾随逗号统一 | — | 完整性 |

开工前要定的两件事：

- **缺键怎么办**。没有 `nil` 的语言只有三条路：编译期要求 key 是常量（只有 record 做得到）、运行时报可判别错误、或只给 `get(d, k, default)`。倾向后两者并存 —— 与 `div` 除零同一套处理，不引入 Option，也不引入 null。
- **标准库开多大**。以「规则里写得出真实用例」为筛子，不以「通用求值器有」为筛子：expr 的 70 个内建里对支付路由真正会用的约 25 个。宁可少给，后补比后删便宜。

## 阶段 2A：路由包（先做）

`decision` 返回 record + 原因码、`reject(code)`、确定性 `split(key, weights)`、渠道快照筛选排序（`sort_by`、`top_k`）、级联重试输入标准与拒付码分类、时间窗标准库（`now` 与时区作参数）。规则集组合放宿主 `RuleSet`，不进语言。

## 阶段 2B：金融包

| 项 | 决策 |
|---|---|
| `money` | **币种进类型**：`money<USD>`，不同币种相加编译期报错；多币种用币种类型变量 `money<C>` 与显式 `fx(rate, m)` 换币 |
| 字面量 | `USD 1.70`（小数位由币种决定，`USD 1.234` 报错）、`2.9%`、`25bps`；不支持 `$` |
| 运算 | `money ± money` 同币种；`money × decimal/int → money` 默认银行家舍入并在目录写明，`round(m, mode)` 显式；`money × money` 报错；`allocate(m, weights)` 最大余数法不丢分 |
| 相等 | money、decimal 精确相等；float 相等给警告（需要警告通道） |
| `date` / `timestamp` | 时区与 `now` 作参数，保持纯函数 |

## 阶段 3：治理工具

规则自带用例（保存即跑）、`funroute check` 批量兼容性检查、Artifact 版本 diff、`ContractCompatible`、决策 trace、`Simulate` 回测、影子运行 `Compare`、`Strict` 模式。全部在 SDK 与 CLI，不碰语言。

## 阶段 4：模型接入深化

批处理第二级（按调用依赖分层合批），前提是先用真实引擎测出第一级的收益边界。

## 阶段 5：执行层提速（阶段 1 之后）

**不是现在做**：阶段 1 的 `record`、`decimal` 与索引都会改变指令集形状，此刻做指令特化，到时要重来一遍。等语言形状定型后作为一个独立阶段做，基线与归因见上面的性能小节。

| 项 | 决策 | 取舍 |
|---|---|---|
| 内核算术与比较特化成 opcode | `add`/`sub`/`mul`/`div`/`lt`/`le`/`gt`/`ge`/`eq` 的参数类型编译期已定，直接发 `OpAddInt` / `OpMulFloat` 这类指令，VM 在栈上算完，不走 `call`、不构造参数视窗、不重做类型校验 | opcode 表变长，`ArtifactVersion` 递增；换来算术从 168 ns 压到 50 ns 内且仍是 0 alloc —— 那时会明显快过 expr。**比 expr 更有条件做**：它的 opcode 还要在运行时 type switch，我们不用 |
| 聚合管道融合 | 编译期把「聚合函数套一个 `ForExpr`」重写成 `ReduceExpr`：`sum([x for x in xs if p])` 单遍折叠，不构造中间数组 | 纯前端 AST 重写，不动 VM，语义等价（`for` 与 `reduce` 共用循环指令）。对照：expr 的 optimizer 有一批同类重写（`filter_first`、`filter_len`、`sum_array`、`in_array`） |
| 免反射的宿主调用 | 为常见 Go 签名自动生成类型化包装，绕开 `reflect.Call` 的约 300 ns | `FunctionSpec` 现在是让人手写的逃生口，这条把它自动化；对照 expr 的 `OpCallTyped` |

## 明确不做

张量类型与 float32 类型（句柄替代）、lambda、递归、语言内状态与循环、`$` 币种符号、`let … in` 语法、`[][]float64` 之上的专用嵌套类型（反射转换替代）。变参函数优先级最低，暂缓。

对照通用求值器新增的不做项：`nil` 与 `??` / `?.`（引入 null 就引入静默传播）、`$env` 与未定义变量（运行时才发现缺参数等于放弃 digest 的意义）、动态 `type()`、正则 `matches`（成本不可静态估计；确需时做成注册时预编译、输入限长的扩展函数，成本明码标价）、`toJSON` / `fromJSON` / base64 / bytes / 位运算（序列化与字节处理归宿主）、pipe `|` 与方法调用链。
