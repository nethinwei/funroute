# 性能报告

由 `make perf` 生成（`go run ./tests/perf`，与 expr 的对照来自 `tests/perf/expr`），不要手改。数字随机器变化，取的是多次运行里最快的一次；能力的边界见 [limits.md](limits.md)。

- 日期：2026-09-26
- 机器：Apple M5，darwin/arm64
- Go：go1.26.3
- 提交：`1cd159c（工作区有未提交的改动）`

## 单次执行

"原生 Go"是同一件事直接用 Go 写：宿主惯常的写法，不查溢出、不过边界；换汇调用的就是规则里用的 `Currencies.Convert`。倍数是 FunRoute 的耗时除以它。

| 场景 | FunRoute | 分配 | 原生 Go | 分配 | 倍数 |
|---|---|---|---|---|---|
| `amount * bps / 10000 + fixed`：`RunValues` | 40 ns | 0 次 | 1.82 ns | 0 次 | 22× |
| 同上：`Run(map)` | 59 ns | 0 次 | 1.82 ns | 0 次 | 32× |
| 同上：`Program.Run`（从宿主 struct 读参数） | 42 ns | 0 次 | 1.82 ns | 0 次 | 23× |
| 一次内核函数调用 `a + b` | 35 ns | 0 次 | 1.74 ns | 0 次 | 20× |
| 一次按 Go 签名注册的函数调用（常见签名，不经反射） | 56 ns | 0 次 | 1.86 ns | 0 次 | 30× |
| `using` 里换汇一次 | 195 ns | 0 次 | 54 ns | 0 次 | 3.6× |
| 500 个元素的 `reduce` | 768 ns | 0 次 | 135 ns | 0 次 | 5.7× |
| 500 个元素的推导式 | 973 ns | 2 次 | 466 ns | 1 次 | 2.1× |
| 500 对元素的嵌套推导式（25 × 20） | 3.4 µs | 5 次 | 476 ns | 1 次 | 7.1× |
| 把 16 个 float 交给宿主函数（与长度无关：不拷贝） | 72 ns | 0 次 | 1.82 ns | 0 次 | 40× |
| 把 1024 个 float 交给宿主函数（与长度无关：不拷贝） | 69 ns | 0 次 | 1.78 ns | 0 次 | 39× |
| 把 65536 个 float 交给宿主函数（与长度无关：不拷贝） | 70 ns | 0 次 | 1.77 ns | 0 次 | 40× |
| 模型调用（引擎每次 20 µs），一条一条 | 27.5 µs | 0 次 | 27.3 µs | 0 次 | 1.0× |
| 同上，64 条一批，折合每条 | 687 ns | 2 次 | 446 ns | 0 次 | 1.5× |

## 大输入的吞吐

参数是固定种子打乱的 0…n−1，两边用同一份。"原生 Go"是同一件事的 Go 循环，在 100 万个元素上测：筛选求和不建中间数组；排序用 `slices.Sort`，std 的 `sort` 在没有 0 与 -0 这种相等却可区分的元素时也用它。

| 程序 | n = 1 万 | n = 10 万 | n = 100 万 | 每个元素 | 原生 Go 每个元素 | 倍数 |
|---|---|---|---|---|---|---|
| `[x + 1 for x in xs]` | 15.2 µs | 100.1 µs | 1.1 ms | 1.1 ns | 0.32 ns | 3.4× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | 32.9 µs | 375.0 µs | 3.0 ms | 3.0 ns | 2.52 ns | 1.2× |
| `{string(x): x for x in xs}` | 383.1 µs | 4.4 ms | 110.9 ms | 110.9 ns | 87.35 ns | 1.3× |
| `sort(xs)` | 291.6 µs | 4.4 ms | 55.2 ms | 55.2 ns | 54.84 ns | 1.0× |
| `sum(xs)` | 7.2 µs | 46.3 µs | 728.4 µs | 0.7 ns | 0.25 ns | 2.9× |
| 1000 × 1000 的嵌套推导式 | — | — | 1.2 ms（100 万对） | 1.2 ns / 对 | 0.29 ns / 对 | 4.0× |

## 编译

`[a * 0 + b, a * 1 + b, …]`：很多重载调用共用同一对参数。

| 项数 | 源码 | 有契约 | 无契约（全部推导） |
|---|---|---|---|
| 100 | 1.2 KB | 1.3 ms | 3.2 ms |
| 500 | 6.2 KB | 6.4 ms | 36.8 ms |
| 1000 | 12.6 KB | 12.1 ms | 135.1 ms |
| 1900 | 24.9 KB | 23.5 ms | 489.3 ms |

`x0 + x1 + …`，变量互不相同，无契约：

| 项数 | 编译 |
|---|---|
| 10 | 88.6 µs |
| 100 | 828.5 µs |
| 1000 | 10.4 ms |

## 语言服务

| 请求 | 一条规则（59 字节） | 1000 项数组，无契约（13 KB） | 8000 个常量写在一行（46 KB） |
|---|---|---|---|
| 打开文档并发布诊断 | 41.0 µs | 133.9 ms | 13.3 ms |
| 悬停 | 4.1 µs | 16.2 µs | 15.6 µs |
| 补全 | 253.0 µs | 4.0 ms | 7.1 ms |
| 语义标记 | 9.8 µs | 2.2 ms | 3.5 ms |
| 格式化 | 7.3 µs | 2.3 ms | 5.0 ms |
| 语法树 | 14.9 µs | 4.5 ms | 5.4 ms |

## 产物

- 浏览器里的语言服务 `funroute.wasm`：7.76 MB（gzip 后 2.03 MB）
- 前端 JS：525 KB

## 与 expr 对照

由 `tests/perf/expr` 生成：它是单独的 module，只有它依赖 expr（v1.17.8），FunRoute 的 go.mod 仍然为空。两边从同一个宿主 struct 读参数，先编译好再反复执行，每边测 300ms。FunRoute 用 `Program.Run`（可并发调用）；expr 用复用的 `vm.VM`，这是它最快的用法，但一个 `VM` 不能并发；宿主函数在 expr 里经 `expr.Function` 注册。每一行都先经 JSON 核对两边答案相同。倍数是 expr 的耗时除以 FunRoute 的，大于 1 表示 FunRoute 更快。

### 标量：运算符、条件、绑定、数值函数、转换、宿主函数

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 36 ns | 0 次 | 30 ns | 1 次 | 0.9× |
| `a * b - a % b + 1` | `a * b - a % b + 1` | 45 ns | 0 次 | 56 ns | 1 次 | 1.2× |
| `a / b` | `int(a / b)` | 35 ns | 0 次 | 38 ns | 2 次 | 1.1× |
| `-a + b` | `-a + b` | 37 ns | 0 次 | 42 ns | 3 次 | 1.1× |
| `x * y + x / y - 1.5` | `x * y + x / y - 1.5` | 44 ns | 0 次 | 72 ns | 5 次 | 1.6× |
| `float(a) / float(b)` | `a / b` | 41 ns | 0 次 | 34 ns | 2 次 | 0.8× |
| `a > b && x < y \|\| !flag` | `a > b && x < y \|\| !flag` | 49 ns | 0 次 | 49 ns | 1 次 | 1.0× |
| `a == 7 && b != 4` | `a == 7 && b != 4` | 40 ns | 0 次 | 41 ns | 1 次 | 1.0× |
| `if(a > b, a, b)` | `a > b ? a : b` | 35 ns | 0 次 | 42 ns | 1 次 | 1.2× |
| `switch(a, case 1 => "one", case 2, 3 => "few", else => "many")` | `a == 1 ? "one" : a in [2, 3] ? "few" : "many"` | 47 ns | 0 次 | 51 ns | 1 次 | 1.1× |
| `switch(case a > 10 => "big", case a > 5 => "mid", else => "small")` | `a > 10 ? "big" : a > 5 ? "mid" : "small"` | 36 ns | 0 次 | 43 ns | 1 次 | 1.2× |
| `let(s = a + b, d = a - b, s * d)` | `let s = a + b; let d = a - b; s * d` | 37 ns | 0 次 | 61 ns | 1 次 | 1.7× |
| `7 * 24 * 3600 + a` | `7 * 24 * 3600 + a` | 31 ns | 0 次 | 30 ns | 2 次 | 1.0× |
| `abs(b - a)` | `abs(b - a)` | 39 ns | 0 次 | 38 ns | 2 次 | 1.0× |
| `max(a, b)` | `max(a, b)` | 39 ns | 0 次 | 53 ns | 2 次 | 1.4× |
| `ceil(x)` | `ceil(x)` | 37 ns | 0 次 | 29 ns | 2 次 | 0.8× |
| `floor(x) + round(y)` | `floor(x) + round(y)` | 48 ns | 0 次 | 51 ns | 4 次 | 1.0× |
| `pow(x, 2.0)` | `x ** 2` | 42 ns | 0 次 | 35 ns | 2 次 | 0.8× |
| `int(y) + a` | `int(y) + a` | 42 ns | 0 次 | 38 ns | 1 次 | 0.9× |
| `float(a) * x` | `float(a) * x` | 39 ns | 0 次 | 43 ns | 3 次 | 1.1× |
| `string(a)` | `string(a)` | 42 ns | 0 次 | 51 ns | 2 次 | 1.2× |
| `host.add_v1(a, b)` | `hostAdd(a, b)` | 53 ns | 0 次 | 44 ns | 2 次 | 0.8× |
| `host.scale_v1(x, a)` | `hostScale(x, a)` | 54 ns | 0 次 | 57 ns | 3 次 | 1.1× |

### 字符串

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `country == "MY"` | `country == "MY"` | 37 ns | 0 次 | 31 ns | 1 次 | 0.8× |
| `country in ["SG", "MY", "TH"]` | `country in ["SG", "MY", "TH"]` | 48 ns | 0 次 | 44 ns | 1 次 | 0.9× |
| `starts_with(card, "4111")` | `card startsWith "4111"` | 40 ns | 0 次 | 34 ns | 1 次 | 0.9× |
| `ends_with(s, "-sg")` | `s endsWith "-sg"` | 42 ns | 0 次 | 35 ns | 1 次 | 0.8× |
| `contains(s, "sg")` | `s contains "sg"` | 47 ns | 0 次 | 37 ns | 1 次 | 0.8× |
| `"sg" in s` | `s contains "sg"` | 44 ns | 0 次 | 36 ns | 1 次 | 0.8× |
| `upper(s)` | `upper(s)` | 67 ns | 1 次 | 64 ns | 3 次 | 0.9× |
| `lower(trim(name))` | `lower(trim(name))` | 72 ns | 1 次 | 91 ns | 5 次 | 1.3× |
| `replace(s, "-", "_")` | `replace(s, "-", "_")` | 99 ns | 1 次 | 88 ns | 4 次 | 0.9× |
| `split(csv, ",")` | `split(csv, ",")` | 137 ns | 2 次 | 103 ns | 4 次 | 0.8× |
| `join(split(csv, ","), "\|")` | `join(split(csv, ","), "\|")` | 188 ns | 3 次 | 162 ns | 6 次 | 0.9× |
| `s + ":" + country` | `s + ":" + country` | 69 ns | 2 次 | 88 ns | 5 次 | 1.3× |
| `len(s)` | `len(s)` | 40 ns | 0 次 | 33 ns | 1 次 | 0.8× |
| `slice(s, 0, 5)` | `s[0:5]` | 44 ns | 0 次 | 51 ns | 2 次 | 1.2× |
| `if(starts_with(card, "4"), "visa", "other")` | `card startsWith "4" ? "visa" : "other"` | 42 ns | 0 次 | 39 ns | 1 次 | 0.9× |
| `host.label_v1(country)` | `hostLabel(country)` | 68 ns | 1 次 | 66 ns | 4 次 | 1.0× |

### 推导式、聚合与 reduce（500 个元素）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `[x + 1 for x in xs]` | `map(xs, # + 1)` | 992 ns | 1 次 | 13.8 µs | 749 次 | 14× |
| `[x for x in xs if x % 3 == 0]` | `filter(xs, # % 3 == 0)` | 1.6 µs | 1 次 | 18.4 µs | 670 次 | 11× |
| `[f * 2.0 for f in fs]` | `map(fs, # * 2.0)` | 1.2 µs | 1 次 | 11.0 µs | 1002 次 | 9.0× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | `sum(filter(xs, # % 3 == 0), # * 2)` | 1.9 µs | 0 次 | 20.3 µs | 960 次 | 11× |
| `reduce(x in xs, total = 0, total + x)` | `reduce(xs, #acc + #, 0)` | 1.0 µs | 0 次 | 14.6 µs | 1000 次 | 14× |
| `len([x for x in xs if x % 2 == 0])` | `count(xs, # % 2 == 0)` | 1.5 µs | 0 次 | 13.2 µs | 501 次 | 8.7× |
| `any([x > 1000 for x in xs])` | `any(xs, # > 1000)` | 2.4 µs | 0 次 | 10.0 µs | 501 次 | 4.1× |
| `all([x >= 0 for x in xs])` | `all(xs, # >= 0)` | 2.5 µs | 0 次 | 10.0 µs | 501 次 | 4.1× |
| `!any([x < 0 for x in xs])` | `none(xs, # < 0)` | 2.5 µs | 0 次 | 10.9 µs | 501 次 | 4.4× |
| `len([x for x in xs if x == 250]) == 1` | `one(xs, # == 250)` | 982 ns | 0 次 | 10.0 µs | 501 次 | 10× |
| `first([x for x in xs if x > 400])` | `find(xs, # > 400)` | 96 ns | 0 次 | 97 ns | 4 次 | 1.0× |
| `first([x for x in xs if x == 499])` | `find(xs, # == 499)` | 972 ns | 0 次 | 6.8 µs | 324 次 | 7.0× |
| `[y + z for y in ys for z in zs]` | `flatten(map(ys, let y = #; map(zs, y + #)))` | 2.7 µs | 2 次 | 21.4 µs | 650 次 | 7.8× |
| `{string(x): x for x in xs}` | `fromPairs(map(xs, [string(#), #]))` | 17.4 µs | 404 次 | 82.2 µs | 3012 次 | 4.7× |
| `group_by(xs, [string(x % 3) for x in xs])` | `groupBy(xs, string(# % 3))` | 41.9 µs | 52 次 | 43.6 µs | 1530 次 | 1.0× |
| `[upper(n) for n in names]` | `map(names, upper(#))` | 3.3 µs | 101 次 | 4.4 µs | 303 次 | 1.3× |

### 数组与字典函数（500 个元素）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `sum(xs)` | `sum(xs)` | 335 ns | 1 次 | 12.4 µs | 1000 次 | 37× |
| `min(xs)` | `min(xs)` | 487 ns | 1 次 | 13.0 µs | 1002 次 | 27× |
| `max(xs)` | `max(xs)` | 457 ns | 1 次 | 13.0 µs | 1002 次 | 28× |
| `avg(xs)` | `mean(xs)` | 379 ns | 1 次 | 11.0 µs | 1003 次 | 29× |
| `median(fs)` | `median(fs)` | 4.8 µs | 2 次 | 4.9 µs | 4 次 | 1.0× |
| `sum(fs)` | `sum(fs)` | 379 ns | 1 次 | 9.9 µs | 999 次 | 26× |
| `sort(xs)` | `sort(ints)` | 4.0 µs | 3 次 | 20.8 µs | 249 次 | 5.2× |
| `sort_desc(xs)` | `sort(ints, "desc")` | 4.1 µs | 3 次 | 20.5 µs | 249 次 | 5.0× |
| `top_k(xs, xs, 5)` | `take(sort(ints, "desc"), 5)` | 22.6 µs | 5 次 | 20.5 µs | 250 次 | 0.9× |
| `reverse(xs)` | `reverse(xs)` | 650 ns | 3 次 | 5.7 µs | 504 次 | 8.7× |
| `unique(concat(xs, xs))` | `uniq(concat(xs, xs))` | 30.3 µs | 20 次 | 1.2 ms | 1025 次 | 39× |
| `take(xs, 10)` | `take(xs, 10)` | 136 ns | 2 次 | 73 ns | 3 次 | 0.5× |
| `first(xs) + last(xs)` | `first(xs) + last(xs)` | 139 ns | 1 次 | 110 ns | 4 次 | 0.8× |
| `concat(ys, zs)` | `concat(ys, zs)` | 1.3 µs | 8 次 | 880 ns | 55 次 | 0.7× |
| `flatten([ys, zs])` | `flatten([ys, zs])` | 1.3 µs | 10 次 | 1.1 µs | 60 次 | 0.9× |
| `range(len(ys))` | `0..len(ys)-1` | 167 ns | 2 次 | 91 ns | 3 次 | 0.5× |
| `len(xs)` | `len(xs)` | 51 ns | 0 次 | 44 ns | 2 次 | 0.9× |
| `xs[250]` | `xs[250]` | 51 ns | 0 次 | 56 ns | 2 次 | 1.1× |
| `250 in xs` | `250 in xs` | 143 ns | 1 次 | 2.8 µs | 252 次 | 20× |
| `index_of(xs, 250)` | `findIndex(xs, # == 250)` | 197 ns | 1 次 | 5.1 µs | 252 次 | 26× |
| `arg_max(xs)` | `reduce(xs, # > xs[#acc] ? #index : #acc, 0)` | 453 ns | 1 次 | 25.5 µs | 1002 次 | 56× |
| `intersect(xs, ys)` | `filter(xs, # in ys)` | 4.7 µs | 12 次 | 129.3 µs | 10333 次 | 28× |
| `except(ys, zs)` | `filter(ys, not (# in zs))` | 820 ns | 11 次 | 5.4 µs | 426 次 | 6.6× |
| `join(names, ",")` | `join(names, ",")` | 667 ns | 2 次 | 641 ns | 4 次 | 1.0× |
| `d["k042"]` | `d["k042"]` | 74 ns | 0 次 | 62 ns | 2 次 | 0.8× |
| `"k042" in d` | `"k042" in d` | 69 ns | 0 次 | 58 ns | 2 次 | 0.8× |
| `get(d, "zzz", 0)` | `d["zzz"] ?? 0` | 86 ns | 0 次 | 57 ns | 1 次 | 0.7× |
| `len(d)` | `len(d)` | 50 ns | 0 次 | 40 ns | 1 次 | 0.8× |
| `sort([k for k, v in d])` | `sort(keys(d))` | 5.9 µs | 5 次 | 6.9 µs | 108 次 | 1.2× |
| `sum([v for k, v in d])` | `sum(values(d))` | 4.9 µs | 1 次 | 5.6 µs | 300 次 | 1.2× |

### 记录（1 个订单、50 个渠道）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `order.amount` | `order.amount` | 33 ns | 0 次 | 27 ns | 1 次 | 0.8× |
| `order.amount * 2 - order.fee` | `order.amount * 2 - order.fee` | 40 ns | 0 次 | 54 ns | 3 次 | 1.4× |
| `{net: order.amount - order.fee, currency: order.currency}` | `{net: order.amount - order.fee, currency: order.currency}` | 77 ns | 0 次 | 130 ns | 4 次 | 1.7× |
| `order with {fee: 0}` | `{amount: order.amount, currency: order.currency, fee: 0}` | 194 ns | 2 次 | 129 ns | 3 次 | 0.7× |
| `[c.name for c in channels if c.healthy]` | `map(filter(channels, .healthy), .name)` | 1.3 µs | 1 次 | 2.8 µs | 86 次 | 2.2× |
| `sum([c.fee for c in channels])` | `sum(channels, .fee)` | 803 ns | 0 次 | 1.9 µs | 96 次 | 2.4× |
| `len([c for c in channels if c.healthy && c.fee < 50])` | `count(channels, .healthy && .fee < 50)` | 1.3 µs | 0 次 | 3.2 µs | 84 次 | 2.4× |
| `sort_by(channels, [c.fee for c in channels])` | `sortBy(channels, .fee)` | 9.8 µs | 13 次 | 3.4 µs | 105 次 | 0.3× |
| `channels[arg_min([c.fee for c in channels])].name` | `find(channels, .fee == min(map(channels, .fee))).name` | 5.0 µs | 6 次 | 1.9 µs | 56 次 | 0.4× |

### 宿主边界：宽 struct 与长向量

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 35 ns | 0 次 | 51 ns | 1 次 | 1.5× |
| `order.amount` | `order.amount` | 37 ns | 0 次 | 40 ns | 1 次 | 1.1× |
| `len(xs)` | `len(xs)` | 53 ns | 0 次 | 45 ns | 2 次 | 0.9× |
| `len(fs)` | `len(fs)` | 47 ns | 0 次 | 29 ns | 2 次 | 0.6× |
| `host.total_v1(fs)` | `hostTotal(fs)` | 41.3 µs | 1 次 | 39.7 µs | 3 次 | 1.0× |
| `sum(fs)` | `sum(fs)` | 37.6 µs | 1 次 | 1.3 ms | 130416 次 | 35× |

### 编译

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 10.4 µs | 179 次 | 5.3 µs | 85 次 | 0.5× |
| `switch(case a > 10 => "big", case a > 5 => "mid", else => "small")` | `a > 10 ? "big" : a > 5 ? "mid" : "small"` | 25.8 µs | 424 次 | 7.4 µs | 127 次 | 0.3× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | `sum(filter(xs, # % 3 == 0), # * 2)` | 35.3 µs | 575 次 | 8.5 µs | 134 次 | 0.2× |
| `[c.name for c in channels if c.healthy]` | `map(filter(channels, .healthy), .name)` | 20.7 µs | 404 次 | 8.2 µs | 149 次 | 0.4× |

### 并行：同一条规则在全部 10 个核上同时跑

`a * b - a % b + 1`，每个 goroutine 从自己的宿主 struct 读参数。数字是每次运行的时间（全部核的总耗时除以总次数），越小越好；上表 expr 用的复用 `vm.VM` 不能并发，这里是它能并发的三种用法。

| 用法 | 1 核 | 10 核 | 分配 |
|---|---|---|---|
| FunRoute `Program.Run` | 42 ns | 7.52 ns | 0 次 |
| expr：每个 goroutine 一个 `vm.VM` | 75 ns | 22 ns | 1 次 |
| expr：`sync.Pool` 管理 `vm.VM` | 80 ns | 28 ns | 1 次 |
| expr：`expr.Run`（每次新建 `VM`） | 168 ns | 49 ns | 3 次 |

写法的差别：expr 的 `/` 总是浮点除法，整数除法写成 `int(a / b)`；expr 的 `sort` 不接受 `[]int64`，排序的几行 expr 读同样内容的 `[]int`；FunRoute 的推导式在 expr 里是带谓词的 `map`/`filter`/`sum`/`count`/`any`，`switch` 是连写的 `?:`；expr 没有 `intersect`/`except`/`arg_max`/`top_k`/`with`/`index_of`，写成它能写的等价形式。

没有对照的：金额（`money`/`ratio`/`fxrate`、`round(…, @mode)`、`using` 与 `->`、`allocate`）、`fallback`、枚举与穷尽的 `switch`、句柄与模型批处理，expr 没有对应；`windows`、`chunk`、`deltas`、`cumsum`、`take_while`、`drop_while`、`stddev`、`percentile`、`rank`、`pad_left`/`pad_right`、`merge` 在 expr 里没有内置函数。
