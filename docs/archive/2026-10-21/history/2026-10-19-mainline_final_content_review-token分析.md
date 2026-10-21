> **历史记录 · 2026-10-21 归档**
> 原位置：`docs/2026-10-19-mainline_final_content_review-token分析.md`。
> 原因：保留旧复核成本实测，当前路径已变，不能用旧调用账预测现在成本。
> 下文保留当时原文；其中“当前”、PID、发布目录及待执行命令均指当时，不能直接用作今天的发布指令。归档不表示遗留问题已解决。
> 当前入口：[现行文档索引](../../../README.md)。

---

# mainline_final_content_review 审核环节 token 分析

> 日期：2026-10-19
> 数据基准：Session 314（room_id=29，2026-10-18 14:46:43 → 15:00:55）
> 源码基准：commit 9698c4e（服务器运行版本）

## 一、现状数据

- 审核环节 28 次调用，总 input 149,006 token，output 1,121 token
- 单条 input ~5,800 token（request_json ~11,700 字符）
- cached：时好时坏（4736 或 NULL 交替）

## 二、单条请求的消息构成（5 条消息）

| # | role | 字符 | 内容 | 稳定性 |
|---|---|---|---|---|
| [0] system | 2608 | 四层审核母版（audit-four-layer.md，通用层+品类层） | 整场稳定 |
| [1] system | 3161 | 完整有效P0（JSON：说明+适用规则词表） | 整场稳定 |
| [2] system | 1268 | 审核任务说明母版（mainline-final-content-review.md） | 整场稳定 |
| [3] system | 1517~2784 | 当前相关事实包（全场全量事实 + 表达约束 + 用户要求） | 同场稳定 |
| [4] user | ~420 | 当前任务 + 正文全文 + 前次P0结果 | 每段变 |

**前 4 条 system 合计 ~8550 字符 ≈ 4736 token，占单条 input 的 81%。**

## 三、为什么 token 高

1. **审核的「标准」比生成还重**：前 4 条 system 就占了 81%，其中「完整有效P0」[1] 3161 字符是最大单块。
2. **事实包 [3] 整段塞进 system**：全场全量事实（所有商品链接、所有 fact）序列化成 JSON，不筛侧面。
3. **user [4] 塞了正文全文**：待审口播全文（200+ 字）而非摘要。

## 四、缓存时好时坏的根因（已定位，非 shuffle）

实测前 4 条 system 哈希逐字一致，只有 user 变。cached 命中时 = 4736（正好等于前 4 条 system 的 token 量）。

根因：**DashScope 前缀缓存池被 mainline_text 的大前缀挤占淘汰**。调用时序是 text→review→text→review 严格交替，mainline_text 每次 ~9500 token 全新 input 频繁写入缓存池，把 review 的前缀挤掉，导致 review 的 cached 时有时无。

## 五、优化结论（1、2、3 逐条验证后）

### ① 审核 P0 瘦身 —— ✅ 已落地（零风险）

`generationP0View` 的瘦身（删「适用范围/适用条件/匹配表达/排除表述」4 个元信息字段）在上一轮 A 方案已提交（commit 7c4405f）。审核环节 [1] 的「完整有效P0」走同一个函数，**自动受益**。预计 [1] 从 3161 → ~2200 字符。

### ② 事实包只喂侧筛事实 —— ❌ 不能做（测试锁死）

尝试把 `finalMainlineContentMessages` 里的 `state.HookFacts()` 改成 `state.HookFactsForSide()`，被测试拦下：

- `TestMainlineSideMaterialCannotReenterThroughContentStrategy`（text_generation_side_scope_test.go:64）断言「audit lost the complete facts」
- 设计意图（测试注释原话）：**「The audit still needs the complete product-authorized facts, including usage conditions; generation filtering must not erase its evidence.」**

即：审核必须用**完整事实包**，否则会漏判「正文编造了当前侧面之外的事实」。这条是正确性红线，不能动。

### ③ 移除正向举例 shuffle —— ❌ 不能删（设计意图）

尝试删 `GenerationMaterialViews` 里的 `rand.Shuffle`，被测试拦下：

- `TestSpeechLibraryRandomExamplesCannotMutateSavedFacts`（speech_library_test.go:313）断言「model views no longer shuffle examples」
- 设计意图：shuffle 是「措辞轮换」的一部分，防止模型死记固定顺序照念正向举例。

## 六、真正能做的优化方向（未实施，待评估）

1. **调度层：让审核调用更连续**。当前 text→review 严格交替，text 的大前缀把 review 前缀挤掉。若能把「同一批 review 调用」聚拢，或调大 DashScope 缓存池，cached 命中率可稳定在 4736/段，等效 input 砍 81%。
2. **事实包 [3] 的序列化瘦身**（不删事实，只改表达）：`GenerationFactView` 输出的「事实内容」里可能带冗余前缀（如 `- [用户指定商品="1号链接"；price/价格]`），这些结构化标签对审核模型是噪音。可评估精简。
3. **user 正文压缩**：审核时正文可截断/去口语冗余，但需确认不影响审核准确性。

## 七、教训

- **改代码前必看测试**。两个「看起来合理的优化」（②③）都被现有测试证明是设计意图，贸然改会破坏正确性。
- **缓存失效根因在调度层，不在数据层**。前 4 条 system 逐字稳定（哈希一致），说明数据没问题；时好时坏是 DashScope 缓存池淘汰策略 + text/review 交替调用导致的。
- 审核环节的「完整事实包」和「shuffle 轮换」是**有意设计**，是审核正确性和防复读的关键，不能为了省 token 牺牲它们。
