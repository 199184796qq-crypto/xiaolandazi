> **历史记录 · 2026-10-21 归档**
> 原位置：`docs/2026-10-18-plan-save-source-mapping-fix-handoff.md`。
> 原因：保存来源映射已实施交接，归档并链接最终部署回执。
> 下文保留当时原文；其中“当前”、PID、发布目录及待执行命令均指当时，不能直接用作今天的发布指令。归档不表示遗留问题已解决。
> 当前入口：[现行文档索引](../../../README.md)。

---

# 保存方案「事实引用了快照之外的资料」— 修复方案交接（给 Codex）

> 承接你上一份排查文档 `docs/archive/2026-10-21/history/2026-10-18-plan-save-source-reference-diagnosis.md`（结论正确）。
> 本文件是 WorkBuddy 交接给你实施的**精确修复指引**，基线 `0225813`。

## 一、根因（已确认，与你的排查一致）

`contentFactsFromSafety()`（`management-service/internal/httpapi/content_pool_sources.go:165`）在解析安全事实标题时，用**完整方括号内容**（含归属前缀）当键查 `knowledge` 映射：

```go
// content_pool_sources.go:177-181 构建 knowledge 映射，键是「分类/名称」：
knowledge[r.Fields["分类"]+"/"+r.Fields["名称"]] = r.ID   // 例: "location/地址信息" -> "knowledge:66"

// content_pool_sources.go:197  取完整 header（含前缀）：
header := line[3:end]   // 例: "用户指定公共；location/地址信息"

// content_pool_sources.go:230-233  用带前缀的 header 查不到，就兜底假来源：
id := knowledge[header]              // 查不到
if id == "" {
    id = fmt.Sprintf("facts:line:%d", i)   // 假来源 facts:line:10
}
```

归属前缀由 `renderFactScopePrefix()`（`live_agent_plan_fact_scope.go:59`）写入，两种形态：

| 前缀常量 | 实际 header 样例 | 真实来源 |
| --- | --- | --- |
| `- [用户指定公共；` | `用户指定公共；location/地址信息` | `knowledge:66` |
| `- [用户指定商品="1号链接"；` | `用户指定商品="1号链接"；per_capita/10人餐人均消费` | `knowledge:63` |

`facts:line:N` 不在 `set.Sources.Rows` 白名单 → `ValidateSet()` 报「事实引用了快照之外的资料」。

## 二、修复目标（一条原则）

**来源 ID 与事实临时 ID 必须分开。** 归属前缀是元信息，不是事实分类/名称的一部分。解析时要：

1. 用现成的 `explicitPlanFactScope(block)`（`live_agent_plan_fact_scope.go:77`）剥离前缀，得到：
   - `fact.Shared == true` → 公共归属
   - `fact.Links == ["1号链接"]` → 商品归属
2. 拿剥离后的真实 `分类/名称` 去查 `knowledge` 映射，得到真实来源 `knowledge:N`。
3. 归属信息落到 `contentpools.Fact` 的 `Products` / `Owner` 字段（不是塞进 `Sources`）。
4. 查不到真实来源时，**不要再生成 `facts:line:N` 冒充来源**；应产出可定位的错误/notice，交由上层决定，绝不绕过 `ValidateSet` 或凭空猜。

## 三、精确改动点（`content_pool_sources.go`）

改 `contentFactsFromSafety()` 里第 197-234 行这段事实解析逻辑（`header != "商品卡" && header != "有效福利"` 的兜底分支）。

当前兜底分支（第 230-234 行）：
```go
id := knowledge[header]
if id == "" {
    id = fmt.Sprintf("facts:line:%d", i)
}
out = append(out, contentpools.Fact{ID: id, Text: line, Sources: []string{id}, Products: append([]int64{}, all...)})
```

改为（伪代码，请按实际类型落地）：
```go
// 1) 剥离归属前缀，得到真实 header 与归属信息
cleanHeader := header
var products []int64
var owner int64
if scoped, explicit, scopeErr := explicitPlanFactScope(line); explicit && scopeErr == nil {
    cleanHeader = scoped  // 需从 scoped 还原出「分类/名称」——见下方说明
    if scoped.Shared {
        products = append([]int64{}, all...)   // 公共：全部商品
    } else if len(scoped.Links) == 1 {
        owner = products[scoped.Links[0]]      // 商品：单一归属
        products = []int64{owner}
    }
} else {
    products = append([]int64{}, all...)       // 无前缀：兼容旧标题，公共
}

// 2) 用真实分类/名称查 knowledge，拿到真实来源
id := knowledge[cleanHeader]
if id == "" {
    // 不允许 facts:line 兜底。产出可定位错误，交由上层。
    return nil, fmt.Errorf("事实「%s」无法关联当前快照来源", cleanHeader)
}
out = append(out, contentpools.Fact{
    ID: id, Text: line, Sources: []string{id},
    Products: products, Owner: owner,
})
```

### 关于「从 scoped 还原 cleanHeader」的实现要点

`explicitPlanFactScope` 返回的 `anchorcycle.ScopedFact` 里 `Text` 字段是**整行原文**（含前缀），需要自己剥前缀。两种可靠做法（二选一）：

- **方案 A（推荐，复用既有前缀常量）**：在 `content_pool_sources.go` 里直接用 `live_agent_plan_fact_scope.go` 的两个前缀常量做 `strings.TrimPrefix`：
  ```go
  cleanHeader := header
  if strings.HasPrefix(line, explicitCommonFactPrefix) {
      cleanHeader = strings.TrimPrefix(header, strings.TrimPrefix(explicitCommonFactPrefix, "- ["))
      // 结果: "location/地址信息"
      shared = true
  } else if strings.HasPrefix(line, explicitProductFactPrefix) {
      // 剥 "- [用户指定商品=" ... "；" 前缀，取 "；" 之后的部分
      // 结果: "per_capita/10人餐人均消费"
  }
  ```
  注意前缀常量带 `- [` 前缀（`explicitCommonFactPrefix = "- [用户指定公共；"`），而 `header` 已经不含 `- [`（`header = line[3:end]`）。剥的时候要从 `header` 上剥掉「`用户指定公共；`」这一段，不是再剥 `- [`。**建议统一用整行 `line` 做 `strings.TrimPrefix`，避免 `- [` 偏移错位。**

- **方案 B**：新增一个纯函数 `stripFactScopePrefix(header string) (clean string, shared bool, link string, ok bool)`，只负责从 header 字符串剥离前缀，返回干净键 + 归属。放 `content_pool_sources.go` 里，配合单测覆盖四种情况（公共/商品/无前缀/非法前缀）。

### 商品归属的 `owner` 取值

`scoped.Links` 存的是 `LinkKey`（如 `"1号链接"`），而 `contentpools.Fact.Owner` 是 `int64`（商品 record ID）。需要 `products[link]`（`content_pool_sources.go:170` 已建 `products := map[string]int64{}`，键是 `p.Link`）把 `LinkKey` 转成 ID：

```go
owner = products[scoped.Links[0]]
```

若 `products[link] == 0`（链接不存在），同样报可定位错误，不要静默塞 `all`。

## 四、必须覆盖的测试（新增到 `content_pool_sources` 或对应测试文件）

1. 公共前缀标题 → 来源解析为真实 `knowledge:N`，`Products == all`。
2. 商品前缀标题 → 来源为真实 `knowledge:N`，`Owner == 对应商品ID`，`Products == [该ID]`。
3. 无前缀旧标题 → 行为不变（兼容，`Products == all`）。
4. 非法/空商品前缀 → 报可定位错误，不兜底 `facts:line`。
5. 用 Codex 文档里的 9 条真实输入逐条验证（`source-mapping-proof.json`），每条 `Sources[0]` 都落在 `set.Sources.Rows` 里。

跑法：
```
cd management-service && go test ./internal/httpapi/ -run "ContentFacts|FactScope|ContentPool" -v
```

## 五、不要做的事（红线）

- ❌ 不要删事实、不要取消 `ValidateSet()` 来源完整性校验。
- ❌ 不要再生成 `facts:line:N` 冒充来源（这是本次根因）。
- ❌ 不要改动 WorkBuddy 已完成的两块：`prepareDerivedSafety` 风险剔除逻辑（`live_derived_safety.go`）、保存入口去阻塞（`live_agent_plan_versions.go` / `content_pool_api.go` / `LiveStrategyView.vue`）。
- ❌ 不要把「保存入口不等待更新」当成「来源映射已修好」，两者分开验收。

## 六、验收口径

修完后，对 `tenant_id=14 / plan_id=5` 重新触发内容池更新，任务应能走到 `CommitContentPoolSet` 不再报「事实引用了快照之外的资料」；`ValidateSet` 通过（因为所有 `Sources[0]` 都是真实 `knowledge:N`）。同时「保存方案」不再因前置更新失败而被中止。

---

如需我这边配合改测试文件或核对边界，按 `协作交接.md` 规则登记交接即可。
