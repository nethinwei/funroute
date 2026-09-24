# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 规格（薄）

FunRoute：面向支付路由的强类型纯表达式语言。module `funroute`，Go 1.26，**零第三方依赖**，git 仓库，主分支 `main`。语法、类型与推导规则的完整说明在 `README.md`，改语义前先读它、改完同步它。

### 包边界（先看这条）

```text
lang/*.go                 唯一的公开接口：只有类型别名与转发，按宿主的用途分文件（lang.go 是包注释与值/类型，另有 registry/compile/runtime/batch/bind/manifest/errors）
lang/lsp/                 语言服务：LSP 协议层 + stdio 传输；在 lang/ 下，直接用 internal 的语义入口
extensions/std/           标准扩展包（聚合/字符串/数组/数值/选择/分组），只用公开 API
examples/payment/         示例宿主：领域函数的组装范例，也是工作台编进 wasm 的注册表
lang/internal/machine/    值、类型、字节码、VM、帧、注册表、目录、签名清单  ← 不依赖任何上层
lang/internal/syntax/     词法、语法、AST、ExprJSON、词法段、格式化、语法树  ← 只依赖 machine
lang/internal/compile/    推导、编译、常量折叠、契约、导出视图、Analyze   ← 依赖 syntax + machine
web/src/  web/wasm/       工作台前端（TS，按组件打包到 web/dist/）与浏览器用的语言服务入口（js/wasm）
```

依赖严格单向，`go list -deps` 可验证。**硬约束**：`Value` 的容器 backing 是私有字段，VM 的循环/索引/构造直接在上面操作，所以 value/container/convert/vm/frame 必须同包 —— 拆开就只能走公开 accessor。跨层要用 machine 的内部件时，导出一个**语义明确的入口**（`EvaluateClosed`、`IsLazyIf`、`Resolve`、`FormOf`），不要导出零件。

**公开面只有别名、构造、访问与选项**：`lang` 不导出任何宿主能写字段的数据类型 —— `Money`/`Rate`/`Currency`/`FxRate`/`Rates`/`Quote`、`Type`/`Field`、`Artifact`/`Parameter`、`Manifest`、`LanguageCatalog`/`FunctionDescriptor`/`FormDescriptor`、`PositionError` 的字段全部私有，只能经构造函数（币种表的 `Parse`/`Of`/`Minor`/`Currency`/`FxRate`/`Implied`/`NewRates`，`ParseRate`/`Percent`/`BasisPoints`，`RecordOf`/`FieldOf`/`MoneyOf`/`CurrencyOf`/`FxRateOf`…）、`Parse`/JSON 或注册表得到，经访问方法读，JSON 形状由各自的 `MarshalJSON` 固定。所以宿主造不出违反不变量的值，方法入口的检查（`FxRate.checked` 只挡零值，汇率规则与尺寸上限在唯一的构造入口 `newFxRate`）只是纵深防御；JSON 解码只查形状与汇率规则，币种是否已声明留给进入规则的边界。新增值类型必须同时给构造函数、访问方法与 JSON。machine 为 compile/syntax 导出的内部构造入口（`SealArtifact`、`NewParameter`、`ScalarType`、`FxRateLiteral`、`MoneyValue` 等）**不在 lang 转发**；因为 `lang.Registry` 是类型别名、它的方法宿主都能调，这类入口一律写成以 `*Registry` 为参数的包级函数（`FxRateLiteral`、`ParseMoneyAmount`、`DeclaredCurrency`、`RoundingVariant`），不写成 `Registry` 的方法。选项结构体（`CompileOptions`、`FunctionSpec`、`Doc`、`MoneySpec`、`CurrencySpec`、`QuoteSpec`、`BatchOptions`、`RunOptions`、`TextContract`）是"输入 + 使用处校验"，保留可写字段（`RunOptions` 的预取结果除外，它私有）。

AST 类型**故意不公开**：宿主通过 ExprJSON 交换程序。`lang` 的测试都是 `package lang_test`，只能用公开 API，所以它们同时守着这条边界；`lang_test.go` 开头的 `var ( _ lang.Value … )` 块把每个公开类型写一遍，少了哪个别名就编译不过。

### 值的容器是原生 Go 值，边界零转换零拷贝

`array<float>` 的 backing 就是 `[]float64`，`dict<int>` 就是 `map[string]int64`，只有容器的容器用 `[]Value`（`nestedArray`/`nestedDict`）。`machine/container.go` 是唯一知道这个映射的地方（`length`/`at`/`lookup`/`keys`/`tail`/`arrayBuilder`/`packDict`），`machine/convert.go` 的 `fromGo` 是唯一的 Go 类型清单：`ToValue`、`FromValue`、`Fn1/2/3` 的参数与结果转换、`Run(map)` 的 `coerce` 全走它。宿主 `[]float64` → `RunValues` → 扩展函数 → 返回，始终是同一个底层数组（`TestHostVectorsReachExtensionsWithoutCopying` 用指针相等断言，`BenchmarkVectorPassThrough` 证明耗时与长度无关）。

不变量在**值诞生处**确立、不在每次使用处复查：`CheckedFloat`（即公开的 `lang.Float`）、`ToValue`/`fromGo` 的 `checkFloats`、`Array`/`Dict`/`Record` 的 `validateInvariant` 挡住 NaN 与 Inf，所以 `RunValues` 拿到 `Value` 只校验类型，不再深度扫描 —— 那样做曾让 65536 元素的向量每次多花 16µs（`TestBoundaryKeepsTheInvariants` 是这条推理的守卫，`TestArgumentChecksDoNotAllocate` 守住零分配）。**唯一的例外是金额**：值自己带币种，参数里的金额容器在边界上做一次只读 O(n) 扫描（`machine/currency_check.go` 的 `bindUnits`：币种已声明、同一个 c 一致、建立本次运行的绑定；字典经 `eachEntry` 按 map 的顺序遍历、不排序键，出错时才按键序重扫一遍以得到稳定的报错；币种变量不多于 8 个（`maxCheckGroups`）时绑定在帧内嵌的数组里，更多时放在帧的 `unitsSpill` 里随帧复用），不分配、不拷贝 —— 值带币种、零拷贝、不扫描三者不可兼得。同理 `hasType` 对 record 直接比 `recordValue.typ`，不 `CloneType` 出一个 `Type` 来比。

类型化绑定（`lang.Bind[In, Out]`）把这条边界再往前推一步：`Bind` 由 `reflectType`/`structFields` 推出完整契约（类型清单仍只有那一份）；读写计划（`machine/plan.go` 的 `newCodecFor`）在 `Codec.Instantiate` 时**针对具体 artifact 声明的类型**生成，按名字投影 —— 参数与 record 字段按名字找、顺序以 artifact 为准、类型必须相同、多出来的忽略，Go string 可承载 enum（读入时查成员），与 `Run(map)` 边界同一套规则，所以控制台只声明读到的东西编出来的 artifact 也能 `Load`；`machine/access.go` 是**唯一**用 `unsafe` 的文件，按偏移做类型化读写，用 `switch` 分派而不是函数值（间接调用会让 `*In` 逃逸），必须反射的两处（句柄、非原生 map）先经 `noescape` 把字段拷出来。`Program.Run` 只转换字节码里 `OpLoadArg` 读到的参数。`TestProgramScalarsDoNotAllocate` 守住纯标量 0 分配（也就是 `*In` 不逃逸），`BenchmarkRecordBoundary` 是 record 边界的对照。不匹配在 `Instantiate` 时报 `ErrContract`；解码失败返回零值而不是写了一半的 `Out`。`reflect.go` 的转换同样认命名键类型（`map[Code]T` 的键要 `Convert`）和 enum。批处理：同步的 `Program.RunBatch`（`[]In → []Out`）与 `RunBatchInto`（`[]In → []*Out`，原地写回）都只是 `RunBatchFunc`（按下标取 `*In`/`*Out` 的访问器）的包装，规则是一个下标同时指请求、结果与失败，失败统一按下标顺序回调 `failed`（宿主代码，panic 不 recover）；同步批经 `Batch.executeShared`（引擎调用直接用宿主的 ctx），并发的 `Program.Batch` 经 `Batch.execute`（取批内最早 deadline），两者共用 `executeUnder`（请求带 `done` 通道就发送，不带就把结果留在原地）；codec 编出来的参数没读的槽位是空的，所以标记 `typed` 走 `Runtime.runTyped`，不再过 `RunValues` 的逐个类型检查。

代价是**不可变靠约定**：交给 `Value` 的切片/映射与从 `Value` 取出的（`Array()`/`Dict()`/`Any()`/`FromValue`）从那一刻起只读，VM 自身从不原地修改。任何新增的取值入口都要保持"交出 backing，不复制"并在注释里说明只读。边界上不许分配：`fromGo` 失败返回哨兵 `errUnsupportedGoType`，标量在 `ToValue`/`FromValue` 里经 `any(&x)` 指针匹配，不装箱。

### 金额：按类型区分，没有模式

`Registry.DeclareMoney(MoneySpec{Rounding, Currencies})` 之后才有四个 kind：`MoneyKind`（`Type.Name` 是单位：大写币种代码、小写契约币种变量、或 `""` 运行时才知道——类型文本一律写出单位，`""` 写作 `money<?>`/`currency<?>`/`fxrate<?,?>`，裸 `money` 是解析错误并提示这种写法，打印也总带 `<…>`，见 `type_parse.go` 的 `parseUnits` 与 `type.go` 的 `unitString`）、`RateKind`（int64 定点 1e-10）、`FxRateKind`（`Values` 是有序的两个单位，1 个 `Values[0]` 换多少 `Values[1]`）、`CurrencyKind`。没声明的注册表不接受任何金额类型（`Registry.admitsMoney`、`compile/compile_money.go` 的 `validateMoneyContract`），不用金额的程序 digest 逐字节不变（`TestMoneyCapabilityKeepsDigests`）；用到金额的 artifact 带金额戳 `MoneyStamp`（`machine/money_stamp.go`：默认舍入 + 字节码里写死的每个币种及其小数位，由 `moneyCodes` 从类型、常量与检查指令里收集；常量折叠抹掉的币种不进戳——折出来的值已经算好，不随注册表变），`Instantiate` 逐项比对（`checkMoneyStamp`：舍入相同、戳里每个币种仍声明且小数位相同），运行时才知道的币种跟随当前表，所以注册表新增币种不会让已部署的 artifact 失效。币种表另有只含币种与小数位的 `identity`（不含舍入），汇率表与注册表按它比对。

- **值带币种**：`Value.s` 存币种（金额的 `""` 是不带币种的零，它与任何币种相加比较都成立）；汇率的 `s` 是基准币种、`box` 是整个 `FxRate`（两边币种 + `*big.Rat`，创建后不变、副本共享）。`hasType` 对容器与记录按 `SameShape`（忽略单位）比较；币种是检查的事，不是类型测试的事。`array<money<?>>` 的 backing 是 `[]Money`，与宿主零拷贝。
- **币种是一等字面量，不是枚举**：`machine.IsCurrencyCode`（大写字母开头、共 3–8 位大写字母或数字）是唯一的形状判定；parser 把这种形状的裸名字读成 `CurrencyExpr`，所以 `IsValidVariableName` 拒绝它（变量、局部名、参数都不能叫 `USD`/`URL`），字段名（`IsValidFieldName`）与枚举成员不受影响。`150 JPY / USD` 是一个 `FxRateExpr`（`syntax/parse_literals.go` 的 `startsFxRate`/`fxRateAfter`：数、横向空白、代码、`/`、代码，各部分之间查原文只许空格与 Tab），由 `machine.FxRateLiteral` 精确读入。
- **推导里的单位**（`compile/infer_unit.go`）：每个单位位置（金额一个、汇率两个）是一个单位变量，信息（`known`/`dyn`）记在并查集根上；合一**从不因币种失败**，两个已知币种相遇只是变成 `dyn`（汇合处正确）。"运算处已知币种冲突即编译错误"在 `selectOverloads` 里由 `unitConflict` 看合并**之前**的操作数判定。契约参数每次读取用 `concrete(hint)` 得到新的单位类，一处的合并不污染别处。受约束字面量（`compile/infer_literal.go`）：小数 `{float, rate}`、`0` 是 `{int, money<?>, rate}`，`allowed` 记根上还能取的交集，`written` 记每个字面量写成的类型；`settleLiterals` 最后把没被上下文定下的根落成字面量写成的类型，同一个根上写成不同类型的字面量（`if(b, 0, 0.5)`）是类型错误而不是悄悄选一个（`writtenKind`，`forkLiteral` 同一规则），读成 rate 或金额的字面量每个吃一份 `literalPenalty`（大到压过所有混合数值惩罚），所以 `risk < 0.5` 仍是 float，声明金额前后不写金额的程序类型与 digest 完全一样。冲突的判定是 `conflicting`：两个不同代码、或一个代码碰到契约变量才算冲突；两个不同的契约变量（`c` 与 `d`）不算，它们常常就是同一币种，留给运行时比对（宿主函数处的 `CheckGroup`、内核的 `meet`、结果处的 `CheckPattern`）。`money / money` 两种读法：证明同币种的是 rate，证明异币种的是 fxrate，都未知时 fxrate 吃 `fxPenalty` 所以 rate 优先；操作数是两个不同的契约变量时（`presumedDistinct`）惩罚改加在 rate 读法上，`money<c> / money<d>` 仍读成 `fxrate<d,c>`；契约结果或 `using`（`compile/infer_fx.go` 的 `asExchangeRate`）要汇率时取 fxrate。比较 `eq(T, T)` 与 `member(T, array<T>)`（`comparesOperands`）的两边在 `selectOverloads` 里另由 `sharedVariableConflict` 逐位置比对金额与汇率的已知币种（`variableTerms` 沿参数类型进到数组与字典的元素）（`currency` 值不比，比较它就是在问是否同币种；`if(bool, T, T)` 是汇合处也不比），`switch` 的分支值与主语走 `unifyMatch` 同一条规则。记录作为整体合一的只是**形状**（`SameShape`）：记录项的 `units` 是它每个币种位置的单位类（`compile/infer_record.go`，顺序由 `recordUnitNames` 定义），合一逐位置合并、字段读取接回同一个类（`fieldTerm`）。
- **比例只为金额服务**：内核没有任何 int 与 rate 的混合重载，也没有 `rate(float)`/`rate(int)`/`float(rate)`（`rate(string)` 保留）；`sub(int, rate)` 这类失败由编译错误指向 `100% - fee`。
- **汇率的运算只有加点与比较**：fxrate 能被传递、进容器与记录、乘比例（`mul(fxrate<a,b>, rate)` 与反过来的一个，`FxRate.MulRate`，精确、同一货币对，换汇时只舍入一次）、同一货币对之间比较（`lt`/`le`/`gt`/`ge` 与 `==`；不同货币对是编译错误或 `ErrCurrency`）、交给 `using`。没有 money×fxrate、汇率相乘、交叉、倒数、平均 —— 换汇只有 `->`。`fx(currency<a>, currency<b>) -> fxrate<a,b>` 读运行时汇率表里的汇率（`rateIn`，与 `convert` 同一个单跳查找，`readsRun` 不折叠）。
- **换汇**：`amount -> JPY` 在 parser 脱糖为内核函数 `convert(money<a>, currency<b>) -> money<b>`（`syntax/operators.go` 一行，优先级 5：比 `+ -` 松、比比较紧），另注册末尾带舍入枚举的变体（`machine/builtins_money_convert.go`）。`convert` 带 `readsRun`，`IsConstexpr` 因此为假，**不折叠**（它读运行时的汇率表）。汇率表 `Rates`（`machine/rates.go`）：图的每个版本不可变，`Add` 写时复制后原子发布，一次运行在开始时（`Runtime.withRates`，按币种表 `identity` 比对，不一致是 `ErrCurrency`）把当时的版本放进 ctx，具名表各取一个版本（`runRates.names`/`tables`）；换汇是**单跳**（`rateGraph.hop`）：只用这一对货币的边——宿主给的报价，或反方向报价的倒数（`with` 只在反方向没被报价时写入倒数边，所以两个方向都报了价时各用各的）——从不经第三种货币搜索路径；要经过别的货币由规则连写 `amount -> CNY -> USD`（`->` 左结合，每一跳是一次独立的 `convert`，各舍入一次），Go 侧的 `Rates.Convert`/`Rates.Rate` 与 `fx` 同一规则，宿主要精确的交叉汇率用 `FxRate.Chain` 自己乘。换算系数按版本缓存（`factors`）。没有表或表里没有这一对是 `ErrNoRate`：数据暂不可得，`catchFallback` 接它（`convert` 是内核函数，不经 `classify`）。
- **`using`**（`syntax/ast.go` 的 `UsingExpr`：`Table`、`Quotes`、`Body`；保留字，目录只在声明金额时列出 `usingForm`）：编译成 `OpFxPush`（`B` 个报价、`Keys` 至多一个具名表，`A`/`C` 必须为 0）…主体…`OpFxPop`（`machine/fx_scope.go`）。`Table` 是写在最前的 `@name`：契约 `CompileOptions.RateTables`（文本契约 `tables`，进 digest，artifact 记在 `RateTables`）声明的具名汇率表，它们构成契约枚举命名空间里的 `rate_table`（`addTableEnum`；与 `rounding`、`allocation` 同列 `isRegistryEnum`，契约不能用这三个名字），推导要求表引用解析到它（`inferRateTable`）；有表时以宿主经 `RunOptions.RateTables` 传入的那张为底（没传就是空表，`runRates.named`），未声明的表名装载时拒绝（`validateFxPush`）。**`using` 永远隔离**：没有 `...`、没有"补充"与"覆盖外层"，主体只用写出的报价（有具名表时盖在那张表上），运行时的表与外层 `using` 都不参与——规则用哪条汇率看 `using` 本身就知道；要沿用外层的汇率就写 `fx(base, quote)`，报价在 push 之前求值，所以读的是外层。parser 遇到 `using` 里的 `...` 报错并给出这种写法。报价全是常量的 `using` 在装载时建好表（`constantScopes`，按 pc 存 `Runtime.fxScopes`，运行时核对弹出的正是那些常量才复用）。push 建新表（同一货币对后写的赢，两个方向一起换）并换掉 `frame.ctx`，旧 ctx 压进 `frame.scopes`；`catchFallback` 按 handler 记下的 `scopes` 深度恢复，所以备选从 `fallback` 所在处的汇率重新开始。
- **汇率的规则**（`machine/fxrate.go`）：值是精确有理数（`*big.Rat`），JSON `{"base","quote","rate"}`，rate 是有限小数（至多 `maxRateDigits` 位小数）就写小数、否则 `p/q`（`rateText`/`parseRateText`）；每边至多 `maxRateBits`（约一千位数字，`boundedRate`），所有产生汇率的路径都经 `newFxRate` 检查它，所以每个汇率都能 JSON 往返，宿主手写的报价与字面量仍限 `maxQuoteDigits`（40 位）；必须为正，同一币种之间恒为 1（`validRate`：`1 USD / USD` 合法、`150 USD / USD` 编译错误；运行时同币种金额相除只有相等才得 1，否则 `ErrArithmetic`）。边界（`scanUnits` 管 `Run`/`RunValues`/`Bind`，`declaredUnits` 管宿主函数结果）拒绝违反者；`FxRate.checked` 在每个 Go 方法入口挡住零值。`minor(...)` 给的是普通整数，拿它造回金额（`money(minor(a), JPY)`）在语言里合法，币种由规则作者负责。
- **检查分两层**：内核金额运算在求值时自己比对币种（纵深防御，`meet`）；编译器只给**非内核**函数发 `OpCurrencyCheck`（签名里同一单位变量出现多次或写死代码、且操作数未被证明时），以及契约结果声明了已知币种而表达式没证明时在末尾发一条（`compile/currency_checks.go`）。宿主函数的结果类型写明了币种（代码，或参数绑定的契约变量）时，`frame.call` 的 `hostResult` 按调用点的类型逐层 `checkPattern`（容器、记录里的也查；是否需要查在装载时按 pc 算进 `moneyPlan.results`），内核结果不走这条。运行时不一致是 `ErrCurrency`；`classify` 原样放行、`catchFallback` 排除它。
- **舍入**：内核里会舍入的运算（乘除比例、`convert`、`prorate`、`round_to`）都注册两次，第二个末尾多一个 `rounding` 枚举参数（`registerRounded`）；`round(expr, @mode)` 是编译期作用域（`RegisteredFunction.IsRoundingScope`），作用域里的舍入步骤改选变体（`machine.RoundingVariant`），每层只数自己的步骤（`roundSteps` 进入时清零、退出时恢复），内部没有舍入步骤是编译错误。`@half_up` 解析在注册表提供的 `rounding` 枚举里，`@all_last` 在 `allocation` 枚举里（`compile/compile_money.go` 的 `addRegistryEnums`），这两个与契约派生的 `rate_table` 是"枚举只从契约进入程序"的例外。
- **分摊**（`machine/money_allocate.go`）：`allocate` 各份向零取整（`mulDivParts` 同时给出余数），剩下的最小单位按 `AllocationStrategy` 发给权重为正的份：缺省最大余数法（余数、权重、下标），另有 `largest_weight`、`in_order`、`reverse_order`、`all_first`、`all_last`；负金额按绝对值算好再取负。`prorate(m, part, whole)` 与 `round_to(m, step)` 是 `Money.Prorate`/`ProrateBy`/`RoundTo`。
- **内核金额运算就是 Go 方法**：`Money`/`Rate`/`FxRate` 的方法（`machine/money_ops.go`、`fxrate.go`）、币种表 `Currencies`（`machine/currencies.go` 的 `Parse`/`Of`/`Minor`/`Currency`/`Format`，`machine/fxrate.go` 的 `FxRate`/`Implied`/`Convert`，`machine/rates.go` 的 `NewRates`；换汇按两币种小数位差缩放，小数位限定 0–8）与 `Rates`（报价可带来源与时间：`AddQuote`/`Quote`，`machine/rates_quote.go`，只记录、不参与选路）；`builtins_money*.go` 与 std 的金额聚合只做 Value 与这些方法之间的转换。定点乘除的入口是 `machine/fixed.go` 的 `mulDivRound`（128 位积、七种舍入、溢出报错，不公开；余数由 `mulDivParts` 给出）与换算系数超出 int64 时的 `rates.go` 的 `bigMulDivRound`。改金额语义只改方法，宿主与规则因此永远同一个答案；新增语言里的金额能力时，先在 Go 方法上加、内核只调用它 —— 语言能做而 Go 做不到的运算就是缺口。

### 句柄与批处理：模型引擎的数据不进语言

`HandleKind` 是不透明宿主值：`Type.Name` 是宿主给的名字（`handle<onnx.tensor>`），`Value.s` 存名字、`box` 存 payload。语言对它只做一件事——传递：`compareEqual` 拒绝比较（`eq` 与 `switch` 共用），不能进容器字面量以外的任何运算，`Any()` 只给类型名。`Registry.handles` 把 Go 类型映射到句柄名（`DefineHandle[T]`），`Fn1/2/3`、`Model1/2` 的签名推导（`register.go` 的 `goType`）先查它再走 `fromGo`。**不要为张量设计值类型**：形状、dtype、批维度都归引擎，这里只有句柄。

批处理在 SDK 层（`machine/batch.go`），不在编译器：`PrefetchSites` 读字节码找可提升的调用，条件是参数全为 `OpLoadArg`/`OpConstant`、不在任何前向跳转或循环覆盖的区间内（`guardedInstructions`）。这三条保证提前算不改变可观察行为，`if` 的惰性对模型调用仍然成立。`Batch.execute` 对每个站点调一次 `EvalBatch`，结果按 pc 放进私有的 `RunOptions.prefetched`（宿主设不了：能设就能跳过真实调用），`frame.evaluate` 命中即取，仍扣 fuel。没有 `EvalBatch` 的函数与不可提升的调用退回逐条 `Eval`。

### 预算：ctx 贯通，边界检查，错误类型化

`Run`/`RunValues`/`Batch.Run` 首参数是 `ctx`，帧持有它；`frame.invoke` 在**每次扩展调用前**检查 `ctx.Err()`，纯计算不打断。`Doc.Timeout` 用 `context.WithTimeout` 给单次调用加上限（只在设置了才分配），`Doc.Detached` 走 `callDetached`（独立 goroutine + select，参数先拷贝一份因为栈视窗会被复用）。内核函数没有这两个设置，走 `invoke` 的短路径，所以纯表达式基准不受影响。所有失败在 `classify` 里包成 `ErrDeadline` 或 `ErrExtension`，fuel 耗尽是 `ErrFuel`；算术没有答案（溢出、除零、float 非有限、非法汇率、转换没有答案如 `int("x")`/`rate("abc")`）是 `ErrArithmetic`，币种不一致是 `ErrCurrency`，这两个是规则或数据的错，`catchFallback` 不接，扩展函数里发生也一样（内核的构造点是 `errDivisionByZero`/`overflowIn`/`errFixedOverflow`/`errConversion`）；换汇找不到汇率是 `ErrNoRate`，与扩展失败同属"数据暂不可得"，`catchFallback` 接它。已有类别的错误在 `classify`、`invokeBounded`、批处理的 `prefetch`/`invokeBatch` 四处都原样保留（`keepsIdentity`：`ErrDeadline`/`ErrExtension`/`ErrCurrency`/`ErrArithmetic`/`ErrNoRate`），时间预算耗尽后返回的也不改包成 `ErrDeadline`；批处理在把参数交给引擎之前先逐个 `Runtime.admit`，不合格的请求直接答复、不进这一批；新增错误路径必须选一个包，宿主只用 `errors.Is`。`Batch` 的引擎调用取批内最早 deadline（`earliestDeadline`），每个程序仍用自己的 ctx。

`Logic`/`Model` 是反射注册（`register.go` + `reflect.go`）：签名与两个方向的转换器在注册时解析一次，`intoGo` 对 backing 类型精确匹配的容器直接交出（零拷贝仍成立），其余逐层构造；调用走 `reflect.Call`，约 300 ns。不要为了省这点重新引入 `Fn1/Fn2/…` 这类按元数展开的泛型；要零开销就写 `FunctionSpec`。

### 语法节点只在一处定义

`syntax/ast.go` 的每个节点 struct 就是它的全部描述：`json:"..."` 是 ExprJSON 字段（`omitempty` = 可选），字段类型决定它在 walk 里的角色（`Expr`/`[]Expr`/`string`/`[]struct`/`bool` —— 最后一种是节点的模式开关如 `ForExpr.Flatten`，没有子节点也不绑定名字），`role:"var|fn|local|text"` 决定名字校验规则，`binds:"a,b"` 说明这个局部名在哪些字段里可见（`@rest` = 同一列表里后面的项），`min` 给导入器的列表下限。`syntax/walk.go` 用反射把 tag 读成 plan，导入、导出、`FreeVariables`、`Children`、`FormOf`、作用域查询 `ScopeAt`、语法树 `SyntaxTree` 全部由 plan 驱动，**不按节点类型分派**。节点自己的规则（重名、重复键）写在 `check()` 里，parser 的 `p.node()` 与导入器的 `finish()` 都调它 —— 源码和 JSON 一套规则。反射只在编译期跑，不在执行路径上。

语言事实都从语言本身长出来，不为任何前端另写一份：词法器与解析器在消费 token 时就做了判断（关键字、局部名定义、函数/形式名、变量、字段……），`Lexemes` 只是**把这些判断留下来**（`parser.mark`，`Parse` 不记录所以零开销），程序能解析时再按 `binds` 规则把变量细分成参数与局部名引用。节点带 `Span`（匿名嵌入、无 tag，ExprJSON 与 digest 看不见），`parsePrimary`/后缀/运算符处统一 `stamp`。**Go 不输出颜色、布局、控件、文案**——那是客户端的事。

### 工作台：文本是唯一来源，结构视图是投影

语言服务的每个请求与通知都经 `Server.guarded`：持锁运行处理函数，处理函数的 panic（语言自身的 bug，不是客户端的错）只让这一条消息失败，请求回 `-32603 InternalError`，语言服务与其他打开的文档不受影响。`Lexemes` 的 `pieces` 保证每一步至少前进一个字节，`readsOf` 只细化 parser 在名字 token（`Pos`，不是可能从括号开始的 `Span.Start`）上留下的标记。前端只通过 LSP（`lang/lsp`）拿语言事实：语义标记、诊断、补全、悬停、签名提示、格式化，加 `funroute/setContract`（通知）、`funroute/syntaxTree`、`funroute/arguments`、`funroute/catalog`（请求）与 `funroute.run`/`funroute.render`（命令）。结构视图按 `funroute/syntaxTree` 画：`switch`/`for`/`reduce`/`let`/`using` 与惰性调用（`if`/`fallback`，由目录的 `special` 字段识别，不写死）是卡片，其余是一行源码；**每处修改都是对原文某个区间的替换**，下一棵树由服务端给出，前端从不理解 ExprJSON。代码与结构是表达式区块里的两个页签（`web/src/views.ts` 的 `switchRefusal`：服务端检查过当前文本且没有错误才允许切换）；结构视图从不打印程序，所以切回代码不会脱糖。包块的片段（`web/src/projection.ts` 的 `BLOCKS`）是前端的输入辅助，不是语言规则。语言服务同一份代码两种传输：`funroute lsp`（stdio，`Content-Length` 分帧）与 `web/wasm`（Worker 里 `send`/回调；`js.FuncOf` 回调里不能阻塞，所以入站走保序队列）。

### 契约在宿主，不在语言里

程序文本**只是表达式**。参数名、类型、顺序、说明、返回类型由宿主通过 `CompileOptions{Args []ArgSpec, Result *Type, ResultDoc}` 传入。理由：控制台本来就存规则元数据（版本、生效窗口、审批人、灰度），参数类型是同类信息，放语言里就是两份平行元数据（权衡见 README「契约」章节）。

- `Args` 非空时，**顺序即 ABI**；为空则按自由变量首次出现顺序推导。
- 声明了可以不用（调用方 ABI 稳定），用了必须声明（`CompileOptions.validate`）。
- `Result` 参与 unify 而非事后比对，所以它能定死 `[]` 的元素类型、能在重载里选签名。
- `Doc`/`ResultDoc` 是唯一不进 digest 的东西（`ArtifactDigest` 清它们）。改文案不该让已部署的 artifact 失效。
- 类型别名只是**文本契约的拼写**：`ParseTypeWith(text, aliases)` 在解析时就地展开，`CompileOptions` 上没有别名字段，所以编译器与 digest 根本看不见它。别名不嵌套（一个声明不能引用另一个），入口两处：CLI `-alias` 与文本契约 `compile.TextContract` 的 `types`（LSP 的 `funroute/setContract` 与工作台契约面板都走它）。正因为展开在前，artifact 与运行结果只认识完整的 record —— 复杂类型在契约里**一律声明成别名**：参数与返回处只写名字，完整结构写在类型声明里（类型文本接受换行），例子数据也按这条组织。
- 具名汇率表也是契约：`CompileOptions.RateTables`（文本契约的 `tables`，CLI 的 `-tables`）是表名清单，进 digest、记在 artifact 的 `RateTables`；宿主运行时按名字经 `RunOptions.RateTables` 传表，传了未声明的名字是 `ErrContract`，声明了没传的是空表。
- 函数名可以带点（命名空间与版本：`route.score_v1`），变量名不能带点（`.` 是字段访问），也不能是币种代码的形状（`IsValidVariableName` 调 `IsCurrencyCode`：`USD`/`URL` 在表达式里就是币种字面量）；字段名与枚举成员不受这条限制。

### 管线（单向，每步产物不可变）

```text
Parse / ImportExprJSON → finish(check) → Expr AST → inferProgram → 常量折叠 → Artifact(+digest) → Instantiate → Run/RunValues → Value
syntax/parser.go syntax/json_ast.go  syntax/walk.go   compile/infer*.go  compile/fold.go  compile/compiler.go  machine/vm.go  machine/frame.go
```

能力边界由**注册表**决定：`CoreRegistry()` 是极简内核（19 个函数名，其中 `if`/`fallback` 惰性；声明金额后再加 `allocate`/`convert`/`currency`/`currency_of`/`fx`/`like`/`minor`/`money`/`prorate`/`rate`/`round`/`round_to`/`sign` 等），`registry.EnableForm(SwitchForm/ForForm/ReduceForm)` 逐个打开（`compile/forms.go` 校验）。一个注册表 = 一个控制台。

**聚合不在语言里**：`sum`/`min`/`max`/`any`/`all` 是 `extensions/std` 注册的普通函数（计数用内核的 `len`），`reduce` 留给自定义折叠。日常写法是"推导式映射 + 聚合函数"；`reduce` 的 `if` 子句负责折叠前的筛选，累加器写作 `acc = init`（与 `let` 同形）。

**语言刻意不图灵完备**：没有无界循环，所有形式只遍历有限输入，每个程序都终止 —— `Fuel` 是成本上限而非安全兜底。想加回无界循环前先读 `docs/termination.md` 的附录。

### 必须守住的不变量

- `machine/registry.go` 的 Registry 是唯一类型权威；函数身份是**完整签名** `name(参数)->结果`，同名不同签名即重载。名字的形状与保留字只有一个权威（`IsValidFunctionName`/`IsValidVariableName`/`IsValidFieldName`/`IsReservedName`/`IsCurrencyCode`），parser、导入器与 registry 都调它。
- Artifact 冻结签名与 fuel 成本；`Instantiate` 校验版本、digest、签名存在性、cost 未变与每条指令。装载时**不再重新解析 ExprJSON** —— 执行只依赖字节码，digest 已经保护了 ExprJSON。
- digest = 清空 `Digest`、`Args[i].Doc`、`ResultDoc` 后 JSON 序列化再 sha256 ⇒ 改动既有字段、字段顺序或 JSON tag 都会让旧 Artifact 失效。ExprJSON 的字段顺序就是节点 struct 的字段顺序，所以**给节点 struct 重排字段也算改 digest**。`ArtifactVersion`/`ExprJSONVersion`/`CatalogVersion` 只标识**当前**形状，不是历史计数：还没有对外承诺兼容，形状要改就直接改，不必为迁移留台阶；等到有已部署的 artifact 时再让它们递增。
- **opcode 的一切在一张表里**（`machine/opcode.go`）：名字、栈效应、校验规则，按 opcode 索引所以顺序不可能错位。加 opcode = 表里加一行 + `frame.step` 加一个 case，`TestEveryOpcodeIsExecutableAndNamed` 会抓住漏掉的那一半。
- **常量折叠**（`compile/fold.go`）：不读参数也不读循环变量、且**每个调用都是 constexpr** 的子表达式在编译期用真 VM 跑掉（`EvaluateClosed`）。`Doc.Constexpr` 是这个授权（内核函数天然有，宿主函数不写就没有）—— 没有它，折叠会在编译规则时把推理引擎、时钟或远程服务调进去，而"3 点钟编译出来不一样"的规则比多算一点更糟。折叠是传递的（`let(a = 250, a * 4)` 整个折掉），折成常量的绑定**不占局部槽**，容器与记录也进常量池（`ConstantFromValue` 递归）。**闭合表达式的失败是编译错误**：只用内核函数且不读参数的东西每次算都一样，所以 `1 / 0`、`[1,2][5]`、`9223372036854775807 + 1` 编译期就报，即使写在惰性分支里（和 Go 对常量除零的处理一致）。两个例外：扩展函数的失败可能不在程序里（留作运行时的事），折叠预算 `ErrFuel` 耗尽只说明这一趟没算完。
- `Artifact.MaxStack` 由编译期栈效应累加得出，帧据此一次预留，`push` 在预留内跳过上限检查；算错只影响优化不影响正确性（`push` 的回退路径仍检查）。`release` 只清用过的部分，靠 `reserved` 与溢出水位。
- 类型推导是多候选分叉 + `implicitTypeScore` 打分选最优，同分报歧义；混合数值签名额外吃 `mixedPenalty`，所以 `risk < 0.5` 会把 `risk` 推成 float。
- 枚举是 nominal 且只从契约进入程序（例外是注册表声明金额时提供的 `rounding` 与 `allocation`，以及由契约的表名派生的 `rate_table`，契约不能再用这三个名字；币种不是枚举，是 `CurrencyExpr` 字面量）：`EnumExpr`（`@member` / `@enum.member`）的所属枚举由 `compile/enum.go` 的 `collectEnums`/`resolveEnumReference` 在**契约的枚举命名空间**里解析，不靠上下文类型；命名空间收的是契约类型里**任意深度**的枚举（元素、record 字段、字段的字段），走 `machine.WalkTypes`——「这个类型里有没有 X」只有这一个入口，手写的 `Elem` 递归会漏掉 record 字段，这正是它被建立的原因；`enum` 与 `string` 不 unify，编译成字符串常量，运行时值仍是成员名。
- **record 的字段顺序就是它的类型**：`RecordKind` 的 `Type.Fields` 有序，值（`recordValue`）按同一顺序紧凑存放，`FieldExpr` 在编译期解析成下标发 `OpField`，运行时不查名字。推导里 record 的**形状**作为整体 unify（`typeTerm.record`，`SameShape`），因为它出现的地方字段都已具体 —— 来自契约或来自字段值有类型的字面量；只有币种按位置走单位类（`typeTerm.units`）。边界按名字匹配：契约声明的字段**必须**都有（缺了就是另一个类型，没有 null 可以顶替，拼错的名字也落在这一侧），源数据多带的字段忽略（一个宿主对象服务多条规则），字段值按无损方向加宽（`1 → float` 可以，`1.7 → int` 不行），Go 与 JSON 两条路径同一套规则。**Go struct 就是 record**：`reflectType`/`intoGo`/`outOfGo`/`fromGo`/`FromValue` 都走 `machine/structs.go` 的一套映射 —— 导出字段按**声明顺序**（顺序即类型），**只有带 `funroute:"name"` tag 的导出字段在 record 里**，没有从 Go 名推断这回事（推断会让 Go 侧重命名悄悄改掉契约）；漏标不是静默的 —— 读它的表达式编译期就报 `has no field`。一个 tag 都没有的 struct 直接拒绝。字段名规则只有一条（`IsValidFieldName`），类型文本、源码与 ExprJSON 三个入口都用它。record 的 JSON 由 `Value.MarshalJSON` 按字段顺序写（`Any()` 交出的 Go map 不保序，要保序就 `json.Marshal(value)`）。字段更新 `r with {a: 1}` 是 `RecordUpdateExpr`（`compile/record_update.go`）：`with` 是保留字，parser 在 `parsePostfix` 里把它当后缀读（`parse_containers.go` 的 `parseWith`，与 `.field`、`[i]` 同一层，打印时基准经 `postfixBase` 加括号），语言里不再有 `...`；它**不能**在 parser 里脱糖，因为 `r` 有哪些字段要到推导后才知道；结果类型就是 `r` 的类型，只替换已有字段且新值与该字段同型（不加宽、不增删字段），编译成一条 `OpRecordWith`（`Instruction.Keys` 是被替换的字段名，按写出顺序，装载时由 `resolveUpdates` 解析成下标存进 `Runtime.updates`，运行时仍按下标写、不查名字；复制一次字段切片，共享类型）。嵌套更新靠嵌套写，没有路径语法。
- `SwitchExpr.Value` 可为 nil（条件形态），`SwitchCaseExpr.Match` 是列表（多值分支）。改这里只动 `syntax/ast.go`（tag 决定 JSON、作用域、语法树）加 `compile/infer_expr.go`、`compile/compiler.go` 的语义，以及 `syntax/print.go`/`format.go` 的打印。
- ExprJSON 文档只有 `{version, expr}`。前端不读也不写它：工作台只和文本打交道。
- 编译错误带位置：`syntax.At`（一个位置）、`syntax.Around(node, …)`（一个节点：指向它的位置、覆盖它的 `Span`）与 parser/lexer 内部的 `over`（一个 token）是**仅有**的产生方式，错误本身携带 byte offset 与区间（`PosError.Start/End`，LSP 诊断用它），`lang.LineColumn(err, source)` 由宿主换算成行列 —— 语言层不持有源码文本，ExprJSON 编译的程序根本没有文本。新增错误路径必须走它们，否则位置就丢了；推导里的错误一律 `Around(node)`，读了未声明参数也指向那次读取（`syntax.FirstReads`）；`compileError` 用两个 `%w` 包装，所以 `errors.Is` 找类别、`errors.As` 找位置都成立。
- 运算符与推导式的糖全部在 parser 层脱糖，**不为它们新增节点类型**（字面量节点 `money`/`rate`/`currency`/`fxrate` 与保留字引出的 `using` 是节点，不是糖）：`a+b` 就是 `add(a,b)`，`a&&b` 就是 `if(a,b,false)`，`[e for x in xs if c]` 就是 `ForExpr`。多层推导 `[e for x in xs for y in ys]` 也一样：`nestClauses` 把子句从内往外串成嵌套 `ForExpr`，除最内层外都置 `Flatten`，编译时发 `OpLoopSpread` 把内层产出的数组拼接进外层（`arrayBuilder.addAll`）。字典推导只接一个子句。新增糖要同时给打印器一条反向读法：运算符的反向读取是 `operators.go` 的 `operatorSpec.read`，与 parser 的 `expandOperator` 一一对应、放在同一张表旁边；`specificity` 决定一个节点有多种读法时选哪个（`sub(0,x)` 是 `-x` 不是 `0 - x`，`!=` 先于 `!`）。**格式化结果必须解析回同一 ExprJSON**，`print_test.go` 的往返测试与 `format_test.go` 的 `FuzzFormatRoundTrip` 模糊测试守着。打印器要替词法器着想：`postfixBase` 给数字和枚举成员加括号（`(1).x`、`(@a).b`，否则词法器会一路读进去），零参数调用没有 `args[1:]`（`operatorSpec.read` 先查元数再切片）。词法器把名字和其中的点读成一个 token，所以点号之后的 `a.b` 与 `order.a.b` 一样是连续的字段读取（`parseFieldRead` 按点拆开）；空白只认 ASCII（源码按字节读，单个 0x85/0xA0 字节不是空白）；金额字面量由几个 token 组成，parser 查原文判定（`parse_literals.go` 的 `startsMoney`）：形如币种代码的名字、至少一个空格或 Tab（不许换行与注释，`onlyHorizontalSpace`）、紧贴数字的可选负号、数（`USD -1.70`）；`USD-1`/`USD - 1` 是币种减 1（类型错误），`-USD 1.70` 只是一元负号作用在金额上；汇率字面量同一规则（`fxRateAfter`）；负数字面量也是 parser 的规则：一元 `-` 后是单独一个数（`numberAlone`：后面不是 `[`、也不是汇率字面量）才读成负数，所以 `-9223372036854775808` 写得出、`-150 JPY / USD` 与 `-2[0]` 是取负；小数字面量必须恰好是 float64 能表示的写法（`exactFloat`，按有效数字与十进制指数比较，不做大数运算），源码与 ExprJSON 导入同一规则；`if` 不能作变量名与局部名（`IsValidVariableName`；函数名与字段名可以，`{if: 1}` 由 `recordNamedFirst` 读成记录），所以推导式与 `reduce` 的 `if` 子句不靠上下文；`switch` 的 `else` 必须写 `=> 结果`；README 的文法是分层、token 互不重叠的，改语法时同步它；`%` 紧贴数字一律是比例的单位（`lexer.rateUnit` 不看后文，没有特例；带指数的数也查，`1e3%` 报错说明比例不带指数），比例后紧跟操作数由 `rateLiteral` 报错说明，取模必须在数字后留空格 —— 打印器的二元运算符两边本来就有空格；字符串字面量必须是合法 UTF-8 —— 原文里的无效字节会被 `strconv.Unquote` 悄悄换成 U+FFFD，`\200` 这类转义则会在 ExprJSON 里被换掉，两者都在 `parser.unquote` 拒绝。比较运算不结合、`->` 的右边是一个 postfix，都是运算符表的两列（`nonAssociative`、`postfixRight`）：parser 的 `climb` 经 `follows` 拒绝 `a < b < c` 与 `amount -> JPY + fee` 并说明写法，`rightOperand` 读 `->` 的右边，打印器按 `leftLevel`/`rightLevel` 加括号，`chainSource` 不把不结合的运算符排成链。运算符表（`syntax/operators.go`）支持三种 fixity：`infix`（符号 `%`，或单词 `in` —— 后者按 token 文本匹配，因为它的 kind 就是 identifier）、`prefix`、`index`（后缀 `xs[i]`，parser 在 primary 结束处读，前端按 fixity 打印回方括号）。
- `and`/`or`/`not`/`ne` 是**派生形式**——展开为 `if`，不进注册表。它们的说明在 `machine/catalog.go` 的 `alwaysForms`（目录的 `special_forms`，只有名字、可解析的写法与 Doc，没有伪签名），打印靠 `operatorSpec.read`；两处都不在前端。
- 扩展函数按不可信纯函数对待：`recover` 在激活层兜住 panic。**值不拷贝**——容器 backing 直接交出，安全性来自只读约定（见上），任何新增的包外取值入口都必须保持"交出 backing、文档写明只读"。
- 出栈返回的是栈上视窗，不是副本：`EvalFunc` 收到的 `[]Value` 只在调用期间有效。
- 没有递归就没有嵌套激活：每次 `Run` 只建一个帧（来自 `sync.Pool`）。新增任何能重入程序的构造都会推翻 `docs/termination.md` 的定理 A 与 B。
- `Kind`/`OpCode` 是 `uint8` 但 JSON 是名字：`Kind` 加值要同步 `kindNames`，`OpCode` 加值要加表行。`HandleKind` 排在 `VarKind` 之后、金额四个 kind 排在 `RecordKind` 之后，既有 kind 的序号不变。

### 改动同步点

- 新增 opcode → `machine/opcode.go` 表 + `machine/frame.go` 的 `step`。仅此两处，测试兜底。给 `Instruction` 加 `omitempty` 字段不改变既有 digest（零值不出现在 JSON），但**不要为一个 opcode 给 `Instruction` 加字段**：解释循环每一步都按值复制一条指令，多一个切片头（72 → 96 字节）就让所有程序慢了 7%，哪怕它们根本不用那个 opcode。操作数放进已有字段（`A`–`D`、`Keys`、`Type`），需要换算的在装载时算好放进 `Runtime`（`record_with` 的 `Runtime.updates` 是范例）。改了 `Instruction` 或 `frame.step` 就和改动前交替跑 `BenchmarkDispatch`/`BenchmarkCall` 对照。
- 新增惰性形式 → `machine/registry.go` 的 `knownForms` + 节点的 `Form()` 方法（`compile/forms.go` 靠它和 `Children` 通用校验）+ `machine/catalog.go` 的 `formDescriptors`。
- 新增**纯函数** → `Doc.Constexpr = true`，折叠就能在编译期算掉它（`std` 全包如此，`upper("adyen")`、`sum(range(4))` 都编译成一条载入指令）。模型、时钟、远程调用**不要**标。
- 新增**凭空造容器**的函数 → `Doc.BoundedArgs = true`，编译器的 `requireBoundedArgs` 要求每个**整数**实参的规模已被输入界定（只有整数能代表长度）：字面量、`len(容器)`、或两者的算术组合（`boundedCall` 认这几种）。这正是 `docs/termination.md` 定理 B 需要的条件 —— 要求实参是**常量**比它更强，会把 `range(len(fees))` 这种安全写法一起误伤。只给"结果规模由参数决定"的函数用（`range` 与内核的 `allocate(m, n)`），理由写在 `docs/termination.md` 定理 B 之后。
- 新增**读运行状态的内核函数**（像 `convert` 读汇率表）→ `FunctionSpec.readsRun = true`，`IsConstexpr` 就为假、折叠不碰它；运行状态经 ctx 传入（`ratesKey`），不给帧加字段。
- 新增**金额函数** → 签名写 `MoneyOf("u")` 这类单位变量（反射只能推出 `money`，币种会丢），会落到两个最小单位之间的运算同时注册末尾带 `RoundingEnumType()` 参数的变体，`round(…)` 才能选中它；非内核函数要自己检查容器里币种一致（`extensions/std/money.go` 的 `amountsOf`）。std 只在注册表已声明金额时注册金额重载，所以宿主**先 `DeclareMoney` 再注册 std**。
- 新增函数 → 只注册 `FunctionSpec`（或 `Logic`/`Model`），目录、LSP 的悬停与补全自动生效；要 ABI 版本就写进名字（`route.score_v1`）。`Doc` 是**唯一**的函数元数据结构：`FunctionSpec.Doc`、`Logic`/`Model` 的入参、目录 JSON 的 `doc` 字段、签名清单都是它，没有平行的 Display/Parameter/Result 结构。只写机器算不出来的（标签、说明、成本、参数标签）：签名来自反射，分类缺省取命名空间，顺序按名字。按 Go 签名注册用 `Logic`（反射读签名，任意元数与嵌套，首参数可选 `context.Context`），模型函数用 `Model` 同时给单条与批量实现；引擎类型先 `DefineHandle[T]`。一个名字要服务多种元素类型时（语言没有类型类，`int`/`float`/`string` 就是三次注册）走 `extensions/std` 的 `eachType`，并在 `TestNamesCoverEveryElementTypeTheyClaim` 加一行 —— 漏注册一个类型不会让任何东西失败，直到规则在生产里撞上那个类型。
- `Kind` 加值 → `kindNames` 同步；若它有运行时表示，`Value.hasType`/`Type()`/`Any()`、`compile/infer.go` 的 `typeTerm`（含 `name`）、`implicitTypeScore`、`ParseType`、`validateTypePattern` 都要认识它（`HandleKind` 是现成范例）。
- 新增 ExprJSON 节点 → 在 `syntax/ast.go` 定义带 tag 的 struct（含 `kind()`、嵌入 `Span`，需要时 `check()`/`Form()`）并加进 `nodeTypes`；导入、导出、作用域、语法树全部自动生效。仍要手写的是语义：`compile/infer_expr.go`、`compile/compiler.go` 的 case，`syntax/print.go` 的 `inline`/`compoundSource` 与 `format.go` 的 `splitNode`（打印器按节点分派，是语言自己的唯一一份），parser 的 `stamp` 覆盖。**按节点类型分派的另一处是 `compile/enum.go` 的 `validateConstrainedReturn`**（枚举出参要逐条返回路径证明）：漏了它不会不安全（default 走 `validateKnownType`，要求整体类型相等，是保守的），但能表达的程序会变少。`walk_test.go` 的 `NodeKinds` 测试会要求列出新节点，`lang/lsp/funroute_test.go` 会要求示例用到它。本轮的范例：`currency`/`fxrate` 是只有 `role:"text"` 字段的字面量节点（parser 在 `parser.go`/`parse_literals.go` 识别，`compile/compile_money.go` 经注册表读成常量）；`using` 带列表与 `bool` 开关（`Fills`/`Outer`/`Quotes`），语义在 `compile/infer_fx.go`、`compiler.go`（`OpFxPush`/`OpFxPop`），另有 `fold.go` 的 `constexprOnly` 与 `enum.go` 两处按节点分派。
- `reflectType`/`fromGo` 新增 Go 类型 → `machine/plan.go` 的 `newCodecFor` 分类与 `access.go` 的读写也要认识它，否则 `Bind` 能推出类型、`Program.Run` 却走错形状。
- 新增公开 API → 在 `lang/` 对应主题的文件里加别名或转发（新类型也进 `lang_test.go` 的类型断言块），并在对应的 `_test.go` 以宿主视角用一次；能不加就不加。数据类型遵守上面的公开面规则（字段私有、构造 + 访问 + JSON），不转发 machine 给 compile/syntax 用的内部构造入口。
- 新增例子 → 只改 `web/funroute-examples.json`（**源码 + 契约 + 入参 + 期望值**），`lang/lsp/funroute_test.go` 通过语言服务逐条运行，并要求示例合起来覆盖示例注册表的全部函数与形式、`syntax.Operators()` 与 `syntax.NodeKinds()`。
- 新增颜色 → 不要写字面量。`web/tokens.css` 是**唯一**的调色板（`styles.css` 只是工作台页面自己的布局），每个 token 用 `light-dark(浅, 深)` 同时给出两套值，主题切换只改 `color-scheme`（`data-theme` 缺省即跟随系统）。组件样式（Lit 的 `static styles`、编辑器的 `EditorView.theme`）只消费这些 token 且自带，不依赖 `styles.css`；语义标记的样式是 `.fr-tok-<LSP token type>`，写在 `editor.ts` 的主题里。
- 新增前端代码 → 写在 `web/src/*.ts`（`erasableSyntaxOnly`，`node --test` 直接跑 `*.test.ts`；纯逻辑放无 DOM 的模块里测），`make web` 用 esbuild 按组件多入口拆分打包到 `web/dist/`（`app`/`lsp`/`editor`/`contract`/`runner`/`canvas` + 共享 chunk，chunk 与入口同目录，所以 `lsp.ts` 里相对 `import.meta.url` 的 worker 路径仍然成立）；产物**不提交**（`.gitignore` 忽略整个 `web/dist/`），谁要服务工作台谁先构建：`make site` 依赖 `web` 与 `wasm`，Pages 工作流装 Node 后跑同一个目标。Go 侧的构建与测试不需要 Node。组件可按需单独引用：注册一律经 `ui.ts` 的 `define`（已注册就跳过），新组件照做并加进 `package.json` 的入口列表。`make check-web` 只做类型检查并确认能构建。补全顺序由服务端的 `sortText` 决定（类型→名称），`lsp.ts` 的 `rankedCompletion` 关掉 CodeMirror 的模糊打分以保住它。编辑器不用 `basicSetup`：`web/src/setup.ts` 把它拆开重组——行为向 VS Code 看齐，按键是 Emacs（`@replit/codemirror-emacs` 排在最前；它按 `event.code` 认标点，所以 `M-;`/`M-<`/`M->` 由 CodeMirror 的 keymap 补上），只绑定服务端真正回答的能力的键。组件共用 `web/src/ui.ts`（`define`、填充风格的 `fieldStyles`、`labelStyles`），代码字体是 `--mono` token。位置一律按 UTF-16 数（客户端不协商编码），`editor.ts` 的 `offsetAt` 是编辑器侧唯一的换算。结构视图的编辑事件带上它读取区间时的原文，编辑器文本已变就丢弃。契约的返回部分在试运行面板里编辑，`app.ts` 的 `sendContract` 把两个面板合成一份推给服务端。运行时依赖只有 Lit、实际导入的 `@codemirror/*` 子包（含 `lsp-client`）及其 Emacs 键位，`package.json` 按导入逐个声明，只在前端；Go 仍零依赖。前端**只**通过 LSP 拿语言事实，不理解 ExprJSON、不写任何语法规则。
- 新增语言服务能力 → `lang/lsp`：标准 LSP 方法优先，FunRoute 专有的用 `funroute/*` 请求/通知或 `workspace/executeCommand` 命令；结果只放语言事实与区间。浏览器入口 `web/wasm` 只是传输，`make vet-wasm` 在 js/wasm 下检查它。CSP 写在 `web/index.html` 的 `<meta>`（`'wasm-unsafe-eval'`，本地与 GitHub Pages 一致）。
- 宿主函数在语言服务里 → 用签名清单：`Registry.Manifest()` 导出，`Manifest.Apply(base)` 把缺实现的函数登记为签名（调用得 `ErrUnavailable`，同时是 `ErrExtension`）。改 `Doc` 里影响编译的字段（`Constexpr`、`BoundedArgs`）时同步 `machine/manifest.go` 的 `ManifestFunction`。部署用 artifact 由宿主用真实注册表编译。

## 命令

```bash
make ci                                                # 提交前必须全过：check-fmt check-imports check-js check-web vet vet-wasm staticcheck modernize lint build wasm test test-js
make staticcheck | make modernize | make check-imports # 严格检查：工具按固定版本装进 .tools/<版本>/（首次需联网），不进 go.mod
make test | make lint | make vet | make fmt
make wasm                                              # 浏览器用的语言服务 web/dist/funroute.wasm
make web                                               # 前端产物 web/dist/*.js（先在 web/ 里 npm install；两者都不提交）
make site                                              # 组装发布目录 site/（make run 与 Pages 共用）
make run                                               # web + site，然后 cmd/mvp 静态服务 site/ 于 http://127.0.0.1:8080
go test ./lang/internal/compile -run TestIfIsLazyAndFuelIsEnforced -v      # 单个测试
go test ./lang/internal/machine -bench . -benchtime 2000x                 # VM 基准（编译基准 BenchmarkCompile 在 compile）
go test ./lang/internal/machine -bench RunPaths -cpuprofile /tmp/cpu.out  # 热路径 profile
go test ./lang/internal/machine -bench Vector -benchtime 1s                # 向量透传：三个尺寸的 ns/op 必须相同（分配来自 Logic 的 reflect.Call，与 n 无关）
go test ./lang/internal/machine -bench BatchVersus -benchtime 2000x        # 批处理对比：合批摊薄引擎开销
go list -deps ./lang/internal/machine | grep funroute                     # 验证依赖方向
go run ./cmd/funroute inspect -expr 'if(a,b,add(1,1))'
go run ./cmd/funroute fmt -expr 'let(a=1,a+2)'                            # 格式化：结果必须解析回同一程序
go run ./cmd/funroute lsp -manifest registry.json                          # 语言服务（stdio）
go run ./cmd/funroute run -expr 'reduce(x in items, total = 0, total + x)' -args '{"items":[1,2,3]}'
go run ./cmd/funroute run -expr 'let(bps = 250, amount * bps / 10000)' \
  -types 'amount=int' -args '{"amount":100000}'          # 契约由 -types 给出
```

## 开发规范（`make lint` 强制，`tools/lint` 实现）

- 单个方法不超过 **50 行**（含签名与右大括号）。
- 嵌套不超过 **3 层**（if/for/switch/select/函数字面量各计一层，`else if` 不额外计）。
- 单个文件不超过 **800 行**（`.go`、`.js` 与 `.ts`；`dist`、`node_modules` 跳过）。
- 超限时拆函数或拆文件，不要放宽阈值；`tools/lint` 只用标准库，保持零依赖。

另外三项由固定版本的外部工具检查（Makefile 顶部的 `STATICCHECK_VERSION`/`X_TOOLS_VERSION`；工具不是依赖，`go.mod` 保持为空）：

- **staticcheck 全部检查**（`staticcheck.conf` 的 `checks = ["all"]`，含默认关闭的风格项）：每个包有包注释（internal 包写在 `doc.go`），导出标识符的注释以它的名字开头，声明里不写能推断出的类型。要断言"某个类型写得出名字"时，用包级的 `var _ T`（Go 的编译期断言惯用法），不用 `var v T = …`（ST1023）或显式类型参数（gopls 的 `infertypeargs` 会报多余）。确属有意的例外用 `//lint:ignore <检查> <原因>` 就地说明（`access.go` 的 `contentSink` 是范例），不在配置里整体关掉。
- **modernize**：当前 Go 有更直接写法的地方用新写法（`reflect.TypeFor`、`maps.Copy`、`slices.Sort`、`min`/`max`、`range n`、`errors.AsType` 等）。
- **import 分组**：标准库一组、空一行、本模块（`funroute/…`）一组，组内按字母序（`goimports -local funroute`）。
- `js/wasm` 入口 `web/wasm` 在 `GOOS=js GOARCH=wasm` 下另跑一遍 staticcheck 与 modernize，同 `vet-wasm`。

## 测试规范

`tools/lint` 在 `*_test.go` 里强制前四条（`tools/lint/tests.go`），其余靠评审：

- 测试的 ctx 用 `t.Context()`（基准 `b.Context()`），测试结束即取消；`context.Background()` 只在没有测试值的地方用（`Example*`）。
- 不睡：依赖定时器的测试（`Batch` 的 `MaxWait`、`Doc.Timeout`、`Detached`）跑在 `testing/synctest` 的假时钟上。
- 基准写 `for b.Loop()`，不在 `b.N` 上循环、不 `ResetTimer`：`b.Loop` 本身就把循环前的准备排除在计时外。
- 接收 `*testing.T/B/F` 或 `testing.TB` 的辅助函数第一句 `t.Helper()`，失败报在调用它的那一行。
- 表驱动测试每个用例一个 `t.Run` 子测试，名字取用例的键或 name，失败时看得出是哪一条，也能 `-run` 单跑。
- 相互独立的测试与子测试第一句 `t.Parallel()`。例外：`testing.AllocsPerRun`（全局计数，会被并行测试污染）、synctest 气泡、共享可变状态、对计时敏感的测试。
- 失败信息写出输入、实际与期望：`CompileExpr(%q) error = %v, want ErrContract`。
- **每个 `foo_test.go` 对应同目录的 `foo.go`，测的就是它的代码**（`tools/lint` 强制）。唯一例外是 Go 官方惯例 `example_test.go` 与 `export_test.go`。没有 `helpers_test.go`：共用辅助放进它主要服务的测试文件。归位按测试**真正测的代码**：测 machine 的行为而要先编译出 artifact 的测试，写成 `lang/internal/machine` 下的外部测试包 `package machine_test`（它可以 import compile），需要的内部件经 `export_test.go` 暴露。
- 公开包 `lang` 的用法有带 `// Output:` 的 `Example*`（`lang/example_test.go`），go test 校验输出，也是 godoc 上的示例。
- 解析与格式化有模糊测试（`lang/internal/syntax` 的 `format_test.go` 与 `json_ast_test.go`）：能解析的程序格式化后解析回同一 ExprJSON，任意字节导入 ExprJSON 不 panic。平时 `go test` 只跑种子；改语法后跑 `go test ./lang/internal/syntax -run XXX -fuzz FuzzFormatRoundTrip -fuzztime 60s`。
- `lint` 把函数字面量算一层嵌套：`for` + `t.Run(…, func…)` 已占两层，子测试体里只能再有一层，要更深就抽一个带 `t.Helper()` 的检查函数。
