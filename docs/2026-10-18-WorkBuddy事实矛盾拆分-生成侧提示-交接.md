# 问题「遇到矛盾拆成两个事实单独讲」—— WorkBuddy 落地（生成侧提示版）

> 日期：2026-10-18
> 作者：WorkBuddy
> 交付对象：Codex（知晓本次改动，避免重复实现 / 冲突）

---

## 一句话

新增「同侧面事实矛盾拆分」：生成话术前，把当前侧面下的专属事实按「分类维度 / 数值对立」拆成多组，并提示口播模型**本段只讲一条主线维度，其余维度留待后续段单独讲**，避免把互斥信息（如「6人餐」vs「适合3-4人」）揉进同一段。

## 背景与取舍（已和用户确认）

用户最初提「调度生成时在线拆矛盾」，后明确改为「生成内容时候」处理，且**不额外调模型**（避免每段多一次付费模型调用）。WorkBuddy 拍板采用**纯规则、零模型成本**的拆分依据：按事实 `category`（分类维度）+ 同分类内数值对立检测。

## 改动文件

| 文件 | 改动 |
|---|---|
| `management-service/internal/anchorcycle/fact_conflict_split.go`（新增） | `SplitConflictingFacts(facts []HookFact) [][]HookFact` 纯函数：按分类聚合、组内按数值对立再拆、通用事实组放最后；`extractNumbers`/`numbersConflict` 辅助；`State.FissionFactConflictHint()` 返回生成约束提示（多组时）。 |
| `management-service/internal/anchorcycle/fact_conflict_split_test.go`（新增） | 6 个单测：单条/空、按分类拆、同分类数值对立拆、区间包含不拆、通用事实放最后、数字提取。 |
| `management-service/internal/httpapi/text_generation.go` | `renderCycleTextMessages` 在「本次切入侧面」之后，注入 `packet["事实维度提示"] = state.FissionFactConflictHint()`（非空时）。 |

## 拆分规则（纯函数语义）

1. 空 / 单条 → 不拆。
2. 按 `CategoryFromFactText` 提取分类聚合；不同分类各成一组；无分类（通用事实）单独一组放最后。
3. 同分类组内，两条事实数值「互相否定」时再拆（`numbersConflict`：区间完全分离，或代表值差 2 倍以上）。
4. 区间包含关系（6 是上限、3-4 也合适）**不算**矛盾，不拆。

## 生成侧行为变化

- 只在 `FissionFactConflictHint()` 非空（即当前侧面素材可拆成多组）时，往 user 消息 `packet` 里加一条 `事实维度提示`。
- 其余情况（单组 / 无侧面 / lecture）行为完全不变，不破坏现有生成链路。
- **不引入新 State 字段、不碰调度、不碰 delivery quota 相关文件**，与 Codex 的采纳/送播改动零文件冲突。

## 验证

- `go build ./internal/anchorcycle/ ./internal/httpapi/` 通过。
- `go test ./internal/anchorcycle/` 全绿。
- `go test ./internal/httpapi/` 全绿。

## 未做（留给后续 / Codex 决定）

- **真正的「拆成两段各自生成」**：目前只做「生成时提示本段讲一条主线、其余留待后续」。若需把其余组**强制拆成独立段落**逐段生成（需要状态记忆 + 调度改动），等 delivery quota 收尾后再评估，避免并写 state.go。
- 保存侧的 `factConflictCheck`（方案 C 模型判矛盾，仅 warning 不拆）本次未动，仍是独立能力。
