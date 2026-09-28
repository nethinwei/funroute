# 命令行、ExprJSON、语言服务与工作台

命令行、规则的规范 JSON 形式、LSP 语言服务、签名清单与浏览器里的策略工作台。

## 命令行

```bash
go run ./cmd/funroute inspect -expr 'if(a, b, add(1, 1))'
# (a: bool, b: int) -> int
go run ./cmd/funroute run -expr 'if(a, b, add(1, 1))' -args '{"a": false, "b": 9}'
# {"digest": "sha256:…", "type": "int", "value": 2}
go run ./cmd/funroute fmt -expr 'let(a=1,a+2)'
# let(a = 1, a + 2)
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
| `lsp` | 语言服务，见下文[语言服务](#语言服务) |

常用选项：

- `-types 'a=int,b=float'` 声明参数与顺序，`-alias 'Order=record{…}'` 声明类型别名，见[契约](contracts.md#契约)。
- `-currencies iso`（缺省，ISO 4217）或 `none` 决定是否声明金额。汇率和别的参数一样从 `-args` 传入，例如 `-types 'rates=array<fxrate>'`。
- `lsp` 不带 `-manifest` 时按 ISO 4217 声明金额；带清单时只用清单里的声明。

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
