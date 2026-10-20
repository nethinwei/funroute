# 性能报告

由 `make perf` 生成（`go run ./tests/perf`，与 expr 的对照来自 `tests/perf/expr`），不要手改。数字随机器变化，取的是多次运行里最快的一次；能力的边界见 [limits.md](limits.md)。

- 日期：2026-10-20
- 机器：Apple M5，darwin/arm64
- Go：go1.26.0
- 提交：`b4ad326（工作区有未提交的改动）`

## 单次执行

"原生 Go"是同一件事直接用 Go 写：宿主惯常的写法，不查溢出、不过边界；换汇调用的就是规则里用的 `Currencies.Convert`。耗时比是 FunRoute 的耗时除以它的：大于 1 表示 FunRoute 更慢，小于 1 表示更快，全文同一口径。

| 场景 | FunRoute | 分配 | 原生 Go | 分配 | 耗时比 |
|---|---|---|---|---|---|
| `amount * bps / 10000 + fixed`：`RunValues` | 40 ns | 0 次 | 1.77 ns | 0 次 | 22× |
| 同上：`Run(map)` | 57 ns | 0 次 | 1.77 ns | 0 次 | 32× |
| 同上：`Program.Run`（从宿主 struct 读参数） | 20 ns | 0 次 | 1.77 ns | 0 次 | 11× |
| 一次内核函数调用 `a + b` | 35 ns | 0 次 | 1.64 ns | 0 次 | 21× |
| 一次按 Go 签名注册的函数调用（常见签名，不经反射） | 50 ns | 0 次 | 1.67 ns | 0 次 | 30× |
| `using` 里换汇一次 | 177 ns | 0 次 | 52 ns | 0 次 | 3.4× |
| 500 个元素的 `reduce` | 716 ns | 0 次 | 130 ns | 0 次 | 5.5× |
| 500 个元素的推导式 | 934 ns | 1 次 | 463 ns | 1 次 | 2.0× |
| 500 对元素的嵌套推导式（25 × 20） | 3.1 µs | 2 次 | 482 ns | 1 次 | 6.4× |
| 500 个渠道的费用之和 `sum([c.fee for c in channels])` | 3.2 µs | 0 次 | 126 ns | 0 次 | 26× |
| 500 个渠道按字段筛选 `[c for c in channels if c.ok && c.fee < 50]` | 6.0 µs | 4 次 | 1.8 µs | 8 次 | 3.3× |
| 500 个渠道的投影 `[{fee: c.fee} for c in channels]` | 23.6 µs | 16 次 | 475 ns | 1 次 | 50× |
| 500 个渠道逐个更新 `[c with {fee: 0} for c in channels]` | 41.7 µs | 16 次 | 1.5 µs | 1 次 | 28× |
| 500 个数逐个封顶 `[min(x, 10) for x in xs]` | 1.3 µs | 1 次 | 469 ns | 1 次 | 2.8× |
| 按下标遍历 500 个数 `sum([xs[i] for i in indices(xs)])` | 3.9 µs | 1 次 | 131 ns | 0 次 | 30× |
| 4096 个渠道按金额费用取最便宜的 10 个（`bottom_k`） | 216.4 µs | 5 次 | 158.9 µs | 2 次 | 1.4× |
| 4096 笔金额去重（`unique`） | 137.0 µs | 31 次 | 146.9 µs | 31 次 | 0.93× |
| 4096 个渠道 record 去重（`unique`） | 497.4 µs | 4137 次 | 267.0 µs | 32 次 | 1.9× |
| 4096 笔金额去掉前一半（`except`） | 135.1 µs | 40 次 | 204.9 µs | 38 次 | 0.66× |
| 4096 个渠道里第一个可用的（`first` 推导式，一半可用） | 116.0 µs | 0 次 | 3.65 ns | 0 次 | 31730× |
| 4096 笔费用每 10 笔一个窗口（`windows`） | 845.8 µs | 18 次 | 39.6 µs | 14 次 | 21× |
| 把 16 个 float 交给宿主函数（与长度无关：不拷贝） | 70 ns | 0 次 | 1.67 ns | 0 次 | 42× |
| 把 1024 个 float 交给宿主函数（与长度无关：不拷贝） | 69 ns | 0 次 | 1.67 ns | 0 次 | 42× |
| 把 65536 个 float 交给宿主函数（与长度无关：不拷贝） | 69 ns | 0 次 | 1.68 ns | 0 次 | 41× |
| 模型调用（引擎每次 20 µs），一条一条 | 26.2 µs | 0 次 | 26.0 µs | 0 次 | 1.0× |
| 同上，64 条一批，折合每条 | 593 ns | 2 次 | 410 ns | 0 次 | 1.4× |

## 大输入的吞吐

参数是固定种子打乱的 0…n−1，两边用同一份。"原生 Go"是同一件事的 Go 循环，在 100 万个元素上测：筛选求和不建中间数组；排序用 `slices.Sort`，std 的 `sort` 在没有 0 与 -0 这种相等却可区分的元素时也用它。

| 程序 | n = 1 万 | n = 10 万 | n = 100 万 | 每个元素 | 原生 Go 每个元素 | 耗时比 |
|---|---|---|---|---|---|---|
| `[x + 1 for x in xs]` | 12.0 µs | 101.8 µs | 955.4 µs | 1.0 ns | 0.29 ns | 3.3× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | 35.3 µs | 301.4 µs | 3.1 ms | 3.1 ns | 2.37 ns | 1.3× |
| `{string(x): x for x in xs}` | 349.0 µs | 4.3 ms | 101.5 ms | 101.5 ns | 76.43 ns | 1.3× |
| `sort(xs)` | 254.8 µs | 4.2 ms | 52.0 ms | 52.0 ns | 52.02 ns | 1× |
| `sum(xs)` | 3.1 µs | 29.3 µs | 298.3 µs | 0.3 ns | 0.25 ns | 1.2× |
| 1000 × 1000 的嵌套推导式 | — | — | 1.2 ms（100 万对） | 1.2 ns / 对 | 0.29 ns / 对 | 4.2× |

## 编译

`[a * 0 + b, a * 1 + b, …]`：很多重载调用共用同一对参数。

| 项数 | 源码 | 有契约 | 无契约（全部推导） |
|---|---|---|---|
| 100 | 1.2 KB | 541.7 µs | 1.4 ms |
| 500 | 6.2 KB | 2.8 ms | 12.8 ms |
| 1000 | 12.6 KB | 5.1 ms | 40.6 ms |
| 1900 | 24.9 KB | 9.9 ms | 128.8 ms |

`x0 + x1 + …`，变量互不相同，无契约：

| 项数 | 编译 |
|---|---|
| 10 | 57.2 µs |
| 100 | 583.9 µs |
| 1000 | 7.5 ms |

一条规则，有契约（深的两行守着编译与格式化随规模线性增长）：

| 规则 | 编译 | 格式化 |
|---|---|---|
| `a + b` | 7.7 µs | 772 ns |
| `switch(case a > 10 => "big", …)` | 16.4 µs | 3.2 µs |
| `sum([x * 2 for x in xs if x % 3 == 0])` | 19.8 µs | 3.5 µs |
| `let(k = 3, t = k * 4, [x * t + a for x in xs if x > k])` | 27.9 µs | 5.1 µs |
| 循环体里 800 项的加法链 | 1.9 ms | 1.1 ms |
| 400 层嵌套的调用 | 911.2 µs | 1.7 ms |

## 语言服务

| 请求 | 一条规则（59 字节） | 1000 项数组，无契约（13 KB） | 8000 个常量写在一行（46 KB） |
|---|---|---|---|
| 打开文档并发布诊断 | 35.1 µs | 39.3 ms | 87.3 ms |
| 悬停 | 4.5 µs | 14.6 µs | 16.7 µs |
| 补全 | 352.1 µs | 5.2 ms | 8.3 ms |
| 语义标记 | 4.5 µs | 280.3 µs | 528.3 µs |
| 格式化 | 8.0 µs | 2.1 ms | 3.8 ms |
| 语法树 | 17.8 µs | 3.6 ms | 3.7 ms |

## 产物

- 浏览器里的语言服务 `funroute.wasm`：9.60 MB（gzip 后 2.53 MB）
- 前端 JS：527 KB

## 与 expr 对照

由 `tests/perf/expr` 生成：它是单独的 module，只有它依赖 expr（v1.17.8），FunRoute 的 go.mod 仍然为空。两边从同一个宿主 struct 读参数，先编译好再反复执行，每边测 300ms。FunRoute 用 `Program.Run`（可并发调用）；expr 用复用的 `vm.VM`，这是它最快的用法，但一个 `VM` 不能并发；宿主函数在 expr 里经 `expr.Function` 注册。每一行都先经 JSON 核对两边答案相同。耗时比是 FunRoute 的耗时除以 expr 的：大于 1 表示 FunRoute 更慢，小于 1 表示更快，与上文对原生 Go 的同一口径。

### 标量：运算符、条件、绑定、数值函数、转换、宿主函数

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 耗时比 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 17 ns | 0 次 | 33 ns | 1 次 | 0.52× |
| `a * b - a % b + 1` | `a * b - a % b + 1` | 21 ns | 0 次 | 55 ns | 1 次 | 0.38× |
| `a / b` | `int(a / b)` | 18 ns | 0 次 | 37 ns | 2 次 | 0.49× |
| `-a + b` | `-a + b` | 17 ns | 0 次 | 39 ns | 3 次 | 0.45× |
| `x * y + x / y - 1.5` | `x * y + x / y - 1.5` | 26 ns | 0 次 | 68 ns | 5 次 | 0.38× |
| `float(a) / float(b)` | `a / b` | 23 ns | 0 次 | 33 ns | 2 次 | 0.69× |
| `a > b && x < y \|\| !flag` | `a > b && x < y \|\| !flag` | 23 ns | 0 次 | 47 ns | 1 次 | 0.49× |
| `a == 7 && b != 4` | `a == 7 && b != 4` | 22 ns | 0 次 | 40 ns | 1 次 | 0.56× |
| `if(a > b, a, b)` | `a > b ? a : b` | 19 ns | 0 次 | 36 ns | 1 次 | 0.51× |
| `switch(a, case 1 => "one", case 2, 3 => "few", else => "many")` | `a == 1 ? "one" : a in [2, 3] ? "few" : "many"` | 29 ns | 0 次 | 49 ns | 1 次 | 0.59× |
| `switch(case a > 10 => "big", case a > 5 => "mid", else => "small")` | `a > 10 ? "big" : a > 5 ? "mid" : "small"` | 24 ns | 0 次 | 40 ns | 1 次 | 0.59× |
| `let(s = a + b, d = a - b, s * d)` | `let s = a + b; let d = a - b; s * d` | 19 ns | 0 次 | 54 ns | 1 次 | 0.34× |
| `7 * 24 * 3600 + a` | `7 * 24 * 3600 + a` | 16 ns | 0 次 | 28 ns | 2 次 | 0.59× |
| `abs(b - a)` | `abs(b - a)` | 18 ns | 0 次 | 36 ns | 2 次 | 0.51× |
| `max(a, b)` | `max(a, b)` | 18 ns | 0 次 | 50 ns | 2 次 | 0.36× |
| `ceil(x)` | `ceil(x)` | 25 ns | 0 次 | 27 ns | 2 次 | 0.92× |
| `floor(x) + round(y)` | `floor(x) + round(y)` | 32 ns | 0 次 | 48 ns | 4 次 | 0.67× |
| `pow(x, 2.0)` | `x ** 2` | 29 ns | 0 次 | 34 ns | 2 次 | 0.85× |
| `int(y) + a` | `int(y) + a` | 19 ns | 0 次 | 32 ns | 1 次 | 0.61× |
| `float(a) * x` | `float(a) * x` | 21 ns | 0 次 | 40 ns | 3 次 | 0.51× |
| `string(a)` | `string(a)` | 32 ns | 0 次 | 49 ns | 2 次 | 0.66× |
| `host.add_v1(a, b)` | `hostAdd(a, b)` | 27 ns | 0 次 | 42 ns | 2 次 | 0.65× |
| `host.scale_v1(x, a)` | `hostScale(x, a)` | 30 ns | 0 次 | 51 ns | 3 次 | 0.58× |

### 字符串

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 耗时比 |
|---|---|---|---|---|---|---|
| `country == "MY"` | `country == "MY"` | 24 ns | 0 次 | 30 ns | 1 次 | 0.77× |
| `country in ["SG", "MY", "TH"]` | `country in ["SG", "MY", "TH"]` | 36 ns | 0 次 | 42 ns | 1 次 | 0.87× |
| `starts_with(card, "4111")` | `card startsWith "4111"` | 28 ns | 0 次 | 33 ns | 1 次 | 0.85× |
| `ends_with(s, "-sg")` | `s endsWith "-sg"` | 29 ns | 0 次 | 33 ns | 1 次 | 0.86× |
| `contains(s, "sg")` | `s contains "sg"` | 31 ns | 0 次 | 35 ns | 1 次 | 0.9× |
| `"sg" in s` | `s contains "sg"` | 31 ns | 0 次 | 34 ns | 1 次 | 0.9× |
| `upper(s)` | `upper(s)` | 54 ns | 1 次 | 62 ns | 3 次 | 0.88× |
| `lower(trim(name))` | `lower(trim(name))` | 58 ns | 1 次 | 88 ns | 5 次 | 0.66× |
| `replace(s, "-", "_")` | `replace(s, "-", "_")` | 51 ns | 1 次 | 83 ns | 4 次 | 0.62× |
| `split(csv, ",")` | `split(csv, ",")` | 105 ns | 1 次 | 100 ns | 4 次 | 1.1× |
| `join(split(csv, ","), "\|")` | `join(split(csv, ","), "\|")` | 125 ns | 2 次 | 149 ns | 6 次 | 0.84× |
| `s + ":" + country` | `s + ":" + country` | 52 ns | 2 次 | 84 ns | 5 次 | 0.62× |
| `len(s)` | `len(s)` | 29 ns | 0 次 | 32 ns | 1 次 | 0.91× |
| `slice(s, 0, 5)` | `s[0:5]` | 44 ns | 0 次 | 49 ns | 2 次 | 0.89× |
| `if(starts_with(card, "4"), "visa", "other")` | `card startsWith "4" ? "visa" : "other"` | 31 ns | 0 次 | 36 ns | 1 次 | 0.85× |
| `host.label_v1(country)` | `hostLabel(country)` | 47 ns | 1 次 | 64 ns | 4 次 | 0.74× |

### 推导式、聚合与 reduce（500 个元素）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 耗时比 |
|---|---|---|---|---|---|---|
| `[x + 1 for x in xs]` | `map(xs, # + 1)` | 966 ns | 1 次 | 13.2 µs | 749 次 | 0.073× |
| `[x for x in xs if x % 3 == 0]` | `filter(xs, # % 3 == 0)` | 1.6 µs | 1 次 | 15.4 µs | 670 次 | 0.1× |
| `[f * 2.0 for f in fs]` | `map(fs, # * 2.0)` | 1.1 µs | 1 次 | 10.8 µs | 1002 次 | 0.1× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | `sum(filter(xs, # % 3 == 0), # * 2)` | 1.8 µs | 0 次 | 19.7 µs | 960 次 | 0.092× |
| `reduce(x in xs, total = 0, total + x)` | `reduce(xs, #acc + #, 0)` | 583 ns | 0 次 | 14.1 µs | 1000 次 | 0.041× |
| `len([x for x in xs if x % 2 == 0])` | `count(xs, # % 2 == 0)` | 1.5 µs | 0 次 | 12.2 µs | 501 次 | 0.12× |
| `any([x > 1000 for x in xs])` | `any(xs, # > 1000)` | 2.3 µs | 0 次 | 9.6 µs | 501 次 | 0.24× |
| `all([x >= 0 for x in xs])` | `all(xs, # >= 0)` | 2.4 µs | 0 次 | 9.6 µs | 501 次 | 0.25× |
| `!any([x < 0 for x in xs])` | `none(xs, # < 0)` | 2.3 µs | 0 次 | 10.3 µs | 501 次 | 0.23× |
| `len([x for x in xs if x == 250]) == 1` | `one(xs, # == 250)` | 932 ns | 0 次 | 9.7 µs | 501 次 | 0.097× |
| `first([x for x in xs if x > 400])` | `find(xs, # > 400)` | 97 ns | 0 次 | 93 ns | 4 次 | 1.0× |
| `first([x for x in xs if x == 499])` | `find(xs, # == 499)` | 925 ns | 0 次 | 6.4 µs | 324 次 | 0.15× |
| `[y + z for y in ys for z in zs]` | `flatten(map(ys, let y = #; map(zs, y + #)))` | 2.7 µs | 2 次 | 20.5 µs | 650 次 | 0.13× |
| `{string(x): x for x in xs}` | `fromPairs(map(xs, [string(#), #]))` | 17.1 µs | 404 次 | 79.1 µs | 3012 次 | 0.22× |
| `group_by(xs, [string(x % 3) for x in xs])` | `groupBy(xs, string(# % 3))` | 41.2 µs | 50 次 | 41.4 µs | 1530 次 | 0.99× |
| `[upper(n) for n in names]` | `map(names, upper(#))` | 3.2 µs | 101 次 | 4.3 µs | 303 次 | 0.74× |

### 数组与字典函数（500 个元素）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 耗时比 |
|---|---|---|---|---|---|---|
| `sum(xs)` | `sum(xs)` | 202 ns | 0 次 | 11.9 µs | 1000 次 | 0.017× |
| `min(xs)` | `min(xs)` | 288 ns | 0 次 | 12.6 µs | 1002 次 | 0.023× |
| `max(xs)` | `max(xs)` | 417 ns | 0 次 | 12.8 µs | 1002 次 | 0.033× |
| `avg(xs)` | `mean(xs)` | 343 ns | 0 次 | 10.7 µs | 1003 次 | 0.032× |
| `median(fs)` | `median(fs)` | 4.5 µs | 1 次 | 4.5 µs | 4 次 | 1.0× |
| `sum(fs)` | `sum(fs)` | 341 ns | 0 次 | 9.5 µs | 999 次 | 0.036× |
| `sort(xs)` | `sort(ints)` | 4.0 µs | 1 次 | 18.2 µs | 249 次 | 0.22× |
| `sort_desc(xs)` | `sort(ints, "desc")` | 4.0 µs | 1 次 | 19.2 µs | 249 次 | 0.21× |
| `top_k(xs, xs, 5)` | `take(sort(ints, "desc"), 5)` | 767 ns | 2 次 | 19.5 µs | 250 次 | 0.039× |
| `reverse(xs)` | `reverse(xs)` | 829 ns | 1 次 | 5.5 µs | 504 次 | 0.15× |
| `unique(concat(xs, xs))` | `uniq(concat(xs, xs))` | 10.5 µs | 13 次 | 1.1 ms | 1025 次 | 0.0097× |
| `take(xs, 10)` | `take(xs, 10)` | 75 ns | 0 次 | 73 ns | 3 次 | 1.0× |
| `first(xs) + last(xs)` | `first(xs) + last(xs)` | 64 ns | 0 次 | 106 ns | 4 次 | 0.61× |
| `concat(ys, zs)` | `concat(ys, zs)` | 141 ns | 1 次 | 844 ns | 55 次 | 0.17× |
| `flatten([ys, zs])` | `flatten([ys, zs])` | 530 ns | 3 次 | 1.1 µs | 60 次 | 0.47× |
| `range(len(ys))` | `0..len(ys)-1` | 107 ns | 1 次 | 88 ns | 3 次 | 1.2× |
| `len(xs)` | `len(xs)` | 36 ns | 0 次 | 42 ns | 2 次 | 0.85× |
| `xs[250]` | `xs[250]` | 36 ns | 0 次 | 54 ns | 2 次 | 0.66× |
| `250 in xs` | `250 in xs` | 116 ns | 0 次 | 2.7 µs | 252 次 | 0.043× |
| `index_of(xs, 250)` | `findIndex(xs, # == 250)` | 116 ns | 0 次 | 5.0 µs | 252 次 | 0.023× |
| `arg_max(xs)` | `reduce(xs, # > xs[#acc] ? #index : #acc, 0)` | 412 ns | 0 次 | 24.2 µs | 1002 次 | 0.017× |
| `intersect(xs, ys)` | `filter(xs, # in ys)` | 4.4 µs | 10 次 | 120.1 µs | 10333 次 | 0.036× |
| `except(ys, zs)` | `filter(ys, not (# in zs))` | 704 ns | 9 次 | 5.1 µs | 426 次 | 0.14× |
| `join(names, ",")` | `join(names, ",")` | 630 ns | 1 次 | 632 ns | 4 次 | 1× |
| `d["k042"]` | `d["k042"]` | 50 ns | 0 次 | 61 ns | 2 次 | 0.82× |
| `"k042" in d` | `"k042" in d` | 51 ns | 0 次 | 57 ns | 2 次 | 0.9× |
| `get(d, "zzz", 0)` | `d["zzz"] ?? 0` | 57 ns | 0 次 | 56 ns | 1 次 | 1.0× |
| `len(d)` | `len(d)` | 39 ns | 0 次 | 40 ns | 1 次 | 0.97× |
| `sort([k for k, v in d])` | `sort(keys(d))` | 5.8 µs | 3 次 | 6.7 µs | 108 次 | 0.87× |
| `sum([v for k, v in d])` | `sum(values(d))` | 4.6 µs | 1 次 | 5.2 µs | 300 次 | 0.89× |

### 记录（1 个订单、50 个渠道）

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 耗时比 |
|---|---|---|---|---|---|---|
| `order.amount` | `order.amount` | 20 ns | 0 次 | 26 ns | 1 次 | 0.77× |
| `order.amount * 2 - order.fee` | `order.amount * 2 - order.fee` | 23 ns | 0 次 | 50 ns | 3 次 | 0.46× |
| `{net: order.amount - order.fee, currency: order.currency}` | `{net: order.amount - order.fee, currency: order.currency}` | 76 ns | 0 次 | 124 ns | 4 次 | 0.61× |
| `order with {fee: 0}` | `{amount: order.amount, currency: order.currency, fee: 0}` | 112 ns | 0 次 | 116 ns | 3 次 | 0.97× |
| `[c.name for c in channels if c.healthy]` | `map(filter(channels, .healthy), .name)` | 725 ns | 1 次 | 2.7 µs | 86 次 | 0.26× |
| `sum([c.fee for c in channels])` | `sum(channels, .fee)` | 430 ns | 0 次 | 1.8 µs | 96 次 | 0.24× |
| `len([c for c in channels if c.healthy && c.fee < 50])` | `count(channels, .healthy && .fee < 50)` | 636 ns | 0 次 | 2.9 µs | 84 次 | 0.22× |
| `sort_by(channels, .fee)` | `sortBy(channels, .fee)` | 1.8 µs | 7 次 | 3.2 µs | 105 次 | 0.56× |
| `min_by(channels, .fee).name` | `find(channels, .fee == min(map(channels, .fee))).name` | 571 ns | 3 次 | 1.7 µs | 56 次 | 0.33× |

### 宿主边界：宽 struct 与长向量

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 耗时比 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 17 ns | 0 次 | 47 ns | 1 次 | 0.36× |
| `order.amount` | `order.amount` | 19 ns | 0 次 | 39 ns | 1 次 | 0.49× |
| `len(xs)` | `len(xs)` | 36 ns | 0 次 | 44 ns | 2 次 | 0.81× |
| `len(fs)` | `len(fs)` | 32 ns | 0 次 | 28 ns | 2 次 | 1.1× |
| `host.total_v1(fs)` | `hostTotal(fs)` | 37.9 µs | 0 次 | 38.0 µs | 3 次 | 1× |
| `sum(fs)` | `sum(fs)` | 36.2 µs | 0 次 | 1.3 ms | 130416 次 | 0.029× |

### 编译

| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 耗时比 |
|---|---|---|---|---|---|---|
| `a + b` | `a + b` | 5.3 µs | 90 次 | 5.2 µs | 85 次 | 1.0× |
| `switch(case a > 10 => "big", case a > 5 => "mid", else => "small")` | `a > 10 ? "big" : a > 5 ? "mid" : "small"` | 11.1 µs | 157 次 | 6.9 µs | 127 次 | 1.6× |
| `sum([x * 2 for x in xs if x % 3 == 0])` | `sum(filter(xs, # % 3 == 0), # * 2)` | 16.0 µs | 260 次 | 8.2 µs | 134 次 | 2.0× |
| `[c.name for c in channels if c.healthy]` | `map(filter(channels, .healthy), .name)` | 10.6 µs | 208 次 | 8.1 µs | 149 次 | 1.3× |

### 并行：同一条规则在全部 10 个核上同时跑

`a * b - a % b + 1`，每个 goroutine 从自己的宿主 struct 读参数。数字是每次运行的时间（全部核的总耗时除以总次数），越小越好；上表 expr 用的复用 `vm.VM` 不能并发，这里是它能并发的三种用法。

| 用法 | 1 核 | 10 核 | 分配 |
|---|---|---|---|
| FunRoute `Program.Run` | 20 ns | 4.12 ns | 0 次 |
| expr：每个 goroutine 一个 `vm.VM` | 74 ns | 22 ns | 1 次 |
| expr：`sync.Pool` 管理 `vm.VM` | 80 ns | 25 ns | 1 次 |
| expr：`expr.Run`（每次新建 `VM`） | 111 ns | 47 ns | 3 次 |

写法的差别：expr 的 `/` 总是浮点除法，整数除法写成 `int(a / b)`；expr 的 `sort` 不接受 `[]int64`，排序的几行 expr 读同样内容的 `[]int`；FunRoute 的推导式在 expr 里是带谓词的 `map`/`filter`/`sum`/`count`/`any`，`switch` 是连写的 `?:`；expr 没有 `intersect`/`except`/`arg_max`/`top_k`/`with`/`index_of`，写成它能写的等价形式。

没有对照的：金额（`money`/`ratio`/`fxrate`、`round(…, @mode)`、`using` 与 `->`、`allocate`）、`fallback`、枚举与穷尽的 `switch`、句柄与模型批处理，expr 没有对应；`windows`、`chunk`、`deltas`、`cumsum`、`take_while`、`drop_while`、`stddev`、`percentile`、`rank`、`pad_left`/`pad_right`、`merge` 在 expr 里没有内置函数。
