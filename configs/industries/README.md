# 行业配置包 · 结构约定（定稿）

**「目录 json」做树形索引 + 每个节点一套四件套（挖掘 md + 侧面 json + 事实分类 json + P0 补充 md）。**
所有标识统一「**英文内部 code + 中文显示名，显示名预留英文（i18n）**」。

---

## 一、目录 json（每个业务域一个）

`ecommerce.json` / `local-service.json`，维护该域的行业**树**，支持任意层级，**每级节点都可配自己的四件套，子级未配置的项向上继承父级**。

```json
{
  "schema_version": 2,
  "biz_domain": "ecommerce",
  "name": "电商",
  "name_en": "E-commerce",
  "defaults": {
    "selling_dimensions": "",
    "sides": "",
    "fact_categories": "",
    "p0_extra": ""
  },
  "nodes": [
    {
      "code": "food",
      "name": "食品",
      "name_en": "Food",
      "selling_dimensions": "food/selling-dimensions.md",
      "sides": "food/sides.json",
      "fact_categories": "food/fact-categories.json",
      "p0_extra": "food/p0-extra.md",
      "children": []
    }
  ]
}
```

### 字段

| 字段 | 说明 |
|---|---|
| `biz_domain` | 业务域英文 code（ecommerce / local_service）|
| `name` / `name_en` | 业务域中文名 / 英文名（英文可选）|
| `defaults` | 根级兜底四件套，整个域最终回退到这里 |
| `nodes[]` | 行业树，节点含 `code`、`name`/`name_en`、四件套路径、`children[]` |

### 四件套解析规则（从叶子向上回溯，逐项独立）

1. 节点该项非空 → 用它；
2. 空 → 逐级向父节点找；
3. 回溯到根仍空 → 用 `defaults` 对应项；
4. `defaults` 也空 → 用程序内置兜底。

> 大类配一份，子类自动继承；子类想差异化，只覆盖不同的那一两个文件，不必重复配全。

### 可选项

- **叶子节点**（无 children）＝ 下拉框可选行业。
- **中间节点**（有 children）＝ 分类目录，可配四件套当子类默认，也可作可选行业（前端决定是否列进下拉框）。

---

## 二、四件套（每个行业一套，单一真相源，互不重复）

| 文件 | 给谁 | 内容 | 什么时候改 |
|---|---|---|---|
| `selling-dimensions.md` | 模型（prompt）| 这个行业要挖哪些卖点维度、怎么挖、硬性覆盖要求 | 想调整「挖哪些卖点」 |
| `sides.json` | 程序（结构化）| 侧面池 + 事实分类→侧面映射（「侧面筛事实」用）| 想调整「侧面/映射」 |
| `fact-categories.json` | 程序（结构化）| 事实分类枚举（事实录入表单 + 校验用）| 想调整「事实怎么分类」 |
| `p0-extra.md` | 模型（prompt）| 行业高危违规承诺清单（团购/食品类目重点）| 想收紧合规 |

## 二点五、母表 + 行业叠加（关键分层）

系统里存在两层配置，职责分明：

| 层 | 内容 | 归属 | 变化频率 |
|---|---|---|---|
| **全局母表** | 四层结构（必说/可说/不提/改写）、通用 P0 硬块（p0-hard-block.md）、挖掘流程骨架、通用维度 | 全局一份，所有行业共用 | 几乎不变 |
| **行业四件套** | 挖掘维度 md + 侧面 json + 事实分类 json + P0 补充 md | 每行业一份 | 按行业调 |

**P0 最终生效 = 通用母表（p0-hard-block.md）+ 行业 `p0-extra.md` 叠加**。行业 p0-extra 只写「母表没有、但该行业特有的高危承诺」，不重复母表已有的通用规则。程序/模型执行时，先套通用母表，再叠加行业补充。

同理，**挖掘维度** = 通用维度（母表）+ 行业专属维度（selling-dimensions.md 里已含两者）；**侧面/事实分类** = 行业 json 完整定义（无行业配置时回退通用默认）。

**铁律**：侧面、映射只在 `sides.json` 定义；事实分类只在 `fact-categories.json` 定义；两个 md 只写 json 表达不了的语义；p0-extra 只写行业增量、不复制母表。改侧面只改 sides.json，改分类只改 fact-categories.json，改维度只改 md，互不重复。

### sides.json 结构

```json
{
  "schema_version": 2,
  "sides": [
    { "code": "scenario", "name": "使用场景", "name_en": "Usage Scenario", "description": "什么场合用得上" }
  ],
  "category_sides": {
    "price": ["value", "comparison"],
    "flavor": ["comparison", "scenario"]
  }
}
```

- `sides[]`：侧面池。`code` 英文内部（程序用），`name`/`name_en` 中英文显示，`description` 描述。
- `category_sides`：事实分类 value → 侧面 **code** 数组（映射用英文 code）。

### fact-categories.json 结构

```json
{
  "schema_version": 1,
  "categories": [
    { "value": "price", "label": "价格 / 单价 / 价差", "label_en": "Price", "group": "价格活动", "group_en": "Price & Promotion", "family": "trade", "description": "商品价格" }
  ]
}
```

- `categories[]`：事实分类枚举。`value` 英文内部，`label`/`label_en` 中英文显示，`group`/`group_en` 分组中英文，`family` 归属族，`description` 说明。

---

## 三、外卖的定位（独立行业，不塞进到店餐饮）

| | 到店团购 restaurant | 外卖 restaurant_delivery |
|---|---|---|
| 决策链 | 买券 → 到店核销 → 体验 | 下单 → 即时配送 → 收货 |
| 核心维度 | 位置/环境/服务/预约核销 | 配送范围/时效/起送价/配送费/打包保温 |
| 侧面 | 到店场景/到店体验/地理位置/预约核销 | 配送时效/配送范围/打包保温/满减算账 |
| 事实分类 | location/environment/appointment/redemption | delivery_range/delivery_time/packing/delivery_fee/min_order |

外卖「配送」接近电商「物流」，但是**即时配送（分钟级）** vs 快递（天级），故外卖配送单列事实分类，不复用电商 shipping_*。外卖行业包第二期补，本期先做到店餐饮。
