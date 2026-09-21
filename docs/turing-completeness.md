# FunRoute 的图灵完备性

本文给出两条互补的结论，并说明它们为什么是同一个设计的两面：

- **定理 A**：启用 `recur` 形式的 FunRoute，在忽略资源界限的理想化语义下是图灵完备的。
- **定理 B**：不启用 `recur` 的 FunRoute 是强正规化的（strongly normalizing）——所有程序在有限步内终止，无需 fuel 兜底。

也就是说，**终止性是注册表的一个配置项**：`registry.EnableForm(lang.RecurForm)` 这一行，就是在“可判定终止”和“图灵完备”之间做选择。

---

## 1. 预备

### 1.1 语法

只取与证明相关的核心片段（完整语法见 `README.md`）：

```text
e ::= c                     字面量（int / float / string / bool）
    | x                     变量
    | f(e₁, …, eₙ)          函数调用（严格求值）
    | if(e₁, e₂, e₃)        条件（惰性）
    | recur(e₁, …, eₙ)      自递归
    | [e for x in e if e]   有限映射 / 筛选（列表推导）
    | reduce(e, x, a, e, e) 有限折叠
```

一个**程序** P 是一个表达式；它的自由变量按首次出现顺序构成参数表 x₁, …, xₙ。

### 1.2 值与环境

值域 V = ℤ ∪ ℝ ∪ Σ\* ∪ {true, false} ∪ Array(V) ∪ Dict(V)，所有值不可变。环境 ρ 是参数到值的有限映射。

### 1.3 操作语义

求值关系 `ρ ⊢P e ⇓ v`（在程序 P 中，环境 ρ 下表达式 e 求值为 v）。下标 P 是必要的：`recur` 引用整个程序。

```text
(LIT)   ρ ⊢P c ⇓ c
(VAR)   ρ ⊢P x ⇓ ρ(x)

(APP)   ρ ⊢P eᵢ ⇓ vᵢ  (i = 1..n)      ⟦f⟧(v₁,…,vₙ) = v
        ─────────────────────────────────────────────
        ρ ⊢P f(e₁,…,eₙ) ⇓ v

(IF-T)  ρ ⊢P e₁ ⇓ true    ρ ⊢P e₂ ⇓ v        (IF-F)  ρ ⊢P e₁ ⇓ false   ρ ⊢P e₃ ⇓ v
        ───────────────────────────────              ────────────────────────────────
        ρ ⊢P if(e₁,e₂,e₃) ⇓ v                        ρ ⊢P if(e₁,e₂,e₃) ⇓ v

(REC)   ρ ⊢P eᵢ ⇓ vᵢ  (i = 1..n)     [x₁↦v₁, …, xₙ↦vₙ] ⊢P P ⇓ v
        ─────────────────────────────────────────────────────────
        ρ ⊢P recur(e₁,…,eₙ) ⇓ v
```

`(IF-T)/(IF-F)` 的前提里只出现被选中的分支——这是惰性的形式化含义，也是递归能停下来的前提。

### 1.4 理想化语义 FunRoute<sup>∞</sup>

实现带有四个资源界限：`Fuel`、`MaxRecursion`、int64 溢出检查、`MaxInstructions`。**FunRoute<sup>∞</sup>** 指去掉前三者（fuel 无限、递归深度无限、整数为任意精度 ℤ）后的语义；`MaxInstructions` 是编译期界限，与程序规模有关而与输入无关，不影响可计算性讨论。第 4 节专门讨论这些界限。

---

## 2. 定理 A：图灵完备性

> **定理 A.** 对任意单带确定型图灵机 M，存在一个 FunRoute 程序 ⟦M⟧（只用到 `if`、`eq`、`add`、`recur` 与四个列表原语），使得 ⟦M⟧ 在 FunRoute<sup>∞</sup> 下忠实模拟 M。因此 FunRoute<sup>∞</sup> 图灵完备。

证明采用**构造性模拟**：给出编译函数 ⟦·⟧，并证明它保持配置转移关系。

### 2.1 图灵机

M = (Q, Γ, δ, q₀, q_h)，其中 Γ = {0, 1}（0 为空白），

```text
δ : (Q \ {q_h}) × Γ → Γ × {L, R} × Q
```

**配置** c = (q, l, r)：q ∈ Q 是状态；r ∈ Γ\* 是磁头及其右侧的内容（磁头在 r 的首位）；l ∈ Γ\* 是磁头左侧的内容，**逆序**存放（离磁头最近的在首位）。两侧未写过的部分视为空白 0，因此有限串足以表示无限带。

当前格 `cur(c)` 定义为：r 非空时 r₀，否则 0。

**单步关系** c ⊢M c′：设 δ(q, cur(c)) = (w, d, q′)，记 r′ = tail(r)（r 为空时取 ε），则

```text
d = R :  c ⊢M (q′, w·l, r′)
d = L :  c ⊢M (q′, tail(l), head(l)·w·r′)     其中 l 为空时 head(l) = 0
```

### 2.2 配置的编码

状态编码为整数 ⌈q⌉ ∈ ℕ，取 ⌈q_h⌉ = h。配置 c = (q, l, r) 编码为环境

```text
⌈c⌉ = [ state ↦ ⌈q⌉,  right ↦ r,  left ↦ l,  steps ↦ n ]
```

其中 r、l 作为 `array<int>`，n 为已执行步数。参数顺序取 (state, right, left, steps)，与自由变量首次出现顺序一致。

### 2.3 编译函数

定义三个辅助表达式（它们只读环境，不含 `recur`）：

```text
CUR    ≜ if(array.is_empty(right), 0, array.head(right))
TAIL_R ≜ if(array.is_empty(right), right, array.tail(right))
HEAD_L ≜ if(array.is_empty(left),  0, array.head(left))
TAIL_L ≜ if(array.is_empty(left),  left, array.tail(left))
```

对每条转移 δ(q, s) = (w, d, q′) 定义**步表达式**

```text
STEP(w, R, q′) ≜ recur(⌈q′⌉, TAIL_R, array.prepend(w, left), add(steps, 1))
STEP(w, L, q′) ≜ recur(⌈q′⌉, array.prepend(HEAD_L, array.prepend(w, TAIL_R)), TAIL_L, add(steps, 1))
```

对每个非停机状态 q 定义**状态分派**

```text
DISP(q) ≜ if(eq(CUR, 0), STEP(δ(q,0)), STEP(δ(q,1)))
```

最后，设 Q \ {q_h} = {q₀, …, q_{k-1}}，

```text
⟦M⟧ ≜ if(eq(state, h), steps,
         if(eq(state, ⌈q₀⌉), DISP(q₀),
            … if(eq(state, ⌈q_{k-2}⌉), DISP(q_{k-2}), DISP(q_{k-1})) … ))
```

⟦M⟧ 的规模是 O(|Q|·|Γ|)，构造是纯机械的：**对任意转移表都按同一模板展开**，这正是“能模拟任意图灵机”的含义。

### 2.4 引理

> **引理 1（读格正确）.** 对任意配置 c，`⌈c⌉ ⊢ CUR ⇓ cur(c)`。

*证明.* 对 r 是否为空分情况，直接应用 (IF-T)/(IF-F) 与 `array.is_empty`、`array.head` 的语义。∎

> **引理 2（单步模拟）.** 设 q ≠ q_h 且 c ⊢M c′。则对任意值 v，
> `⌈c⌉ ⊢⟦M⟧ ⟦M⟧ ⇓ v` 当且仅当 `⌈c′⌉ ⊢⟦M⟧ ⟦M⟧ ⇓ v`。

*证明.* 因 q ≠ q_h，最外层 `eq(state, h)` 为 false，由 (IF-F) 进入分派链；`eq(state, ⌈q⌉)` 在第 ⌈q⌉ 层为 true，由 (IF-T) 选中 DISP(q)。由引理 1，`eq(CUR, 0)` 判定出当前符号 s，于是选中 STEP(δ(q, s))。设 δ(q,s) = (w,d,q′)。

按 (REC)，先求值 STEP 的四个实参：由 `array.*` 的语义，它们分别求值为 ⌈q′⌉、c′ 的 right、c′ 的 left、n+1——这与 2.1 中 ⊢M 的定义逐项一致（d = R 与 d = L 两种情况分别核对）。于是 (REC) 的第二个前提恰为 `⌈c′⌉ ⊢ ⟦M⟧ ⇓ v`，两个方向的蕴含同时成立。∎

### 2.5 定理 A 的证明

*证明.* 设 M 从初始配置 c₀ 出发，经 N 步到达停机配置 c_N =(q_h, l, r)。对 N 归纳使用引理 2，得

```text
⌈c₀⌉ ⊢ ⟦M⟧ ⇓ v  ⟺  ⌈c_N⌉ ⊢ ⟦M⟧ ⇓ v
```

在 c_N 处 state = h，最外层 (IF-T) 直接给出 `⇓ N`。故 ⟦M⟧ 在输入 ⌈c₀⌉ 上求值为 M 的运行步数；若把停机分支换成 `left` 或 `right`，同理得到停机带内容。

反之，若 M 不停机，则不存在有限的求值推导树：每次应用 (REC) 都需要一个严格更小的子推导，而单步关系永不到达 q_h。因此 ⟦M⟧ 在 ⌈c₀⌉ 上发散。二者合起来即“忠实模拟”。

由于 M 任意，且 ⟦·⟧ 是能行的（一个有限的语法变换），FunRoute<sup>∞</sup> 可模拟任意图灵机，故图灵完备。∎

**注**：证明只用到 `if`、`eq`、`add`、`recur` 和 `array.is_empty/head/tail/prepend`。`switch`、`for`、`reduce` 与全部算术、转换函数都不是完备性所必需的——它们是人机工效，不是表达力。

### 2.6 实证

`BB(3)` 忙碌海狸（3 状态 2 符号）按上述模板编译后：

| 项 | 值 |
|---|---|
| 生成的表达式 | 987 字节 |
| 编译后指令数 | 191 |
| 编译耗时 | ~2 ms |
| 运行结果（步数） | 14 |
| 运行结果（磁带 1 的个数） | 6 |
| 独立 Python 模拟器 | 14 步 / 6 个 1 |

两者一致，且与 BB(3) 的已知值吻合。清点磁带这一步复用了同一个 `recur`：停机时切换到一个计数状态，继续用同一循环走完两侧带——这也说明**多个“函数”可以编码进单一 `recur`**（状态参数即程序计数器），这一点在 2.5 之外额外回答了“没有互递归怎么办”。

不停机的机器（单状态右移写 1）在 5000 fuel 下于 4ms 被拦截，报 `execution fuel exhausted`。

---

## 3. 定理 B：不启用 recur 时强正规化

> **定理 B.** 若注册表未启用 `RecurForm`，且所有注册的扩展函数都终止，则任何通过类型检查的 FunRoute 程序在任意输入上都终止。

*证明.* 无 `recur` 时，语义规则中不再有 (REC)，即**没有任何规则以整个程序 P 作为前提**。定义度量

```text
μ(e) = 表达式 e 的语法树节点数
```

考察每条规则的前提相对结论的度量：

- (LIT)、(VAR)：无前提。
- (APP)：每个前提 eᵢ 是 e 的真子树，μ(eᵢ) < μ(f(e₁,…,eₙ))；⟦f⟧ 的求值按假设终止。
- (IF-T)/(IF-F)：前提 e₁ 与被选分支都是真子树。
- `for`/`reduce`：source 是真子树；body 被求值的次数等于 source 求值所得数组的长度 m（一个有限值），每次求值的 body 也是真子树。

于是求值推导树满足：每个分支上 μ 严格递减，且每个节点的子节点数有限（(APP) 为 n，循环形式为 m+1）。由 König 引理，一棵每节点有限分叉且每条路径有限的树是有限树，故求值在有限步内结束。∎

**推论（可判定性的分界）.** 启用 `recur` 后，“某个 FunRoute 程序是否终止”与图灵机停机问题等价，因而不可判定——所以 fuel 不是可以靠静态分析消除的保守措施，而是**必需**的。未启用 `recur` 时终止性由定理 B 结构性保证，fuel 退化为纯粹的成本上限。

这正是把 `recur` 做成注册表开关、而不是语言内建关键字的理由：运营控制台拿到的是一门**总是停机**的语言，工程师控制台拿到的是一门图灵完备的语言，二者共用同一套语义、同一个编译器和同一个 VM。

---

## 4. 实现的资源界限

理想化语义与真实实现的差距：

| 界限 | 实现 | 影响 |
|---|---|---|
| `Fuel` | 每条指令扣 1，扩展调用另扣其 `Cost` | 总步数有上界，程序**一定**停机 |
| `MaxRecursion` | 非尾位置的 `recur` 计入深度 | 嵌套激活有上界 |
| 尾调用 | 尾位置的 `recur` 复用帧，不增加深度 | 尾递归是循环，只受 fuel 约束 |
| int64 | 溢出报错而非回绕 | 计数器有上界，不会静默出错 |
| `MaxInstructions` | 编译期上界（默认 10 000） | 限制程序规模，与输入无关 |

因此：**实现上的 FunRoute 不是图灵机，而是一台带显式资源预算的机器**——每个程序都停机，停机与否可判定。这不是缺陷：支付路由要的正是“可解释、可计费、有上界”的计算。定理 A 说明表达力不是瓶颈；定理 B 与 fuel 说明安全边界是可以按控制台选择的。

从可计算性的角度说：固定 fuel 上限的 FunRoute 只能表达**有界时间可计算**的函数族；抹去 fuel 才落在图灵完备一侧。这与 C、Java 等在有限内存机器上的处境完全一致，惯例上仍称语言本身图灵完备。

---

## 5. 复现

编译器是一个约 40 行的机械变换，核心是 2.3 的三条定义：

```python
CUR    = "if(array.is_empty(right),0,array.head(right))"
TAIL_R = "if(array.is_empty(right),right,array.tail(right))"
HEAD_L = "if(array.is_empty(left),0,array.head(left))"
TAIL_L = "if(array.is_empty(left),left,array.tail(left))"

def step(write, move, nxt):                       # STEP(w, d, q′)
    if move == "R":
        new_left, new_right = f"array.prepend({write},left)", TAIL_R
    else:
        new_left = TAIL_L
        new_right = f"array.prepend({HEAD_L},array.prepend({write},{TAIL_R}))"
    return f"recur({nxt},{new_right},{new_left},add(steps,1))"

def compile_machine(table, states):               # ⟦M⟧
    def disp(q):
        return f"if(eq({CUR},0),{step(*table[(q,0)])},{step(*table[(q,1)])})"
    body = disp(states - 1)
    for q in range(states - 2, -1, -1):
        body = f"if(eq(state,{q}),{disp(q)},{body})"
    return f"if(eq(state,3),steps,{body})"
```

运行时需要一个启用了 `recur` 并注册了列表原语的注册表：

```go
registry := lang.CoreRegistry()
registry.EnableForm(lang.RecurForm)
lang.RegisterArrayPrimitives(registry)

artifact, _ := lang.CompileExpr(program, registry, lang.CompileOptions{
    ArgTypes: map[string]lang.Type{
        "state": lang.IntType, "steps": lang.IntType,
        "left": lang.ArrayOf(lang.IntType), "right": lang.ArrayOf(lang.IntType),
    },
})
runtime, _ := lang.Instantiate(artifact, registry)
value, err := runtime.Run(
    map[string]any{"state": 0, "steps": 0, "left": []any{}, "right": []any{}},
    lang.RunOptions{Fuel: 10_000_000, MaxRecursion: 1_000_000},
)
```
