// Each example is the two parts a program has: the expression, and the
// contract its host holds. They are never merged into one text — the page
// sends them as separate fields, exactly as a console would.
//
// The expression is source, not hand-built ExprJSON, so an example that stops
// compiling fails loudly instead of drifting out of sync with the language.
// Adding one means adding an entry here; nothing else in the page changes.
export const EXAMPLES = [
  {
    label: "主备路由",
    description: "扩展函数判断健康度，不健康就降级",
    source: "if(route.is_healthy_v1(health), \"adyen_primary\", \"stripe_backup\")",
    contract: {
      args: [
        { name: "health", type: "string", doc: "渠道健康状态，UP 表示可用" },
      ],
      result: { type: "string", doc: "选中的渠道" },
    },
  },
  {
    label: "国家分流",
    description: "一个分支匹配多个值",
    source: "switch(country,\n  case \"SG\", \"MY\", \"TH\" => \"adyen_asia\",\n  case \"US\", \"CA\"        => \"stripe_na\",\n  else \"stripe_global\"\n)",
    contract: {
      args: [
        { name: "country", type: "string", doc: "ISO 3166-1 二字码" },
      ],
      result: { type: "string", doc: "渠道号" },
    },
  },
  {
    label: "分层费率",
    description: "无主体 switch 就是条件链；阈值在编译期算掉",
    source: "switch(\n  case amount > 1000 * 100 => \"manual_review\",\n  case amount > 100 * 100  => \"three_ds\",\n  else \"auto_approve\"\n)",
    contract: {
      args: [
        { name: "amount", type: "int", doc: "订单金额，单位：分" },
      ],
      result: { type: "string", doc: "处理路径" },
    },
  },
  {
    label: "编译期算清",
    description: "三个常量绑定：零局部槽、零运行时乘法",
    source: "let(\n  bps       = 250,\n  base_fee  = 3 * 100 + 50,\n  total_bps = bps * 2,\n  amount * total_bps / 10000 + base_fee\n)",
    contract: {
      args: [
        { name: "amount", type: "int", doc: "订单金额，单位：分" },
      ],
      result: { type: "int", doc: "应收总额，单位：分" },
    },
  },
  {
    label: "筛选健康渠道",
    description: "列表推导式带筛选",
    source: "[c for c in channels if route.is_healthy_v1(c)]",
    contract: {
      args: [
        { name: "channels", type: "array<string>", doc: "候选渠道的健康状态" },
      ],
      result: { type: "array<string>", doc: "可用渠道" },
    },
  },
  {
    label: "累计金额",
    description: "reduce 折叠成一个值",
    source: "reduce(price in prices, total from 0, total + price)",
    contract: {
      args: [
        { name: "prices", type: "array<int>", doc: "各笔金额，单位：分" },
      ],
      result: { type: "int", doc: "合计，单位：分" },
    },
  },
  {
    label: "权重筛选",
    description: "字典推导式，按 key 排序保证可重放",
    source: "[name for name, weight in weights if weight > floor]",
    contract: {
      args: [
        { name: "weights", type: "dict<float>", doc: "渠道权重，key 是渠道号" },
        { name: "floor", type: "float", doc: "权重下限" },
      ],
      result: { type: "array<string>", doc: "达标渠道" },
    },
  },
  {
    label: "风控组合",
    description: "布尔运算是派生形式，展开成惰性 if",
    source: "!trusted && (risk > 0.8 || amount > 1000 * 100)",
    contract: {
      args: [
        { name: "amount", type: "int", doc: "订单金额，单位：分" },
        { name: "risk", type: "float", doc: "风控评分，0-1" },
        { name: "trusted", type: "bool", doc: "是否白名单商户" },
      ],
      result: { type: "bool", doc: "是否需要人工复核" },
    },
  },
  {
    label: "打分选优",
    description: "扩展函数返回 float，与阈值比较",
    source: "if(route.score_v1(auth_rate, cost) > 0.6, \"adyen_primary\", \"stripe_backup\")",
    contract: {
      args: [
        { name: "auth_rate", type: "float", doc: "授权成功率，0-1" },
        { name: "cost", type: "float", doc: "渠道成本，0-1" },
      ],
      result: { type: "string", doc: "渠道号" },
    },
  },
  {
    label: "模型评分",
    description: "引擎张量以句柄穿过表达式：向量化 → 评分，语言不看内容",
    source: "let(e = model.embed_v1(features), if(model.fraud_v1(e) > 0.8, \"review\", \"accept\"))",
    contract: {
      args: [{ name: "features", type: "array<float>", doc: "特征向量，交给模型引擎" }],
      result: { type: "string", doc: "处置" },
    },
  },
  {
    label: "稳定 ABI",
    description: "契约声明了但表达式不用：调用方不必改",
    source: "amount * 2",
    contract: {
      args: [
        { name: "amount", type: "int", doc: "订单金额，单位：分" },
        { name: "legacy_flag", type: "bool", doc: "调用方仍在传，表达式已不用" },
      ],
      result: { type: "int", doc: "应收金额，单位：分" },
    },
  },
];
