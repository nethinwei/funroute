# 在 Go 中使用

在 Go 里编译、运行规则，注册扩展函数，接入模型，处理超时与错误，以及按控制台开放能力。

## 编译与运行

```go
registry := funroute.CoreRegistry()
registry.EnableForm(funroute.SwitchForm, funroute.ForForm, funroute.ReduceForm)
std.Register(registry)

// 文本或 ExprJSON → artifact
artifact, _ := funroute.CompileExpr(`if(a, b, add(1, 1))`, registry, funroute.CompileOptions{
    Args: []funroute.ArgSpec{
        {Name: "a", Type: funroute.BoolType},
        {Name: "b", Type: funroute.IntType},
    },
})

// artifact 绑定到注册表，签名不符会拒绝
runtime, _ := funroute.Instantiate(artifact, registry)

// 按名字传参（适合表单）
result, _ := runtime.Run(ctx, map[string]any{"a": false, "b": 9})

// 按顺序传参（热路径，省掉名字查找）
result, _ = runtime.RunValues(ctx, []funroute.Value{funroute.Bool(false), funroute.Int(9)})
```

整条管线：

```text
源码 ──ParseToJSON──→ ExprJSON
  └──────────┬──────────┘
      CompileExpr / CompileJSON
      类型推导（契约参与）→ 常量折叠 → 字节码 Artifact + digest
             │
        Instantiate（校验签名，拒绝漂移）
             │
      Run(map) / RunValues(slice) → Value
```

宿主只需要四个概念：

| | |
|---|---|
| `Registry` | 有哪些函数和形式，是类型的唯一权威 |
| `CompileOptions` | 契约：参数、类型、返回类型、说明 |
| `Artifact` | 不可变的字节码 + digest，可存储、可传输（JSON）；字段私有，经 `Args()`、`Result()`、`Digest()` 等读取 |
| `Runtime` | 绑定到注册表后可运行的 Artifact |

`funroute` 是唯一的公开包，实现都在 `internal/` 下，改动不会波及宿主。AST 类型刻意不公开，程序一律用 ExprJSON 交换。公开包里的数据类型（`Type`、`Artifact`、`Money`、`FxRate`、`Manifest`、目录……）都没有宿主可写的字段，只能经构造函数、`Parse` 或注册表得到，经访问方法读取，所以宿主拿不到一个不合规则的值；要宿主填写的只有选项结构体（`CompileOptions`、`FunctionSpec`、`Doc`、`MoneySpec`、`BatchOptions` 等），它们在使用处校验。

## 类型化绑定：契约就是两个 Go 类型

宿主本来就有请求 struct 和结果 struct，契约可以直接从它们读出来。`funroute.Bind[In, Out]` 反射**一次**：`In` 里带 `funroute:"name"` tag 的导出字段是参数，**声明顺序即 ABI**（规则与[记录](language.md#记录)相同，没有 tag 的字段不在契约里；一个都没有，比如 `struct{}`，就是不带参数的规则）；`Out` 是返回类型。得到的 `Binding` 是只接受这套契约的编译器：

```go
type RouteIn struct {
    Country string    `funroute:"country"`
    Amount  int64     `funroute:"amount"`
    Order   Order     `funroute:"order"`   // struct 字段是 record 参数
    Scores  []float64 `funroute:"scores"`  // 原样透传，不复制
}

binding, _ := funroute.Bind[RouteIn, Decision](registry)
program, _ := binding.Compile(source)          // 返回类型参与推导，不是 Decision 就编译失败
program, _ = binding.Load(storedArtifact)       // 库里存的 artifact：按名字匹配，见下文
decision, _ := program.Run(ctx, &request)
```

`binding.Options()` 给出推出的 `CompileOptions`，控制台展示契约、语言服务检查程序都用它；`program.Artifact()` 交出编译好的 artifact，用来存库和发布。

**为什么快**：`Program.Run` 不查名字、不反射、不拼 map 或 `[]Value`，参数按绑定时算好的偏移直接从 struct 读出，结果按下标写回 `Out`。程序**没读的参数不做任何转换**，所以一个宿主 struct 可以服务多条规则。代价是没读的参数也不检查（enum 成员资格只对读到的参数检查）；只读几个 bool、int、float、string 字段的 record 参数也一样，只读那几个字段，其余字段不检查。规则看不到没读的值，结果不受影响。

剩下的分配都有名目。装载时的值流分析知道每个参数、每个数组去了哪里：只在程序里被读的，就地读、不分配；会交给宿主函数或放进结果的，才要自己的内存。

| 参数 | 分配 |
|---|---|
| 标量、字符串 | 0 次 |
| 程序只遍历、取长度或下标的原生切片（`[]int64`、`[]float64`、`[]string`、`[]bool`） | 0 次（切片头拷进帧，元素不复制也不读） |
| 只被遍历或取长度、循环里只读元素字段的 struct 切片（字段都是 bool、int、float、string） | 0 次（切片头拷进帧，每一轮把元素读进循环自带的 record） |
| 其他切片或映射 | 1 次（装箱它的头，元素不复制） |
| 程序只读字段的 record | 0 次（读进帧自带的 record；每次读都是一个 bool、int、float、string 字段时不建 record，只把读到的字段读进寄存器） |
| 其他 record | 1 次 |

| 结果 | `Run` | `RunInto` |
|---|---|---|
| 标量、record 本身 | 0 次 | 0 次 |
| 推导式产出的数组（整个结果，或结果 record 的一个字段） | 1 次（它的内存） | 0 次，只要 `out` 里那个切片有足够的容量 |

程序里的中间数组——只被遍历、取长度或下标的推导式与数组字面量——建在帧自己的内存里，帧跨运行复用，稳定之后 0 次分配。

`program.RunInto(ctx, &request, &out)` 把结果写进 `out`，并在 `out` 已有的切片里建数组结果，宿主拿同一个 `out` 反复运行就不再为结果分配。`out` 里的切片若与参数共用内存，就不在上面建（避免边读边写）；出错时 `out` 的内容不确定。

`Program.Run` 可以在任意多个 goroutine 里同时调用，每次从池里取一个帧、用完还回去。同一个 goroutine 要反复跑同一条规则时，用 `program.Session()`：会话自己持有一个帧，省掉每次取还的开销（`a + b` 从约 19 ns 到约 12 ns）。会话不能并发使用，每个 goroutine 各取一个：

```go
session := program.Session()
for _, request := range requests {
    if err := session.RunInto(ctx, &request, &out); err != nil { … }
}
```

规则不调用宿主函数时（只用内核与内核库），运行不设 `recover`：宿主函数是唯一可能 panic 的外来代码，调用它的规则照常把 panic 变成 `ErrExtension`。

结果里的容器是程序的 backing，只读。

**运行路径怎么选**：表单与 JSON 用 `Run(map)`；值预先构造好、要反复复用的用 `RunValues`；服务的热路径用 `Program`，一个 goroutine 反复跑同一条规则用它的 `Session`。

`[]float64` 与 `map[string]float64` 原样交给程序，不读也不复制：float 按 IEEE 754，NaN 与 ±Infinity 照常参与运算，所以边界上没有要检查的东西，长向量的开销与长度无关。规则要区分 NaN，就写 `x != x`（只有 NaN 不等于自己）。

**载入别处编译的 artifact**：`Load` 按名字匹配，规则与 `Run(map)` 的边界相同。

- artifact 声明的每个参数必须是 `In` 里同名的字段，顺序以 artifact 为准；record 声明的每个字段必须在 struct 的 tag 字段里。
- 类型必须完全相同，不做数值加宽。唯一的例外是枚举：Go 的 `string` 字段可以承载契约里的 enum，读入时检查成员资格。
- `In`、`Out` 多出来的字段一律忽略，`Out` 里没被结果覆盖的字段留零值。所以控制台只声明规则读到的参数和字段，照样能载入。
- 不匹配在载入时就报 `ErrContract`，并写明是哪个参数、哪个字段。
- `Bind` 自己推出的契约里没有枚举（Go 类型表达不了），需要枚举的契约由控制台编译后 `Load`。

## 在 Go 里算钱

规则算出的钱最终还是在 Go 里用：记账、展示、再分摊。`funroute.Money`、`funroute.Ratio`、`funroute.FxRate`、`funroute.ExactMoney` 带着与语言相同的运算方法，**内核的金额运算就是这些方法**，所以宿主在规则旁边算出的数与规则逐分相同。方法一律返回 `(值, error)`，错误与规则里完全相同：币种不一致是 `ErrCurrency`，溢出、除零、非法汇率、放不下的比值是 `ErrArithmetic`。它们都是几个 int64，运算不分配内存。

这些类型的字段都是私有的，宿主写不出一个不合规则的值：金额、币种与汇率由**币种表**造出（所以币种一定已声明），或经 JSON 解码得到（解码只检查形状与汇率规则，币种是否已声明在进入规则的边界上、或经币种表 `Format` 时检查），比例由 `ParseRatio`/`Percent`/`BasisPoints` 读出，读取一律经访问方法（`m.Currency()`、`m.Minor()`、`fx.Base()`……）。零值只有两个有意义：`funroute.Money{}` 是不带币种的零（与规则里的 `0` 一样能和任何币种相遇），`funroute.Ratio{}` 是 0。

要用到币种小数位的操作都在币种表上：`registry.Currencies()` 取规则用的那一张（没声明金额的注册表返回 `nil, false`），不跑规则的服务用 `funroute.NewCurrencies(spec)` 按同一个 spec 建一张。金额只能由币种表造出，包里没有包级的 `Parse`：只有币种表知道 `"USD 1.70"` 是多少个最小单位，币种不在表里也就造不出金额。

下表里的变量：

```go
table, _ := registry.Currencies()   // *funroute.Currencies，或 funroute.NewCurrencies(spec)
// m、o 是 funroute.Money，e 是 funroute.ExactMoney，r 是 funroute.Ratio，fx、next 是 funroute.FxRate，c 是 funroute.Currency，mode 是 funroute.Rounding（如 funroute.RoundHalfUp）
```

| 类别 | 入口 |
|---|---|
| 造金额 | `table.Parse("USD 1.70")`、`table.Of("USD", "1.70")`、`table.Minor("USD", 170)`；`table.Format(m)` 写回 `"USD 1.70"`，`table.Places("USD")` 是小数位 |
| 读金额 | `m.Currency()`、`m.Minor()`、`m.Sign()`、`m.IsZero()` |
| 加减比较 | `m.Add(o)`、`m.Sub(o)`、`m.Neg()`、`m.Abs()`、`m.Cmp(o)` |
| 乘除（当场舍入） | `m.MulInt(n)`、`m.MulRatio(r, mode)`、`m.DivRatio(r, mode)`、`m.Ratio(o)`（同币种之比，得比例）、`m.Prorate(part, whole, mode)`、`m.ProrateBy(part, whole, mode)`（按比例取，只舍入一次）、`m.RoundTo(step, mode)`（现金舍入） |
| 精确计算（规则里的 `round`） | `m.Exact()` 得 `ExactMoney`；`e.Add`/`Sub`/`Neg`/`MulInt`/`MulRatio`/`DivRatio`/`Cmp`/`Sign`、`table.ConvertExact(e, fx)` 都不舍入；`e.Round(mode)` 舍入一次得回 `Money` |
| 分摊与统计 | `m.Allocate(权重...)`、`m.Split(n)`（一分不丢，剩下的最小单位按最大余数法发出；n 为 1–10000，与规则里的 `allocate` 相同）、`m.AllocateBy(strategy, 权重...)`、`m.SplitBy(strategy, n)`（按 `funroute.AllocateAllLast` 等策略发）、`funroute.AverageMoney(ms, mode)`、`funroute.MedianMoney(ms, mode)`（与 std 的 `avg`/`median` 相同） |
| 比例 | `funroute.ParseRatio("0.029")`（也读分数 `"1/3"`）、`funroute.Percent("2.9")`、`funroute.BasisPoints("25")`；`r.Add`/`Sub`/`Mul`/`Div`、`r.Cmp`、`r.Sign`、`r.IsZero`、`r.String()` |
| 币种 | `table.Currency("USD")`，`c.Code()` |
| 汇率 | `table.FxRate("USD", "JPY", "150.25")`、`table.Implied(over, under)`；`fx.Base()`、`fx.Quote()`、`fx.String()`（`150.25 JPY / USD`，与规则里的字面量同一写法）、`fx.Decimal(places)`（半偶舍入后的展示文本，汇率本身不变）、`fx.Inverse()`、`fx.Chain(next)`（串联，前一个的报价币种必须是后一个的基准币种）、`fx.MulRatio(r)`（加点，与规则里的 `fx * 101%` 相同）、`fx.Cmp(other)` |
| 换汇 | `table.Convert(m, fx, mode)`：按一个汇率换并舍入一次，与规则里的 `convert(m, JPY, @mode)` 相同 |

汇率是精确比值：`"150.25"` 就是 601/4，`fx.Chain(next)` 是精确的乘积，只有换出来的金额舍入一次。同一币种之间恒为 1：`table.FxRate("USD", "USD", "1")` 合法，别的数是 `ErrArithmetic`。`table.Implied(over, under)` 是一笔交易隐含的汇率——over 是为 under 付出的：`Implied(JPY 15000, USD 100.00)` 是 `150 JPY / USD`，与规则里的 `implied(settled, paid)` 相同；两笔金额必须非零、同号、币种不同。

**汇率就是参数**：规则换汇用的报价由宿主作为参数传进来，Go 里就是一个 `[]funroute.FxRate`，零拷贝进入规则：

```go
table, _ := registry.Currencies()
usdJPY, _ := table.FxRate("USD", "JPY", "150")
usdEUR, _ := table.FxRate("USD", "EUR", "0.92")
market := []funroute.FxRate{usdJPY, usdEUR}   // 契约：market: array<fxrate>

// 规则：using(market, round(amount -> JPY, @half_even))
amount, _ := funroute.ToValue(dollars)
quotes, _ := funroute.ToValue(market)
value, err := runtime.RunValues(ctx, []funroute.Value{amount, quotes})
```

报价从哪来、何时过期、用哪一套，全由宿主决定：规则只看得见传给它的那几个汇率。类型化绑定里它就是 struct 的一个字段（`` Market []funroute.FxRate `funroute:"market"` ``）。

```go
table, _ := registry.Currencies()
amount, _ := table.Parse("USD 120.00")
rate, _ := funroute.Percent("2.9")
fee, _ := amount.MulRatio(rate, funroute.RoundHalfUp)          // 与规则里的 mul(amount, 2.9%, @half_up) 相同
exact, _ := amount.Exact().MulRatio(rate)
once, _ := exact.Round(funroute.RoundHalfUp)                   // 与 round(amount * 2.9%, @half_up) 相同
usdJPY, _ := table.FxRate("USD", "JPY", "150")
yen, _ := table.Convert(fee, usdJPY, funroute.RoundHalfUp)
text, _ := table.Format(yen)                               // JPY 522
```

`Parse` 读 `USD -1.70`，与规则里的字面量同一种写法（符号写在数上）；`Format` 写不出的金额（不带币种却不是零）返回 `ErrCurrency`，不会按错的小数位写出一段读不回来的文字。规则的结果经 `value.Money()`、`value.Ratio()`、`value.FxRate()`、`value.Currency()` 取出；Go 值交给规则用 `funroute.ToValue`。

## 注册扩展函数

注册只有一个入口：`registry.Register(funroute.FunctionSpec{…})`。最常用的是在 `Go` 里给一个 Go 函数，签名用反射读取：

```go
registry.Register(funroute.FunctionSpec{
    Name: "risk.score_v1",
    Doc: funroute.Doc{
        Label:       "风险评分",
        Description: "根据国家和金额计算风险分。",
        Params:      []string{"国家", "金额"},
    },
    Go: func(country string, amount int64) float64 { return 0.9 },
})
```

- 参数可以是 Go 标量、任意嵌套的切片和 `map[string]…`、struct（见[记录](language.md#记录)）或句柄；首参数可选 `context.Context`；返回 `R`，会失败的返回 `(R, error)`。
- 常见签名——标量与 `[]float64`/`[]int64` 进、标量出，可带 `error`——直接调用，0 次分配（连同一次运行约 50 ns，见 [`docs/perf.md`](perf.md)）；其余签名经 `reflect.Call`，每次调用多出几百纳秒，其中 struct 与切片参数按注册时规划好的布局读写，不在调用时再读 struct 标签。也可以不填 `Go`，手写 `Params`、`Result`、`Eval`；两种写法二选一。
- 以一个数组为参数的聚合可以声明 `Fold`，套推导式调用时就边算边折叠，不建数组、也不调用它：`Fold: &funroute.Fold{Step: "add", Init: funroute.Int(0)}` 是一个求和，`Step` 是把"到目前的答案"和下一个元素并起来的内核函数；`Stops`/`Stop` 让一个 bool 的折叠遇到 `Stop` 就停（`any` 停在 true）；`Counts` 是计数；`First` 取第一个元素并就此停下（`first`），一个都没有时对空数组调用函数本身、照样报错。折叠必须与函数本身给出同样的答案。
- 金额直接写 Go 类型：`funroute.Money`、`funroute.Ratio`、`funroute.FxRate`、`funroute.Currency` 及它们的切片与映射（零拷贝），`Go` 的签名反射就能读出；手写 `Params` 时用 `funroute.MoneyType` 等。币种是值的属性，签名不约束它：收到几笔金额的函数自己检查它们同币种（错了返回 `ErrCurrency`），返回的金额币种必须已声明，否则是 `ErrCurrency`。
- 名字里的 `_v1` 只是约定。函数的身份是完整签名，签名变了，旧 artifact 会拒绝装载。
- `Doc` 只写机器算不出来的东西：标签、说明、参数标签，以及可选的案例 `Examples: []funroute.Example{{Source: "risk.score_v1(\"SG\", 100)", Result: "0.9"}}`（源码与它的 JSON 结果）。签名来自 Go 类型，分类默认取命名空间（`risk.score_v1` → `risk`）。
- 语言服务从注册表读函数的说明与案例，用在悬停和补全里，新增函数不需要改任何前端代码。
- 内核与标准库的每个函数都带案例，由测试逐条运行、比对结果，并要求它们合起来用到这个名字的每一个重载，所以案例不会与实现走样。

扩展函数被当作**不可信的纯函数**：VM 会兜住 panic，但无法证明它真的没有副作用，生产环境仍需代码审查。完整的宿主范例见 `internal/demo/`（工作台用的演示控制台）。

**容器不拷贝**：`func(xs []float64)` 收到的就是宿主传进来的那个切片，返回的切片也原样进入 VM。代价是一条约定：交给 `Value` 的切片或映射，从那一刻起只读。

## 句柄：让模型数据穿过规则

FunRoute 不定义张量。模型引擎的数据以**不透明句柄**的形式流过表达式：规则只能把它传给下一个函数，不能比较、不能索引。

```go
funroute.DefineHandle[*ort.Tensor](registry, "onnx.tensor")

registry.Register(funroute.FunctionSpec{Name: "model.embed_v2", Doc: doc,
    Go: func(ctx context.Context, features []float64) (*ort.Tensor, error) { … }})
registry.Register(funroute.FunctionSpec{Name: "model.fraud_v3", Doc: doc,
    Go: func(ctx context.Context, emb *ort.Tensor) (float64, error) { … }})
```

```text
let(emb = model.embed_v2(features),
    switch(case model.fraud_v3(emb) > 0.9 => "reject", else => "accept"))
```

契约里可以写 `handle<onnx.tensor>` 声明句柄参数。

## 模型批处理

推理引擎要按批调用才划算。`GoBatch` 与 `Go` 一起给出批量实现，`Batch` 把一个时间窗内的请求合成一批：

```go
registry.Register(funroute.FunctionSpec{
    Name:    "model.fraud_v3",
    Doc:     funroute.Doc{Timeout: 8 * time.Millisecond},
    Go:      func(ctx context.Context, emb *ort.Tensor) (float64, error) { … },       // 单条
    GoBatch: func(ctx context.Context, embs []*ort.Tensor) ([]float64, error) { … }, // 批量
})

batch := funroute.NewBatch(runtime, funroute.BatchOptions{MaxSize: 256, MaxWait: 2 * time.Millisecond})
result, err := batch.Run(ctx, args)   // 可在任意 goroutine 调用，阻塞到本批完成
```

[类型化绑定](#类型化绑定契约就是两个-go-类型)有两种批处理：

- 宿主手里已经有 N 条请求时，用同步的 `RunBatch`：每个可合批的模型只调一次，结果按下标对应。一条失败不影响其他条：失败的那条结果是零值，并按下标顺序回调 `failed(i, err)`。`failed` 必填，所以失败不会被零值悄悄吞掉；全部成功时不为错误分配任何东西。
- 多个 goroutine 各自提交时，用 `program.Batch`。

```go
outs := make([]RouteOut, len(requests))                             // requests []RouteIn，整批共用 ctx
program.RunBatch(ctx, len(requests),
    func(i int) *RouteIn { return &requests[i] },                   // 第 i 条请求在哪
    func(i int) *RouteOut { return &outs[i] },                      // 第 i 个结果写到哪
    func(i int, err error) { log.Printf("request %d: %v", i, err) })

batch := program.Batch(funroute.BatchOptions{MaxSize: 256, MaxWait: 2 * time.Millisecond})
defer batch.Close()
decision, err := batch.Run(ctx, &request)
```

`RunBatch(ctx, n, in func(i int) *In, out func(i int) *Out, opts, failed)` 只有这一个入口：**一个下标同时指请求、结果和失败**，请求从 `in(i)` 读，结果写进 `out(i)`，`failed(i, err)` 说的就是第 i 条。请求与结果放在哪由宿主说：两个切片、每个请求自己的响应对象（结果直接写进去，不分配也不复制）、更大对象里的字段、跨批复用的缓冲区都是同一个调用。`in(i)` 在运行前调用一次，`out(i)` 在该条跑完后调用一次；返回 nil 只让那一条失败（`ErrContract`）。

几条共同的保证：

- **写回的 `Out` 总是完整的结果**：先清零再写，artifact 没声明的字段也是零，和 `Run` 返回的一样，跨规则复用的缓冲区不会残留上一条规则的值；失败那条保持零值，不会写一半。因此宿主自己的数据不要和规则结果放在同一个 `Out` 里。
- **ctx 一路传到引擎**：整批在宿主传入的 `ctx` 下运行，deadline 和 ctx 里的值（trace 等）都能到达模型调用。
- **分配**：`RunBatch` 的全部参数共用一次分配；`program.Batch` 每条请求多分配一次参数切片，因为请求要排队。和 `Program.Run` 一样，只转换程序读到的参数。
- **只合批不改变结果的调用**：参数直接来自入参或常量，且不在循环或条件分支里。`if`、`switch`、`fallback` 里的调用仍按需逐条执行。

## 超时、兜底与错误

- 请求的时间预算通过 `ctx` 传入，VM 在每次调用扩展函数前检查（标了 `Constexpr` 的纯函数除外：它们快、不等待任何东西）；循环每 32768 轮也检查一次（最慢的常见循环体约 1 ms 一次），所以规则在截止时间后约 1 ms 内停下，报 `ErrDeadline`。`ctx` 既没有截止时间也不能取消时一次也不查。语言必然终止，但遍历很长的输入、嵌套遍历字面量仍可能跑很久：给 `ctx` 设截止时间就是给规则设上限。
- `Doc.Timeout` 是单个函数的上限，实际 deadline 取 `min(请求剩余时间, Timeout)`。合批时取批内最早的 deadline，所以 `MaxWait` 要远小于请求预算。
- 无法取消的引擎绑定注册时标 `Doc.Detached: true`，VM 在独立 goroutine 中等它，到点就放弃。

规则里用 `fallback` 做兜底，按顺序尝试，前一个失败才试下一个：

```text
fallback(primary.quote_v1(order), secondary.quote_v1(order), 0.0)
```

`fallback` 只接住**数据暂时不可得**：扩展函数失败、超时、换汇找不到汇率。**规则或数据自身的错误**（算术失败、数据上没有答案、币种不一致）不会被吞掉，即使发生在扩展函数里：扩展函数返回的错误已经带上面任何一个类别（如 `ErrCurrency`、`ErrArithmetic`、`ErrNoFxRate`）时原样保留，不会被包成 `ErrExtension`。前一个候选在 `using` 里失败时，下一个候选从 `fallback` 所在处的汇率重新开始。

所有错误都可以用 `errors.Is` 区分。扩展函数自己的错误被归类后仍在错误链上：`errors.Is(err, funroute.ErrExtension)` 与 `errors.Is(err, 你的哨兵错误)` 都成立，`errors.As` 也取得到你的错误类型，超时同样认得出 `context.DeadlineExceeded`。

| 错误 | 含义 | `fallback` 接吗 |
|---|---|---|
| `ErrCompile` | 源码、ExprJSON 或类型有误 | —（编译期） |
| `ErrContract` | 契约非法、表达式读了未声明的参数，或入参不合契约 | —（运行前） |
| `ErrDeadline` | 请求或函数的时间预算耗尽 | 接 |
| `ErrExtension` | 扩展函数报错 | 接 |
| `ErrUnavailable` | 调用了只登记签名、没有实现的函数；同时是 `ErrExtension` | 接 |
| `ErrNoFxRate` | 换汇时 `using` 里没有这一对货币的汇率 | 接 |
| `ErrCurrency` | 币种不一致或未声明 | 不接 |
| `ErrArithmetic` | 算术没有答案，见下 | 不接 |
| `ErrDomain` | 数据上没有答案，见下 | 不接 |

`ErrArithmetic` 包括：整数溢出、整数除零、汇率不为正或同币种汇率不是 1、比例或汇率的分子分母放不进 int64，转换没有答案（`int("x")`、`int(2.5)`、`bool("yes")`、`ratio("abc")`、float 表示不了的 int），以及数值参数超出它的取值范围（`range` 的步长与长度、`percentile` 的比例、`pad_left` 的宽度、`allocate` 的份数与权重、`pow` 的负整数指数）。

`ErrDomain` 包括：下标越界、字典没有这个键、空数组的 `first`/`last`/`avg`/`median`/极值、要逐项对齐的两个数组长度不同（`sort_by`、`group_by`）、字典推导产生重复的键、数组里没有要找的元素。要兜底就先判断（`len(xs) > 0`、`"k" in d`）或写 `get(d, "k", 默认值)`，`fallback` 不接它。

这两类在内核、扩展函数还是 Go 方法里发生都一样；由入参带来的同时也是 `ErrContract`。

编译错误带位置：`errors.As` 取出 `*funroute.PositionError`，`Offset()` 与 `Span()` 是字节偏移与区间，`funroute.LineColumn(err, source)` 换算成行列。

## 按控制台开放能力

`switch`、列表推导、`reduce` 语法上总能解析，但**能不能用由注册表决定**：

```go
operator := funroute.CoreRegistry()
operator.EnableForm(funroute.SwitchForm, funroute.ForForm, funroute.ReduceForm)

minimal := funroute.CoreRegistry()
minimal.EnableForm(funroute.SwitchForm)   // 只给多分支，不给遍历
```

用了未启用的形式会在编译期报错（`reduce is not enabled in this registry`）。这条检查对源码和 ExprJSON 都生效，直接提交 JSON 也绕不过去。

内核（`funroute.CoreRegistry()`）是 19 个运算与转换，加上上面「聚合」一节的内核库：

- 控制：`if`、`fallback`、`eq`
- 比较：`lt`、`le`、`gt`、`ge`
- 算术：`add`、`sub`、`mul`、`div`、`mod`
- 容器：`at`、`member`、`len`
- 转换：`int`、`float`、`string`、`bool`

`&&`、`||`、`!`、`!=` 不在注册表里，它们展开成 `if`。其余一切（标准库、领域函数、模型）都由宿主注册。

金额同样是能力：`registry.DeclareMoney(spec)` 之后才有金额类型、字面量、金额运算、换汇 `->` 和 `using`。

- **先声明币种、再注册标准包**，标准包才会注册它对金额的重载。
- 用到金额的 artifact 带一个**金额戳**：字节码里写死的每个币种（字面量与常量里的代码）及其小数位。这些币种不再声明、或小数位变了，注册表就拒绝装载它。
- 运行时才知道的币种跟随当前的币种表，所以注册表新增币种、或改动 artifact 没有写到的币种，已部署的 artifact 照常装载。

## 性能

字节码装载时翻译成带类型的寄存器形式再执行：参数、常量和局部变量原地读，内核的算术与比较是一条条专用指令，比较与其后的跳转、推导式的收集与下一轮都合成一条。循环体只是 int/float/bool 的运算、筛选、折叠或收集时，按 256 个元素一块按列执行（可能停下的循环从 16 个一块起逐块翻倍），遇到会失败或停下的元素就交回逐个执行，结果与错误都不变。Artifact 的格式与 digest 不受影响。

**执行本身不分配内存**。装载时的值流分析知道每个数组去了哪里：只在程序里被遍历、取长度或下标的数组建在帧自己的内存里，帧跨运行复用，稳定之后 0 次分配；只有交给宿主的结果才要自己的内存，而 `Program.RunInto` 可以把它建在宿主给的切片里。金额也一样：边界上的币种扫描、金额运算、换汇与 `using` 都是 0 次分配。

编译器另做两处不改变结果的改写：声明了 `Fold` 的函数（`sum`、`any`、`all`、`first`、`len`）套推导式时编译成单遍折叠，不建中间数组；嵌套推导的内层源不依赖外层元素时只算一次。`any`/`all` 因此在决定答案的元素处停下，后面的元素不再计算，也不会报错，和 `||`、`&&` 一样。

Apple M5 上的几个数（2026-09-28；完整的表见 [`docs/perf.md`](perf.md)，由 `make perf` 生成，那里每项都与同一件事直接用 Go 写的耗时对照，并与 expr 逐行对照）：

| 场景 | 耗时 | 分配 |
|---|---|---|
| `amount * bps / 10000 + fixed`：`Program.Run` / `RunValues` / `Run(map)` | 21 / 42 / 60 ns | 0 |
| 按 Go 签名注册的宿主函数调用（常见签名不经反射），连同一次运行 | 53 ns | 0 |
| `[x + 1 for x in xs]`，每个元素 | 1.0 ns | 结果 1 次 |
| `sum([x * 2 for x in xs if x % 3 == 0])`，每个元素 | 3.2 ns | 0 |
| 500 元素 `reduce` | 0.96 µs | 0 |
| 把 16 到 65536 个 float 交给宿主函数（与长度无关：不拷贝） | 71 ns | 0 |
| 模型调用（引擎每次 20 µs）：单条 vs 64 条一批 | 27 µs vs 0.62 µs / 请求 | — |

与 expr 同机对照时，执行的 100 行里 FunRoute 耗时更短的 92 行、持平 3 行；编译因为多做类型推导、常量折叠、字节码验证与 digest，耗时是 expr 的 1.0–2.0×。
