# 契约与类型推导

规则的参数、类型与返回类型由宿主给出；没给的由推导补上，能在编译期算完的就算完。

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

[局部绑定](language.md#局部绑定let)那个例子编译出 7 条指令、0 个局部变量槽：三个绑定都被折成了常量，运行时只剩一次乘、一次除、一次加。

哪些函数能在编译期调用由宿主授权（`Doc.Constexpr`）。内核和标准库都可以；模型、时钟、远程调用**不应该**标 —— 否则编译规则时就会去调用推理引擎，同一条规则在不同时间编译出不同的结果。
