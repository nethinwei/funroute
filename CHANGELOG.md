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
- **向量循环不再打崩进程**：循环体先在 int 组里算、再收集成 bool（`[x * 2 > 5 for x in xs]`、`len([x % 3 == 0 for x in xs])`），16 项以上时原来 panic；不调宿主函数的程序不设 recover，宿主进程随之崩溃，调了的则被当成 `ErrExtension` 交给 `fallback`。
- **伪造的 artifact 不再能在装载时 panic**：`loop_collect` 的操作数原来不校验，A 改成 2、-1 等就打崩 `Instantiate`；现在 A 只能是 0 或 1，其余操作数为 0。
- **格式化**：`a -> (-1)`（即 `convert(a, -1)`）原来打印成解析不回的 `a -> -1`，以 bool 为键的推导式 `{(true): 1 for x in xs}` 打印成 `{true: …}`。
- **`ConstArgs` 不再在编译期调用宿主**：`matches(s, host.pattern_v1())` 原来在编译时真的调用了未标 Constexpr 的宿主函数。
- **编译错误的位置**：`[]`、`x`、`first([])`、`at(d, "k")` 这类结果类型推不出的程序报错不带位置；`switch` 的 case 与主体类型不符时报在分支结果上，现在报在那个 case 上。
- **`FxRate.Chain` 串回起点不再丢掉点差**：原来一律得 1，与规则里照乘积换出的答案不同。
- **精确金额不会被写成宿主的 `Money`**：伪造 artifact 让 `round` 之外的精确金额到达宿主时，原来把分子当作最小单位写进去（半分钱写成 USD 0.01），现在是错误。
- `allocate(m, n)` 的 n 在 32 位平台上先在 int64 上比较上限，不会被截断绕过 10000。
- **批处理**：被拒绝的请求原来不计入"不再等待"，其余请求都不等了也不取消引擎调用；全部被拒的批次仍以零条调用引擎。
- **`round(prorate(m, part, whole))` 与 `prorate(m, part, whole, @mode)` 同一个答案**：按两笔金额取比例时，精确路径原来先把 part/whole 做成比例，whole 是最小金额时比例放不下就报 `ErrArithmetic`，而结果放得下。新增 `ExactMoney.ProrateBy`。
- **编译出的 artifact 一定能装载**：宿主的 Constexpr 函数返回含 handle 的 record 或数组时，原来被折成一个装载时读不回的常量；现在含 handle 的类型一律不折叠。
- `repeat("", n)` 在 32 位平台上不再因 n 回绕而 panic；宿主切片视图的置换下标不再用 int32。
- 内核专用的取下标指令与 `reverse`/`sort_by` 的重排遇到不认识的存储时交给通用路径，不再静默给出旧值或原数组（目前不可达，防将来新增存储时出错）。
- **float 数组的相等不再取决于是否共用内存**：`let(xs = [0.0 / 0.0], xs == xs)` 原来是 `true`（同一个 backing 走了捷径），`xs == [0.0 / 0.0]` 却是 `false`；现在都逐项按 IEEE 754，NaN 不等于任何值。`switch` 的值匹配、`unique` 同理。
- **`ConstArgs` 的实参在折叠预算内求不出值时是编译错误**：原来静默放过，`matches` 写错的模式要到运行时才报。
- `Run(map)` 收到零值 `FxRate` 或超出年份的 `time.Time` 时报 `ErrCurrency`、`ErrDomain`，原来被报成类型不符。
- 契约里两个同名不同的枚举报 `ErrContract`，文案每次相同；`CompileOptions.Enums()` 每次保留同一个。指令数超过 `MaxInstructions` 的编译错误带位置。
- 向量循环在失败或停止处交回循环体时，已结算的项计入 `ctx` 的检查与折叠预算。
- 已关闭的 `Batch` 收到的请求归还池中。
- **`reduce` 的条件不再看到累加器**：`reduce(x in xs if acc > 1000, acc = 100, acc + x)` 里条件的 `acc` 是同名参数（推导一直这样定型），编译却按累加器取值，答案错（得 100，应为 103）；参数与累加器类型不同时编译成功、装载失败。
- **闭合失败写在哪个分支都是编译错误**：分支本身读循环项、只有其中一部分闭合时（`sum([if(true, x, 1 / 0 + x) for x in [1, 2]])`），原来能编译，换成读参数的同一写法却报错。
- `ConstArgs` 的实参在整个调用能折叠时也按编译期规则检查：`matches("abc", "(")` 与 `matches(s, "(")` 是同一条编译错误，指在模式上，原来前者是 `ErrDomain` 的运行错误。
- 语言服务：光标在选择器名字中间（`sort_by(quotes, .f|ee)`）也补全字段；`&&`、`||`、`!`、`!=` 与一元负号展开出的操作数不再占运算符的源码区间，悬停不再显示 `&&: bool`，语法树里不再出现幽灵操作数；多段选择器 `.customer.fee` 在语法树里有路径的位置；`args: null` 视同没有参数；格式化遇到表达式里的注释时回复原因，原来静默不动。
- `Doc.Detached` 的函数拿到的实参连同容器先复制一份：超时后被丢在后台的调用不再读宿主可能已复用的内存（宪章 S5）。
- 已关闭的 `Batch` 拒绝请求的错误归入 `ErrContract`。换汇约分后分母放不下时的文案说明是汇率过细，不再复用"金额溢出"。
- **比较或字段读取存进 `let` 后被读两次时答案错**：`let(c = a < b, add(if(c, 10, 20), if(c, 1, 0)))` 原来得 10（应为 11），整个闭合时连折叠出的常量也是错的。翻译器把比较与跳转合成一条时删掉了它写进局部变量的值；现在只在栈临时值上合并。
- **宿主结构体或切片里的零值 `FxRate` 不再让 `ToValue`/`Run` panic**，是 `ErrCurrency`。
- **`let` 绑定的闭合失败写在未走分支里也是编译错误**：`let(x = 0, if(true, 2, 1 / x))` 原来能编译。
- **重载的代价在 32 位平台上不再溢出**：同一份源码在 386 上曾推断出不同类型（`at(xs, i)` 推不出类型，2048 个以上的零之和推成 `money`）。
- `len([0.1000000000000000055511151231257827 for x in xs])` 这类计数不编译的字面量也按写出的值检查。
- `Binding.Options()` 返回的契约是真正的拷贝：改它的参数类型会被采用，原来被缓存的类型盖住。
- 汇率 JSON 的 `rate` 收整数（`"rate": 150`），与金额、比例一致。
- 语言服务：悬停在 `.fee` 上是字段的类型（`int`），不再是整个键数组；分组括号里仍有签名提示；`(USD 1.70)` 的悬停给出最小单位；`(.fee)` 的路径位置不含括号；超大的列号不再溢出成负数；`a != b` 的语法树直接是 `a` 与 `b`；结构视图里 record 与字典的值是卡片时保留字段名与键。
- 没声明金额时，契约自己叫 `rounding`/`allocation` 的枚举按契约的枚举处理，与别的枚举同名成员时报歧义。
- **语法树对运算符链不再指数级重复**：每个运算符的操作数原来先随展开建一次、再按写出的操作数建一次，`!!!…!0` 或 `a + a + …` 每多一层耗时翻倍（20 个 `+` 要 1.6 s），`funroute/syntaxTree` 与格式化 fuzz 会卡住；现在每个节点只建一次。
- **夏令时切换在零点的日子不再落到前一天**：`start_of_day` 在零点被跳过的时区（`America/Santiago`、`Asia/Beirut`）原来给出前一天 23:00，`add_days` 落进被跳过的钟点时同样会少加一天（`America/Havana`）。现在跳过的钟点读成跳过之后同样远的时刻，重复的钟点取较早的一次，与 java.time、RFC 5545 一致。
- **整数 `stddev` 不再先转成 float 再相减**：2^53 以上相邻的整数原来被折成同一个 float，`stddev([9007199254740993, 9007199254740995])` 得 2（应为 1）；现在以 128 位求和、按整数算每项到均值的距离。
- **点号后有空白时名字的位置**：`order . amount`、`f(x) . a` 的字段名原来没有位置（结构视图里改不了名），`sort_by(xs, . fee)` 的路径区间含空格、少一个字母，改名会改坏源码。
- **折叠失败报在失败处**：`if(false, 1, 1 / 0)`、`2 + 3 * (1 / 0)`、`let(a = 0, b = 2, b / a)` 原来报在整个闭合部分的开头，现在报在 `/` 上，与走了的分支、未走的分支一致。
- `len([])`、`len({})` 这类推不出元素类型的空字面量是带位置的类型错误，原来是编译阶段的 `cannot compile array with unresolved type`；`first([])` 的错误指在 `[]` 上。
- `USD 1.00 -> JPY` 写在 `using` 外时先报"只有 using 有汇率"，原来先让人加 `round`，加了才报真正的原因。
- **数组的 `min`/`max` 与两个数的同名函数同一个答案**：数组版原来把 0 与 -0 当作相等、取先出现的，`min([0.0, -0.0])` 得 `0` 而 `min(0.0, -0.0)` 得 `-0`，经 `1 / x` 能翻转符号。现在都按 IEEE 754-2019 的 minimum/maximum，-0 比 0 小；`arg_min`/`arg_max`、`min_by`/`max_by` 同理，下标与 `min`/`max` 的值指向同一个元素。
- **语言自己的 Go 类型不会按别的类型的内存读写**：宿主边界读写 `money.Money`、`time.Time` 等语言自己的 Go 类型时，`Currency` 原来写在默认分支里，将来新增一种却忘了补分支，就会按 `Currency` 的布局读写另一种类型；现在每种一个分支，其余是错误。
- 正则的模式缓存满了以后不再计数，计数器不会绕回成负数、让缓存超过 4096 个上限。


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
- `FxRate.Chain` 串回起点：乘积恰为 1 仍是同币种汇率，带点差的是 `ErrArithmetic`（原来一律是 1）。
- `ParseRounding` 的错误归入 `ErrArithmetic`，与 `ParseAllocation` 一致，文案多了 `arithmetic failed: ` 前缀。
- `ConstArgs` 的实参是宿主函数调用时从能编译变为编译错误。
- `let(n = len(xs), range(n))`、`let(n = len(fees), allocate(m, n))` 这类 let 别名从编译错误变为合法程序。
- 上面那些编译错误新增位置与 `type error:` 前缀。
- `funroute fmt` 与 `Format` 输出 `a -> (-1)`、`{(true): …}`。
- 目录与签名清单里 `sort_by`、`top_k`、`min_by`、`arg_max` 等的 `description` 写明了遇 NaN 的顺序。
- `Currency{}` 的 `MarshalJSON` 报 `ErrCurrency`（与零值 `FxRate` 一样），原来写出读不回的 `""`。
- `Manifest.Apply` 遇到注册表里已有、但 `Fold`、`BoundedArgs`、`ConstArgs` 或内核身份与清单不同的函数时报错，原来静默保留注册表的。
- `round` 拿到的值里没有落到最小单位之间的结果是编译错误，按值的去向判断：`round(if(amount * 2.9% > cap, amount, cap), @up)` 里的乘比例只拿去比较，原来能编译，现在报 `nothing inside round(…)`。
- `RunInto` 的说明收窄为"不在程序读到的参数内存上建结果"：`out` 与程序不读的参数共用内存时照常写入，这是调用者的选择。
- `time("…")` 与边界上的 RFC 3339 文本不再接受逗号作小数点、超过 9 位的小数（原来静默截到纳秒），是 `ErrDomain`/`ErrContract`。
- 函数名、句柄名与枚举名的点号两边都要有名字：`f..g(x)`、`f.(x)` 是语法错误，ExprJSON 与 `Register` 同样拒绝。
- ExprJSON 同一对象里重复的键是错误，原来取最后一个。
- 日历函数在夏令时切换处的答案见上。
- 含 0 与 -0 的 float 数组，`min`、`max`、`arg_min`、`arg_max`、`min_by`、`max_by` 的答案见上。
- `median`、`percentile` 读 float 时 -0 排在 0 前面：0 与 -0 并列在中位处时，原来得哪一个取决于排序的实现，现在确定。


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
- 金额的 `==`、`!=`、`in`、`index_of` 与 `dict<money>` 的相等不再分配（500 笔金额原来 2–3 次）；容器的 `Equal` 不再为比较类型新建 `Type`，字典的相等不再排序键。
- 编译时找自由变量从平方变为线性：4 万个不同的自由变量从 826 ms 到 4.4 ms。
- `unique`、`intersect`、`except` 与金额键的 `sort_by` 等不再为检查币种新建数组类型，各少一次分配。
- `bool`、`string`、金额与 record 值的字典推导式只建一份 map（10 项的 `dict<bool>` 从 8 次分配到 4 次）；字典字面量走同一个构造器。
- 批处理按合批的调用点保存答案，不再按程序的全部调用：20 个宿主调用、1 个合批模型的 64 条请求从每批 131 KB 到 30 KB。
- `using` 记住最近一次找到的货币对：150 个报价里循环换 500 笔从 430 µs 到 71 µs，与报价数基本无关。
- `unique`、`intersect`、`except` 对数组的数组、含数组的 record 也按键去重：4000 个 `[i, i + 1]` 的 `unique` 从 61 ms 到 0.39 ms。
- `min`、`max`、`abs` 与 arena 数组的取下标直接写进 let 的寄存器，少一条 `move`。
- 折叠时检查未走分支只求值一次每个闭合部分：800 层能整体折叠的 `if` 链从 0.38 s 到 0.05 s（原来随深度平方增长）。perf 报告新增这一行。
- 等权 `allocate(m, n)`/`Split` 直接算商与余数：10000 份从 182 µs、22 次分配到 61 µs、2 次，与按权重分逐份相同。
- 语法树按名字找位置时二分：1600 个同名字段读取从 17.3 ms 到 2.1 ms。
- `Value.Dict()`、转成 Go map、结果写 JSON 时不再先排序字典键；`windows`/`chunk` 预留结果容量；`std.Register` 不再复制整张币种表。
- `unique`/`intersect`/`except` 对汇率与含字典的 record 也按键去重：2000 个含字典的 record 从 175 ms 到 0.26 ms。
- 只含币种字段的 record 相等不再为金额检查建计划、分配。
- 契约参数按名字查找：4000 个参数的编译从 36.6 ms 到 7.2 ms；编译路径不再为判断是否声明金额而复制币种表。
- 金额数组的边界检查只在币种变化时查币种表：4096 笔同币种金额的 `len(xs)` 从 52 µs 到 6.2 µs，`windows` 结果里每个视图同样受益。
- 推断里两个开放变量相遇时，把被较少重载选择盯着的那个绑到另一个：参数在每次调用处都与新变量合并，原来每次都把链拉长、把它攒下的观察者再复制一遍。无契约的 `[a * 0 + b, …]` 1000 项从 35 ms 到 10 ms、4000 项从 430 ms 到 38 ms，有契约的不变。
- ExprJSON 按 token 流一次建出树，不再先校验整段再解码：导入快约 30%，内存少约 15%。
- `unique`、`intersect`、`except` 遇到含 NaN 的 record、数组或字典不再退回两两比较：NaN 不等于任何值，这样的项每次都有一个只属于它的键。4000 个一半重复的 record 中有一个 NaN 时，`unique` 从 649 ms 到 1.2 ms，`intersect(xs, xs)` 从 1.29 s 到 1.3 ms。time、duration 与币种字段也有了键，原来含它们的 record 同样两两比较。
- `median`、`percentile` 用三路划分的选择代替整体排序：20 万个 int 的 `median` 从 7.8 ms 到 1.1 ms、`percentile` 从 7.8 ms 到 0.5 ms；1000 个的 `percentile` 从 7.5 µs 到 4.2 µs，`median` 持平（+2.5%）。主元取 Tukey 的九点中值，划分超过两倍位长的深度时改为排序，任何输入都不退化成平方。
- `s[i]` 与 `len(s)` 有了专用指令，从同一次运行里上一次读到的位置接着走：顺着或倒着读一段文本每步一个字符，原来每次从头数码点，循环是文本长度的平方。8000 个字符的 `[s[i] for i in range(len(s))]` 从 16.8 ms 到 0.12 ms，`len(s)` 写在循环体里从 15.4 ms 到 0.07 ms；折叠与 `fallback` 候选里的调用仍走函数本身。帧交还时放开读过的文本。
- `Ratio` 加减去掉一次多余的约分；`let` 下的深层未走分支检查是线性的。


### 文档

- 新增 [`docs/semantics.md`](docs/semantics.md)（语义宪章）；README、language.md、money.md、go.md、contracts.md 改为引用它：首页的"强类型"改为"结构强类型"，"同一输入同一答案"限定为同一环境，Artifact 冻结的是签名绑定而不是行为，扩展函数的纯度是宿主的承诺。
- termination.md 的定理 B 改正：多项式的次数按语法归纳（循环相加、调用乘以函数的代价次数），原证明说内核函数都是线性的；新增内核与 std 的代价表。
- go.md 原来把 `any`/`all`/`first` 的融合写成"不改变结果的改写"，它们是短路形式（宪章 S4）。
- money.md 写明多跳换汇与 `FxRate.Chain` 只在起点与终点币种不同时相同；termination.md 写明 let 别名不改变有界性。
- 语义宪章 S7 写明装载期验证器保证的是安全（不崩溃、不挂起、类型正确），不重做编译器的其他证明，例如精确金额只在 `round` 里；go.md 说明宿主直接调的 `Value.Equal` 与规则里的 `==` 对两种币种的区别。
- go.md 写明 `Currencies.Parse` 的代码与数之间只认空格；`make perf` 先写临时文件，成功后才替换 `docs/perf.md`。


### 工程

- 行为金库只记录 JSON 写不出的那个值，不记录 `encoding/json` 随 Go 版本变化的错误文案，Go 1.26 与 1.27 下结果相同。
- `make perf` 新增金额与 record 的选择场景，以及路由规则常见形状（字段求和、按字段筛选、投影、`with`、封顶、按下标遍历）。
- 随机循环体的向量与标量逐项对照，随机程序生成器补上 bool 数组；伪造 artifact 的操作数变异 fuzz。
- ISO 4217 表的小数位写在代码表里（`JPY:0`），不再另列例外表；`internal/compile` 的循环编译拆到 `loop.go`。


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
