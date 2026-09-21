# 改进计划与决策记录

本文记录 FunRoute 从"路由表达式"走向"受治理的金融决策语言"的阶段计划，以及每个阶段已经做出的取舍。原则只有一条，所有取舍都从它推出：**性能让位于适用性，正确性不让位于任何东西。**

定位：相对于通用表达式求值器（如 `expr`），FunRoute 的价值不在求值本身，而在它周围的治理层 —— 规则是带 digest 与冻结 ABI 的 Artifact、能力边界由注册表定义且默认关闭、契约归宿主并参与推导、成本有界且必终止、文本与画布无损互转、模型接入是一等公民。每一项改进都应在这几条之内。

## 已完成

- 容器零拷贝值层：原生 Go 切片/映射作 backing，`Value` 56 字节，边界零转换。
- 不透明句柄 `handle<name>`：引擎数据穿过表达式，只能传递，不能比较。
- 批处理第一级：字节码证明可提升的模型调用按批合并，惰性 `if` 不受影响。
- 语法节点单一权威：struct tag 驱动导入、导出、作用域、schema 与前端卡片。
- 契约归宿主；历史兼容层清除；变量名禁点（`.` 留给字段访问）。

## 阶段 0：地基收口

| 项 | 决策 | 取舍 |
|---|---|---|
| 反射注册 `Logic` / `Model`，删除 `Fn1/2/3`、`Model1/2` | 两个入口，名字同长；`FunctionSpec` 仍是手写逃生口 | 扩展调用每次约 +200 ns、几次分配；内核函数不走反射，纯表达式基准不变。换来任意元数、任意嵌套、Go 常见标量类型 |
| `ctx` 贯通：`Run(ctx, …)`、`Batch.Run(ctx, …)`，扩展函数可选首参数 `context.Context`，`Doc.Timeout` 为函数级上限 | 实际 deadline = min(请求剩余预算, 函数上限)；VM 只在扩展调用前检查 | API 破坏性改动，趁未发布做 |
| 超时执行方式 | **默认信任 ctx；注册时可标 `Detached`**，VM 放独立 goroutine 等，到点放弃 | `Detached` 每次约 1 µs，被放弃的调用继续占资源直到自行结束；只给无取消能力的引擎绑定用 |
| `fallback(expr, default)` 惰性形式 | 只接扩展错误与超时，**不接** fuel 耗尽与类型错误 | 规则不能吞掉自身 bug |
| 预算耗尽且无 `fallback` | 直接返回 `ErrDeadline`，宿主兜底 | 不加"规则级默认值"，避免同一件事两层机制 |
| 类型化错误 | `ErrCompile`、`ErrContract`、`ErrFuel`、`ErrDeadline`、`ErrExtension`，`errors.Is` 可分 | — |
| 枚举类型 + 返回类型穷尽检查 | 契约声明 `enum<channel>{…}`，`switch` 对枚举做穷尽检查 | 一个 kind、一种契约声明 |

## 阶段 1：结构与语言补齐

| 项 | 决策 | 取舍 |
|---|---|---|
| `decimal` 类型（定点 int64 系数 + 指数，中间运算 128 位） | **不带后缀的小数字面量默认 decimal**，`1.7f` 显式 float | 用户决策。写钱的人不会误用 float。代价：模型分数比较 `risk > 0.5` 需要字面量按上下文定型（见下），现有示例中的 float 字面量要复核 |
| 数值字面量定型 | 小数字面量是**未定型常量**：默认 decimal，与 float 上下文 unify 时成为 float（同 Go 的 untyped constant）；`1.7f` 强制 float、`1.7d` 强制 decimal | 这是让"默认 decimal"与模型分数共存的唯一不引入大量重载的方式；`implicitTypeScore` 与 `mixedPenalty` 相应调整 |
| record 类型 | struct 映射、字段访问 `r.field`、契约按字段声明 | 贯穿推导、ExprJSON、常量、前端的最大改动；路由、对账、模型多输出都依赖它 |
| 索引 `xs[i]`、`d[k]`，`%`，`in` | 内核函数 + 糖，不加节点 | — |
| `any` / `all` / `first` / `sum` | 脱糖到 `for` / `reduce` | 终止性不变 |
| 多层与字典推导、`else =>`、尾随逗号统一 | — | 完整性 |
| 错误带行列与节点路径 | 编译错误附 ExprJSON 路径，画布高亮 | — |

## 阶段 2A：路由包（先做）

`decision` 返回 record + 原因码、`reject(code)`、确定性 `split(key, weights)`、渠道快照筛选排序（`sort_by`、`top_k`）、级联重试输入标准与拒付码分类、时间窗标准库（`now` 与时区作参数）。规则集组合放宿主 `RuleSet`，不进语言。

## 阶段 2B：金融包

| 项 | 决策 |
|---|---|
| `money` | **币种进类型**：`money<USD>`，不同币种相加编译期报错；多币种用币种类型变量 `money<C>` 与显式 `fx(rate, m)` 换币 |
| 字面量 | `USD 1.70`（小数位由币种决定，`USD 1.234` 报错）、`2.9%`、`25bps`；不支持 `$` |
| 运算 | `money ± money` 同币种；`money × decimal/int → money` 默认银行家舍入并在目录写明，`round(m, mode)` 显式；`money × money` 报错；`allocate(m, weights)` 最大余数法不丢分 |
| 相等 | money、decimal 精确相等；float 相等给警告（需要警告通道） |
| `date` / `timestamp` | 时区与 `now` 作参数，保持纯函数 |

## 阶段 3：治理工具

规则自带用例（保存即跑）、`funroute check` 批量兼容性检查、Artifact 版本 diff、`ContractCompatible`、决策 trace、`Simulate` 回测、影子运行 `Compare`、`Strict` 模式。全部在 SDK 与 CLI，不碰语言。

## 阶段 4：模型接入深化

批处理第二级（按调用依赖分层合批），前提是先用真实引擎测出第一级的收益边界。

## 明确不做

张量类型与 float32 类型（句柄替代）、lambda、递归、语言内状态与循环、`$` 币种符号、`let … in` 语法、`[][]float64` 之上的专用嵌套类型（反射转换替代）。变参函数优先级最低，暂缓。
