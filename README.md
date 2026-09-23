# FunRoute

FunRoute 是一门给**支付路由规则**用的小语言：强类型、纯表达式、必然终止。

一条规则就是一个表达式，比如：

```text
switch(country,
  case "SG"       => "adyen_sg",
  case "MY", "TH" => "adyen_asia",
  else               "stripe_global")
```

仓库里有三样东西：

- **Go SDK**（`lang/`）：解析、类型推导、编译成字节码、执行。零第三方依赖。
- **语言服务**（`lang/lsp`）：一个 LSP 服务，诊断、补全、悬停、格式化都从语言本身得来。它既能以 stdio 运行，也能编成 WebAssembly 在浏览器里运行。
- **策略工作台**（`web/`）：编辑器加结构视图，全部语言能力来自语言服务，在浏览器里运行，不需要后端。

## 为什么是这样一门语言

支付规则由运营编写、由平台执行，出错就是资损。FunRoute 的每条设计都服务于"规则可审查、可预测、改不坏"：

- **只有表达式**。没有语句、赋值、可变变量，也没有任何隐式的宿主能力（网络、时钟、数据库）。
- **所有程序必然终止**。循环只能遍历有限的输入，语言刻意不图灵完备；最坏延迟可以从输入规模静态估出。
- **参数和类型归宿主管**。规则文本里不写参数声明，调用方在编译时传入"契约"；不传就全靠推导。
- **类型尽量推出来**。参数类型由函数签名反推，数值只做安全的提升，跨类型必须显式转换。
- **编译产物不会漂移**。Artifact 带 digest，冻结了它用到的每个函数签名；运行环境里函数变了，旧产物会拒绝装载。
- **文本是唯一的来源**。源码和规范化 JSON（ExprJSON）可以无损互转；工作台的结构视图只是同一段文本的投影。
- **能力由宿主开关**。内核只有 19 个函数名，`switch`、推导式、`reduce` 等按需启用，一个注册表就是一个控制台。

## 快速上手

需要 Go 1.26。

```bash
# 看推导出的签名
go run ./cmd/funroute inspect -expr 'if(a, b, add(1, 1))'
# (a: bool, b: int) -> int

# 运行
go run ./cmd/funroute run -expr 'if(a, b, add(1, 1))' -args '{"a": false, "b": 9}'
# 2

# 格式化
go run ./cmd/funroute fmt -expr 'let(a=1,a+2)'
# let(a = 1, a + 2)

# 启动策略工作台，打开 http://127.0.0.1:8080
make run
```

`if(a, b, add(1, 1))` 里没有任何类型声明。编译器从 `if(bool, T, T) -> T` 和 `add(int, int) -> int` 两个签名推出 `a` 是 `bool`、`b` 是 `int`。`if` 是惰性的，没选中的分支不会执行。

CLI 的子命令：`inspect`（签名与指令数）、`run`（执行）、`compile`（输出 artifact）、`export`（输出 ExprJSON）、`fmt`（格式化）、`lsp`（语言服务，见[语言服务](#语言服务)）。`-types 'a=int,b=float'` 给参数类型，`-alias` 声明类型别名（见[契约](#契约)）。

## 语言导览

### 值与类型

| 类型 | 字面量 | 说明 |
|---|---|---|
| `bool` | `true` | |
| `int` | `42`、`1_000_000` | 有符号 64 位，溢出报错 |
| `float` | `0.25` | float64，拒绝 NaN / Infinity |
| `string` | `"SGD"` | UTF-8，JSON 风格转义 |
| `array<T>` | `[1, 2, 3]` | 元素同型 |
| `dict<T>` | `{"primary": 1}` | 键是字符串，值同型 |
| `record{…}` | `{amount: 1200, currency: "SGD"}` | 字段固定、各有类型，见[记录](#记录) |
| `enum<name>{a,b}` | `@adyen` | 只能由契约声明，见[枚举](#枚举) |

几条规则：

- 字符串与数值之间不做隐式转换，用 `int(...)`、`float(...)`、`string(...)`、`bool(...)` 显式转换。
- 空的 `[]` / `{}` 需要从上下文（函数签名或契约）得到元素类型。
- 取整函数（`ceil`/`floor`/`round`）返回 `int`。要 float 就写 `float(floor(x))`。
- **没有 null**。数组越界、字典缺键、除零都是错误。要兜底就写出来：`get(d, "k", 0)`。
- 金额不应该用 `float`。生产环境应注册专门的 `money` 类型（已在路线图中）。

### 运算符

运算符只是函数调用的简写，`a + b` 就是 `add(a, b)`。优先级从低到高：

| 优先级 | 运算符 | 对应函数 |
|---|---|---|
| 1 | `\|\|` | `if(a, true, b)`（短路） |
| 2 | `&&` | `if(a, b, false)`（短路） |
| 3 | `==` `!=` | `eq`，`!=` 是 `eq` 取反 |
| 4 | `<` `<=` `>` `>=` `in` | `lt` `le` `gt` `ge` `member` |
| 5 | `+` `-` | `add` `sub` |
| 6 | `*` `/` `%` | `mul` `div` `mod` |
| 7 | `!` `-`（一元） | `if(a, false, true)`、`sub(0, a)` |
| 8 | `xs[i]` `d["k"]` | `at` |

二元运算符左结合，括号可改变优先级。`&&` 和 `||` 展开成惰性的 `if`，所以天然短路 —— `x != 0 && 10 / x > 2` 不会除零。

`in` 有三种用法：`x in xs` 查数组元素，`"k" in d` 查字典键，`"b" in text` 查子串。字符串按 UTF-8 码点处理，`s[i]` 返回单字符字符串。

注释用 `//`，数字可以写 `1_000_000`，列表可以带尾随逗号。

### 条件：`if` 与 `switch`

`if(条件, 是, 否)` 只执行选中的分支。

多分支用 `switch`，它有两种形态：

```text
// 值匹配：一个分支可以列多个值
switch(country,
  case "SG"       => "adyen_sg",
  case "MY", "TH" => "adyen_asia",
  else               "stripe_global")

// 条件链：不写主体，每个 case 是一个布尔条件，替代嵌套 if
switch(
  case amount > 10_000 => "manual_review",
  case risk > 0.8      => "reject",
  else                    "auto")
```

分支按顺序惰性求值。通常必须写 `else`（`else =>` 也可以）；只有当主体是枚举、且每个成员都被覆盖时才能省略 —— 契约里给枚举加了新成员，旧规则会编译失败，而不是悄悄漏掉。

### 局部绑定：`let`

`let(名字 = 值, …, 结果)` 给中间值起名：

```text
let(
  bps       = 250,
  base_fee  = 3 * 100 + 50,
  total_bps = bps * 2,
  amount * total_bps / 10000 + base_fee
)
```

后面的绑定可以引用前面的。这个例子里三个绑定都不依赖参数，编译期就被算成常量（见[编译期求值](#编译期求值)）。

### 遍历：列表推导

遍历和筛选用列表推导，写法和 Python 一样，读作"产出什么 ← 从哪来 ← 什么条件"：

```text
[channel for channel in channels if route.is_healthy_v1(channel)]
```

- `if` 子句可以省略（纯映射）。
- `channel` 是局部变量，不会变成外部参数。
- 遍历字典写 `for k, v in d`。
- 多个 `for` 连写就是笛卡尔积，结果是扁平数组：

  ```text
  [{channel: c, currency: k} for c in channels for k in currencies]
  ```

  每个 `for` 都可以带自己的 `if`：`for c in channels if healthy(c) for k in currencies`。

字典推导产出字典，只接受一个 `for` 子句：

```text
{k: v * 2 for k, v in rates}
```

### 聚合：标准库与 `reduce`

求和、计数、取最值这些日常操作，直接用标准库 `extensions/std` 的函数：

```text
sum([price for price in prices if price >= minimum])
```

需要自定义折叠时用 `reduce`。三个位置依次是：遍历什么、累加器及初值、每一步怎么算：

```text
reduce(price in prices, total = 0, total + price)
reduce(price in prices if price >= minimum, total = 0, total + price)   // 带筛选
reduce(name, weight in weights, total = 0.0, total + weight)            // 遍历字典
```

累加器写成 `total = 0`，和 `let` 的绑定同一个写法。

标准库的主要内容（完整清单见工作台的「函数说明」，或在编辑器里悬停查看）：

| 类别 | 函数 |
|---|---|
| 聚合 | `sum` `min` `max` `any` `all` `avg` `median` `stddev` `percentile` |
| 序列 | `range` `indices` `first` `last` `take` `slice` `reverse` `concat` `unique` `flatten` `sort` `sort_desc` `top_k` `bottom_k` `take_while` `drop_while` `windows` `chunk` `deltas` `cumsum` |
| 选择与分组 | `index_of` `arg_min` `arg_max` `sort_by` `sort_by_desc` `group_by` `rank` `intersect` `except` |
| 字典 | `get` `merge` |
| 字符串 | `upper` `lower` `trim` `contains` `starts_with` `ends_with` `split` `join` `replace` `pad_left` `pad_right` |
| 数值 | `abs` `ceil` `floor` `round` `pow` |

`range(n)` 是唯一能凭空造出数组的函数，所以它的参数必须由输入规模界定：字面量、`len(容器)`，或它们的算术组合。`range(len(fees))` 可以，`range(n)`（`n` 是任意入参）不行 —— 否则一个整数就能让规则跑任意久。

### 记录

`record` 对应宿主手里的对象：字段固定，每个字段有自己的类型。规则可以读字段，也可以构造新记录返回：

```text
{net: order.amount - fee, currency: order.currency}
```

- **字段顺序是类型的一部分**。`record{a: int, b: int}` 和 `record{b: int, a: int}` 是两个类型。字段访问在编译期解析成下标，运行时不查名字。
- **没有缺失字段**。传入的数据少一个字段就直接拒绝；读一个不存在的字段是编译错误。多出来的字段会被忽略。
- **靠写法区分字典和记录**：`{"k": v}` 是字典，`{k: v}` 是记录。
- **相等逐字段比较**，`==`、`in`、`switch` 都适用。
- 可以随意组合：`[o.amount for o in orders]`、`order.tags[0]`、`dict<record{…}>` 都成立。
- **字段更新**写作 `{...order, amount: order.amount - fee}`：复制一份记录，替换写出的字段，其余原样保留。结果的类型就是原记录的类型，所以 `Order` 进、`Order` 出。只能替换已有字段、新值必须是该字段的类型，字段名不存在或类型不同都是编译错误。`...` 只能出现一次、必须写在最前，后面至少跟一个字段。嵌套字段靠嵌套来改：`{...b, customer: {...b.customer, amount: 1}}`。

Go struct 可以直接当 record 用，只有带 `funroute` tag 的导出字段会进入记录，顺序就是声明顺序：

```go
type Order struct {
    Amount       int64     `funroute:"amount"`
    CurrencyCode string    `funroute:"currency_code"`
    Tags         []string  `funroute:"tags"`
    UpdatedAt    time.Time // 没有 tag，不在记录里
}
```

不会从 Go 字段名推断记录字段名。否则有人在 Go 侧改个名字，契约就被悄悄改了。漏标 tag 也不会静默出错：读那个字段的规则会在编译期报 `has no field`。

### 语法速查

```text
program    = expression                             // 契约由宿主给出，不在文本里
expression = expression binary expression           // 中缀，见运算符表
           | unary expression                       // ! -
           | primary
primary    = integer | float | string | "true" | "false"
           | "@" identifier [ "." identifier ]      // 枚举成员
           | identifier                             // 变量
           | identifier { "." identifier } "(" [ args ] ")"   // 函数调用，名字可带点
           | "[" [ args ] "]"                       // 数组
           | "[" expression loop { loop } "]"       // 列表推导
           | "{" [ string ":" expression { "," string ":" expression } [ "," ] ] "}"          // 字典
           | "{" identifier ":" expression { "," identifier ":" expression } [ "," ] "}"      // 记录
           | "{" "..." expression "," identifier ":" expression { "," identifier ":" expression } [ "," ] "}"  // 字段更新
           | "{" expression ":" expression loop "}" // 字典推导
           | "(" expression ")"
           | primary "[" expression "]"             // 索引
           | primary "." identifier                 // 字段
args       = expression { "," expression } [ "," ]
loop       = "for" identifier [ "," identifier ] "in" expression [ "if" expression ]
```

`switch`、`reduce`、`let` 在外形上是函数调用，内部用 `case … =>`、`else`、`x in xs`、`acc = init` 标出各个位置。`for`、`in`、`else`、`case` 是保留字，不能用作变量名或函数名。变量名不能带点，`.` 留给字段访问。

## 契约

规则文本**只是表达式**。参数叫什么、什么类型、什么顺序、返回什么，由宿主在编译时传入：

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

规则：

| 情形 | 行为 |
|---|---|
| 给了 `Args` | 顺序就是调用 ABI |
| 没给 `Args` | 按变量在表达式中首次出现的顺序推导 |
| 声明了但没用 | 允许。规则不再需要某个值时，调用方不必跟着改 |
| 用了但没声明 | 编译错误：`the expression reads "x" but the contract does not declare it` |
| `Result` | 参与类型推导，可以定下 `[]` 的元素类型、在重载中选签名 |
| `Doc` / `ResultDoc` | 只是说明文字，不影响 digest，改文案不会让已部署的 artifact 失效 |

`Result` 不会改变字面量的类型：`1 + 2` 是整数加法，把结果声明成 `float` 会报错，而不是自动转换。

**为什么不把参数写进规则文本？** 支付控制台本来就存着规则的元数据：版本、生效时间、灰度比例、审批记录。参数类型是同一类信息，写进语言就会有两份元数据，迟早对不上。而且"调用方还在传哪些参数"只有宿主知道。

### 类型别名

同一个 record 出现在多个参数上时，可以先起个名字：

```bash
go run ./cmd/funroute run \
  -alias 'Order=record{amount: int, currency: string}' \
  -types 'a=Order,b=Order' \
  -expr 'if(a.amount > b.amount, a, b).currency' \
  -args '{"a":{"amount":100,"currency":"USD"},"b":{"amount":300,"currency":"EUR"}}'
```

HTTP API 的契约里写在 `types` 字段：

```json
{"types": {"Order": "record{amount: int, currency: string}"},
 "args": [{"name": "a", "type": "Order"}, {"name": "b", "type": "array<Order>"}],
 "result": {"type": "Order"}}
```

别名只是一种**写法**：解析时就地展开，编译器和 artifact 都看不到它，用别名和写全字段编出的是同一个 artifact。因此：

- 别名不能引用别的别名；
- 没声明的名字仍然报错（`unknown type "Order"`）；
- Go 宿主直接复用 `lang.Type` 变量即可，不需要别名。

### 枚举

枚举只能由契约声明：Go 里写 `lang.EnumOf("channel", "adyen", "stripe")`，文本契约写 `enum<channel>{adyen,stripe}`。表达式里用 `@成员` 引用：

```text
switch(channel, case @adyen => @stripe, case @stripe => @adyen)   // 已穷尽，不需要 else
let(preferred = @stripe, channel == preferred)
@channel.adyen                                                     // 多个枚举有同名成员时写全名
```

- 枚举是**具名类型**：`enum<a>{x}` 和 `enum<b>{x}` 不是同一个类型，枚举也不能当字符串用，需要时写 `string(channel)`。
- `@adyen` 属于哪个枚举，由契约里声明过的枚举决定。只有一个枚举含它就写短名；有多个时编译器要求写全名。
- 运行时，枚举入参会拒绝集合外的字符串；枚举返回值要求编译器能证明每条路径都落在成员内。

### 导出视图

规则被贴到工单或聊天里时，读者看不到契约。`lang.RenderWithContract` 把契约写成注释：

```text
// amount:  int              订单金额，单位：分
// country: string           ISO 3166-1 二字码
// →        int              应收总额，单位：分

switch(country, case "SG", "MY" => amount * 2, else amount)
```

注释不是语法：这段文本粘回控制台，编出同一个 digest。

## 类型推导

类型的唯一来源是函数注册表里的签名。例如扩展函数声明了 `risk.approved_v1(string, int) -> bool`，那么：

```text
if(risk.approved_v1(country, amount), "primary", "backup")
```

会推出 `(country: string, amount: int) -> string`。

数值的便利规则：

- 没有其他约束时，`add(a, b)` 把参数推成 `int`；
- 出现浮点字面量时变量被推成 `float`：`risk < 0.5` 得到 `risk: float`；
- `add(1, 1.5)` 这样的混合运算把整数提升为 `float`，超出 float64 精确范围的整数会被拒绝；
- 混合签名 `(int, float)` 只在没有同型解读时才使用。

推导结果可以用 `-types` 或 `CompileOptions.Args` 覆盖：

```bash
go run ./cmd/funroute inspect -expr 'add(a,b)' -types 'a=float,b=float'
```

## 编译期求值

不依赖参数的子表达式，编译期就会被算完，不需要任何关键字：

```text
[1, 2, 3]                    → 一条载入指令
upper("adyen")               → 一条载入指令
sum(range(4))                → 一条载入指令
let(base = {a: 1}, base.a)   → 一条载入指令
1 / 0                        → 编译错误，即使写在不会走到的分支里
```

前面 `let` 的例子编译出 7 条指令、0 个局部变量槽：三个绑定都被折成了常量，运行时只剩一次乘、一次除、一次加。

哪些函数能在编译期调用由宿主授权（`Doc.Constexpr`）。内核和标准库都可以；模型、时钟、远程调用**不应该**标 —— 否则编译规则时就会去调用推理引擎，同一条规则在不同时间编译出不同的结果。

## 在 Go 中使用

### 编译与运行

```go
registry := lang.CoreRegistry()
registry.EnableForm(lang.SwitchForm, lang.ForForm, lang.ReduceForm)
std.Register(registry)

// 文本或 ExprJSON → artifact
artifact, _ := lang.CompileExpr(`if(a, b, add(1, 1))`, registry, lang.CompileOptions{
    Args: []lang.ArgSpec{
        {Name: "a", Type: lang.BoolType},
        {Name: "b", Type: lang.IntType},
    },
})

// artifact 绑定到注册表，签名不符会拒绝
runtime, _ := lang.Instantiate(artifact, registry)

// 按名字传参（适合表单）
result, _ := runtime.Run(ctx, map[string]any{"a": false, "b": 9}, lang.RunOptions{Fuel: 10_000})

// 按顺序传参（热路径，省掉名字查找）
result, _ = runtime.RunValues(ctx, []lang.Value{lang.Bool(false), lang.Int(9)}, lang.RunOptions{Fuel: 10_000})
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
| `Artifact` | 不可变的字节码 + digest，可存储、可传输 |
| `Runtime` | 绑定到注册表后可运行的 Artifact |

`lang/lang.go` 是唯一的公开包。AST 类型刻意不公开，程序一律用 ExprJSON 交换。

### 类型化绑定：契约就是两个 Go 类型

宿主本来就有请求 struct 和结果 struct，契约可以直接从它们读出来。`lang.Bind[In, Out]` 反射**一次**：`In` 里带 `funroute:"name"` tag 的导出字段是参数，**声明顺序即 ABI**（规则与[记录](#记录)相同，没有 tag 的字段不在契约里；一个都没有，比如 `struct{}`，就是不带参数的规则）；`Out` 是返回类型。得到的 `Binding` 是只接受这套契约的编译器：

```go
type RouteIn struct {
    Country string    `funroute:"country"`
    Amount  int64     `funroute:"amount"`
    Order   Order     `funroute:"order"`   // struct 字段是 record 参数
    Scores  []float64 `funroute:"scores"`  // 原样透传，不复制
}

binding, _ := lang.Bind[RouteIn, Decision](registry)
program, _ := binding.Compile(source)          // 返回类型参与推导，不是 Decision 就编译失败
program, _ = binding.Load(storedArtifact)       // 库里存的 artifact：按名字匹配，见下文
decision, _ := program.Run(ctx, &request, lang.RunOptions{})
```

`binding.Options()` 给出推出的 `CompileOptions`，控制台展示契约、语言服务检查程序都用它。`Program.Run` 不查名字、不反射、不拼 map 或 `[]Value`：参数按绑定时算好的偏移直接从 struct 读进参数区，结果按下标写回 `Out`；程序**没读的参数不做任何转换**，所以一个宿主 struct 可以服务多条规则。代价是没读的参数也不做检查：enum 成员资格、NaN 检查只对程序读到的参数生效，而 `Run(map)` 会检查全部声明的参数。规则看不到没读的值，结果不受影响。

剩下的分配都有名目：标量与字符串 0 次；每个切片/映射参数 1 次（装箱它的头，元素不复制，`[]float64` 仍做一遍 NaN 检查）；每个 record 参数 2 次（字段与 record 本身）。结果里的容器是程序的 backing，只读。三条路径的取舍：表单与 JSON 用 `Run(map)`；向量预先检查好、要反复复用的用 `RunValues`；服务的热路径用 `Program`。`program.Artifact()` 交出编译好的 artifact，用来存库和发布。

`Load` 载入别处编译的 artifact 时**按名字匹配**，规则和 `Run(map)` 的边界一样：artifact 声明的每个参数必须是 `In` 里同名的字段，顺序以 artifact 为准；record 声明的每个字段必须在 struct 的 tag 字段里；类型必须完全相同，不做数值加宽。`In`、`Out` 多出来的字段一律忽略，`Out` 里没被结果覆盖的字段留零值。所以控制台只声明规则读到的参数和字段，照样能载入。唯一的例外是枚举：Go 的 `string` 字段可以承载契约里的 enum，读入时检查成员资格，不是成员就报 `ErrContract`。不匹配的地方在载入时就报 `ErrContract`，并写明是哪个参数、哪个字段。`Bind` 自己推出的契约里没有枚举（Go 类型表达不了），需要枚举的契约由控制台编译后 `Load`。

### 注册扩展函数

最常用的方式是按 Go 函数签名注册，签名用反射读取：

```go
lang.Logic(registry, "risk.score_v1", lang.Doc{
    Label:       "风险评分",
    Description: "根据国家和金额计算风险分。",
    Cost:        25,
    Params:      []string{"国家", "金额"},
}, func(country string, amount int64) (float64, error) {
    return 0.9, nil
})
```

- 参数可以是 Go 标量、任意嵌套的切片和 `map[string]…`、struct（见[记录](#记录)）或句柄；首参数可选 `context.Context`；返回 `(R, error)`。
- 每次调用约 300 ns。对性能敏感的函数可以手写 `lang.FunctionSpec`，成本与内核函数相同。
- 名字里的 `_v1` 只是约定。函数的身份是完整签名，签名或成本变了，旧 artifact 会拒绝装载。
- `Doc` 只写机器算不出来的东西：标签、说明、成本、参数标签。签名来自 Go 类型，分类默认取命名空间（`risk.score_v1` → `risk`）。
- 语言服务从注册表读函数的说明，用在悬停和补全里，新增函数不需要改任何前端代码。

扩展函数被当作**不可信的纯函数**：VM 会兜住 panic，但无法证明它真的没有副作用，生产环境仍需代码审查。完整示例见 `examples/payment/`。

**容器不拷贝**：`func(xs []float64)` 收到的就是宿主传进来的那个切片，返回的切片也原样进入 VM。代价是一条约定：交给 `Value` 的切片或映射，从那一刻起只读。

### 句柄：让模型数据穿过规则

FunRoute 不定义张量。模型引擎的数据以**不透明句柄**的形式流过表达式：规则只能把它传给下一个函数，不能比较、不能索引。

```go
lang.DefineHandle[*ort.Tensor](registry, "onnx.tensor")

lang.Logic(registry, "model.embed_v2", doc, func(ctx context.Context, features []float64) (*ort.Tensor, error) { … })
lang.Logic(registry, "model.fraud_v3", doc, func(ctx context.Context, emb *ort.Tensor) (float64, error) { … })
```

```text
let(emb = model.embed_v2(features),
    switch(case model.fraud_v3(emb) > 0.9 => "reject", else "accept"))
```

契约里可以写 `handle<onnx.tensor>` 声明句柄参数。

### 模型批处理

推理引擎要按批调用才划算。用 `lang.Model` 同时注册单条和批量两种实现，`Batch` 把一个时间窗内的请求合成一批：

```go
lang.Model(registry, "model.fraud_v3", lang.Doc{Cost: 20, Timeout: 8 * time.Millisecond},
    func(ctx context.Context, emb *ort.Tensor) (float64, error) { … },        // 单条
    func(ctx context.Context, embs []*ort.Tensor) ([]float64, error) { … })   // 批量

batch := lang.NewBatch(runtime, lang.BatchOptions{MaxSize: 256, MaxWait: 2 * time.Millisecond})
result, err := batch.Run(ctx, args)   // 可在任意 goroutine 调用，阻塞到本批完成
```

类型化绑定（见[类型化绑定](#类型化绑定契约就是两个-go-类型)）有两种批处理。宿主手里已经有 N 条请求时用同步的 `RunBatch`：每个可合批的模型只调一次，结果按下标对应。一条失败不影响其他条：失败的那条结果是零值，并按下标顺序回调 `failed(i, err)`（必填，所以失败不会被零值悄悄吞掉），全部成功时不为错误分配任何东西。多个 goroutine 各自提交时用 `program.Batch`：

```go
outs := program.RunBatch(ctx, requests, lang.RunOptions{}, func(i int, err error) {
    log.Printf("request %d: %v", i, err)
})                                                                  // requests []RouteIn，整批共用 ctx

batch := program.Batch(lang.BatchOptions{MaxSize: 256, MaxWait: 2 * time.Millisecond})
defer batch.Close()
decision, err := batch.Run(ctx, &request)
```

三种形状共用一条规则：**一个下标同时指请求、结果和失败**——`out[i]` 对应 `in[i]`，`failed(i, err)` 说的就是 `in[i]`。

- `RunBatch(ctx, in []In, opts, failed) []Out`：结果新分配。
- `RunBatchInto(ctx, in []In, out []*Out, opts, failed)`：结果直接写进宿主已有的对象（比如每个请求自己的响应），不为结果分配也不复制；`in` 与 `out` 必须等长。
- `RunBatchFunc(ctx, n, in func(i int) *In, out func(i int) *Out, opts, failed)`：其余一切形状——`In`/`Out` 是更大对象里的字段、请求分散在堆上、结果缓冲区跨批复用。`in(i)` 在运行前调用一次，`out(i)` 在该条跑完后调用一次；返回 nil 只让那一条失败（`ErrContract`）。

写回的 `Out` 每次都是完整的结果：先清零再写，artifact 没声明的字段也是零，和 `Run` 返回的一样，所以跨规则复用的缓冲区不会残留上一条规则写的值；失败那条保持零值，不会写一半。所以宿主自己的数据不要和规则的结果放在同一个 `Out` 里。整批在宿主传入的 `ctx` 下运行，模型调用也是，所以 deadline 和 ctx 里的值（trace 等）都能到达引擎。三者的全部参数共用一次分配；`program.Batch` 每条请求多分配一次参数切片，因为请求要排队。两者和 `Program.Run` 一样，只转换程序读到的参数。

只有"提前算也不改变结果"的调用才会被合批：参数直接来自入参或常量，且不在循环或条件分支里。`if`、`switch`、`fallback` 里的调用仍按需逐条执行。

### 超时、兜底与错误

- 请求的时间预算通过 `ctx` 传入，VM 在每次调用扩展函数前检查。
- `Doc.Timeout` 是单个函数的上限，实际 deadline 取 `min(请求剩余时间, Timeout)`。合批时取批内最早的 deadline，所以 `MaxWait` 要远小于请求预算。
- 无法取消的引擎绑定注册时标 `Doc.Detached: true`，VM 在独立 goroutine 中等它，到点就放弃。

规则里用 `fallback` 做兜底，按顺序尝试，前一个失败才试下一个：

```text
fallback(primary.quote_v1(order), secondary.quote_v1(order), 0.0)
```

`fallback` 只接住扩展函数的失败和超时。fuel 耗尽、除零、类型错误这些**规则自身的错误**不会被吞掉。

所有错误都可以用 `errors.Is` 区分：

| 错误 | 含义 |
|---|---|
| `ErrCompile` | 源码、ExprJSON 或类型有误 |
| `ErrContract` | 契约非法，或表达式读了未声明的参数 |
| `ErrDeadline` | 请求或函数的时间预算耗尽 |
| `ErrExtension` | 扩展函数报错 |
| `ErrFuel` | 超出成本上限 |

编译错误带位置，`lang.LineColumn(err, source)` 换算成行列。

## 按控制台开放能力

`switch`、列表推导、`reduce` 语法上总能解析，但**能不能用由注册表决定**：

```go
operator := lang.CoreRegistry()
operator.EnableForm(lang.SwitchForm, lang.ForForm, lang.ReduceForm)

minimal := lang.CoreRegistry()
minimal.EnableForm(lang.SwitchForm)   // 只给多分支，不给遍历
```

用了未启用的形式会在编译期报错（`reduce is not enabled in this registry`）。这条检查对源码和 ExprJSON 都生效，直接提交 JSON 也绕不过去。

内核（`lang.CoreRegistry()`）只有 19 个函数名：

- 控制：`if`、`fallback`、`eq`
- 比较：`lt`、`le`、`gt`、`ge`
- 算术：`add`、`sub`、`mul`、`div`、`mod`
- 容器：`at`、`member`、`len`
- 转换：`int`、`float`、`string`、`bool`

`&&`、`||`、`!`、`!=` 不在注册表里，它们展开成 `if`。其余一切（标准库、领域函数、模型）都由宿主注册。

## ExprJSON

ExprJSON 是表达式的规范化 JSON 形式：artifact 的 digest 覆盖它，宿主之间交换、存储和比较程序也用它。

```json
{
  "version": 1,
  "expr": { "node": "call", "name": "mul", "args": [] }
}
```

```bash
go run ./cmd/funroute export -expr 'if(a,b,add(1,1))'
```

- 文档里只有表达式，契约不在其中。
- 源码 → JSON → 源码可以无损往返：`Export(Import(Export(e))) == Export(e)`。注释、数字分隔符和尾随逗号不会保留。
- `version` 不匹配的文档会被拒绝，不会去猜。
- 运算符不产生新节点：`a + b` 在 JSON 里就是 `add` 调用，反向打印时再还原成中缀。

## 语言服务

`lang/lsp` 是一个 [Language Server Protocol](https://microsoft.github.io/language-server-protocol/) 服务。它报告的都是语言本身知道的事实——词法器和解析器对每一段源码的判断、编译器的诊断和类型、格式化器的排版——至于怎么显示，由客户端决定。

| 能力 | 来自 |
|---|---|
| 语义标记 | 词法器与解析器的判断：关键字、运算符、参数、局部名（定义处 / 引用处）、函数、字段、字面量、注释 |
| 诊断 | 编译器，带出错的区间；读了契约没声明的变量，会指到那次读取 |
| 格式化 | 格式化器；打印结果一定能解析回同一个程序，表达式中间有注释时拒绝格式化，而不是丢掉注释 |
| 悬停 | 节点推导出的类型、调用选中的签名、宿主写的函数说明和参数说明 |
| 补全 | 程序的参数（契约声明的，或没有声明时从文本推导的）、当前位置可见的局部名、注册表里的函数和形式；按"局部名 → 参数 → 函数 → 形式"再按名字排序；`@` 之后是契约里的枚举成员 |
| 签名提示 | 正在输入的调用，靠词法段找到，所以写到一半也能工作 |

另外有几个 FunRoute 自己的扩展：

- `funroute/setContract`（通知）：宿主把契约推给服务。契约是宿主的数据，不写在文本里。
- `funroute/syntaxTree`（请求）：带区间的具体语法树，供结构视图使用。
- `funroute/arguments`（请求）：程序要的参数，按顺序给出名字、类型和说明；契约没有声明时是从文本推导出的，试运行面板据此列出输入框。
- `funroute/catalog`（请求）：注册表里的函数与形式及宿主写的说明，供函数说明与块面板使用。
- `workspace/executeCommand`：`funroute.run` 用给定参数运行程序；`funroute.render` 把契约写成注释附在规则上方。

**两种运行方式，同一份代码**：

```bash
funroute lsp -manifest registry.json     # stdio，给 VS Code 这类编辑器
make wasm                                 # web/dist/funroute.wasm，在浏览器的 Worker 里运行
```

### 签名清单：没有实现也能检查

浏览器里跑不了宿主的深度模型或网络调用，开发工具里通常也不应该链接它们。语言服务需要的只是函数的**签名**：

```go
manifest := hostRegistry.Manifest()   // 导出：签名、成本、Doc、句柄、形式
json.Marshal(manifest)                 // 交给语言服务

base := lang.CoreRegistry()            // 内核 + 标准库是原生实现
std.Register(base)
manifest.Apply(base)                   // 宿主的函数只登记签名
```

- 类型检查、悬停、补全、签名提示都照常工作。
- 运行时调用只有签名的函数，会返回 `lang.ErrUnavailable`（它同时也是 `ErrExtension`，所以 `fallback` 会照常兜底）。`lang.TrackUnavailable(ctx)` 会记下这次运行调用了哪些这样的函数，试运行的结果里会标出来。
- 清单和真实注册表的签名或成本不一致时，`Apply` 会拒绝。
- **部署用的 artifact 由宿主用真实注册表编译**：宿主的纯函数如果标了 `Constexpr`，两边的常量折叠结果会不同，digest 也会跟着不同。

## 策略工作台

```bash
make run        # 构建前端与 wasm，组装 site/，然后启动静态服务：http://127.0.0.1:8080
```

工作台是纯静态页面：语言服务以 WebAssembly 的形式在 Worker 里运行。`make site` 把要发布的文件组装到 `site/`，`cmd/mvp` 只负责提供这个目录，GitHub Pages（`.github/workflows/pages.yml`）上传的也是它，所以本地能跑的就是线上发布的。

- **编辑器**：CodeMirror 接上语言服务，高亮、诊断、补全、悬停、签名提示、格式化都来自服务端。`⌘/Ctrl + Enter` 运行。
- **结构视图**：同一段文本的投影。`switch`、列表推导、`reduce`、`let`、`if`、`fallback` 画成卡片，其余部分是一行源码。这里的每一处修改，都是对原文某个区间的替换；选中一个块再点某个表达式，就用这个块把它包起来。
- **契约面板**：类型别名和参数（名字、类型、说明），推送给语言服务。
- **试运行**：顶部声明返回类型与说明（契约的返回部分放在它描述的结果旁边）；每个参数带类型与说明，值按 JSON 填写，原样交给服务端解码，大整数也不会丢精度；结果标出成功、失败或“有函数在浏览器里没有实现”，并给出结果类型与耗时。

前端是 TypeScript（`web/src/`），用 esbuild 打包到 `web/dist/`。产物不提交：`make site`（`make run` 与 Pages 都经过它）会先构建；只用 Go 的语言、CLI 与语言服务不需要 Node。每个组件单独成一个模块，公共部分拆成共享 chunk，别的页面按需引用即可：`lsp.js`（`startClient`，可传入自己的 Worker）、`editor.js`（`createEditor`，样式自带）、`contract.js`（`<fr-contract>`）、`runner.js`（`<fr-runner>`）、`canvas.js`（`<fr-structure>`）；`app.js` 是把它们组装起来的工作台。颜色只来自 `web/tokens.css`，用组件的页面引入它就有亮暗两套主题。编辑器的行为向 VS Code 看齐（Tab 接受补全、括号自动闭合、Alt 点击加光标、Shift+Alt 拖出列选择），按键用 Emacs 的（`C-a`/`C-e`/`C-k`/`C-y`、`C-s` 搜索、`M-/` 补全、`M-;` 注释、`C-/` 撤销，另有 `⌘/Ctrl+Enter` 运行、`Shift+Alt+F` 格式化）；浏览器自己占着的键（如 `C-w`、`C-n`、`C-t`）拿不到。运行时依赖只有 Lit、CodeMirror 和它的 Emacs 键位（`@replit/codemirror-emacs`），只用在前端；Go 这边仍然是零第三方依赖。

示例定义在 `web/funroute-examples.json`，每条都带契约、样例入参和期望结果。测试会通过语言服务逐条运行，并要求这些示例合起来覆盖示例注册表的全部函数和形式、全部运算符和全部节点种类 —— 新增了能力却不补示例，CI 会失败。

## 性能

VM 是栈式字节码解释器，**执行本身不分配内存**，只有程序构造的数据（比如推导式产出的数组）才分配。

Apple M5，`go test ./lang/internal/compile -bench . -benchtime 1s -count 5`，取中位数：

| 场景 | 耗时 | 分配 |
|---|---|---|
| 简单表达式 `RunValues` | 179 ns | 0 |
| 简单表达式 `Run(map)` | 216 ns | 0 |
| 简单表达式 `Program.Run`（从宿主 struct 读参数） | 212 ns | 0 |
| record 进、record 出：`ToValue` + `RunValues` + `FromValue` vs `Program.Run` | 2.77 µs vs 0.76 µs | 20 次 vs 5 次 |
| 200 层嵌套算术 | 5.7 µs | 0 |
| 500 元素 `reduce` | 44 µs | 2 次 / 4 KB |
| 500 元素推导式映射 | 44 µs | 5 次 / 8 KB |
| 500 元素嵌套推导式 | 77 µs | 8 次 / 12 KB |
| 向量透传，n = 16 / 1024 / 65536 | 271 / 271 / 271 ns | 4 次 |
| `Logic` 注册的函数调用 vs 内核 `add` | 329 ns vs 189 ns | 6 次 vs 0 |
| 编译（含推导与常量折叠） | 43 µs | 625 次 / 93 KB |
| 模型调用（模拟 20 µs 引擎开销）：单条 vs 64 条一批 | 27.6 µs vs 1.03 µs / 请求 | — |

向量透传的耗时与长度无关，说明容器从宿主到扩展函数全程没有拷贝。

与通用求值器 `expr` v1.17.8 同机对照（与上表同一时段测量，`expr` 用 struct 环境，复现方法见 [`docs/roadmap.md`](docs/roadmap.md)「执行性能基线」）：算术 `amount * bps / 10000 + fixed`，FunRoute 161 ns / 0 次分配（`Program.Run` 从宿主 struct 读参数时 205 ns / 0 次），expr 70 ns / 5 次分配；64 元素 filter+sum，FunRoute 6.0 µs / 8 次分配，expr 3.4 µs / 168 次分配。单次延迟 expr 快 1.7–2.3 倍，主要差在调用协议（它把 `a * b` 编成一条内联指令，我们编成一次函数调用）。提速方案见 [`docs/roadmap.md`](docs/roadmap.md) 阶段 5。

## 现状

已完成：解析、宿主契约、类型推导与重载、编译期求值、字节码 VM、Artifact digest、record、nominal 枚举与穷尽检查、推导式与 `reduce`、句柄与模型批处理、超时与 `fallback`、类型化错误、标准库、格式化器、签名清单、语言服务（stdio 与 WebAssembly）和工作台。

用于真实支付前还缺：`money` 类型、显式业务错误、决策 trace。语言服务还缺错误恢复（写到一半的程序目前只能给出词法层面的事实）和对表达式内部注释的格式化。详细计划与取舍见 [`docs/roadmap.md`](docs/roadmap.md)。

为什么语言必然终止、最坏延迟为什么有多项式上界、以及"如果要图灵完备该怎么加"，见 [`docs/termination.md`](docs/termination.md)。

## 开发

```bash
make ci        # 格式、前端类型检查与构建、vet（含 js/wasm）、lint、build、Go 与 JS 测试，提交前必须全过
make test      # Go 测试
make wasm      # 浏览器用的语言服务：web/dist/funroute.wasm
make web       # 前端产物：web/dist/*.js（需要先在 web/ 里 npm install；不提交）
make test-js   # 前端纯逻辑与 wasm 会话测试（node --test）
make site      # 组装发布目录 site/（make run 与 Pages 都用它）
make run       # 启动工作台
go test ./lang/internal/compile -bench . -benchtime 2000x   # VM 基准
```

`make lint` 强制三条预算：函数不超过 50 行、嵌套不超过 3 层、文件不超过 800 行。

代码结构：

```text
lang/lang.go              公开接口
lang/internal/machine/    值、类型、字节码、VM、注册表、目录、签名清单   （不依赖上层）
lang/internal/syntax/     词法、语法、AST、ExprJSON、词法段、格式化、语法树（只依赖 machine）
lang/internal/compile/    推导、编译、常量折叠、契约、Analyze          （依赖 syntax + machine）
lang/lsp/                 语言服务：协议、stdio 传输
extensions/std/           标准库，只用公开 API
examples/payment/         示例宿主
web/src/                  工作台前端（TypeScript），每个组件一个入口，打包到 web/dist/
web/wasm/                 浏览器用的语言服务入口（js/wasm）
cmd/funroute  cmd/mvp     CLI（含 fmt、lsp）与工作台静态服务
```

改动时的同步点与必须守住的不变量记在 [`CLAUDE.md`](CLAUDE.md)。
