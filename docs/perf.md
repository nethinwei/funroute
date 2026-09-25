# 性能报告

由 `make perf` 生成（`go run ./tests/perf`，与 expr 的对照来自 `tests/perf/expr`），不要手改。数字随机器变化，取的是多次运行里最快的一次；能力的边界见 [limits.md](limits.md)。

- 日期：2026-09-26
- 机器：Apple M5，darwin/arm64
- Go：go1.26.3
- 提交：`0016fc3（工作区有未提交的改动）`

## 单次执行

"原生 Go"是同一件事直接用 Go 写：宿主惯常的写法，不查溢出、不过边界；换汇调用的就是规则里用的 `Currencies.Convert`。倍数是 FunRoute 的耗时除以它。

| 场景 | FunRoute | 分配 | 原生 Go | 分配 | 倍数 |
|---|---|---|---|---|---|
| `amount * bps / 10000 + fixed`：`RunValues` | 40 ns | 0 次 | 1.81 ns | 0 次 | 22× |
| 同上：`Run(map)` | 59 ns | 0 次 | 1.81 ns | 0 次 | 33× |
| 同上：`Program.Run`（从宿主 struct 读参数） | 41 ns | 0 次 | 1.81 ns | 0 次 | 22× |
| 一次内核函数调用 `a + b` | 35 ns | 0 次 | 1.71 ns | 0 次 | 20× |
| 一次按 Go 签名注册的函数调用（常见签名，不经反射） | 53 ns | 0 次 | 1.71 ns | 0 次 | 31× |
| `using` 里换汇一次 | 194 ns | 0 次 | 54 ns | 0 次 | 3.6× |
| 500 个元素的 `reduce` | 685 ns | 0 次 | 134 ns | 0 次 | 5.1× |
| 500 个元素的推导式 | 942 ns | 2 次 | 482 ns | 1 次 | 2.0× |
| 500 对元素的嵌套推导式（25 × 20） | 3.4 µs | 5 次 | 490 ns | 1 次 | 6.9× |
| 把 16 个 float 交给宿主函数（与长度无关：不拷贝） | 73 ns | 0 次 | 1.74 ns | 0 次 | 42× |
| 把 1024 个 float 交给宿主函数（与长度无关：不拷贝） | 73 ns | 0 次 | 1.75 ns | 0 次 | 42× |
| 把 65536 个 float 交给宿主函数（与长度无关：不拷贝） | 72 ns | 0 次 | 1.73 ns | 0 次 | 42× |
| 模型调用（引擎每次 20 µs），一条一条 | 27.3 µs | 0 次 | 27.0 µs | 0 次 | 1.0× |
| 同上，64 条一批，折合每条 | 673 ns | 2 次 | 424 ns | 0 次 | 1.6× |

## 大输入的吞吐

参数是固定种子打乱的 0…n−1，两边用同一份。"原生 Go"是同一件事的 Go 循环，在 100 万个元素上测：筛选求和不建中间数组；排序用 `slices.Sort`，std 的 `sort` 在没有 0 与 -0 这种相等却可区分的元素时也用它。

| 程序 | n = 1 万 | n = 10 万 | n = 100 万 | 每个元素 | 原生 Go 每个元素 | 倍数 |
|---|---|---|---|---|---|---|
| `[x + 1 for x in xs]` | 13.1 µs | 161.1 µs | 1.2 ms | 1.2 ns | 0.33 ns | 3.8× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | 31.7 µs | 321.8 µs | 3.1 ms | 3.1 ns | 2.39 ns | 1.3× |
| `{string(x): x for x in xs}` | 369.5 µs | 4.4 ms | 107.5 ms | 107.5 ns | 82.61 ns | 1.3× |
| `sort(xs)` | 282.2 µs | 4.4 ms | 54.5 ms | 54.5 ns | 53.78 ns | 1.0× |
| `sum(xs)` | 7.2 µs | 70.7 µs | 732.3 µs | 0.7 ns | 0.25 ns | 2.9× |
| 1000 × 1000 的嵌套推导式 | — | — | 1.3 ms（100 万对） | 1.3 ns / 对 | 0.30 ns / 对 | 4.5× |

## 编译

`[a * 0 + b, a * 1 + b, …]`：很多重载调用共用同一对参数。

| 项数 | 源码 | 有契约 | 无契约（全部推导） |
|---|---|---|---|
| 100 | 1.2 KB | 1.1 ms | 3.1 ms |
| 500 | 6.2 KB | 6.4 ms | 37.2 ms |
| 1000 | 12.6 KB | 12.4 ms | 135.4 ms |
| 1900 | 24.9 KB | 23.2 ms | 488.2 ms |

`x0 + x1 + …`，变量互不相同，无契约：

| 项数 | 编译 |
|---|---|
| 10 | 84.9 µs |
| 100 | 824.8 µs |
| 1000 | 10.4 ms |

## 语言服务

| 请求 | 一条规则（59 字节） | 1000 项数组，无契约（13 KB） | 8000 个常量写在一行（46 KB） |
|---|---|---|---|
| 打开文档并发布诊断 | 41.6 µs | 133.5 ms | 12.6 ms |
| 悬停 | 3.8 µs | 20.4 µs | 14.9 µs |
| 补全 | 254.4 µs | 3.7 ms | 6.8 ms |
| 语义标记 | 9.5 µs | 2.1 ms | 3.6 ms |
| 格式化 | 7.6 µs | 2.1 ms | 5.1 ms |
| 语法树 | 14.8 µs | 4.4 ms | 5.2 ms |

## 产物

- 浏览器里的语言服务 `funroute.wasm`：7.78 MB（gzip 后 2.04 MB）
- 前端 JS：525 KB

## 与 expr 对照

由 `tests/perf/expr` 生成：它是单独的 module，只有它依赖 expr（v1.17.8），FunRoute 的 go.mod 仍然为空。两边从同一个宿主 struct 读参数，先编译好再反复执行，每边测 300ms。FunRoute 用 `Program.Run`（可并发调用）；expr 用复用的 `vm.VM`，这是它最快的用法，但一个 `VM` 不能并发；宿主函数在 expr 里经 `expr.Function` 注册。每一行都先经 JSON 核对两边答案相同。倍数是 expr 的耗时除以 FunRoute 的，大于 1 表示 FunRoute 更快。

### 标量：运算符、条件、绑定、数值函数、转换、宿主函数

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 35 ns | 0 次 | 29 ns | 1 次 | 0.8× |
| `a * b - a % b + 1` | `a * b - a % b + 1` | 41 ns | 0 次 | 55 ns | 1 次 | 1.3× |
| `a / b` | `int(a / b)` | 35 ns | 0 次 | 38 ns | 2 次 | 1.1× |
| `-a + b` | `-a + b` | 35 ns | 0 次 | 40 ns | 3 次 | 1.1× |
| `x * y + x / y - 1.5` | `x * y + x / y - 1.5` | 43 ns | 0 次 | 70 ns | 5 次 | 1.6× |
| `float(a) / float(b)` | `a / b` | 41 ns | 0 次 | 34 ns | 2 次 | 0.8× |
| `a > b && x < y \|\| !flag` | `a > b && x < y \|\| !flag` | 48 ns | 0 次 | 49 ns | 1 次 | 1.0× |
| `a == 7 && b != 4` | `a == 7 && b != 4` | 39 ns | 0 次 | 40 ns | 1 次 | 1.0× |
| `if(a > b, a, b)` | `a > b ? a : b` | 36 ns | 0 次 | 37 ns | 1 次 | 1.1× |
| `switch(a, case 1 => "one", case 2, 3 => "few", else => "many")` | `a == 1 ? "one" : a in [2, 3] ? "few" : "many"` | 45 ns | 0 次 | 50 ns | 1 次 | 1.1× |
| `switch(case a > 10 => "big", case a > 5 => "mid", else => "small")` | `a > 10 ? "big" : a > 5 ? "mid" : "small"` | 35 ns | 0 次 | 42 ns | 1 次 | 1.2× |
| `let(s = a + b, d = a - b, s * d)` | `let s = a + b; let d = a - b; s * d` | 37 ns | 0 次 | 56 ns | 1 次 | 1.5× |
| `7 * 24 * 3600 + a` | `7 * 24 * 3600 + a` | 31 ns | 0 次 | 29 ns | 2 次 | 0.9× |
| `abs(b - a)` | `abs(b - a)` | 40 ns | 0 次 | 37 ns | 2 次 | 0.9× |
| `max(a, b)` | `max(a, b)` | 39 ns | 0 次 | 52 ns | 2 次 | 1.3× |
| `ceil(x)` | `ceil(x)` | 36 ns | 0 次 | 27 ns | 2 次 | 0.8× |
| `floor(x) + round(y)` | `floor(x) + round(y)` | 47 ns | 0 次 | 50 ns | 4 次 | 1.1× |
| `pow(x, 2.0)` | `x ** 2` | 44 ns | 0 次 | 35 ns | 2 次 | 0.8× |
| `int(y) + a` | `int(y) + a` | 41 ns | 0 次 | 33 ns | 1 次 | 0.8× |
| `float(a) * x` | `float(a) * x` | 38 ns | 0 次 | 41 ns | 3 次 | 1.1× |
| `string(a)` | `string(a)` | 39 ns | 0 次 | 51 ns | 2 次 | 1.3× |
| `host.add_v1(a, b)` | `hostAdd(a, b)` | 52 ns | 0 次 | 44 ns | 2 次 | 0.8× |
| `host.scale_v1(x, a)` | `hostScale(x, a)` | 57 ns | 0 次 | 51 ns | 3 次 | 0.9× |

### 字符串

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `country == "MY"` | `country == "MY"` | 36 ns | 0 次 | 31 ns | 1 次 | 0.9× |
| `country in ["SG", "MY", "TH"]` | `country in ["SG", "MY", "TH"]` | 47 ns | 0 次 | 43 ns | 1 次 | 0.9× |
| `starts_with(card, "4111")` | `card startsWith "4111"` | 39 ns | 0 次 | 34 ns | 1 次 | 0.9× |
| `ends_with(s, "-sg")` | `s endsWith "-sg"` | 40 ns | 0 次 | 34 ns | 1 次 | 0.9× |
| `contains(s, "sg")` | `s contains "sg"` | 42 ns | 0 次 | 35 ns | 1 次 | 0.8× |
| `"sg" in s` | `s contains "sg"` | 43 ns | 0 次 | 35 ns | 1 次 | 0.8× |
| `upper(s)` | `upper(s)` | 67 ns | 1 次 | 63 ns | 3 次 | 0.9× |
| `lower(trim(name))` | `lower(trim(name))` | 69 ns | 1 次 | 88 ns | 5 次 | 1.3× |
| `replace(s, "-", "_")` | `replace(s, "-", "_")` | 84 ns | 1 次 | 85 ns | 4 次 | 1.0× |
| `split(csv, ",")` | `split(csv, ",")` | 122 ns | 2 次 | 100 ns | 4 次 | 0.8× |
| `join(split(csv, ","), "\|")` | `join(split(csv, ","), "\|")` | 170 ns | 3 次 | 156 ns | 6 次 | 0.9× |
| `s + ":" + country` | `s + ":" + country` | 69 ns | 2 次 | 86 ns | 5 次 | 1.2× |
| `len(s)` | `len(s)` | 39 ns | 0 次 | 32 ns | 1 次 | 0.8× |
| `slice(s, 0, 5)` | `s[0:5]` | 43 ns | 0 次 | 51 ns | 2 次 | 1.2× |
| `if(starts_with(card, "4"), "visa", "other")` | `card startsWith "4" ? "visa" : "other"` | 43 ns | 0 次 | 38 ns | 1 次 | 0.9× |
| `host.label_v1(country)` | `hostLabel(country)` | 67 ns | 1 次 | 65 ns | 4 次 | 1.0× |

### 推导式、聚合与 reduce（500 个元素）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `[x + 1 for x in xs]` | `map(xs, # + 1)` | 962 ns | 1 次 | 13.8 µs | 749 次 | 14× |
| `[x for x in xs if x % 3 == 0]` | `filter(xs, # % 3 == 0)` | 1.6 µs | 1 次 | 15.7 µs | 670 次 | 9.7× |
| `[f * 2.0 for f in fs]` | `map(fs, # * 2.0)` | 1.2 µs | 1 次 | 10.9 µs | 1002 次 | 9.0× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | `sum(filter(xs, # % 3 == 0), # * 2)` | 1.7 µs | 0 次 | 20.3 µs | 960 次 | 12× |
| `reduce(x in xs, total = 0, total + x)` | `reduce(xs, #acc + #, 0)` | 606 ns | 0 次 | 14.4 µs | 1000 次 | 24× |
| `len([x for x in xs if x % 2 == 0])` | `count(xs, # % 2 == 0)` | 1.5 µs | 0 次 | 12.5 µs | 501 次 | 8.5× |
| `any([x > 1000 for x in xs])` | `any(xs, # > 1000)` | 2.3 µs | 0 次 | 10.0 µs | 501 次 | 4.3× |
| `all([x >= 0 for x in xs])` | `all(xs, # >= 0)` | 2.4 µs | 0 次 | 12.0 µs | 501 次 | 5.0× |
| `!any([x < 0 for x in xs])` | `none(xs, # < 0)` | 2.4 µs | 0 次 | 10.9 µs | 501 次 | 4.6× |
| `len([x for x in xs if x == 250]) == 1` | `one(xs, # == 250)` | 1.0 µs | 0 次 | 9.9 µs | 501 次 | 9.7× |
| `first([x for x in xs if x > 400])` | `find(xs, # > 400)` | 99 ns | 0 次 | 95 ns | 4 次 | 1.0× |
| `[y + z for y in ys for z in zs]` | `flatten(map(ys, let y = #; map(zs, y + #)))` | 2.7 µs | 2 次 | 21.0 µs | 650 次 | 7.7× |
| `{string(x): x for x in xs}` | `fromPairs(map(xs, [string(#), #]))` | 16.8 µs | 404 次 | 79.7 µs | 3012 次 | 4.8× |
| `group_by(xs, [string(x % 3) for x in xs])` | `groupBy(xs, string(# % 3))` | 41.7 µs | 52 次 | 43.8 µs | 1530 次 | 1.1× |
| `[upper(n) for n in names]` | `map(names, upper(#))` | 3.3 µs | 101 次 | 4.3 µs | 303 次 | 1.3× |

### 数组与字典函数（500 个元素）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `sum(xs)` | `sum(xs)` | 342 ns | 1 次 | 12.1 µs | 1000 次 | 35× |
| `min(xs)` | `min(xs)` | 349 ns | 1 次 | 12.7 µs | 1002 次 | 36× |
| `max(xs)` | `max(xs)` | 450 ns | 1 次 | 12.4 µs | 1002 次 | 27× |
| `avg(xs)` | `mean(xs)` | 384 ns | 1 次 | 10.9 µs | 1003 次 | 28× |
| `median(fs)` | `median(fs)` | 5.1 µs | 2 次 | 4.9 µs | 4 次 | 1.0× |
| `sum(fs)` | `sum(fs)` | 519 ns | 1 次 | 9.7 µs | 999 次 | 19× |
| `sort(xs)` | `sort(ints)` | 4.1 µs | 3 次 | 19.0 µs | 249 次 | 4.6× |
| `sort_desc(xs)` | `sort(ints, "desc")` | 4.1 µs | 3 次 | 21.4 µs | 249 次 | 5.3× |
| `top_k(xs, xs, 5)` | `take(sort(ints, "desc"), 5)` | 23.0 µs | 5 次 | 20.2 µs | 250 次 | 0.9× |
| `reverse(xs)` | `reverse(xs)` | 655 ns | 3 次 | 5.7 µs | 504 次 | 8.6× |
| `unique(concat(xs, xs))` | `uniq(concat(xs, xs))` | 30.5 µs | 20 次 | 1.1 ms | 1025 次 | 37× |
| `take(xs, 10)` | `take(xs, 10)` | 136 ns | 2 次 | 74 ns | 3 次 | 0.5× |
| `first(xs) + last(xs)` | `first(xs) + last(xs)` | 143 ns | 1 次 | 109 ns | 4 次 | 0.8× |
| `concat(ys, zs)` | `concat(ys, zs)` | 1.3 µs | 8 次 | 875 ns | 55 次 | 0.7× |
| `flatten([ys, zs])` | `flatten([ys, zs])` | 1.4 µs | 10 次 | 1.2 µs | 60 次 | 0.8× |
| `range(len(ys))` | `0..len(ys)-1` | 173 ns | 2 次 | 90 ns | 3 次 | 0.5× |
| `len(xs)` | `len(xs)` | 53 ns | 0 次 | 43 ns | 2 次 | 0.8× |
| `xs[250]` | `xs[250]` | 53 ns | 0 次 | 54 ns | 2 次 | 1.0× |
| `250 in xs` | `250 in xs` | 138 ns | 1 次 | 2.9 µs | 252 次 | 21× |
| `index_of(xs, 250)` | `findIndex(xs, # == 250)` | 197 ns | 1 次 | 5.2 µs | 252 次 | 26× |
| `arg_max(xs)` | `reduce(xs, # > xs[#acc] ? #index : #acc, 0)` | 469 ns | 1 次 | 25.5 µs | 1002 次 | 54× |
| `intersect(xs, ys)` | `filter(xs, # in ys)` | 4.8 µs | 12 次 | 122.7 µs | 10333 次 | 25× |
| `except(ys, zs)` | `filter(ys, not (# in zs))` | 826 ns | 11 次 | 5.3 µs | 426 次 | 6.4× |
| `join(names, ",")` | `join(names, ",")` | 681 ns | 2 次 | 651 ns | 4 次 | 1.0× |
| `d["k042"]` | `d["k042"]` | 73 ns | 0 次 | 62 ns | 2 次 | 0.8× |
| `"k042" in d` | `"k042" in d` | 70 ns | 0 次 | 58 ns | 2 次 | 0.8× |
| `get(d, "zzz", 0)` | `d["zzz"] ?? 0` | 91 ns | 0 次 | 57 ns | 1 次 | 0.6× |
| `len(d)` | `len(d)` | 50 ns | 0 次 | 39 ns | 1 次 | 0.8× |
| `sort([k for k, v in d])` | `sort(keys(d))` | 5.6 µs | 5 次 | 6.5 µs | 108 次 | 1.2× |
| `sum([v for k, v in d])` | `sum(values(d))` | 4.5 µs | 1 次 | 5.5 µs | 299 次 | 1.2× |

### 记录（1 个订单、50 个渠道）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `order.amount` | `order.amount` | 34 ns | 0 次 | 27 ns | 1 次 | 0.8× |
| `order.amount * 2 - order.fee` | `order.amount * 2 - order.fee` | 41 ns | 0 次 | 52 ns | 3 次 | 1.3× |
| `{net: order.amount - order.fee, currency: order.currency}` | `{net: order.amount - order.fee, currency: order.currency}` | 79 ns | 0 次 | 128 ns | 4 次 | 1.6× |
| `order with {fee: 0}` | `{amount: order.amount, currency: order.currency, fee: 0}` | 189 ns | 2 次 | 119 ns | 3 次 | 0.6× |
| `[c.name for c in channels if c.healthy]` | `map(filter(channels, .healthy), .name)` | 1.3 µs | 1 次 | 2.8 µs | 86 次 | 2.1× |
| `sum([c.fee for c in channels])` | `sum(channels, .fee)` | 873 ns | 0 次 | 1.9 µs | 96 次 | 2.1× |
| `len([c for c in channels if c.healthy && c.fee < 50])` | `count(channels, .healthy && .fee < 50)` | 1.4 µs | 0 次 | 3.0 µs | 84 次 | 2.2× |
| `sort_by(channels, [c.fee for c in channels])` | `sortBy(channels, .fee)` | 11.4 µs | 13 次 | 3.3 µs | 105 次 | 0.3× |
| `channels[arg_min([c.fee for c in channels])].name` | `find(channels, .fee == min(map(channels, .fee))).name` | 4.1 µs | 6 次 | 1.7 µs | 56 次 | 0.4× |

### 宿主边界：宽 struct 与长向量

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 35 ns | 0 次 | 48 ns | 1 次 | 1.4× |
| `order.amount` | `order.amount` | 34 ns | 0 次 | 41 ns | 1 次 | 1.2× |
| `len(xs)` | `len(xs)` | 54 ns | 0 次 | 46 ns | 2 次 | 0.9× |
| `len(fs)` | `len(fs)` | 16.0 µs | 0 次 | 29 ns | 2 次 | 0.0× |
| `host.total_v1(fs)` | `hostTotal(fs)` | 56.7 µs | 1 次 | 40.5 µs | 3 次 | 0.7× |
| `sum(fs)` | `sum(fs)` | 54.6 µs | 1 次 | 1.3 ms | 130416 次 | 24× |

### 编译

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 11.0 µs | 179 次 | 5.5 µs | 85 次 | 0.5× |
| `switch(case a > 10 => "big", case a > 5 => "mid", else => "small")` | `a > 10 ? "big" : a > 5 ? "mid" : "small"` | 26.6 µs | 424 次 | 7.3 µs | 127 次 | 0.3× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | `sum(filter(xs, # % 3 == 0), # * 2)` | 35.8 µs | 575 次 | 8.7 µs | 134 次 | 0.2× |
| `[c.name for c in channels if c.healthy]` | `map(filter(channels, .healthy), .name)` | 21.1 µs | 404 次 | 8.4 µs | 149 次 | 0.4× |

### 并行：同一条规则在全部 10 个核上同时跑

`a * b - a % b + 1`，每个 goroutine 从自己的宿主 struct 读参数。数字是每次运行的时间（全部核的总耗时除以总次数），越小越好；上表 expr 用的复用 `vm.VM` 不能并发，这里是它能并发的三种用法。

| 用法 | 1 核 | 10 核 | 分配 |
|---|---|---|---|
| FunRoute `Program.Run` | 41 ns | 7.22 ns | 0 次 |
| expr：每个 goroutine 一个 `vm.VM` | 74 ns | 22 ns | 1 次 |
| expr：`sync.Pool` 管理 `vm.VM` | 82 ns | 25 ns | 1 次 |
| expr：`expr.Run`（每次新建 `VM`） | 111 ns | 49 ns | 3 次 |

写法的差别：expr 的 `/` 总是浮点除法，整数除法写成 `int(a / b)`；expr 的 `sort` 不接受 `[]int64`，排序的几行 expr 读同样内容的 `[]int`；FunRoute 的推导式在 expr 里是带谓词的 `map`/`filter`/`sum`/`count`/`any`，`switch` 是连写的 `?:`；expr 没有 `intersect`/`except`/`arg_max`/`top_k`/`with`/`index_of`，写成它能写的等价形式。

没有对照的：金额（`money`/`ratio`/`fxrate`、`round(…, @mode)`、`using` 与 `->`、`allocate`）、`fallback`、枚举与穷尽的 `switch`、句柄与模型批处理，expr 没有对应；`windows`、`chunk`、`deltas`、`cumsum`、`take_while`、`drop_while`、`stddev`、`percentile`、`rank`、`pad_left`/`pad_right`、`merge` 在 expr 里没有内置函数。
