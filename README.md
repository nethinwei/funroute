# FunRoute

FunRoute 是一门给**支付路由规则**用的小语言：强类型、纯表达式、必然终止。

一条规则就是一个表达式，比如：

```text
switch(country,
  case "SG"       => "adyen_sg",
  case "MY", "TH" => "adyen_asia",
  else => "stripe_global")
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

CLI 的子命令：`inspect`（签名与指令数）、`run`（执行）、`compile`（输出 artifact）、`export`（输出 ExprJSON）、`fmt`（格式化）、`lsp`（语言服务，见[语言服务](#语言服务)）。`-types 'a=int,b=float'` 给参数类型，`-alias` 声明类型别名，`-tables settlement` 声明具名汇率表（见[契约](#契约)）。金额由 `-currencies`（缺省 `iso`，即 ISO 4217；`none` 不声明）与 `-rounding`（缺省 `half_up`）声明；`run -rates 'USD/JPY 150; @settlement; USD/JPY 149.5'` 给换汇用的汇率，写法与工作台的汇率面板相同，用 `;` 分隔，`@名字` 开始一张具名表。`lsp` 不带 `-manifest` 时按 ISO 4217 与 `half_up` 声明金额，带清单时只用清单里的金额声明。

## 语言导览

### 值与类型

| 类型 | 字面量 | 说明 |
|---|---|---|
| `bool` | `true` | |
| `int` | `42`、`1_000_000` | 有符号 64 位，溢出报错 |
| `float` | `0.25` | float64，拒绝 NaN / Infinity |
| `string` | `"SGD"` | 必须是合法 UTF-8（原文与转义结果都查），JSON 风格转义 |
| `array<T>` | `[1, 2, 3]` | 元素同型 |
| `dict<T>` | `{"primary": 1}` | 键是字符串，值同型 |
| `record{…}` | `{amount: 1200, currency: "SGD"}` | 字段固定、各有类型，见[记录](#记录) |
| `enum<name>{a,b}` | `@adyen` | 只能由契约声明，见[枚举](#枚举) |
| `money<USD>` / `money<c>` / `money<?>` | `USD 1.70` | 某币种的最小单位整数，注册表声明币种后才有，见[金额](#金额) |
| `rate` | `2.9%`、`25bps` | 定点比例，精度 1e-10，只和金额、比例一起用 |
| `fxrate<A,B>` | `150 JPY / USD` | 1 A 换多少 B，精确有理数；也来自契约或异币种金额相除 |
| `currency<USD>` / `currency<c>` / `currency<?>` | `USD` | 注册表声明的币种，写它的代码即可 |

几条规则：

- 字符串与数值之间不做隐式转换，用 `int(...)`、`float(...)`、`string(...)`、`bool(...)` 显式转换。
- 空的 `[]` / `{}` 需要从上下文（函数签名或契约）得到元素类型。
- 取整函数（`ceil`/`floor`/`round`）返回 `int`。要 float 就写 `float(floor(x))`。
- **没有 null**。数组越界、字典缺键、除零都是错误。要兜底就写出来：`get(d, "k", 0)`。
- 金额不要用 `float`：注册表声明币种后用 `money`，见[金额](#金额)。

### 取值范围

每种类型能表示什么、在哪里会不精确、超出时怎样，一张表说完。超出范围一律是错误（能在编译期算出的在编译期报），从不悄悄回绕或截断。

| 类型 | 范围与精度 | 精确吗 | 超出或非法时 |
|---|---|---|---|
| `bool` | `true` / `false` | — | — |
| `int` | −9,223,372,036,854,775,808 ~ 9,223,372,036,854,775,807（约 ±9.22×10¹⁸） | 精确 | 溢出、除零是 `ErrArithmetic`；整数除法向零截断 |
| `float` | IEEE 754 float64，约 ±1.8×10³⁰⁸，15–17 位有效数字 | 不精确：`0.1 + 0.2` 是 `0.30000000000000004` | NaN / Infinity 进不来，算出来也报错 |
| `string` | 任意长度的合法 UTF-8 | 精确 | 下标与 `len` 按码点数 |
| `array<T>` / `dict<T>` | 长度只受内存与 fuel 限制；字典的键是字符串 | — | 越界、缺键报错 |
| `record{…}` | 字段固定，按声明顺序存放 | — | 缺字段是另一个类型 |
| `enum` | 契约声明的成员 | — | 不是成员就不是这个类型 |
| `money` | int64 个最小单位；能表示多少主单位取决于币种的小数位，见[金额的表达能力](#金额的表达能力) | 精确 | 溢出报错；位数超过币种小数位报错 |
| `rate` | 十位小数定点（最小 0.0000000001），范围约 ±922,337,203.6854775807 | 在十位小数内精确 | 字面量超过十位小数、结果超范围报错 |
| `fxrate<A,B>` | 任意正有理数，另带两边币种；文本至多 40 位数字；同一币种之间恒为 1 | 精确：`150.25` 就是 15025/100，异币种金额相除得到的也是精确比值 | 零或负数、同币种却不是 1 都是 `ErrArithmetic` |
| `currency` | 注册表声明的代码：大写字母开头，3–8 位大写字母或数字 | — | 未声明是 `ErrCurrency` |

金额用 `money`、比例用 `rate`，不要用 `float`：`float` 是二进制近似，适合评分、概率这类本来就不精确的量。

### 运算符

运算符只是函数调用的简写，`a + b` 就是 `add(a, b)`。优先级从低到高：

| 优先级 | 运算符 | 对应函数 |
|---|---|---|
| 1 | `\|\|` | `if(a, true, b)`（短路） |
| 2 | `&&` | `if(a, b, false)`（短路） |
| 3 | `==` `!=` | `eq`，`!=` 是 `eq` 取反 |
| 4 | `<` `<=` `>` `>=` `in` | `lt` `le` `gt` `ge` `member` |
| 5 | `->` | `convert`（换汇，见[换汇](#换汇与汇率表)；只在声明了金额的注册表里有） |
| 6 | `+` `-` | `add` `sub` |
| 7 | `*` `/` `%` | `mul` `div` `mod`（数字字面量后的取模要空格：`10 % 3`；`10%` 是比例） |
| 8 | `!` `-`（一元） | `if(a, false, true)`、`sub(0, a)` |
| 9 | `xs[i]` `d["k"]` | `at` |

二元运算符左结合，括号可改变优先级。`&&` 和 `||` 展开成惰性的 `if`，所以天然短路 —— `x != 0 && 10 / x > 2` 不会除零。`->` 比 `+`/`-` 松、比比较紧：`fee + amount -> JPY` 换的是和，`amount -> JPY > cap` 比的是换后的金额，`(amount -> JPY) * 2` 要括号。

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
  else => "stripe_global")

// 条件链：不写主体，每个 case 是一个布尔条件，替代嵌套 if
switch(
  case amount > 10_000 => "manual_review",
  case risk > 0.8      => "reject",
  else => "auto")
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
- **字段更新**写作 `order with {amount: order.amount - fee}`：复制一份记录，替换写出的字段，其余原样保留。结果的类型就是原记录的类型，所以 `Order` 进、`Order` 出。只能替换已有字段、新值必须是该字段的类型，字段名不存在或类型不同都是编译错误。`with` 是后缀，和 `.字段`、`[下标]` 同一层：`orders[0] with {fee: 0}`、`(order with {fee: 0}).amount`，别的表达式作基准要加括号；花括号里至少写一个字段。嵌套字段靠嵌套来改：`b with {customer: b.customer with {amount: 1}}`。

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

### 金额

宿主用 `Registry.DeclareMoney` 声明币种（代码与小数位）和默认舍入方式后，语言多出四种类型。没有声明的注册表里它们不存在，不写金额的程序编译出的 artifact 连 digest 都与以前逐字节相同。金额与比例的**写法**属于语法，与是否声明无关：在任何注册表里 `10%3` 都是语法错误，形如币种代码的名字（`URL`）都不能当变量。

```go
registry.DeclareMoney(lang.MoneySpec{Rounding: lang.RoundHalfUp, Currencies: std.ISO4217()})
```

#### 金额的表达能力

金额是 int64 个最小单位，上限固定是 9,223,372,036,854,775,807 个最小单位，能表示多少主单位取决于币种的小数位（声明时限定 0–8 位）：

| 小数位 | 例子 | 最小单位 | 最大金额（最小值是它的相反数再减一个最小单位） |
|---|---|---|---|
| 0 | JPY、KRW、VND | 1 | 9,223,372,036,854,775,807（约 9.2×10¹⁸） |
| 2 | USD、EUR、CNY | 0.01 | 92,233,720,368,547,758.07（约 9.2×10¹⁶，全球财富总量约 4.7×10¹⁴ 美元） |
| 3 | KWD、BHD | 0.001 | 9,223,372,036,854,775.807（约 9.2×10¹⁵） |
| 6 | USDT、USDC（按业务精度声明） | 0.000001 | 9,223,372,036,854.775807（约 9.2×10¹²） |
| 8 | BTC、ETH（按业务精度声明） | 0.00000001 | 92,233,720,368.54775807（约 9.2×10¹⁰） |

不同运算的精确程度与出错条件：

| 情况 | 结果 | 精确吗 | 何时报错 |
|---|---|---|---|
| 字面量 `USD 1.70`、宿主输入 `"USD 1.70"` | 最小单位整数 | 精确；位数超过小数位**报错不舍入**（`USD 1.001`） | 超出上表范围 |
| 加减、比较、`abs`、取负 | 同币种金额 | 精确 | 溢出是 `ErrArithmetic`；币种不同是 `ErrCurrency` |
| `money × int` | 同币种金额 | 精确 | 溢出 |
| `money × rate`、`money / rate` | 同币种金额 | 128 位中间结果，**只在最后舍入一次**到最小单位（注册表默认舍入，或 `round(…, @mode)` 指定） | 只有最终结果超出范围才报错，中间值再大也不会 |
| `amount -> JPY`（换汇） | 目标币种的金额 | 这一对货币的汇率（精确有理数）按两币种小数位差缩放后**只舍入一次**；连写的 `-> CNY -> USD` 每一跳各舍入一次 | 结果超出目标币种的范围；没有汇率表或表里没有这一对是 `ErrNoRate` |
| 比例之间的加减乘除 | `rate` | 十位小数，银行家舍入 | 结果超出 `rate` 范围、除以零 |
| `money / money`（同币种） | `rate`（如实际费率） | 十位小数，银行家舍入 | 比值超出 `rate` 范围（约 ±9.2 亿） |
| `money / money`（不同币种） | `fxrate`（成交汇率） | 精确，不舍入 | 零或异号的金额是 `ErrArithmetic` |
| `money / int`、`money × money`、`money × float` | 编译错误，并提示 `allocate`、`2.9%` 或 `rate("0.029")` | — | — |
| `allocate(m, [权重])`、`allocate(m, n)` | 几笔同币种金额 | 一分不丢：各份向零取整，剩下的最小单位缺省按最大余数法发出（被舍掉最多的份先得），末尾可写策略 `@largest_weight`、`@in_order`、`@reverse_order`、`@all_first`、`@all_last` | 份数超过 10000、权重为负或全为零、权重之和溢出 |
| `prorate(m, part, whole)` | 同币种金额 | `m × part / whole`，128 位中间结果，**只舍入一次**；part 与 whole 可以是整数，也可以是另一币种的两笔同币种金额 | whole 为零；part 与 whole 币种不同 |
| `round_to(m, 粒度)` | 同币种金额 | 取到粒度（同币种的正金额，`CHF 0.05`）的整数倍，舍入一次 | 粒度不大于零、币种不同 |
| `fxrate × rate` | 同一货币对的 `fxrate` | 精确（加点或折让），用它换汇只舍入一次 | 结果不为正；同币种汇率不为 1 |
| `sum`、`cumsum` | 同币种金额 | 精确 | 总和溢出 |
| `avg`、`median` | 同币种金额 | 舍入一次到最小单位 | 从不因总和溢出而失败：先除后加，平均值总在最小与最大之间 |
| 不带币种的金额 | 只有零（字面量 `0`、`lang.Money{}`） | — | 不带币种却不是零是 `ErrCurrency` |

所以金额只在这些地方舍入到最小单位：乘除比例、换汇、`prorate`、`round_to`、`avg`/`median`，方式由注册表的默认值或 `round(…, @mode)` 决定。`allocate` 不舍入：它向零取整后把差额按策略发完。比例之间的运算（金额相除得到比例、比例相乘相除）舍入到十位小数，方式固定为银行家舍入，`round` 不改变它。汇率从不舍入。其余运算要么精确，要么报错。

**加密货币按业务精度声明**：没有 ISO 代码（ISO 24165 的 DTI 是给监管报送的标识符，不是币种代码），代号由宿主定，稳定币 `USDT`/`USDC` 取 6 位，`BTC` 8 位，`ETH`/`SOL` 这类链上 9–18 位的取 8 位 —— 按 wei 计 int64 只放得下约 9.2 ETH。链上的精确金额由宿主的记账系统在边界换算与对账，规则只按业务精度路由与计费；同一代币在不同链上是不同资产时，用不同的代号（`USDTTRX`，代号只能是大写字母与数字）。示例宿主 `examples/payment` 就是这样声明的。

**金额与普通计算按类型区分，逐个运算判定，没有模式。** 一个运算的操作数里有 `money`，它就按金额规则执行，否则一切照旧，同一条规则里两者可以并存：

```text
let(
  fee = amount * 2.9% + USD 0.30,        // 金额：精确，落到分时舍入
  ok  = risk < 0.5 && len(retries) < 3,  // 普通：float 与 int
  if(ok && fee < cap, "adyen", "stripe")
)
```

**类型与字面量**

| 类型 | 写法 | 说明 |
|---|---|---|
| `money<USD>` | `USD 1.70`、`USD -1.70`、`JPY 100` | 币种写死；负号写在数上；代码与数之间只能是空格或 Tab（不能换行、不能有注释）；位数超过币种的小数位是编译错误 |
| `money<c>` | 契约里声明 | 币种变量（小写）：同一个 c 的金额同币种，每次运行由参数绑定；不同的变量（c 与 d）可能绑到同一币种 |
| `money<?>` | 契约里声明、或运算产生 | 币种要到运行时才知道，值自己带着币种。`?` 必须写出来：裸的 `money` 不是类型，报错会提示写 `money<?>`（`currency<?>`、`fxrate<?,?>` 同理），所以读契约的人看得出这是"币种不限"而不是漏写 |
| `rate` | `2.9%`、`25bps`、`0.029` | 小数字面量和金额或比例一起运算时就是 `rate`，和 float 一起时仍是 float |
| `fxrate<A,B>` | `150.25 JPY / USD`、`fxrate<?,USD>` | 字面量读作"1 USD 换 150.25 JPY"；契约里一边或两边可以是 `?`（运行时才知道） |
| `currency<USD>`、`currency<c>`、`currency<?>` | `USD`、`JPY` | 币种字面量；可进 `switch`、作字典的键、和 `==` 比较 |

**币种就是币种代码**：形如币种代码的名字（大写字母开头、共 3–8 位大写字母或数字）在表达式里就是那个币种：`money(170, USD)`、`currency(m) == USD`、`switch(currency(m), case USD => …, case JPY => …, else => …)`。它不是枚举，不写 `@`。所以变量、局部名和参数都不能取这种形状的名字（`USD`、`URL` 都不行）；枚举成员（`@CARD`）与记录字段（`order.USD`）不受影响。

**汇率字面量** `150 JPY / USD` 是一个字面量，不是两次运算：数在前、中间是报价币种、`/` 后是基准币种，写在同一行、各部分之间只能是空格或 Tab；数是不带指数的十进制文本，按写下的样子精确读入，小数位不受币种限制（`0.00000000065 USD / JPY` 也行），写在任何能写表达式的地方。同一币种之间恒为 1：`1 USD / USD` 合法，`150 USD / USD` 是编译错误。

**`%` 紧贴在数字后面就是比例，没有例外**（带指数的数也一样，而比例不能带指数，所以 `1e3%` 与 `1e3%3` 都是语法错误）：`2.9%`、`10%-3%`（即 `10% - 3%`）、`2.9%-fee`（比例减费用）都是比例。取模要在 `%` 前留空格（`10 % 3`），或者 `%` 前不是数字字面量（`x%3`、`(10)%3`）。`10%3`、`10%x` 这种比例后面紧跟操作数的写法是语法错误，报错会说明原因，不会悄悄读成取模。字面量 `0` 可以当任意币种的金额（`amount > 0`、`if(x, fee, 0)`），也可以当比例（`fee > 0`、`-fee`），别的整数不行。

**比例只为金额服务**：`rate` 只和金额、比例一起运算。比例之间可以加减乘除、比较（`100% - fee`、`fee * 2.5%`、`fee < 5%`），金额可以乘除比例；整数与比例之间没有任何运算，也没有 float 与比例的互转。所以 `1 - fee` 要写成 `100% - fee`，`n * fee` 先把 `n` 用在金额上（`amount * n * fee`），编译错误会这样提示。要从文本读比例用 `rate("0.029")`。

**乘除按量纲推导**：结果只能是金额、比例或汇率，其余是编译错误。

| 运算 | 结果 |
|---|---|
| `money<c> ± money<c>`、比较 | 同币种；币种不同是错误 |
| `money<c> ± money<d>`、比较（两个不同的契约变量） | 编译通过，运行时比对：两者常常就是同一币种（同币种退款） |
| `money<c> * int`、`int * money<c>` | 精确的 `money<c>` |
| `money<c> * rate`、`rate * money<c>`、`money<c> / rate` | `money<c>`，落在两个最小单位之间时舍入 |
| `rate ± rate`、`rate * rate`、`rate / rate`、比较 | `rate` |
| `money<c> / money<c>` | `rate`（实际费率） |
| `money<B> / money<A>` | `fxrate<A,B>`（成交汇率：到账额 ÷ 支付额）；`money<c> / money<d>` 也读成 `fxrate<d,c>` |
| `fxrate<A,B> * rate`、`rate * fxrate<A,B>` | 同一货币对的 `fxrate`（加点或折让） |
| `fxrate<A,B>` 之间比较 | `bool`：哪个换到的更多；货币对不同是错误 |
| `money / int`、`money * money`、`money * float`、`int` 与 `rate` 混合 | 编译错误，并提示改用 `allocate`、`100% - fee` 等 |

两边币种都不知道的 `money / money` 读成 `rate`，运行时检查同币种；契约的返回类型或 `using` 要一个汇率时才读成 `fxrate`。

**汇率只有两种运算：加点与比较**。`fxrate` 可以作结果、进容器与记录、乘比例得到同一货币对的加点汇率（`fx(EUR, USD) * (100% + 3.5%)`，精确，换汇时只舍入一次——比先换汇再乘比例少一次舍入）、同一货币对之间比较大小与相等（不同货币对是编译错误或 `ErrCurrency`），但不能乘金额，也不能相乘、交叉、取倒数、求平均。换汇只有一种写法：`->`。`fx(基准, 报价)` 读出本次运行的汇率表里那对货币的汇率，与 `->` 用的是同一条报价。

#### 换汇与汇率表

`amount -> JPY` 把金额换成日元，结果是 `money<JPY>`；目标也可以是一个 `currency` 值，`amount -> target` 的结果币种要到运行时才知道。它读本次运行的汇率表：Go 宿主给 `RunOptions.Rates`，工作台在试运行面板的"汇率表"里填。

- **单跳**：每个 `->` 只用这一对货币的汇率：表里这个方向的报价，或者反方向报价的倒数（两个方向都报了价时各用各的）。它从不自己找中转货币，表里只有 USD/JPY 与 USD/EUR 时，`amount -> EUR` 在 JPY 金额上是 `ErrNoRate`。所以规则换汇用的是哪条汇率，看规则就知道，与表里还有哪些报价无关。
- **要经过别的货币就连写**：`amount -> USD -> EUR`、`JPY 150 -> CNY -> USD`。`->` 左结合，每一跳是一次独立的换汇，落到中间币种的最小单位时舍入一次，中间的 CNY 是一笔真实的金额（清算里实际发生的就是它）。每一跳的舍入方式同样由 `round(…, @mode)` 选：`round(amount -> USD -> EUR, @down)`。要整段只舍入一次，就把交叉汇率算好放进表里（Go 侧用 `FxRate.Chain` 精确相乘），或在规则里用 `using(fx(JPY, CNY)` 之类读出的汇率算好再换。
- **换不了就是 `ErrNoRate`**：没有汇率表、或表里没有这一对货币的汇率（两个方向都没有）。它和扩展函数失败一样说明数据暂时不可得，所以 `fallback` 接它。兜底值要让下游认得出来：兜底成 `JPY 0` 的金额在成本比较里会被当成最便宜的通道，应连同一个标记一起返回（`fallback({jpy: amount -> JPY, priced: true}, {jpy: JPY 0, priced: false})`），或者整段换成协议价。已是目标币种的金额与不带币种的零不需要汇率，没有汇率表也原样返回。
- `->` 读运行时的汇率表，不在编译期折叠，写死的 `USD 1 -> JPY` 也留到运行时算。

#### 局部汇率：`using`

`using(汇率, …, 主体)` 让主体里的 `->` **只**按这些汇率换汇：运行时的汇率表（`RunOptions.Rates`）与外层的 `using` 都不参与，所以规则换汇用的是哪条汇率，看 `using` 本身就知道。汇率是任意 `fxrate` 表达式：字面量 `150.25 JPY / USD`、成交汇率 `settled / paid`、契约参数，或者从外层读出来的 `fx(USD, JPY)`——要沿用外层的某个汇率，就这样把它写进来。汇率在进入主体之前、在 `using` 外面求值，所以 `fx` 读的是外层。同一货币对写了两次时后写的为准。

| 写法 | 含义 |
|---|---|
| `using(150 JPY / USD, amount -> JPY)` | 主体只用这个汇率 |
| `using(settled / paid, fx(USD, JPY), refund -> EUR)` | 成交汇率，加上从外层沿用的 USD/JPY；表里的其他货币对不参与 |
| `using(fx(EUR, USD) * (100% + 3.5%), amount -> USD)` | 读出外层汇率再加点 |
| `using(@settlement, amount -> JPY)`、`using(@settlement, 151 JPY / USD, …)` | **具名表**：以契约声明的一张汇率表（`CompileOptions.RateTables`，文本契约的 `tables`）为底，后面写的汇率盖在它上面 |

```text
using(settled / paid, refund -> EUR)                          // 退款按原交易的成交汇率换
fallback(amount -> JPY, using(150 JPY / USD, amount -> JPY))  // 市价换不了时，整段改用备用汇率
```

`fallback` 加 `using` 是**整段备选**：备选那一段只用它自己的汇率。同一币种之间的汇率是恒等（`1 USD / USD` 写了也不改变什么）。`using` 可以嵌套，内层同样只用它自己写的汇率。`using` 是保留字，只在声明了金额的注册表里有意义。

具名表让一条规则同时用几张汇率表，比如市场价与清算价：契约声明表名，宿主运行时用 `RunOptions.RateTables` 按名字传进来，每张表取运行开始时的版本；契约声明了而宿主没传的表是空表，在上面换汇是 `ErrNoRate`；传了契约没声明的名字是 `ErrContract`。`@settlement` 在契约的枚举 `rate_table` 里解析（与契约自己的枚举成员重名时写 `@rate_table.settlement`），所以契约不能再声明叫 `rate_table` 的枚举。

**币种检查**：两边币种都已知时编译期检查：两个不同的代码，或代码碰到契约变量，都是编译错误（`amount + USD 1` 在 `amount: money<c>` 下——规则只在 c 恰好是 USD 时才成立，契约本该写 `money<USD>`）；两个不同的契约变量（`money<c>` 与 `money<d>`）编译通过、运行时比对；任一边要到运行时才知道也是编译通过、运行时比对。运行时币种不一致、币种未声明都是 `ErrCurrency`（参数的币种不对同时也是 `ErrContract`，`Run` 与 `RunValues` 一样；宿主函数的结果违反它自己签名的币种也是 `ErrCurrency`），`fallback` 不接它。相等比较与 `in` 同样如此：`usd == eur`、`usd in [eur]` 两边都已知时编译期报错；容器里的金额与单个金额一样，`[u] == [e]` 在两边币种都要到运行时才知道时，币种不同是 `ErrCurrency` 而不是 `false`；比较两个 `currency` 值本身就是在问是否同一币种，永远有答案。

**舍入**：默认方式由注册表声明，每一步落到最小单位时就舍入一次（写法即舍入点）。`round(expr, @half_even)` 让 expr 里每一步都改用指定方式；里面没有舍入步骤是编译错误。嵌套时每一层只管自己的步骤：`round(round(m * 50%, @up), @down)` 的外层什么也没舍入，同样是编译错误。方式有 `half_even`、`half_up`、`half_down`、`down`、`up`、`ceiling`、`floor`，它们是注册表提供的枚举 `rounding` 的成员，所以契约不能再声明叫 `rounding` 的枚举。

**函数**：`money(最小单位, 币种)` 由整数与币种造出金额（输入里两者分开时用：`money(o.amount, o.currency)`，`o.currency` 声明为 `currency<?>`），`currency_of(字符串)` 校验币种代码，`like(同币种金额, n)` 在同一币种里造 n 个最小单位，`minor`、`currency`、`sign` 把金额拆开（`minor(...)` 得到的是普通整数：拿它造回另一币种的金额，比如 `money(minor(a), JPY)`，在语言里是允许的，币种对不对由规则作者负责），`string(币种)` 得到代码，`allocate(m, [权重])` 与 `allocate(m, n)` 分摊且一分不丢（剩下的最小单位缺省按最大余数法发出，末尾写 `@all_last` 这类策略可以改；策略是注册表提供的枚举 `allocation` 的成员，契约不能再用这个名字），`prorate(m, part, whole)` 按比例取金额且只舍入一次，`round_to(m, 粒度)` 做现金舍入，`fx(基准, 报价)` 读运行时的汇率，`rate(字符串)` 把十进制文本精确读成比例（读不出是 `ErrArithmetic`）。标准包的 `sum`/`min`/`max`/`avg`/`median`/`sort`/`cumsum`/`sort_by`/`top_k` 等在声明了币种后也接受金额，并检查同一数组里币种一致。

**宿主那边**：Go 用 `lang.Money`、`lang.Rate`、`lang.FxRate`、`lang.Currency`（JSON 解码只检查形状；币种是否已声明在进入规则的边界上、或经币种表 `Format` 时检查），由币种表构造（见[在 Go 里算钱](#在-go-里算钱)），`[]lang.Money` 与规则之间零拷贝；JSON 输入认 `"USD 1.70"` 与 `{"currency": "USD", "minor": 170}` 两种，汇率是 `{"base": "USD", "quote": "JPY", "rate": "150.25"}`，`Registry.EncodeJSON` 把金额写成 `"USD 1.70"`。

### 语法速查

```text
// ── 词法 ─────────────────────────────────────────────────────────────
// token 之间可以有空白（ASCII 空白字符）和注释（// 到行尾）；token 内部不能有注释，也不能有写出来之外的空白。
// 词法器从左到右每次取尽量长的一段，认成下面的一种 token。一段文字合乎几种写法时按这个次序取：
// 保留字，然后形如币种代码的 code，然后 name；数字后面紧贴 % 或 bps 就是 rate，不是 number。
digits     = digit { [ "_" ] digit }                // 下划线只能夹在两个数字之间：1_000
decimal    = digits [ "." digits ]                  // 数字后的 "." 必须接数字
exponent   = ( "e" | "E" ) [ "+" | "-" ] digits
number     = decimal [ exponent ]                   // 没有 "." 也没有指数是 int，否则是 float；float 必须恰好是写下的值（0.30000000000000001 报错）
rate       = decimal ( "%" | "bps" )                // 紧贴数字的 % 一律是比例的单位，带指数的数也一样（1e3% 报错）；bps 后不能紧接字母或数字
word       = ( letter | "_" ) { letter | digit | "_" }
code       = upper 2*7( upper | digit )             // 大写字母开头、共 3–8 位：币种，不是变量
name       = word { "." word }                      // 名字连同其中的点是一个 token，含义见下文
enum       = "@" word [ "." word ]                  // @member 或 @enum.member
string     = '"' { 字符 | 转义 } '"'                // 转义同 Go，内容必须是合法 UTF-8
保留字     = true false switch reduce let for in else case using with

// ── 解析器查原文的字面量 ───────────────────────────────────────────────
// 它们由几个 token 组成，解析器检查 token 之间的原文：只能是空格与 Tab，不能换行、不能有注释。
hspace     = ( " " | "\t" ) { " " | "\t" }
money      = code hspace [ "-" ] decimal            // USD 1.70、USD -1.70：负号属于数，紧贴数字
fxrate     = decimal hspace code [ hspace ] "/" [ hspace ] code   // 150 JPY / USD：1 USD 换 150 JPY
// 负数字面量也是解析器的规则：一元 "-" 后是单独一个 number 时读成负数，所以 -9223372036854775808
// 写得出来；-150 JPY / USD、-2[0] 仍是对后面的整个 postfix 取负。1-2 是减法：词法器从不把负号读进数字。

// ── 文法 ─────────────────────────────────────────────────────────────
// 每一层只引用更紧的下一层，所以优先级与结合都由文法本身决定。
program        = expression EOF                     // 契约由宿主给出，不在文本里
expression     = or
or             = and { "||" and }
and            = equality { "&&" equality }
equality       = comparison [ ( "==" | "!=" ) comparison ]                        // 比较不结合：a == b == c 是语法错误
comparison     = conversion [ ( "<" | "<=" | ">" | ">=" | "in" ) conversion ]     // a < b < c 同样是语法错误
conversion     = additive { "->" postfix }          // 左结合；右边是一个币种：代码、名字、字段、调用或括号。amount -> JPY + fee 是语法错误
additive       = multiplicative { ( "+" | "-" ) multiplicative }
multiplicative = unary { ( "*" | "/" | "%" ) unary }
unary          = ( "!" | "-" ) unary | postfix      // -USD 1.70 是对金额取负
postfix        = primary { "[" expression "]" | "." word | "with" "{" word ":" expression { "," word ":" expression } [ "," ] "}" }  // 字段更新：order with {fee: 0}
primary        = number | rate | money | fxrate | code | string | "true" | "false" | enum
               | name                               // 变量；第一个点之后是字段读取：order.amount
               | name "(" [ args ] ")"              // 函数调用，名字可带点：route.score_v1(x)
               | "switch" "(" [ expression "," ] case { "," case } ( [ "," ] ")" | "," "else" "=>" expression ")" )
               | "reduce" "(" locals "in" expression [ "if" expression ] "," word "=" expression "," expression ")"
               | "let" "(" binding { binding } expression ")"
               | "using" "(" [ enum "," ] { quote "," } expression ")"   // 约束见下文
               | "[" [ args ] "]"                   // 数组
               | "[" expression loop { loop } "]"   // 列表推导
               | "{" [ string ":" expression { "," string ":" expression } [ "," ] ] "}"      // 字典
               | "{" word ":" expression { "," word ":" expression } [ "," ] "}"              // 记录
               | "{" expression ":" expression loop "}"   // 字典推导，只接一个 loop
               | "(" expression ")"
case           = "case" expression { "," expression } "=>" expression
binding        = word "=" expression ","
loop           = "for" locals "in" expression [ "if" expression ]
locals         = word [ "," word ]
args           = expression { "," expression } [ "," ]
quote          = expression                         // 一个 fxrate；外层的汇率用 fx(base, quote) 读进来
```

金额的负号属于数，紧贴数字写：`USD -1.70`（与宿主文本、`EncodeJSON` 的输出同一种写法）。`-USD 1.70` 不是字面量，是对金额 `USD 1.70` 取负，和 `-x` 一样；`USD - 1` 与 `USD-1` 是币种 `USD` 减 1，类型错误 —— 代码形状的名字永远是币种，不是变量；`HTTP`、`ORDER1` 这样的名字同样不能当变量。

**名字与点**：词法器把 `order.amount` 连同其中的点读成一个名字。后面跟着 `(` 时整个名字是函数名（命名空间与版本：`route.score_v1(x)`，语言没有一等函数，`route` 不是变量）；否则第一段是变量，其后每一段是字段读取，与 `order . amount`、`orders[0].amount` 的字段读取完全一样。变量名本身不能带点。第一段形如币种代码时是币种：`USD.x` 是错误（币种没有字段），`USD(1)` 是名叫 `USD` 的函数调用。

`switch`、`reduce`、`let`、`using` 外形像函数调用，但它们是保留字引出的形式，内部用 `case … =>`、`else =>`、`x in xs`、`acc = init` 标出各个位置；`if`、`fallback` 则是普通的函数名。`if` 同时是推导式与 `reduce` 里筛选子句的开头，所以变量与局部名不能叫 `if`（字段可以）。

`switch` 有主语时，每个 `case` 的值与主语比较相等，几个值任一相等就选这一支；没有主语时每个 `case` 是 `bool` 条件，按顺序取第一个成立的。没有 `else` 只在主语是契约里的枚举、且各 `case` 覆盖了它的全部成员时才能编译，所以运行时不会有"没有分支匹配"的情况。

`using` 的几条约束是静态检查：除了具名表，至少要有一个汇率；具名表 `@name` 写在最前面。`reduce`、推导式的 `if` 与 `for`、`in` 只在这些位置有这个意思。

## 契约

规则文本**只是表达式**。参数叫什么、什么类型、什么顺序、返回什么，由宿主在编译时传入：

```go
result := lang.IntType
artifact, err := lang.CompileExpr(
    `switch(country, case "SG", "MY" => amount * 2, else => amount)`,
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
- 不来自契约类型的枚举有三个：声明了金额的注册表提供的舍入方式（`@half_up` 等，枚举名 `rounding`）与分摊策略（`@all_last` 等，枚举名 `allocation`），以及由契约声明的具名汇率表构成的 `rate_table`（`@settlement`）；契约不能再用这三个名字，契约自己的枚举成员与它们重名时优先解析成契约的。币种不是枚举：写 `USD`，不写 `@USD`。

### 导出视图

规则被贴到工单或聊天里时，读者看不到契约。`lang.RenderWithContract` 把契约写成注释：

```text
// amount:  int              订单金额，单位：分
// country: string           ISO 3166-1 二字码
// →        int              应收总额，单位：分

switch(country, case "SG", "MY" => amount * 2, else => amount)
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
| `Artifact` | 不可变的字节码 + digest，可存储、可传输（JSON）；字段私有，经 `Args()`、`Result()`、`Digest()` 等读取 |
| `Runtime` | 绑定到注册表后可运行的 Artifact |

`lang` 是唯一的公开包。AST 类型刻意不公开，程序一律用 ExprJSON 交换。公开包里的数据类型（`Type`、`Artifact`、`Money`、`FxRate`、`Manifest`、目录……）都没有宿主可写的字段，只能经构造函数、`Parse` 或注册表得到，经访问方法读取，所以宿主拿不到一个不合规则的值；要宿主填写的只有选项结构体（`CompileOptions`、`FunctionSpec`、`Doc`、`MoneySpec`、`RunOptions`、`BatchOptions` 等），它们在使用处校验。

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

### 在 Go 里算钱

规则算出的钱最终还是在 Go 里用：记账、展示、再分摊。`lang.Money`、`lang.Rate`、`lang.FxRate` 带着与语言相同的运算方法，**内核的金额运算就是这些方法**，所以宿主在规则旁边算出的数与规则逐分相同。方法一律返回 `(值, error)`，错误与规则里完全相同：币种不一致是 `ErrCurrency`，溢出、除零、非法汇率、非法舍入方式是 `ErrArithmetic`，找不到汇率是 `ErrNoRate`。

这些类型的字段都是私有的，宿主写不出一个不合规则的值：金额、币种与汇率由**币种表**造出（所以币种一定已声明），或经 JSON 解码得到（解码只检查形状与汇率规则，币种是否已声明在进入规则的边界上、或经币种表 `Format` 时检查），比例由 `ParseRate`/`Percent`/`BasisPoints` 读出，读取一律经访问方法（`m.Currency()`、`m.Minor()`、`fx.Base()`……）。零值只有两个有意义：`lang.Money{}` 是不带币种的零（与规则里的 `0` 一样能和任何币种相遇），`lang.Rate{}` 是 0。它们与 JSON 的往返形状见[金额](#金额)。

要用到币种小数位的操作都在币种表上：`registry.Currencies()` 取规则用的那一张（没声明金额的注册表返回 `nil, false`），不跑规则的服务用 `lang.NewCurrencies(spec)` 按同一个 spec 建一张。

| 类别 | 入口 |
|---|---|
| 造金额 | `table.Parse("USD 1.70")`、`table.Of("USD", "1.70")`、`table.Minor("USD", 170)`；`table.Format(m)` 写回 `"USD 1.70"`，`table.Places("USD")` 是小数位 |
| 读金额 | `m.Currency()`、`m.Minor()`、`m.Sign()`、`m.IsZero()` |
| 加减比较 | `m.Add(o)`、`m.Sub(o)`、`m.Neg()`、`m.Abs()`、`m.Cmp(o)` |
| 乘除 | `m.MulInt(n)`、`m.MulRate(r, mode)`、`m.DivRate(r, mode)`、`m.Ratio(o)`（同币种之比，得比例）、`m.Like(n)`（同一币种的 n 个最小单位）、`m.Prorate(part, whole, mode)`、`m.ProrateBy(part, whole, mode)`（按比例取，只舍入一次）、`m.RoundTo(step, mode)`（现金舍入） |
| 分摊与统计 | `m.Allocate(权重...)`、`m.Split(n)`（一分不丢，剩下的最小单位按最大余数法发出；n 为 1–10000，与规则里的 `allocate` 相同）、`m.AllocateBy(strategy, 权重...)`、`m.SplitBy(strategy, n)`（按 `lang.AllocateAllLast` 等策略发）、`lang.AverageMoney(ms, mode)`、`lang.MedianMoney(ms, mode)`（与 std 的 `avg`/`median` 相同） |
| 比例 | `lang.ParseRate("0.029")`、`lang.Percent("2.9")`、`lang.BasisPoints("25")`；`r.Add`/`Sub`/`Mul`/`Div`、`r.Cmp`、`r.Sign`、`r.IsZero`、`r.String()` |
| 币种 | `table.Currency("USD")`，`c.Code()` |
| 汇率 | `table.FxRate("USD", "JPY", "150.25")`、`table.Implied(over, under)`；`fx.Base()`、`fx.Quote()`、`fx.String()`（`USD/JPY 150.25`，没有有限小数时写分数 `10/7`）、`fx.Decimal(places)`（半偶舍入后的展示文本，汇率本身不变）、`fx.Inverse()`、`fx.Chain(next)`（串联，前一个的报价币种必须是后一个的基准币种）、`fx.MulRate(r)`（加点，与规则里的 `fx * 101%` 相同）、`fx.Cmp(other)` |
| 换汇 | `table.Convert(m, fx, mode)`：按一个汇率换；`rates.Convert(m, "JPY", mode)`：按汇率表里这一对货币的汇率换（单跳，与规则里的 `->` 相同） |

汇率是精确有理数：`"150.25"` 就是 15025/100，`fx.Chain(next)` 是精确的乘积，只有换出来的金额舍入一次。同一币种之间恒为 1：`table.FxRate("USD", "USD", "1")` 合法，别的数是 `ErrArithmetic`。`table.Implied(over, under)` 是一笔交易隐含的汇率——over 是为 under 付出的：`Implied(JPY 15000, USD 100.00)` 是 `USD/JPY 150`，与规则里的 `settled / paid` 相同；两笔金额必须非零且同号。

**汇率表**是规则里 `->` 换汇用的那张表：

```go
table, _ := registry.Currencies()
rates := table.NewRates()
_ = rates.Add("USD", "JPY", "150")          // 1 USD 换 150 JPY；再加同一货币对就是更新
_ = rates.Add("USD", "EUR", "0.92")

euros, _ := table.Parse("EUR 92.00")
dollars, _ := rates.Convert(euros, "USD", lang.RoundHalfEven)   // USD 100.00：USD/EUR 的倒数
yen, _ := rates.Convert(dollars, "JPY", lang.RoundHalfEven)     // JPY 15000；euros 直接换 JPY 是 ErrNoRate
back, _ := rates.Rate("JPY", "USD")                              // JPY/USD 1/150：反向报价的倒数

value, err := runtime.Run(ctx, args, lang.RunOptions{Rates: rates})
```

- `Add` 的汇率是纯十进制文本（不带符号、不带指数、至多 40 位数字），必须为正；同币种只接受 `"1"`，且什么也不做。已有的 `FxRate` 用 `rates.AddRate(fx)` 加入。
- `rates.AddQuote(lang.QuoteSpec{Base, Quote, Rate, Source, At, Until})` 连同来源与时间一起加入，`rates.Quote(base, quote)` 读回宿主给的那条报价（`q.Rate()`、`q.Source()`、`q.At()`、`q.Until()`）。表只记录不判定：过了 `Until` 的报价照样换汇，哪条算过期由宿主决定，在建表时滤掉或不传。
- 任何算出来的汇率（`Implied`、`Chain`、`Rates.Rate`、加点）每边至多约一千位数字，超出是 `ErrArithmetic`；所以每个汇率都能按它自己的 JSON 写出再读回。
- 汇率表并发安全、写时复制：`Add` 发布一个新版本，一次运行（或一次 `Convert`）在开始时取当时的版本，运行中途的 `Add` 不影响它。所以行情线程可以一直更新同一张表，规则照常并发运行。
- `RunOptions.Rates` 对 `Run`、`RunValues`、`Program.Run` 和批处理都一样；汇率表的币种表必须与注册表声明的币种和小数位一致（默认舍入方式可以不同：汇率与它无关），否则是 `ErrCurrency`。不给汇率表时，规则里的 `->` 是 `ErrNoRate`。
- `RunOptions.RateTables` 按名字传入契约声明的具名汇率表（`using(@settlement, …)`），规则同样对每张表取运行开始时的版本；契约声明了但没传的表是空表，传了契约没声明的名字是 `ErrContract`。`artifact.RateTables()` 读出契约声明的表名。

```go
table, _ := registry.Currencies()
amount, _ := table.Parse("USD 120.00")
rate, _ := lang.Percent("2.9")
fee, _ := amount.MulRate(rate, table.Rounding())   // 与规则里的 amount * 2.9% 相同
usdJPY, _ := table.FxRate("USD", "JPY", "150")
yen, _ := table.Convert(fee, usdJPY, lang.RoundHalfUp)
text, _ := table.Format(yen)                       // JPY 522
```

`Parse` 读 `USD -1.70`，与规则里的字面量同一种写法（符号写在数上）；`Format` 写不出的金额（不带币种却不是零）返回 `ErrCurrency`，不会按错的小数位写出一段读不回来的文字。规则的结果经 `value.Money()`、`value.Rate()`、`value.FxRate()`、`value.Currency()` 取出；Go 值交给规则用 `lang.ToValue`。

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
    switch(case model.fraud_v3(emb) > 0.9 => "reject", else => "accept"))
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

`fallback` 只接住扩展函数的失败、超时和换汇找不到汇率（`ErrNoRate`）——这些是数据暂时不可得。前一个候选在 `using` 里失败时，下一个候选从 `fallback` 所在处的汇率重新开始。fuel 耗尽、算术失败、币种不一致、类型错误这些**规则或数据自身的错误**不会被吞掉，即使它们发生在扩展函数里：扩展函数返回的 `ErrCurrency`、`ErrArithmetic`、`ErrNoRate` 保留原来的类别，不会被包成 `ErrExtension`，时间预算耗尽之后返回的也一样。

所有错误都可以用 `errors.Is` 区分：

| 错误 | 含义 |
|---|---|
| `ErrCompile` | 源码、ExprJSON 或类型有误 |
| `ErrContract` | 契约非法，或表达式读了未声明的参数 |
| `ErrDeadline` | 请求或函数的时间预算耗尽 |
| `ErrExtension` | 扩展函数报错 |
| `ErrFuel` | 超出成本上限 |
| `ErrCurrency` | 币种不一致或未声明，汇率表与注册表的币种表不一致；`fallback` 不接它 |
| `ErrNoRate` | 换汇没有汇率表，或表里找不到路径；`fallback` 接它 |
| `ErrArithmetic` | 算术没有答案：溢出、除零、float 非有限、汇率不为正或同币种汇率不是 1、汇率大得写不下，以及转换没有答案（`int("x")`、`int(2.5)`、`rate("abc")`、float 表示不了的 int）；在内核、扩展函数还是 Go 方法里发生都一样，`fallback` 不接它；参数带来的同时也是 `ErrContract` |

编译错误带位置：`errors.As` 取出 `*lang.PositionError`，`Offset()` 与 `Span()` 是字节偏移与区间，`lang.LineColumn(err, source)` 换算成行列。

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

金额同样是能力：`registry.DeclareMoney(spec)` 之后才有金额类型、字面量、金额运算、换汇 `->`（内核函数 `convert`）和 `using`，编译期固化下来的金额事实以金额戳的形式记进用到金额的 artifact：默认舍入方式，以及字节码里写死的每个币种（字面量、常量、类型、检查里的代码）的小数位。默认舍入变了、或这些币种的小数位变了、不再声明了的注册表拒绝装载它；只在运行时才知道的币种（`money<c>`）跟随当前的币种表，所以注册表新增币种、改动 artifact 没有写到的币种，已部署的 artifact 照常装载。宿主要先声明币种、再注册标准包，标准包才会注册它对金额的重载。

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
- 金额与比例字面量是 `{"node":"money","currency":"USD","amount":"1.70"}` 与 `{"node":"rate","value":"2.9","unit":"%"}`，保留写下的十进制文本：折成多少最小单位取决于注册表的小数位，而 ExprJSON 不能依赖注册表。币种是 `{"node":"currency","code":"USD"}`，汇率字面量是 `{"node":"fxrate","rate":"150.25","quote":"JPY","base":"USD"}`，`->` 就是 `convert` 调用；`using` 是 `{"node":"using","quotes":[…],"body":…}`，`fills` 是写在 `...` 之前的汇率，`outer` 表示写了 `...`。

## 语言服务

`lang/lsp` 是一个 [Language Server Protocol](https://microsoft.github.io/language-server-protocol/) 服务。它报告的都是语言本身知道的事实——词法器和解析器对每一段源码的判断、编译器的诊断和类型、格式化器的排版——至于怎么显示，由客户端决定。

| 能力 | 来自 |
|---|---|
| 语义标记 | 词法器与解析器的判断：关键字、运算符、参数、局部名（定义处 / 引用处）、函数、字段、字面量、币种、注释 |
| 诊断 | 编译器，带出错的区间；读了契约没声明的变量，会指到那次读取 |
| 格式化 | 格式化器；打印结果一定能解析回同一个程序，表达式中间有注释时拒绝格式化，而不是丢掉注释 |
| 悬停 | 节点推导出的类型、调用选中的签名、宿主写的函数说明和参数说明 |
| 补全 | 程序的参数（契约声明的，或没有声明时从文本推导的）、当前位置可见的局部名、注册表里的函数和形式；按"局部名 → 参数 → 函数 → 形式"再按名字排序；声明了金额时注册表的币种也是补全项；`@` 之后是契约里的枚举成员与舍入方式 |
| 签名提示 | 正在输入的调用，靠词法段找到，所以写到一半也能工作 |

另外有几个 FunRoute 自己的扩展：

- `funroute/setContract`（通知）：宿主把契约推给服务。契约是宿主的数据，不写在文本里。
- `funroute/syntaxTree`（请求）：带区间的具体语法树，供结构视图使用。
- `funroute/arguments`（请求）：程序要的参数，按顺序给出名字、类型和说明；契约没有声明时是从文本推导出的，试运行面板据此列出输入框。
- `funroute/catalog`（请求）：注册表里的函数与形式及宿主写的说明，供函数说明与块面板使用。
- `workspace/executeCommand`：`funroute.run` 用给定参数运行程序，可带 `rates: [{base, quote, rate}]` 作本次运行的汇率表，失败时给出错误类别（其中找不到汇率是 `norate`）；`funroute.render` 把契约写成注释附在规则上方。

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
- **代码与结构两个页签**：表达式区块里的"代码"是编辑器，"结构"是结构视图，右侧的试运行两边共用。只有当前文本已检查完、没有错误时才能切换，否则停在原页签并在状态栏说明原因。
- **结构视图**：同一段文本的投影。切回代码时文本与写下的一字不差：运算符、推导式、`2.9%` 这类写法原样保留，从不脱糖。`switch`、列表推导、`reduce`、`let`、`using`、`if`、`fallback` 画成卡片，其余部分是一行源码。这里的每一处修改，都是对原文某个区间的替换；选中一个块再点某个表达式，就用这个块把它包起来。
- **契约面板**：类型别名、参数（名字、类型、说明）和具名汇率表的名字，推送给语言服务。
- **试运行**：顶部声明返回类型与说明（契约的返回部分放在它描述的结果旁边）；每个参数带类型与说明，值按 JSON 填写，原样交给服务端解码，大整数也不会丢精度；"汇率表"一行一条报价（`USD/JPY 150.25`，即 1 USD 换 150.25 JPY），是规则里 `->` 用的表，`@settlement` 一行开始一张具名汇率表，其后的报价属于它；有一行写错时不运行，并指出是第几行；结果标出成功、失败或“有函数在浏览器里没有实现”，并给出结果类型与耗时。

前端是 TypeScript（`web/src/`），用 esbuild 打包到 `web/dist/`。产物不提交：`make site`（`make run` 与 Pages 都经过它）会先构建；只用 Go 的语言、CLI 与语言服务不需要 Node。每个组件单独成一个模块，公共部分拆成共享 chunk，别的页面按需引用即可：`lsp.js`（`startClient`，可传入自己的 Worker）、`editor.js`（`createEditor`，样式自带）、`contract.js`（`<fr-contract>`）、`runner.js`（`<fr-runner>`）、`canvas.js`（`<fr-structure>`）；`app.js` 是把它们组装起来的工作台。颜色只来自 `web/tokens.css`，用组件的页面引入它就有亮暗两套主题。编辑器的行为向 VS Code 看齐（Tab 接受补全、括号自动闭合、Alt 点击加光标、Shift+Alt 拖出列选择），按键用 Emacs 的（`C-a`/`C-e`/`C-k`/`C-y`、`C-s` 搜索、`M-/` 补全、`M-;` 注释、`C-/` 撤销，另有 `⌘/Ctrl+Enter` 运行、`Shift+Alt+F` 格式化）；浏览器自己占着的键（如 `C-w`、`C-n`、`C-t`）拿不到。运行时依赖只有 Lit、CodeMirror 和它的 Emacs 键位（`@replit/codemirror-emacs`），只用在前端；Go 这边仍然是零第三方依赖。

示例定义在 `web/funroute-examples.json`，每条都带契约、样例入参和期望结果，用到 `->` 的还带 `rates`，用到具名表的还带 `tables`（都与 `funroute.run` 同形）。测试会通过语言服务逐条运行，并要求这些示例合起来覆盖示例注册表的全部函数和形式、全部运算符和全部节点种类 —— 新增了能力却不补示例，CI 会失败。

## 性能

VM 是栈式字节码解释器，**执行本身不分配内存**，只有程序构造的数据（比如推导式产出的数组）才分配。金额参数在边界上的币种扫描（数组、字典、记录）也不分配。`using` 例外：报价全是常量的 `using` 在装载时建好汇率表，每次运行只分配交出这张表的 context（2 次）；有具名表或运行时才知道的报价时，每次运行要新建一张表（具名表要先复制），分配随表的大小增长。

Apple M5，`go test ./lang/internal/machine ./lang/internal/compile -bench . -benchtime 1s -count 5`，取中位数（VM 与边界的基准在 machine，编译基准在 compile）：

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

已完成：解析、宿主契约、类型推导与重载、编译期求值、字节码 VM、Artifact digest、record、nominal 枚举与穷尽检查、推导式与 `reduce`、句柄与模型批处理、超时与 `fallback`、类型化错误、标准库、金额（`money`/`rate`/`fxrate`/`currency`、币种检查、换汇 `->`、汇率表、具名汇率表与 `using`、汇率加点与比较、`prorate`、`round_to`、按策略分摊）、格式化器、签名清单、语言服务（stdio 与 WebAssembly）和工作台。

用于真实支付前还缺：日期与时间、显式业务错误、决策 trace。语言服务还缺错误恢复（写到一半的程序目前只能给出词法层面的事实）和对表达式内部注释的格式化。详细计划与取舍见 [`docs/roadmap.md`](docs/roadmap.md)。

为什么语言必然终止、最坏延迟为什么有多项式上界、以及"如果要图灵完备该怎么加"，见 [`docs/termination.md`](docs/termination.md)。

## 开发

```bash
make ci        # 格式、import 分组、前端检查与构建、vet 与 staticcheck/modernize（均含 js/wasm）、lint、build、wasm、Go 与 JS 测试，提交前必须全过
make test      # Go 测试
make wasm      # 浏览器用的语言服务：web/dist/funroute.wasm
make web       # 前端产物：web/dist/*.js（需要先在 web/ 里 npm install；不提交）
make test-js   # 前端纯逻辑与 wasm 会话测试（node --test）
make site      # 组装发布目录 site/（make run 与 Pages 都用它）
make run       # 启动工作台
go test ./lang/internal/machine -bench . -benchtime 2000x   # VM 基准
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
