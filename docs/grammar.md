# 语法与取值范围

解析器的完整规格与每种类型的取值范围。日常写规则只需要 [language.md](language.md)。

## 语法参考

下面的文法与词法规则是解析器的完整规格；日常写规则只需要前面的[语言导览](language.md)。

```text
// ── 词法 ─────────────────────────────────────────────────────────────
// token 之间可以有空白（ASCII 空白字符）和注释（// 到行尾）；token 内部不能有注释，也不能有写出来之外的空白。
// 词法器从左到右每次取尽量长的一段，认成下面的一种 token。一段文字合乎几种写法时按这个次序取：
// 保留字，然后形如币种代码的 code，然后 name；数字后面紧贴 % 或 bps 就是 ratio，不是 number。
digits     = digit { [ "_" ] digit }                // 下划线只能夹在两个数字之间：1_000
decimal    = digits [ "." digits ]                  // 数字后的 "." 必须接数字
exponent   = ( "e" | "E" ) [ "+" | "-" ] digits
number     = decimal [ exponent ]                   // 没有 "." 也没有指数是 int，否则是 float；float 必须恰好是写下的值（0.30000000000000001 报错）
duration   = digits unit { digits unit }            // 90s、2h30m、1_500ms；unit 取最长的一个（1ms 是毫秒），最后一个单位后不能紧接字母、数字或点
unit       = "h" | "m" | "s" | "ms" | "us" | "ns"
ratio      = decimal ( "%" | "bps" )                // 紧贴数字的 % 一律是比例的单位，带指数的数也一样（1e3% 报错）；bps 后不能紧接字母或数字
word       = ( letter | "_" ) { letter | digit | "_" }
code       = upper 2*7( upper | digit )             // 大写字母开头、共 3–8 位：币种，不是变量
name       = word { "." word }                      // 名字连同其中的点是一个 token，含义见下文
enum       = "@" word [ "." word ]                  // @member 或 @enum.member
string     = '"' { 字符 | 转义 } '"'                // 转义同 Go，内容必须是合法 UTF-8
保留字     = true false switch reduce let for in else case using with

// ── 解析器查原文的字面量 ───────────────────────────────────────────────
// 它们由几个 token 组成，解析器检查 token 之间的原文：只能是空格与 Tab，不能换行、不能有注释。
hspace     = ( " " | "\t" ) { " " | "\t" }
money      = code hspace [ "-" ] decimal            // USD 1.70、USD -1.70：负号属于数，紧贴数字
fxrate     = decimal hspace code [ hspace ] "/" [ hspace ] code   // 150 JPY / USD：1 USD 换 150 JPY
// 负数字面量也是解析器的规则：一元 "-" 后是单独一个 number 时读成负数，所以 -9223372036854775808
// 写得出来；-150 JPY / USD、-2[0] 仍是对后面的整个 postfix 取负。1-2 是减法：词法器从不把负号读进数字。

// ── 文法 ─────────────────────────────────────────────────────────────
// 每一层只引用更紧的下一层，所以优先级与结合都由文法本身决定。
program        = expression EOF                     // 契约由宿主给出，不在文本里
expression     = or
or             = and { "||" and }
and            = equality { "&&" equality }
equality       = comparison [ ( "==" | "!=" ) comparison ]                        // 比较不结合：a == b == c 是语法错误
comparison     = conversion [ ( "<" | "<=" | ">" | ">=" | "in" ) conversion ]     // a < b < c 同样是语法错误
conversion     = additive { "->" postfix }          // 左结合；右边是一个币种：代码、名字、字段、调用或括号。amount -> JPY + fee 是语法错误
additive       = multiplicative { ( "+" | "-" ) multiplicative }
multiplicative = unary { ( "*" | "/" | "%" ) unary }
unary          = ( "!" | "-" ) unary | postfix      // -USD 1.70 是对金额取负
postfix        = primary { "[" expression "]" | "." word | "with" "{" word ":" expression { "," word ":" expression } [ "," ] "}" }  // 字段更新：order with {fee: 0}
primary        = number | ratio | money | fxrate | code | string | "true" | "false" | enum
               | name                               // 变量；第一个点之后是字段读取：order.amount
               | name "(" [ args ] ")"              // 函数调用，名字可带点：route.score_v1(x)
               | "switch" "(" [ expression "," ] case { "," case } ( [ "," ] ")" | "," "else" "=>" expression ")" )
               | "reduce" "(" locals "in" expression [ "if" expression ] "," word "=" expression "," expression ")"
               | "let" "(" binding { binding } expression ")"
               | "using" "(" quote { "," quote } "," expression ")"      // 至少一个报价，最后是主体
               | "[" [ args ] "]"                   // 数组
               | "[" expression loop { loop } "]"   // 列表推导
               | "{" [ string ":" expression { "," string ":" expression } [ "," ] ] "}"      // 字典
               | "{" word ":" expression { "," word ":" expression } [ "," ] "}"              // 记录
               | "{" expression ":" expression loop "}"   // 字典推导，只接一个 loop
               | "(" expression ")"
               | "." word                           // 选择器，只作调用第一个实参之后的实参：sort_by(xs, .fee)；word 里的点是嵌套字段
case           = "case" expression { "," expression } "=>" expression
binding        = word "=" expression ","
loop           = "for" locals "in" expression [ "if" expression ]
locals         = word [ "," word ]
args           = expression { "," expression } [ "," ]
quote          = expression                         // 一个 fxrate 或 array<fxrate>；外层的汇率用 fx(base, quote) 读进来
```

几条容易踩到的规则：

- **金额的负号写在数上**：`USD -1.70`（与宿主文本、`EncodeJSON` 的输出同一种写法）。`-USD 1.70` 是对金额取负，和 `-x` 一样；`USD - 1`、`USD-1` 是币种减 1，类型错误。
- **代码形状的名字永远是币种**：`USD`、`HTTP`、`ORDER1` 都不能当变量。`USD.x` 是错误（币种没有字段），`USD(1)` 是名叫 `USD` 的函数调用。
- **名字与点**：`order.amount` 连同其中的点是一个名字。后面紧跟 `(` 时整个名字是函数名（`route.score_v1(x)`，语言没有一等函数，`route` 不是变量）；否则第一段是变量，其后每一段是字段读取。变量名本身不能带点。
- **保留字引出的形式**：`switch`、`reduce`、`let`、`using` 外形像函数调用，但内部用 `case … =>`、`else =>`、`x in xs`、`acc = init` 标出各个位置；`if`、`fallback` 则是普通的函数名。
- **`if` 不能作变量名与局部名**（字段可以），因为它也是推导式与 `reduce` 里筛选子句的开头。
- **`switch` 的两种形态**：有主语时，`case` 的值与主语比较相等，几个值任一相等就选这一支；没有主语时每个 `case` 是 `bool` 条件，取第一个成立的。只有主语是契约里的枚举、且各 `case` 覆盖了全部成员时才能省略 `else`，所以运行时不会有"没有分支匹配"。
- **嵌套至多 1000 层**（括号、调用、容器、前缀运算符各算一层），更深的是语法错误；ExprJSON 同一上限。

## 取值范围

每种类型能表示什么、在哪里会不精确、超出时怎样，一张表说完。超出范围一律是错误（能在编译期算出的在编译期报），从不悄悄回绕或截断。

| 类型 | 范围与精度 | 精确吗 | 超出或非法时 |
|---|---|---|---|
| `bool` | `true` / `false` | — | — |
| `int` | −9,223,372,036,854,775,808 ~ 9,223,372,036,854,775,807（约 ±9.22×10¹⁸） | 精确 | 溢出、除零是 `ErrArithmetic`；整数除法向零截断 |
