# 性能报告

由 `make perf` 生成（`go run ./tests/perf`，与 expr 的对照来自 `tests/perf/expr`），不要手改。数字随机器变化，取的是多次运行里最快的一次；能力的边界见 [limits.md](limits.md)。

- 日期：2026-09-26
- 机器：Apple M5，darwin/arm64
- Go：go1.26.3
- 提交：`e04201d（工作区有未提交的改动）`

## 单次执行

"原生 Go"是同一件事直接用 Go 写：宿主惯常的写法，不查溢出、不过边界；换汇调用的就是规则里用的 `Currencies.Convert`。倍数是 FunRoute 的耗时除以它。

| 场景 | FunRoute | 分配 | 原生 Go | 分配 | 倍数 |
|---|---|---|---|---|---|
| `amount * bps / 10000 + fixed`：`RunValues` | 38 ns | 0 次 | 1.87 ns | 0 次 | 21× |
| 同上：`Run(map)` | 56 ns | 0 次 | 1.87 ns | 0 次 | 30× |
| 同上：`Program.Run`（从宿主 struct 读参数） | 23 ns | 0 次 | 1.87 ns | 0 次 | 12× |
| 一次内核函数调用 `a + b` | 34 ns | 0 次 | 1.72 ns | 0 次 | 20× |
| 一次按 Go 签名注册的函数调用（常见签名，不经反射） | 60 ns | 0 次 | 1.68 ns | 0 次 | 36× |
| `using` 里换汇一次 | 194 ns | 0 次 | 57 ns | 0 次 | 3.4× |
| 500 个元素的 `reduce` | 973 ns | 0 次 | 134 ns | 0 次 | 7.3× |
| 500 个元素的推导式 | 976 ns | 1 次 | 466 ns | 1 次 | 2.1× |
| 500 对元素的嵌套推导式（25 × 20） | 3.2 µs | 2 次 | 478 ns | 1 次 | 6.7× |
| 把 16 个 float 交给宿主函数（与长度无关：不拷贝） | 70 ns | 0 次 | 1.75 ns | 0 次 | 40× |
| 把 1024 个 float 交给宿主函数（与长度无关：不拷贝） | 70 ns | 0 次 | 1.75 ns | 0 次 | 40× |
| 把 65536 个 float 交给宿主函数（与长度无关：不拷贝） | 70 ns | 0 次 | 1.75 ns | 0 次 | 40× |
| 模型调用（引擎每次 20 µs），一条一条 | 27.2 µs | 0 次 | 26.7 µs | 0 次 | 1.0× |
| 同上，64 条一批，折合每条 | 653 ns | 2 次 | 422 ns | 0 次 | 1.5× |

## 大输入的吞吐

参数是固定种子打乱的 0…n−1，两边用同一份。"原生 Go"是同一件事的 Go 循环，在 100 万个元素上测：筛选求和不建中间数组；排序用 `slices.Sort`，std 的 `sort` 在没有 0 与 -0 这种相等却可区分的元素时也用它。

| 程序 | n = 1 万 | n = 10 万 | n = 100 万 | 每个元素 | 原生 Go 每个元素 | 倍数 |
|---|---|---|---|---|---|---|
| `[x + 1 for x in xs]` | 15.5 µs | 163.2 µs | 1.3 ms | 1.3 ns | 0.33 ns | 3.8× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | 38.4 µs | 342.4 µs | 3.4 ms | 3.4 ns | 2.33 ns | 1.5× |
| `{string(x): x for x in xs}` | 375.8 µs | 4.2 ms | 107.8 ms | 107.8 ns | 82.26 ns | 1.3× |
| `sort(xs)` | 284.5 µs | 4.4 ms | 54.0 ms | 54.0 ns | 53.96 ns | 1.0× |
| `sum(xs)` | 4.9 µs | 52.3 µs | 483.0 µs | 0.5 ns | 0.25 ns | 1.9× |
| 1000 × 1000 的嵌套推导式 | — | — | 1.4 ms（100 万对） | 1.4 ns / 对 | 0.38 ns / 对 | 3.6× |

## 编译

`[a * 0 + b, a * 1 + b, …]`：很多重载调用共用同一对参数。

| 项数 | 源码 | 有契约 | 无契约（全部推导） |
|---|---|---|---|
| 100 | 1.2 KB | 882.8 µs | 2.5 ms |
| 500 | 6.2 KB | 5.0 ms | 35.0 ms |
| 1000 | 12.6 KB | 9.5 ms | 131.3 ms |
| 1900 | 24.9 KB | 18.3 ms | 475.9 ms |

`x0 + x1 + …`，变量互不相同，无契约：

| 项数 | 编译 |
|---|---|
| 10 | 77.5 µs |
| 100 | 748.6 µs |
| 1000 | 8.1 ms |

## 语言服务

| 请求 | 一条规则（59 字节） | 1000 项数组，无契约（13 KB） | 8000 个常量写在一行（46 KB） |
|---|---|---|---|
| 打开文档并发布诊断 | 38.4 µs | 129.5 ms | 9.3 ms |
| 悬停 | 4.6 µs | 18.5 µs | 12.7 µs |
| 补全 | 271.1 µs | 4.1 ms | 7.1 ms |
| 语义标记 | 11.4 µs | 2.1 ms | 3.6 ms |
| 格式化 | 8.5 µs | 2.1 ms | 4.8 ms |
| 语法树 | 18.9 µs | 4.2 ms | 5.2 ms |

## 产物

- 浏览器里的语言服务 `funroute.wasm`：7.94 MB（gzip 后 2.08 MB）
- 前端 JS：525 KB

## 与 expr 对照

由 `tests/perf/expr` 生成：它是单独的 module，只有它依赖 expr（v1.17.8），FunRoute 的 go.mod 仍然为空。两边从同一个宿主 struct 读参数，先编译好再反复执行，每边测 300ms。FunRoute 用 `Program.Run`（可并发调用）；expr 用复用的 `vm.VM`，这是它最快的用法，但一个 `VM` 不能并发；宿主函数在 expr 里经 `expr.Function` 注册。每一行都先经 JSON 核对两边答案相同。倍数是 expr 的耗时除以 FunRoute 的，大于 1 表示 FunRoute 更快。

### 标量：运算符、条件、绑定、数值函数、转换、宿主函数

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 19 ns | 0 次 | 29 ns | 1 次 | 1.5× |
| `a * b - a % b + 1` | `a * b - a % b + 1` | 25 ns | 0 次 | 55 ns | 1 次 | 2.2× |
| `a / b` | `int(a / b)` | 20 ns | 0 次 | 38 ns | 2 次 | 1.9× |
| `-a + b` | `-a + b` | 20 ns | 0 次 | 41 ns | 3 次 | 2.0× |
| `x * y + x / y - 1.5` | `x * y + x / y - 1.5` | 27 ns | 0 次 | 73 ns | 5 次 | 2.7× |
| `float(a) / float(b)` | `a / b` | 26 ns | 0 次 | 34 ns | 2 次 | 1.3× |
| `a > b && x < y \|\| !flag` | `a > b && x < y \|\| !flag` | 27 ns | 0 次 | 50 ns | 1 次 | 1.8× |
| `a == 7 && b != 4` | `a == 7 && b != 4` | 26 ns | 0 次 | 40 ns | 1 次 | 1.5× |
| `if(a > b, a, b)` | `a > b ? a : b` | 20 ns | 0 次 | 37 ns | 1 次 | 1.9× |
| `switch(a, case 1 => "one", case 2, 3 => "few", else => "many")` | `a == 1 ? "one" : a in [2, 3] ? "few" : "many"` | 33 ns | 0 次 | 51 ns | 1 次 | 1.6× |
| `switch(case a > 10 => "big", case a > 5 => "mid", else => "small")` | `a > 10 ? "big" : a > 5 ? "mid" : "small"` | 25 ns | 0 次 | 42 ns | 1 次 | 1.7× |
| `let(s = a + b, d = a - b, s * d)` | `let s = a + b; let d = a - b; s * d` | 22 ns | 0 次 | 57 ns | 1 次 | 2.5× |
| `7 * 24 * 3600 + a` | `7 * 24 * 3600 + a` | 19 ns | 0 次 | 29 ns | 2 次 | 1.5× |
| `abs(b - a)` | `abs(b - a)` | 23 ns | 0 次 | 37 ns | 2 次 | 1.6× |
| `max(a, b)` | `max(a, b)` | 25 ns | 0 次 | 50 ns | 2 次 | 2.0× |
| `ceil(x)` | `ceil(x)` | 25 ns | 0 次 | 27 ns | 2 次 | 1.1× |
| `floor(x) + round(y)` | `floor(x) + round(y)` | 34 ns | 0 次 | 49 ns | 4 次 | 1.5× |
| `pow(x, 2.0)` | `x ** 2` | 30 ns | 0 次 | 35 ns | 2 次 | 1.2× |
| `int(y) + a` | `int(y) + a` | 27 ns | 0 次 | 33 ns | 1 次 | 1.2× |
| `float(a) * x` | `float(a) * x` | 23 ns | 0 次 | 40 ns | 3 次 | 1.8× |
| `string(a)` | `string(a)` | 30 ns | 0 次 | 49 ns | 2 次 | 1.6× |
| `host.add_v1(a, b)` | `hostAdd(a, b)` | 40 ns | 0 次 | 42 ns | 2 次 | 1.1× |
| `host.scale_v1(x, a)` | `hostScale(x, a)` | 40 ns | 0 次 | 50 ns | 3 次 | 1.3× |

### 字符串

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `country == "MY"` | `country == "MY"` | 24 ns | 0 次 | 30 ns | 1 次 | 1.2× |
| `country in ["SG", "MY", "TH"]` | `country in ["SG", "MY", "TH"]` | 36 ns | 0 次 | 42 ns | 1 次 | 1.2× |
| `starts_with(card, "4111")` | `card startsWith "4111"` | 29 ns | 0 次 | 32 ns | 1 次 | 1.1× |
| `ends_with(s, "-sg")` | `s endsWith "-sg"` | 39 ns | 0 次 | 36 ns | 1 次 | 0.9× |
| `contains(s, "sg")` | `s contains "sg"` | 32 ns | 0 次 | 34 ns | 1 次 | 1.1× |
| `"sg" in s` | `s contains "sg"` | 32 ns | 0 次 | 36 ns | 1 次 | 1.1× |
| `upper(s)` | `upper(s)` | 56 ns | 1 次 | 63 ns | 3 次 | 1.1× |
| `lower(trim(name))` | `lower(trim(name))` | 58 ns | 1 次 | 85 ns | 5 次 | 1.5× |
| `replace(s, "-", "_")` | `replace(s, "-", "_")` | 59 ns | 1 次 | 84 ns | 4 次 | 1.4× |
| `split(csv, ",")` | `split(csv, ",")` | 99 ns | 1 次 | 94 ns | 4 次 | 0.9× |
| `join(split(csv, ","), "\|")` | `join(split(csv, ","), "\|")` | 124 ns | 2 次 | 150 ns | 6 次 | 1.2× |
| `s + ":" + country` | `s + ":" + country` | 54 ns | 2 次 | 84 ns | 5 次 | 1.6× |
| `len(s)` | `len(s)` | 30 ns | 0 次 | 31 ns | 1 次 | 1.0× |
| `slice(s, 0, 5)` | `s[0:5]` | 34 ns | 0 次 | 49 ns | 2 次 | 1.4× |
| `if(starts_with(card, "4"), "visa", "other")` | `card startsWith "4" ? "visa" : "other"` | 31 ns | 0 次 | 37 ns | 1 次 | 1.2× |
| `host.label_v1(country)` | `hostLabel(country)` | 55 ns | 1 次 | 65 ns | 4 次 | 1.2× |

### 推导式、聚合与 reduce（500 个元素）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `[x + 1 for x in xs]` | `map(xs, # + 1)` | 997 ns | 1 次 | 13.1 µs | 749 次 | 13× |
| `[x for x in xs if x % 3 == 0]` | `filter(xs, # % 3 == 0)` | 1.5 µs | 1 次 | 15.2 µs | 670 次 | 9.9× |
| `[f * 2.0 for f in fs]` | `map(fs, # * 2.0)` | 1.0 µs | 1 次 | 10.3 µs | 1002 次 | 10× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | `sum(filter(xs, # % 3 == 0), # * 2)` | 1.9 µs | 0 次 | 19.9 µs | 960 次 | 11× |
| `reduce(x in xs, total = 0, total + x)` | `reduce(xs, #acc + #, 0)` | 615 ns | 0 次 | 14.2 µs | 1000 次 | 23× |
| `len([x for x in xs if x % 2 == 0])` | `count(xs, # % 2 == 0)` | 1.4 µs | 0 次 | 12.0 µs | 501 次 | 8.3× |
| `any([x > 1000 for x in xs])` | `any(xs, # > 1000)` | 2.3 µs | 0 次 | 9.6 µs | 501 次 | 4.2× |
| `all([x >= 0 for x in xs])` | `all(xs, # >= 0)` | 2.3 µs | 0 次 | 9.7 µs | 501 次 | 4.2× |
| `!any([x < 0 for x in xs])` | `none(xs, # < 0)` | 2.3 µs | 0 次 | 10.6 µs | 501 次 | 4.5× |
| `len([x for x in xs if x == 250]) == 1` | `one(xs, # == 250)` | 943 ns | 0 次 | 9.6 µs | 501 次 | 10× |
| `first([x for x in xs if x > 400])` | `find(xs, # > 400)` | 92 ns | 0 次 | 93 ns | 4 次 | 1.0× |
| `first([x for x in xs if x == 499])` | `find(xs, # == 499)` | 940 ns | 0 次 | 7.1 µs | 324 次 | 7.5× |
| `[y + z for y in ys for z in zs]` | `flatten(map(ys, let y = #; map(zs, y + #)))` | 2.6 µs | 2 次 | 20.4 µs | 650 次 | 7.8× |
| `{string(x): x for x in xs}` | `fromPairs(map(xs, [string(#), #]))` | 16.4 µs | 404 次 | 78.7 µs | 3012 次 | 4.8× |
| `group_by(xs, [string(x % 3) for x in xs])` | `groupBy(xs, string(# % 3))` | 39.9 µs | 50 次 | 42.3 µs | 1530 次 | 1.1× |
| `[upper(n) for n in names]` | `map(names, upper(#))` | 3.2 µs | 101 次 | 4.2 µs | 303 次 | 1.3× |

### 数组与字典函数（500 个元素）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `sum(xs)` | `sum(xs)` | 319 ns | 0 次 | 11.7 µs | 1000 次 | 37× |
| `min(xs)` | `min(xs)` | 447 ns | 0 次 | 13.3 µs | 1002 次 | 30× |
| `max(xs)` | `max(xs)` | 429 ns | 0 次 | 13.2 µs | 1002 次 | 31× |
| `avg(xs)` | `mean(xs)` | 354 ns | 0 次 | 10.7 µs | 1003 次 | 30× |
| `median(fs)` | `median(fs)` | 4.5 µs | 1 次 | 4.7 µs | 4 次 | 1.0× |
| `sum(fs)` | `sum(fs)` | 361 ns | 0 次 | 10.1 µs | 999 次 | 28× |
| `sort(xs)` | `sort(ints)` | 3.9 µs | 1 次 | 18.5 µs | 249 次 | 4.7× |
| `sort_desc(xs)` | `sort(ints, "desc")` | 3.9 µs | 1 次 | 21.0 µs | 249 次 | 5.4× |
| `top_k(xs, xs, 5)` | `take(sort(ints, "desc"), 5)` | 772 ns | 2 次 | 20.2 µs | 250 次 | 26× |
| `reverse(xs)` | `reverse(xs)` | 771 ns | 1 次 | 5.6 µs | 504 次 | 7.3× |
| `unique(concat(xs, xs))` | `uniq(concat(xs, xs))` | 10.3 µs | 13 次 | 1.1 ms | 1025 次 | 106× |
| `take(xs, 10)` | `take(xs, 10)` | 75 ns | 0 次 | 72 ns | 3 次 | 1.0× |
| `first(xs) + last(xs)` | `first(xs) + last(xs)` | 64 ns | 0 次 | 108 ns | 4 次 | 1.7× |
| `concat(ys, zs)` | `concat(ys, zs)` | 140 ns | 1 次 | 855 ns | 55 次 | 6.1× |
| `flatten([ys, zs])` | `flatten([ys, zs])` | 526 ns | 3 次 | 1.2 µs | 60 次 | 2.2× |
| `range(len(ys))` | `0..len(ys)-1` | 103 ns | 1 次 | 90 ns | 3 次 | 0.9× |
| `len(xs)` | `len(xs)` | 36 ns | 0 次 | 43 ns | 2 次 | 1.2× |
| `xs[250]` | `xs[250]` | 38 ns | 0 次 | 55 ns | 2 次 | 1.4× |
| `250 in xs` | `250 in xs` | 118 ns | 0 次 | 2.8 µs | 252 次 | 24× |
| `index_of(xs, 250)` | `findIndex(xs, # == 250)` | 115 ns | 0 次 | 4.9 µs | 252 次 | 43× |
| `arg_max(xs)` | `reduce(xs, # > xs[#acc] ? #index : #acc, 0)` | 407 ns | 0 次 | 24.8 µs | 1002 次 | 61× |
| `intersect(xs, ys)` | `filter(xs, # in ys)` | 4.5 µs | 11 次 | 120.7 µs | 10333 次 | 27× |
| `except(ys, zs)` | `filter(ys, not (# in zs))` | 791 ns | 10 次 | 5.1 µs | 426 次 | 6.5× |
| `join(names, ",")` | `join(names, ",")` | 639 ns | 1 次 | 634 ns | 4 次 | 1.0× |
| `d["k042"]` | `d["k042"]` | 57 ns | 0 次 | 62 ns | 2 次 | 1.1× |
| `"k042" in d` | `"k042" in d` | 56 ns | 0 次 | 57 ns | 2 次 | 1.0× |
| `get(d, "zzz", 0)` | `d["zzz"] ?? 0` | 52 ns | 0 次 | 55 ns | 1 次 | 1.1× |
| `len(d)` | `len(d)` | 36 ns | 0 次 | 38 ns | 1 次 | 1.1× |
| `sort([k for k, v in d])` | `sort(keys(d))` | 5.4 µs | 3 次 | 9.4 µs | 108 次 | 1.7× |
| `sum([v for k, v in d])` | `sum(values(d))` | 4.6 µs | 1 次 | 5.4 µs | 299 次 | 1.2× |

### 记录（1 个订单、50 个渠道）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `order.amount` | `order.amount` | 22 ns | 0 次 | 26 ns | 1 次 | 1.1× |
| `order.amount * 2 - order.fee` | `order.amount * 2 - order.fee` | 26 ns | 0 次 | 50 ns | 3 次 | 1.9× |
| `{net: order.amount - order.fee, currency: order.currency}` | `{net: order.amount - order.fee, currency: order.currency}` | 74 ns | 0 次 | 124 ns | 4 次 | 1.7× |
| `order with {fee: 0}` | `{amount: order.amount, currency: order.currency, fee: 0}` | 98 ns | 0 次 | 119 ns | 3 次 | 1.2× |
| `[c.name for c in channels if c.healthy]` | `map(filter(channels, .healthy), .name)` | 1.2 µs | 1 次 | 2.7 µs | 86 次 | 2.1× |
| `sum([c.fee for c in channels])` | `sum(channels, .fee)` | 779 ns | 0 次 | 1.8 µs | 96 次 | 2.3× |
| `len([c for c in channels if c.healthy && c.fee < 50])` | `count(channels, .healthy && .fee < 50)` | 1.3 µs | 0 次 | 2.9 µs | 84 次 | 2.3× |
| `sort_by(channels, [c.fee for c in channels])` | `sortBy(channels, .fee)` | 2.2 µs | 7 次 | 3.3 µs | 105 次 | 1.5× |
| `channels[arg_min([c.fee for c in channels])].name` | `find(channels, .fee == min(map(channels, .fee))).name` | 997 ns | 3 次 | 1.7 µs | 56 次 | 1.7× |

### 宿主边界：宽 struct 与长向量

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 20 ns | 0 次 | 46 ns | 1 次 | 2.3× |
| `order.amount` | `order.amount` | 22 ns | 0 次 | 38 ns | 1 次 | 1.7× |
| `len(xs)` | `len(xs)` | 39 ns | 0 次 | 46 ns | 2 次 | 1.2× |
| `len(fs)` | `len(fs)` | 32 ns | 0 次 | 28 ns | 2 次 | 0.9× |
| `host.total_v1(fs)` | `hostTotal(fs)` | 36.0 µs | 0 次 | 37.7 µs | 3 次 | 1.0× |
| `sum(fs)` | `sum(fs)` | 35.7 µs | 0 次 | 1.2 ms | 130416 次 | 34× |

### 编译

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 7.8 µs | 156 次 | 5.1 µs | 85 次 | 0.6× |
| `switch(case a > 10 => "big", case a > 5 => "mid", else => "small")` | `a > 10 ? "big" : a > 5 ? "mid" : "small"` | 19.5 µs | 354 次 | 6.8 µs | 127 次 | 0.3× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | `sum(filter(xs, # % 3 == 0), # * 2)` | 27.7 µs | 512 次 | 8.0 µs | 134 次 | 0.3× |
| `[c.name for c in channels if c.healthy]` | `map(filter(channels, .healthy), .name)` | 16.7 µs | 369 次 | 7.8 µs | 149 次 | 0.5× |

### 并行：同一条规则在全部 10 个核上同时跑

`a * b - a % b + 1`，每个 goroutine 从自己的宿主 struct 读参数。数字是每次运行的时间（全部核的总耗时除以总次数），越小越好；上表 expr 用的复用 `vm.VM` 不能并发，这里是它能并发的三种用法。

| 用法 | 1 核 | 10 核 | 分配 |
|---|---|---|---|
| FunRoute `Program.Run` | 24 ns | 4.56 ns | 0 次 |
| expr：每个 goroutine 一个 `vm.VM` | 70 ns | 21 ns | 1 次 |
| expr：`sync.Pool` 管理 `vm.VM` | 77 ns | 24 ns | 1 次 |
| expr：`expr.Run`（每次新建 `VM`） | 105 ns | 48 ns | 3 次 |

写法的差别：expr 的 `/` 总是浮点除法，整数除法写成 `int(a / b)`；expr 的 `sort` 不接受 `[]int64`，排序的几行 expr 读同样内容的 `[]int`；FunRoute 的推导式在 expr 里是带谓词的 `map`/`filter`/`sum`/`count`/`any`，`switch` 是连写的 `?:`；expr 没有 `intersect`/`except`/`arg_max`/`top_k`/`with`/`index_of`，写成它能写的等价形式。

没有对照的：金额（`money`/`ratio`/`fxrate`、`round(…, @mode)`、`using` 与 `->`、`allocate`）、`fallback`、枚举与穷尽的 `switch`、句柄与模型批处理，expr 没有对应；`windows`、`chunk`、`deltas`、`cumsum`、`take_while`、`drop_while`、`stddev`、`percentile`、`rank`、`pad_left`/`pad_right`、`merge` 在 expr 里没有内置函数。
