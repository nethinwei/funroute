# 性能报告

由 `make perf` 生成（`go run ./tests/perf`，与 expr 的对照来自 `tests/perf/expr`），不要手改。数字随机器变化，取的是多次运行里最快的一次；能力的边界见 [limits.md](limits.md)。

- 日期：2026-09-26
- 机器：Apple M5，darwin/arm64
- Go：go1.26.3
- 提交：`f9246c2（工作区有未提交的改动）`

## 单次执行

"原生 Go"是同一件事直接用 Go 写：宿主惯常的写法，不查溢出、不过边界；换汇调用的就是规则里用的 `Currencies.Convert`。倍数是 FunRoute 的耗时除以它。

| 场景 | FunRoute | 分配 | 原生 Go | 分配 | 倍数 |
|---|---|---|---|---|---|
| `amount * bps / 10000 + fixed`：`RunValues` | 39 ns | 0 次 | 1.72 ns | 0 次 | 22× |
| 同上：`Run(map)` | 55 ns | 0 次 | 1.72 ns | 0 次 | 32× |
| 同上：`Program.Run`（从宿主 struct 读参数） | 22 ns | 0 次 | 1.72 ns | 0 次 | 13× |
| 一次内核函数调用 `a + b` | 33 ns | 0 次 | 1.65 ns | 0 次 | 20× |
| 一次按 Go 签名注册的函数调用（常见签名，不经反射） | 58 ns | 0 次 | 1.63 ns | 0 次 | 36× |
| `using` 里换汇一次 | 184 ns | 0 次 | 54 ns | 0 次 | 3.4× |
| 500 个元素的 `reduce` | 662 ns | 0 次 | 131 ns | 0 次 | 5.0× |
| 500 个元素的推导式 | 854 ns | 1 次 | 442 ns | 1 次 | 1.9× |
| 500 对元素的嵌套推导式（25 × 20） | 3.0 µs | 2 次 | 449 ns | 1 次 | 6.8× |
| 把 16 个 float 交给宿主函数（与长度无关：不拷贝） | 69 ns | 0 次 | 1.66 ns | 0 次 | 41× |
| 把 1024 个 float 交给宿主函数（与长度无关：不拷贝） | 68 ns | 0 次 | 1.71 ns | 0 次 | 40× |
| 把 65536 个 float 交给宿主函数（与长度无关：不拷贝） | 67 ns | 0 次 | 1.69 ns | 0 次 | 40× |
| 模型调用（引擎每次 20 µs），一条一条 | 29.1 µs | 0 次 | 29.0 µs | 0 次 | 1.0× |
| 同上，64 条一批，折合每条 | 663 ns | 2 次 | 453 ns | 0 次 | 1.5× |

## 大输入的吞吐

参数是固定种子打乱的 0…n−1，两边用同一份。"原生 Go"是同一件事的 Go 循环，在 100 万个元素上测：筛选求和不建中间数组；排序用 `slices.Sort`，std 的 `sort` 在没有 0 与 -0 这种相等却可区分的元素时也用它。

| 程序 | n = 1 万 | n = 10 万 | n = 100 万 | 每个元素 | 原生 Go 每个元素 | 倍数 |
|---|---|---|---|---|---|---|
| `[x + 1 for x in xs]` | 14.7 µs | 149.2 µs | 1.2 ms | 1.2 ns | 0.32 ns | 3.7× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | 33.0 µs | 307.5 µs | 3.0 ms | 3.0 ns | 2.27 ns | 1.3× |
| `{string(x): x for x in xs}` | 357.7 µs | 4.1 ms | 101.9 ms | 101.9 ns | 74.96 ns | 1.4× |
| `sort(xs)` | 303.8 µs | 4.4 ms | 53.0 ms | 53.0 ns | 51.24 ns | 1.0× |
| `sum(xs)` | 4.8 µs | 47.3 µs | 469.2 µs | 0.5 ns | 0.25 ns | 1.9× |
| 1000 × 1000 的嵌套推导式 | — | — | 1.1 ms（100 万对） | 1.1 ns / 对 | 0.30 ns / 对 | 3.6× |

## 编译

`[a * 0 + b, a * 1 + b, …]`：很多重载调用共用同一对参数。

| 项数 | 源码 | 有契约 | 无契约（全部推导） |
|---|---|---|---|
| 100 | 1.2 KB | 518.9 µs | 1.3 ms |
| 500 | 6.2 KB | 2.9 ms | 11.6 ms |
| 1000 | 12.6 KB | 5.7 ms | 35.2 ms |
| 1900 | 24.9 KB | 10.4 ms | 109.1 ms |

`x0 + x1 + …`，变量互不相同，无契约：

| 项数 | 编译 |
|---|---|
| 10 | 51.9 µs |
| 100 | 546.4 µs |
| 1000 | 7.5 ms |

## 语言服务

| 请求 | 一条规则（59 字节） | 1000 项数组，无契约（13 KB） | 8000 个常量写在一行（46 KB） |
|---|---|---|---|
| 打开文档并发布诊断 | 25.4 µs | 34.9 ms | 6.1 ms |
| 悬停 | 3.7 µs | 21.2 µs | 11.8 µs |
| 补全 | 235.1 µs | 2.6 ms | 4.8 ms |
| 语义标记 | 8.0 µs | 1.6 ms | 2.8 ms |
| 格式化 | 6.1 µs | 1.6 ms | 4.1 ms |
| 语法树 | 12.9 µs | 3.8 ms | 4.3 ms |

## 产物

- 浏览器里的语言服务 `funroute.wasm`：7.96 MB（gzip 后 2.08 MB）
- 前端 JS：525 KB

## 与 expr 对照

由 `tests/perf/expr` 生成：它是单独的 module，只有它依赖 expr（v1.17.8），FunRoute 的 go.mod 仍然为空。两边从同一个宿主 struct 读参数，先编译好再反复执行，每边测 300ms。FunRoute 用 `Program.Run`（可并发调用）；expr 用复用的 `vm.VM`，这是它最快的用法，但一个 `VM` 不能并发；宿主函数在 expr 里经 `expr.Function` 注册。每一行都先经 JSON 核对两边答案相同。倍数是 expr 的耗时除以 FunRoute 的，大于 1 表示 FunRoute 更快。

### 标量：运算符、条件、绑定、数值函数、转换、宿主函数

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 20 ns | 0 次 | 29 ns | 1 次 | 1.4× |
| `a * b - a % b + 1` | `a * b - a % b + 1` | 26 ns | 0 次 | 53 ns | 1 次 | 2.1× |
| `a / b` | `int(a / b)` | 19 ns | 0 次 | 38 ns | 2 次 | 2.0× |
| `-a + b` | `-a + b` | 21 ns | 0 次 | 40 ns | 3 次 | 1.9× |
| `x * y + x / y - 1.5` | `x * y + x / y - 1.5` | 27 ns | 0 次 | 70 ns | 5 次 | 2.6× |
| `float(a) / float(b)` | `a / b` | 26 ns | 0 次 | 33 ns | 2 次 | 1.3× |
| `a > b && x < y \|\| !flag` | `a > b && x < y \|\| !flag` | 27 ns | 0 次 | 50 ns | 1 次 | 1.8× |
| `a == 7 && b != 4` | `a == 7 && b != 4` | 25 ns | 0 次 | 39 ns | 1 次 | 1.5× |
| `if(a > b, a, b)` | `a > b ? a : b` | 19 ns | 0 次 | 37 ns | 1 次 | 2.0× |
| `switch(a, case 1 => "one", case 2, 3 => "few", else => "many")` | `a == 1 ? "one" : a in [2, 3] ? "few" : "many"` | 32 ns | 0 次 | 50 ns | 1 次 | 1.5× |
| `switch(case a > 10 => "big", case a > 5 => "mid", else => "small")` | `a > 10 ? "big" : a > 5 ? "mid" : "small"` | 23 ns | 0 次 | 41 ns | 1 次 | 1.7× |
| `let(s = a + b, d = a - b, s * d)` | `let s = a + b; let d = a - b; s * d` | 22 ns | 0 次 | 56 ns | 1 次 | 2.6× |
| `7 * 24 * 3600 + a` | `7 * 24 * 3600 + a` | 19 ns | 0 次 | 29 ns | 2 次 | 1.5× |
| `abs(b - a)` | `abs(b - a)` | 24 ns | 0 次 | 38 ns | 2 次 | 1.5× |
| `max(a, b)` | `max(a, b)` | 24 ns | 0 次 | 50 ns | 2 次 | 2.1× |
| `ceil(x)` | `ceil(x)` | 25 ns | 0 次 | 27 ns | 2 次 | 1.1× |
| `floor(x) + round(y)` | `floor(x) + round(y)` | 32 ns | 0 次 | 48 ns | 4 次 | 1.5× |
| `pow(x, 2.0)` | `x ** 2` | 29 ns | 0 次 | 34 ns | 2 次 | 1.2× |
| `int(y) + a` | `int(y) + a` | 26 ns | 0 次 | 32 ns | 1 次 | 1.2× |
| `float(a) * x` | `float(a) * x` | 23 ns | 0 次 | 40 ns | 3 次 | 1.7× |
| `string(a)` | `string(a)` | 30 ns | 0 次 | 49 ns | 2 次 | 1.6× |
| `host.add_v1(a, b)` | `hostAdd(a, b)` | 39 ns | 0 次 | 43 ns | 2 次 | 1.1× |
| `host.scale_v1(x, a)` | `hostScale(x, a)` | 39 ns | 0 次 | 50 ns | 3 次 | 1.3× |

### 字符串

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `country == "MY"` | `country == "MY"` | 25 ns | 0 次 | 32 ns | 1 次 | 1.3× |
| `country in ["SG", "MY", "TH"]` | `country in ["SG", "MY", "TH"]` | 37 ns | 0 次 | 43 ns | 1 次 | 1.2× |
| `starts_with(card, "4111")` | `card startsWith "4111"` | 30 ns | 0 次 | 33 ns | 1 次 | 1.1× |
| `ends_with(s, "-sg")` | `s endsWith "-sg"` | 29 ns | 0 次 | 33 ns | 1 次 | 1.1× |
| `contains(s, "sg")` | `s contains "sg"` | 31 ns | 0 次 | 34 ns | 1 次 | 1.1× |
| `"sg" in s` | `s contains "sg"` | 31 ns | 0 次 | 35 ns | 1 次 | 1.1× |
| `upper(s)` | `upper(s)` | 52 ns | 1 次 | 63 ns | 3 次 | 1.2× |
| `lower(trim(name))` | `lower(trim(name))` | 57 ns | 1 次 | 84 ns | 5 次 | 1.5× |
| `replace(s, "-", "_")` | `replace(s, "-", "_")` | 58 ns | 1 次 | 82 ns | 4 次 | 1.4× |
| `split(csv, ",")` | `split(csv, ",")` | 98 ns | 1 次 | 99 ns | 4 次 | 1.0× |
| `join(split(csv, ","), "\|")` | `join(split(csv, ","), "\|")` | 125 ns | 2 次 | 150 ns | 6 次 | 1.2× |
| `s + ":" + country` | `s + ":" + country` | 55 ns | 2 次 | 86 ns | 5 次 | 1.6× |
| `len(s)` | `len(s)` | 31 ns | 0 次 | 32 ns | 1 次 | 1.0× |
| `slice(s, 0, 5)` | `s[0:5]` | 34 ns | 0 次 | 49 ns | 2 次 | 1.4× |
| `if(starts_with(card, "4"), "visa", "other")` | `card startsWith "4" ? "visa" : "other"` | 31 ns | 0 次 | 36 ns | 1 次 | 1.2× |
| `host.label_v1(country)` | `hostLabel(country)` | 53 ns | 1 次 | 64 ns | 4 次 | 1.2× |

### 推导式、聚合与 reduce（500 个元素）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `[x + 1 for x in xs]` | `map(xs, # + 1)` | 985 ns | 1 次 | 13.2 µs | 749 次 | 13× |
| `[x for x in xs if x % 3 == 0]` | `filter(xs, # % 3 == 0)` | 1.5 µs | 1 次 | 15.4 µs | 670 次 | 10.0× |
| `[f * 2.0 for f in fs]` | `map(fs, # * 2.0)` | 1.1 µs | 1 次 | 10.5 µs | 1002 次 | 9.9× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | `sum(filter(xs, # % 3 == 0), # * 2)` | 1.8 µs | 0 次 | 19.6 µs | 960 次 | 11× |
| `reduce(x in xs, total = 0, total + x)` | `reduce(xs, #acc + #, 0)` | 951 ns | 0 次 | 13.9 µs | 1000 次 | 15× |
| `len([x for x in xs if x % 2 == 0])` | `count(xs, # % 2 == 0)` | 1.5 µs | 0 次 | 12.4 µs | 501 次 | 8.4× |
| `any([x > 1000 for x in xs])` | `any(xs, # > 1000)` | 2.3 µs | 0 次 | 9.6 µs | 501 次 | 4.3× |
| `all([x >= 0 for x in xs])` | `all(xs, # >= 0)` | 2.2 µs | 0 次 | 9.7 µs | 501 次 | 4.4× |
| `!any([x < 0 for x in xs])` | `none(xs, # < 0)` | 2.2 µs | 0 次 | 10.5 µs | 501 次 | 4.7× |
| `len([x for x in xs if x == 250]) == 1` | `one(xs, # == 250)` | 977 ns | 0 次 | 10.0 µs | 501 次 | 10× |
| `first([x for x in xs if x > 400])` | `find(xs, # > 400)` | 95 ns | 0 次 | 94 ns | 4 次 | 1.0× |
| `first([x for x in xs if x == 499])` | `find(xs, # == 499)` | 988 ns | 0 次 | 6.3 µs | 324 次 | 6.4× |
| `[y + z for y in ys for z in zs]` | `flatten(map(ys, let y = #; map(zs, y + #)))` | 2.7 µs | 2 次 | 20.8 µs | 650 次 | 7.8× |
| `{string(x): x for x in xs}` | `fromPairs(map(xs, [string(#), #]))` | 16.4 µs | 404 次 | 78.5 µs | 3012 次 | 4.8× |
| `group_by(xs, [string(x % 3) for x in xs])` | `groupBy(xs, string(# % 3))` | 41.0 µs | 50 次 | 44.0 µs | 1530 次 | 1.1× |
| `[upper(n) for n in names]` | `map(names, upper(#))` | 3.2 µs | 101 次 | 4.2 µs | 303 次 | 1.3× |

### 数组与字典函数（500 个元素）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `sum(xs)` | `sum(xs)` | 409 ns | 0 次 | 12.0 µs | 1000 次 | 29× |
| `min(xs)` | `min(xs)` | 428 ns | 0 次 | 12.2 µs | 1002 次 | 28× |
| `max(xs)` | `max(xs)` | 400 ns | 0 次 | 12.6 µs | 1002 次 | 32× |
| `avg(xs)` | `mean(xs)` | 335 ns | 0 次 | 10.5 µs | 1003 次 | 31× |
| `median(fs)` | `median(fs)` | 4.4 µs | 1 次 | 4.4 µs | 4 次 | 1.0× |
| `sum(fs)` | `sum(fs)` | 329 ns | 0 次 | 9.5 µs | 999 次 | 29× |
| `sort(xs)` | `sort(ints)` | 3.8 µs | 1 次 | 18.6 µs | 249 次 | 4.9× |
| `sort_desc(xs)` | `sort(ints, "desc")` | 3.9 µs | 1 次 | 20.0 µs | 249 次 | 5.1× |
| `top_k(xs, xs, 5)` | `take(sort(ints, "desc"), 5)` | 774 ns | 2 次 | 19.8 µs | 250 次 | 26× |
| `reverse(xs)` | `reverse(xs)` | 753 ns | 1 次 | 5.5 µs | 504 次 | 7.2× |
| `unique(concat(xs, xs))` | `uniq(concat(xs, xs))` | 10.4 µs | 13 次 | 1.1 ms | 1025 次 | 108× |
| `take(xs, 10)` | `take(xs, 10)` | 75 ns | 0 次 | 74 ns | 3 次 | 1.0× |
| `first(xs) + last(xs)` | `first(xs) + last(xs)` | 66 ns | 0 次 | 105 ns | 4 次 | 1.6× |
| `concat(ys, zs)` | `concat(ys, zs)` | 138 ns | 1 次 | 842 ns | 55 次 | 6.1× |
| `flatten([ys, zs])` | `flatten([ys, zs])` | 533 ns | 3 次 | 1.2 µs | 60 次 | 2.2× |
| `range(len(ys))` | `0..len(ys)-1` | 98 ns | 1 次 | 86 ns | 3 次 | 0.9× |
| `len(xs)` | `len(xs)` | 38 ns | 0 次 | 43 ns | 2 次 | 1.1× |
| `xs[250]` | `xs[250]` | 39 ns | 0 次 | 56 ns | 2 次 | 1.4× |
| `250 in xs` | `250 in xs` | 120 ns | 0 次 | 2.8 µs | 252 次 | 24× |
| `index_of(xs, 250)` | `findIndex(xs, # == 250)` | 115 ns | 0 次 | 4.9 µs | 252 次 | 43× |
| `arg_max(xs)` | `reduce(xs, # > xs[#acc] ? #index : #acc, 0)` | 400 ns | 0 次 | 24.5 µs | 1002 次 | 61× |
| `intersect(xs, ys)` | `filter(xs, # in ys)` | 4.5 µs | 11 次 | 121.4 µs | 10333 次 | 27× |
| `except(ys, zs)` | `filter(ys, not (# in zs))` | 769 ns | 10 次 | 5.1 µs | 426 次 | 6.6× |
| `join(names, ",")` | `join(names, ",")` | 594 ns | 1 次 | 581 ns | 4 次 | 1.0× |
| `d["k042"]` | `d["k042"]` | 55 ns | 0 次 | 61 ns | 2 次 | 1.1× |
| `"k042" in d` | `"k042" in d` | 54 ns | 0 次 | 56 ns | 2 次 | 1.0× |
| `get(d, "zzz", 0)` | `d["zzz"] ?? 0` | 54 ns | 0 次 | 57 ns | 1 次 | 1.1× |
| `len(d)` | `len(d)` | 36 ns | 0 次 | 39 ns | 1 次 | 1.1× |
| `sort([k for k, v in d])` | `sort(keys(d))` | 5.3 µs | 3 次 | 6.4 µs | 108 次 | 1.2× |
| `sum([v for k, v in d])` | `sum(values(d))` | 4.4 µs | 1 次 | 5.3 µs | 299 次 | 1.2× |

### 记录（1 个订单、50 个渠道）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `order.amount` | `order.amount` | 22 ns | 0 次 | 26 ns | 1 次 | 1.2× |
| `order.amount * 2 - order.fee` | `order.amount * 2 - order.fee` | 29 ns | 0 次 | 51 ns | 3 次 | 1.8× |
| `{net: order.amount - order.fee, currency: order.currency}` | `{net: order.amount - order.fee, currency: order.currency}` | 75 ns | 0 次 | 123 ns | 4 次 | 1.6× |
| `order with {fee: 0}` | `{amount: order.amount, currency: order.currency, fee: 0}` | 97 ns | 0 次 | 117 ns | 3 次 | 1.2× |
| `[c.name for c in channels if c.healthy]` | `map(filter(channels, .healthy), .name)` | 1.2 µs | 1 次 | 2.7 µs | 86 次 | 2.2× |
| `sum([c.fee for c in channels])` | `sum(channels, .fee)` | 782 ns | 0 次 | 1.8 µs | 96 次 | 2.4× |
| `len([c for c in channels if c.healthy && c.fee < 50])` | `count(channels, .healthy && .fee < 50)` | 1.2 µs | 0 次 | 2.9 µs | 84 次 | 2.4× |
| `sort_by(channels, [c.fee for c in channels])` | `sortBy(channels, .fee)` | 2.2 µs | 7 次 | 3.3 µs | 105 次 | 1.5× |
| `channels[arg_min([c.fee for c in channels])].name` | `find(channels, .fee == min(map(channels, .fee))).name` | 985 ns | 3 次 | 1.7 µs | 56 次 | 1.7× |

### 宿主边界：宽 struct 与长向量

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 19 ns | 0 次 | 46 ns | 1 次 | 2.4× |
| `order.amount` | `order.amount` | 23 ns | 0 次 | 40 ns | 1 次 | 1.7× |
| `len(xs)` | `len(xs)` | 37 ns | 0 次 | 46 ns | 2 次 | 1.2× |
| `len(fs)` | `len(fs)` | 36 ns | 0 次 | 30 ns | 2 次 | 0.8× |
| `host.total_v1(fs)` | `hostTotal(fs)` | 38.5 µs | 0 次 | 36.8 µs | 3 次 | 1.0× |
| `sum(fs)` | `sum(fs)` | 37.7 µs | 0 次 | 1.3 ms | 130416 次 | 33× |

### 编译

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 5.3 µs | 90 次 | 5.1 µs | 85 次 | 1.0× |
| `switch(case a > 10 => "big", case a > 5 => "mid", else => "small")` | `a > 10 ? "big" : a > 5 ? "mid" : "small"` | 11.4 µs | 161 次 | 6.8 µs | 127 次 | 0.6× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | `sum(filter(xs, # % 3 == 0), # * 2)` | 16.4 µs | 267 次 | 8.1 µs | 134 次 | 0.5× |
| `[c.name for c in channels if c.healthy]` | `map(filter(channels, .healthy), .name)` | 10.6 µs | 207 次 | 7.7 µs | 149 次 | 0.7× |

### 并行：同一条规则在全部 10 个核上同时跑

`a * b - a % b + 1`，每个 goroutine 从自己的宿主 struct 读参数。数字是每次运行的时间（全部核的总耗时除以总次数），越小越好；上表 expr 用的复用 `vm.VM` 不能并发，这里是它能并发的三种用法。

| 用法 | 1 核 | 10 核 | 分配 |
|---|---|---|---|
| FunRoute `Program.Run` | 24 ns | 4.54 ns | 0 次 |
| expr：每个 goroutine 一个 `vm.VM` | 70 ns | 21 ns | 1 次 |
| expr：`sync.Pool` 管理 `vm.VM` | 77 ns | 26 ns | 1 次 |
| expr：`expr.Run`（每次新建 `VM`） | 109 ns | 49 ns | 3 次 |

写法的差别：expr 的 `/` 总是浮点除法，整数除法写成 `int(a / b)`；expr 的 `sort` 不接受 `[]int64`，排序的几行 expr 读同样内容的 `[]int`；FunRoute 的推导式在 expr 里是带谓词的 `map`/`filter`/`sum`/`count`/`any`，`switch` 是连写的 `?:`；expr 没有 `intersect`/`except`/`arg_max`/`top_k`/`with`/`index_of`，写成它能写的等价形式。

没有对照的：金额（`money`/`ratio`/`fxrate`、`round(…, @mode)`、`using` 与 `->`、`allocate`）、`fallback`、枚举与穷尽的 `switch`、句柄与模型批处理，expr 没有对应；`windows`、`chunk`、`deltas`、`cumsum`、`take_while`、`drop_while`、`stddev`、`percentile`、`rank`、`pad_left`/`pad_right`、`merge` 在 expr 里没有内置函数。
