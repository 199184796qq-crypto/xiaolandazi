# Learning log

## 2026-09-22 — initial distilled state

### Evidence

- User provided a long-form真人直播 sample centered on a皖南土黄鸡 product story.
- Early system output was described as link-heavy and dry.
- After separating style sample, product facts, and prompt rules, user judged the result around 60/100.
- After switching from forced seven content stages to one-pass continuous speech with runtime-only seven audio chunks, user judged a later result 75/100.
- User explicitly stated that reasonable inference is acceptable; the main concern is avoiding absolute claims and advertising-law high-risk wording.

### Stable rules promoted

- Product is the main character; links are actions, not the content spine.
- Learn style from samples but keep sample facts isolated.
- Generate one continuous speech first; chunk only for playback.
- Do not regress later portions into parameter/link recital.
- Allow mild inference with hedged language; block strong certainty and unsupported claims.
- Keep advertising-compliance language checks active.
- Preserve smooth TTS/playback behavior while tuning content unless a runtime defect appears.

### Open quality gap after 75/100

Focus on:

- more consistent human tone from beginning to end;
- richer use of verified product material without formulaic transitions;
- more natural late-stage CTA;
- less AI-style explanatory framing;
- maintaining factual richness when the guard edits risky sentences.


## 2026-09-22 — 稳定骨架 + 动态表达落地

### 新规则

- 同一商品每轮保持大致相同的故事骨架，不重新发明整套结构。
- 每轮轮换开头方式、重点素材、句子节奏、承接方式和收口方式。
- 最近完整口播作为“负参考”，核心事实可以重复，但长句、开头、承接和收尾尽量不要复刻。
- 每轮只重点展开2~3组素材，其余真实信息简洁带过，保证“同一套商品逻辑，但听起来不是同一篇稿子”。
- 变化不能依靠编造“后台私信、刚有人问、评论区都在问”等伪实时互动。
- 为保持直播延迟稳定，不额外增加第三次“改写模型”调用；变化在首轮生成中完成，相似度只做观测和追踪。
