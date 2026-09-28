# 语言导览

FunRoute 的值、类型、运算符与各个形式。金额见 [money.md](money.md)，完整文法见 [grammar.md](grammar.md)。

## 值与类型

| 类型 | 字面量 | 说明 |
|---|---|---|
| `bool` | `true` | |
| `int` | `42`、`1_000_000` | 有符号 64 位，溢出报错 |
| `float` | `0.25` | float64，按 IEEE 754，NaN 与 ±Infinity 是合法的值 |
| `string` | `"SGD"` | 必须是合法 UTF-8（原文与转义结果都查），JSON 风格转义 |
| `array<T>` | `[1, 2, 3]` | 元素同型 |
| `dict<T>` | `{"primary": 1}` | 键是字符串，值同型 |
| `record{…}` | `{amount: 1200, currency: "SGD"}` | 字段固定、各有类型，见[记录](#记录) |
| `enum<name>{a,b}` | `@adyen` | 只能由契约声明，见[枚举](contracts.md#枚举) |
| `money` | `USD 1.70` | 某币种的最小单位整数，币种是值的属性；注册表声明币种后才有，见[金额](money.md) |
| `ratio` | `2.9%`、`25bps` | 精确比例，只和金额、比例一起用 |
| `fxrate` | `150 JPY / USD` | 1 USD 换多少 JPY，精确比值；也来自契约或 `implied(到账额, 支付额)` |
| `currency` | `USD` | 注册表声明的币种，写它的代码即可 |
| `time` | `time("2026-09-28T10:00:00+08:00")` | 与时区无关的时刻；宿主传 `time.Time`，JSON 是 RFC 3339 文本，见[时间](#时间) |
| `duration` | `90s`、`2h30m`、`1500ms` | 时长，单位 `h` `m` `s` `ms` `us` `ns`；宿主传 `time.Duration`，JSON 是 Go 的时长文本 `1h30m0s` |

几条规则：

- 字符串与数值之间不做隐式转换，用 `int(...)`、`float(...)`、`string(...)`、`bool(...)` 显式转换。
- 空的 `[]` / `{}` 需要从上下文（函数签名或契约）得到元素类型。
- 取整函数（`ceil`/`floor`/`round`）对 float 返回 `int`（金额的 `round(m, @mode)` 见[金额](money.md)）。要 float 就写 `float(floor(x))`。
- **没有 null**。数组越界、字典缺键、除零都是错误。要兜底就写出来：`get(d, "k", 0)`。
- 金额不要用 `float`：注册表声明币种后用 `money`，见[金额](money.md)。
- 每种类型的范围、精度和越界时的行为见[附录 B](grammar.md#取值范围)；完整文法见[附录 A](grammar.md#语法参考)。

## 时间

`time` 是时刻，`duration` 是时长，两者都按纳秒计。时刻加减时长还是时刻，两个时刻相减是时长，时长能加减、乘除整数、互相比较：

```text
now > paid_at + 30m                     // 已经过了半小时
now - paid_at                           // 过了多久：duration
hour(paid_at, "Asia/Shanghai") >= 22    // 上海时间晚上十点以后
weekday(paid_at, "UTC") >= 6            // 周末：1 是周一，7 是周日
add_days(start_of_day(now, "Asia/Shanghai"), 1, "Asia/Shanghai")   // 上海明天零点
```

- **没有 `now`**。当前时间由宿主作为参数传入，同一输入永远同一答案。
- **时区写在调用处**：`hour`、`weekday`、`day`、`month`、`start_of_day`、`add_days` 都要一个 IANA 时区名（`"Asia/Shanghai"`、`"UTC"`）；不认识的名字、空串与 `"Local"` 是 `ErrDomain`。时区规则来自宿主机器的 tzdata，所以这些调用不在编译期折叠。
- `add_days` 按那个时区的日历加天，钟点不变；遇到夏令时切换，一天不是 24 小时。
- 负的时长写 `-30m`；对变量取负写 `0s - d`（`-d` 是 `0 - d`，整数减时长没有定义）。

## 运算符

运算符只是函数调用的简写，`a + b` 就是 `add(a, b)`。优先级从低到高：

| 优先级 | 运算符 | 对应函数 |
|---|---|---|
| 1 | `\|\|` | `if(a, true, b)`（短路） |
| 2 | `&&` | `if(a, b, false)`（短路） |
| 3 | `==` `!=` | `eq`，`!=` 是 `eq` 取反 |
| 4 | `<` `<=` `>` `>=` `in` | `lt` `le` `gt` `ge` `member` |
| 5 | `->` | `convert`（换汇，见[换汇](money.md#换汇--与-using)；只在声明了金额的注册表里有） |
| 6 | `+` `-` | `add` `sub` |
| 7 | `*` `/` `%` | `mul` `div` `mod`（数字字面量后的取模要空格：`10 % 3`；`10%` 是比例） |
| 8 | `!` `-`（一元） | `if(a, false, true)`、`sub(0, a)` |
| 9 | `xs[i]` `d["k"]` | `at` |

二元运算符左结合，括号可改变优先级。`&&` 和 `||` 展开成惰性的 `if`，所以天然短路 —— `x != 0 && 10 / x > 2` 不会除零。`->` 比 `+`/`-` 松、比比较紧：`fee + amount -> JPY` 换的是和，`amount -> JPY > cap` 比的是换后的金额，`(amount -> JPY) * 2` 要括号（`->` 要写在 `using` 与 `round` 里，见[金额](money.md)）。

`in` 有三种用法：`x in xs` 查数组元素，`"k" in d` 查字典键，`"b" in text` 查子串。字符串按 UTF-8 码点处理，`s[i]` 返回单字符字符串。

注释用 `//`，数字可以写 `1_000_000`，列表可以带尾随逗号。

## 条件：`if` 与 `switch`

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

## 局部绑定：`let`

`let(名字 = 值, …, 结果)` 给中间值起名：

```text
let(
  bps       = 250,
  base_fee  = 3 * 100 + 50,
  total_bps = bps * 2,
  amount * total_bps / 10000 + base_fee
)
```

后面的绑定可以引用前面的。这个例子里三个绑定都不依赖参数，编译期就被算成常量（见[编译期求值](contracts.md#编译期求值)）。

## 遍历：列表推导

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

## 聚合：内核库与 `reduce`

求和、计数、取最值这些日常操作，直接用内核库的函数，`funroute.CoreRegistry()` 自带，不用另外注册：

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

内核库与标准库的主要内容（完整清单见工作台的「函数说明」，或在编辑器里悬停查看；每个函数都附有可以直接运行的案例）。常用的在内核里，调用就是内核调用：不查截止时间、不再检查结果类型，直接操作数组与字典自己的 backing；低频的与金额的重载在标准库 `extensions/std`（`std.Register(registry)`）：

| 类别 | 内核库 | 标准库 |
|---|---|---|
| 聚合 | `sum` `min` `max` `any` `all` `avg` `median` `stddev` `percentile` | 它们的金额重载 |
| 序列 | `range` `indices` `first` `last` `take` `slice` `reverse` `concat` `unique` `flatten` `sort` `sort_desc` `top_k` `bottom_k` | `take_while` `drop_while` `windows` `chunk` `deltas` `cumsum` |
| 选择与分组 | `index_of` `arg_min` `arg_max` `min_by` `max_by` `sort_by` `sort_by_desc` | `group_by` `rank` `intersect` `except` |
| 字典 | `get` `merge` | |
| 时间 | `time` `hour` `weekday` `day` `month` `start_of_day` `add_days` | |
| 字符串 | `upper` `lower` `trim` `trim_prefix` `trim_suffix` `contains` `starts_with` `ends_with` `index_of` `last_index_of` `matches` `split` `join` `replace` | `pad_left` `pad_right` `repeat`（结果至多 10000 个字符） |
| 数值 | `abs` `ceil` `floor` `round` `pow` `mod`（float） | |

`sum`、`any`、`all` 与内核的 `len` 套推导式时边算边折叠，不建中间数组；`any`/`all` 在决定答案的元素处停下，后面的元素不再计算——`any([10 / x > 2 for x in xs])` 在第一个为真的元素之后不会再除零，和 `||` 一样。

`range(n)` 是唯一能凭空造出数组的函数，所以它的参数必须由输入规模界定：字面量、`len(容器)`，或它们的算术组合。`range(len(fees))` 可以，`range(n)`（`n` 是任意入参）不行 —— 否则一个整数就能让规则跑任意久。

## 记录

`record` 对应宿主手里的对象：字段固定，每个字段有自己的类型。规则可以读字段，也可以构造新记录返回：

```text
{net: order.amount - fee, currency: order.currency}
```

- **字段顺序是类型的一部分**。`record{a: int, b: int}` 和 `record{b: int, a: int}` 是两个类型。字段访问在编译期解析成下标，运行时不查名字。
- **没有缺失字段**。传入的数据少一个字段就直接拒绝；读一个不存在的字段是编译错误。多出来的字段会被忽略。
- **靠写法区分字典和记录**：`{"k": v}` 是字典，`{k: v}` 是记录。
- **相等逐字段比较**，`==`、`in`、`switch` 都适用。
- 可以随意组合：`[o.amount for o in orders]`、`order.tags[0]`、`dict<record{…}>` 都成立。

**按字段选择**：调用第一个实参之后写 `.字段`，给第一个实参的每一项按字段取键，第一个实参只写一遍。

```text
sort_by(channels, .fee)            // 即 sort_by(channels, [c.fee for c in channels])
top_k(channels, .meta.success, 3)  // 嵌套字段照样点下去
min_by(channels, .fee).name        // 最便宜的那个渠道
group_by(orders, .channel)
```

- 选择器就是那条推导式，答案与失败都一样；第一个实参不是名字时只求值一次。所以它需要注册表开启推导式（`for`）。
- 只能写在调用第一个实参之后，第一个实参是它读的数组；写在别处是编译错误。
- 记录本身不可排序。按多个字段排序就按次要的先排：排序是稳定的，`sort_by(sort_by(xs, .name), .fee)` 先按 `fee`、再按 `name`。

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
