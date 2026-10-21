> **历史记录 · 2026-10-21 归档**
> 原位置：`docs/2026-10-19-给Codex-主线token优化A+C二进制发布交接.md`。
> 原因：已有后续实操/回滚记录，旧生产PID/哈希不是现况；遗留问题迁到当前待办再关闭。
> 下文保留当时原文；其中“当前”、PID、发布目录及待执行命令均指当时，不能直接用作今天的发布指令。归档不表示遗留问题已解决。
> 当前入口：[现行文档索引](../../../README.md)。

---

# 给 Codex：主线生成 token 优化（A+C）二进制发布交接

> 交接时间：2026-10-19 00:2x
> 交接方：WorkBuddy → Codex
> 代码提交：`7c4405f`（perf: 主线生成token优化）
> 生产当前：`current` = `/opt/xiaolan/releases/20261019-prompt-boundaries-957e99b`，Management 二进制仍是 `9698c4e`（PID 1115478，SHA256 `0ff49ea00ed66c0bd1f194b29d0ddf4b62cab0a7b6fd13810687443edd394af1`）
> 本次性质：**改 Go 代码，需重编译 management-service 二进制 + 重启 Management 服务**（不是母版增量，不能套用「不重启」结论）

---

## 0. 背景与目标

session 314 实测：`mainline_text` 41 次调用 input 366,646 token，cached 仅 7,936（约 2%）。

单条 mainline_text = 3~4 条消息 ≈ 9500 input token，最大头是 system 里嵌入的 stable JSON（有效P0 3460字符 + 风格母版 1783 + 表达标尺 726）。

两个问题：
1. **input 大**：有效P0 含大量模型用不上的元信息字段。
2. **缓存命中率低**：system 前缀在「开场/讲解回归」环节与正常环节之间结构不稳定，且 prepend 层存在 3/4 消息跳变。

本次改动针对问题 1（A）和问题 2 的 renderCycleTextMessages 内部部分（C）。

---

## 1. 代码改动（2 个文件，已编译 + 测试通过）

### A. `management-service/internal/speechbase/render.go` — `GenerationP0View` 精简

删掉 4 个生成模型用不上的元信息字段，保留模型真正需要的：

| 字段 | 处理 | 理由 |
|---|---|---|
| 适用范围 `r.Scope` | 删 | `all`/`ordinary_food` 是程序元信息 |
| 适用条件 `r.Condition` | 删 | `always`/`when_unsubstantiated` 程序元信息 |
| 匹配表达 `r.Patterns` | 删 | 正则给模型读无意义，程序扫描用 |
| 排除表述 `r.ExcludedTerms` | 删 | 多为 null/[] |
| 类别 / 处理方式 / 相关表述 / 规则说明 | 保留 | 模型回避极限词的核心依据 |
| 所需依据 `r.RequiredBasis` | 保留（非空时） | semantic 规则判断依据 |

同时压缩「说明」总则段（去掉「凡命中…优先回避」的重复强调）。

**关键红线**：`相关表述` 词表**必须保留**——它是模型主动回避极限词的依据，砍了会重新拉高退回率（session314 教训）。

### C. `management-service/internal/httpapi/text_generation.go` — `renderCycleTextMessages`

把「开场母版 cycle-opening.md」和「讲解回归母版 lecture-mainline-transition.md」从 `foundation`（system 消息）移出，改放 user 消息的 `环节指令` 字段。这样 `foundation`（mainline-text-task.md）整场逐字稳定，system 前缀不再因环节变化而跳变，DashScope 前缀缓存才能命中。

改动要点：
- `phaseDirective`（开场母版）/ `transitionDirective`（讲解回归母版）收集后，合并进 `packet["环节指令（仅本段生效，不作为商品事实）"]`。
- system 消息恒为 `foundation + fixed`（stable JSON），不再 append 环节内容。

---

## 2. 验证状态（本机已完成）

- `go build ./...` 通过（Go 1.27.1，go.work 多模块）。
- `go test ./internal/speechbase/ ./internal/httpapi/` 通过（含 P0 敏感词测试、P0 view 测试）。
- Linux 交叉编译通过：`GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o management-service ./cmd/management`（产物 ~34MB）。

---

## 3. 未解决的遗留问题（如实告知，需 Codex 判断是否一并修）

实测发现 system 消息数在 **3 和 4 之间跳变**（n=4 的段多一条 3717 字符的「总约束」= `TextPromptContract` 的 body = text-generation-foundation.md + 适配母版）。

根因链路（三层 prepend 叠加 + 早退条件）：
1. `speech_models.go:190`：`p.SystemPrompt != ""` 时 prepend（模型池 system_prompt = 83字符「链接纠正」）。
2. `completeCycleText`（text_generation.go:315 附近）：`profile.SystemPrompt != ""` 时 prepend。
3. `PrepareTextRequest`（text_profiles.go:114）：`r.TextPromptSHA == ""` 时 prepend body（3717「总约束」）。

三层叠加导致 foundation「总约束」时而出现（n=4）时而不出现（n=3）。**这个 3/4 跳变是缓存失效的另一个根因，本次 C 方案未触碰**（改它牵涉 `TextPromptSHA` 赋值时机 + 多层 prepend 的去重，风险高）。

**建议**：本次先发布 A+C（renderCycleTextMessages 部分），观察缓存命中率是否提升；prepend 层 3/4 跳变作为后续独立任务排查，不要在本次混改。

---

## 4. 发布方式

这是**二进制发布**，不是母版增量：

1. 用 `7c4405f` 代码重编译 management-service Linux 二进制（命令见上）。
2. 走 Codex 的二进制发布流程（`capture_baseline.py` 抓基线 → 打包 bin/management-service + 必要的母版 → `deploy_release.py` 部署，需重启 Management）。
3. **注意**：本次改了 Go 代码，`prepare_deploy.py` 的母版 diff 断言不适用；要按「二进制替换」流程处理，不能用「母版增量不重启」的结论。

部署后验证：
- `live_model_exchanges.cached_input_tokens` 应从 ~2% 提升（预期 stable JSON 命中缓存）。
- `有效P0` 在 request_json 里的字符数应从 3460 降到 ~2200。
- 回归：开场/正常/讲解三类话术正常生成，P0 拦截不退化（退回率不升）。

---

## 5. 一句话总结

> A 瘦身有效P0（删 4 个元信息字段，3460→~2200字符）+ C 把开场/讲解母版移入 user 稳定 system 前缀，代码已编译测试通过，提交 7c4405f。需重编译二进制+重启，不能套用母版增量不重启流程。prepend 层 3/4 消息跳变是遗留 bug，建议后续单独修。
