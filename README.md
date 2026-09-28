# FunRoute

[![ci](https://github.com/nethinwei/funroute/actions/workflows/ci.yml/badge.svg)](https://github.com/nethinwei/funroute/actions/workflows/ci.yml)
[![在线工作台](https://img.shields.io/badge/在线工作台-打开-2ea44f)](https://nethinwei.github.io/funroute/)

FunRoute 是一门给**支付路由规则**用的小语言：强类型、纯表达式、必然终止。一条规则就是一个表达式：

```text
switch(country,
  case "SG"       => "adyen_sg",
  case "MY", "TH" => if(amount >= 100000, "adyen_asia", "local_asia"),
  else => "stripe_global")
```

## 在线试一试

打开 **[策略工作台](https://nethinwei.github.io/funroute/)**，不用安装任何东西：

1. 在顶部「从示例开始」挑一条：支付路由、费率计费、跨境汇率、结算分账、风险控制等 90 多条示例，每条都带契约和样例入参。
2. 在「表达式」里改规则，诊断、补全、悬停与签名提示即时出现；`⌘/Ctrl + Enter` 运行，旁边的试运行给出结果、类型与耗时，参数值直接改 JSON。
3. 点「结构」页签看同一段规则的结构视图，在那里改也只是替换原文的一段；右侧能看语法树和全部函数的说明。

语言服务编成 WebAssembly 在浏览器里运行，没有后端；本地 `make run` 起的是同一个页面。

## 为什么是这样一门语言

支付规则由运营编写、由平台执行，出错就是资损。每条设计都服务于"规则可审查、可预测、改不坏"：

- **只有表达式**：没有语句与可变状态，也没有隐式的网络、时钟、数据库；程序**必然终止**，最坏成本可由输入规模静态估出。
- **契约归宿主**：参数、类型与返回类型由调用方给出（或直接用 Go struct），规则文本不写声明；没给的由推导补上。
- **金额精确**：`USD 1.70` 是最小单位的整数，从不经过 float；没有默认舍入，换汇只在 `using` 里。
- **产物不漂移**：Artifact 带 digest、冻结了用到的每个函数签名；运行环境变了，旧产物拒绝装载。
- **能力由宿主开关**：一个注册表就是一个控制台，形式、金额与扩展函数按需打开。
- **执行快、边界零拷贝**：装载时翻译成寄存器形式执行，Go 切片原样交进交出；与 expr 同机对照，执行的 100 行里 92 行更快（[perf.md](docs/perf.md)）。

## 快速上手

需要 Go 1.26。

```bash
go get github.com/nethinwei/funroute
```

契约就是一个 Go struct，规则编译一次、反复运行：

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/nethinwei/funroute"
)

// Order is the rule's contract: the tagged fields are its arguments.
type Order struct {
	Country string `funroute:"country"`
	Amount  int64  `funroute:"amount"`
}

func main() {
	registry := funroute.CoreRegistry()
	if err := registry.EnableForm(funroute.SwitchForm); err != nil {
		log.Fatal(err)
	}
	binding, err := funroute.Bind[Order, string](registry)
	if err != nil {
		log.Fatal(err)
	}
	program, err := binding.Compile(`switch(country,
  case "SG"       => "adyen_sg",
  case "MY", "TH" => if(amount >= 100000, "adyen_asia", "local_asia"),
  else => "stripe_global")`)
	if err != nil {
		log.Fatal(err)
	}
	channel, err := program.Run(context.Background(), &Order{Country: "TH", Amount: 250000})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(channel) // adyen_asia
}
```

`program.Run` 可以并发调用，参数从 struct 直接读、不经 map 与反射；写错类型的规则（比如返回了 int）在 `Compile` 时就被拒绝。金额、扩展函数、模型批处理、超时与 `fallback` 见 [在 Go 中使用](docs/go.md)；更多可运行的宿主程序在 [`examples/`](examples)（`go run ./examples/routing`）。

不写 Go 也能在命令行里试：

```bash
# 推导出的签名：没写类型，a 与 b 由 if 和 add 的签名推出
go run ./cmd/funroute inspect -expr 'if(a, b, add(1, 1))'
# (a: bool, b: int) -> int

# 运行：输出结果、类型与 artifact 的 digest
go run ./cmd/funroute run -expr 'if(a, b, add(1, 1))' -args '{"a": false, "b": 9}'
# {"digest": "sha256:…", "type": "int", "value": 2}

# 格式化
go run ./cmd/funroute fmt -expr 'let(a=1,a+2)'
# let(a = 1, a + 2)
```

全部子命令与选项见 [命令行](docs/tooling.md#命令行)。

## 文档

| 文档 | 内容 |
|---|---|
| [语言导览](docs/language.md) | 值与类型、运算符、`if`/`switch`、`let`、推导式、聚合与 `reduce`、记录、时间 |
| [金额](docs/money.md) | `money`/`ratio`/`fxrate`/`currency`、`round(…, @mode)`、换汇 `->` 与 `using`、分摊 |
| [契约与类型推导](docs/contracts.md) | 契约、类型别名、枚举、类型从哪里来、编译期求值 |
| [在 Go 中使用](docs/go.md) | 编译与运行、类型化绑定、注册扩展函数、句柄与模型批处理、超时与错误、按控制台开放能力 |
| [命令行、ExprJSON、语言服务与工作台](docs/tooling.md) | CLI、规范 JSON、LSP、签名清单、策略工作台 |
| [语法与取值范围](docs/grammar.md) | 完整文法与每种类型的范围 |
| [能力边界](docs/limits.md) · [终止性](docs/termination.md) · [性能](docs/perf.md) | 实测的边界、为什么每条规则都会停、与原生 Go 和 expr 的对照 |
| [路线图与设计决策](docs/roadmap.md) · [更新记录](CHANGELOG.md) | 接下来做什么、每个取舍为什么、每个版本改了什么 |
| [开发](docs/development.md) | 构建、检查、代码结构 |

## 现状

语言、金额、SDK、语言服务与工作台都已可用（见 [更新记录](CHANGELOG.md)）。用于真实支付前还缺：决策 trace、路由包（带原因码的 `decision`、`reject(code)`、确定性的 `split`）与治理工具（规则自带用例、批量兼容性检查、回测与影子运行），计划见 [路线图](docs/roadmap.md)。

## 版本与兼容

按[语义化版本](https://semver.org/lang/zh-CN/)发布，目前是 v0.x：公开 API、语法、ExprJSON、Artifact 与签名清单的形状在两个次版本之间都可能不兼容地改变，改动写在 [CHANGELOG.md](CHANGELOG.md)。

- **Artifact 跟着版本走**：`ArtifactVersion`、`ExprJSONVersion`、`ManifestVersion` 只标识当前形状，形状变了旧的就被拒绝装载（报出版本不符），不会被猜着读。升级后用规则的源码或 ExprJSON 重新编译即可；同一版本内，digest 保证装载的就是编译出来的那一份。
- **答案不悄悄改变**：`tests/golden` 的行为金库逐字节记着随机程序的答案与失败，在 arm64 上生成、CI 在 amd64 上核对；有意改变某个答案的会写进更新记录。
- v1.0 之后才对公开 API 与 Artifact 格式做兼容承诺。

## 许可证

[MIT](LICENSE)。
