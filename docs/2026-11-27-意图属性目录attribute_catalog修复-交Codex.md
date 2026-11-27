# 意图输入 attribute_catalog 错误与修复（交 Codex）

> 日期：2026-11-27
> 现象：意图请求里的 `attribute_catalog` 把**商品名/泛类**当成了属性名，模型据此选 `attributes` 会选错。
> 目标：把 `attribute_catalog` 从“claim_key → topic”改成“claim_key → 规范属性”，线上/本地同步。

---

## 一、现在是什么

意图 v2 请求 payload 里：

```json
"attribute_catalog": [
  {"key": "无核无籽", "topic": "爱媛果冻橙"},
  {"key": "折算单价", "topic": "价格"},
  {"key": "品种来源", "topic": "爱媛砂糖橘杂交品种"},
  {"key": "直播价", "topic": "金秋砂糖橘价格"},
  {"key": "适用人群", "topic": "商品"},
  {"key": "邮寄", "topic": "配送"}
]
```

程序含义：

- `key` = 事实的 `claim_key`；
- `topic` = 程序告诉模型“这条事实属于哪个属性/主题”；
- 模型用这个目录决定 asks[].attributes。

它和别名表不是一回事：

| 东西 | 作用 |
|---|---|
| `attribute_catalog` | 本次请求带的事实 `claim_key → 属性名` 清单，给意图模型选属性用 |
| `live_fact_attribute_aliases` | 数据库里的“别名 → 规范属性”归一表（价格规格→价格、邮寄→配送） |
| `live_fact_attributes` | 114 个规范属性目录 |

---

## 二、错在哪

### 1. 用了 `topic`，不是 `attribute`

事实表：

- `topic`：主题，历史数据里可能是商品名；
- `attribute`：**真实属性名**（线上 UI 展示的那个）。

当前本地 lab 的 `intentPayloadAligned` 生成 catalog 时：

```go
name := danmakutest.AttributeName(item.Topic, links, stores)
catalog = append(catalog, map[string]string{"key": key, "topic": name})
```

`AttributeName` 在 topic 是商品名时很可能原样返回商品名，于是：

```json
{"key":"无核无籽","topic":"爱媛果冻橙"}      // 产品名被当属性
{"key":"品种来源","topic":"爱媛砂糖橘杂交品种"}
{"key":"直播价","topic":"金秋砂糖橘价格"}
{"key":"适用人群","topic":"商品"}             // 泛类，不是属性
```

### 2. 后果

模型看到 attributes 目录里有“爱媛果冻橙”，可能输出：

```json
"attributes": ["爱媛果冻橙"]
```

召回按属性过滤时：

- 真实属性是“果核与果籽/品种来源/价格”，不是“爱媛果冻橙”；
- 属性过滤匹配不到 → 空召回或只剩全局；
- 回答只能靠简介或宽召回，grounding 变差。

### 3. 哪些字段可以进 catalog

| 来源 | 是否可用 |
|---|---|
| `claims.attribute` | ✅ 首选 |
| `AttributeName(topic)` 且结果不是商品名/对象名 | ✅ 兜底 |
| `topic` 本身是商品名 | ❌ 不能当属性 |
| “商品/商品信息/其他”这类泛类 | ⚠️ 可保留为兜底，但不应该占主要属性 |
| 对象目录 display_name | ❌ 不能出现在属性目录 |

---

## 三、正确形态

`attribute_catalog` 应该是：

```json
"attribute_catalog": [
  {"key": "无核无籽", "topic": "果核与果籽"},
  {"key": "折算单价", "topic": "价格"},
  {"key": "品种来源", "topic": "品种来源"},
  {"key": "直播价", "topic": "价格"},
  {"key": "适用人群", "topic": "适用人群"},
  {"key": "邮寄", "topic": "配送"}
]
```

并且 `attributes` 列表也只放规范属性：

```json
"attributes": ["价格","配送","果核与果籽","品种来源","适用人群", ...]
```

对象（1号链接/全门店/商品名）只出现在 `objects`，不进入 `attributes`。

---

## 四、修复方案

### 1. 生成 catalog 时改用 `attribute`

本地：`cmd/fact-rerank-lab/planctx.go` 的 `intentPayloadAligned`

当前：

```go
for _, item := range cached.facts {
    name := danmakutest.AttributeName(item.Topic, links, stores)
    ...
    catalog = append(catalog, map[string]string{"key": key, "topic": name})
}
```

改成：

```go
for _, item := range cached.facts {
    name := strings.TrimSpace(item.Attribute)   // 真实属性
    if name == "" {
        name = danmakutest.AttributeName(item.Topic, links, stores) // 兜底
    }
    if name == "" || isObjectOrProduct(name, links, stores) {
        continue // 不能把商品名/对象名当属性
    }
    ...
    catalog = append(catalog, map[string]string{"key": key, "topic": name})
}
```

`attributes` 列表也从同一份 name 去重生成，保证 catalog 和 attributes 一致。

### 2. 属性名归一（可选但建议）

`name` 先过一遍规范属性：

- 精确匹配 `live_fact_attributes.canonical_name`；
- 再查 `live_fact_attribute_aliases`（别名→规范）；
- 仍不匹配可走 embedding 最近属性（阈值 ≥0.55），或保留原名但记录日志。

不要在这里用产品名做属性。

### 3. 过滤对象名/产品名

需要排除：

- `objects` 里的 display_name（1号链接/2号链接/全门店…）；
- `live_agent_plan_product_links.product_name`；
- 事实 `topic` 如果是商品名（与 objects/product_name 相同）。

排除后如果没有任何属性，宁可该事实不进 catalog，也不要塞“商品名”。

### 4. 线上同步

线上 `answerflow` 的意图 payload 构造（`intentPayload`）也要检查：

- 是否用 `topic` 生成 attribute_catalog；
- 是否用 `claims.attribute`；
- 是否把 product_name 当属性。

改完后线上/本地请求里的 catalog 应一致。

### 5. 提示词不需要大改

提示词第 16 条已经要求“属性只能来自 attributes”，第 17 条要求“不使用输入目录以外的业务词”。

问题在**输入目录本身是脏的**，不是提示词。

可以加一句校验（可选）：

> `attributes` 必须来自 attribute_catalog 的 topic，并且不能是对象名或商品名。

但不要靠提示词兜数据问题，优先修 payload。

### 6. 校验与日志

- 生成 catalog 时记录：总事实数、进 catalog 的事实数、被过滤的对象名/商品名数量；
- 意图返回后记录：模型输出的 attributes 是否都在 catalog 里；
- 不在 catalog 里的属性：打日志 + 走 `normalizeAttributes` 映射，映射不到则丢弃。

---

## 五、验收用例

### 1. catalog 内容

plan 9「好充食果业」跑意图请求，`attribute_catalog` 里：

- 不应出现：`爱媛果冻橙`、`爱媛砂糖橘杂交品种`、`金秋砂糖橘价格`、`1号链接`、`全门店`；
- 应出现：`价格`、`配送`、`果核与果籽`、`品种来源`、`适用人群`、`商品名称` 等规范属性。

### 2. 意图输出

| 弹幕 | 期望 attributes |
|---|---|
| 这个多少钱 | 价格类 |
| 1号好吃吗 | 口感/适用人群（或空→按对象全量） |
| 能邮寄吗 | 配送/邮寄类 |
| 3号和5的区别 | 空 + scope=全部（对比全量） |
| 哈尔滨有吗 | 门店范围/覆盖区域/配送范围 |
| 1号怎么样 | 空 + 全部（泛问） |

### 3. 回归

- 改前/改后各跑 20 条真实弹幕；
- 对比：意图 attributes、召回条数、回答正确率、空召回率；
- 确认对象/product 名称不再出现在 attributes。

---

## 六、涉及文件

| 文件 | 改动 |
|---|---|
| `cmd/fact-rerank-lab/planctx.go` | `intentPayloadAligned` 用 `item.Attribute`，过滤对象/商品名 |
| `internal/answerflow/pipeline.go`（线上） | `intentPayload` 同步修 |
| `internal/db/fact_attribute_catalog_seed.sql` | 规范属性/别名（现有，用于归一） |
| `cmd/fact-rerank-lab/attribute_expand.go` | 属性扩展；catalog 修好后扩展输入会更干净 |
| `configs/prompts/anchor-style/danmaku-test-intent-v2.md` | 原则上不用改；可选加校验句 |

---

## 七、原则

1. **属性目录只放属性**：对象、产品名、门店名不进 attributes；
2. **优先用 `claims.attribute`**，topic 只做兜底；
3. **catalog 和 attributes 列表同源**，避免模型看到两套名字；
4. 不靠提示词硬背业务词，靠数据清洗；
5. 改完必须用 plan 9 实际请求验证 catalog 和意图输出。
