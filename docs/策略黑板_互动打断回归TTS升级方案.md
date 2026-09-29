# 策略黑板：互动、打断、回归、称呼、仿真人与 TTS 升级方案

> 项目：小蓝直播搭子  
> 目标：把直播间事件处理升级为“单次完整口播任务”，统一串联互动策略、打断策略、回归策略、称呼策略、仿真人表达和 TTS。  
> 本文件只定义升级方案，不修改现有业务代码。

---

## 1. 核心目标

本次升级不再把“互动策略、打断策略、回归策略、称呼策略”当成互相独立的功能，而是统一为一条完整业务链：

```text
直播间事件
  ↓
互动策略：决定现在要不要说、处理哪件事
  ↓
打断策略：决定怎么从当前主线出去
  ↓
回归策略：决定互动结束后怎么回到主线
  ↓
称呼策略：决定本轮有称呼 / 无称呼、称呼谁、可用什么称呼
  ↓
仿真人 / 表达策略：决定情绪、语速、口语、停顿、节奏
  ↓
策略黑板汇总全部决策
  ↓
最终话术生成器一次性生成完整可播正文
  ↓
审核 / 事实校验
  ↓
TTS
  ↓
Core 音频调度与分发
  ↓
回归主线
```

最重要的原则：

> **所有前置策略只做决策、约束和上下文准备，不分别生成几段文字再拼接。最终只能由一个“最终话术生成器”一次性生成完整口播。**

这样才能避免：
- 模板拼接感；
- 打断突兀；
- 回归生硬；
- 回答结尾与主线重复；
- 称呼硬塞在句头；
- 每次都“宝子/老板/朋友”；
- 仿真人变成固定口癖模板。

---

## 2. 当前已有基础

### 2.1 互动时间窗

当前：
`core-service/internal/paidpipeline/interaction_scheduler.go`

已经实现：
- 事件累计；
- 最小间隔；
- 最大等待；
- 批量聚合；
- TTL；
- 优先级。

当前典型窗口包括：
- 回复弹幕：约 12 秒；
- 回应关注：约 45 秒；
- 回应点赞：约 60 秒；
- 点名欢迎：约 10 秒最大等待；
- 打包欢迎：约 35 秒最大等待。

这部分保留，并升级成 **Interaction Mission Builder**。

### 2.2 打断安全切点

当前：
`core-service/internal/httpapi/audio_runtime.go`

已经有：
- 主线当前播放位置；
- 安全句末切点；
- 回答 / 抢答两种切入；
- 最后语义段处理；
- 插播期间暂停主线；
- 插播完成后恢复。

这部分继续保留，但必须把“打断前上下文”写入策略黑板。

### 2.3 回归策略

当前已有：
- `DIRECT`
- `BRIDGE`
- `FUSION_SKIP`
- `CROSS_RESUME`
- `RE_ANCHOR`
- `SWITCH_PLAN`

并且已经有回归去重逻辑。

这部分升级为正式的 `ResumePlan`。

### 2.4 称呼策略

当前：
`core-service/internal/strategycenter/store.go`

已有：
- 系统称呼；
- 自定义称呼；
- 启用/禁用；
- 权重；
- `PickAddressing`。

但当前更像“独立选一个称呼”，后续必须升级成“最终生成约束”，不能程序硬拼。

### 2.5 完整口播任务雏形

当前 paid pipeline 已经存在类似提示：

> 这是一次完整口播任务。结合当前主线、打断策略、回归策略和其它生成约束，一次生成最终可播正文。

这正是本次升级的基础。

---

## 3. 统一核心对象：SpeechMission 策略黑板

建议新增：

`core-service/internal/speechmission`

每一次因为直播间事件需要主播开口，都创建唯一的 `SpeechMission`。

建议结构：

```go
type SpeechMission struct {
    ID        string
    TenantID  int64
    RoomID    int64
    CreatedAt time.Time

    EventContext    EventContext
    Interaction    InteractionPlan
    Interrupt      InterruptPlan
    Resume         ResumePlan
    Addressing     AddressingPlan
    HumanStyle     HumanStylePlan
    Mainline       MainlineContext
    Generation     GenerationRules

    GeneratedText  string
    TTSDirective   TTSDirective

    State           MissionState
    Trace           MissionTrace
}
```

策略黑板的作用不是保存“几率”，而是完整保存：

- 为什么本轮要开口；
- 处理了哪些事件；
- 为什么此时打断；
- 从哪里打断；
- 打断前刚刚说了什么；
- 回答后要回哪里；
- 哪些内容不能重复；
- 这次要不要称呼；
- 称呼谁；
- 可用什么称呼；
- 是否需要真人化口语；
- 最终生成了什么文字；
- TTS 任务是什么；
- 最后实际恢复到哪里。

---

## 4. 互动策略升级

互动策略只回答两个问题：

1. **现在有没有必要说话？**
2. **本轮处理哪些事件？**

它不负责生成正文。

建议输出：

```text
InteractionPlan {
    primary_action
    primary_event
    merged_events
    priority
    response_goal
    user_focus
    must_handle_before
}
```

### 4.1 事件类型

保留并扩展：
- `reply_chat`
- `reply_follow`
- `reply_like`
- `welcome_named`
- `welcome_batch`

后续可增加：
- 礼物回应；
- 下单回应；
- 高频问题聚合；
- 低热度主动互动；
- 特殊运营事件。

### 4.2 概率不再是唯一触发依据

强约束：
- 最大等待；
- 最小冷却；
- 事件是否还有效；
- 是否可处理；
- 当前是否有更高优先级任务；
- 是否已经有 TTS 正在播。

软选择：
- 多个候选任务同时成立时的权重；
- 多样性；
- 当前热度；
- 近期是否刚处理过同类事件。

原则：

> 事件到了最大等待时间，应该形成任务，不应因为随机概率没命中而永久丢失。

---

## 5. 打断策略升级

打断策略只负责：

> **怎么从当前主线出去。**

建议输出：

```text
InterruptPlan {
    mode
    cut_at_ms
    cut_segment_id
    before_sentence
    current_topic
    current_product
    semantic_state
    urgency
}
```

最关键字段：
- `before_sentence`
- `current_topic`
- `current_product`

因为最终话术生成器必须知道“主播刚刚说到哪里”。

### 5.1 打断方式

可以保留当前内部候选，但语义统一为：
- 等完整句结束；
- 快速句末切入；
- 紧急事件立即切；
- 短暂停顿后进入；
- 顺着当前内容自然带出去；
- 当前不适合打断，继续排队。

### 5.2 内部策略名不对外暴露

例如：
- hard_cut
- bridge
- cross_resume
- A/B 轨
- track

全部属于内部实现。

---

## 6. 回归策略升级

回归策略回答：

> **互动结束以后，主播从哪里、怎么重新进入主线。**

建议：

```text
ResumePlan {
    strategy
    resume_at_ms
    resume_segment_id
    resume_topic
    mainline_before
    mainline_after
    need_bridge
    bridge_goal
    forbidden_repeat
    skip_segments
}
```

### 6.1 DIRECT

互动很短，主线语义没断，直接继续。

### 6.2 BRIDGE

互动正文末尾包含自然桥接。

例如系统只告诉模型：

> 回答完以后自然带回“压榨工艺”，不要重复刚刚说过的话。

桥接文字必须和完整互动正文一次生成。

### 6.3 FUSION_SKIP

如果互动回答已经覆盖了主线后面准备说的内容，就跳过重复部分。

### 6.4 CROSS_RESUME

互动时间较长，原打断点已经失去语境，则跨过若干语义段，从新的安全入口进入。

### 6.5 RE_ANCHOR

主线仍值得继续，但上下文断裂，先重新建立一个短锚点，再继续。

### 6.6 SWITCH_PLAN

原主线已经不适合继续时，切换下一主线段或下一方案。默认谨慎使用。

### 6.7 回归必须在生成前决定

正确流程：

```text
先确定 ResumePlan
  ↓
把 ResumePlan 交给最终话术生成器
  ↓
再生成完整互动正文
```

不能等 TTS 生成完以后才决定桥接。

### 6.8 去重

继续保留现有回归去重，并升级为：
- 文本重复；
- 语义重复。

比较：
- 互动回答尾部；
- 回归目标前后句；
- 主线刚刚已经播过的内容。

---

## 7. 称呼策略升级

称呼策略不输出“句子前缀”，只输出约束。

建议：

```text
AddressingPlan {
    enabled
    should_address
    target_user_id
    target_nickname
    candidates
    recently_used
    max_occurrences
    natural_only
}
```

### 7.1 必须允许三种结果

1. 使用昵称；
2. 使用泛称；
3. 完全不使用称呼。

“无称呼”是正常结果，不是异常兜底。

### 7.2 选择依据

综合：
- 是否明确针对某个人；
- 互动类型；
- 当前热度；
- 最近是否已经频繁点名；
- 最近是否已经用了相同称呼；
- 当前主播风格；
- 本轮是否需要增强回应感。

### 7.3 禁止硬拼

错误：

```text
"老板，" + generated_answer
```

正确：

```text
AddressingPlan 作为生成约束交给模型
```

由模型自然决定：
- “小王这个问题问得刚好……”
- “这个问题我给大家说一下……”
- 或者完全不称呼。

---

## 8. 仿真人 / 表达策略

建议新增：

`core-service/internal/speechstyle`

这一层只决定“怎么说”，不决定事实。

建议：

```text
HumanStylePlan {
    emotion
    energy
    pace
    filler_level
    pause_style
    sentence_length
    self_correction
    repetition_level
    address_tendency
    call_to_action_level
}
```

允许适量：
- “嗯”
- “对”
- “这个”
- “其实”
- “我给你说一下”
- 自然短句；
- 轻微停顿；
- 轻微重复；
- 极低频自我修正。

但必须有近期冷却：
- 不能每段都“嗯”；
- 不能每段都“家人们”；
- 不能每次都点名；
- 不能固定频率制造口误。

真人化是软约束，不是模板。

---

## 9. 最终话术生成器

建议新增：

`core-service/internal/speechcomposer`

这是整条链路唯一允许生成最终主播文字的模块。

输入：

```text
SpeechMission 黑板
+ 当前直播方案
+ 事实依据
+ 规则层
+ 行业层
+ 用户层
+ 主播风格
+ 近期说话历史
```

输出：

```text
GeneratedSpeech {
    text
    semantic_sections
    bridge_range
    addressed_user
    emotion_hint
    estimated_duration
}
```

### 9.1 一次生成完整正文

一次生成内容可同时包含：
- 自然进入互动；
- 回应事件；
- 必要的事实回答；
- 必要称呼；
- 真人口语；
- 自然收尾；
- 必要的回归桥接。

不允许多个策略分别生成后再字符串拼接。

### 9.2 Prompt 表达方式

Prompt 告诉模型“行为约束”，不告诉内部策略代码名。

示例：

```text
主播刚刚讲到：……
现在需要回应：……
本轮主要目标：……
如果合适可以称呼“小王”或“朋友”，但不要强行使用称呼。
互动结束后需要自然回到“压榨工艺”。
不要重复刚刚已经说过的：……
整体语气自然，像真人主播临场回应。
允许少量口语和短停顿，不要刻意制造固定口癖。
只输出一整段可以直接播出的正文，不解释策略。
```

### 9.3 禁止内部信息泄露

最终正文禁止出现：
- 打断策略；
- 回归策略；
- BRIDGE / DIRECT；
- Prompt；
- 系统规则；
- Agent；
- TTS；
- 双轨；
- A/B；
- mission_id；
- 队列；
- 调度器。

---

## 10. 文本审核

最终文字送 TTS 前走轻量校验：

```text
生成正文
  ↓
内部信息泄露检查
  ↓
平台 / 合规检查
  ↓
事实依据检查
  ↓
用户纠正记忆检查
  ↓
重复检查
  ↓
长度检查
  ↓
TTS
```

如果只是语言不自然，可快速重写一次。

如果事实冲突，不允许模型自行编造。

---

## 11. TTS 升级

TTS 不只接收文字，还支持可选表达参数。

建议统一：

```text
TTSRequest {
    text
    voice_id
    emotion
    pace
    volume
    pause_profile
    provider_options
}
```

如果供应商支持：
- 情绪；
- 语速；
- 韵律；
- 停顿；
- 强度；

则映射 `HumanStylePlan`。

如果不支持：
- 忽略对应参数；
- 不影响文本链路。

继续保持主线与插播统一音色 / 克隆音色。

---

## 12. 状态机

建议统一：

```text
CREATED
  ↓
PLANNING_INTERACTION
  ↓
PLANNING_INTERRUPT
  ↓
PLANNING_RESUME
  ↓
PLANNING_EXPRESSION
  ↓
GENERATING_TEXT
  ↓
VALIDATING_TEXT
  ↓
SYNTHESIZING_TTS
  ↓
WAITING_CUT_POINT
  ↓
PLAYING
  ↓
RESUMING_MAINLINE
  ↓
COMPLETED
```

异常状态：
- `FAILED`
- `CANCELLED`
- `EXPIRED`
- `SUPERSEDED`

其中 `SUPERSEDED` 用于：
- 低优先级欢迎任务被更高优先级问题替代；
- 任务被合并；
- 已无处理价值。

---

## 13. 同房间并发与队列

同一个直播间：

> **同一时间只能有一个前台互动口播任务处于 PLAYING。**

其它任务排队。

排序不能只按 FIFO，建议综合：

```text
硬优先级
+ 等待时长
+ 最大等待逼近程度
+ 事件价值
+ 是否可以合并
+ 最近同类冷却
```

抢答可以提高优先级，但不能覆盖已经播放中的完整互动 TTS。

---

## 14. 黑板生命周期

### 创建

互动时间窗形成任务时创建。

### 规划

依次写入：
- InteractionPlan
- InterruptPlan
- ResumePlan
- AddressingPlan
- HumanStylePlan

### 冻结

最终文本生成前冻结核心字段：
- 主要事件；
- 打断点；
- 回归目标；
- 称呼约束；
- 事实上下文。

### 播放期

只追加运行事实：
- TTS 完成；
- 实际播放开始；
- 实际播放结束；
- 设备确认；
- 实际恢复位置；
- 异常原因。

### 完成

保存 Trace，供：
- 审计；
- 统计；
- 排错；
- 后续策略学习。

---

## 15. 日志与可观测性

每一个 Mission 至少记录：

```text
mission_id
room_id
trigger_events
interaction_plan
interrupt_plan
resume_plan
addressing_plan
human_style_plan
generated_text
tts_task_id
switch_at_ms
resume_at_ms
play_started_at
play_completed_at
resume_completed_at
failure_reason
```

后台后续可增加“本轮策略黑板”抽屉：

```text
为什么触发
→ 处理了什么事件
→ 为什么此时打断
→ 在哪里打断
→ 为什么这样回归
→ 是否使用称呼
→ 仿真人策略
→ 最终生成正文
→ TTS 状态
→ 实际恢复位置
```

终端用户不显示内部代码名。

---

## 16. 统计升级

当前“概率命中次数”不应继续作为互动策略核心统计。

建议增加：
- 事件累计数；
- 形成任务数；
- 合并事件数；
- 实际播出次数；
- 超时任务数；
- 平均等待时间；
- 冷却抑制次数；
- 高优先级覆盖次数；
- 回归成功次数；
- 回归去重次数；
- 生成失败次数；
- TTS 失败次数；
- 平均互动口播时长。

概率统计保留为内部调试指标。

---

## 17. 热更新

以下内容应支持直接热生效，不要求重新生成主线音频：
- 互动策略；
- 称呼策略；
- 仿真人表达；
- 事实依据；
- 用户纠正；
- 主播风格；
- 部分回归偏好。

原则：

```text
正在执行的 Mission 使用冻结版本
新 Mission 使用最新版本
```

不能播放到一半被新策略改写。

---

## 18. 数据兼容

现有：
`live_strategy_center_configs`

可继续承担策略配置。

V1 不要求立即大规模迁表。

运行态后续可新增：
- `core_speech_missions`
- `core_speech_mission_events`
- `core_speech_mission_trace`

初期也可先：
- 当前 Mission 放内存；
- 最近状态放 Redis；
- 完整 Trace 进日志 / MySQL。

---

## 19. 失败降级

### 最终文本生成失败
- 不打断主线；
- 最多快速重试一次；
- 低价值任务过期丢弃；
- 高价值问题重新入队。

### 回归策略失败
降级：
- 最近安全语义点；
- 去重后的 DIRECT / SAFE。

### 称呼策略失败
降级：
- 不使用称呼。

### 仿真人策略失败
降级：
- 普通自然口语。

### TTS 失败
- 记录失败；
- 释放 Mission；
- 恢复主线；
- 禁止房间长期卡在 suspended。

---

## 20. 建议代码结构

### 保留

```text
core-service/internal/paidpipeline
core-service/internal/strategycenter
core-service/internal/httpapi/audio_runtime.go
core-service/internal/coreaudio
core-service/internal/roomaudio
core-service/internal/speechruntime
core-service/internal/director
```

### 建议新增

```text
core-service/internal/speechmission/
    mission.go
    registry.go
    state.go
    trace.go

core-service/internal/speechplanner/
    interaction.go
    interrupt.go
    resume.go
    addressing.go
    human_style.go

core-service/internal/speechcomposer/
    composer.go
    prompt.go
    validator.go
```

TTS 如果现有抽象已经足够，不重复新建模块，只增加统一 `TTSDirective` 映射层。

---

## 21. 现有模块改造方向

### paidpipeline/interaction_scheduler.go

从：
> 生成 Candidate / Decision

升级为：
> 生成 InteractionPlan，并创建 SpeechMission。

### strategycenter/store.go

继续负责：
- 配置；
- 权重；
- 启停；
- 统计。

但不负责整个实时口播任务的编排。

### httpapi/audio_runtime.go

逐步把：
- 打断决策；
- 回归决策；
- 去重；

抽离到 `speechplanner`。

最终主要负责：
- API；
- 编排调用；
- 音频运行时交接。

### coreaudio / roomaudio

继续只负责：
- 主线；
- 插播；
- 播放；
- 暂停；
- 恢复；
- 分发。

不要把语言策略塞进音频层。

---

## 22. 分阶段实施

### 第一阶段：先建统一黑板

目标：
- 建 SpeechMission；
- 把现有互动、打断、回归、称呼决策都写入同一个对象；
- 先不改变现有播放行为；
- 完整 trace 可查。

验收：
> 一条直播事件从进入 Core 到最终播放，都可以通过同一个 mission_id 追踪。

### 第二阶段：最终文字统一生成

目标：
- 所有策略只提供约束；
- 新建 Final Speech Composer；
- 称呼不再硬拼；
- 回归桥接和互动正文一次生成。

验收：
> 一个 Mission 只有一份最终可播正文。

### 第三阶段：回归全面接入

目标：
- ResumePlan 必须生成前确定；
- BRIDGE 真正进入正文；
- FUSION_SKIP / CROSS_RESUME / RE_ANCHOR 统一结构；
- 去重结果写回黑板。

验收：
> 不再出现互动最后一句与回归主线第一句明显重复。

### 第四阶段：仿真人

目标：
- HumanStylePlan；
- 称呼冷却；
- 口语冷却；
- 情绪 / 节奏；
- TTS 支持则映射。

验收：
> 连续多次互动不能形成固定开头、固定称呼、固定口癖模板。

### 第五阶段：可视化与统计

目标：
- 后台实时查看 SpeechMission；
- 互动统计改为真正任务统计；
- 支持按 mission_id 排错。

---

## 23. 测试场景

至少覆盖：

1. 单条弹幕 → 正常打断 → 回答 → 直接回归；
2. 弹幕 + 关注同时发生 → 合并处理；
3. 连续大量点赞 → 聚合一次回应；
4. 高频进房 → 禁止连续点名；
5. 低热直播间 → 适当增强互动；
6. 高热直播间 → 压低非必要欢迎；
7. 当前主线没有合适切点 → 排队；
8. 互动 TTS 正在播 → 第二任务不得覆盖；
9. BRIDGE → 桥接必须包含在最终正文；
10. 回答覆盖后续主线 → FUSION_SKIP；
11. 回答尾部与回归主线重复 → 去重触发；
12. 称呼策略结果为“无称呼”；
13. 有昵称但模型自然不使用；
14. TTS 不支持 emotion → 正常降级；
15. TTS 失败 → 主线正常恢复；
16. 策略热更新 → 当前 Mission 不变，新 Mission 生效；
17. 方案切换 → 新 Mission 使用新方案；
18. 生成结果试图泄露内部信息 → 拦截；
19. 直播结束 → 清理所有待处理 Mission；
20. 主线末尾发生互动 → 正确进入下一安全主线。

---

## 24. 验收标准

### 业务验收

- 每次主播因为互动事件开口，都有唯一 SpeechMission；
- 一个 Mission 可以合并多个事件；
- 每轮明确记录为什么说、怎么打断、怎么回归；
- 回归必须在文本生成前决定；
- 称呼允许有，也允许没有；
- 真人化不是固定模板；
- 最终只生成一整段完整可播正文。

### 技术验收

- 策略层、生成层、音频层职责分开；
- 可通过 mission_id 追踪完整链路；
- 同房间插播严格串行；
- TTS 失败不会卡死主线；
- 热更新只影响新任务；
- 回归重复有防护；
- 不破坏现有主线播放链。

### 听感验收

目标听感：

> 主播正在讲主线 → 直播间发生值得回应的事件 → 主播自然停下来回应 → 可能有称呼，也可能没有 → 可能带一点真人口语和情绪 → 回答完成后自然接回原主线。

不能再是：

> 主线 → 模板欢迎 → 模板回答 → 模板桥接 → 主线。

---

## 25. 最终架构结论

本次升级最终要把系统从：

```text
多个独立策略
+ 概率抽取
+ 独立输出
+ 后端拼接
```

升级为：

```text
事件驱动
→ 单次 SpeechMission
→ 多策略共同决策
→ 策略黑板汇总
→ 一次性生成真人化完整口播
→ TTS
→ Core 播放
→ 智能回归主线
```

一句话定义：

> **有事发生，先决定要不要说；再决定怎么出去；再决定怎么回来；再决定怎么像真人说；最后一次生成一整段话，送给 TTS。**

本文件作为后续互动策略、打断策略、回归策略、称呼策略、仿真人表达和 TTS 链路升级的统一技术基线。
