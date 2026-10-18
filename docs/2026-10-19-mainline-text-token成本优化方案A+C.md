# mainline_text token 成本优化方案（A+C）：根因分析与改造

> 分析时间：2026-10-19 00:1x
> 数据来源：session 314（room_id=29）`live_model_exchanges` 表实测 + 服务器运行版本 `9698c4e` 源码
> 结论：**单条 mainline_text 请求 ~9500 input token，最大头是「有效P0」(3460字符) + 「主播风格母版」(1783) + 「主播表达标尺」(726) 组成的 stable JSON；缓存命中率仅 ~2% 的根因是「开场段 vs 正常段」消息条数不稳定。**

---

## 一、实测数据（session 314）

- mainline_text 41 次调用，input 366,646 token，cached 仅 7,936（约 2%）。
- 单条请求 input 约 9,500~10,000 token，request_json 字符约 17,800。
- 单条 = 3~4 条消息（见下），总 ~16,900 字符 ≈ 9500 token。

### 消息构成（正常段，如 id=24103，3 条）

| # | role | 字符 | 内容 |
|---|---|---|---|
| [0] system | 83 | 链接称呼纠正（1号链接/2号链接） |
| [1] system | 9,042 | 主线正文生成母版 + **5696 字符 stable JSON** |
| [2] user | 7,941 | 本段任务（可说事实+卖点×角度+改写+字数上限） |

### 消息构成（开场段，如 id=24099，4 条）

| # | role | 字符 | 内容 |
|---|---|---|---|
| [0] system | **3,717** | **foundation = text-generation-foundation.md + cycle-opening.md（开场多这一条）** |
| [1] system | 83 | 链接称呼纠正 |
| [2] system | 9,042 | 主线正文生成母版 + 5696 字符 stable JSON |
| [3] user | 2,088 | 本段任务 |

### stable JSON（5696 字符）内部构成

| 字段 | 字符 | 占比 |
|---|---|---|
| **有效P0** | 3,460 | 60% |
| 主播风格母版 | 1,783 | 31% |
| 主播表达标尺 | 726 | 12% |
| 不提 | 65 | 1% |
| 用户已确认的长期纠正规则 | 2（空[]） | 0% |

「有效P0」3460 字符再拆：9 条「适用规则」，每条带 9 个字段（类别/适用范围/适用条件/处理方式/相关表述/匹配表达/排除表述/所需依据/规则说明）+ 1 段「说明」。其中「相关表述」词表（极限词那条 70+ 词）和「说明」段是最大头。

---

## 二、两个问题的根因（精确）

### 问题1：为什么 input 这么大

「有效P0」+「风格母版」+「表达标尺」这三块稳定上下文，被 `json.Marshal` 序列化成 5696 字符 JSON，**每段都整段塞进 system**。这是硬性的每段成本，41 段 = 366k token 的主因。

### 问题2：为什么缓存命中率只有 2%

**根因：开场段比正常段多一条 `foundation` system（3717 字符），导致 system 前缀在两种形态间跳变。**

代码证据（`management-service/internal/httpapi/text_generation.go` `renderCycleTextMessages`）：

1. `foundation`（mainline-text-task.md 母版）在 `Phase=="opening"` 时 append 了 `cycle-opening.md`（约 3700 字符），否则不 append。
2. 但更关键的是 `completeCycleText` 第 315-317 行：
   ```go
   if profile.SystemPrompt != "" {
       req.Messages = append([]agentgateway.Message{{Role:"system", Content: profile.SystemPrompt}}, req.Messages...)
   }
   ```
   `profile.SystemPrompt`（= text-generation-foundation.md + 模型适配母版）被 prepend 成独立一条 system。

3. qwen provider（`provider_qwen.go` 223-228 行）给**所有 system 消息**打 `cache_control:{type:ephemeral}`，DashScope 做**前缀缓存**。

4. 实测相邻 4 条 system 前缀哈希在两个值间交替（`94388715...` 12844字符 ↔ `ffeb4f77...` 9126字符），差 3718 字符——正是开场段 vs 正常段的差异。

**结论：前缀缓存要求 system 从第一条开始逐字稳定；开场段在开头插入了 3717 字符的 foundation，把后续所有内容（包括那段本应命中缓存的 stable JSON）的缓存位全部打乱。整场 41 段里开场/正常交替，任何一段都吃不到前一段的缓存。**

---

## 三、改造方案（A + C）

### A：瘦身「有效P0」（改母版 + 改 `GenerationP0View`，不改拦截语义）

`GenerationP0View`（`speechbase/render.go`）每条规则输出 9 字段，其中对生成模型**无用**的元信息可去掉：

1. **去掉「适用范围」「适用条件」**：`"适用范围":"all"` / `"适用条件":"always"` 是程序用元信息，生成模型只看「处理方式」和「相关表述」。省 ~200 字。
2. **去掉「匹配表达」正则**：`"(?i)(?:no[.]?1|...)"` 这种正则给模型读毫无意义，是程序扫描用的。省 ~150 字。
3. **去掉「排除表述」**（多数为 null/[]）：省 ~50 字。
4. **压缩「规则说明」的免责句**：如「不将单字命中等同于法定违法认定」「本词表为保守业务硬禁表」这类给人看的说明，模型不需要。省 ~300 字。
5. **压缩「说明」总则段**：现在 ~250 字反复说 hard/semantic 处理，可压到 2 句。省 ~150 字。

**预计「有效P0」从 3460 字符降到 ~2200 字符（省 36%），stable JSON 从 5696 降到 ~4400 字符。** 零拦截语义损失（这些都是冗余元信息）。

> ⚠️ 注意「相关表述」词表**不能动**——它是模型主动回避极限词的依据，砍了会重新拉高退回率（session314 的教训）。

### C：稳定 system 前缀吃缓存（改代码，根治）

**目标：让 system 从第一条起整场逐字稳定，且把稳定块和变化块彻底分离。**

具体改法（`renderCycleTextMessages` + `completeCycleText`）：

1. **统一 system 顺序**：把 `profile.SystemPrompt`（foundation）**始终作为第一条**，开场段的 `cycle-opening.md` **不要 append 到 foundation 里**，改成放进 user 消息（或单独的可变 system 放最后）。消除「开场多一条」的结构差异。

2. **stable JSON 单独成一条纯 system**：现在是 `foundation + fixed` 拼成一条、再被 prepend。改成：
   ```
   [system] profile.SystemPrompt（稳定，第1条）
   [system] stable JSON（有效P0+风格母版+标尺，稳定，第2条）
   [user] 动态任务（每段变）
   ```
   开场母版 `cycle-opening.md` 内容移到 user（因为它只在开场段出现，属于「变」的部分）。

3. **确保 stable 逐字稳定**：`EffectiveCorrections()`（长期纠正规则）现在是空 `[]`，如果它会随场次增长，要确认它确实是「整场不变」——若逐段变，它也必须移出 stable。

**预期收益**：stable JSON（4400 字符）每次命中缓存，等效 input 成本从 9500 token/段 降到 ~5100 token/段，**mainline_text 总 input 成本砍 40%+**（叠加 A 的瘦身，最高可砍 55%）。

---

## 四、风险与验证

- **A 风险低**：纯删元信息字段，但需回归验证「退回率不升」（因为去掉了正则/适用条件，模型会不会更敢用极限词？——不会，相关表述词表还在，处理方式还在）。
- **C 风险中**：改了消息拼装顺序，需回归验证：① 开场/正常/讲解三类话术都正常生成；② 缓存命中率实测提升；③ P0 拦截不退化。
- **验证方式**：部署后看 `live_model_exchanges.cached_input_tokens` 是否从 ~2% 升到显著值（预期 40%+）。

---

## 五、待办

- [ ] 确认 `EffectiveCorrections()` 是否整场稳定（决定它要不要移出 stable）。
- [ ] A 方案：改 `GenerationP0View` 去掉 4 个元信息字段 + 压缩说明。
- [ ] C 方案：改 `renderCycleTextMessages`/`completeCycleText` 统一 system 顺序、stable 独立成条、开场母版移入 user。
- [ ] 重编译 management-service、走 Codex 增量发布。
