# FunRoute

FunRoute 是面向支付路由的强类型、纯表达式函数语言。仓库同时提供 Go SDK、无框架拖拽式 JS 组件和可直接运行的 MVP。它的核心约束是：

- 没有语句、赋值、可变变量、反射或隐式宿主能力；
- 自由变量自动成为函数参数；
- 参数类型由内置函数和扩展函数的签名反向推导；
- 只做安全、可解释的数值提升，跨领域转换必须显式写 `int(...)`、`float(...)`、`string(...)` 或 `bool(...)`；
- 编译产物只引用精确的函数签名，运行时注册表漂移会拒绝装载；
- 内核只有 10 个函数名，惰性形式 `switch(...)`、`for(...)`、`reduce(...)`、`recur(...)` 由宿主按控制台逐个启用；
- 启用 `recur(...)` 即得到图灵完备层，不终止的程序由 fuel 与递归深度拦截；
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

它会被推导为 `(country: string) -> string`。遍历和筛选也保持函数形态：

```text
for(channels,channel,route.is_healthy@1(channel),channel)
```

四个参数依次是输入数组、局部名称、过滤条件和生成结果；过滤条件可省略。`channel` 是局部变量，不会出现在外部 `args` 中，结果类型自动推导为 `array<string>`。

聚合用 `reduce`，同样保持函数形态：

```text
reduce(prices,price,total,0,add(total,price))
```

五个参数依次是输入数组、元素局部名、累加器局部名、累加器初值和累加表达式；`price` 与 `total` 都是局部变量，不进入外部 `args`，推导结果是 `(prices: array<int>) -> int`。`reduce` 只遍历有限数组，每次迭代按 fuel 计费，不引入递归。

## 语法

```text
program    = expression
expression = expression binary expression      // 中缀，见下表
           | unary expression                  // ! -
           | primary
primary    = integer | float | string | "true" | "false"
           | identifier
           | identifier "(" [ expression { "," expression } [ "," ] ] ")"
           | "[" [ expression { "," expression } [ "," ] ] "]"
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

`for` 与 `reduce` 还接受关键字形式，让位置参数的含义写在语法里（两种形式脱糖到同一个节点）：

```text
for(channel in channels where is_healthy(channel), channel)
reduce(price in prices, total from 0, total + price)
```

`in`、`where`、`from`、`else`、`case` 是保留名，不能用作变量名或函数名。

`switch`、`for`、`reduce` 和 `recur` 仍使用上面的函数调用外形；编译器把它们识别为惰性、多分支和局部变量结构，不引入语句式语法。

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

## 类型推导

类型的唯一权威来源是函数注册表：

1. 内置函数；
2. 业务扩展函数。

变量在规范化 AST 中第一次出现的顺序决定参数顺序；字典节点会先按 key 排序，保证源码与 ExprJSON 往返后的参数 ABI 不变。编译器为每个参数建立类型变量，再使用所有函数签名做统一（unification）。例如：

```text
if(risk.approved@1(country,amount),"primary","backup")
```

扩展函数若声明 `risk.approved@1(string, int) -> bool`，编译器会联合
`if(bool,T,T)->T` 自动推导：

```text
(country: string, amount: int) -> string
```

类型便利规则按“安全且可解释”设计：

- `add(a,b)` 没有其他约束时默认把自由参数推导成 `int`；
- `add(1,1.5)` 这类混合数值会把整数精确提升为 `float`；超过 float64 精确整数范围时拒绝，而不是静默丢精度；
- 表达式里出现浮点字面量时，变量会被拉成 `float`：`risk < 0.5` 推出 `(risk: float)`，`amount * 1.5` 推出 `(amount: float)`。混合数值签名（`(int,float)`）是**末选**，只在不存在同型解读时才用；
- 字符串和数值之间不做隐式转换，使用 `int(...)`、`float(...)`、`string(...)`、`bool(...)` 明确表达；
- 扩展函数签名和字面量优先决定类型，例如 `route.score@1(success,cost)` 会推出两个参数都是 `float`。

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

`!=`、`&&`、`||`、`!` **不是**内核函数，而是 `if` 的糖：`a != b` 是 `if(eq(a,b),false,true)`，`a && b` 是 `if(a,b,false)`。因为 `if` 惰性，`&&`/`||` 自动短路。

其余一切都是宿主的选择：`lang.RegisterArrayPrimitives(registry)` 加上 `array.is_empty`/`array.prepend`/`array.head`/`array.tail` 四个列表原语，`registry.EnableForm(...)` 打开惰性形式。

## 形式开关与控制台

惰性形式不是内建关键字，而是注册表上的开关。语法始终能被解析，**能不能用由注册表决定**：

```go
operator := lang.CoreRegistry()
operator.EnableForm(lang.SwitchForm, lang.ForForm, lang.ReduceForm)

engineer := lang.CoreRegistry()
engineer.EnableForm(lang.SwitchForm, lang.ForForm, lang.ReduceForm, lang.RecurForm)
```

| 形式 | 语义 | 终止性 |
|---|---|---|
| `switch(...)` | 值匹配或条件链，按顺序惰性选择一个结果 | 结构上总是终止 |
| `for(source,item,[condition],result)` | 映射，可选筛选 | 只遍历输入数组 |
| `reduce(source,item,acc,init,body)` | 折叠进累加器 | 只遍历输入数组 |
| `recur(args...)` | 用新实参重新进入整个表达式 | 图灵完备，靠 fuel 与 `MaxRecursion` 拦截 |

一个注册表就是一个控制台：运营控制台开 `switch/for/reduce`，工程师控制台再加 `recur`。用未启用的形式会在编译期被拒绝：

```text
recur is not enabled in this registry
```

这条检查在 AST 上做，源码和 ExprJSON 走同一条路径，所以运营侧直接提交 ExprJSON 也拿不到递归。`recur(args...)` 的实参个数与类型必须与推导出的参数完全一致：

```bash
go run ./cmd/funroute run -profile engineer \
  -expr 'if(eq(n,0),acc,recur(sub(n,1),add(acc,n)))' \
  -args '{"n":100,"acc":0}'
```

返回 `5050`。不终止的程序不是编译期错误，而是运行期被拦截：

```text
recursion limit 8 exceeded
execution fuel exhausted at instruction 3
```

`Registry.Catalog()` 只列出该注册表启用的形式，因此拖拽面板看到的就是它实际能用的语言。

随机访问、排序、聚合、支付渠道能力、成本模型等不进入通用内核，由使用者注册扩展函数。

## 扩展函数

扩展函数与内置函数使用同一注册接口：

```go
registry := lang.CoreRegistry()

err := registry.Register(lang.FunctionSpec{
    Name:   "risk.score@1",
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

名称中的 `@1` 是推荐的 ABI 版本。Artifact 同时冻结完整签名和 fuel 成本；同名函数被改成不同类型或成本时，旧 Artifact 会拒绝实例化。

`Display` 只用于控制台展示，不参与类型推导和执行。`Registry.Catalog()` 会输出全部可展示函数的标签、说明、分类、颜色、图标、参数说明、结果说明、示例、成本和类型签名。拖拽组件直接消费该目录，所以新增扩展函数不需要再维护一份前端清单。完整示例见 `extensions/paymentdemo/`。

Go 无法从语言层证明回调实现真的无副作用，因此生产扩展 SDK 仍需代码审查、静态检查和 capability 封装。VM 会克隆输入值，并捕获 extension panic，但不会把数据库、网络、时钟等能力主动暴露给函数。

## API

```go
expr, _ := lang.Parse(`if(a,b,add(1,1))`)
exprJSON, _ := lang.ExportExprJSON(expr)
sameExpr, _ := lang.ImportExprJSON(exprJSON)

artifact, _ := lang.CompileAST(sameExpr, registry, lang.CompileOptions{})
runtime, _ := lang.Instantiate(artifact, registry)
result, _ := runtime.Run(
    map[string]any{"a": false, "b": 9},
	lang.RunOptions{Fuel: 10_000},
)
```

职责划分：

```text
Parse / ImportExprJSON
        ↓
类型推导 + 重载选择
        ↓
CompileAST → 不可变字节码 Artifact
        ↓
Instantiate → 绑定精确扩展签名
        ↓
Run(args) → Value
```

## ExprJSON

ExprJSON 当前是 **v2**：v1 的 `switch` 节点把 `match` 存成单个节点且要求 `value` 必须存在，v2 改成列表与可选主体，旧文档会被明确拒绝而不是猜测解读。

`export` 命令导出的是代码 AST，不是求值结果：

```bash
go run ./cmd/funroute export \
  -expr 'if(a,b,add(1,1))'
```

整数、浮点、字符串和变量均有不同的 node tag；字典键会排序。因此：

```text
Export(Import(Export(expr))) == Export(expr)
```

可以直接用于可视化编辑、版本 diff、digest 和回放。

## 拖拽式 JS 库

`web/funroute-designer.js` 注册原生 Web Component，`web/funroute-source.js` 是它与宿主共用的纯函数模块（表达式打印、格式化、词法分析，不碰 DOM）：

```html
<link rel="stylesheet" href="funroute-designer.css">
<funroute-designer id="designer"></funroute-designer>
<script type="module">
  import "./funroute-designer.js";
  const designer = document.querySelector("#designer");
  designer.catalog = await fetch("/api/catalog").then(r => r.json());
  designer.value = {version: 1, expr: {node: "int", int: 1}};
  console.log(designer.value);  // ExprJSON
  console.log(designer.source); // 1
</script>
```

组件支持：

- 从函数目录拖入函数卡片；
- 把变量、整数、浮点数、字符串、布尔值、数组和字典拖入参数槽；
- 移动、删除和嵌套已有节点；
- 可增删分支的 `switch(...)` 卡片，带局部名称/可选过滤条件的 `for(...)` 卡片，带元素名/累加器名的 `reduce(...)` 卡片；
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

- 支付路由：根据扩展函数 `route.is_healthy@1` 选择主备渠道；
- `switch`：按国家选择渠道；
- `for`：筛选健康渠道数组。

页面自上而下是：工具栏、带语法高亮的表达式编辑器、参数与执行结果、拖拽画布、规范化 ExprJSON。参数和结果紧挨着表达式，改完参数不用滚到页面底部再运行。

编辑器里直接键入表达式再点“生成节点”就解析成节点树（回车等价，Shift+回车换行），“格式化”把超过 40 字符的表达式按参数逐行缩进，“复制”把当前表达式送进剪贴板；拖拽修改节点时编辑器内容同步刷新为格式化后的源码。

MVP 服务只持有一个注册表，页面上没有权限开关：这个 demo 注册表启用了全部形式（含 `recur`），所以“递归示例”能直接跑。真实部署应按登录身份给不同注册表，运营控制台少写一个 `lang.RecurForm` 就是了。

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

VM 是确定性栈式字节码解释器，**程序执行本身不分配内存**——只有程序构造的数据（输入转换、`for` 的输出数组）才分配。Apple M5，`go test ./lang -bench .`：

| 基准 | 耗时 | 分配 |
|---|---|---|
| 2000 次尾递归（`recur` 求和） | 347 µs | 0 次 |
| 500 元素列表遍历（`head`/`tail` 递归） | 94 µs | 1 次 |
| 500 元素 `reduce` 折叠 | 48 µs | 1 次 |
| 500 元素 `for` 映射 | 49 µs | 3 次 |
| 简单表达式一次求值 | 239 ns | 0 次 |
| 编译（含类型推导） | 28 µs | — |

支撑这些数字的实现要点：

- **值不可变，内部零拷贝**：`items`/`entries` 是私有字段，包外只能通过 `Array()`/`Dict()` 拿到副本，所以 VM 压栈、绑定局部、折叠累加器都不深拷贝；`array.tail` 直接共享后缀，O(1)。
- **正确的尾调用**：尾位置的 `recur` 复用当前帧（编译期标记，`Instruction.C = tailCall`），尾递归是循环而非嵌套激活，因此只受 fuel 约束、不受 `MaxRecursion` 约束。参数位置的 `recur` 仍是真正的嵌套，受深度限制。
- **帧池化**：`Runtime` 用 `sync.Pool` 复用激活帧，栈与顶层参数存在帧内联数组里。
- **操作数零拷贝**：出栈返回栈上视窗而非副本，因此扩展函数收到的 `[]Value` 只在调用期间有效（`EvalFunc` 文档已说明，需要保留就自行复制）。
- **紧凑枚举**：`Kind` 与 `OpCode` 是 `uint8`，指令分派走跳表；两者的 JSON 表示仍是原来的名字，因此 Artifact digest 与既有 ExprJSON 不受影响。
- **panic 防护在激活层**：`recover` 每次激活一次，不在每次扩展调用上。

## 当前实现边界

当前是语言内核 v0.3：

- 已实现解析、类型推导、泛型与重载、数值安全提升、显式转换、扩展注册、规范化 JSON AST、字节码编译、Artifact digest、惰性 `if/switch`、共用一套循环指令的 `for/reduce`、可开关的 `recur`、fuel/stack/递归深度限制和 VM；
- `for`/`reduce` 的局部变量由编译器分配，不污染外部参数；语言不提供一等 lambda、闭包和自定义局部函数，图灵完备只通过 `recur` 自递归达成；
- 编译后端当前是确定性栈式字节码，稳定 API 已把后续 Wasm/JIT 与语言前端隔离；
图灵完备性的形式化证明（含不启用 `recur` 时的强正规化证明）见 [`docs/turing-completeness.md`](docs/turing-completeness.md)。

- 正式用于支付前，还需要 `decimal/money`、结构化 record、错误/Option 类型、决策 trace、Wasm 后端和独立宿主 ABI manifest。

选择这个顺序是为了先冻结函数式语法、类型便利规则和扩展边界，再替换机器码后端，避免语言语义与 JIT 同时变化。

## 验证

```bash
make ci     # check-fmt + vet + lint + build + test
make test   # 只跑测试
go test ./lang -bench . -benchtime 2000x   # VM 基准
```

`make lint` 由 `tools/lint`（仅标准库）实现，强制风格预算：单个方法不超过 50 行、嵌套不超过 3 层、单个文件不超过 800 行。

测试覆盖样例推导、数值提升与显式转换、同型容器拒绝、扩展函数推导、函数式 `switch/for/reduce`、形式开关边界（含 ExprJSON 路径）、递归深度与 fuel 拦截、局部变量、展示目录、ExprJSON 往返、惰性分支、Artifact 防篡改和 MVP HTTP API。
