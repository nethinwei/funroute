# 性能报告

由 `make perf` 生成（`go run ./tests/perf`，与 expr 的对照来自 `tests/perf/expr`），不要手改。数字随机器变化，取的是多次运行里最快的一次；能力的边界见 [limits.md](limits.md)。

- 日期：2026-09-28
- 机器：Apple M5，darwin/arm64
- Go：go1.26.3
- 提交：`ecfe05f（工作区有未提交的改动）`

## 单次执行

"原生 Go"是同一件事直接用 Go 写：宿主惯常的写法，不查溢出、不过边界；换汇调用的就是规则里用的 `Currencies.Convert`。耗时比是 FunRoute 的耗时除以它的：大于 1 表示 FunRoute 更慢，小于 1 表示更快，全文同一口径。

| 场景 | FunRoute | 分配 | 原生 Go | 分配 | 耗时比 |
|---|---|---|---|---|---|
| `amount * bps / 10000 + fixed`：`RunValues` | 42 ns | 0 次 | 1.80 ns | 0 次 | 23× |
| 同上：`Run(map)` | 60 ns | 0 次 | 1.80 ns | 0 次 | 33× |
| 同上：`Program.Run`（从宿主 struct 读参数） | 21 ns | 0 次 | 1.80 ns | 0 次 | 12× |
| 一次内核函数调用 `a + b` | 38 ns | 0 次 | 1.70 ns | 0 次 | 22× |
| 一次按 Go 签名注册的函数调用（常见签名，不经反射） | 53 ns | 0 次 | 1.73 ns | 0 次 | 31× |
| `using` 里换汇一次 | 206 ns | 0 次 | 56 ns | 0 次 | 3.7× |
| 500 个元素的 `reduce` | 958 ns | 0 次 | 134 ns | 0 次 | 7.2× |
| 500 个元素的推导式 | 913 ns | 1 次 | 481 ns | 1 次 | 1.9× |
| 500 对元素的嵌套推导式（25 × 20） | 3.0 µs | 2 次 | 487 ns | 1 次 | 6.1× |
| 把 16 个 float 交给宿主函数（与长度无关：不拷贝） | 71 ns | 0 次 | 1.74 ns | 0 次 | 41× |
| 把 1024 个 float 交给宿主函数（与长度无关：不拷贝） | 71 ns | 0 次 | 1.75 ns | 0 次 | 41× |
| 把 65536 个 float 交给宿主函数（与长度无关：不拷贝） | 70 ns | 0 次 | 1.74 ns | 0 次 | 40× |
| 模型调用（引擎每次 20 µs），一条一条 | 27.2 µs | 0 次 | 26.9 µs | 0 次 | 1.0× |
| 同上，64 条一批，折合每条 | 622 ns | 2 次 | 419 ns | 0 次 | 1.5× |

## 大输入的吞吐

参数是固定种子打乱的 0…n−1，两边用同一份。"原生 Go"是同一件事的 Go 循环，在 100 万个元素上测：筛选求和不建中间数组；排序用 `slices.Sort`，std 的 `sort` 在没有 0 与 -0 这种相等却可区分的元素时也用它。

| 程序 | n = 1 万 | n = 10 万 | n = 100 万 | 每个元素 | 原生 Go 每个元素 | 耗时比 |
|---|---|---|---|---|---|---|
| `[x + 1 for x in xs]` | 13.1 µs | 142.1 µs | 1.0 ms | 1.0 ns | 0.34 ns | 3.1× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | 34.0 µs | 355.1 µs | 3.2 ms | 3.2 ns | 2.45 ns | 1.3× |
| `{string(x): x for x in xs}` | 382.7 µs | 4.6 ms | 108.2 ms | 108.2 ns | 81.26 ns | 1.3× |
| `sort(xs)` | 257.6 µs | 4.4 ms | 54.1 ms | 54.1 ns | 53.32 ns | 1.0× |
| `sum(xs)` | 3.2 µs | 30.0 µs | 315.8 µs | 0.3 ns | 0.25 ns | 1.3× |
| 1000 × 1000 的嵌套推导式 | — | — | 1.3 ms（100 万对） | 1.3 ns / 对 | 0.29 ns / 对 | 4.5× |

## 编译

`[a * 0 + b, a * 1 + b, …]`：很多重载调用共用同一对参数。

| 项数 | 源码 | 有契约 | 无契约（全部推导） |
|---|---|---|---|
| 100 | 1.2 KB | 551.2 µs | 1.4 ms |
| 500 | 6.2 KB | 2.6 ms | 13.0 ms |
| 1000 | 12.6 KB | 5.5 ms | 41.6 ms |
| 1900 | 24.9 KB | 10.2 ms | 129.5 ms |

`x0 + x1 + …`，变量互不相同，无契约：

| 项数 | 编译 |
|---|---|
| 10 | 60.8 µs |
| 100 | 610.8 µs |
| 1000 | 8.5 ms |

一条规则，有契约（深的两行守着编译与格式化随规模线性增长）：

| 规则 | 编译 | 格式化 |
|---|---|---|
| `a + b` | 7.8 µs | 794 ns |
| `switch(case a > 10 => "big", …)` | 16.3 µs | 3.3 µs |
| `sum([x * 2 for x in xs if x % 3 == 0])` | 20.0 µs | 3.6 µs |
| `let(k = 3, t = k * 4, [x * t + a for x in xs if x > k])` | 27.7 µs | 5.1 µs |
| 循环体里 800 项的加法链 | 2.0 ms | 1.2 ms |
| 400 层嵌套的调用 | 946.7 µs | 1.8 ms |

## 语言服务

| 请求 | 一条规则（59 字节） | 1000 项数组，无契约（13 KB） | 8000 个常量写在一行（46 KB） |
|---|---|---|---|
| 打开文档并发布诊断 | 28.3 µs | 40.8 ms | 91.5 ms |
| 悬停 | 3.9 µs | 20.4 µs | 17.0 µs |
| 补全 | 319.8 µs | 4.2 ms | 6.5 ms |
| 语义标记 | 9.1 µs | 1.6 ms | 2.5 ms |
| 格式化 | 6.5 µs | 2.1 ms | 3.7 ms |
| 语法树 | 15.2 µs | 3.8 ms | 3.7 ms |

## 产物

- 浏览器里的语言服务 `funroute.wasm`：9.28 MB（gzip 后 2.43 MB）
- 前端 JS：525 KB

## 与 expr 对照

由 `tests/perf/expr` 生成：它是单独的 module，只有它依赖 expr（v1.17.8），FunRoute 的 go.mod 仍然为空。两边从同一个宿主 struct 读参数，先编译好再反复执行，每边测 300ms。FunRoute 用 `Program.Run`（可并发调用）；expr 用复用的 `vm.VM`，这是它最快的用法，但一个 `VM` 不能并发；宿主函数在 expr 里经 `expr.Function` 注册。每一行都先经 JSON 核对两边答案相同。耗时比是 FunRoute 的耗时除以 expr 的：大于 1 表示 FunRoute 更慢，小于 1 表示更快，与上文对原生 Go 的同一口径。

### 标量：运算符、条件、绑定、数值函数、转换、宿主函数

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 耗时比 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 19 ns | 0 次 | 30 ns | 1 次 | 0.65× |
| `a * b - a % b + 1` | `a * b - a % b + 1` | 21 ns | 0 次 | 56 ns | 1 次 | 0.39× |
| `a / b` | `int(a / b)` | 19 ns | 0 次 | 38 ns | 2 次 | 0.51× |
| `-a + b` | `-a + b` | 18 ns | 0 次 | 42 ns | 3 次 | 0.44× |
| `x * y + x / y - 1.5` | `x * y + x / y - 1.5` | 27 ns | 0 次 | 72 ns | 5 次 | 0.38× |
| `float(a) / float(b)` | `a / b` | 23 ns | 0 次 | 35 ns | 2 次 | 0.66× |
| `a > b && x < y \|\| !flag` | `a > b && x < y \|\| !flag` | 24 ns | 0 次 | 49 ns | 1 次 | 0.49× |
| `a == 7 && b != 4` | `a == 7 && b != 4` | 24 ns | 0 次 | 41 ns | 1 次 | 0.59× |
| `if(a > b, a, b)` | `a > b ? a : b` | 19 ns | 0 次 | 38 ns | 1 次 | 0.49× |
| `switch(a, case 1 => "one", case 2, 3 => "few", else => "many")` | `a == 1 ? "one" : a in [2, 3] ? "few" : "many"` | 30 ns | 0 次 | 52 ns | 1 次 | 0.57× |
| `switch(case a > 10 => "big", case a > 5 => "mid", else => "small")` | `a > 10 ? "big" : a > 5 ? "mid" : "small"` | 26 ns | 0 次 | 43 ns | 1 次 | 0.6× |
| `let(s = a + b, d = a - b, s * d)` | `let s = a + b; let d = a - b; s * d` | 19 ns | 0 次 | 56 ns | 1 次 | 0.34× |
| `7 * 24 * 3600 + a` | `7 * 24 * 3600 + a` | 18 ns | 0 次 | 29 ns | 2 次 | 0.62× |
| `abs(b - a)` | `abs(b - a)` | 22 ns | 0 次 | 37 ns | 2 次 | 0.59× |
| `max(a, b)` | `max(a, b)` | 22 ns | 0 次 | 52 ns | 2 次 | 0.43× |
| `ceil(x)` | `ceil(x)` | 26 ns | 0 次 | 27 ns | 2 次 | 0.94× |
| `floor(x) + round(y)` | `floor(x) + round(y)` | 33 ns | 0 次 | 49 ns | 4 次 | 0.68× |
| `pow(x, 2.0)` | `x ** 2` | 30 ns | 0 次 | 35 ns | 2 次 | 0.87× |
| `int(y) + a` | `int(y) + a` | 20 ns | 0 次 | 33 ns | 1 次 | 0.59× |
| `float(a) * x` | `float(a) * x` | 21 ns | 0 次 | 42 ns | 3 次 | 0.49× |
| `string(a)` | `string(a)` | 34 ns | 0 次 | 52 ns | 2 次 | 0.65× |
| `host.add_v1(a, b)` | `hostAdd(a, b)` | 30 ns | 0 次 | 47 ns | 2 次 | 0.64× |
| `host.scale_v1(x, a)` | `hostScale(x, a)` | 31 ns | 0 次 | 52 ns | 3 次 | 0.61× |

### 字符串

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 耗时比 |
|---|---|---|---|---|---|---|
| `country == "MY"` | `country == "MY"` | 25 ns | 0 次 | 31 ns | 1 次 | 0.79× |
| `country in ["SG", "MY", "TH"]` | `country in ["SG", "MY", "TH"]` | 37 ns | 0 次 | 43 ns | 1 次 | 0.86× |
| `starts_with(card, "4111")` | `card startsWith "4111"` | 30 ns | 0 次 | 34 ns | 1 次 | 0.87× |
| `ends_with(s, "-sg")` | `s endsWith "-sg"` | 29 ns | 0 次 | 34 ns | 1 次 | 0.85× |
| `contains(s, "sg")` | `s contains "sg"` | 32 ns | 0 次 | 36 ns | 1 次 | 0.87× |
| `"sg" in s` | `s contains "sg"` | 33 ns | 0 次 | 36 ns | 1 次 | 0.92× |
| `upper(s)` | `upper(s)` | 55 ns | 1 次 | 64 ns | 3 次 | 0.87× |
| `lower(trim(name))` | `lower(trim(name))` | 59 ns | 1 次 | 90 ns | 5 次 | 0.66× |
| `replace(s, "-", "_")` | `replace(s, "-", "_")` | 62 ns | 1 次 | 87 ns | 4 次 | 0.71× |
| `split(csv, ",")` | `split(csv, ",")` | 116 ns | 1 次 | 109 ns | 4 次 | 1.1× |
| `join(split(csv, ","), "\|")` | `join(split(csv, ","), "\|")` | 141 ns | 2 次 | 160 ns | 6 次 | 0.88× |
| `s + ":" + country` | `s + ":" + country` | 57 ns | 2 次 | 90 ns | 5 次 | 0.64× |
| `len(s)` | `len(s)` | 35 ns | 0 次 | 32 ns | 1 次 | 1.1× |
| `slice(s, 0, 5)` | `s[0:5]` | 34 ns | 0 次 | 51 ns | 2 次 | 0.67× |
| `if(starts_with(card, "4"), "visa", "other")` | `card startsWith "4" ? "visa" : "other"` | 34 ns | 0 次 | 38 ns | 1 次 | 0.88× |
| `host.label_v1(country)` | `hostLabel(country)` | 105 ns | 1 次 | 78 ns | 4 次 | 1.3× |

### 推导式、聚合与 reduce（500 个元素）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 耗时比 |
|---|---|---|---|---|---|---|
| `[x + 1 for x in xs]` | `map(xs, # + 1)` | 1.1 µs | 1 次 | 17.0 µs | 749 次 | 0.067× |
| `[x for x in xs if x % 3 == 0]` | `filter(xs, # % 3 == 0)` | 1.8 µs | 1 次 | 15.8 µs | 670 次 | 0.11× |
| `[f * 2.0 for f in fs]` | `map(fs, # * 2.0)` | 1.2 µs | 1 次 | 11.5 µs | 1002 次 | 0.1× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | `sum(filter(xs, # % 3 == 0), # * 2)` | 1.7 µs | 0 次 | 20.4 µs | 960 次 | 0.083× |
| `reduce(x in xs, total = 0, total + x)` | `reduce(xs, #acc + #, 0)` | 794 ns | 0 次 | 14.4 µs | 1000 次 | 0.055× |
| `len([x for x in xs if x % 2 == 0])` | `count(xs, # % 2 == 0)` | 1.5 µs | 0 次 | 12.5 µs | 501 次 | 0.12× |
| `any([x > 1000 for x in xs])` | `any(xs, # > 1000)` | 2.3 µs | 0 次 | 10.1 µs | 501 次 | 0.23× |
| `all([x >= 0 for x in xs])` | `all(xs, # >= 0)` | 2.3 µs | 0 次 | 10.2 µs | 501 次 | 0.23× |
| `!any([x < 0 for x in xs])` | `none(xs, # < 0)` | 2.3 µs | 0 次 | 10.9 µs | 501 次 | 0.21× |
| `len([x for x in xs if x == 250]) == 1` | `one(xs, # == 250)` | 976 ns | 0 次 | 10.2 µs | 501 次 | 0.096× |
| `first([x for x in xs if x > 400])` | `find(xs, # > 400)` | 99 ns | 0 次 | 185 ns | 4 次 | 0.53× |
| `first([x for x in xs if x == 499])` | `find(xs, # == 499)` | 977 ns | 0 次 | 6.6 µs | 324 次 | 0.15× |
| `[y + z for y in ys for z in zs]` | `flatten(map(ys, let y = #; map(zs, y + #)))` | 3.0 µs | 2 次 | 22.0 µs | 650 次 | 0.14× |
| `{string(x): x for x in xs}` | `fromPairs(map(xs, [string(#), #]))` | 18.3 µs | 404 次 | 85.1 µs | 3012 次 | 0.22× |
| `group_by(xs, [string(x % 3) for x in xs])` | `groupBy(xs, string(# % 3))` | 43.1 µs | 50 次 | 43.9 µs | 1530 次 | 0.98× |
| `[upper(n) for n in names]` | `map(names, upper(#))` | 3.3 µs | 101 次 | 4.4 µs | 303 次 | 0.76× |

### 数组与字典函数（500 个元素）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 耗时比 |
|---|---|---|---|---|---|---|
| `sum(xs)` | `sum(xs)` | 209 ns | 0 次 | 12.3 µs | 1000 次 | 0.017× |
| `min(xs)` | `min(xs)` | 308 ns | 0 次 | 13.0 µs | 1002 次 | 0.024× |
| `max(xs)` | `max(xs)` | 423 ns | 0 次 | 12.9 µs | 1002 次 | 0.033× |
| `avg(xs)` | `mean(xs)` | 373 ns | 0 次 | 11.2 µs | 1003 次 | 0.033× |
| `median(fs)` | `median(fs)` | 4.8 µs | 1 次 | 4.9 µs | 4 次 | 0.98× |
| `sum(fs)` | `sum(fs)` | 366 ns | 0 次 | 11.5 µs | 999 次 | 0.032× |
| `sort(xs)` | `sort(ints)` | 5.0 µs | 1 次 | 20.0 µs | 249 次 | 0.25× |
| `sort_desc(xs)` | `sort(ints, "desc")` | 4.7 µs | 1 次 | 21.0 µs | 249 次 | 0.22× |
| `top_k(xs, xs, 5)` | `take(sort(ints, "desc"), 5)` | 791 ns | 2 次 | 20.9 µs | 250 次 | 0.038× |
| `reverse(xs)` | `reverse(xs)` | 816 ns | 1 次 | 5.6 µs | 504 次 | 0.15× |
| `unique(concat(xs, xs))` | `uniq(concat(xs, xs))` | 11.6 µs | 13 次 | 1.1 ms | 1025 次 | 0.01× |
| `take(xs, 10)` | `take(xs, 10)` | 77 ns | 0 次 | 75 ns | 3 次 | 1.0× |
| `first(xs) + last(xs)` | `first(xs) + last(xs)` | 68 ns | 0 次 | 110 ns | 4 次 | 0.62× |
| `concat(ys, zs)` | `concat(ys, zs)` | 149 ns | 1 次 | 916 ns | 55 次 | 0.16× |
| `flatten([ys, zs])` | `flatten([ys, zs])` | 554 ns | 3 次 | 1.2 µs | 60 次 | 0.48× |
| `range(len(ys))` | `0..len(ys)-1` | 108 ns | 1 次 | 89 ns | 3 次 | 1.2× |
| `len(xs)` | `len(xs)` | 39 ns | 0 次 | 44 ns | 2 次 | 0.89× |
| `xs[250]` | `xs[250]` | 44 ns | 0 次 | 58 ns | 2 次 | 0.75× |
| `250 in xs` | `250 in xs` | 122 ns | 0 次 | 2.8 µs | 252 次 | 0.043× |
| `index_of(xs, 250)` | `findIndex(xs, # == 250)` | 121 ns | 0 次 | 5.1 µs | 252 次 | 0.024× |
| `arg_max(xs)` | `reduce(xs, # > xs[#acc] ? #index : #acc, 0)` | 425 ns | 0 次 | 25.6 µs | 1002 次 | 0.017× |
| `intersect(xs, ys)` | `filter(xs, # in ys)` | 4.9 µs | 11 次 | 126.1 µs | 10333 次 | 0.039× |
| `except(ys, zs)` | `filter(ys, not (# in zs))` | 890 ns | 10 次 | 5.3 µs | 426 次 | 0.17× |
| `join(names, ",")` | `join(names, ",")` | 642 ns | 1 次 | 648 ns | 4 次 | 0.99× |
| `d["k042"]` | `d["k042"]` | 50 ns | 0 次 | 64 ns | 2 次 | 0.78× |
| `"k042" in d` | `"k042" in d` | 52 ns | 0 次 | 60 ns | 2 次 | 0.88× |
| `get(d, "zzz", 0)` | `d["zzz"] ?? 0` | 57 ns | 0 次 | 57 ns | 1 次 | 1.0× |
| `len(d)` | `len(d)` | 40 ns | 0 次 | 41 ns | 1 次 | 0.98× |
| `sort([k for k, v in d])` | `sort(keys(d))` | 6.2 µs | 3 次 | 6.9 µs | 108 次 | 0.89× |
| `sum([v for k, v in d])` | `sum(values(d))` | 4.8 µs | 1 次 | 5.6 µs | 300 次 | 0.87× |

### 记录（1 个订单、50 个渠道）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 耗时比 |
|---|---|---|---|---|---|---|
| `order.amount` | `order.amount` | 21 ns | 0 次 | 27 ns | 1 次 | 0.78× |
| `order.amount * 2 - order.fee` | `order.amount * 2 - order.fee` | 24 ns | 0 次 | 53 ns | 3 次 | 0.46× |
| `{net: order.amount - order.fee, currency: order.currency}` | `{net: order.amount - order.fee, currency: order.currency}` | 77 ns | 0 次 | 128 ns | 4 次 | 0.6× |
| `order with {fee: 0}` | `{amount: order.amount, currency: order.currency, fee: 0}` | 112 ns | 0 次 | 120 ns | 3 次 | 0.93× |
| `[c.name for c in channels if c.healthy]` | `map(filter(channels, .healthy), .name)` | 984 ns | 1 次 | 2.8 µs | 86 次 | 0.36× |
| `sum([c.fee for c in channels])` | `sum(channels, .fee)` | 528 ns | 0 次 | 1.9 µs | 96 次 | 0.28× |
| `len([c for c in channels if c.healthy && c.fee < 50])` | `count(channels, .healthy && .fee < 50)` | 906 ns | 0 次 | 3.0 µs | 84 次 | 0.3× |
| `sort_by(channels, .fee)` | `sortBy(channels, .fee)` | 1.9 µs | 7 次 | 3.4 µs | 105 次 | 0.57× |
| `min_by(channels, .fee).name` | `find(channels, .fee == min(map(channels, .fee))).name` | 692 ns | 3 次 | 1.8 µs | 56 次 | 0.39× |

### 宿主边界：宽 struct 与长向量

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 耗时比 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 21 ns | 0 次 | 48 ns | 1 次 | 0.44× |
| `order.amount` | `order.amount` | 21 ns | 0 次 | 40 ns | 1 次 | 0.53× |
| `len(xs)` | `len(xs)` | 39 ns | 0 次 | 46 ns | 2 次 | 0.86× |
| `len(fs)` | `len(fs)` | 34 ns | 0 次 | 33 ns | 2 次 | 1.0× |
| `host.total_v1(fs)` | `hostTotal(fs)` | 44.3 µs | 0 次 | 39.8 µs | 3 次 | 1.1× |
| `sum(fs)` | `sum(fs)` | 39.7 µs | 0 次 | 1.4 ms | 130416 次 | 0.029× |

### 编译

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 耗时比 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 5.5 µs | 88 次 | 5.4 µs | 85 次 | 1.0× |
| `switch(case a > 10 => "big", case a > 5 => "mid", else => "small")` | `a > 10 ? "big" : a > 5 ? "mid" : "small"` | 11.6 µs | 157 次 | 7.2 µs | 127 次 | 1.6× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | `sum(filter(xs, # % 3 == 0), # * 2)` | 17.0 µs | 262 次 | 8.5 µs | 134 次 | 2.0× |
| `[c.name for c in channels if c.healthy]` | `map(filter(channels, .healthy), .name)` | 10.9 µs | 202 次 | 8.4 µs | 149 次 | 1.3× |

### 并行：同一条规则在全部 10 个核上同时跑

`a * b - a % b + 1`，每个 goroutine 从自己的宿主 struct 读参数。数字是每次运行的时间（全部核的总耗时除以总次数），越小越好；上表 expr 用的复用 `vm.VM` 不能并发，这里是它能并发的三种用法。

| 用法 | 1 核 | 10 核 | 分配 |
|---|---|---|---|
| FunRoute `Program.Run` | 21 ns | 3.97 ns | 0 次 |
| expr：每个 goroutine 一个 `vm.VM` | 73 ns | 22 ns | 1 次 |
| expr：`sync.Pool` 管理 `vm.VM` | 81 ns | 27 ns | 1 次 |
| expr：`expr.Run`（每次新建 `VM`） | 113 ns | 49 ns | 3 次 |

写法的差别：expr 的 `/` 总是浮点除法，整数除法写成 `int(a / b)`；expr 的 `sort` 不接受 `[]int64`，排序的几行 expr 读同样内容的 `[]int`；FunRoute 的推导式在 expr 里是带谓词的 `map`/`filter`/`sum`/`count`/`any`，`switch` 是连写的 `?:`；expr 没有 `intersect`/`except`/`arg_max`/`top_k`/`with`/`index_of`，写成它能写的等价形式。

没有对照的：金额（`money`/`ratio`/`fxrate`、`round(…, @mode)`、`using` 与 `->`、`allocate`）、`fallback`、枚举与穷尽的 `switch`、句柄与模型批处理，expr 没有对应；`windows`、`chunk`、`deltas`、`cumsum`、`take_while`、`drop_while`、`stddev`、`percentile`、`rank`、`pad_left`/`pad_right`、`merge` 在 expr 里没有内置函数。
