# FunRoute

FunRoute 是面向支付路由的强类型、纯表达式函数语言。仓库同时提供 Go SDK、无框架拖拽式 JS 组件和可直接运行的 MVP。它的核心约束是：

- 没有语句、赋值、可变变量、反射或隐式宿主能力；
- 程序文本只是表达式；参数、类型与返回类型是**宿主的契约**，编译时传入（省了就全靠推导）；
- 自由变量自动成为函数参数；
- 参数类型由内置函数和扩展函数的签名反向推导；
- 不依赖参数的子表达式在**编译期**算掉，包括头部的常量绑定；
- 只做安全、可解释的数值提升，跨领域转换必须显式写 `int(...)`、`float(...)`、`string(...)` 或 `bool(...)`；
- 编译产物只引用精确的函数签名，运行时注册表漂移会拒绝装载；
- 内核只有 14 个函数名，惰性形式 `switch(...)`、列表推导 `[...]`、`reduce(...)` 由宿主按需启用；
- **所有程序保证终止**：迭代只遍历有限输入，语言刻意不图灵完备，fuel 是成本上限而非安全兜底；
- 源表达式可无损往返规范化 `ExprJSON`。

## 示例

```text
if(a,b,add(1,1))
```

编译器联合以下签名：

```text
if(bool, T, T) -> T
add(int, int) -> int
```

自动推出：

```text
(a: bool, b: int) -> int
```

运行：

```bash
go run ./cmd/funroute inspect \
  -expr 'if(a,b,add(1,1))'

go run ./cmd/funroute run \
  -expr 'if(a,b,add(1,1))' \
  -args '{"a":false,"b":9}'
```

第二条命令返回整数 `2`。`if` 是惰性特殊形式，未选择的分支不会执行。

多分支路由仍是函数式表达式：

```text
switch(country,"SG","adyen_sg","MY","stripe_my","stripe_global")
```

它会被推导为 `(country: string) -> string`。遍历和筛选用**列表推导式**，和 Python / Haskell 同形：

```text
[channel for channel in channels if route.is_healthy_v1(channel)]
```

读作“产出什么 ← 从哪来 ← 什么条件”。`if` 子句可省略（纯映射）。`channel` 是局部变量，不会出现在外部 `args` 中，结果类型自动推导为 `array<string>`。

推导式是一个**表达式**，返回新数组——这也是它不叫 `for` 的原因：在 C/Java/Go/Python 里 `for` 都是不返回值的语句，而这里的对应物是 Python 的 `[e for x in xs if c]`、LINQ 的 `.Where().Select()`、SQL 的 `SELECT e FROM xs WHERE c`。

聚合用 `reduce`，同样保持函数形态：

```text
reduce(prices,price,total,0,add(total,price))
```

五个参数依次是输入数组、元素局部名、累加器局部名、累加器初值和累加表达式；`price` 与 `total` 都是局部变量，不进入外部 `args`，推导结果是 `(prices: array<int>) -> int`。`reduce` 只遍历有限数组，每次迭代按 fuel 计费，不引入递归。

## 语法

```text
program    = expression                        // 契约是宿主的，不在文本里
expression = expression binary expression      // 中缀，见下表
           | unary expression                  // ! -
           | primary
primary    = integer | float | string | "true" | "false"
           | identifier
           | identifier "(" [ expression { "," expression } [ "," ] ] ")"
           | "[" [ expression { "," expression } [ "," ] ] "]"
           | "[" expression "for" identifier "in" expression [ "if" expression ] "]"
           | "{" [ string ":" expression { "," string ":" expression } [ "," ] ] "}"
           | "(" expression ")"
```

运算符是**源码层的糖**，脱糖后 AST 里只有函数调用，因此 ExprJSON 与拖拽画布完全不变；反向打印会还原成中缀，源码与节点树可以来回转换：

| 优先级 | 运算符 | 脱糖为 |
|---|---|---|
| 1 | `\|\|` | `if(a, true, b)`（短路） |
| 2 | `&&` | `if(a, b, false)`（短路） |
| 3 | `==` `!=` `<` `<=` `>` `>=` | `eq` / `if(eq(a,b),false,true)` / `lt` / `le` / `gt` / `ge` |
| 4 | `+` `-` | `add` / `sub` |
| 5 | `*` `/` | `mul` / `div` |
| 6 | `!` `-`（一元） | `if(a,false,true)` / `sub(0,a)` |

二元运算符都是左结合，`(...)` 可覆盖优先级。另外：`//` 行注释、`1_000_000` 数字分隔符、列表尾随逗号——这三项是纯词法糖，不进 AST，所以不会在 ExprJSON 往返中保留。

`switch` 有三种形态，前两种用 `case` 标出分支起点（这也是消除歧义的关键：否则 `switch(A, B => C)` 既可读作“主体 A”又可读作“条件 A 或 B”）：

```text
switch(country,                          // 值匹配
  case "SG"       => "adyen_sg",
  case "MY", "TH" => "adyen_asia",       // 多值分支，任一命中
  else               "stripe_global")

switch(                                  // 无主体 = 条件链，替代嵌套 if
  case amount > 10_000 => "manual_review",
  case risk > 0.8      => "reject",
  else                    "auto")

switch(country, "SG", "a", "MY", "b", "c")   // 位置形式，仍然接受
```

两种形态是**同一个节点**：ExprJSON 的 `switch` 节点里 `value` 缺失即条件形态，`case.match` 是一个列表。所以拖拽面板上仍然是一张多分支卡片（主体槽留空即切到条件模式，每个分支的匹配值可增删），不会退化成一串嵌套 `if` 卡片。分支按书写顺序惰性求值，未选中的分支不会被计算。

`reduce` 也接受关键字形式，让位置参数的含义写在语法里（与位置形式脱糖到同一个节点）：

```text
reduce(price in prices, total from 0, total + price)
reduce(prices, price, total, 0, add(total, price))      // 位置形式，仍然接受
```

`for`、`in`、`if`、`from`、`else`、`case` 在这些位置是关键字；`for`、`in`、`from`、`else`、`case` 同时是保留名，不能用作变量名或函数名。旧的 `for(...)` 函数写法已移除，写成它会得到明确的迁移提示。

`switch` 与 `reduce` 仍使用上面的函数调用外形；编译器把它们识别为惰性、多分支和局部变量结构，不引入语句式语法。

支持的值类型：

| 类型 | 示例 | 约束 |
|---|---|---|
| `bool` | `true` | 条件和比较结果所需的基础类型 |
| `int` | `42` | 有符号 64 位，溢出报错 |
| `float` | `0.25` | IEEE 754 float64，拒绝 NaN/Infinity |
| `string` | `"SGD"` | JSON 风格转义、UTF-8 |
| `array<T>` | `[1,2,3]` | 所有元素必须同型 |
| `dict<T>` | `{"primary":1}` | key 固定为 string，所有 value 必须同型 |

空数组/字典必须从所在函数签名或编译参数提示中获得元素类型。金额不应使用 `float`；生产版应注册独立的 `money`/`decimal` 类型和函数族。

## 契约

程序文本**只是表达式**。谁传进来、叫什么、什么类型、返回什么，是**宿主的数据**，编译时传入：

```go
result := lang.IntType
artifact, err := lang.CompileExpr(
    `switch(country, case "SG", "MY" => amount * 2, else amount)`,
    registry,
    lang.CompileOptions{
        Args: []lang.ArgSpec{
            {Name: "country", Type: lang.StringType, Doc: "ISO 3166-1 二字码"},
            {Name: "amount", Type: lang.IntType, Doc: "订单金额，单位：分"},
        },
        Result:    &result,
        ResultDoc: "应收总额，单位：分",
    },
)
```

### 为什么不放在语言里

也可以让一段文本用头部声明自己的参数与返回类型，从而自包含。不这么做的理由是：

- 一个支付控制台**本来就存**规则元数据 —— 规则 ID、版本号、生效窗口、灰度比例、审批记录、回滚指针。参数类型和说明是同类信息。放进语言就有了两份平行的元数据，迟早不一致。
- "调用方仍在传 `legacy_flag`，表达式已不用"这条信息的权威是**调用方契约**。表达式作者凭什么知道调用方还在传什么？宿主知道。
- 语言里的头部要求源码文本与结构化面板**双向同步**；契约在宿主则两者各管一块，少一整类 bug。
代价是一段裸表达式不自包含 —— 这一条由导出视图补上（见下）。

### 规则

| | |
|---|---|
| `Args` 非空 | 顺序即 ABI；为空则按自由变量首次出现顺序推导 |
| 声明了不用 | **可以** —— 表达式不再需要某个值时，调用方 ABI 不必跟着改 |
| 用了不声明 | 错误：`the expression reads "x" but the contract does not declare it` |
| `Result` | 参与 unify 而非事后比对，所以能定死 `[]` 的元素类型、能在重载里选签名 |
| `Doc` / `ResultDoc` | 唯一不进 digest 的东西 —— 改文案不会让已部署的 artifact 失效 |

`Result` 定不了字面量的类型：`1 + 2` 是 int 加法，声明 float 是真错误而不是转换请求。

### 导出视图

规则离开控制台时（工单、RFC、聊天里），裸表达式读者不知道 `amount` 是分还是元。`RenderWithContract` 把契约写成注释：

```text
// amount:  int              订单金额，单位：分
// country: string           ISO 3166-1 二字码
// →        int              应收总额，单位：分

switch(country, case "SG", "MY" => amount * 2, else amount)
```

注释**不是语法**：这段文本解析成同一个程序、编译出同一个 digest，粘回控制台照样工作；再次解析不会把注释带回来。权威始终是宿主记录。

### 编译期算清

"这个值是常量吗"由**编译器**判断，不需要关键字声明 —— 所以没有 `@const`：在一个无可变性、无类型层计算的纯语言里，求值时机完全由"它依赖什么"决定，而那是编译器 100% 算得准的事。

不读参数也不读循环变量的子表达式，在编译期就用真正的 VM 跑掉：

```text
let(
  bps       = 250,
  base_fee  = 3 * 100 + 50,
  total_bps = bps * 2,
  amount * total_bps / 10000 + base_fee
)
```

编译结果是 7 条指令、**0 个局部槽**：三个绑定全部折成常量（250、350、500），`total_bps` 说明折叠是传递的。运行时只剩一次乘、一次除、一次加。

- 折叠用同一个 VM，代码库里只有一套语义。
- 折不动就原样发指令，所以语义绝不改变 —— 这也是它能安全穿过惰性 `if` 的原因：`if(flag, 1 / 0, 42)` 里那个除零分支只是没被折叠，flag 为假照旧返回 42，为真照旧是运行时错误。
- 常量池只存标量，所以闭合的数组/字典仍在运行时构造。

## 类型推导

类型的唯一权威来源是函数注册表：

1. 内置函数；
2. 业务扩展函数。

不写声明时，变量在规范化 AST 中第一次出现的顺序决定参数顺序；字典节点会先按 key 排序，保证源码与 ExprJSON 往返后的参数 ABI 不变。写了 `param` 声明则由声明顺序决定（见「参数声明」）。编译器为每个参数建立类型变量，再使用所有函数签名做统一（unification）。例如：

```text
if(risk.approved_v1(country,amount),"primary","backup")
```

扩展函数若声明 `risk.approved_v1(string, int) -> bool`，编译器会联合
`if(bool,T,T)->T` 自动推导：

```text
(country: string, amount: int) -> string
```

类型便利规则按“安全且可解释”设计：

- `add(a,b)` 没有其他约束时默认把自由参数推导成 `int`；
- `add(1,1.5)` 这类混合数值会把整数精确提升为 `float`；超过 float64 精确整数范围时拒绝，而不是静默丢精度；
- 表达式里出现浮点字面量时，变量会被拉成 `float`：`risk < 0.5` 推出 `(risk: float)`，`amount * 1.5` 推出 `(amount: float)`。混合数值签名（`(int,float)`）是**末选**，只在不存在同型解读时才用；
- 字符串和数值之间不做隐式转换，使用 `int(...)`、`float(...)`、`string(...)`、`bool(...)` 明确表达；
- 扩展函数签名和字面量优先决定类型，例如 `route.score_v1(success,cost)` 会推出两个参数都是 `float`。

Go 调用方仍可通过编译环境覆盖默认值：

```bash
go run ./cmd/funroute inspect \
  -expr 'add(a,b)' \
  -types 'a=float,b=float'
```

## 最小内核

`lang.CoreRegistry()` 只有 14 个通用函数名，没有任何惰性形式；同名重载在拖拽面板合并为一张卡：

- 控制：`if`、`eq`
- 比较：`lt`、`le`、`gt`、`ge`（各有 int/float/string 与混合数值签名）
- 算术：`add`、`sub`、`mul`、`div`
- 转换：`int`、`float`、`string`、`bool`

布尔运算不在内核里——它们是**派生形式**，见下一节。

其余一切都是宿主的选择：`lang.RegisterArrayPrimitives(registry)` 加上 `array.is_empty`/`array.prepend`/`array.head`/`array.tail` 四个列表原语，`registry.EnableForm(...)` 打开惰性形式。

## 派生形式

`and`、`or`、`not` 和 `!=` 是**派生表达式**（derived expression，与 Scheme R7RS 同义）：语言用它们的展开来定义它们，核心里并不存在。

| 派生形式 | 写法 | 展开为 |
|---|---|---|
| and | `a && b` | `if(a, b, false)` |
| or | `a \|\| b` | `if(a, true, b)` |
| not | `!a` | `if(a, false, true)` |
| ne | `a != b` | `if(eq(a, b), false, true)` |

因为 `if` 惰性，`&&` 与 `||` 自动短路。这不是可选设计：在严格求值的语言里 `&&` **不可能**是普通函数，否则两侧都会被求值——Excel 的 `AND()` 就是这个坑（`IF(AND(A1<>0, 10/A1>2), …)` 会除零）。OCaml、Rust、Go 同样把它们定义为语言内建语法而非函数；只有 Haskell 那样整体惰性的语言才能让 `(&&)` 是普通函数。

派生形式**不进注册表**，不增加节点类型、opcode 或推导规则，但仍是一等的命名构造：

- `Registry.Catalog()` 把它们列在 `special_forms` 里（无需 `EnableForm`，因为 `if` 总在内核）；
- 拖拽面板有独立的「逻辑与 / 逻辑或 / 逻辑非」卡片，只暴露真正的操作数槽，固定分支隐藏，所以卡片不会被编辑成别的东西；
- 打印器把这三种 `if` 模式还原成 `&&` / `||` / `!` / `!=`，源码与节点树双向一致。

## 形式开关与控制台

惰性形式不是内建关键字，而是注册表上的开关。语法始终能被解析，**能不能用由注册表决定**：

```go
operator := lang.CoreRegistry()
operator.EnableForm(lang.SwitchForm, lang.ForForm, lang.ReduceForm)

minimal := lang.CoreRegistry()
minimal.EnableForm(lang.SwitchForm)      // 只给多分支，不给遍历
```

| 形式 | 语义 | 终止性 |
|---|---|---|
| `switch(...)` | 值匹配或条件链，按顺序惰性选择一个结果 | 结构上总是终止 |
| `[result for item in source if condition]` | 映射，可选筛选 | 只遍历输入数组 |
| `reduce(source,item,acc,init,body)` | 折叠进累加器 | 只遍历输入数组 |


一个注册表就是一个控制台：想给多少语言就开多少。用未启用的形式会在编译期被拒绝：

```text
reduce is not enabled in this registry
```

这条检查在 AST 上做，源码和 ExprJSON 走同一条路径，所以运营侧直接提交 ExprJSON 也绕不过去。

**没有无界循环、没有递归**。迭代一律用推导式与 `reduce`——它们的局部变量（`item`、累加器）是局部的，不会泄漏成参数契约，而且遍历次数由输入长度界定，所以程序必然终止。需要无界搜索的计算交给扩展函数，在 Go 侧设自己的上限与超时。

`Registry.Catalog()` 只列出该注册表启用的形式，因此拖拽面板看到的就是它实际能用的语言。

随机访问、排序、聚合、支付渠道能力、成本模型等不进入通用内核，由使用者注册扩展函数。

## 扩展函数

扩展函数与内置函数使用同一注册接口：

```go
registry := lang.CoreRegistry()

err := registry.Register(lang.FunctionSpec{
    Name:   "risk.score_v1",
    Params: []lang.Type{lang.StringType, lang.IntType},
    Result: lang.FloatType,
    Cost:   25,
    Eval: func(args []lang.Value) (lang.Value, error) {
        // 这里只应进行确定性的纯计算；动态数据应通过 args 传入。
        return lang.Float(0.9), nil
    },
    Display: lang.FunctionDisplay{
        Label:       "风险评分",
        Description: "根据国家和金额计算风险分。",
        Category:    "风控",
        Color:       "#DC2626",
        Icon:        "!",
        Parameters: []lang.ParameterDisplay{
            {Name: "country", Label: "国家"},
            {Name: "amount", Label: "金额"},
        },
        Result: lang.ResultDisplay{Label: "风险分"},
    },
})
```

名称里的 `_v1` 只是命名约定，语言不解析它：函数身份是完整签名，版本表达在名字本身。Artifact 同时冻结完整签名和 fuel 成本；同名函数被改成不同类型或成本时，旧 Artifact 会拒绝实例化。

`Display` 只用于控制台展示，不参与类型推导和执行。`Registry.Catalog()` 会输出全部可展示函数的标签、说明、分类、颜色、图标、参数说明、结果说明、示例、成本和类型签名。拖拽组件直接消费该目录，所以新增扩展函数不需要再维护一份前端清单。完整示例见 `extensions/paymentdemo/`。

更常用的是按 Go 签名注册：`lang.Logic(registry, name, doc, fn)`。`fn` 可以是任意元数的函数，参数与返回值是 Go 的标量（`bool`、各宽度的整数、`float32/float64`、`string`）、任意深度嵌套的切片与 string 键映射、以及注册表 `DefineHandle` 过的类型，首参数可选 `context.Context`，返回 `(R, error)`。签名与两个方向的转换在注册时用反射解析一次，调用时走 `reflect.Call`，每次约 300 ns、几次分配；内核函数是手写 `FunctionSpec`，不走反射，对性能敏感的宿主函数也可以这样写。

**容器不转换也不拷贝**：`func(xs []float64) (float64, error)` 收到的就是宿主传进来的那个切片；返回的切片原样成为 VM 里的值。其他形状（`[][]int32`、`map[string][]float64`）逐层构造，叶子仍是零拷贝。代价是一条包无法强制的约定：交给 `Value` 的切片或映射，以及从 `Value` 取出的，从那一刻起只读。

Go 无法从语言层证明回调实现真的无副作用，因此生产扩展 SDK 仍需代码审查、静态检查和 capability 封装。VM 捕获 extension panic，但不会把数据库、网络、时钟等能力主动暴露给函数。

### 句柄：引擎的数据穿过表达式

FunRoute 不定义张量。模型引擎（ONNX Runtime、TensorRT、libtorch）的张量以**不透明句柄**流过表达式：语言只知道它的名字，不能索引、不能比较、只能传给下一个函数。

```go
registry := lang.CoreRegistry()
lang.DefineHandle[*ort.Tensor](registry, "onnx.tensor")   // Go 类型 ↔ handle<onnx.tensor>

lang.Logic(registry, "model.embed_v2", doc, func(ctx context.Context, features []float64) (*ort.Tensor, error) { … })
lang.Logic(registry, "model.fraud_v3", doc, func(ctx context.Context, emb *ort.Tensor) (float64, error) { … })
```

```text
let(emb = model.embed_v2(features),
    switch(case model.fraud_v3(emb) > 0.9 => "reject", else "accept"))
```

`emb` 是引擎张量的指针，从一个模型到下一个模型没有一个字节进 Go 堆；`e == f` 在运行时报错"handles cannot be compared"；`handle<a>` 与 `handle<b>` 是不同类型，契约里可以写 `handle<onnx.tensor>` 声明参数。目录的 `value_types` 会列出注册表定义过的句柄类型。

### 模型与批处理

表达式不是一次路由决策的瓶颈，模型推理才是，而推理引擎要按批调用。`Model` 注册的函数同时带单条和批量两种实现（批量版的每个参数与结果都变成切片），`Batch` 把一个时间窗内的请求合成一批：

```go
lang.Model(registry, "model.fraud_v3", lang.Doc{Cost: 20, Timeout: 8 * time.Millisecond},
    func(ctx context.Context, emb *ort.Tensor) (float64, error) { … },       // Run 用
    func(ctx context.Context, embs []*ort.Tensor) ([]float64, error) { … })  // Batch 用：一次引擎调用

batch := lang.NewBatch(runtime, lang.BatchOptions{MaxSize: 256, MaxWait: 2 * time.Millisecond})
result, err := batch.Run(ctx, args)   // 任意 goroutine 调用，阻塞到本批完成
```

### 预算与超时

一次路由决策有延迟预算，慢的只会是模型。预算以 `ctx` 随请求进来（`Run(ctx, …)`、`RunValues(ctx, …)`、`Batch.Run(ctx, …)`），每个扩展调用都看到它；VM 只在调用前检查，纯计算部分是纳秒级不值得打断。函数级 `Doc.Timeout` 是这个模型的延迟上限，实际交给引擎的 deadline 是 `min(请求剩余预算, Timeout)`，一个慢模型吃不掉后面分支的时间。合批时引擎调用取**批内最早**的 deadline，所以 `MaxWait` 必须远小于请求预算。

默认信任函数遵守 `ctx`（Go 惯例，零开销）；确实无法取消的引擎绑定注册时标 `Doc.Detached: true`，VM 在独立 goroutine 里等它，到点即放弃，被放弃的调用继续运行到自己结束。

错误是类型化的：`ErrDeadline`（预算耗尽）、`ErrExtension`（函数报错）、`ErrFuel`（程序超出成本上限）用 `errors.Is` 区分。规则层的降级形式 `fallback` 只接前两种，程序自己的问题不该被规则吞掉。

`Batch` 只提升字节码能证明**提前算不改变任何可观察行为**的调用（`PrefetchSites`）：参数直接来自请求参数或常量、不在循环里、没有条件跳转能跳过它。`if`/`switch` 分支里的模型调用仍按需逐条执行，惰性语义不变。被提升的调用在程序里仍扣 fuel，所以预算与运行方式无关；引擎报错记在每个请求上，只在程序真的走到那次调用时抛出。

## 包边界

```text
lang/lang.go              唯一的公开接口：类型别名与转发，约 40 个标识符
lang/internal/machine/    值、类型、字节码、VM、帧、注册表、目录     ← 不依赖任何上层
lang/internal/syntax/     词法、语法、AST、ExprJSON                 ← 只依赖 machine
lang/internal/compile/    推导、编译、常量折叠、契约、导出视图        ← 依赖 syntax + machine
```

依赖严格单向，`go list -deps ./lang/internal/machine` 可验证。宿主需要的只有四样：

| | |
|---|---|
| `Registry` | 有哪些函数与惰性形式 —— 类型的唯一权威 |
| `CompileOptions` | 契约：参数、顺序、类型、说明、返回类型 |
| `Artifact` | 不可变字节码 + digest，可存储、可传输 |
| `Runtime` | 绑定到注册表的 Artifact，可运行 |

**AST 类型故意不公开**，也没有任何函数交出一个：程序用 ExprJSON 交换 —— 那正是 ExprJSON 存在的理由。`ParseToJSON` 把文本变成规范文档，`CompileExpr` / `CompileJSON` 接受文本或文档。`lang/lang_test.go` 是 `package lang_test`，只能用公开 API，所以它同时守着这条边界。

`machine` 为什么是一个包而不是拆得更细：`Value` 的容器 backing（原生 Go 切片/映射）是私有字段，VM 的循环、索引、构造直接在上面操作；拆开 value 与 vm 就只能走公开 accessor，每次取元素都要经过一层。

语法节点只在一处定义：`syntax/ast.go` 的每个节点 struct 用 tag 说明它的 JSON 字段、绑定哪些局部名及可见范围、前端默认值与列表下限；导入、导出、自由变量收集、`Children`、给前端的 `NodeSchema` 全部由 `syntax/walk.go` 从这些 tag 派生。

## API

宿主从不接触 AST：文本进去，规范文档或 artifact 出来。

```go
// 文本 → 规范 ExprJSON，前端要渲染的就是它
document, _ := lang.ParseToJSON(`if(a, b, add(1, 1))`)

// 文本或文档 → artifact，两条路在编译器里汇合
artifact, _ := lang.CompileJSON(document, registry, lang.CompileOptions{
    Args: []lang.ArgSpec{
        {Name: "a", Type: lang.BoolType},
        {Name: "b", Type: lang.IntType},
    },
})

runtime, _ := lang.Instantiate(artifact, registry)

// 按名字传（控制台填表单）
result, _ := runtime.Run(ctx, map[string]any{"a": false, "b": 9}, lang.RunOptions{Fuel: 10_000})

// 按 ABI 顺序传（服务热路径，省掉名字查找与转换）
result, _ = runtime.RunValues(ctx, []lang.Value{lang.Bool(false), lang.Int(9)}, lang.RunOptions{Fuel: 10_000})

// 容器零拷贝：特征向量包一层就进 VM，扩展函数拿到的是同一个切片
features, _ := lang.ToValue([]float64{0.2, 0.7, 0.1})
score, _ := runtime.RunValues(ctx, []lang.Value{features}, lang.RunOptions{Fuel: 10_000})
value, _ := lang.FromValue[float64](score)
```

`Run(map)` 也走同一条路：`map[string]any` 里放的是 `[]float64` 时直接包装；只有 JSON 解码出的 `[]any`、`float64` 当 int 这类形态才逐元素转换。前端要渲染的目录用 `lang.Catalog(registry)` 取 —— 它比 `Registry.Catalog()` 多带每种 ExprJSON 节点的 schema。

职责划分：

```text
ParseToJSON（文本）       CompileJSON（文档）
        ↓                      ↓
     语法 → AST ──────────────┘
        ↓
类型推导 + 重载选择（契约参与其中）
        ↓
常量折叠 → 不可变字节码 Artifact + digest
        ↓
Instantiate → 绑定精确扩展签名，拒绝漂移
        ↓
Run(map) / RunValues(slice) → Value
```

## ExprJSON

`version` 随文档形状变化，版本不符的文档被明确拒绝而不是猜测解读。

文档只有表达式 —— 契约不在里面，它是宿主记录的一部分：

```json
{
  "version": 1,
  "expr": { "node": "call", "name": "mul", "args": [] }
}
```

`export` 命令导出的是代码 AST，不是求值结果：

```bash
go run ./cmd/funroute export \
  -expr 'if(a,b,add(1,1))'
```

整数、浮点、字符串和变量均有不同的 node tag；字典键会排序；字段顺序就是节点 struct 的定义顺序。因此：

```text
Export(Import(Export(expr))) == Export(expr)
```

可以直接用于可视化编辑、版本 diff、digest 和回放。

节点的形状只写在 `syntax/ast.go` 的 struct tag 里，导入器按 tag 校验（缺字段、未知字段、列表下限、名字合法性），解析器与导入器共用同一份节点自检（重名、重复键）。同一份定义还以 `nodes` 字段随目录下发给前端：

```json
{ "node": "for", "form": "for", "fields": [
  { "name": "source", "kind": "expr" },
  { "name": "variable", "kind": "name", "role": "local", "default": "item" },
  { "name": "key_variable", "kind": "name", "optional": true, "role": "local" },
  { "name": "where", "kind": "expr", "optional": true },
  { "name": "yield", "kind": "expr" } ] }
```

前端的 `cleanNode`、空白模板与卡片布局都从它生成，所以新增或修改节点时，Go 与 JS 之间没有第二份要同步的清单。

## 拖拽式 JS 库

`web/funroute-designer.js` 注册原生 Web Component，`web/funroute-source.js` 是它与宿主共用的纯函数模块（表达式打印、格式化、词法分析，不碰 DOM）：

```html
<link rel="stylesheet" href="funroute-designer.css">
<funroute-designer id="designer"></funroute-designer>
<script type="module">
  import "./funroute-designer.js";
  const designer = document.querySelector("#designer");
  designer.catalog = await fetch("/api/catalog").then(r => r.json()); // lang.Catalog(registry) 的输出，含 nodes
  designer.value = {version: 2, expr: {node: "int", int: 1}};
  console.log(designer.value);  // ExprJSON
  console.log(designer.source); // 1
</script>
```

组件支持：

- 从函数目录拖入函数卡片；
- 把变量、整数、浮点数、字符串、布尔值、数组和字典拖入参数槽；
- 移动、删除和嵌套已有节点；
- `switch`、列表推导、`reduce`、`let`、数组、字典的卡片由目录里的节点 schema 生成：表达式槽、局部名输入、可增删的分支/绑定/键值，下限与默认名来自 schema，只有文案（`FIELD_TEXT`）是前端自己的；另有逻辑与/或/非卡片；
- 同名重载合并成一张卡，例如界面只显示一个 `add`，类型由编译器选择；
- 函数分类、搜索、说明、签名和参数提示；
- `value` 双向绑定规范化 ExprJSON，并触发 `funroute-change` 事件。

尺寸行为：面板高度由画布一侧决定，节点越多面板越高，下限是跟随窗口的 `max(360px, 55vh)`；画布只在节点树过宽时出现横向滚动条。函数目录列按外层宽度在 210–340px 之间伸缩，它脱离文档流并在内部滚动，所以目录再长也不会把面板撑高，底部也不会留白。宿主给 `<funroute-designer>` 设 `height` 仍然有效，此时改由画布纵向滚动。

不依赖 React/Vue、构建工具或第三方包，可以嵌入现有管理后台。

## 运行 MVP

```bash
go run ./cmd/mvp
```

打开 `http://127.0.0.1:8080`。页面内置三个模板：

- 支付路由：根据扩展函数 `route.is_healthy_v1` 选择主备渠道；
- `switch`：按国家选择渠道；
- `for`：筛选健康渠道数组。

页面自上而下是：工具栏、带语法高亮的表达式编辑器、参数与执行结果、拖拽画布、规范化 ExprJSON。参数和结果紧挨着表达式，改完参数不用滚到页面底部再运行。

编辑器里直接键入表达式再点“生成节点”就解析成节点树（回车等价，Shift+回车换行），“格式化”把超过 40 字符的表达式按参数逐行缩进，“复制”把当前表达式送进剪贴板；拖拽修改节点时编辑器内容同步刷新为格式化后的源码。

MVP 服务只持有一个注册表，页面上没有权限开关：这个 demo 注册表启用了全部形式。真实部署应按登录身份给不同注册表，少写一个 `EnableForm` 参数就是一档更小的语言。

MVP 提供：

```text
GET  /api/catalog  函数目录与展示元数据，内容由服务端注册表决定
POST /api/parse    源码 → 规范化 ExprJSON（只解析，不做类型推导）
POST /api/compile  ExprJSON/源码 → 参数、返回类型和 Artifact 摘要
POST /api/run      编译并执行
GET  /api/health   健康检查
```

能用哪些形式完全由服务端注册表决定，请求里没有任何权限字段。

## 性能

VM 是确定性栈式字节码解释器，**程序执行本身不分配内存**——只有程序构造的数据（`for` 的输出数组）才分配。Apple M5，`go test ./lang/internal/compile -bench . -benchtime 1s`：

| 基准 | 耗时 | 分配 |
|---|---|---|
| 200 层嵌套算术（纯分派） | 4.5 µs | 0 次 |
| 500 元素嵌套推导式 | 64 µs | 8 次 / 12 KB |
| 500 元素 `reduce` 折叠 | 35 µs | 2 次 |
| 500 元素推导式映射 | 36 µs | 5 次 / 8 KB |
| 简单表达式一次求值 `Run(map)` | 165 ns | 0 次 |
| 简单表达式一次求值 `RunValues` | 139 ns | 0 次 |
| 向量透传 `model.score(features)`，n = 16 / 1 024 / 65 536，`Logic` 反射注册 | 256 / 270 / 257 ns | 4 次 |
| 反射注册的两参数函数调用 `Logic` vs 内核 `add` | 300 ns vs 166 ns | 6 次 vs 0 次 |
| 编译（含类型推导与常量折叠） | 35 µs | — |
| 模型调用，模拟 20 µs 引擎开销：单条 vs 64 条一批 | 28.6 µs vs 1.35 µs / 请求 | — |

向量透传的耗时不随长度变化，是零拷贝的直接证据：宿主的 `[]float64` 进 VM、进扩展函数、出来，始终是同一个底层数组。那约 130 ns 的差价是 `reflect.Call` 与参数装箱，是"任意签名"的代价；不肯付的宿主函数写成 `FunctionSpec` 即可回到内核函数的成本。

支撑这些数字的实现要点：

- **容器就是原生 Go 值**：`array<float>` 的 backing 是 `[]float64`，`dict<int>` 是 `map[string]int64`，只有容器的容器才用 `[]Value`；宿主传入、扩展函数取出、返回值包装都是同一个 backing。元素占 8 字节而不是一个 `Value`，500 元素推导式的内存从 172 KB 降到 12 KB。
- **值不可变靠约定而非拷贝**：`Array()`/`Dict()`/`Any()`/`FromValue` 交出的是 backing 本身，持有者只读；VM 自身从不原地修改。`Value` 本体 56 字节（原 96），每次压栈、弹栈、绑定局部都少拷 40 字节。
- **`array.tail` 共享后缀**，O(1)。
- **单一激活**：没有递归就没有嵌套激活，每次 `Run` 只有一个帧，且帧来自 `sync.Pool`；栈与顶层参数存在帧内联数组里。
- **操作数零拷贝**：出栈返回栈上视窗而非副本，因此扩展函数收到的 `[]Value` 只在调用期间有效（`EvalFunc` 文档已说明，需要保留就自行复制）。
- **边界不分配**：`Run(map)` 对每个参数先试精确 Go 类型再走宽松路径，失败用哨兵错误而非 `fmt.Errorf`；`ToValue`/`FromValue` 的标量经指针匹配，不装箱。
- **紧凑枚举**：`Kind` 与 `OpCode` 是 `uint8`，指令分派走跳表；两者的 JSON 表示仍是原来的名字，因此 Artifact digest 与既有 ExprJSON 不受影响。
- **panic 防护在激活层**：`recover` 每次激活一次，不在每次扩展调用上。
- **不依赖参数的运算不进运行时**：常量折叠在编译期把闭合子表达式算成常量，折成常量的 `let` 绑定连局部槽都不占。编译因此略慢（一次性），运行时更短。
- **两条调用路径**：`Run(map)` 按名字转换，`RunValues(slice)` 按 ABI 顺序直接传 —— profile 显示名字查找与转换占一次短决策的 22%，所以热路径值得走后者。

## 当前实现边界

当前是语言内核 v0.3：

- 已实现解析、宿主契约（参数顺序/类型/说明、返回类型参与推导）、类型推导、泛型与重载、数值安全提升、显式转换、扩展注册、规范化 JSON AST、字节码编译、常量折叠、Artifact digest、惰性 `if/switch`、共用一套循环指令的推导式与 `reduce`、fuel/stack 限制和 VM；
- 推导式与 `reduce` 的局部变量由编译器分配，不污染外部参数；语言不提供一等 lambda、闭包、自定义局部函数与无界循环；
- 编译后端当前是确定性栈式字节码，稳定 API 已把后续 Wasm/JIT 与语言前端隔离；
终止性与表达力的形式化论证（强正规化、多项式时间上界、表达力边界，以及“如果要图灵完备该怎么加”）见 [`docs/termination.md`](docs/termination.md)。

- 常量池只存标量，所以闭合的数组/字典仍在运行时构造；要折叠它们需要扩展 Artifact 的常量格式（会 bump `ArtifactVersion`）；
- 正式用于支付前，还需要 `decimal/money`、结构化 record、错误/Option 类型、决策 trace、Wasm 后端和独立宿主 ABI manifest。

选择这个顺序是为了先冻结函数式语法、类型便利规则和扩展边界，再替换机器码后端，避免语言语义与 JIT 同时变化。

## 验证

```bash
make ci     # check-fmt + vet + lint + build + test
make test   # 只跑测试
go test ./lang/internal/compile -bench . -benchtime 2000x   # VM 基准
```

`make lint` 由 `tools/lint`（仅标准库）实现，强制风格预算：单个方法不超过 50 行、嵌套不超过 3 层、单个文件不超过 800 行。

测试覆盖样例推导、数值提升与显式转换、同型容器拒绝、扩展函数推导、函数式 `switch/for/reduce`、形式开关边界（含 ExprJSON 路径）、递归深度与 fuel 拦截、局部变量、展示目录、ExprJSON 往返与导入校验、节点 schema 与定义一致、容器跨边界零拷贝（指针相等断言）、惰性分支、Artifact 防篡改和 MVP HTTP API。
