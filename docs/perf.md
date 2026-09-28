# 性能报告

由 `make perf` 生成（`go run ./tests/perf`，与 expr 的对照来自 `tests/perf/expr`），不要手改。数字随机器变化，取的是多次运行里最快的一次；能力的边界见 [limits.md](limits.md)。

- 日期：2026-09-28
- 机器：Apple M5，darwin/arm64
- Go：go1.26.3
- 提交：`88b8d3b（工作区有未提交的改动）`

## 单次执行

"原生 Go"是同一件事直接用 Go 写：宿主惯常的写法，不查溢出、不过边界；换汇调用的就是规则里用的 `Currencies.Convert`。倍数是 FunRoute 的耗时除以它。

| 场景 | FunRoute | 分配 | 原生 Go | 分配 | 倍数 |
|---|---|---|---|---|---|
| `amount * bps / 10000 + fixed`：`RunValues` | 40 ns | 0 次 | 1.78 ns | 0 次 | 22× |
| 同上：`Run(map)` | 57 ns | 0 次 | 1.78 ns | 0 次 | 32× |
| 同上：`Program.Run`（从宿主 struct 读参数） | 20 ns | 0 次 | 1.78 ns | 0 次 | 11× |
| 一次内核函数调用 `a + b` | 36 ns | 0 次 | 1.68 ns | 0 次 | 21× |
| 一次按 Go 签名注册的函数调用（常见签名，不经反射） | 51 ns | 0 次 | 1.64 ns | 0 次 | 31× |
| `using` 里换汇一次 | 196 ns | 0 次 | 55 ns | 0 次 | 3.5× |
| 500 个元素的 `reduce` | 974 ns | 0 次 | 129 ns | 0 次 | 7.5× |
| 500 个元素的推导式 | 878 ns | 1 次 | 461 ns | 1 次 | 1.9× |
| 500 对元素的嵌套推导式（25 × 20） | 3.0 µs | 2 次 | 487 ns | 1 次 | 6.2× |
| 把 16 个 float 交给宿主函数（与长度无关：不拷贝） | 70 ns | 0 次 | 1.71 ns | 0 次 | 41× |
| 把 1024 个 float 交给宿主函数（与长度无关：不拷贝） | 69 ns | 0 次 | 1.67 ns | 0 次 | 41× |
| 把 65536 个 float 交给宿主函数（与长度无关：不拷贝） | 73 ns | 0 次 | 1.68 ns | 0 次 | 43× |
| 模型调用（引擎每次 20 µs），一条一条 | 29.7 µs | 0 次 | 28.9 µs | 0 次 | 1.0× |
| 同上，64 条一批，折合每条 | 614 ns | 2 次 | 464 ns | 0 次 | 1.3× |

## 大输入的吞吐

参数是固定种子打乱的 0…n−1，两边用同一份。"原生 Go"是同一件事的 Go 循环，在 100 万个元素上测：筛选求和不建中间数组；排序用 `slices.Sort`，std 的 `sort` 在没有 0 与 -0 这种相等却可区分的元素时也用它。

| 程序 | n = 1 万 | n = 10 万 | n = 100 万 | 每个元素 | 原生 Go 每个元素 | 倍数 |
|---|---|---|---|---|---|---|
| `[x + 1 for x in xs]` | 13.9 µs | 108.8 µs | 1.1 ms | 1.1 ns | 0.32 ns | 3.4× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | 36.7 µs | 348.1 µs | 3.0 ms | 3.0 ns | 2.35 ns | 1.3× |
| `{string(x): x for x in xs}` | 385.5 µs | 4.4 ms | 100.4 ms | 100.4 ns | 76.02 ns | 1.3× |
| `sort(xs)` | 314.4 µs | 4.4 ms | 51.8 ms | 51.8 ns | 51.18 ns | 1.0× |
| `sum(xs)` | 4.9 µs | 47.1 µs | 473.0 µs | 0.5 ns | 0.25 ns | 1.9× |
| 1000 × 1000 的嵌套推导式 | — | — | 1.1 ms（100 万对） | 1.1 ns / 对 | 0.30 ns / 对 | 3.6× |

## 编译

`[a * 0 + b, a * 1 + b, …]`：很多重载调用共用同一对参数。

| 项数 | 源码 | 有契约 | 无契约（全部推导） |
|---|---|---|---|
| 100 | 1.2 KB | 489.8 µs | 1.4 ms |
| 500 | 6.2 KB | 2.8 ms | 12.9 ms |
| 1000 | 12.6 KB | 5.5 ms | 40.2 ms |
| 1900 | 24.9 KB | 10.1 ms | 124.4 ms |

`x0 + x1 + …`，变量互不相同，无契约：

| 项数 | 编译 |
|---|---|
| 10 | 58.2 µs |
| 100 | 608.1 µs |
| 1000 | 7.8 ms |

## 语言服务

| 请求 | 一条规则（59 字节） | 1000 项数组，无契约（13 KB） | 8000 个常量写在一行（46 KB） |
|---|---|---|---|
| 打开文档并发布诊断 | 27.7 µs | 39.1 ms | 86.9 ms |
| 悬停 | 4.3 µs | 16.8 µs | 15.6 µs |
| 补全 | 297.5 µs | 3.8 ms | 6.7 ms |
| 语义标记 | 8.9 µs | 1.7 ms | 2.9 ms |
| 格式化 | 6.5 µs | 1.6 ms | 4.2 ms |
| 语法树 | 14.1 µs | 3.8 ms | 4.1 ms |

## 产物

- 浏览器里的语言服务 `funroute.wasm`：9.18 MB（gzip 后 2.41 MB）
- 前端 JS：525 KB

## 与 expr 对照

由 `tests/perf/expr` 生成：它是单独的 module，只有它依赖 expr（v1.17.8），FunRoute 的 go.mod 仍然为空。两边从同一个宿主 struct 读参数，先编译好再反复执行，每边测 300ms。FunRoute 用 `Program.Run`（可并发调用）；expr 用复用的 `vm.VM`，这是它最快的用法，但一个 `VM` 不能并发；宿主函数在 expr 里经 `expr.Function` 注册。每一行都先经 JSON 核对两边答案相同。倍数是 expr 的耗时除以 FunRoute 的，大于 1 表示 FunRoute 更快。

### 标量：运算符、条件、绑定、数值函数、转换、宿主函数

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 18 ns | 0 次 | 29 ns | 1 次 | 1.6× |
| `a * b - a % b + 1` | `a * b - a % b + 1` | 21 ns | 0 次 | 54 ns | 1 次 | 2.6× |
| `a / b` | `int(a / b)` | 18 ns | 0 次 | 37 ns | 2 次 | 2.0× |
| `-a + b` | `-a + b` | 18 ns | 0 次 | 40 ns | 3 次 | 2.3× |
| `x * y + x / y - 1.5` | `x * y + x / y - 1.5` | 26 ns | 0 次 | 68 ns | 5 次 | 2.7× |
| `float(a) / float(b)` | `a / b` | 22 ns | 0 次 | 33 ns | 2 次 | 1.5× |
| `a > b && x < y \|\| !flag` | `a > b && x < y \|\| !flag` | 23 ns | 0 次 | 47 ns | 1 次 | 2.0× |
| `a == 7 && b != 4` | `a == 7 && b != 4` | 23 ns | 0 次 | 39 ns | 1 次 | 1.7× |
| `if(a > b, a, b)` | `a > b ? a : b` | 18 ns | 0 次 | 37 ns | 1 次 | 2.0× |
| `switch(a, case 1 => "one", case 2, 3 => "few", else => "many")` | `a == 1 ? "one" : a in [2, 3] ? "few" : "many"` | 29 ns | 0 次 | 49 ns | 1 次 | 1.7× |
| `switch(case a > 10 => "big", case a > 5 => "mid", else => "small")` | `a > 10 ? "big" : a > 5 ? "mid" : "small"` | 24 ns | 0 次 | 41 ns | 1 次 | 1.7× |
| `let(s = a + b, d = a - b, s * d)` | `let s = a + b; let d = a - b; s * d` | 18 ns | 0 次 | 54 ns | 1 次 | 3.0× |
| `7 * 24 * 3600 + a` | `7 * 24 * 3600 + a` | 17 ns | 0 次 | 29 ns | 2 次 | 1.7× |
| `abs(b - a)` | `abs(b - a)` | 21 ns | 0 次 | 36 ns | 2 次 | 1.7× |
| `max(a, b)` | `max(a, b)` | 21 ns | 0 次 | 50 ns | 2 次 | 2.3× |
| `ceil(x)` | `ceil(x)` | 24 ns | 0 次 | 27 ns | 2 次 | 1.1× |
| `floor(x) + round(y)` | `floor(x) + round(y)` | 33 ns | 0 次 | 49 ns | 4 次 | 1.5× |
| `pow(x, 2.0)` | `x ** 2` | 30 ns | 0 次 | 34 ns | 2 次 | 1.1× |
| `int(y) + a` | `int(y) + a` | 19 ns | 0 次 | 32 ns | 1 次 | 1.7× |
| `float(a) * x` | `float(a) * x` | 21 ns | 0 次 | 40 ns | 3 次 | 1.9× |
| `string(a)` | `string(a)` | 32 ns | 0 次 | 49 ns | 2 次 | 1.5× |
| `host.add_v1(a, b)` | `hostAdd(a, b)` | 27 ns | 0 次 | 43 ns | 2 次 | 1.6× |
| `host.scale_v1(x, a)` | `hostScale(x, a)` | 30 ns | 0 次 | 50 ns | 3 次 | 1.7× |

### 字符串

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `country == "MY"` | `country == "MY"` | 23 ns | 0 次 | 30 ns | 1 次 | 1.3× |
| `country in ["SG", "MY", "TH"]` | `country in ["SG", "MY", "TH"]` | 36 ns | 0 次 | 42 ns | 1 次 | 1.2× |
| `starts_with(card, "4111")` | `card startsWith "4111"` | 28 ns | 0 次 | 33 ns | 1 次 | 1.2× |
| `ends_with(s, "-sg")` | `s endsWith "-sg"` | 28 ns | 0 次 | 34 ns | 1 次 | 1.2× |
| `contains(s, "sg")` | `s contains "sg"` | 29 ns | 0 次 | 35 ns | 1 次 | 1.2× |
| `"sg" in s` | `s contains "sg"` | 31 ns | 0 次 | 34 ns | 1 次 | 1.1× |
| `upper(s)` | `upper(s)` | 53 ns | 1 次 | 62 ns | 3 次 | 1.2× |
| `lower(trim(name))` | `lower(trim(name))` | 58 ns | 1 次 | 88 ns | 5 次 | 1.5× |
| `replace(s, "-", "_")` | `replace(s, "-", "_")` | 62 ns | 1 次 | 85 ns | 4 次 | 1.4× |
| `split(csv, ",")` | `split(csv, ",")` | 111 ns | 1 次 | 99 ns | 4 次 | 0.9× |
| `join(split(csv, ","), "\|")` | `join(split(csv, ","), "\|")` | 128 ns | 2 次 | 149 ns | 6 次 | 1.2× |
| `s + ":" + country` | `s + ":" + country` | 52 ns | 2 次 | 84 ns | 5 次 | 1.6× |
| `len(s)` | `len(s)` | 30 ns | 0 次 | 32 ns | 1 次 | 1.1× |
| `slice(s, 0, 5)` | `s[0:5]` | 33 ns | 0 次 | 49 ns | 2 次 | 1.5× |
| `if(starts_with(card, "4"), "visa", "other")` | `card startsWith "4" ? "visa" : "other"` | 31 ns | 0 次 | 37 ns | 1 次 | 1.2× |
| `host.label_v1(country)` | `hostLabel(country)` | 48 ns | 1 次 | 67 ns | 4 次 | 1.4× |

### 推导式、聚合与 reduce（500 个元素）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `[x + 1 for x in xs]` | `map(xs, # + 1)` | 993 ns | 1 次 | 13.6 µs | 749 次 | 14× |
| `[x for x in xs if x % 3 == 0]` | `filter(xs, # % 3 == 0)` | 1.6 µs | 1 次 | 16.2 µs | 670 次 | 10× |
| `[f * 2.0 for f in fs]` | `map(fs, # * 2.0)` | 1.1 µs | 1 次 | 10.8 µs | 1002 次 | 9.8× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | `sum(filter(xs, # % 3 == 0), # * 2)` | 1.7 µs | 0 次 | 20.2 µs | 960 次 | 12× |
| `reduce(x in xs, total = 0, total + x)` | `reduce(xs, #acc + #, 0)` | 996 ns | 0 次 | 14.0 µs | 1000 次 | 14× |
| `len([x for x in xs if x % 2 == 0])` | `count(xs, # % 2 == 0)` | 1.9 µs | 0 次 | 12.4 µs | 501 次 | 6.4× |
| `any([x > 1000 for x in xs])` | `any(xs, # > 1000)` | 2.2 µs | 0 次 | 10.1 µs | 501 次 | 4.5× |
| `all([x >= 0 for x in xs])` | `all(xs, # >= 0)` | 3.4 µs | 0 次 | 9.7 µs | 501 次 | 2.9× |
| `!any([x < 0 for x in xs])` | `none(xs, # < 0)` | 2.2 µs | 0 次 | 10.4 µs | 501 次 | 4.8× |
| `len([x for x in xs if x == 250]) == 1` | `one(xs, # == 250)` | 949 ns | 0 次 | 9.8 µs | 501 次 | 10× |
| `first([x for x in xs if x > 400])` | `find(xs, # > 400)` | 96 ns | 0 次 | 93 ns | 4 次 | 1.0× |
| `first([x for x in xs if x == 499])` | `find(xs, # == 499)` | 967 ns | 0 次 | 6.3 µs | 324 次 | 6.5× |
| `[y + z for y in ys for z in zs]` | `flatten(map(ys, let y = #; map(zs, y + #)))` | 2.6 µs | 2 次 | 20.3 µs | 650 次 | 7.9× |
| `{string(x): x for x in xs}` | `fromPairs(map(xs, [string(#), #]))` | 17.6 µs | 404 次 | 78.2 µs | 3012 次 | 4.4× |
| `group_by(xs, [string(x % 3) for x in xs])` | `groupBy(xs, string(# % 3))` | 41.1 µs | 50 次 | 41.2 µs | 1530 次 | 1.0× |
| `[upper(n) for n in names]` | `map(names, upper(#))` | 3.3 µs | 101 次 | 4.3 µs | 303 次 | 1.3× |

### 数组与字典函数（500 个元素）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `sum(xs)` | `sum(xs)` | 321 ns | 0 次 | 11.7 µs | 1000 次 | 36× |
| `min(xs)` | `min(xs)` | 405 ns | 0 次 | 12.3 µs | 1002 次 | 30× |
| `max(xs)` | `max(xs)` | 408 ns | 0 次 | 13.0 µs | 1002 次 | 32× |
| `avg(xs)` | `mean(xs)` | 350 ns | 0 次 | 10.4 µs | 1003 次 | 30× |
| `median(fs)` | `median(fs)` | 4.8 µs | 1 次 | 4.7 µs | 4 次 | 1.0× |
| `sum(fs)` | `sum(fs)` | 349 ns | 0 次 | 9.6 µs | 999 次 | 28× |
| `sort(xs)` | `sort(ints)` | 3.8 µs | 1 次 | 18.6 µs | 249 次 | 4.9× |
| `sort_desc(xs)` | `sort(ints, "desc")` | 3.9 µs | 1 次 | 19.6 µs | 249 次 | 5.0× |
| `top_k(xs, xs, 5)` | `take(sort(ints, "desc"), 5)` | 779 ns | 2 次 | 19.5 µs | 250 次 | 25× |
| `reverse(xs)` | `reverse(xs)` | 765 ns | 1 次 | 5.5 µs | 504 次 | 7.2× |
| `unique(concat(xs, xs))` | `uniq(concat(xs, xs))` | 10.1 µs | 13 次 | 1.1 ms | 1025 次 | 110× |
| `take(xs, 10)` | `take(xs, 10)` | 73 ns | 0 次 | 72 ns | 3 次 | 1.0× |
| `first(xs) + last(xs)` | `first(xs) + last(xs)` | 64 ns | 0 次 | 104 ns | 4 次 | 1.6× |
| `concat(ys, zs)` | `concat(ys, zs)` | 145 ns | 1 次 | 857 ns | 55 次 | 5.9× |
| `flatten([ys, zs])` | `flatten([ys, zs])` | 525 ns | 3 次 | 1.1 µs | 60 次 | 2.1× |
| `range(len(ys))` | `0..len(ys)-1` | 107 ns | 1 次 | 86 ns | 3 次 | 0.8× |
| `len(xs)` | `len(xs)` | 36 ns | 0 次 | 42 ns | 2 次 | 1.2× |
| `xs[250]` | `xs[250]` | 36 ns | 0 次 | 55 ns | 2 次 | 1.5× |
| `250 in xs` | `250 in xs` | 117 ns | 0 次 | 2.8 µs | 252 次 | 24× |
| `index_of(xs, 250)` | `findIndex(xs, # == 250)` | 118 ns | 0 次 | 5.0 µs | 252 次 | 42× |
| `arg_max(xs)` | `reduce(xs, # > xs[#acc] ? #index : #acc, 0)` | 403 ns | 0 次 | 24.6 µs | 1002 次 | 61× |
| `intersect(xs, ys)` | `filter(xs, # in ys)` | 4.5 µs | 11 次 | 121.0 µs | 10333 次 | 27× |
| `except(ys, zs)` | `filter(ys, not (# in zs))` | 775 ns | 10 次 | 5.1 µs | 426 次 | 6.6× |
| `join(names, ",")` | `join(names, ",")` | 560 ns | 1 次 | 676 ns | 4 次 | 1.2× |
| `d["k042"]` | `d["k042"]` | 46 ns | 0 次 | 61 ns | 2 次 | 1.3× |
| `"k042" in d` | `"k042" in d` | 51 ns | 0 次 | 57 ns | 2 次 | 1.1× |
| `get(d, "zzz", 0)` | `d["zzz"] ?? 0` | 55 ns | 0 次 | 56 ns | 1 次 | 1.0× |
| `len(d)` | `len(d)` | 40 ns | 0 次 | 39 ns | 1 次 | 1.0× |
| `sort([k for k, v in d])` | `sort(keys(d))` | 5.6 µs | 3 次 | 6.6 µs | 108 次 | 1.2× |
| `sum([v for k, v in d])` | `sum(values(d))` | 4.5 µs | 1 次 | 5.2 µs | 300 次 | 1.1× |

### 记录（1 个订单、50 个渠道）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `order.amount` | `order.amount` | 20 ns | 0 次 | 26 ns | 1 次 | 1.3× |
| `order.amount * 2 - order.fee` | `order.amount * 2 - order.fee` | 23 ns | 0 次 | 51 ns | 3 次 | 2.3× |
| `{net: order.amount - order.fee, currency: order.currency}` | `{net: order.amount - order.fee, currency: order.currency}` | 74 ns | 0 次 | 123 ns | 4 次 | 1.7× |
| `order with {fee: 0}` | `{amount: order.amount, currency: order.currency, fee: 0}` | 101 ns | 0 次 | 116 ns | 3 次 | 1.1× |
| `[c.name for c in channels if c.healthy]` | `map(filter(channels, .healthy), .name)` | 876 ns | 1 次 | 2.7 µs | 86 次 | 3.0× |
| `sum([c.fee for c in channels])` | `sum(channels, .fee)` | 505 ns | 0 次 | 1.8 µs | 96 次 | 3.6× |
| `len([c for c in channels if c.healthy && c.fee < 50])` | `count(channels, .healthy && .fee < 50)` | 891 ns | 0 次 | 2.9 µs | 84 次 | 3.3× |
| `sort_by(channels, .fee)` | `sortBy(channels, .fee)` | 1.9 µs | 7 次 | 3.2 µs | 105 次 | 1.7× |
| `min_by(channels, .fee).name` | `find(channels, .fee == min(map(channels, .fee))).name` | 670 ns | 3 次 | 1.7 µs | 56 次 | 2.5× |

### 宿主边界：宽 struct 与长向量

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 17 ns | 0 次 | 46 ns | 1 次 | 2.7× |
| `order.amount` | `order.amount` | 19 ns | 0 次 | 39 ns | 1 次 | 2.0× |
| `len(xs)` | `len(xs)` | 39 ns | 0 次 | 46 ns | 2 次 | 1.2× |
| `len(fs)` | `len(fs)` | 33 ns | 0 次 | 28 ns | 2 次 | 0.9× |
| `host.total_v1(fs)` | `hostTotal(fs)` | 36.9 µs | 0 次 | 36.6 µs | 3 次 | 1.0× |
| `sum(fs)` | `sum(fs)` | 36.6 µs | 0 次 | 1.3 ms | 130416 次 | 35× |

### 编译

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 5.2 µs | 85 次 | 5.4 µs | 85 次 | 1.0× |
| `switch(case a > 10 => "big", case a > 5 => "mid", else => "small")` | `a > 10 ? "big" : a > 5 ? "mid" : "small"` | 11.4 µs | 152 次 | 7.2 µs | 127 次 | 0.6× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | `sum(filter(xs, # % 3 == 0), # * 2)` | 16.9 µs | 262 次 | 8.5 µs | 134 次 | 0.5× |
| `[c.name for c in channels if c.healthy]` | `map(filter(channels, .healthy), .name)` | 10.6 µs | 202 次 | 7.9 µs | 149 次 | 0.7× |

### 并行：同一条规则在全部 10 个核上同时跑

`a * b - a % b + 1`，每个 goroutine 从自己的宿主 struct 读参数。数字是每次运行的时间（全部核的总耗时除以总次数），越小越好；上表 expr 用的复用 `vm.VM` 不能并发，这里是它能并发的三种用法。

| 用法 | 1 核 | 10 核 | 分配 |
|---|---|---|---|
| FunRoute `Program.Run` | 20 ns | 3.72 ns | 0 次 |
| expr：每个 goroutine 一个 `vm.VM` | 71 ns | 21 ns | 1 次 |
| expr：`sync.Pool` 管理 `vm.VM` | 76 ns | 25 ns | 1 次 |
| expr：`expr.Run`（每次新建 `VM`） | 105 ns | 46 ns | 3 次 |

写法的差别：expr 的 `/` 总是浮点除法，整数除法写成 `int(a / b)`；expr 的 `sort` 不接受 `[]int64`，排序的几行 expr 读同样内容的 `[]int`；FunRoute 的推导式在 expr 里是带谓词的 `map`/`filter`/`sum`/`count`/`any`，`switch` 是连写的 `?:`；expr 没有 `intersect`/`except`/`arg_max`/`top_k`/`with`/`index_of`，写成它能写的等价形式。

没有对照的：金额（`money`/`ratio`/`fxrate`、`round(…, @mode)`、`using` 与 `->`、`allocate`）、`fallback`、枚举与穷尽的 `switch`、句柄与模型批处理，expr 没有对应；`windows`、`chunk`、`deltas`、`cumsum`、`take_while`、`drop_while`、`stddev`、`percentile`、`rank`、`pad_left`/`pad_right`、`merge` 在 expr 里没有内置函数。
