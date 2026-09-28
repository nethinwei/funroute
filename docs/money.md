# 金额

声明了币种的注册表才有金额：精确的最小单位整数、显式舍入、只在 `using` 里换汇。取舍见 [roadmap.md](roadmap.md) 的「金额」「舍入」「换汇」。

宿主调用 `Registry.DeclareMoney` 声明币种（代码与小数位）之后，语言多出四种类型：`money`、`ratio`、`fxrate`、`currency`。没有声明的注册表里它们不存在，不写金额的程序编译出的 artifact 连 digest 都与以前逐字节相同。金额与比例的**写法**属于语法，与是否声明无关：在任何注册表里 `10%3` 都是语法错误，形如币种代码的名字（`URL`）都不能当变量。

```go
registry.DeclareMoney(funroute.MoneySpec{Currencies: std.ISO4217()})
```

整套设计只有三条规则：

1. **币种是值的属性，不是类型的一部分**。`money` 就是一个类型，和 Go 的 `funroute.Money{currency, minor}` 是同一个东西；两笔金额币种对不对，在它们相遇的运算上检查。
2. **舍入永远写出来**。没有注册表级的默认舍入方式：会落到两个最小单位之间的运算，要么写在 `round(…, @mode)` 里，要么当场写出舍入方式。
3. **汇率就是参数**。换汇只能写在 `using(汇率…, 主体)` 里，汇率要么写在规则里，要么是契约声明的参数；没有隐藏的运行时汇率表。

## 类型与字面量

| 类型 | 写法 | 说明 |
|---|---|---|
| `money` | `USD 1.70`、`USD -1.70`、`JPY 100` | 某币种的 int64 个最小单位；负号写在数上；代码与数之间只能是空格或 Tab（不能换行、不能有注释）；位数超过币种的小数位是编译错误，不舍入 |
| `ratio` | `2.9%`、`25bps`、`0.029` | 精确的比值（int64 分子/分母）；小数字面量和金额或比例一起运算时就是 `ratio`，按写下的数字精确读入（至多 18 位小数），从不经过 float；和 float 一起时仍是 float |
| `fxrate` | `150.25 JPY / USD` | 1 USD 换 150.25 JPY，精确比值，另带两边币种 |
| `currency` | `USD`、`JPY` | 币种字面量；可进 `switch`、作字典的键、和 `==` 比较 |

几点说明（写法的精确规则见[附录 A](grammar.md#语法参考)）：

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

## 运算

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

## 舍入：`round(…, @mode)`

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

## 换汇：`->` 与 `using`

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

## 表达能力与精度

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

## 币种检查与函数

**币种检查**：在运算发生的地方检查——加减、比较、`==`、`in`、金额相除、换汇、标准包的聚合（同一数组里的金额必须同币种）。两边都是写死的币种时，常量折叠在编译期就报出来（`USD 1 + EUR 1`）；其余在运行时比对，币种不一致、币种未声明都是 `ErrCurrency`，`fallback` 不接它。参数的币种没声明同时也是 `ErrContract`；宿主函数返回的金额币种没声明也是 `ErrCurrency`。比较两个 `currency` 值本身就是在问是否同一币种，永远有答案。

**函数**：

- `money(最小单位, 币种)` 由整数与币种造出金额（输入里两者分开时用：`money(o.amount, o.currency)`，`o.currency` 声明为 `currency`）；`currency_of(字符串)` 校验币种代码。
- `minor`、`currency`、`sign` 把金额拆开；`string(币种)` 得到代码。`minor(...)` 得到的是普通整数：拿它造回另一币种的金额（`money(minor(a), JPY)`）在语言里是允许的，币种对不对由规则作者负责。
- `allocate(m, [权重])` 与 `allocate(m, n)` 分摊且一分不丢；策略是注册表提供的枚举 `allocation` 的成员，契约不能再用这个名字。
- `prorate(m, part, whole)` 按比例取金额（part 与 whole 可以是整数，也可以是另一币种的两笔同币种金额）；`round_to(m, 粒度, @mode)` 做现金舍入。
- `implied(到账额, 支付额)` 得成交汇率，`fx(基准, 报价)` 读出当前 `using` 里的汇率，`ratio(字符串)` 把十进制文本或分数精确读成比例（读不出是 `ErrArithmetic`）。
- 标准包的 `sum`/`min`/`max`/`avg`/`median`/`sort`/`cumsum`/`sort_by`/`top_k` 等在声明了币种后也接受金额。

**宿主那边**：Go 用 `funroute.Money`、`funroute.Ratio`、`funroute.FxRate`、`funroute.Currency`，由币种表构造（见[在 Go 里算钱](go.md#在-go-里算钱)），`[]funroute.Money`、`[]funroute.FxRate` 与规则之间零拷贝；JSON 输入认 `"USD 1.70"` 与 `{"currency": "USD", "minor": 170}` 两种，汇率是 `{"base": "USD", "quote": "JPY", "rate": "150.25"}`（`rate` 也可以是分数 `"1/3"`）；金额、比例与汇率里的数只收字符串、`json.Number` 与整数，float64 一律拒绝——解码成 `any` 的 JSON 数已经被舍入过，`funroute.DecodeArgs` 把数保留为原文；`Registry.EncodeJSON` 把金额写成 `"USD 1.70"`。
