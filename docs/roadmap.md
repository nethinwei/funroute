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
- 聚合与遍历分工：`reduce` 保留为自定义折叠的逃生口（对照 expr —— 它有 70 个内建，仍然保留 `reduce`），日常聚合由可选包 `extensions/std` 提供（`sum`/`min`/`max`/`any`/`all`/`range` 等），不进内核。`reduce` 有推导式同款的 `if` 子句，累加器初值写作 `acc = init`（与 `let` 同形）。`range` 的参数规模必须由输入界定（`Doc.BoundedArgs`：字面量、`len(容器)` 或两者的算术组合），否则一个标量就能代表任意长的数组，定理 B 失效。
- 编译期算清（`constexpr`）：不读参数且每个调用都获授权的表达式在编译期跑完，容器与记录也进常量池（`[1,2,3]`、`upper("adyen")`、`sum(range(4))` 各编译成一条载入指令）；闭合表达式的失败是**编译错误**，包括写在惰性分支里的（`1 / 0`、`[1,2][5]`、溢出），与 Go 对常量除零的处理一致。授权由 `Doc.Constexpr` 给出：内核与 `extensions/std` 全有，模型/时钟/远程调用没有 —— 否则参数恰好是常量时，推理引擎会在编译规则时被调进去。
- 前端与 SDK 的边界清理：契约面板的样式随库走（此前 MVP 外壳的 `.fr-muted` 在全局覆盖画布自己的字号）、控制块面板去掉空转的搜索框（面板里最多六个块）、`funroute-core.js` 只导出外部真正 import 的符号；`lang` 包的每个公开类型都在 `lang_test.go` 被宿主视角显式命名一次，少一个别名就编译不过。
- 策略工作台的编辑模型：控制块是卡片、其余是一行就地编辑的源码；控制块集合、类型建议、值节点文案都由目录推导，前端不再有硬编码清单。

## 语言服务与工作台改造（已完成，2026-09-23）

起因：前端 `funroute-core.js` 等在 JS 里重新实现了一遍语言（打印、正则高亮、作用域、节点规范化、值解析），Go 为此在目录里导出运算符模板、节点 schema、关键字表。两份实现已经漂移（契约注释不一致），并带着 bug（`(a + b).x` 打印成 `a + b.x`、`-x` 打印成 `0 - x`，int64 字面量经 `JSON.parse` 静默失真）。一次"把 JS 平移到 Go"的尝试因为越界（UI 形状的输出、画布操作进了 Go）被整体回滚。

| 决策 | 取舍 |
|---|---|
| **Go 只做语言，能力从语言本身长出来**：`Lexemes` 保留词法器与解析器已有的判断，节点带 `Span`，诊断带区间，格式化器在 AST 上打印（反向读法与 `expandOperator` 一一对应），`Analyze` 交出推导已有的类型与签名，`SyntaxTree` 由 walk plan 派生 | 不给颜色、布局、控件；写到一半的程序目前只有词法层面的事实，解析错误恢复是下一步 |
| **对外接口是 LSP**（`lang/lsp`），同一份代码 stdio（`funroute lsp`）与 WASM（`web/wasm`，Worker 里运行）两种传输 | 标准协议替我们划清了语言与 UI 的边界；契约用 `funroute/setContract` 推送，结构视图用 `funroute/syntaxTree`，运行与带契约注释的导出用命令 |
| **签名清单**：`Registry.Manifest()` / `Manifest.Apply`，宿主函数在没有实现的地方只登记签名，调用得 `ErrUnavailable`（同时是 `ErrExtension`），`TrackUnavailable` 记下试运行调用了哪些 | 部署用 artifact 由宿主用真实注册表编译：宿主纯函数若标了 `Constexpr`，两边折叠不同，digest 会不同 |
| **工作台是文本的投影**：CodeMirror + LSP 客户端；结构视图按语法树画卡片，每处修改都是对原文区间的替换；前端不理解 ExprJSON | 旧画布的"拖动已有节点""增删列表项"没有移植：它们需要前端知道语法，或在 Go 里做画布操作，两者都违反边界。需要时用 LSP 的 code action 在 Go 里实现 |
| **前端引入依赖但只在前端**：TypeScript + esbuild，运行时只有 Lit 与 CodeMirror；产物构建而不提交，Go 仍零依赖 | MVP 只剩静态文件服务，同一套文件发布到 GitHub Pages；WASM 约 1.6 MB（gzip） |
| 退场：目录的 `source`（运算符模板、关键字、名字正则）与 `nodes`（节点 schema）、`default` tag、`value_types`、画布分组排序、`lang.Catalog`、MVP HTTP API、`web/embed.go`；形式描述不再伪装成带假签名的函数 | 目录只剩函数与形式；形式的写法必须能解析（测试守着） |

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

expr 的内建是 70 个函数 + 20 个运算符，本语言内核当时是 15 个函数名 + 7 档运算符（阶段 1 后为 19 个函数名）。逐项核对后差距分两类：

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

## 阶段 1：结构与语言补齐（进行中）

顺序按**返工成本**排，不按影响面排：`record` 贯穿推导、ExprJSON、digest 与前端，越晚做返工越大；`decimal` 只影响示例里的 float 字面量，是小时级的改写。所以原先「先 `decimal`」的顺序改为下表 —— `decimal` 退到 `record` 之后、`money`（阶段 2B）之前，前面先补三项低风险的地基。

| 顺序 | 项 | 决策 | 取舍 |
|---|---|---|---|
| 1 ✅ | 索引 `xs[i]`、`d[k]`，`%`，`in`，`len` | 内核函数 + 糖，不加节点，不动 digest；越界与缺键是错误，不引入 null | 已完成。内核从 15 个函数名增到 19 个（`at`/`member`/`len`/`mod`），`len` 顺带取代了 `std` 里同义的 `count` |
| 2 ✅ | record 类型 | 契约按字段声明（`record{amount: int, currency: string}`）、字段访问 `r.field`、记录字面量 `{name: 值}`；字段顺序即类型，访问编译成下标 | 已完成：两个节点（`record`/`field`）、两个 opcode（`make_record`/`field`）、`RecordKind` 贯通类型/值/推导/边界/前端。Go struct 双向映射也已完成（只有带 `funroute` tag 的导出字段在记录里，不做任何名字推断；声明顺序即类型），JSON 输出保字段序，字段名规则三个入口统一。字段更新 `{...order, amount: 1}` 也已完成：结果与原记录同类型，编译成一条 `record_with` |
| 3 ✅ | 标准库扩展包 `extensions/std` | 不进内核，由注册表开关；全包标 `Doc.Constexpr`，所以闭合调用在编译期算完 | 不需要 lambda，终止性不变。已落地：聚合 `sum`/`min`/`max`/`any`/`all`/`range`（`min`/`max` 也接字符串），字符串 `upper`/`lower`/`trim`/`contains`/`starts_with`/`ends_with`/`slice`/`split`/`join`/`replace`，数组 `first`/`last`/`take`/`slice`/`reverse`/`concat`/`unique`/`flatten`/`sort`，数值 `abs`/`ceil`/`floor`/`round`/`mod`(float)/`pow`、二元 `min`/`max`，统计 `avg`/`median`/`stddev`/`percentile`，选择 `indices`/`index_of`/`arg_min`/`arg_max`/`sort_by`，分组与序列 `group_by`/`rank`/`cumsum`，补位 `pad_left`/`pad_right`。计数由内核的 `len` 承担。`round` 是数值取整（半数远离零），与 money 的舍入是两件事 —— 后者要回答的是「怎么把一笔钱分到最小单位而不丢分」。便捷函数按"能组合出来 ≠ 好写"补齐：`top_k`/`bottom_k`/`sort_desc`/`sort_by_desc`/`windows`/`chunk`/`deltas`/`intersect`/`except`/`take_while`/`drop_while`。字典的 `get(d, k, default)` 与 `merge(a, b)` 也在这里 —— 缺键与叠层是推导式表达不了的两件事，而 `keys`/`values` 是 `[k for k, v in d]`，所以不收。**`zip` 与 `join` 不做**：前者产出的配对得有字段名，固定成 left/right 不如让人自己写 `{channel: channels[i], fee: fees[i]}`；后者的左连接要"匹配不上就是空"，而语言里没有空。两者写进语法说明的 recipe。没有 `**` 运算符，`pow` 一种写法就够。日期时间排在 money 之后 |
| 4 ✅（行列部分） | 错误带行列 | `syntax.At` 是唯一产生方式，错误携带 offset，`lang.LineColumn(err, source)` 由宿主换算；MVP 的 API 回 `line`/`column`，CLI 与前端按 `行:列` 显示 | 已完成。**ExprJSON 节点路径没做**：画布逐槽提交，出错时它已经知道是哪个槽；路径要等"整棵树提交后要高亮某个节点"这个消费者真出现再说 |
| 5 | ~~`decimal` 类型~~ → 直接做 `money`（见阶段 2B） | **不做通用 decimal**：钱本来就是「某币种的最小单位整数」，`money` 底层是 int64，乘费率用 `math/bits` 做 128 位中间运算。零依赖、零分配、精确 | 用户决策（2026-09-23）。通用 decimal 只有和 money 一起才有价值。费率与汇率由定点的 `rate`/`fxrate` 承担（见阶段 2B） |
| 5（同批） | 数值字面量定型 | 先取消（它原本为「decimal 当通用数值类型」而存在），后在阶段 2B 以更小的规模恢复：小数字面量只在 float 与 `rate` 之间按上下文定型 | 不再是通用数值塔，推导里只多一种受约束的字面量项 |
| 6 ✅ | 字典推导 `{k: v for k, v in d}`、`else =>`、尾随逗号 | `ForExpr` 加一个可选的 `yield_key`：有它就产出 dict，没有就产出 array；不加节点 | 已完成。它同时消掉一个陷阱：`{country: rate}` 一直被当成字段名为 country 的记录，而想要动态键的人本来无路可走。`else` 后的 `=>` 现在可写可不写（两种写法同一棵树）|
| 7 ✅ | 契约的类型声明 | 文本契约里 `types` / CLI `-alias` 给 record 起个名字，参数与返回类型按名字引用；`lang.ParseTypeWith` 在解析类型文本时就地展开 | 已完成。别名**只是拼写**：编译器、artifact、digest 都看不见它，写全字段与用别名编译出同一个 artifact（`TestDeclaredTypesAreSpellingOnly` 比对 digest）。因此别名**不嵌套**（没有解析顺序、没有环）、未声明的名字仍是错误、Go 宿主不需要它（复用 `lang.Type` 变量即可）。契约面板加一行「类型」即可声明，声明的名字进类型输入框的候选 |
| 8 ✅ | 多层推导（笛卡尔积）与 `take_while`/`drop_while` | `for` 子句可连写，parser 脱糖成嵌套 `for`，除最内层外都带 `flatten`；VM 新增 `loop_spread` 拼接内层产出 | 已完成，推翻了原先"不做，用 `flatten([[...]])` 代替"的判断 —— 那个写法是脑筋急转弯，而这是 Python/Haskell 写笛卡尔积的方式。代价很小：**不加节点类型**（一个 bool 标志，`walk.go` 的反射 plan 多认一种字段类型）、既有 digest 不动（零值不出现在 JSON）、终止性不变（有限 × 有限）。字典推导仍只接一个子句 —— 拼接字典要回答"键重复算谁的"。`take_while`/`drop_while` 用同样的平行数组形态：判断是第二个数组，不需要 lambda。**`combinations`/`permutations` 不做**：结果规模是阶乘级，`BoundedArgs` 那套论证在它这里不成立 |
| 9 ✅ | 一次完备性与重复逻辑审查 | 见下 | `Type` 的结构递归收敛到 `machine.WalkTypes`/`TypeContains` 一个入口 —— 手写的 `Elem` 递归漏了 record 字段，**契约里 record 字段上的枚举因此完全不可用**（`o.ch == @adyen` 报"不属于任何枚举"），这是 record 与枚举两个特性的交叉缺口。运行期两处"重复做已经做过的事"：`RunValues` 每次调用深度重扫参数找 NaN（65536 元素 16µs，占 85% CPU；不变量本就由 `CheckedFloat`/`checkFloats`/`Array`/`Dict`/`Record` 在值诞生处确立），`hasType` 对 record `CloneType` 出一个 `Type` 来比（每次 192 B）——前者 62 倍，后者归零，`TestArgumentChecksDoNotAllocate` 守着。补齐：字符串成为完整容器（`s[i]`、`"b" in text`，按码点）、取整返回 int、`get`/`merge`、`+` 用在容器上时的错误指路 |

开工前要定的两件事：

- **缺键怎么办**。没有 `nil` 的语言只有三条路：编译期要求 key 是常量（只有 record 做得到）、运行时报可判别错误、或只给 `get(d, k, default)`。倾向后两者并存 —— 与 `div` 除零同一套处理，不引入 Option，也不引入 null。
- **标准库开多大**。以「规则里写得出真实用例」为筛子，不以「通用求值器有」为筛子：expr 的 70 个内建里对支付路由真正会用的约 25 个。宁可少给，后补比后删便宜。

## 阶段 2A：路由包（在 2B 之后）

（record 已经就位，所以 `sort_by(channels, .cost)` 这类写法不必再走平行 key 数组的权宜方案。）`decision` 返回 record + 原因码、`reject(code)`、确定性 `split(key, weights)`、渠道快照筛选排序（`sort_by`、`top_k`）、级联重试输入标准与拒付码分类、时间窗标准库（`now` 与时区作参数）。规则集组合放宿主 `RuleSet`，不进语言。

## 阶段 2B：金额（已完成，2026-09-23）

落地情况：全部实现并由测试覆盖。下面几张表按时间记录决策，后面的会推翻前面的，以最后两节（「换汇、币种与公开接口」「审查后的修正」）为准；被推翻的行标了【已推翻】。`make ci` 全绿，格式化往返与 ExprJSON 导入各跑 60 秒模糊测试无失败。零分配守住：金额标量与 `[]Money` 参数的 `RunValues`、带 `lang.Money` 字段的 `Program.Run` 都是 0 分配（`TestMoneyRunsDoNotAllocate`、`TestProgramMoneyDoesNotAllocate`）。基准（darwin/arm64，Apple M5）：未用金额的程序不受影响，`BenchmarkCall` 180–200 ns、`BenchmarkDispatch` 约 5.7 µs，与改动前一致；`BenchmarkMoneyFee`（money<c> 的费率加固定费再封顶）约 575 ns。与决策的偏差只有一处：调用处的币种检查只发给非内核函数，内核金额运算在求值时自己比对币种（决策里的"纵深防御"因此覆盖了全部内核运算）。


顺序调到 2A 之前，理由与阶段 1 相同 —— 按返工成本排：`money` 加 kind、加字面量、加推导里的币种项，贯穿推导、ExprJSON、digest、边界与前端；2A 的 `decision` 里的费用字段与时间窗都依赖它，阶段 5 的指令特化也在等它定型。

**一句话**：金额与普通计算**按类型区分，逐个运算判定，没有模式**。一个运算的操作数里有 `money`，它就按金额规则执行（同币种、定点、舍入、`ErrCurrency`）；否则行为与现在完全一样。曾考虑过"表达式里出现一个 money 就整条进入金融模式"，否决：它让一行的含义取决于别处写了什么（加一句 `&& fee < cap`，`risk < 0.5` 里的 `0.5` 就变了义），而路由规则天然混合模型分数与费用。Catala、F# units of measure、safe-money 都是"金额是类型，不是模式"。

### 类型与量纲

| 项 | 决策 | 取舍与参照 |
|---|---|---|
| 三个类型 | `money<C>`（C 这种货币的一定数量）、`rate`（纯比例）、`fxrate<A,B>`（1 A = r B）；另有 `currency`（注册表币种的闭集，可进 `switch`、作字典键、由逻辑算出） | CDM 的汇率同样是 `unit` + `perUnitOf` |
| 量纲规则【已推翻】 | 乘除的结果量纲按代数算，只能落在 C（money）、无量纲（rate）、B/A（fxrate）三者之一，否则报错；`fxrate<C,C>` 化为 `rate`；加减与比较只在同量纲之间 | F# units of measure 的模型，但比它收紧（它允许 USD²）。由此自动得到：`money<B> / money<A> → fxrate<A,B>`（隐含汇率）、`money<C> / money<C> → rate`（实际费率）、`money<A> × fxrate<A,B> → money<B>`（换汇就是乘法，不另设 `fx`）、`fxrate<A,B> × fxrate<B,C> → fxrate<A,C>`、`fxrate<A,B> / fxrate<A,C> → fxrate<C,B>`（交叉汇率）、`money / rate`（反推含费总额）、`1 / fxrate<A,B> → fxrate<B,A>` |
| 渐进的币种 | 值**运行时带币种**；编译期的币种三态：具体（`money<USD>`，来自字面量与常量）、变量（`money<c>`，来自契约，变量小写）、未知（`money`，来自动态计算）。两侧都已知时编译期检查、运行时不查；任一侧未知就编译通过、运行时比对 | 否决过"运行时擦除币种"（safe-money/F# 的做法）：它要求币种编译期可知，规则自己算出币种（`money(amount, switch channel {…})`）或一次调用里订单币种各异（折算历史交易）就写不出来。主流库（Moneta、Joda、Dinero、go-money）全是运行时检查，编译期检查只有 safe-money 与 F#；这里两者兼得 |
| 结果里的币种变量 | 结果类型里的每个币种变量必须出现在某个参数里 | 参数化多态的标准规则 |
| `rate` 的表示 | int64 定点，精度 1e-10（DAML 的 `Decimal` 同款）；内部运算的舍入固定为银行家，不暴露；int 隐式加宽为 rate（`1 + 2.5%`、`1 / r`），float 不加宽 | 1e-8 对汇率偏紧，1e-12 的上限只有约 920 万 |
| 币种与小数位 | 注册表声明币种集合与小数位，`extensions/std` 附一份 ISO 4217 默认表；小数位**进类型从而进 digest**；注册表声明了币种，金额的类型、字面量与函数才可用 | 渠道与 ISO 常不一致（Stripe 的 ISK、HUF、TWD）。反例是 PostgreSQL 的 `money` 类型按 `lc_monetary` 定小数位，换个区域设置值就变了义 |
| 字面量定型 | 小数字面量按上下文定型：与 money/rate 运算时是 `rate`（`amount * 0.029` 精确），与 float 运算时是 `float`（`risk < 0.5`），无约束时默认 float。推导里是一种受约束的字面量项，最后统一落定默认值，线性，不分叉候选 | 这是阶段 1 取消的"数值字面量定型"，因 `rate` 重新有了用处，规模比当时设想的小（Go 的未定型常量、Haskell 的 defaulting） |

### 运算

| 项 | 决策 | 取舍与参照 |
|---|---|---|
| 两个世界的交接 | 只在这几处：`money × int`、`money × rate`、`money / money`、`money(int, cur)`、`minor(m) → int`、`rate(float)`、字面量 `0`。其余越界都是编译错误并指路：`amount + 5` 提示写 `USD 5` 或 `money(5, cur)`，`amount * risk` 提示写 `rate(risk)` | — |
| 构造【已推翻】 | `money(minor: int, cur: currency)`，金额是**最小单位整数**（`money(170, @USD)` 即 1.70）。`cur` 是常量时结果为 `money<USD>`，是契约的币种变量时为 `money<c>`，其余为 `money`（未知）。字符串转币种显式写 `currency_of(s)`，未注册报 `ErrCurrency` | Stripe、Adyen 的惯例；输入里的币种是数据，不能指望宿主总把金额与币种合成一个值 |
| 除法【已推翻】 | `money / int` 禁止，指向 `allocate`；`allocate(m, weights: array<int>)` 与 `allocate(m, n)`，余数先给比例最大的一份，一分不丢 | Dinero v2 删掉了 `divide`（"$10 ÷ 3 = $3.33，3 × $3.33 = $9.99"）；余数规则同 Dinero v2（Fowler 与 go-money 从第一个开始给，结果依赖参数顺序） |
| 零 | 零就是整数 0：编译期常量 `0` 可以加宽成任意币种的 money（`amount > 0`、`if(x, fee, 0)`），别的整数不行；运行时产生的零（空数组的 `sum`）是"最小单位 0、币种为空"，与任何币种相加、比较都成立；输出时结果类型是 `money<c>` 就补上 c 的绑定，是未知的 `money` 就按整数 0 输出（`Money{0, ""}`） | Ledger 允许不带币种的 0 |
| 负数 | 允许（退款、冲正、调账）；要求非负的契约由宿主在边界检查 | 各家都允许 |
| 聚合 | `sum`/`min`/`max`/`sort`/`sort_by`/`top_k`/`arg_min`/`cumsum`/`deltas`/`abs` 照常；`avg`/`median` 按默认舍入落到最小单位（先除后加、余数进位，所以总和超出 int64 时平均值照样算得出）；`stddev`/`percentile` 不给 money；`sign(m) → int` | — |
| 取值 | `minor(m) → int`，`currency(m) → currency` | CDM 的 `ISOCurrencyCodeEnum`、Joda 的 `CurrencyUnit` 注册表 |
| 按币种的阈值 | 首选宿主直接传同币种阈值（或 `amount_usd: money<USD>`），其次 `like(amount, n)` 在同一币种里造出 n 个最小单位配合查表 | Stripe Radar 规则里写的是 `:amount_in_usd: > 1000`：平台先折算，规则只比 USD |
| 溢出 | int64 最小单位溢出报错；闭合表达式即编译错误 | COBOL `ON SIZE ERROR`、Solidity 0.8 同 |

### 舍入

| 项 | 决策 | 取舍与参照 |
|---|---|---|
| 默认方式 | **注册表声明**（进 digest），`round(…, mode)` 在调用处显式覆盖；语言不内置默认 | 业界互相矛盾：欧盟 1103/97 与 Excel `ROUND` 是半数远离零，IEEE 754 十进制、.NET、Python、DAML 是银行家，Joda 与 safe-money 不设默认、每次显式，COBOL 不写 `ROUNDED` 就截断（反例），Excel 与 VBA 同产品两套（反例）。一个注册表对应一个控制台，它知道对接的渠道怎么算 |
| 时机 | **每步舍入**：每个 `money × rate`/`money × fxrate` 立即落到最小单位，写法即舍入点；`a * r1 * r2` 与 `a * (r1 * r2)` 可能差一分，格式化保留括号 | Catala、Joda 的 `Money`、COBOL 同。否决延迟舍入（safe-money 的 `Dense`、Joda 的 `BigMoney`、Dinero 的小数位增长）：要多一个比最小单位更细的中间类型和一套规则 |
| float 进入 | `rate(x)` 按 float 的**最短十进制表示**转换（`strconv.FormatFloat(x, 'f', -1, 64)`；不用 `'g'`，它会写出 `ParseRate` 不读的指数形式），所以 `0.03` 就是 0.03 | Joda 的 `BigDecimal.valueOf(double)` |

### 写法、边界与错误

| 项 | 决策 | 取舍与参照 |
|---|---|---|
| 字面量 | money：`USD 1.70`、`USD -1.70`（负号写在数上，紧贴数字；`-USD 1.70` 是对金额取负，不是字面量）、`JPY 100`，位数超过币种小数位报错；rate：`2.9%`（`%` 紧贴数字就是比例，没有例外；取模写 `10 % 3`，`10%3` 是语法错误）、`25bps`、`0.5bps`，超出精度报错；fxrate 先不做字面量；ExprJSON 里金额存十进制文本 `"1.70"`（否则 ExprJSON 依赖注册表的小数位） | Beancount 写 `1.70 USD`，Catala 的 `30%` 是 decimal 字面量；没有找到汇率字面量的先例 |
| 契约 | 宿主可写 `money<c>`（编译期检查）或 `money`（运行时检查）；同一个 c 绑定的参数在装载时校验一致 | — |
| Go | `lang.Money{Minor int64, Currency string}`；钱的数组 backing 直接是 `[]lang.Money`，与宿主之间零拷贝 | — |
| JSON | 输入既认 `"USD 1.70"` 也认 `{"currency": "USD", "minor": 170}`，输出按宿主选择 | Stripe、Adyen 用最小单位整数，PayPal、ISO 20022 用十进制字符串，Google 的 `google.type.Money` 是 `units` + `nanos` |
| 错误 | 新增 `ErrCurrency`：运行时币种不一致、`currency_of` 收到未注册的币种、装载时同一个 c 绑定的参数不一致（后者同时是 `ErrContract`）。`fallback` **不接** `ErrCurrency`：它与缺键一样说明规则或数据有问题 | money 是语言的特性，值得一个专门的错误类别 |

### 读代码后补定（2026-09-23）

| 项 | 决策 |
|---|---|
| 小数位怎么进 digest | 放不进类型（`money<c>` 编译期没有小数位，`hasType` 按 `Type.Equal` 比较）。改为 artifact 带金额戳 `Money{Rounding, Table}`（币种表的 sha256），只在程序用到金额类 kind 时写入，`Instantiate` 与注册表比对；不用金额的程序 digest 逐字节不变 |
| 币种变量的一致性 | 在每次运行绑定参数时校验（装载 artifact 时还没有参数值），失败同时是 `ErrContract` 与 `ErrCurrency` |
| 两边未知的 `money / money` | 默认得 `rate` 并在运行时检查同币种；单位可证明不同、或结果声明为 `fxrate` 时才得 `fxrate` |
| `round(expr, mode)`【已推翻】 | 舍入**作用域**：管住 `expr` 里所有舍入步骤，嵌套以最内层为准，内部没有舍入步骤是编译错误。`mode` 是注册表提供的枚举 `rounding`（`@half_up` 等），币种是注册表提供的枚举 `currency`（`@USD`）；"枚举只从契约进入程序"的不变量改为"来自契约或注册表（仅这两个）"，契约里保留这两个名字 |
| `%` 消歧 | `%` 紧贴在数字后面就是比例的单位，**没有例外**，不看后面跟什么：`10%-3` 是 `10% - 3`（-290%），`2.9%-fee` 是比例减费用；比例后面紧跟操作数（`10%3`、`10%x`、`10%(x)`）是语法错误并说明原因。取模要在 `%` 前留空格，或左边不是数字字面量（`x%3`）。三次改定：最初"后面不是操作数才是比例"要看后文，`7%-2` 取模、`7% - 2` 比例，同一个 `%` 靠空白决定意思，是歧义的来源；没有对外承诺兼容，所以直接取消所有特例 |
| 边界扫描 | 金额容器在边界上做一次只读 O(n) 扫描（币种已注册、同一 c 一致、建立绑定），不分配，零拷贝保留。这是"`RunValues` 不深度扫描"的明确例外：值带币种、零拷贝、不扫描三者不可兼得 |
| 纵深防御 | 编译期已证明同币种的内核加减仍在运行时比一次币种（约 1 ns），防推导或编译器自身的 bug |
| 其他类型的形状【已推翻】 | `lang.Rate`（int64，1e-10）、`lang.FxRate{Base, Quote, Rate}`、`lang.Currency`（具名 string）；JSON 输入接受十进制字符串或数字，输出十进制字符串。补上 `float(rate)`；`rate(float)` 超出 10 位时银行家舍入到 1e-10 |
| 十进制输出 | `Value.MarshalJSON` 拿不到小数位，只输出 `{"currency","minor"}`；`"USD 1.70"` 由知道注册表的编码入口输出（LSP 与 CLI 用它） |
| 换汇缩放【已推翻】 | `money × fxrate` 按两币种小数位差缩放，求值器经闭包持有币种表；小数位限定 0–8，保证 `1e10·10^k` 不溢出 int64 |
| 终止性 | `allocate(m, n)` 标 `BoundedArgs`（凭空造出长度 n 的数组，定理 B） |
| 契约写法 | 补上 `currency<c>`：契约字段声明它，`money(amount, cur)` 就得 `money<c>` |
| 第一版的限制 | 反射推不出币种变量：std 的金额函数用 `FunctionSpec` 手写，`Bind` 推出的契约暂时只能是 `money`；record 整体合一，字段里的币种不单独推导；`amount + USD 5` 在 `amount: money<c>` 时是编译错误，提示指向 `like` |
| 错误路径 | `frame.classify` 原样放行 `ErrCurrency`（现在会被压平成 `ErrExtension` 的文本），`catchFallback` 显式排除它 |
| 注册顺序 | 宿主先 `DeclareMoney` 再注册 std；std 只在声明了币种时注册金额重载 |

### 测试后补定（2026-09-23）

五个测试子代理补齐金额测试后发现的设计问题，对照其他金融语言的做法定下：

| 项 | 决策 | 参照与取舍 |
|---|---|---|
| 记录里的金额 | 按单个金额的规则渐进：记录项带每个币种位置的单位类（`compile/infer_record.go`），同形状的记录逐位置合并；`{fee: USD 0.30}` 满足 `record{fee: money}`，`{fee: u}` 满足 `record{fee: money<USD>}` 时末尾检查，已证明是别的币种报编译错误；分支里币种不同的记录在汇合处合并，字段读取时再检查 | safe-money 要求 `toSomeDense` 显式转换；TypeScript/渐进类型是已知流向动态、反向插检查。单个金额本来就是后者，记录是唯一例外，属于实现缺口 |
| 汇率 × 整数【已推翻】 | `fxrate × int`、`int × fxrate`、`fxrate / int` 结果仍是同一对币种；除法按比例的十位小数银行家舍入，与 `fxrate / rate` 一致；Go 侧是 `FxRate.MulInt`/`DivInt` | Joda-Money、JSR 354 的汇率就是一个数；F# units 里整数没有量纲 |
| 两边未知的汇率相乘【已推翻】 | 读成串联 `fxrate`；往返（得 rate）只在能证明两边互为反向时成立，否则吃 `fxPenalty` | F# units 与 HM 推导取最一般的类型，往返是串联在 a = c 时的特例 |
| 常量折叠失败的位置 | `syntax.AroundError` 把运行期错误放到被折叠的节点上，`errors.Is` 仍找得到原类别 | Go 对常量除零同样报位置 |
| 加密货币 | 按业务精度声明（稳定币 6 位、其余 8 位），链上精确金额由宿主记账系统换算；不做 128 位金额 | int64 按 wei 只能表示约 9.2 ETH；128 位会让 `Value` 变大、所有程序变慢 |
| 负金额的写法【已推翻】 | 字面量与宿主文本都只有 `USD -1.70` 一种：负号属于数。金额是一个词法 token —— 代码、至少一个空白（不含注释）、紧贴数字的可选负号、不带指数的数 —— 所以 `USD - 1`、`USD-1` 是名为 `USD` 的变量减 1；`-USD 1.70` 在规则里是一元负号作用在金额上，在宿主文本里是未声明的币种 `-USD`。README 的文法按优先级分层写、各 token 互不重叠，没有歧义 | 规则、宿主文本、`EncodeJSON` 的输出同一种形状；打印器不再需要为 `sub(0, USD 1)` 写成 `0 - USD 1` 的特例 |

### 金融上不成立的操作（2026-09-24 定）

| 项 | 决策 | 取舍 |
|---|---|---|
| 算术失败的类别 | 新增 `ErrArithmetic`：溢出、除零、float 非有限、非法汇率；规则或数据的错，`fallback` 不接，扩展函数里发生也原样放行；边界上的同时是 `ErrContract` | 此前内核的溢出与除零没有任何类别，宿主无法 `errors.Is` |
| 零与负的汇率 | 边界（参数、宿主函数结果）与运算途中都拒绝 | 负汇率与零汇率没有金融含义，只会把错数据算成错账 |
| 同币种汇率 | 恒为 1：传入必须是 1，运算得到的规整为 1（加点差、往返的舍入误差都消失）；同币种金额相除只有相等才得 1，否则 `ErrArithmetic` | 宿主对同币种传恒等汇率，规则不必判断币种是否相同；同币种不收汇差 |
| 汇率相减【已推翻】 | 删除，编译错误并提示 `ask / bid - 1`；保留相加用于平均 | 报价之差不是能换汇的汇率 |
| `money(minor(a), @JPY)`【已推翻】 | 编译期拒绝：`money`/`like` 的整数实参来自 `minor(...)`（含经 `let`/`reduce` 转手）| 不经换汇就改币种的后门；`minor` 仍可读出整数 |
| 负零 | `USD -0` 字面量与 ExprJSON 的 `"-0"` 拒绝 | 没有意义的第二种零 |

### 换汇、币种与公开接口（2026-09-24 定并完成）

起因：汇率此前是定点的 `fxrate` 值，规则写 `amount * fx`，另有一整套汇率运算（串联、交叉、倒数、点差、平均）。它们让规则可以把汇率乘到错的币种上、逐段舍入出差一分的链，而且每一种运算都要回答"同币种时怎样""结果不为正时怎样"。同时 Go 公开包里的 `Money{Currency, Minor}`、`Type`、`Artifact` 等字段宿主可写，一个不合规则的值可以绕过边界直接进 Go 方法。本轮把两件事一起收紧，全部落地并由测试覆盖。

| 项 | 决策 | 取舍 |
|---|---|---|
| 公开接口 | `lang` 不导出任何宿主能写字段的数据类型：`Money`/`Rate`/`Currency`/`FxRate`、`Type`/`Field`、`Artifact`/`Parameter`、`Manifest`、目录描述、`PositionError` 字段全部私有，经币种表、`ParseRate`/`Percent`/`BasisPoints`、`RecordOf`/`FieldOf`/`MoneyOf` 等构造，经访问方法读，JSON 形状逐字节不变。撤掉 `MoneyValue`/`RateValue`/`FxRateValue`/`CurrencyValue`、`RateScale`、`MulDivRound`、`Constant`/`Instruction`/`CallReference`/`OpCode`/`MoneyStamp`/`ManifestFunction`、`RateFromFloat`/`RateFromInt`/`Rate.Float64`、`Rate.MulInt`/`DivInt`、`Currencies.Back`；`FxRate.Then` 改名 `Chain`；新增 `AverageMoney`/`MedianMoney`、`ErrNoRate`、`Rates` | 选项结构体（`CompileOptions`、`FunctionSpec`、`Doc`、`MoneySpec`、`RunOptions` 等）仍可写：它们是输入，在使用处校验。宿主写 `table.Minor("USD", 170)` 而不是字面量结构体，多一次错误检查，换来"币种一定已声明"在类型层面成立 |
| 比例只为金额服务 | 删掉 int 与 rate 的一切混合（`int ± rate`、`int × rate`、`rate ÷ int`、`int ÷ rate`、比较）与 `rate(float)`/`rate(int)`/`float(rate)`，保留 `rate(string)`；`1 - fee` 写成 `100% - fee`，编译错误会提示。字面量 `0` 也可读作 rate，所以 `-fee`、`-2.9%` 照写 | 推翻阶段 2B"int 隐式加宽为 rate"与"补上 `float(rate)`"：比例离开金额就没有用处，而混合运算让 `n * fee` 这种量纲不明的式子能编译。float 与比例互转是把近似值带进精确计算的入口 |
| 币种是一等公民 | 形如代码的名字（大写字母开头、3–8 位大写字母或数字）就是币种字面量：`money(170, USD)`、`currency(m) == USD`；`@USD` 取消，`currency` 不再是注册表枚举，只剩 `rounding`。变量名不能是代码形状（`USD`、`URL` 都不行），字段名与枚举成员不受限 | 币种不是契约带来的闭集，是注册表的事实，写 `@` 暗示它和 `@adyen` 一样来自契约。代价是少了一批可用的变量名；代码形状本来就少见，而歧义（`USD` 是变量还是币种）没有了 |
| 汇率是精确有理数 | `fxrate<A,B>` 的值是 `big.Rat`：字面量 `150.25 JPY / USD`（数在前，精确，小数位不受币种限制）、异币种金额相除、契约参数、宿主函数结果；JSON `{"base","quote","rate"}`，rate 是有限小数就写小数、否则 `"p/q"` | 取代十位小数定点的汇率：`0.0000000065` 这类报价在定点下丢位，链式换算逐段截断。精确值只在换出金额时舍入一次，"汇率链的结果与报价加入顺序无关"才成立；汇率不在热路径的算术里，`big.Rat` 的开销只在换汇时出现 |
| 没有汇率运算【已推翻】 | fxrate 只能作结果、进容器与记录、`==`（同一货币对；不同货币对是 `ErrCurrency`）、交给 `using`；不能乘金额，没有串联、交叉、倒数、点差、平均 | 取代阶段 2B 的"换汇就是乘法"与 2026-09-23/24 补定的汇率 × 整数、汇率相加、串联读法。每种汇率运算都是一处可以把汇率用在错误币种上的地方；路径与链由汇率表负责，规则只说"换成什么" |
| 同币种汇率恒为 1 | `1 USD / USD` 合法、`150 USD / USD` 编译错误；运行时同币种金额相除得 fxrate 时只有相等才得 1，否则 `ErrArithmetic`；`table.FxRate("USD", "USD", "1")` 合法，汇率表加同币种 `"1"` 是空操作 | 规则与宿主不必先判断"支付币种是否等于结算币种"：同币种换汇就是原样，不会收汇差 |
| 换汇 `->` | `amount -> JPY`（或 `amount -> target`，target 是 currency 值）脱糖为内核函数 `convert`，优先级介于 `+ -` 与比较之间（`fee + amount -> JPY` 换的是和）；另有带舍入枚举的变体供 `round(…, @mode)` 选择。它读运行时的汇率表，不是 constexpr，不折叠 | 换汇是一个运算符而不是一个乘法：写法说明意图，目标币种写在规则里，规则就不可能把 USD/JPY 用在 EUR 上 |
| 汇率表【选路部分已推翻，见「换汇单跳」】 | `table.NewRates()`、`rates.Add("USD", "JPY", "150.25")`（纯十进制、至多 40 位、为正）、`AddRate`、`Convert`、`Rate`；`RunOptions.Rates` 传给所有运行入口。路径是经过币种最少的（BFS），直接报价优先于反向倒数，同长按代码排序；整条链精确相乘后只舍入一次（EUR 0.05 经 USD 到 JPY 得 8，逐段舍入是 9） | **写时复制 + 运行快照**：`Add` 复制图、原子发布新版本，一次运行在开始时取当时的版本。行情线程随时更新，规则并发读不加锁，同一次运行里的两次换汇也不会看到两个版本的汇率。确定的路径选择让同一张表永远给同一个答案 |
| `ErrNoRate` | 没有汇率表或找不到路径；`fallback` 接它 | 与扩展失败同类：数据暂时不可得，而不是规则或数据错了（`ErrCurrency`/`ErrArithmetic` 仍不接） |
| 局部汇率 `using`【已推翻，见「`using` 永远隔离」】 | `using(..., rate, …, body)`：body 里的 `->` 按这些汇率换。`...` 是外层汇率表，**后写的赢**：`...` 之后的覆盖外层同一货币对（两个方向），之前的只补外层没有的货币对（能与外层拼路径），不写 `...` 则隔离；嵌套逐层叠加。字节码 `OpFxPush`/`OpFxPop`，`fallback` 恢复进入时的作用域 | "后写的赢"借鉴记录更新 `{...r, a: 1}` 与 JS 的对象展开：同一门语言里 `...` 只有一种读法，位置本身就说明优先级，不需要 `override`/`default` 两个关键字。`fallback(amount -> JPY, using(150 JPY / USD, amount -> JPY))` 是整段备选，与"补充"不同：两边汇率不拼路径 |
| 工作台与语言服务 | `funroute.run` 接受 `rates`，错误类别加 `norate`；试运行面板有"汇率表"（一行一条 `USD/JPY 150.25`），示例可带 `rates`；语义标记加 `currency`，币种是普通补全项；结构视图把 `using` 画成卡片 | — |

### 审查后的修正（2026-09-24 定并完成）

一次全面审查（五个方向并行：运行时、推导、词法与文法、文档与公开面、跨境金融场景）加一份外部文法评审之后定下并落地。

| 项 | 决策 | 取舍 |
|---|---|---|
| 字面量相遇 | 同一个开放类型上写成不同类型的字面量（`if(b, 0, 0.5)`）是类型错误，除非上下文要一个比例；此前会悄悄落成其中一个，运行时返回类型与 artifact 不符 | 声明金额前后，不写金额的程序行为不变，这条承诺因此成立 |
| 宿主结果的币种 | 签名写明了币种的宿主函数，结果按调用点的类型逐层检查（容器、记录里也查）；内核结果不查 | 编译器信任签名、`hasType` 只比形状，两者合起来漏掉了容器里的币种 |
| 汇率的尺寸 | 每边至多约一千位，所有产生汇率的路径经同一个构造检查；宿主手写的报价仍限 40 位 | 此前算出来的汇率写得出 JSON 却读不回 |
| 批处理 | 先检查参数，引擎只看到合格的请求 | 此前引擎会收到币种错误甚至类型错误的参数 |
| 装载 | 金额、币种、汇率常量按注册表的币种表检查 | digest 只证明没被改过，不证明封装者诚实 |
| 错误类别 | 已有类别的错误在 `classify`、限时调用、批处理里都原样保留；转换失败（`int("x")`、`rate("abc")`）归 `ErrArithmetic` | 宿主返回的 `ErrNoRate`、`ErrCurrency` 此前会被包成 `ErrExtension` 或 `ErrDeadline` |
| 两个币种变量 | `money<c>` 与 `money<d>` 相遇编译通过、运行时比对；代码碰到变量仍是编译错误；`money<c> / money<d>` 仍读成汇率 | 同币种收付与退款是常态，编译期假定它们不同会拒绝正确的规则 |
| 金额戳 | 只记默认舍入与字节码里写死的币种的小数位；运行时才知道的币种跟随当前表 | 此前是整张币种表的哈希，新增一个币种就让所有 artifact 失效 |
| `allocate` | 缺省最大余数法；末尾可写策略 `@largest_weight`、`@in_order`、`@reverse_order`、`@all_first`、`@all_last`（注册表的枚举 `allocation`） | 此前余数按权重给，小份额方系统性少分；清分里"舍入差全给收单行一份"之类的约定需要策略 |
| `minor` 复用 | 删除"`minor` 的结果不能造回金额"的检查 | 按文本追踪堵不住（`JPY 1 * minor(usd)`），又会误伤现金舍入这类写法；保持语法完备，责任在规则作者 |
| 汇率运算 | 放开加点（`fxrate × rate`，精确、只在换汇时舍入一次）与同一货币对的比较；新增 `fx(base, quote)` 读运行时汇率 | "汇率没有运算"迫使加点写成"先换汇再乘比例"，反而多舍入一次 |
| `prorate`、`round_to` | `prorate(m, part, whole)` 精确按比例取；`round_to(m, 粒度)` 做现金舍入 | 按比例退手续费在百亿级金额上差几个最小单位；现金舍入此前写不出来 |
| 具名汇率表 | 契约声明表名，`using(@settlement, …)` 以宿主传入的那张为底；没传的表是空表；报价可带来源与时间，只记录不判定 | 市场价与清算价要同时用；报价时效由宿主决定 |
| 文法收紧 | 金额与汇率字面量内部只认空格与 Tab；`->` 的右边是一个 postfix；比较不结合；`else` 必须写 `=>`；`if` 不能作变量名；小数字面量必须恰好是 float64 能表示的写法；负号捷径只用于单独的数；文法补上 `EOF`、`decimal` 与片段的优先级 | 都是"不同读者可能读出不同意思"的地方：跨行的金额、`amount -> JPY + fee`、`a == b == c`、两种 `else` |
| 换汇单跳 | `->`、`Rates.Convert`、`Rates.Rate`、`fx` 只用这一对货币的报价或反向报价的倒数，不再搜索经第三种货币的路径；要中转就在规则里连写 `amount -> CNY -> USD`，每一跳各舍入一次 | 自动选路让规则用哪条汇率取决于表里还有什么，等长路径的结果还可能不同；写明每一跳，汇率与舍入点都看得见。取代此前的"BFS 最短路径、整条链只舍入一次"，路径策略也就不必做了 |
| `using` 分配 | 报价全是常量的 `using` 在装载时建好表，运行时 2 次分配（此前 51 次） | 动态报价或具名表时仍要新建一张表 |
| `using` 永远隔离 | 删掉 `using` 里的 `...`，连同"补充"（写在 `...` 之前）与"覆盖外层"（写在之后）：主体只用写出的报价，有具名表时盖在那张表上；要沿用外层的汇率就写 `fx(base, quote)`。ExprJSON 去掉 `fills`/`outer`，`OpFxPush` 只剩 `B` 个报价与可选的表名 | 同一个 `...` 前后位置不同意思就不同，读规则的人要记住三种组合才知道用的是哪条汇率；隔离之后看 `using` 本身就知道，沿用外层也只多写一个 `fx(…)`，还能顺手加点 |
| 字段更新写作 `with` | `{...order, fee: 0}` 改为 `order with {fee: 0}`：`with` 是保留字、后缀，与 `.field` 同一层；词法里的 `...` 整个删掉 | `using` 去掉 `...` 之后它只剩这一处，而花括号开头要多看一个记号才知道是字面量还是更新；`with` 从左往右读就是"这份记录，改成这些"。`->` 已是换汇，不复用 |
| 官方函数的案例 | `Doc.Examples`（`{source, result}`）：内核与标准库每个函数名都有案例，合起来选中该名字的每一个重载（舍入变体经 `round(…)`），形式也有；测试逐条编译运行、比对结果；悬停与补全显示案例 | 函数说明只有文字时，重载与边界要读者自己猜；案例由测试运行，不会和实现走样 |

### 拆出去单独做

- **按报价时效判定**：报价已带 `At`/`Until`，但表不判定过期；若要由语言判定，需要确定性的"当前时刻"作为运行参数。
- **放宽币种代码与小数位**：以数字开头的代码（`1INCH`）、超过 8 位小数（链上精度）。会动词法与 int64 的溢出边界。
- **字典推导的多个 `for`**：需要先定重复键的含义。
- **float 相等警告**：要先有警告通道（编译结果带 warnings，LSP 用 Warning 级别），与 money 无耦合。
- **`date` / `timestamp`**：时区与 `now` 作参数、保持纯函数；表示、时区数据怎么带、`now` 的形状另议。

## 阶段 3：治理工具

规则自带用例（保存即跑）、`funroute check` 批量兼容性检查、Artifact 版本 diff、`ContractCompatible`、决策 trace、`Simulate` 回测、影子运行 `Compare`、`Strict` 模式。全部在 SDK 与 CLI，不碰语言。

## 阶段 4：模型接入深化

批处理第二级（按调用依赖分层合批），前提是先用真实引擎测出第一级的收益边界。

## 阶段 5：执行层提速（阶段 1 之后）

**不是现在做**：阶段 1 的 `record` 与索引、阶段 2B 的 `money` 都会改变指令集形状，此刻做指令特化，到时要重来一遍。等语言形状定型后作为一个独立阶段做，基线与归因见上面的性能小节。

| 项 | 决策 | 取舍 |
|---|---|---|
| 内核算术与比较特化成 opcode | `add`/`sub`/`mul`/`div`/`lt`/`le`/`gt`/`ge`/`eq` 的参数类型编译期已定，直接发 `OpAddInt` / `OpMulFloat` 这类指令，VM 在栈上算完，不走 `call`、不构造参数视窗、不重做类型校验 | opcode 表变长，`ArtifactVersion` 递增；换来算术从 168 ns 压到 50 ns 内且仍是 0 alloc —— 那时会明显快过 expr。**比 expr 更有条件做**：它的 opcode 还要在运行时 type switch，我们不用 |
| 聚合管道融合 | 编译期把「聚合函数套一个 `ForExpr`」重写成 `ReduceExpr`：`sum([x for x in xs if p])` 单遍折叠，不构造中间数组 | 纯前端 AST 重写，不动 VM，语义等价（`for` 与 `reduce` 共用循环指令）。对照：expr 的 optimizer 有一批同类重写（`filter_first`、`filter_len`、`sum_array`、`in_array`） |
| 免反射的宿主调用 | 为常见 Go 签名自动生成类型化包装，绕开 `reflect.Call` 的约 300 ns | `FunctionSpec` 现在是让人手写的逃生口，这条把它自动化；对照 expr 的 `OpCallTyped` |
| 类型化绑定的参数读取 | `Program.Run` 比 `RunValues` 每个标量参数多约 15 ns（2026-09-23 实测：两个参数 212 对 179 ns，三个参数的算术 205 对 161 ns），花在 `encodeArgs` 的逐字段分派上：`load` 的 shape 分派、`loadScalar` 的包装与 enum 检查、`loadInt` 的 kind 分派，每一层都是一次函数调用。实例化时把参数计划压平成一张「kind + 偏移」的扁平表，标量在一个紧凑循环里直接读，只有容器、record、句柄才进递归的 `load` | 只动 `machine/plan.go` 与 `access.go`，artifact 与公开 API 不变。必须守住 `TestProgramScalarsDoNotAllocate`（0 分配、`*In` 不逃逸）与 `box` 的内容逃逸标签，用 `BenchmarkRunPaths/Program/typed` 与改动前交替对照 |

## 明确不做

张量类型与 float32 类型（句柄替代）、lambda、递归、语言内状态与循环、`$` 币种符号、`let … in` 语法、`[][]float64` 之上的专用嵌套类型（反射转换替代）。变参函数优先级最低，暂缓。

对照通用求值器新增的不做项：`nil` 与 `??` / `?.`（引入 null 就引入静默传播）、`$env` 与未定义变量（运行时才发现缺参数等于放弃 digest 的意义）、动态 `type()`、正则 `matches`（成本不可静态估计；确需时做成注册时预编译、输入限长的扩展函数，成本明码标价）、`toJSON` / `fromJSON` / base64 / bytes / 位运算（序列化与字节处理归宿主）、pipe `|` 与方法调用链。
