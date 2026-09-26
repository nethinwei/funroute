# 性能报告

由 `make perf` 生成（`go run ./tests/perf`，与 expr 的对照来自 `tests/perf/expr`），不要手改。数字随机器变化，取的是多次运行里最快的一次；能力的边界见 [limits.md](limits.md)。

- 日期：2026-09-26
- 机器：Apple M5，darwin/arm64
- Go：go1.26.3
- 提交：`b351f75（工作区有未提交的改动）`

## 单次执行

"原生 Go"是同一件事直接用 Go 写：宿主惯常的写法，不查溢出、不过边界；换汇调用的就是规则里用的 `Currencies.Convert`。倍数是 FunRoute 的耗时除以它。

| 场景 | FunRoute | 分配 | 原生 Go | 分配 | 倍数 |
|---|---|---|---|---|---|
| `amount * bps / 10000 + fixed`：`RunValues` | 37 ns | 0 次 | 2.24 ns | 0 次 | 17× |
| 同上：`Run(map)` | 56 ns | 0 次 | 2.24 ns | 0 次 | 25× |
| 同上：`Program.Run`（从宿主 struct 读参数） | 23 ns | 0 次 | 2.24 ns | 0 次 | 10× |
| 一次内核函数调用 `a + b` | 33 ns | 0 次 | 1.86 ns | 0 次 | 18× |
| 一次按 Go 签名注册的函数调用（常见签名，不经反射） | 58 ns | 0 次 | 1.74 ns | 0 次 | 34× |
| `using` 里换汇一次 | 187 ns | 0 次 | 54 ns | 0 次 | 3.5× |
| 500 个元素的 `reduce` | 740 ns | 0 次 | 127 ns | 0 次 | 5.8× |
| 500 个元素的推导式 | 879 ns | 2 次 | 457 ns | 1 次 | 1.9× |
| 500 对元素的嵌套推导式（25 × 20） | 3.2 µs | 5 次 | 482 ns | 1 次 | 6.5× |
| 把 16 个 float 交给宿主函数（与长度无关：不拷贝） | 74 ns | 0 次 | 1.81 ns | 0 次 | 41× |
| 把 1024 个 float 交给宿主函数（与长度无关：不拷贝） | 74 ns | 0 次 | 1.85 ns | 0 次 | 40× |
| 把 65536 个 float 交给宿主函数（与长度无关：不拷贝） | 74 ns | 0 次 | 1.75 ns | 0 次 | 42× |
| 模型调用（引擎每次 20 µs），一条一条 | 29.4 µs | 0 次 | 29.5 µs | 0 次 | 1.0× |
| 同上，64 条一批，折合每条 | 643 ns | 2 次 | 452 ns | 0 次 | 1.4× |

## 大输入的吞吐

参数是固定种子打乱的 0…n−1，两边用同一份。"原生 Go"是同一件事的 Go 循环，在 100 万个元素上测：筛选求和不建中间数组；排序用 `slices.Sort`，std 的 `sort` 在没有 0 与 -0 这种相等却可区分的元素时也用它。

| 程序 | n = 1 万 | n = 10 万 | n = 100 万 | 每个元素 | 原生 Go 每个元素 | 倍数 |
|---|---|---|---|---|---|---|
| `[x + 1 for x in xs]` | 14.2 µs | 149.3 µs | 1.1 ms | 1.1 ns | 0.37 ns | 2.9× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | 35.0 µs | 307.1 µs | 3.0 ms | 3.0 ns | 2.48 ns | 1.2× |
| `{string(x): x for x in xs}` | 361.5 µs | 4.2 ms | 100.8 ms | 100.8 ns | 75.74 ns | 1.3× |
| `sort(xs)` | 272.1 µs | 4.4 ms | 53.4 ms | 53.4 ns | 53.27 ns | 1.0× |
| `sum(xs)` | 4.8 µs | 70.9 µs | 641.8 µs | 0.6 ns | 0.25 ns | 2.5× |
| 1000 × 1000 的嵌套推导式 | — | — | 1.3 ms（100 万对） | 1.3 ns / 对 | 0.29 ns / 对 | 4.4× |

## 编译

`[a * 0 + b, a * 1 + b, …]`：很多重载调用共用同一对参数。

| 项数 | 源码 | 有契约 | 无契约（全部推导） |
|---|---|---|---|
| 100 | 1.2 KB | 1.2 ms | 3.1 ms |
| 500 | 6.2 KB | 6.2 ms | 37.2 ms |
| 1000 | 12.6 KB | 12.4 ms | 133.5 ms |
| 1900 | 24.9 KB | 22.9 ms | 465.4 ms |

`x0 + x1 + …`，变量互不相同，无契约：

| 项数 | 编译 |
|---|---|
| 10 | 81.1 µs |
| 100 | 822.9 µs |
| 1000 | 9.8 ms |

## 语言服务

| 请求 | 一条规则（59 字节） | 1000 项数组，无契约（13 KB） | 8000 个常量写在一行（46 KB） |
|---|---|---|---|
| 打开文档并发布诊断 | 38.0 µs | 124.4 ms | 11.7 ms |
| 悬停 | 3.7 µs | 15.4 µs | 11.5 µs |
| 补全 | 242.0 µs | 3.5 ms | 7.0 ms |
| 语义标记 | 9.7 µs | 2.0 ms | 3.6 ms |
| 格式化 | 7.2 µs | 2.0 ms | 4.7 ms |
| 语法树 | 13.9 µs | 4.1 ms | 5.0 ms |

## 产物

- 浏览器里的语言服务 `funroute.wasm`：7.88 MB（gzip 后 2.06 MB）
- 前端 JS：525 KB

## 与 expr 对照

由 `tests/perf/expr` 生成：它是单独的 module，只有它依赖 expr（v1.17.8），FunRoute 的 go.mod 仍然为空。两边从同一个宿主 struct 读参数，先编译好再反复执行，每边测 300ms。FunRoute 用 `Program.Run`（可并发调用）；expr 用复用的 `vm.VM`，这是它最快的用法，但一个 `VM` 不能并发；宿主函数在 expr 里经 `expr.Function` 注册。每一行都先经 JSON 核对两边答案相同。倍数是 expr 的耗时除以 FunRoute 的，大于 1 表示 FunRoute 更快。

### 标量：运算符、条件、绑定、数值函数、转换、宿主函数

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 20 ns | 0 次 | 28 ns | 1 次 | 1.4× |
| `a * b - a % b + 1` | `a * b - a % b + 1` | 24 ns | 0 次 | 54 ns | 1 次 | 2.2× |
| `a / b` | `int(a / b)` | 19 ns | 0 次 | 37 ns | 2 次 | 2.0× |
| `-a + b` | `-a + b` | 20 ns | 0 次 | 41 ns | 3 次 | 2.1× |
| `x * y + x / y - 1.5` | `x * y + x / y - 1.5` | 28 ns | 0 次 | 71 ns | 5 次 | 2.6× |
| `float(a) / float(b)` | `a / b` | 25 ns | 0 次 | 33 ns | 2 次 | 1.3× |
| `a > b && x < y \|\| !flag` | `a > b && x < y \|\| !flag` | 25 ns | 0 次 | 46 ns | 1 次 | 1.8× |
| `a == 7 && b != 4` | `a == 7 && b != 4` | 25 ns | 0 次 | 41 ns | 1 次 | 1.6× |
| `if(a > b, a, b)` | `a > b ? a : b` | 20 ns | 0 次 | 37 ns | 1 次 | 1.9× |
| `switch(a, case 1 => "one", case 2, 3 => "few", else => "many")` | `a == 1 ? "one" : a in [2, 3] ? "few" : "many"` | 33 ns | 0 次 | 49 ns | 1 次 | 1.5× |
| `switch(case a > 10 => "big", case a > 5 => "mid", else => "small")` | `a > 10 ? "big" : a > 5 ? "mid" : "small"` | 24 ns | 0 次 | 40 ns | 1 次 | 1.7× |
| `let(s = a + b, d = a - b, s * d)` | `let s = a + b; let d = a - b; s * d` | 21 ns | 0 次 | 54 ns | 1 次 | 2.6× |
| `7 * 24 * 3600 + a` | `7 * 24 * 3600 + a` | 18 ns | 0 次 | 28 ns | 2 次 | 1.6× |
| `abs(b - a)` | `abs(b - a)` | 23 ns | 0 次 | 36 ns | 2 次 | 1.6× |
| `max(a, b)` | `max(a, b)` | 24 ns | 0 次 | 51 ns | 2 次 | 2.2× |
| `ceil(x)` | `ceil(x)` | 24 ns | 0 次 | 27 ns | 2 次 | 1.1× |
| `floor(x) + round(y)` | `floor(x) + round(y)` | 32 ns | 0 次 | 49 ns | 4 次 | 1.5× |
| `pow(x, 2.0)` | `x ** 2` | 29 ns | 0 次 | 34 ns | 2 次 | 1.2× |
| `int(y) + a` | `int(y) + a` | 26 ns | 0 次 | 32 ns | 1 次 | 1.2× |
| `float(a) * x` | `float(a) * x` | 22 ns | 0 次 | 41 ns | 3 次 | 1.8× |
| `string(a)` | `string(a)` | 29 ns | 0 次 | 50 ns | 2 次 | 1.7× |
| `host.add_v1(a, b)` | `hostAdd(a, b)` | 39 ns | 0 次 | 42 ns | 2 次 | 1.1× |
| `host.scale_v1(x, a)` | `hostScale(x, a)` | 39 ns | 0 次 | 50 ns | 3 次 | 1.3× |

### 字符串

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `country == "MY"` | `country == "MY"` | 25 ns | 0 次 | 30 ns | 1 次 | 1.2× |
| `country in ["SG", "MY", "TH"]` | `country in ["SG", "MY", "TH"]` | 33 ns | 0 次 | 43 ns | 1 次 | 1.3× |
| `starts_with(card, "4111")` | `card startsWith "4111"` | 29 ns | 0 次 | 34 ns | 1 次 | 1.2× |
| `ends_with(s, "-sg")` | `s endsWith "-sg"` | 31 ns | 0 次 | 33 ns | 1 次 | 1.1× |
| `contains(s, "sg")` | `s contains "sg"` | 31 ns | 0 次 | 35 ns | 1 次 | 1.1× |
| `"sg" in s` | `s contains "sg"` | 30 ns | 0 次 | 34 ns | 1 次 | 1.1× |
| `upper(s)` | `upper(s)` | 55 ns | 1 次 | 61 ns | 3 次 | 1.1× |
| `lower(trim(name))` | `lower(trim(name))` | 58 ns | 1 次 | 88 ns | 5 次 | 1.5× |
| `replace(s, "-", "_")` | `replace(s, "-", "_")` | 59 ns | 1 次 | 82 ns | 4 次 | 1.4× |
| `split(csv, ",")` | `split(csv, ",")` | 109 ns | 2 次 | 96 ns | 4 次 | 0.9× |
| `join(split(csv, ","), "\|")` | `join(split(csv, ","), "\|")` | 131 ns | 3 次 | 149 ns | 6 次 | 1.1× |
| `s + ":" + country` | `s + ":" + country` | 54 ns | 2 次 | 83 ns | 5 次 | 1.5× |
| `len(s)` | `len(s)` | 29 ns | 0 次 | 31 ns | 1 次 | 1.1× |
| `slice(s, 0, 5)` | `s[0:5]` | 35 ns | 0 次 | 51 ns | 2 次 | 1.5× |
| `if(starts_with(card, "4"), "visa", "other")` | `card startsWith "4" ? "visa" : "other"` | 32 ns | 0 次 | 38 ns | 1 次 | 1.2× |
| `host.label_v1(country)` | `hostLabel(country)` | 56 ns | 1 次 | 65 ns | 4 次 | 1.2× |

### 推导式、聚合与 reduce（500 个元素）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `[x + 1 for x in xs]` | `map(xs, # + 1)` | 960 ns | 1 次 | 13.4 µs | 749 次 | 14× |
| `[x for x in xs if x % 3 == 0]` | `filter(xs, # % 3 == 0)` | 1.5 µs | 1 次 | 15.8 µs | 670 次 | 10× |
| `[f * 2.0 for f in fs]` | `map(fs, # * 2.0)` | 1.1 µs | 1 次 | 10.8 µs | 1002 次 | 10.0× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | `sum(filter(xs, # % 3 == 0), # * 2)` | 1.9 µs | 0 次 | 20.4 µs | 960 次 | 11× |
| `reduce(x in xs, total = 0, total + x)` | `reduce(xs, #acc + #, 0)` | 979 ns | 0 次 | 14.4 µs | 1000 次 | 15× |
| `len([x for x in xs if x % 2 == 0])` | `count(xs, # % 2 == 0)` | 1.5 µs | 0 次 | 12.5 µs | 501 次 | 8.2× |
| `any([x > 1000 for x in xs])` | `any(xs, # > 1000)` | 2.6 µs | 0 次 | 9.9 µs | 501 次 | 3.8× |
| `all([x >= 0 for x in xs])` | `all(xs, # >= 0)` | 2.4 µs | 0 次 | 9.8 µs | 501 次 | 4.2× |
| `!any([x < 0 for x in xs])` | `none(xs, # < 0)` | 2.2 µs | 0 次 | 12.0 µs | 501 次 | 5.3× |
| `len([x for x in xs if x == 250]) == 1` | `one(xs, # == 250)` | 994 ns | 0 次 | 10.2 µs | 501 次 | 10× |
| `first([x for x in xs if x > 400])` | `find(xs, # > 400)` | 100 ns | 0 次 | 95 ns | 4 次 | 1.0× |
| `first([x for x in xs if x == 499])` | `find(xs, # == 499)` | 942 ns | 0 次 | 6.4 µs | 324 次 | 6.8× |
| `[y + z for y in ys for z in zs]` | `flatten(map(ys, let y = #; map(zs, y + #)))` | 2.6 µs | 2 次 | 20.3 µs | 650 次 | 7.7× |
| `{string(x): x for x in xs}` | `fromPairs(map(xs, [string(#), #]))` | 16.3 µs | 404 次 | 89.0 µs | 3012 次 | 5.4× |
| `group_by(xs, [string(x % 3) for x in xs])` | `groupBy(xs, string(# % 3))` | 39.8 µs | 52 次 | 41.3 µs | 1530 次 | 1.0× |
| `[upper(n) for n in names]` | `map(names, upper(#))` | 3.1 µs | 101 次 | 4.1 µs | 303 次 | 1.3× |

### 数组与字典函数（500 个元素）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `sum(xs)` | `sum(xs)` | 343 ns | 1 次 | 11.9 µs | 1000 次 | 35× |
| `min(xs)` | `min(xs)` | 414 ns | 1 次 | 12.2 µs | 1002 次 | 29× |
| `max(xs)` | `max(xs)` | 419 ns | 1 次 | 12.1 µs | 1002 次 | 29× |
| `avg(xs)` | `mean(xs)` | 358 ns | 1 次 | 10.9 µs | 1003 次 | 31× |
| `median(fs)` | `median(fs)` | 4.8 µs | 2 次 | 4.8 µs | 4 次 | 1.0× |
| `sum(fs)` | `sum(fs)` | 364 ns | 1 次 | 9.5 µs | 999 次 | 26× |
| `sort(xs)` | `sort(ints)` | 3.9 µs | 3 次 | 18.5 µs | 249 次 | 4.8× |
| `sort_desc(xs)` | `sort(ints, "desc")` | 3.8 µs | 3 次 | 20.9 µs | 249 次 | 5.4× |
| `top_k(xs, xs, 5)` | `take(sort(ints, "desc"), 5)` | 759 ns | 4 次 | 20.4 µs | 250 次 | 27× |
| `reverse(xs)` | `reverse(xs)` | 760 ns | 3 次 | 5.6 µs | 504 次 | 7.3× |
| `unique(concat(xs, xs))` | `uniq(concat(xs, xs))` | 10.1 µs | 16 次 | 1.1 ms | 1025 次 | 106× |
| `take(xs, 10)` | `take(xs, 10)` | 110 ns | 2 次 | 74 ns | 3 次 | 0.7× |
| `first(xs) + last(xs)` | `first(xs) + last(xs)` | 82 ns | 1 次 | 109 ns | 4 次 | 1.3× |
| `concat(ys, zs)` | `concat(ys, zs)` | 171 ns | 4 次 | 857 ns | 55 次 | 5.0× |
| `flatten([ys, zs])` | `flatten([ys, zs])` | 552 ns | 6 次 | 1.1 µs | 60 次 | 2.0× |
| `range(len(ys))` | `0..len(ys)-1` | 126 ns | 2 次 | 87 ns | 3 次 | 0.7× |
| `len(xs)` | `len(xs)` | 38 ns | 0 次 | 42 ns | 2 次 | 1.1× |
| `xs[250]` | `xs[250]` | 40 ns | 0 次 | 55 ns | 2 次 | 1.4× |
| `250 in xs` | `250 in xs` | 129 ns | 1 次 | 2.8 µs | 252 次 | 22× |
| `index_of(xs, 250)` | `findIndex(xs, # == 250)` | 125 ns | 1 次 | 4.9 µs | 252 次 | 39× |
| `arg_max(xs)` | `reduce(xs, # > xs[#acc] ? #index : #acc, 0)` | 415 ns | 1 次 | 24.3 µs | 1002 次 | 59× |
| `intersect(xs, ys)` | `filter(xs, # in ys)` | 4.4 µs | 12 次 | 120.2 µs | 10333 次 | 27× |
| `except(ys, zs)` | `filter(ys, not (# in zs))` | 798 ns | 11 次 | 5.1 µs | 426 次 | 6.4× |
| `join(names, ",")` | `join(names, ",")` | 600 ns | 2 次 | 659 ns | 4 次 | 1.1× |
| `d["k042"]` | `d["k042"]` | 62 ns | 0 次 | 63 ns | 2 次 | 1.0× |
| `"k042" in d` | `"k042" in d` | 58 ns | 0 次 | 59 ns | 2 次 | 1.0× |
| `get(d, "zzz", 0)` | `d["zzz"] ?? 0` | 56 ns | 0 次 | 60 ns | 1 次 | 1.1× |
| `len(d)` | `len(d)` | 40 ns | 0 次 | 39 ns | 1 次 | 1.0× |
| `sort([k for k, v in d])` | `sort(keys(d))` | 5.5 µs | 5 次 | 6.3 µs | 108 次 | 1.1× |
| `sum([v for k, v in d])` | `sum(values(d))` | 4.6 µs | 1 次 | 5.4 µs | 299 次 | 1.2× |

### 记录（1 个订单、50 个渠道）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `order.amount` | `order.amount` | 23 ns | 0 次 | 26 ns | 1 次 | 1.1× |
| `order.amount * 2 - order.fee` | `order.amount * 2 - order.fee` | 28 ns | 0 次 | 52 ns | 3 次 | 1.9× |
| `{net: order.amount - order.fee, currency: order.currency}` | `{net: order.amount - order.fee, currency: order.currency}` | 75 ns | 0 次 | 122 ns | 4 次 | 1.6× |
| `order with {fee: 0}` | `{amount: order.amount, currency: order.currency, fee: 0}` | 186 ns | 2 次 | 115 ns | 3 次 | 0.6× |
| `[c.name for c in channels if c.healthy]` | `map(filter(channels, .healthy), .name)` | 1.2 µs | 1 次 | 2.7 µs | 86 次 | 2.3× |
| `sum([c.fee for c in channels])` | `sum(channels, .fee)` | 754 ns | 0 次 | 1.8 µs | 96 次 | 2.4× |
| `len([c for c in channels if c.healthy && c.fee < 50])` | `count(channels, .healthy && .fee < 50)` | 1.2 µs | 0 次 | 3.0 µs | 84 次 | 2.4× |
| `sort_by(channels, [c.fee for c in channels])` | `sortBy(channels, .fee)` | 6.7 µs | 11 次 | 3.2 µs | 105 次 | 0.5× |
| `channels[arg_min([c.fee for c in channels])].name` | `find(channels, .fee == min(map(channels, .fee))).name` | 3.9 µs | 6 次 | 1.7 µs | 56 次 | 0.4× |

### 宿主边界：宽 struct 与长向量

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 18 ns | 0 次 | 46 ns | 1 次 | 2.5× |
| `order.amount` | `order.amount` | 23 ns | 0 次 | 38 ns | 1 次 | 1.6× |
| `len(xs)` | `len(xs)` | 39 ns | 0 次 | 44 ns | 2 次 | 1.1× |
| `len(fs)` | `len(fs)` | 36 ns | 0 次 | 28 ns | 2 次 | 0.8× |
| `host.total_v1(fs)` | `hostTotal(fs)` | 37.9 µs | 1 次 | 38.0 µs | 3 次 | 1.0× |
| `sum(fs)` | `sum(fs)` | 37.9 µs | 1 次 | 1.3 ms | 130416 次 | 33× |

### 编译

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 10.3 µs | 182 次 | 5.2 µs | 85 次 | 0.5× |
| `switch(case a > 10 => "big", case a > 5 => "mid", else => "small")` | `a > 10 ? "big" : a > 5 ? "mid" : "small"` | 25.0 µs | 426 次 | 6.9 µs | 127 次 | 0.3× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | `sum(filter(xs, # % 3 == 0), # * 2)` | 33.2 µs | 575 次 | 8.1 µs | 134 次 | 0.2× |
| `[c.name for c in channels if c.healthy]` | `map(filter(channels, .healthy), .name)` | 20.0 µs | 404 次 | 7.8 µs | 149 次 | 0.4× |

### 并行：同一条规则在全部 10 个核上同时跑

`a * b - a % b + 1`，每个 goroutine 从自己的宿主 struct 读参数。数字是每次运行的时间（全部核的总耗时除以总次数），越小越好；上表 expr 用的复用 `vm.VM` 不能并发，这里是它能并发的三种用法。

| 用法 | 1 核 | 10 核 | 分配 |
|---|---|---|---|
| FunRoute `Program.Run` | 24 ns | 4.62 ns | 0 次 |
| expr：每个 goroutine 一个 `vm.VM` | 73 ns | 21 ns | 1 次 |
| expr：`sync.Pool` 管理 `vm.VM` | 79 ns | 25 ns | 1 次 |
| expr：`expr.Run`（每次新建 `VM`） | 105 ns | 49 ns | 3 次 |

写法的差别：expr 的 `/` 总是浮点除法，整数除法写成 `int(a / b)`；expr 的 `sort` 不接受 `[]int64`，排序的几行 expr 读同样内容的 `[]int`；FunRoute 的推导式在 expr 里是带谓词的 `map`/`filter`/`sum`/`count`/`any`，`switch` 是连写的 `?:`；expr 没有 `intersect`/`except`/`arg_max`/`top_k`/`with`/`index_of`，写成它能写的等价形式。

没有对照的：金额（`money`/`ratio`/`fxrate`、`round(…, @mode)`、`using` 与 `->`、`allocate`）、`fallback`、枚举与穷尽的 `switch`、句柄与模型批处理，expr 没有对应；`windows`、`chunk`、`deltas`、`cumsum`、`take_while`、`drop_while`、`stddev`、`percentile`、`rank`、`pad_left`/`pad_right`、`merge` 在 expr 里没有内置函数。
