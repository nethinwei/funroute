# 性能报告

由 `make perf` 生成（`go run ./tests/perf`，与 expr 的对照来自 `tests/perf/expr`），不要手改。数字随机器变化，取的是多次运行里最快的一次；能力的边界见 [limits.md](limits.md)。

- 日期：2026-09-26
- 机器：Apple M5，darwin/arm64
- Go：go1.26.3
- 提交：`4bc3964（工作区有未提交的改动）`

## 单次执行

"原生 Go"是同一件事直接用 Go 写：宿主惯常的写法，不查溢出、不过边界；换汇调用的就是规则里用的 `Currencies.Convert`。倍数是 FunRoute 的耗时除以它。

| 场景 | FunRoute | 分配 | 原生 Go | 分配 | 倍数 |
|---|---|---|---|---|---|
| `amount * bps / 10000 + fixed`：`RunValues` | 39 ns | 0 次 | 1.87 ns | 0 次 | 21× |
| 同上：`Run(map)` | 56 ns | 0 次 | 1.87 ns | 0 次 | 30× |
| 同上：`Program.Run`（从宿主 struct 读参数） | 24 ns | 0 次 | 1.87 ns | 0 次 | 13× |
| 一次内核函数调用 `a + b` | 34 ns | 0 次 | 1.68 ns | 0 次 | 20× |
| 一次按 Go 签名注册的函数调用（常见签名，不经反射） | 60 ns | 0 次 | 1.70 ns | 0 次 | 35× |
| `using` 里换汇一次 | 193 ns | 0 次 | 55 ns | 0 次 | 3.5× |
| 500 个元素的 `reduce` | 761 ns | 0 次 | 134 ns | 0 次 | 5.7× |
| 500 个元素的推导式 | 1.0 µs | 2 次 | 480 ns | 1 次 | 2.1× |
| 500 对元素的嵌套推导式（25 × 20） | 3.3 µs | 5 次 | 486 ns | 1 次 | 6.8× |
| 把 16 个 float 交给宿主函数（与长度无关：不拷贝） | 77 ns | 0 次 | 1.75 ns | 0 次 | 44× |
| 把 1024 个 float 交给宿主函数（与长度无关：不拷贝） | 77 ns | 0 次 | 1.75 ns | 0 次 | 44× |
| 把 65536 个 float 交给宿主函数（与长度无关：不拷贝） | 77 ns | 0 次 | 1.74 ns | 0 次 | 44× |
| 模型调用（引擎每次 20 µs），一条一条 | 27.2 µs | 0 次 | 26.9 µs | 0 次 | 1.0× |
| 同上，64 条一批，折合每条 | 640 ns | 2 次 | 423 ns | 0 次 | 1.5× |

## 大输入的吞吐

参数是固定种子打乱的 0…n−1，两边用同一份。"原生 Go"是同一件事的 Go 循环，在 100 万个元素上测：筛选求和不建中间数组；排序用 `slices.Sort`，std 的 `sort` 在没有 0 与 -0 这种相等却可区分的元素时也用它。

| 程序 | n = 1 万 | n = 10 万 | n = 100 万 | 每个元素 | 原生 Go 每个元素 | 倍数 |
|---|---|---|---|---|---|---|
| `[x + 1 for x in xs]` | 11.2 µs | 113.3 µs | 1.4 ms | 1.4 ns | 0.44 ns | 3.2× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | 33.8 µs | 289.2 µs | 3.1 ms | 3.1 ns | 2.50 ns | 1.3× |
| `{string(x): x for x in xs}` | 363.6 µs | 4.4 ms | 106.6 ms | 106.6 ns | 79.90 ns | 1.3× |
| `sort(xs)` | 328.3 µs | 4.4 ms | 54.3 ms | 54.3 ns | 54.00 ns | 1.0× |
| `sum(xs)` | 10.5 µs | 70.5 µs | 639.5 µs | 0.6 ns | 0.25 ns | 2.6× |
| 1000 × 1000 的嵌套推导式 | — | — | 1.4 ms（100 万对） | 1.4 ns / 对 | 0.28 ns / 对 | 5.0× |

## 编译

`[a * 0 + b, a * 1 + b, …]`：很多重载调用共用同一对参数。

| 项数 | 源码 | 有契约 | 无契约（全部推导） |
|---|---|---|---|
| 100 | 1.2 KB | 1.1 ms | 3.0 ms |
| 500 | 6.2 KB | 6.2 ms | 37.0 ms |
| 1000 | 12.6 KB | 12.5 ms | 133.9 ms |
| 1900 | 24.9 KB | 22.9 ms | 481.9 ms |

`x0 + x1 + …`，变量互不相同，无契约：

| 项数 | 编译 |
|---|---|
| 10 | 83.8 µs |
| 100 | 867.4 µs |
| 1000 | 9.9 ms |

## 语言服务

| 请求 | 一条规则（59 字节） | 1000 项数组，无契约（13 KB） | 8000 个常量写在一行（46 KB） |
|---|---|---|---|
| 打开文档并发布诊断 | 41.9 µs | 129.7 ms | 11.7 ms |
| 悬停 | 4.2 µs | 19.8 µs | 11.5 µs |
| 补全 | 251.0 µs | 3.8 ms | 7.0 ms |
| 语义标记 | 10.1 µs | 2.0 ms | 3.5 ms |
| 格式化 | 7.4 µs | 2.1 ms | 4.6 ms |
| 语法树 | 14.2 µs | 4.1 ms | 5.2 ms |

## 产物

- 浏览器里的语言服务 `funroute.wasm`：7.85 MB（gzip 后 2.05 MB）
- 前端 JS：525 KB

## 与 expr 对照

由 `tests/perf/expr` 生成：它是单独的 module，只有它依赖 expr（v1.17.8），FunRoute 的 go.mod 仍然为空。两边从同一个宿主 struct 读参数，先编译好再反复执行，每边测 300ms。FunRoute 用 `Program.Run`（可并发调用）；expr 用复用的 `vm.VM`，这是它最快的用法，但一个 `VM` 不能并发；宿主函数在 expr 里经 `expr.Function` 注册。每一行都先经 JSON 核对两边答案相同。倍数是 expr 的耗时除以 FunRoute 的，大于 1 表示 FunRoute 更快。

### 标量：运算符、条件、绑定、数值函数、转换、宿主函数

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 20 ns | 0 次 | 29 ns | 1 次 | 1.5× |
| `a * b - a % b + 1` | `a * b - a % b + 1` | 25 ns | 0 次 | 56 ns | 1 次 | 2.2× |
| `a / b` | `int(a / b)` | 20 ns | 0 次 | 38 ns | 2 次 | 1.9× |
| `-a + b` | `-a + b` | 21 ns | 0 次 | 41 ns | 3 次 | 2.0× |
| `x * y + x / y - 1.5` | `x * y + x / y - 1.5` | 28 ns | 0 次 | 72 ns | 5 次 | 2.6× |
| `float(a) / float(b)` | `a / b` | 26 ns | 0 次 | 34 ns | 2 次 | 1.3× |
| `a > b && x < y \|\| !flag` | `a > b && x < y \|\| !flag` | 27 ns | 0 次 | 49 ns | 1 次 | 1.8× |
| `a == 7 && b != 4` | `a == 7 && b != 4` | 26 ns | 0 次 | 41 ns | 1 次 | 1.6× |
| `if(a > b, a, b)` | `a > b ? a : b` | 20 ns | 0 次 | 38 ns | 1 次 | 1.9× |
| `switch(a, case 1 => "one", case 2, 3 => "few", else => "many")` | `a == 1 ? "one" : a in [2, 3] ? "few" : "many"` | 34 ns | 0 次 | 51 ns | 1 次 | 1.5× |
| `switch(case a > 10 => "big", case a > 5 => "mid", else => "small")` | `a > 10 ? "big" : a > 5 ? "mid" : "small"` | 25 ns | 0 次 | 43 ns | 1 次 | 1.7× |
| `let(s = a + b, d = a - b, s * d)` | `let s = a + b; let d = a - b; s * d` | 23 ns | 0 次 | 56 ns | 1 次 | 2.5× |
| `7 * 24 * 3600 + a` | `7 * 24 * 3600 + a` | 21 ns | 0 次 | 29 ns | 2 次 | 1.4× |
| `abs(b - a)` | `abs(b - a)` | 24 ns | 0 次 | 38 ns | 2 次 | 1.6× |
| `max(a, b)` | `max(a, b)` | 25 ns | 0 次 | 52 ns | 2 次 | 2.0× |
| `ceil(x)` | `ceil(x)` | 26 ns | 0 次 | 27 ns | 2 次 | 1.0× |
| `floor(x) + round(y)` | `floor(x) + round(y)` | 34 ns | 0 次 | 49 ns | 4 次 | 1.5× |
| `pow(x, 2.0)` | `x ** 2` | 29 ns | 0 次 | 35 ns | 2 次 | 1.2× |
| `int(y) + a` | `int(y) + a` | 27 ns | 0 次 | 33 ns | 1 次 | 1.2× |
| `float(a) * x` | `float(a) * x` | 24 ns | 0 次 | 41 ns | 3 次 | 1.7× |
| `string(a)` | `string(a)` | 30 ns | 0 次 | 52 ns | 2 次 | 1.7× |
| `host.add_v1(a, b)` | `hostAdd(a, b)` | 41 ns | 0 次 | 44 ns | 2 次 | 1.1× |
| `host.scale_v1(x, a)` | `hostScale(x, a)` | 42 ns | 0 次 | 52 ns | 3 次 | 1.2× |

### 字符串

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `country == "MY"` | `country == "MY"` | 27 ns | 0 次 | 31 ns | 1 次 | 1.2× |
| `country in ["SG", "MY", "TH"]` | `country in ["SG", "MY", "TH"]` | 35 ns | 0 次 | 43 ns | 1 次 | 1.2× |
| `starts_with(card, "4111")` | `card startsWith "4111"` | 30 ns | 0 次 | 34 ns | 1 次 | 1.1× |
| `ends_with(s, "-sg")` | `s endsWith "-sg"` | 30 ns | 0 次 | 34 ns | 1 次 | 1.1× |
| `contains(s, "sg")` | `s contains "sg"` | 32 ns | 0 次 | 35 ns | 1 次 | 1.1× |
| `"sg" in s` | `s contains "sg"` | 33 ns | 0 次 | 36 ns | 1 次 | 1.1× |
| `upper(s)` | `upper(s)` | 56 ns | 1 次 | 63 ns | 3 次 | 1.1× |
| `lower(trim(name))` | `lower(trim(name))` | 60 ns | 1 次 | 88 ns | 5 次 | 1.5× |
| `replace(s, "-", "_")` | `replace(s, "-", "_")` | 61 ns | 1 次 | 87 ns | 4 次 | 1.4× |
| `split(csv, ",")` | `split(csv, ",")` | 113 ns | 2 次 | 99 ns | 4 次 | 0.9× |
| `join(split(csv, ","), "\|")` | `join(split(csv, ","), "\|")` | 141 ns | 3 次 | 156 ns | 6 次 | 1.1× |
| `s + ":" + country` | `s + ":" + country` | 56 ns | 2 次 | 88 ns | 5 次 | 1.6× |
| `len(s)` | `len(s)` | 34 ns | 0 次 | 33 ns | 1 次 | 1.0× |
| `slice(s, 0, 5)` | `s[0:5]` | 36 ns | 0 次 | 51 ns | 2 次 | 1.4× |
| `if(starts_with(card, "4"), "visa", "other")` | `card startsWith "4" ? "visa" : "other"` | 32 ns | 0 次 | 38 ns | 1 次 | 1.2× |
| `host.label_v1(country)` | `hostLabel(country)` | 57 ns | 1 次 | 67 ns | 4 次 | 1.2× |

### 推导式、聚合与 reduce（500 个元素）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `[x + 1 for x in xs]` | `map(xs, # + 1)` | 955 ns | 1 次 | 14.0 µs | 749 次 | 15× |
| `[x for x in xs if x % 3 == 0]` | `filter(xs, # % 3 == 0)` | 1.6 µs | 1 次 | 16.0 µs | 670 次 | 9.9× |
| `[f * 2.0 for f in fs]` | `map(fs, # * 2.0)` | 1.1 µs | 1 次 | 11.0 µs | 1002 次 | 9.9× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | `sum(filter(xs, # % 3 == 0), # * 2)` | 1.9 µs | 0 次 | 20.5 µs | 960 次 | 11× |
| `reduce(x in xs, total = 0, total + x)` | `reduce(xs, #acc + #, 0)` | 945 ns | 0 次 | 14.8 µs | 1000 次 | 16× |
| `len([x for x in xs if x % 2 == 0])` | `count(xs, # % 2 == 0)` | 1.5 µs | 0 次 | 12.5 µs | 501 次 | 8.1× |
| `any([x > 1000 for x in xs])` | `any(xs, # > 1000)` | 2.4 µs | 0 次 | 10.0 µs | 501 次 | 4.3× |
| `all([x >= 0 for x in xs])` | `all(xs, # >= 0)` | 2.4 µs | 0 次 | 10.0 µs | 501 次 | 4.2× |
| `!any([x < 0 for x in xs])` | `none(xs, # < 0)` | 2.4 µs | 0 次 | 10.8 µs | 501 次 | 4.6× |
| `len([x for x in xs if x == 250]) == 1` | `one(xs, # == 250)` | 1.0 µs | 0 次 | 10.1 µs | 501 次 | 10× |
| `first([x for x in xs if x > 400])` | `find(xs, # > 400)` | 99 ns | 0 次 | 97 ns | 4 次 | 1.0× |
| `first([x for x in xs if x == 499])` | `find(xs, # == 499)` | 1.0 µs | 0 次 | 6.5 µs | 324 次 | 6.4× |
| `[y + z for y in ys for z in zs]` | `flatten(map(ys, let y = #; map(zs, y + #)))` | 2.7 µs | 2 次 | 21.1 µs | 650 次 | 7.7× |
| `{string(x): x for x in xs}` | `fromPairs(map(xs, [string(#), #]))` | 17.1 µs | 404 次 | 82.9 µs | 3012 次 | 4.9× |
| `group_by(xs, [string(x % 3) for x in xs])` | `groupBy(xs, string(# % 3))` | 41.7 µs | 52 次 | 43.7 µs | 1530 次 | 1.0× |
| `[upper(n) for n in names]` | `map(names, upper(#))` | 3.2 µs | 101 次 | 4.3 µs | 303 次 | 1.3× |

### 数组与字典函数（500 个元素）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `sum(xs)` | `sum(xs)` | 316 ns | 1 次 | 12.4 µs | 1000 次 | 39× |
| `min(xs)` | `min(xs)` | 438 ns | 1 次 | 12.6 µs | 1002 次 | 29× |
| `max(xs)` | `max(xs)` | 435 ns | 1 次 | 13.0 µs | 1002 次 | 30× |
| `avg(xs)` | `mean(xs)` | 369 ns | 1 次 | 10.7 µs | 1003 次 | 29× |
| `median(fs)` | `median(fs)` | 4.8 µs | 2 次 | 4.9 µs | 4 次 | 1.0× |
| `sum(fs)` | `sum(fs)` | 366 ns | 1 次 | 10.0 µs | 999 次 | 27× |
| `sort(xs)` | `sort(ints)` | 4.1 µs | 3 次 | 20.0 µs | 249 次 | 4.9× |
| `sort_desc(xs)` | `sort(ints, "desc")` | 4.1 µs | 3 次 | 20.3 µs | 249 次 | 4.9× |
| `top_k(xs, xs, 5)` | `take(sort(ints, "desc"), 5)` | 806 ns | 4 次 | 20.4 µs | 250 次 | 25× |
| `reverse(xs)` | `reverse(xs)` | 808 ns | 3 次 | 5.6 µs | 504 次 | 6.9× |
| `unique(concat(xs, xs))` | `uniq(concat(xs, xs))` | 10.6 µs | 16 次 | 1.1 ms | 1025 次 | 107× |
| `take(xs, 10)` | `take(xs, 10)` | 112 ns | 2 次 | 73 ns | 3 次 | 0.7× |
| `first(xs) + last(xs)` | `first(xs) + last(xs)` | 87 ns | 1 次 | 109 ns | 4 次 | 1.3× |
| `concat(ys, zs)` | `concat(ys, zs)` | 170 ns | 4 次 | 867 ns | 55 次 | 5.1× |
| `flatten([ys, zs])` | `flatten([ys, zs])` | 575 ns | 6 次 | 1.2 µs | 60 次 | 2.0× |
| `range(len(ys))` | `0..len(ys)-1` | 133 ns | 2 次 | 90 ns | 3 次 | 0.7× |
| `len(xs)` | `len(xs)` | 41 ns | 0 次 | 43 ns | 2 次 | 1.0× |
| `xs[250]` | `xs[250]` | 44 ns | 0 次 | 56 ns | 2 次 | 1.3× |
| `250 in xs` | `250 in xs` | 132 ns | 1 次 | 2.8 µs | 252 次 | 21× |
| `index_of(xs, 250)` | `findIndex(xs, # == 250)` | 132 ns | 1 次 | 5.1 µs | 252 次 | 39× |
| `arg_max(xs)` | `reduce(xs, # > xs[#acc] ? #index : #acc, 0)` | 434 ns | 1 次 | 25.5 µs | 1002 次 | 59× |
| `intersect(xs, ys)` | `filter(xs, # in ys)` | 4.7 µs | 12 次 | 125.6 µs | 10333 次 | 27× |
| `except(ys, zs)` | `filter(ys, not (# in zs))` | 831 ns | 11 次 | 5.3 µs | 426 次 | 6.4× |
| `join(names, ",")` | `join(names, ",")` | 599 ns | 2 次 | 607 ns | 4 次 | 1.0× |
| `d["k042"]` | `d["k042"]` | 60 ns | 0 次 | 63 ns | 2 次 | 1.1× |
| `"k042" in d` | `"k042" in d` | 57 ns | 0 次 | 58 ns | 2 次 | 1.0× |
| `get(d, "zzz", 0)` | `d["zzz"] ?? 0` | 58 ns | 0 次 | 58 ns | 1 次 | 1.0× |
| `len(d)` | `len(d)` | 42 ns | 0 次 | 41 ns | 1 次 | 1.0× |
| `sort([k for k, v in d])` | `sort(keys(d))` | 5.7 µs | 5 次 | 6.8 µs | 108 次 | 1.2× |
| `sum([v for k, v in d])` | `sum(values(d))` | 4.8 µs | 1 次 | 5.6 µs | 300 次 | 1.2× |

### 记录（1 个订单、50 个渠道）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `order.amount` | `order.amount` | 24 ns | 0 次 | 27 ns | 1 次 | 1.1× |
| `order.amount * 2 - order.fee` | `order.amount * 2 - order.fee` | 30 ns | 0 次 | 52 ns | 3 次 | 1.8× |
| `{net: order.amount - order.fee, currency: order.currency}` | `{net: order.amount - order.fee, currency: order.currency}` | 76 ns | 0 次 | 128 ns | 4 次 | 1.7× |
| `order with {fee: 0}` | `{amount: order.amount, currency: order.currency, fee: 0}` | 101 ns | 0 次 | 119 ns | 3 次 | 1.2× |
| `[c.name for c in channels if c.healthy]` | `map(filter(channels, .healthy), .name)` | 1.2 µs | 1 次 | 2.8 µs | 86 次 | 2.3× |
| `sum([c.fee for c in channels])` | `sum(channels, .fee)` | 802 ns | 0 次 | 1.9 µs | 96 次 | 2.4× |
| `len([c for c in channels if c.healthy && c.fee < 50])` | `count(channels, .healthy && .fee < 50)` | 1.3 µs | 0 次 | 3.1 µs | 84 次 | 2.3× |
| `sort_by(channels, [c.fee for c in channels])` | `sortBy(channels, .fee)` | 2.3 µs | 8 次 | 3.4 µs | 105 次 | 1.4× |
| `channels[arg_min([c.fee for c in channels])].name` | `find(channels, .fee == min(map(channels, .fee))).name` | 1.0 µs | 4 次 | 1.8 µs | 56 次 | 1.7× |

### 宿主边界：宽 struct 与长向量

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 20 ns | 0 次 | 48 ns | 1 次 | 2.4× |
| `order.amount` | `order.amount` | 24 ns | 0 次 | 40 ns | 1 次 | 1.7× |
| `len(xs)` | `len(xs)` | 45 ns | 0 次 | 46 ns | 2 次 | 1.0× |
| `len(fs)` | `len(fs)` | 38 ns | 0 次 | 29 ns | 2 次 | 0.8× |
| `host.total_v1(fs)` | `hostTotal(fs)` | 39.7 µs | 1 次 | 39.7 µs | 3 次 | 1.0× |
| `sum(fs)` | `sum(fs)` | 37.6 µs | 1 次 | 1.3 ms | 130416 次 | 35× |

### 编译

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 10.5 µs | 184 次 | 5.3 µs | 85 次 | 0.5× |
| `switch(case a > 10 => "big", case a > 5 => "mid", else => "small")` | `a > 10 ? "big" : a > 5 ? "mid" : "small"` | 26.2 µs | 428 次 | 7.2 µs | 127 次 | 0.3× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | `sum(filter(xs, # % 3 == 0), # * 2)` | 35.0 µs | 577 次 | 8.5 µs | 134 次 | 0.2× |
| `[c.name for c in channels if c.healthy]` | `map(filter(channels, .healthy), .name)` | 20.7 µs | 407 次 | 8.3 µs | 149 次 | 0.4× |

### 并行：同一条规则在全部 10 个核上同时跑

`a * b - a % b + 1`，每个 goroutine 从自己的宿主 struct 读参数。数字是每次运行的时间（全部核的总耗时除以总次数），越小越好；上表 expr 用的复用 `vm.VM` 不能并发，这里是它能并发的三种用法。

| 用法 | 1 核 | 10 核 | 分配 |
|---|---|---|---|
| FunRoute `Program.Run` | 26 ns | 5.16 ns | 0 次 |
| expr：每个 goroutine 一个 `vm.VM` | 73 ns | 22 ns | 1 次 |
| expr：`sync.Pool` 管理 `vm.VM` | 80 ns | 25 ns | 1 次 |
| expr：`expr.Run`（每次新建 `VM`） | 109 ns | 48 ns | 3 次 |

写法的差别：expr 的 `/` 总是浮点除法，整数除法写成 `int(a / b)`；expr 的 `sort` 不接受 `[]int64`，排序的几行 expr 读同样内容的 `[]int`；FunRoute 的推导式在 expr 里是带谓词的 `map`/`filter`/`sum`/`count`/`any`，`switch` 是连写的 `?:`；expr 没有 `intersect`/`except`/`arg_max`/`top_k`/`with`/`index_of`，写成它能写的等价形式。

没有对照的：金额（`money`/`ratio`/`fxrate`、`round(…, @mode)`、`using` 与 `->`、`allocate`）、`fallback`、枚举与穷尽的 `switch`、句柄与模型批处理，expr 没有对应；`windows`、`chunk`、`deltas`、`cumsum`、`take_while`、`drop_while`、`stddev`、`percentile`、`rank`、`pad_left`/`pad_right`、`merge` 在 expr 里没有内置函数。
