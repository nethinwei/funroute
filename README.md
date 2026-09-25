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

- **Go SDK**（`import "github.com/nethinwei/funroute"`）：解析、类型推导、编译成字节码、执行。零第三方依赖。
- **语言服务**（`lsp`）：一个 LSP 服务，诊断、补全、悬停、格式化都从语言本身得来。它既能以 stdio 运行，也能编成 WebAssembly 在浏览器里运行。
- **策略工作台**（`web/`）：编辑器加结构视图，全部语言能力来自语言服务，在浏览器里运行，不需要后端。

**目录**：[为什么是这样一门语言](#为什么是这样一门语言) · [快速上手](#快速上手) · [语言导览](#语言导览)（[金额](#金额)） · [契约](#契约) · [类型推导与编译期求值](#类型推导与编译期求值) · [在 Go 中使用](#在-go-中使用) · [按控制台开放能力](#按控制台开放能力) · [ExprJSON](#exprjson) · [语言服务](#语言服务) · [策略工作台](#策略工作台) · [性能](#性能) · [现状](#现状) · [开发](#开发) · [附录 A：语法参考](#附录-a语法参考) · [附录 B：取值范围](#附录-b取值范围)

相关文档：[`docs/roadmap.md`](docs/roadmap.md)（路线图与设计决策）、[`docs/termination.md`](docs/termination.md)（为什么每条规则都会停）。

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

需要 Go 1.26。在自己的项目里使用：

```bash
go get github.com/nethinwei/funroute
```

```go
import (
    "github.com/nethinwei/funroute"
    "github.com/nethinwei/funroute/extensions/std" // 标准库函数，按需
)
```

在本仓库里试 CLI：

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

CLI 一共六个子命令：

| 子命令 | 作用 |
|---|---|
| `inspect` | 打印推导出的签名、digest 与指令数 |
| `run` | 执行，`-args` 以 JSON 给入参 |
| `compile` | 输出 artifact（JSON） |
| `export` | 输出 ExprJSON |
| `fmt` | 格式化（也可从标准输入读程序） |
| `lsp` | 语言服务，见[语言服务](#语言服务) |

常用选项：

- `-types 'a=int,b=float'` 声明参数与顺序，`-alias 'Order=record{…}'` 声明类型别名，见[契约](#契约)。
- `-currencies iso`（缺省，ISO 4217）或 `none` 决定是否声明金额。汇率和别的参数一样从 `-args` 传入，例如 `-types 'rates=array<fxrate>'`。
- `lsp` 不带 `-manifest` 时按 ISO 4217 声明金额；带清单时只用清单里的声明。

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
| `money` | `USD 1.70` | 某币种的最小单位整数，币种是值的属性；注册表声明币种后才有，见[金额](#金额) |
| `ratio` | `2.9%`、`25bps` | 精确比例，只和金额、比例一起用 |
| `fxrate` | `150 JPY / USD` | 1 USD 换多少 JPY，精确比值；也来自契约或 `implied(到账额, 支付额)` |
| `currency` | `USD` | 注册表声明的币种，写它的代码即可 |

几条规则：

- 字符串与数值之间不做隐式转换，用 `int(...)`、`float(...)`、`string(...)`、`bool(...)` 显式转换。
- 空的 `[]` / `{}` 需要从上下文（函数签名或契约）得到元素类型。
- 取整函数（`ceil`/`floor`/`round`）对 float 返回 `int`（金额的 `round(m, @mode)` 见[金额](#金额)）。要 float 就写 `float(floor(x))`。
- **没有 null**。数组越界、字典缺键、除零都是错误。要兜底就写出来：`get(d, "k", 0)`。
- 金额不要用 `float`：注册表声明币种后用 `money`，见[金额](#金额)。
- 每种类型的范围、精度和越界时的行为见[附录 B](#附录-b取值范围)；完整文法见[附录 A](#附录-a语法参考)。

### 运算符

运算符只是函数调用的简写，`a + b` 就是 `add(a, b)`。优先级从低到高：

| 优先级 | 运算符 | 对应函数 |
|---|---|---|
| 1 | `\|\|` | `if(a, true, b)`（短路） |
| 2 | `&&` | `if(a, b, false)`（短路） |
| 3 | `==` `!=` | `eq`，`!=` 是 `eq` 取反 |
| 4 | `<` `<=` `>` `>=` `in` | `lt` `le` `gt` `ge` `member` |
| 5 | `->` | `convert`（换汇，见[换汇](#换汇--与-using)；只在声明了金额的注册表里有） |
| 6 | `+` `-` | `add` `sub` |
| 7 | `*` `/` `%` | `mul` `div` `mod`（数字字面量后的取模要空格：`10 % 3`；`10%` 是比例） |
| 8 | `!` `-`（一元） | `if(a, false, true)`、`sub(0, a)` |
| 9 | `xs[i]` `d["k"]` | `at` |

二元运算符左结合，括号可改变优先级。`&&` 和 `||` 展开成惰性的 `if`，所以天然短路 —— `x != 0 && 10 / x > 2` 不会除零。`->` 比 `+`/`-` 松、比比较紧：`fee + amount -> JPY` 换的是和，`amount -> JPY > cap` 比的是换后的金额，`(amount -> JPY) * 2` 要括号（`->` 要写在 `using` 与 `round` 里，见[金额](#金额)）。

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

标准库的主要内容（完整清单见工作台的「函数说明」，或在编辑器里悬停查看；每个函数都附有可以直接运行的案例）：

| 类别 | 函数 |
|---|---|
| 聚合 | `sum` `min` `max` `any` `all` `avg` `median` `stddev` `percentile` |
| 序列 | `range` `indices` `first` `last` `take` `slice` `reverse` `concat` `unique` `flatten` `sort` `sort_desc` `top_k` `bottom_k` `take_while` `drop_while` `windows` `chunk` `deltas` `cumsum` |
| 选择与分组 | `index_of` `arg_min` `arg_max` `sort_by` `sort_by_desc` `group_by` `rank` `intersect` `except` |
| 字典 | `get` `merge` |
| 字符串 | `upper` `lower` `trim` `contains` `starts_with` `ends_with` `split` `join` `replace` `pad_left` `pad_right`（宽度至多 10000） |
| 数值 | `abs` `ceil` `floor` `round` `pow` |

`sum`、`any`、`all` 与内核的 `len` 套推导式时边算边折叠，不建中间数组；`any`/`all` 在决定答案的元素处停下，后面的元素不再计算——`any([10 / x > 2 for x in xs])` 在第一个为真的元素之后不会再除零，和 `||` 一样。

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

**字段更新**用 `with`：复制一份记录，替换写出的字段，其余原样保留。

```text
order with {amount: order.amount - fee}
orders[0] with {fee: 0}
(order with {fee: 0}).amount
b with {customer: b.customer with {amount: 1}}   // 嵌套字段靠嵌套来改
```

- 结果的类型就是原记录的类型：`Order` 进、`Order` 出。
- 只能替换已有字段，新值必须是该字段的类型；字段名不存在或类型不同都是编译错误。花括号里至少写一个字段。
- `with` 是后缀，和 `.字段`、`[下标]` 同一层；其他表达式作基准要加括号。

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

宿主调用 `Registry.DeclareMoney` 声明币种（代码与小数位）之后，语言多出四种类型：`money`、`ratio`、`fxrate`、`currency`。没有声明的注册表里它们不存在，不写金额的程序编译出的 artifact 连 digest 都与以前逐字节相同。金额与比例的**写法**属于语法，与是否声明无关：在任何注册表里 `10%3` 都是语法错误，形如币种代码的名字（`URL`）都不能当变量。

```go
registry.DeclareMoney(funroute.MoneySpec{Currencies: std.ISO4217()})
```

整套设计只有三条规则：

1. **币种是值的属性，不是类型的一部分**。`money` 就是一个类型，和 Go 的 `funroute.Money{currency, minor}` 是同一个东西；两笔金额币种对不对，在它们相遇的运算上检查。
2. **舍入永远写出来**。没有注册表级的默认舍入方式：会落到两个最小单位之间的运算，要么写在 `round(…, @mode)` 里，要么当场写出舍入方式。
3. **汇率就是参数**。换汇只能写在 `using(汇率…, 主体)` 里，汇率要么写在规则里，要么是契约声明的参数；没有隐藏的运行时汇率表。

#### 类型与字面量

| 类型 | 写法 | 说明 |
|---|---|---|
| `money` | `USD 1.70`、`USD -1.70`、`JPY 100` | 某币种的 int64 个最小单位；负号写在数上；代码与数之间只能是空格或 Tab（不能换行、不能有注释）；位数超过币种的小数位是编译错误，不舍入 |
| `ratio` | `2.9%`、`25bps`、`0.029` | 精确的比值（int64 分子/分母）；小数字面量和金额或比例一起运算时就是 `ratio`，按写下的数字精确读入（至多 18 位小数），从不经过 float；和 float 一起时仍是 float |
| `fxrate` | `150.25 JPY / USD` | 1 USD 换 150.25 JPY，精确比值，另带两边币种 |
| `currency` | `USD`、`JPY` | 币种字面量；可进 `switch`、作字典的键、和 `==` 比较 |

几点说明（写法的精确规则见[附录 A](#附录-a语法参考)）：

- **契约里就写 `money`、`ratio`、`fxrate`、`currency`**，不带参数；`money<USD>` 是解析错误。类型不约束币种，要约束就在规则里写出来（`currency(amount) == USD`），或由宿主在进入规则之前检查。
- **币种就是它的代码**：`money(170, USD)`、`currency(m) == USD`、`switch(currency(m), case USD => …, else => …)`。它不是枚举，不写 `@`。因此变量和参数不能取代码形状的名字（`USD`、`URL`）；枚举成员（`@CARD`）与记录字段（`order.USD`）不受影响。
- **汇率字面量** `150 JPY / USD` 读作"1 USD 换 150 JPY"，是一个字面量而不是两次运算。数按写下的样子精确读入，小数位不受币种限制（`0.00000000065 USD / JPY` 也行）。同一币种之间恒为 1：`1 USD / USD` 合法，`150 USD / USD` 是编译错误。
- **`%` 紧贴数字就是比例**：`2.9%`、`10%-3%`（即 `10% - 3%`）。取模要在 `%` 前留空格：`10 % 3`。`10%3` 是语法错误，报错会说明原因，不会悄悄读成取模。
- **字面量 `0`** 可以当金额（`amount > 0`、`if(x, fee, 0)`），也可以当比例（`fee > 0`、`-fee`）；别的整数不行。

**不带币种的零**：`0`、`sum([])`、Go 的 `funroute.Money{}` 都是不带币种的零，它与任何币种相加、比较都成立，JSON 写作 `0`。不带币种却不是零的金额不存在（`ErrCurrency`）。

**金额与普通计算按类型区分，逐个运算判定，没有模式。** 一个运算的操作数里有 `money`，它就按金额规则执行，否则一切照旧，同一条规则里两者可以并存：

```text
let(
  fee = round(amount * 2.9%, @half_up) + USD 0.30,   // 金额：精确，舍入写明
  ok  = risk < 0.5 && len(retries) < 3,              // 普通：float 与 int
  if(ok && fee < cap, "adyen", "stripe")
)
```

#### 运算

结果只能是金额、比例或汇率，其余是编译错误，报错会给出改法。下表的例子都可以直接运行：

| 写法 | 结果 | 说明 |
|---|---|---|
| `USD 1.70 + USD 0.30` | `USD 2.00` | 同币种加减、比较、取负、`abs`；`USD 1 + EUR 1` 是编译错误 |
| `refund + paid` | 同币种时是和，否则 `ErrCurrency` | 两个参数都是 `money`，币种要到运行时才知道，所以在相加处比对 |
| `USD 1.25 * 3` | `USD 3.75` | 乘整数，精确 |
| `round(USD 10 * 2.9%, @half_even)` | `USD 0.29` | 乘比例；写在 `round` 外是编译错误 |
| `mul(USD 0.50, 2.9%, @up)` | `USD 0.02` | 同一个运算，当场舍入；`div`、`convert`、`prorate` 同理 |
| `round(USD 10 / 50%, @half_even)` | `USD 20.00` | 除以比例，比如由税后反推税前 |
| `100% - 2.9%` | `0.971` | 比例之间加减乘除、比较，精确 |
| `USD 0.29 / USD 10` | `0.029` | 金额相除只有一种意思：同币种之比，得比例（实际费率）；币种不同是 `ErrCurrency` |
| `implied(JPY 15000, USD 100)` | `150 JPY / USD` | 成交汇率：到账额与支付额，两者必须非零、同号、币种不同 |
| `150 JPY / USD * 101%` | `151.5 JPY / USD` | 汇率乘比例：加点或折让，精确 |
| `150 JPY / USD < 151 JPY / USD` | `true` | 同一货币对的汇率可以比较；`150 JPY / USD < 0.9 EUR / USD` 是编译错误 |
| `USD 10 / 3`、`USD 1 * USD 1`、`amount * risk`（`risk: float`）、`n * 2.9%`（`n: int`） | 编译错误 | 平均分用 `allocate(m, 3)`，比例写成 `2.9%` 字面量，`1 - fee` 写成 `100% - fee` |

**比例只为金额服务**：`ratio` 只和金额、比例一起运算。整数与比例之间没有任何运算，也没有 float 与比例的互转，所以 `1 - fee` 要写成 `100% - fee`，`n * fee` 先把 `n` 用在金额上（`amount * n * fee`）。要从文本读比例用 `ratio("0.029")`（也接受分数 `ratio("1/3")`）。

**汇率只有两种运算：加点与比较**。`fxrate` 可以作结果、进容器与记录、乘比例得到同一货币对的加点汇率、同一货币对之间比较大小与相等（不同货币对是编译错误或 `ErrCurrency`），但不能乘金额，也不能相乘、交叉、取倒数、求平均。换汇只有一种写法：`->`。

比例与汇率的结果在 JSON 里写作文本与对象：`97.1%` 是 `"0.971"`，三分之一是 `"1/3"`，`150 JPY / USD` 是 `{"base":"USD","quote":"JPY","rate":"150"}`。

#### 舍入：`round(…, @mode)`

会落到两个最小单位之间的运算只有四类：金额乘比例、金额除以比例、换汇（`->` 与 `convert`）、`prorate`。它们各有两种写法：

- **不写舍入方式**（运算符 `*`、`/`、`->` 只有这一种）：只能写在 `round(表达式, @mode)` 里。比例、汇率与 `round` 里的中间金额都是有理数（约分后的 int64 分子/分母），所以在里面**精确**计算：不管有几步——乘比例、除比例、换汇、连写的多跳换汇——都不舍入，`round` 在最后舍入一次。
- **末尾写舍入方式**：`mul(amount, 2.9%, @up)`、`convert(amount, JPY, @half_even)`、`prorate(fee, 1, 3, @down)`，写在哪里都行，当场舍入。

```text
round(USD 0.05 * 50% * 50%, @half_up)             // USD 0.01：精确值是 0.0125 美元，只舍入一次
mul(mul(USD 0.05, 50%, @half_up), 50%, @half_up)  // USD 0.02：每一步各舍入一次
round(amount * (interchange + scheme + markup), @half_even)   // 三段比例先相加，整体只舍入一次
```

- `round` 里没有这类运算是编译错误（`round(amount, @up)` 什么也没舍入）；嵌套时每一层只管自己的：`round(round(m * 50%, @up), @down)` 的外层同样是编译错误。
- **精确值不出 `round`**：`round` 里的中间金额可以相加减、取负、乘整数、比较、进 `if`/`switch`/`let`/`fallback` 的分支，但不能进数组、字典、记录、推导式，不能交给宿主函数或 `minor`、`allocate`，也不能直接作结果——编译错误会指到那个位置。所以 `round` 外面看到的金额永远是整数个最小单位。
- `round_to(m, 粒度, @mode)`、std 的 `avg(xs, @mode)`、`median(xs, @mode)` 本身就是一次舍入，只有写出舍入方式的形式。
- 方式有 `half_even`、`half_up`、`half_down`、`down`、`up`、`ceiling`、`floor`，是注册表提供的枚举 `rounding` 的成员，所以契约不能再声明叫 `rounding` 的枚举。
- `allocate` 不舍入：它向零取整后把差额按策略发完，一分不丢。

#### 换汇：`->` 与 `using`

`amount -> JPY` 把金额换成日元（目标也可以是一个 `currency` 值），它只能写在 `using` 里，用的汇率就是 `using` 列出的那些：

```text
using(150 JPY / USD, round(amount -> JPY, @half_even))            // 写在规则里的报价
using(market, round(amount -> JPY, @down))                         // 契约参数 market: array<fxrate>
using(implied(settled, paid), round(refund -> EUR, @half_even))    // 按原交易的成交汇率退款
using(market, using(fx(EUR, USD) * 103.5%, round(amount -> USD, @half_even)))   // 读出外层的汇率再加点
```

- **汇率是参数**。`using(报价, …, 主体)` 的每个报价是一个 `fxrate` 或一个 `array<fxrate>`。宿主的行情、清算价、协议价，就是契约里声明的参数（Go 传 `[]funroute.FxRate`，零拷贝）；要几套汇率就声明几个参数，每个 `using` 只用它列出的那些。
- **`using` 永远隔离**：主体只用它自己列出的报价，外层 `using` 不参与，所以规则换汇用的是哪条汇率，看 `using` 本身就知道。要沿用外层的某个汇率就用 `fx(基准, 报价)` 读出来写进去：报价在进入主体之前、在 `using` 外面求值，所以 `fx` 读的是外层。
- **单跳**：每个 `->` 只用这一对货币的汇率：这个方向的报价，或者反方向报价的倒数；同一货币对出现多次时后写的为准。它从不自己找中转货币，要经过别的货币就连写：`amount -> USD -> EUR`。`->` 左结合，每一跳只用自己那一对货币的汇率；和 `round` 里的其他运算一样，中间的 USD 保持精确，不落到最小单位，`round` 在最后只舍入一次，结果与在 Go 里用 `FxRate.Chain` 把交叉汇率乘好再换一次相同。清算里中间币种是一笔要各自入账的真实金额时，就一跳一跳写出舍入方式：`convert(convert(amount, USD, @half_even), EUR, @half_even)`。

```text
using(150.5 JPY / USD, 0.5 EUR / JPY, round(USD 0.01 -> JPY -> EUR, @half_even))   // EUR 0.75：精确值 0.7525 欧元，只舍入一次
using(150.5 JPY / USD, 0.5 EUR / JPY, convert(convert(USD 0.01, JPY, @half_even), EUR, @half_even))   // EUR 1.00：先落到 JPY 2
```
- **换不了就是 `ErrNoFxRate`**：`using` 里没有这一对货币。它和扩展函数失败一样说明数据暂时不可得，所以 `fallback` 接它；`fallback` 加 `using` 是整段备选，备选那一段只用它自己的汇率：`fallback(using(market, …), using(agreed, …))`。兜底值要让下游认得出来：兜底成 `JPY 0` 的金额在成本比较里会被当成最便宜的通道，应连同一个标记一起返回。已是目标币种的金额与不带币种的零不需要汇率，原样返回。
- 写在任何 `using` 之外的 `->`、`convert`、`fx` 是编译错误。`->` 读运行时的报价，不在编译期折叠。`using` 是保留字，只在声明了金额的注册表里有意义。

#### 表达能力与精度

金额是 int64 个最小单位，能表示多少主单位取决于币种的小数位（声明时限定 0–8 位）：

| 小数位 | 例子 | 最小单位 | 最大金额（最小值是它的相反数再减一个最小单位） |
|---|---|---|---|
| 0 | JPY、KRW、VND | 1 | 9,223,372,036,854,775,807（约 9.2×10¹⁸） |
| 2 | USD、EUR、CNY | 0.01 | 92,233,720,368,547,758.07（约 9.2×10¹⁶，全球财富总量约 4.7×10¹⁴ 美元） |
| 3 | KWD、BHD | 0.001 | 9,223,372,036,854,775.807（约 9.2×10¹⁵） |
| 6 | USDT、USDC（按业务精度声明） | 0.000001 | 9,223,372,036,854.775807（约 9.2×10¹²） |
| 8 | BTC、ETH（按业务精度声明） | 0.00000001 | 92,233,720,368.54775807（约 9.2×10¹⁰） |

比例、汇率与 `round` 里的精确金额都是**约分后的 int64 分子/分母**：`1/3`、报价的倒数都精确，运算从不分配内存。代价是精度上限：分子、分母各自要放得进 int64（约 18 位数字），比例的小数文本至多 18 位小数。放不下就是 `ErrArithmetic`，从不悄悄舍入；中间积用 128 位计算，只有结果放不下才报错。

| 情况 | 结果 | 精确吗 | 何时报错 |
|---|---|---|---|
| 字面量 `USD 1.70`、宿主输入 `"USD 1.70"` | 最小单位整数 | 精确；位数超过小数位**报错不舍入**（`USD 1.001`） | 超出上表范围 |
| 加减、比较、`abs`、取负、`money × int` | 同币种金额 | 精确 | 溢出是 `ErrArithmetic`；币种不同是 `ErrCurrency` |
| `money × ratio`、`money / ratio`、换汇、`prorate` | 同币种或目标币种的金额 | 在 `round` 里精确，整段只舍入一次；写出舍入方式时当场舍入一次 | 结果或精确中间值放不下；换汇找不到汇率是 `ErrNoFxRate` |
| 比例之间的加减乘除、`money / money` | `ratio` | 精确 | 分子或分母放不下、除以零；金额相除币种不同是 `ErrCurrency` |
| `fxrate × ratio`、`implied` | `fxrate` | 精确 | 结果不为正、放不下；同币种汇率不为 1 |
| `allocate(m, [权重])`、`allocate(m, n)` | 几笔同币种金额 | 一分不丢：各份向零取整，剩下的最小单位缺省按最大余数法发出（被舍掉最多的份先得），末尾可写策略 `@largest_weight`、`@in_order`、`@reverse_order`、`@all_first`、`@all_last` | 份数超过 10000、权重为负或全为零、权重之和溢出 |
| `round_to(m, 粒度, @mode)` | 同币种金额 | 取到粒度（同币种的正金额，`CHF 0.05`）的整数倍，舍入一次 | 粒度不大于零、币种不同 |
| `sum`、`cumsum` | 同币种金额 | 精确 | 总和溢出 |
| `avg(xs, @mode)`、`median(xs, @mode)` | 同币种金额 | 舍入一次到最小单位 | 从不因总和溢出而失败：先除后加，平均值总在最小与最大之间 |

所以金额只在写出舍入方式的地方舍入：`round(…, @mode)` 的结尾，或带 `@mode` 的那一步。汇率与比例从不舍入。其余运算要么精确，要么报错。

**加密货币按业务精度声明**：没有 ISO 代码（ISO 24165 的 DTI 是给监管报送的标识符，不是币种代码），代号由宿主定，稳定币 `USDT`/`USDC` 取 6 位，`BTC` 8 位，`ETH`/`SOL` 这类链上 9–18 位的取 8 位 —— 按 wei 计 int64 只放得下约 9.2 ETH。链上的精确金额由宿主的记账系统在边界换算与对账，规则只按业务精度路由与计费；同一代币在不同链上是不同资产时，用不同的代号（`USDTTRX`，代号只能是大写字母与数字）。演示控制台 `internal/demo` 就是这样声明的。

#### 币种检查与函数

**币种检查**：在运算发生的地方检查——加减、比较、`==`、`in`、金额相除、换汇、标准包的聚合（同一数组里的金额必须同币种）。两边都是写死的币种时，常量折叠在编译期就报出来（`USD 1 + EUR 1`）；其余在运行时比对，币种不一致、币种未声明都是 `ErrCurrency`，`fallback` 不接它。参数的币种没声明同时也是 `ErrContract`；宿主函数返回的金额币种没声明也是 `ErrCurrency`。比较两个 `currency` 值本身就是在问是否同一币种，永远有答案。

**函数**：

- `money(最小单位, 币种)` 由整数与币种造出金额（输入里两者分开时用：`money(o.amount, o.currency)`，`o.currency` 声明为 `currency`）；`currency_of(字符串)` 校验币种代码。
- `minor`、`currency`、`sign` 把金额拆开；`string(币种)` 得到代码。`minor(...)` 得到的是普通整数：拿它造回另一币种的金额（`money(minor(a), JPY)`）在语言里是允许的，币种对不对由规则作者负责。
- `allocate(m, [权重])` 与 `allocate(m, n)` 分摊且一分不丢；策略是注册表提供的枚举 `allocation` 的成员，契约不能再用这个名字。
- `prorate(m, part, whole)` 按比例取金额（part 与 whole 可以是整数，也可以是另一币种的两笔同币种金额）；`round_to(m, 粒度, @mode)` 做现金舍入。
- `implied(到账额, 支付额)` 得成交汇率，`fx(基准, 报价)` 读出当前 `using` 里的汇率，`ratio(字符串)` 把十进制文本或分数精确读成比例（读不出是 `ErrArithmetic`）。
- 标准包的 `sum`/`min`/`max`/`avg`/`median`/`sort`/`cumsum`/`sort_by`/`top_k` 等在声明了币种后也接受金额。

**宿主那边**：Go 用 `funroute.Money`、`funroute.Ratio`、`funroute.FxRate`、`funroute.Currency`，由币种表构造（见[在 Go 里算钱](#在-go-里算钱)），`[]funroute.Money`、`[]funroute.FxRate` 与规则之间零拷贝；JSON 输入认 `"USD 1.70"` 与 `{"currency": "USD", "minor": 170}` 两种，汇率是 `{"base": "USD", "quote": "JPY", "rate": "150.25"}`（`rate` 也可以是分数 `"1/3"`）；金额、比例与汇率里的数只收字符串、`json.Number` 与整数，float64 一律拒绝——解码成 `any` 的 JSON 数已经被舍入过，`funroute.DecodeArgs` 把数保留为原文；`Registry.EncodeJSON` 把金额写成 `"USD 1.70"`。

## 契约

规则文本**只是表达式**。参数叫什么、什么类型、什么顺序、返回什么，由宿主在编译时传入：

```go
result := funroute.IntType
artifact, err := funroute.CompileExpr(
    `switch(country, case "SG", "MY" => amount * 2, else => amount)`,
    registry,
    funroute.CompileOptions{
        Args: []funroute.ArgSpec{
            {Name: "country", Type: funroute.StringType, Doc: "ISO 3166-1 二字码"},
            {Name: "amount", Type: funroute.IntType, Doc: "订单金额，单位：分"},
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

文本契约（语言服务的 `funroute/setContract` 与工作台的契约面板都用它）写在 `types` 字段：

```json
{"types": {"Order": "record{amount: int, currency: string}"},
 "args": [{"name": "a", "type": "Order"}, {"name": "b", "type": "array<Order>"}],
 "result": {"type": "Order"}}
```

别名只是一种**写法**：解析时就地展开，编译器和 artifact 都看不到它，用别名和写全字段编出的是同一个 artifact。因此：

- 别名不能引用别的别名；
- 没声明的名字仍然报错（`unknown type "Order"`）；
- Go 宿主直接复用 `funroute.Type` 变量即可，不需要别名。

### 枚举

枚举只能由契约声明：Go 里写 `funroute.EnumOf("channel", "adyen", "stripe")`，文本契约写 `enum<channel>{adyen,stripe}`。表达式里用 `@成员` 引用：

```text
switch(channel, case @adyen => @stripe, case @stripe => @adyen)   // 已穷尽，不需要 else
let(preferred = @stripe, channel == preferred)
@channel.adyen                                                     // 多个枚举有同名成员时写全名
```

- 枚举是**具名类型**：`enum<a>{x}` 和 `enum<b>{x}` 不是同一个类型，枚举也不能当字符串用，需要时写 `string(channel)`。
- `@adyen` 属于哪个枚举，由契约里声明过的枚举决定。只有一个枚举含它就写短名；有多个时编译器要求写全名。
- 运行时，枚举入参会拒绝集合外的字符串；枚举返回值要求编译器能证明每条路径都落在成员内。
- 不来自契约类型的枚举有两个：声明了金额的注册表提供的舍入方式（`@half_up` 等，枚举名 `rounding`）与分摊策略（`@all_last` 等，枚举名 `allocation`）；契约不能再用这两个名字，契约自己的枚举成员与它们重名时优先解析成契约的。币种不是枚举：写 `USD`，不写 `@USD`。

### 导出视图

规则被贴到工单或聊天里时，读者看不到契约。`funroute.RenderWithContract` 把契约写成注释：

```text
// amount:  int              订单金额，单位：分
// country: string           ISO 3166-1 二字码
// →        int              应收总额，单位：分

switch(country, case "SG", "MY" => amount * 2, else => amount)
```

注释不是语法：这段文本粘回控制台，编出同一个 digest。

## 类型推导与编译期求值

### 类型从哪里来

类型的唯一来源是函数注册表里的签名。例如扩展函数声明了 `risk.approved_v1(string, int) -> bool`，那么：

```text
if(risk.approved_v1(country, amount), "primary", "backup")
```

会推出 `(country: string, amount: int) -> string`。

数值的便利规则：

- 没有其他约束时，`add(a, b)` 把参数推成 `int`；
- 出现浮点字面量时变量被推成 `float`：`risk < 0.5` 得到 `risk: float`；
- `add(1, 1.5)` 这样的混合运算把整数提升为 `float`，超出 float64 精确范围的整数会被拒绝；
- 混合签名 `(int, float)` 只在没有同型解读时才使用；
- 小数字面量读作 `float` 时必须恰好是写下的那个 float64：`0.30000000000000001` 会被拒绝并指出最近的是 `0.3`；读作 `ratio` 时没有这个限制。

推导只读一遍程序。几个重载都合适的调用先等着，其他地方能确定的类型全部确定之后，再由内向外逐个决定：优先不留下未定的类型，其次不改变字面量的种类，再次不做提升，最后选最朴素的类型；最优的并列时报歧义并列出候选。耗时随程序长度近线性。

推导结果可以用 `-types` 或 `CompileOptions.Args` 覆盖：

```bash
go run ./cmd/funroute inspect -expr 'add(a,b)' -types 'a=float,b=float'
```

### 编译期求值

不依赖参数的子表达式，编译期就会被算完，不需要任何关键字：

```text
[1, 2, 3]                    → 一条载入指令
upper("adyen")               → 一条载入指令
sum(range(4))                → 一条载入指令
let(base = {a: 1}, base.a)   → 一条载入指令
1 / 0                        → 编译错误，即使写在不会走到的分支里
```

[局部绑定](#局部绑定let)那个例子编译出 7 条指令、0 个局部变量槽：三个绑定都被折成了常量，运行时只剩一次乘、一次除、一次加。

哪些函数能在编译期调用由宿主授权（`Doc.Constexpr`）。内核和标准库都可以；模型、时钟、远程调用**不应该**标 —— 否则编译规则时就会去调用推理引擎，同一条规则在不同时间编译出不同的结果。

## 在 Go 中使用

### 编译与运行

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

### 类型化绑定：契约就是两个 Go 类型

宿主本来就有请求 struct 和结果 struct，契约可以直接从它们读出来。`funroute.Bind[In, Out]` 反射**一次**：`In` 里带 `funroute:"name"` tag 的导出字段是参数，**声明顺序即 ABI**（规则与[记录](#记录)相同，没有 tag 的字段不在契约里；一个都没有，比如 `struct{}`，就是不带参数的规则）；`Out` 是返回类型。得到的 `Binding` 是只接受这套契约的编译器：

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

**为什么快**：`Program.Run` 不查名字、不反射、不拼 map 或 `[]Value`，参数按绑定时算好的偏移直接从 struct 读出，结果按下标写回 `Out`。程序**没读的参数不做任何转换**，所以一个宿主 struct 可以服务多条规则。代价是没读的参数也不检查（enum 成员资格、NaN 只对读到的参数检查）；只读几个 bool、int、float、string 字段的 record 参数也一样，只读那几个字段，其余字段不检查。规则看不到没读的值，结果不受影响。

剩下的分配都有名目。装载时的值流分析知道每个参数、每个数组去了哪里：只在程序里被读的，就地读、不分配；会交给宿主函数或放进结果的，才要自己的内存。

| 参数 | 分配 |
|---|---|
| 标量、字符串 | 0 次 |
| 程序只遍历、取长度或下标的原生切片（`[]int64`、`[]float64`、`[]string`、`[]bool`） | 0 次（切片头拷进帧，元素不复制；`[]float64` 仍做一遍 NaN 检查） |
| 只被遍历或取长度、循环里只读元素字段的 struct 切片（字段都是 bool、int、float、string） | 0 次（切片头拷进帧，每一轮把元素读进循环自带的 record） |
| 其他切片或映射 | 1 次（装箱它的头，元素不复制） |
| 程序只读字段的 record | 0 次（读进帧自带的 record；每次读都是一个 bool、int、float、string 字段时不建 record，只把读到的字段读进寄存器） |
| 其他 record | 1 次 |

| 结果 | `Run` | `RunInto` |
|---|---|---|
| 标量、record 本身 | 0 次 | 0 次 |
| 推导式产出的数组（整个结果，或结果 record 的一个字段） | 1 次（它的内存） | 0 次，只要 `out` 里那个切片有足够的容量 |

程序里的中间数组——只被遍历、取长度或下标的推导式与数组字面量——建在帧自己的内存里，帧跨运行复用，稳定之后 0 次分配。

`program.RunInto(ctx, &request, &out, opts)` 把结果写进 `out`，并在 `out` 已有的切片里建数组结果，宿主拿同一个 `out` 反复运行就不再为结果分配。`out` 里的切片若与参数共用内存，就不在上面建（避免边读边写）；出错时 `out` 的内容不确定。

结果里的容器是程序的 backing，只读。

**三条运行路径怎么选**：表单与 JSON 用 `Run(map)`；向量预先检查好、要反复复用的用 `RunValues`；服务的热路径用 `Program`。

`Program` 每次运行都把读到的 `[]float64` 与 `map[string]float64` 检查一遍，拒绝 NaN 与 Infinity：值在进入语言时确立不变量，而宿主 struct 里的切片每次运行都是新进来的。这一遍只读、不复制，受内存带宽限制（65536 个元素约 16 µs）。同一个大向量要跨很多次运行复用，就用 `funroute.ToValue` 构造一次（在这里检查），再经 `RunValues` 传入，此后不再检查。没有"宿主担保、跳过检查"的开关：担保错了，`score > 0.8` 遇到 NaN 得 false，规则静默地走错分支。

**载入别处编译的 artifact**：`Load` 按名字匹配，规则与 `Run(map)` 的边界相同。

- artifact 声明的每个参数必须是 `In` 里同名的字段，顺序以 artifact 为准；record 声明的每个字段必须在 struct 的 tag 字段里。
- 类型必须完全相同，不做数值加宽。唯一的例外是枚举：Go 的 `string` 字段可以承载契约里的 enum，读入时检查成员资格。
- `In`、`Out` 多出来的字段一律忽略，`Out` 里没被结果覆盖的字段留零值。所以控制台只声明规则读到的参数和字段，照样能载入。
- 不匹配在载入时就报 `ErrContract`，并写明是哪个参数、哪个字段。
- `Bind` 自己推出的契约里没有枚举（Go 类型表达不了），需要枚举的契约由控制台编译后 `Load`。

### 在 Go 里算钱

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

### 注册扩展函数

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

- 参数可以是 Go 标量、任意嵌套的切片和 `map[string]…`、struct（见[记录](#记录)）或句柄；首参数可选 `context.Context`；返回 `R`，会失败的返回 `(R, error)`。
- 常见签名——标量与 `[]float64`/`[]int64` 进、标量出，可带 `error`——直接调用，每次约 25 ns、0 次分配；其余签名经 `reflect.Call`，约 300 ns。也可以不填 `Go`，手写 `Params`、`Result`、`Eval`；两种写法二选一。
- 以一个数组为参数的聚合可以声明 `Fold`，套推导式调用时就边算边折叠，不建数组、也不调用它：`Fold: &funroute.Fold{Step: "add", Init: funroute.Int(0)}` 是一个求和，`Step` 是把"到目前的答案"和下一个元素并起来的内核函数；`Stops`/`Stop` 让一个 bool 的折叠遇到 `Stop` 就停（`any` 停在 true）；`Counts` 是计数；`First` 取第一个元素并就此停下（`first`），一个都没有时对空数组调用函数本身、照样报错。折叠必须与函数本身给出同样的答案。
- 金额直接写 Go 类型：`funroute.Money`、`funroute.Ratio`、`funroute.FxRate`、`funroute.Currency` 及它们的切片与映射（零拷贝），`Go` 的签名反射就能读出；手写 `Params` 时用 `funroute.MoneyType` 等。币种是值的属性，签名不约束它：收到几笔金额的函数自己检查它们同币种（错了返回 `ErrCurrency`），返回的金额币种必须已声明，否则是 `ErrCurrency`。
- 名字里的 `_v1` 只是约定。函数的身份是完整签名，签名变了，旧 artifact 会拒绝装载。
- `Doc` 只写机器算不出来的东西：标签、说明、参数标签，以及可选的案例 `Examples: []funroute.Example{{Source: "risk.score_v1(\"SG\", 100)", Result: "0.9"}}`（源码与它的 JSON 结果）。签名来自 Go 类型，分类默认取命名空间（`risk.score_v1` → `risk`）。
- 语言服务从注册表读函数的说明与案例，用在悬停和补全里，新增函数不需要改任何前端代码。
- 内核与标准库的每个函数都带案例，由测试逐条运行、比对结果，并要求它们合起来用到这个名字的每一个重载，所以案例不会与实现走样。

扩展函数被当作**不可信的纯函数**：VM 会兜住 panic，但无法证明它真的没有副作用，生产环境仍需代码审查。完整的宿主范例见 `internal/demo/`（工作台用的演示控制台）。

**容器不拷贝**：`func(xs []float64)` 收到的就是宿主传进来的那个切片，返回的切片也原样进入 VM。代价是一条约定：交给 `Value` 的切片或映射，从那一刻起只读。

### 句柄：让模型数据穿过规则

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

### 模型批处理

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

### 超时、兜底与错误

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

`ErrArithmetic` 包括：溢出、除零、float 非有限、汇率不为正或同币种汇率不是 1、比例或汇率的分子分母放不进 int64，转换没有答案（`int("x")`、`int(2.5)`、`bool("yes")`、`ratio("abc")`、float 表示不了的 int），以及数值参数超出它的取值范围（`range` 的步长与长度、`percentile` 的比例、`pad_left` 的宽度、`allocate` 的份数与权重、`pow` 的负整数指数）。

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

内核（`funroute.CoreRegistry()`）只有 19 个函数名：

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
- 值的位置只接受对应类型的 JSON 值，不接受 `null`；导入对文档只解码一次，耗时随文档大小线性增长。
- 金额相关的节点保留写下的十进制文本，因为折成多少最小单位取决于注册表的小数位，而 ExprJSON 不能依赖注册表：

  | 写法 | ExprJSON |
  |---|---|
  | `USD 1.70` | `{"node":"money","currency":"USD","amount":"1.70"}` |
  | `2.9%` | `{"node":"ratio","value":"2.9","unit":"%"}` |
  | `USD` | `{"node":"currency","code":"USD"}` |
  | `150.25 JPY / USD` | `{"node":"fxrate","rate":"150.25","quote":"JPY","base":"USD"}` |
  | `amount -> JPY` | `convert` 调用 |
  | `using(q, body)` | `{"node":"using","quotes":[…],"body":…}` |

## 语言服务

`lsp` 是一个 [Language Server Protocol](https://microsoft.github.io/language-server-protocol/) 服务。它报告的都是语言本身知道的事实——词法器和解析器对每一段源码的判断、编译器的诊断和类型、格式化器的排版——至于怎么显示，由客户端决定。

| 能力 | 来自 |
|---|---|
| 语义标记 | 词法器与解析器的判断：关键字、运算符、参数、局部名（定义处 / 引用处）、函数、字段、字面量、币种、注释 |
| 诊断 | 编译器，带出错的区间；读了契约没声明的变量，会指到那次读取 |
| 格式化 | 格式化器；打印结果一定能解析回同一个程序，表达式中间有注释时拒绝格式化，而不是丢掉注释 |
| 悬停 | 节点推导出的类型、调用选中的签名、宿主写的函数说明、案例和参数说明 |
| 补全 | 程序的参数（契约声明的，或没有声明时从文本推导的）、当前位置可见的局部名、注册表里的函数和形式；按"局部名 → 参数 → 函数 → 形式"再按名字排序；声明了金额时注册表的币种也是补全项；`@` 之后是契约里的枚举成员、舍入方式与分摊策略；`order with {` 里是这份记录还没写的字段；函数与形式的补全带说明和案例 |
| 签名提示 | 正在输入的调用，靠词法段找到，所以写到一半也能工作 |

另外有几个 FunRoute 自己的扩展：

- `funroute/setContract`（通知）：宿主把契约推给服务。契约是宿主的数据，不写在文本里。
- `funroute/syntaxTree`（请求）：带区间的具体语法树，供结构视图使用。
- `funroute/arguments`（请求）：程序要的参数，按顺序给出名字、类型和说明；契约没有声明时是从文本推导出的，试运行面板据此列出输入框。
- `funroute/catalog`（请求）：注册表里的函数与形式及宿主写的说明，供函数说明与块面板使用。
- `workspace/executeCommand`：`funroute.run` 用给定参数运行程序（汇率也是参数），失败时给出错误类别（其中找不到汇率是 `nofxrate`）；`funroute.render` 把契约写成注释附在规则上方。

**两种运行方式，同一份代码**：

```bash
funroute lsp -manifest registry.json     # stdio，给 VS Code 这类编辑器
make wasm                                 # web/dist/funroute.wasm，在浏览器的 Worker 里运行
```

### 签名清单：没有实现也能检查

浏览器里跑不了宿主的深度模型或网络调用，开发工具里通常也不应该链接它们。语言服务需要的只是函数的**签名**：

```go
manifest := hostRegistry.Manifest()   // 导出：签名、Doc、句柄、形式
json.Marshal(manifest)                 // 交给语言服务

base := funroute.CoreRegistry()            // 内核 + 标准库是原生实现
std.Register(base)
manifest.Apply(base)                   // 宿主的函数只登记签名
```

- 类型检查、悬停、补全、签名提示都照常工作。
- 运行时调用只有签名的函数，会返回 `funroute.ErrUnavailable`（它同时也是 `ErrExtension`，所以 `fallback` 会照常兜底）。`funroute.TrackUnavailable(ctx)` 会记下这次运行调用了哪些这样的函数，试运行的结果里会标出来。
- 清单和真实注册表的签名不一致时，`Apply` 会拒绝。
- **部署用的 artifact 由宿主用真实注册表编译**：宿主的纯函数如果标了 `Constexpr`，两边的常量折叠结果会不同，digest 也会跟着不同。

## 策略工作台

```bash
make run        # 构建前端与 wasm，组装 site/，然后启动静态服务：http://127.0.0.1:8080
```

工作台是纯静态页面：语言服务以 WebAssembly 的形式在 Worker 里运行。`make site` 把要发布的文件组装到 `site/`，`cmd/playground` 只负责提供这个目录，GitHub Pages（`.github/workflows/pages.yml`）上传的也是它，所以本地能跑的就是线上发布的。

- **编辑器**：CodeMirror 接上语言服务，高亮、诊断、补全、悬停、签名提示、格式化都来自服务端。`⌘/Ctrl + Enter` 运行。
- **代码与结构两个页签**：表达式区块里的"代码"是编辑器，"结构"是结构视图，右侧的试运行两边共用。只有当前文本已检查完、没有错误时才能切换，否则停在原页签并在状态栏说明原因。
- **结构视图**：同一段文本的投影。切回代码时文本与写下的一字不差：运算符、推导式、`2.9%` 这类写法原样保留，从不脱糖。`switch`、列表推导、`reduce`、`let`、`using`、`if`、`fallback` 画成卡片，其余部分是一行源码。这里的每一处修改，都是对原文某个区间的替换；选中一个块再点某个表达式，就用这个块把它包起来。
- **契约面板**：类型别名与参数（名字、类型、说明），推送给语言服务。
- **试运行**：
  - 顶部声明返回类型与说明（契约的返回部分放在它描述的结果旁边）。
  - 每个参数带类型与说明，值按 JSON 填写，原样交给服务端解码，大整数也不会丢精度。汇率参数就是报价的 JSON 数组：`[{"base":"USD","quote":"JPY","rate":"150.25"}]`。
  - 结果标出成功、失败或"有函数在浏览器里没有实现"，并给出结果类型与耗时。

**编辑器按键**：行为向 VS Code 看齐（Tab 接受补全、括号自动闭合、Alt 点击加光标、Shift+Alt 拖出列选择），按键用 Emacs 的：

| 按键 | 作用 |
|---|---|
| `C-a` / `C-e` / `C-k` / `C-y` | 行首 / 行尾 / 剪到行尾 / 粘贴 |
| `C-s` | 搜索 |
| `M-/` | 补全 |
| `M-;` | 注释 |
| `C-/` | 撤销 |
| `⌘/Ctrl + Enter` | 运行 |
| `Shift + Alt + F` | 格式化 |

浏览器自己占着的键（如 `C-w`、`C-n`、`C-t`）拿不到。

**前端构建与复用**：

- 前端是 TypeScript（`web/src/`），用 esbuild 打包到 `web/dist/`。产物不提交，`make site`（`make run` 与 Pages 都经过它）会先构建；只用 Go 的语言、CLI 与语言服务不需要 Node。
- 每个组件单独成一个模块，公共部分拆成共享 chunk，别的页面可以按需引用：

  | 模块 | 内容 |
  |---|---|
  | `lsp.js` | `startClient`，可传入自己的 Worker |
  | `editor.js` | `createEditor`，样式自带 |
  | `contract.js` | `<fr-contract>` |
  | `runner.js` | `<fr-runner>` |
  | `canvas.js` | `<fr-structure>` |
  | `app.js` | 把以上组装起来的工作台 |

- 颜色只来自 `web/tokens.css`，引入它就有亮暗两套主题。
- 运行时依赖只有 Lit、CodeMirror 与它的 Emacs 键位（`@replit/codemirror-emacs`），只在前端；Go 这边仍然零第三方依赖。

示例定义在 `web/funroute-examples.json`，每条都带契约、样例入参和期望结果（入参与 `funroute.run` 同形，汇率也在其中）。测试会通过语言服务逐条运行，并要求这些示例合起来覆盖示例注册表的全部函数和形式、全部运算符和全部节点种类 —— 新增了能力却不补示例，CI 会失败。

## 性能

字节码装载时翻译成带类型的寄存器形式再执行：参数、常量和局部变量原地读，内核的算术与比较是一条条专用指令，比较与其后的跳转、推导式的收集与下一轮都合成一条。循环体只是 int/float/bool 的运算、筛选、折叠或收集时，按 256 个元素一块按列执行（可能停下的循环从 16 个一块起逐块翻倍），遇到会失败或停下的元素就交回逐个执行，结果与错误都不变。Artifact 的格式与 digest 不受影响。

**执行本身不分配内存**。装载时的值流分析知道每个数组去了哪里：只在程序里被遍历、取长度或下标的数组建在帧自己的内存里，帧跨运行复用，稳定之后 0 次分配；只有交给宿主的结果才要自己的内存，而 `Program.RunInto` 可以把它建在宿主给的切片里。金额也一样：边界上的币种扫描、金额运算、换汇与 `using` 都是 0 次分配。

编译器另做两处不改变结果的改写：声明了 `Fold` 的函数（`sum`、`any`、`all`、`first`、`len`）套推导式时编译成单遍折叠，不建中间数组；嵌套推导的内层源不依赖外层元素时只算一次。`any`/`all` 因此在决定答案的元素处停下，后面的元素不再计算，也不会报错，和 `||`、`&&` 一样。

Apple M5 上的几个数（完整的对照表见 [`docs/perf.md`](docs/perf.md)，由 `make perf` 生成，每项旁边是同一件事直接用 Go 写的耗时）：

| 场景 | 耗时 | 分配 |
|---|---|---|
| `amount * bps / 10000 + fixed`：`RunValues` / `Program.Run` / `Run(map)` | 32 / 47 / 54 ns | 0 |
| 按 Go 签名注册的宿主函数调用（常见签名不经反射） | 45 ns | 0 |
| `[x + 1 for x in xs]`，每个元素 | 1.2 ns | 结果 1 次 |
| `sum([x * 2 for x in xs if x % 3 == 0])`，每个元素 | 4.2 ns | 0 |
| 500 元素 `reduce` | 1.0 µs | 0 |
| 把 16 到 65536 个 float 交给宿主函数（与长度无关：不拷贝） | 65 ns | 0 |
| 模型调用（引擎每次 20 µs）：单条 vs 64 条一批 | 27 µs vs 0.66 µs / 请求 | — |

## 现状

**已完成**：

- 语言：解析、类型推导与重载、编译期求值、record 与字段更新、nominal 枚举与穷尽检查、推导式与 `reduce`、格式化器。
- 金额：`money`/`ratio`/`fxrate`/`currency`、显式舍入与 `round` 内的精确计算、换汇 `->` 与 `using`、汇率加点与比较、`implied`、`prorate`、`round_to`、按策略分摊。
- 执行与宿主：宿主契约、字节码 VM、Artifact digest、类型化绑定、句柄与模型批处理、超时与 `fallback`、类型化错误、标准库、签名清单。
- 工具：语言服务（stdio 与 WebAssembly）与策略工作台。

**用于真实支付前还缺**：日期与时间、显式业务错误、决策 trace。语言服务还缺错误恢复（写到一半的程序目前只能给出词法层面的事实）和对表达式内部注释的格式化。详细计划与取舍见 [`docs/roadmap.md`](docs/roadmap.md)。

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
go test ./internal/machine -bench . -benchtime 2000x   # VM 基准
```

`make lint` 强制三条预算：函数不超过 50 行、嵌套不超过 3 层、文件不超过 800 行。

代码结构：

```text
funroute.go            公开包 funroute：只有别名与转发，根目录唯一的 Go 文件
lsp/                   语言服务：协议、stdio 传输
extensions/std/        标准库，只用公开 API
internal/money/        金额、比例、汇率、币种表与舍入：纯 Go 运算（只依赖标准库）
internal/machine/      值、类型、字节码、VM、注册表、目录、签名清单（依赖 money）
internal/syntax/       词法、语法、AST、ExprJSON、词法段、格式化、语法树（只依赖 machine）
internal/compile/      推导、编译、常量折叠、契约、Analyze（依赖 syntax + machine）
internal/demo/         演示控制台：宿主组装注册表的范例，工作台用它（只用公开包）
examples/              可运行的 Go 宿主程序：路由、金额、批处理（go run ./examples/<名字>，只用公开包）
tests/api/             公开面的测试与 godoc 示例，按主题组织（只用公开包）
tests/conformance/     把 web/funroute-examples.json 的每个示例经公开 API 跑完整条流水线
tests/limits/          生成并守着 docs/limits.md 的表格（make limits）
tests/perf/            性能报告，写进 docs/perf.md（make perf）
tests/perf/expr/       与 expr 的对照（单独的 module，只有它依赖 expr）
web/src/               工作台前端（TypeScript），每个组件一个入口，打包到 web/dist/
web/wasm/              浏览器用的语言服务入口（js/wasm）
cmd/funroute  cmd/playground  CLI（含 fmt、lsp）与工作台静态服务
```

`internal/` 下的实现只有 `funroute.go` 与 `lsp/` 可以导入，由 `make lint` 检查。

改动时的同步点与必须守住的不变量记在 [`CLAUDE.md`](CLAUDE.md)。

## 附录 A：语法参考

下面的文法与词法规则是解析器的完整规格；日常写规则只需要前面的[语言导览](#语言导览)。

```text
// ── 词法 ─────────────────────────────────────────────────────────────
// token 之间可以有空白（ASCII 空白字符）和注释（// 到行尾）；token 内部不能有注释，也不能有写出来之外的空白。
// 词法器从左到右每次取尽量长的一段，认成下面的一种 token。一段文字合乎几种写法时按这个次序取：
// 保留字，然后形如币种代码的 code，然后 name；数字后面紧贴 % 或 bps 就是 ratio，不是 number。
digits     = digit { [ "_" ] digit }                // 下划线只能夹在两个数字之间：1_000
decimal    = digits [ "." digits ]                  // 数字后的 "." 必须接数字
exponent   = ( "e" | "E" ) [ "+" | "-" ] digits
number     = decimal [ exponent ]                   // 没有 "." 也没有指数是 int，否则是 float；float 必须恰好是写下的值（0.30000000000000001 报错）
ratio      = decimal ( "%" | "bps" )                // 紧贴数字的 % 一律是比例的单位，带指数的数也一样（1e3% 报错）；bps 后不能紧接字母或数字
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
primary        = number | ratio | money | fxrate | code | string | "true" | "false" | enum
               | name                               // 变量；第一个点之后是字段读取：order.amount
               | name "(" [ args ] ")"              // 函数调用，名字可带点：route.score_v1(x)
               | "switch" "(" [ expression "," ] case { "," case } ( [ "," ] ")" | "," "else" "=>" expression ")" )
               | "reduce" "(" locals "in" expression [ "if" expression ] "," word "=" expression "," expression ")"
               | "let" "(" binding { binding } expression ")"
               | "using" "(" quote { "," quote } "," expression ")"      // 至少一个报价，最后是主体
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
quote          = expression                         // 一个 fxrate 或 array<fxrate>；外层的汇率用 fx(base, quote) 读进来
```

几条容易踩到的规则：

- **金额的负号写在数上**：`USD -1.70`（与宿主文本、`EncodeJSON` 的输出同一种写法）。`-USD 1.70` 是对金额取负，和 `-x` 一样；`USD - 1`、`USD-1` 是币种减 1，类型错误。
- **代码形状的名字永远是币种**：`USD`、`HTTP`、`ORDER1` 都不能当变量。`USD.x` 是错误（币种没有字段），`USD(1)` 是名叫 `USD` 的函数调用。
- **名字与点**：`order.amount` 连同其中的点是一个名字。后面紧跟 `(` 时整个名字是函数名（`route.score_v1(x)`，语言没有一等函数，`route` 不是变量）；否则第一段是变量，其后每一段是字段读取。变量名本身不能带点。
- **保留字引出的形式**：`switch`、`reduce`、`let`、`using` 外形像函数调用，但内部用 `case … =>`、`else =>`、`x in xs`、`acc = init` 标出各个位置；`if`、`fallback` 则是普通的函数名。
- **`if` 不能作变量名与局部名**（字段可以），因为它也是推导式与 `reduce` 里筛选子句的开头。
- **`switch` 的两种形态**：有主语时，`case` 的值与主语比较相等，几个值任一相等就选这一支；没有主语时每个 `case` 是 `bool` 条件，取第一个成立的。只有主语是契约里的枚举、且各 `case` 覆盖了全部成员时才能省略 `else`，所以运行时不会有"没有分支匹配"。
- **嵌套至多 1000 层**（括号、调用、容器、前缀运算符各算一层），更深的是语法错误；ExprJSON 同一上限。

## 附录 B：取值范围

每种类型能表示什么、在哪里会不精确、超出时怎样，一张表说完。超出范围一律是错误（能在编译期算出的在编译期报），从不悄悄回绕或截断。

| 类型 | 范围与精度 | 精确吗 | 超出或非法时 |
|---|---|---|---|
| `bool` | `true` / `false` | — | — |
| `int` | −9,223,372,036,854,775,808 ~ 9,223,372,036,854,775,807（约 ±9.22×10¹⁸） | 精确 | 溢出、除零是 `ErrArithmetic`；整数除法向零截断 |
| `float` | IEEE 754 float64，约 ±1.8×10³⁰⁸，15–17 位有效数字 | 不精确：`0.1 + 0.2` 是 `0.30000000000000004` | NaN / Infinity 进不来，算出来也报错 |
| `string` | 任意长度的合法 UTF-8 | 精确 | 下标与 `len` 按码点数 |
| `array<T>` / `dict<T>` | 长度只受内存限制；字典的键是字符串 | — | 越界、缺键报错 |
| `record{…}` | 字段固定，按声明顺序存放 | — | 缺字段是另一个类型 |
| `enum` | 契约声明的成员 | — | 不是成员就不是这个类型 |
| `money` | int64 个最小单位；能表示多少主单位取决于币种的小数位，见[表达能力与精度](#表达能力与精度) | 精确 | 溢出报错；位数超过币种小数位报错 |
| `ratio` | 约分后的 int64 分子/分母（各约 18 位数字）；小数文本至多 18 位小数 | 精确：`1/3` 就是三分之一 | 分子或分母放不下是 `ErrArithmetic` |
| `fxrate` | 正的 int64 分子/分母，另带两边币种；同一币种之间恒为 1 | 精确：`150.25` 就是 601/4，报价的倒数也精确 | 零或负数、同币种却不是 1、放不下都是 `ErrArithmetic` |
| `currency` | 注册表声明的代码：大写字母开头，3–8 位大写字母或数字 | — | 未声明是 `ErrCurrency` |

每种运算在哪里停下、程序能多大多深、性能与表达能力的边界，实测的数字见 [docs/limits.md](docs/limits.md)。

金额用 `money`、比例用 `ratio`，不要用 `float`：`float` 是二进制近似，适合评分、概率这类本来就不精确的量。
