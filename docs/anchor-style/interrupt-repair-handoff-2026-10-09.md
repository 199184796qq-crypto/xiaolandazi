# 新聊天交接：打断修复第一批

## 最新发布：2026-10-09 17:44，北京时间

**已部署** `20261009-174056-product-content-pools`；监控仍为 PAUSED。后文“尚未部署”等是历史状态，以本节为准。

本次实现商品各自专属内容池、显式适用范围的公共内容池，以及任意多福利的单个/多个/全部商品范围。数据库触发器仅记录资料修订，不调用模型。直播策略顶部累计净变化，用户点击“更新受影响内容”才生成/核对受影响池，未变化池复用；核对并保存智能体后整体冻结快照，下次开播采用，本场保持原版本。稳定任务编号与事实/商品关联写数据库，显示名称不再作为关联键。并发领取、防迟到覆盖和失效重试已验证。只换声音沿用原内容快照。

旧线上 version57 原轮盘19与保存范围有40条匹配任务，现按该版本原轮盘恢复选择；真实只读验证首轮 ready。源方案与版本57均未重写，未替用户生成/采纳新内容、启动直播或声音。线上新内容集和更新任务仍为0，页面显示待用户更新迁入新池。

验证：完整后端测试/vet、前端生产构建、新功能浏览器回归、20商品/601任务、实际MySQL隔离表20商品/3福利/8并发CAS等通过；真实模型的20商品合成归属及公共/私有生成核对协议通过，关闭思考。前端全套74/79，5项为修改前已存在的操作栏/试听字符串断言，未改其实现。部署后44条实际来源事实原子都对应真实资料编号，旧版本保持57；10张新表、21个触发器已安装。三个服务active，公开页面200及4个入口JS/CSS哈希匹配。

源码实现 `c12f042`，发布构建 `025f9b2d25203571c87f86b24ea28de30dfd8ebf`（vcs.modified=false）。Management SHA：`24fd7ebed957d97780334a5967ba2e57ffe547397aaeba5847966c5780b274bc`。Core与Gateway及客户/销售页面保持上一版，62份完整母版已核对。仅Management于17:43:56重启，发布前两次确认零运行场次/Core音频idle。回退目录 `/opt/xiaolan/releases/20261009-154439-historical-failure-ui`，旧二进制哈希未改变；回退服务可保留新增数据结构。原E盘其他任务改动保留，新增 live-content-agent-foundation.md 是其他任务未发布文件，本次没有采纳/覆盖。

操作：刷新网页后，旧方案可以直接开始测试首轮修复；采用新池则点击“更新受影响内容”→核对公共/商品专属预览与福利范围→保存智能体→开始。实际扬声器播放及完整互动回归等待用户测试，不能把首轮调度验证当成实际播放完成。不得自行恢复监控。

## 补充发布（2026-10-09 15:44，北京时间）

当前版本：`20261009-154439-historical-failure-ui`，保留上一版全部方案快照修复。

用户要求“先不要监控”，定时监控已暂停，不得因用户继续测试自行恢复。

用户第二张“主线声音生成失败”弹窗经诊断仍对应15:09的旧失败场次221，发布后没有新的启动记录。页面首次调阅历史停止状态会重新弹错误，服务重启后旧错误详情不在临时缓存又显示为通用停止说明。前端已修复：首次加载历史失败不弹窗、不当成本场错误；新场次失败、正在运行的场次转为失败仍正常提示，重复轮询或错误说明变化不会重复弹。

3项回归（包括实际页面refreshRuntime函数）和页面构建通过，公开网页200且新JS哈希与发布包一致。仅更新桌面页面，未重启任何服务、未启动用户直播、未改写旧场次或方案。源码提交：`a1721cd`。

## 最新发布状态（2026-10-09 15:36，北京时间）

已上线，当前版本：`20261009-153446-plan-workspace-notices`。后文“尚未部署”等说明是此前交接时的历史进度，以本节为准。

- 互动执行与回答→可选短桥→实际主线回归已在15:04发布；本次修复方案一致性检查的位置。
- 用户最新决定：校验放直播策略调阅智能体方案时，标注通知即可。本场继续使用保存快照；事实、福利或轮盘来源变化不再停止声音。新内容在下次保存、下次开播采用。
- room26 V14/version57 原故障：两个保存快照的事实SHA不同，正文仅事实项标题“算账口径-老鸭”改为“算账”。24条商品范围事实已经只读核对，全部兼容；保存方案数据库没有被改写。
- 兼容改名只调整本场副本，不继承不同价格、链接或条件的归属。无法对应的事实仅提示并不进入商品专属讲解。只换声音的保存路径已同步采集安全事实与商品范围，避免产生新错配。
- 源码提交：`2a43aee9eb74f523fb17f56baf579d0b37d75e29`；前序互动执行提交：`cda976d5571750d3b5410ff616e49ca0a90fc8db`、字段兼容提交：`58257edbce33c7ea1a5df7fdb16b5dd22ae990b4`。
- 独立源码目录：`C:\Users\19918\Documents\Codex\2026-10-08\e-95-failed-to-fetch-oss\work\interrupt-phase1-source`。E盘原仓库的其他任务改动保留。
- 本次后端完整测试和vet、前端构建及本次相关5项页面测试通过；读取实际线上方案检查用时30ms。前端完整套件71/76通过，余5项在修改前基线上同样失败，属于操作栏文字断言与旧试听实现断言；未借本次修复更改这些其他任务实现。
- 两次发布前确认零运行场次和Core音频idle。当前三个服务active，网页200且加载新提示界面，Core/Gateway及客户/销售页面不变，59份母版完整核对。上一版文件没有被覆盖。
- 回退目标：`/opt/xiaolan/releases/20261009-150439-interrupt-execution-return`；回退该版本会恢复这次方案差异停音问题。
- 用户可以刷新页面直接开播测试，无需为这次事实标题改名重存方案。监控每2分钟只读检查，首次实际互动完整回归或新的异常才通知；旧场次220/221的已知错误不重复报告。
- 内容丰富度及运行时新角度选择依用户决定留到互动执行顺畅、完成率验证后；不要误报已完成内容优化。新场次真实播放仍待用户开始测试，不把只读核对、声音生成或下发当成已播放完成。

更新时间：2026-10-09。此文件用于直接接续实施，不需要重新讨论已确认原则。

## 1. 当前任务与真实进度

用户已授权：**先完成问题1、2、3及内容策略接入，完成验证，部署服务器；问题和解决方案落盘。** 既有系统继续保留，新内容方案只是附加策略，不得推倒原来的调度、卖点×角度轮盘、事实库、用户问答/纠正、打断安全点及回归机制。

最新用户要求是整理交接，准备换聊天。本聊天到交接为止停止实施，没有执行新部署。

真实进度：

- 问题清单、设计原则、本批范围及验收项已落盘。
- 第1、2项有第一版代码，**仍未完成完整回归**。
- 第3项及新的目的复用/任务选择接入仍未实现。此前已有的事实→目的×角度策略是另一批已上线功能，不能混为这批完成。
- 新代码尚未提交Git，尚未部署。不能说修复已经上线。
- 最近Core回归：HTTP API与coreaudio通过；agentdecision有1个失败，见第5节。

## 2. 必须使用的目录与Git状态

原项目：`E:\直播伴播`。先读 `E:\直播伴播\AGENTS.md`。

**本批业务代码都在独立Git worktree，继续修改这个目录：**

`C:\Users\19918\Documents\Codex\2026-10-08\e-95-failed-to-fetch-oss\work\interrupt-phase1-source`

原项目和此worktree当前HEAD均为：

`1079eea7303c97d612227fa41d97d1f069483481`

独立worktree为detached HEAD，由shell `git worktree add --detach` 创建。Codex app的create_worktree因本聊天无项目关联而失败，不需要重试或重新建树。本批6个业务文件修改和新切点文件仅在这个worktree，**原项目里没有这批业务代码**。

原项目有其他任务改动，保留：

- `docs/2026-10-09-房间状态统一维护与系统异常总表.md`（修改）
- `docs/2026-10-09-声音模型偏好保存表名修复.md`（未跟踪）
- `scripts/server/deploy-room-authority-overlay.sh`（未跟踪）

原项目中本聊天自己的未跟踪文件：两个讨论/实施文档及两个probe母版，见下文。不要使用reset、clean或覆盖他人修改，也不要把他人改动打包进本批。

用户早前要求过Git保存和部署；可以提交本批自己的改动。发布前重新核对原项目、当前生产版本，防止其他任务已推进。

## 3. 已确认设计，后续不要偏离

### 1：连续未来主线与切点

- 24秒是互动准备预算，不能因为当前音轨不足24秒就一直领不到任务。
- Core收到的是连续主线音流；可以由多个短文件组成，但准备窗口要跨音轨。
- 缓存要覆盖切点前文案、回归点文案、回归后一个完整语义单元；不能只带两句残片。
- 提前准备方案A/B/C：优质切点、普通切点、以完成任务为先的降级切点。
- **C默认仍是完整句末，不允许未经授权切半句。** 要记录实际切点级别及数量，不能把所有句末都算A。
- 严格按规则筛选互动；一旦入队争取条条完成，有条件选最佳，没有条件优先完成。

### 2：准备超过切点

- 原切点已过去时，主线继续走，不暂停等模型/TTS。
- 声音就绪后重新选后续安全切点，约3秒余量；不要重新等24秒或重生成同一回答。
- 可复用未播的正文与声音，但事实、纠正、场次/方案变化时要失效。
- 已准备声音需要有限保护期；不能无限续期，也不能因普通弹幕TTL轻易消失。
- 下播、取消、换场后的晚结果不能插入；一条回答完整结束并回归后才处理下一条。

### 3：问题识别与聚合

- 修复“发哪个快递”“生油还是熟油”等真实问题落入普通弹幕、很快过期的问题。
- 复用现有问题事件聚合器，保留事件ID与原话；不要再建第二套聚合器或重复入队。
- 直播**实际声音开始输出后**进入观察窗，默认120秒、后台可配置。期间可以统计聚合，但不产生互动队列、不互动。不能从点击开播或创建场次时间算起。
- 目的知识跨场次保留：第一次/歧义问模型，积累后语义高置信匹配；低置信或相近目的不能硬套。不是每条弹幕都做完整多轮模型分析。
- 精确重复问题先复用；语义向量查询仍有成本，不能宣传零成本。
- 长期知识按租户、房间、方案隔离；队列和执行状态只属于本场，不能用数据库恢复上一场任务。

### 内容策略接入

- 智能体首次生成时，可用模型初建目的×角度表；实播中逐步积累目的、角度及问题表达。
- 用户问题先判断与当前业务的关系，理解目的，再选择能回答的角度。
- 内容角度要有优先级、完整事实依赖、采用条件，以及必答/可选推进任务。条件由程序判断，不把排队和业务决策交给最后的口播模型。
- 价格问题先给清楚价格；有真实、有效且条件满足的优惠可以自然带上。不能编优惠，不要每个回答都机械逼单。
- 主线继续卖点×角度轮盘，将事实→目的×角度作为补充讲述策略，不能夺走原轮盘和直播大轮回。
- 最后口播使用**普通模型API，不走Agent**；目标是程序+提示词+线上模型能执行，不是Codex现场代写。
- 事实库的“事实内容 / 不要这样说 / 改成这样说”要完整送入。按意思自然改写，允许合理生活场景、用途和主观价值外延；禁止把写作指导念出来，不能增加未证实的价格、承诺或履约信息。
- 用户交互规则中的问答及自然语言纠正要兼容，优先于一般表达建议。主播风格是最后表达包装，不要强制成逐句硬模板。
- 新增或修改的大段母版都存 `configs/prompts/anchor-style/` MD，由程序加载；不要大段塞进Go常量。
- 组件协调用既有全局消息队长广播，各注册成员自行处理/丢弃，不回到一对一通知。

问题4（队首让位、准备/播放选择分离、准备并发上限）及问题5（完整分阶段期限、终止原因）留下一批；本批只做1/2必要的改动，不重写整套队列。

## 4. 已写第一版代码，逐项核对后继续

路径均相对独立worktree，最后状态经 `git status --short` 核对。

| 文件 | 已有改动 | 仍需处理 |
| --- | --- | --- |
| `core-service/internal/audioout/client.go` | 增加ProgramWindowTrack及RoomProgramSnapshot.FutureWindow，带track ID/index、连续相对偏移、时长、完整文字、timeline和safe points | 不改播放游标/协议含义；检查完整回归内容 |
| `core-service/internal/coreaudio/client.go` | sequential主线快照输出当前及后续已就绪音轨，最多32轨或未来约90秒；不返回额外audio URL | 新增跨轨快照验证；实际起播时刻尚未添加 |
| 新文件 `core-service/internal/httpapi/interaction_preparation_window.go` | 跨轨找24–60秒内切点；整理切前/回归上下文，轨末可取下一轨；返回A/B/C预案 | A/B/C选择仍初版，B/C可能同位置；实际C统计未完成；预案还未完整贯穿执行 |
| `core-service/internal/httpapi/agent_decisions.go` | 未准备任务改查连续future window，无上下文返回waiting_future_buffer；已准备声音改查近安全点；claim返回cut_plans | 最终实际执行仍依当前轨；准备上下文与重选后的实际回归可能不同，需验证 |
| `core-service/internal/httpapi/audio_runtime.go` | 声音生成后安全点窗口改为3–30秒，fallback到33秒，增加实际cut选择日志 | 原选点函数仍先等级后距离，可能不是真正最近；当前轨末被排除，跨轨实际切入尚未完整验证 |
| `core-service/internal/agentdecision/queue.go` | Item增加PreparedUntil；cut-reschedule第一次给2分钟保护且不再延长，保留原ExpiresAt；普通生成失败仍按原重试规则 | claim timeout等路径仍有原ExpiresAt逻辑；缓存本身也是2分钟，需对齐检查；修改失败的旧测试并补有限期限回归 |
| `management-service/internal/httpapi/live_agent_plan_voice.go` | 新生成音轨的句末分B，最后单元末分A，避免全A | 已发布旧音轨不会被这改动自动重标；需要真实质量/降级记录验证 |

以上Go文件已gofmt。尚未新增本批专用回归测试，不能只依现有测试宣布完成。

两个probe母版已复制到原项目和独立worktree，但**未接入生产**：

- `configs/prompts/anchor-style/interaction-task-angle-probe-analysis.md`
- `configs/prompts/anchor-style/interaction-task-angle-probe-speech.md`

## 5. 最近测试结果

在独立worktree的 `core-service` 执行：

```text
go test ./internal/httpapi ./internal/coreaudio ./internal/agentdecision
```

实际结果：

```text
ok livecompanion/core/internal/httpapi 3.528s
ok livecompanion/core/internal/coreaudio 2.108s
FAIL TestCutReschedulePreservesPreparedAnswerWithoutSpendingGenerationRetry
cut_reschedule_test.go:32: cut reschedule extended the event lifetime
FAIL livecompanion/core/internal/agentdecision
```

测试进程已经结束（session 94239已取完），不用再等。

原因：旧测试要求已准备声音也必须在原事件ExpiresAt之后消失；这次设计允许一次性声音保护期，同时保留原事件ExpiresAt。应把测试改成验证原ExpiresAt不变、保护期不被反复续长、到保护期终点必退出、普通生成失败仍受重试限制，而非简单删除断言。

Management本批代码尚未测试；真实模型、真实跨轨音流、切入回答及回归主线本批均未验收。

## 6. 下一步的代码入口与可用实现思路

先完成修复和测试，后部署。下面是调查所得，**只是可用接法，尚未实现，不必机械照搬**。

### 问题入口/聚合

- `core-service/internal/roombrain/manager.go`：HeuristicClassifier目前漏“哪个”“还是”“好久”“多钱”等口语；Classify为确定性无模型逻辑。
- `core-service/internal/basepipeline/pipeline.go`：免费采集/基础处理层，先brain.Ingest再Classify；不要在这里塞付费模型或阻塞采集。
- `core-service/internal/roomintel/pool.go`：原聚合器。Add中问句优先由questionTopic(Content)算topic，可能覆盖外部传入目的topic。若后续补判，要按原event ID修订，不能Add第二遍造成统计翻倍。
- `core-service/internal/paidpipeline/pipeline.go`：Handle区分negative/question/order/ordinary chat。paid边界可做语义/模型补判，但main中的events observer同步调用Handle，**长模型调用不能阻塞collector**。
- `core-service/internal/paidpipeline/interaction_policy.go`：enqueueDirectEvent；reply_question目前调用内存semantic topic resolver后qualifiedQuestionTopic。该resolver跨场不持久，物流家族过宽会误并快递公司/发货地/时效。
- `core-service/internal/paidpipeline/debt_scheduler.go`、interaction_scheduler.go：既有债务及聚合调度入口，需检查补判后的题目是否重复入队。

可考虑Management提供Core内网token保护的目的解析接口，异步、有界处理；精确复用→高置信语义匹配（要求候选分差）→新/歧义普通API补判。通过既有messagecaptain广播补判结果，原聚合器按event ID更新。晚结果要核对run epoch，防止换场插入。

### 目的/角度资产及最终生成

- `management-service/internal/contentstrategy/strategy.go`：已有Snapshot：完整Facts、Purposes、Rows。Render目前仅过滤所有事实依赖都在allowedFacts中的行，然后把全部剩余角度交给模型。缺程序选角度/优先级/条件/近期避重。
- `management-service/internal/httpapi/live_content_strategy.go`：初建分析、缓存及存储；通过普通API依次分析目的、角度，完整保存模型交换。明确disabled版本必须保持禁用，不自动覆盖用户方案。
- `management-service/internal/db/live_content_strategy.go`：已有live_plan_analysis_assets不可变知识资产。GetLatestPlanContentStrategy按tenant、plan、facts hash及场次开始前版本读取。
- `management-service/internal/semantic/store.go`：Service支持按tenant/room/plan/content_type持久索引、Search、Sync；可加interaction_purpose类型，复用semantic_documents。语义模型当前已配置，不要打印key。
- `management-service/internal/decisionexecutor/interaction_facts.go`：互动读取published版本的审核事实及本场策略；不要改为读取未发布的编辑态事实。
- `management-service/internal/decisionexecutor/worker.go`：decisionItem可增加内容计划字段；answerPrompt叠加完整事实、用户记忆/纠正、素材、风格；最终普通API生成。保留商业安全校验及长度容忍。
- answerPrompt约3278行仍有“主线衔接硬要求”，并把切前/回归截为120/140字，要求最后一句强衔接。已准备声音跨切点复用时上下文会变化，需处理这处硬绑定，避免无关规格和“收口”指令被读出。
- `management-service/internal/decisionexecutor/prepared_interaction.go`：未播正文/声音缓存，已有tenant/room/session/decision及事实/优惠/纠正scope保护，TTL2分钟；复用它，不再造重复缓存。
- `management-service/internal/httpapi/anchor_style_cycle.go`：约540行调用ContentStrategy.Render("主线",state.ProductFacts(task))；可传当前卖点×角度、近期内容及真实使用记录，让程序筛选适用任务。多商品价格目的的refs不能让单品表达全部被过滤掉。

### 开场观察与全局消息

- `core-service/internal/coreaudio/client.go`：startRoomAudioMainlineMirror内的publishFrame，在成功PublishPCMAt时才能认定实际起播；StartedAt现在是程序创建时间，不适合直接计120秒。
- `core-service/internal/messagecaptain/`：已有全局消息队长；coreaudio/HTTP API/paid scheduler已注册，不要新增一对一调用通知。
- 开场观察必须覆盖自动问题、债务、暖场、欢迎点赞和正常手工入队/claim入口；测试模拟/预览可与真实直播区别处理。期间保留原聚合数据。

## 7. 生产部署资料与注意事项

上次读取生产状态：

```text
/opt/xiaolan/current -> /opt/xiaolan/releases/20261009-051935-fact-purpose-angle
Management/Core/Gateway三服务active
active runtime sessions = 0
```

**这是先前读到的状态，交接时没有重新读取。发布前必须重查，其他任务也在部署。**

SSH示例（PowerShell）：

```powershell
ssh -n -i "$env:USERPROFILE\.ssh\xiaolan-ecs-deploy" -o IdentitiesOnly=yes -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=yes ecs-user@47.114.55.117 'readlink -f /opt/xiaolan/current'
```

服务器环境 `/etc/xiaolan/management.env` 只在服务器root环境source；不要输出凭证。端口Management8080、Core8081、Gateway8083。

上次版本二进制SHA256：

- Management：`d27805a6464c270939f79609395eb9af70d62313ec95db755f77f70e8989a62d`
- Core：`edb932cfde8a1cafa828e44316467bc63ed0674fc441606c717a3b3dbd5ff8db`
- Gateway：`d794e9f0b7fe964116394a8d24427e9b3f20d69310086eae1f5346137b433ffd`

已有部署模板 `scripts/server/deploy-content-strategy-overlay.sh` 仅更新Management及母版，目前要求Core不变。本批需调整为受限Core+Management覆盖发布，保留web/gateway及其他资源。部署锁、expected current、旧二进制hash、归档hash、全母版manifest、活动场次复查、原子切换和失败回滚都要保留。

该脚本用cp -al复用旧版本，替换文件前必须unlink，防止修改旧版共享inode。不要破坏回滚版。实际运行中有场次时不得擅自重启终止直播。

源校验信息：本聊天 `work/CONTENT-STRATEGY-SOURCE.json` 的files字段保存上一批基线SHA；独立worktree初始源码对比为source_differences []。不要输出整个巨大files表。

旧build方式：`go build -trimpath -ldflags '-s -w' -o ... ./cmd/...`，注意VCS stamp对二进制hash的影响。

已有服务器只读辅助：`/tmp/run-content-strategy-probe.sh inspect` 可看活动场次及部分方案；`/tmp/monitor-live-interactions.sh`。`/tmp/api-conversation-test` **读stdin JSON，不支持-h**，不要误运行挂住。

验收必须包含跨轨准备、准备超时后复用、切入、完整回答、恢复主线、新旧场次隔离及目的识别/复用。服务健康200不能等同功能验收。

## 8. 既有实测证据与文档

优先阅读独立worktree：

- `docs/anchor-style/interrupt-repair-phase1-2026-10-09.md`：本批实施范围和验收项。
- `docs/anchor-style/interrupt-repair-decisions-2026-10-09.md`：已确认讨论记录。

本聊天用户输出目录：

`C:\Users\19918\Documents\Codex\2026-10-08\e-95-failed-to-fetch-oss\outputs`

- `菜籽油场次219复盘.md`
- `直播互动监控记录.md`
- `互动目的与推进任务挖掘API测试.md`
- `互动目的与推进任务挖掘API原始结果.json`
- `打断修复第一批实施与验收记录.md`
- `打断修复讨论记录-2026-10-09.md`

场次219：tenant14、room15菜籽油，2026-10-09北京时间10:50:07–11:07:25手工结束。13条原始chat，实际回应3条；7个明确商品问题只有1条人工触发回应；有3个已准备音频因切点失效重排后消失。主因是短轨远切点领取限制、误分类、顺延TTL问题，没有证据说明模型普遍执行失败。

该场主线37段启动，8卖点家族×3角度共24种覆盖；31正文=24+7复用，6收尾，7个小循环开始、6个完成。39/39主线生成包含既有目的角度资产，但不能由prompt存在推断每个角度实际用了，更不能等同这批修复已运行。

最近普通API测试用deepseek-v4-pro，thinking=false，真实模型输出和失败都保存。价格/贵不贵能解析购买/优惠/价值目的，但有斤价/餐桌表达偏好和排序不稳，需要程序任务选择。V2曾因只返回2角度而要求3–5失败，后改为2–5并实测通过。不要隐藏失败或代写模型结果。

鸭子确认事实：绸都杨鸭子，简称杨鸭子；油炸鸭，现卤现炸，每天新鲜，无隔夜货/存货；85.5元/只，约47.5元/斤；一只够一桌人一顿；鸭子这道菜不到90元（不是整桌饭不到90元）；禁止按克算价格。测试中10元券只是假设，**不能写入生产优惠**。鸭子plan4、菜籽油plan2已有上一批策略资产。

## 9. 建议新聊天直接执行的顺序

1. 读AGENTS及此文件，核对独立worktree diff；不要从原项目误当本批代码已在那儿。
2. 完成1/2的近句末重选、跨轨回归和A/B/C执行记录；修改冲突测试，补本批真实回归。
3. 完成3的问题补判与原聚合器接法，实际起播120秒观察窗；做租户隔离、精确/语义复用、歧义及取消晚结果测试。
4. 将目的/必答与推进任务/优先角度/条件策略接到主线和互动，保留已发布事实、问答及纠正，最终仍用普通API生成；真实模型验证。
5. 完成Go检查及连续音流完整链路；写验收证据，不夸大已完成程度。
6. 重查线上版本/活动场次/部署锁，按当前基线构建发布Core+Management和完整母版目录，回滚保护；验证后记录版本、Git提交及未验收项。

用户已授权上述修复和部署，不需要重新问是否继续或是否上线。没有授权派生子agent，本批此前也没有使用子agent。


## 新增确认：回归桥生成流程（2026-10-09）

状态：用户确认的设计，尚未实现和验收；不代表生产已有此行为。

1. 先从连续未来主线窗口提前规划切入点和回归点，保留切前、回归及回归后完整语义内容，再带着这些上下文生成核心回答及声音。预计位置作为内容参考，核心回答不硬绑定具体回归句，不要求每次带桥。
2. 声音就绪后复核提前规划的位置。原切点仍可执行则使用；错过原切点则找声音就绪后的最近可执行完整句末，并更新相应回归位置，不重新等待24秒、不因等优质切点或桥未生成暂停主线。实际切入后立即播放已准备的核心回答，保存实际位置；事实、纠正及场次身份仍有效时复用回答和声音，具体桥按新回归入口生成。
3. 核心回答播放期间检查其内容与候选回归语义单元的重复。已被回答完整覆盖的单元可以跳过；只是同话题或部分相似、仍含未讲的重要信息时不能整段跳。确定首个未覆盖的完整回归入口。
4. 根据音频真实时长及当前剩余播放时间判断能否完成桥接。长短不按固定字数。预算包含桥文本生成、TTS、音频读取/入流及安全余量；预算用实际耗时统计，并在生成过程中设置截止时间。
5. 短回答或预算不足：完成去重后直接回归，没有重复就接原回归点，有完整覆盖就接去重后的回归点；不生成桥，也不等待桥。
6. 预算足够：按核心回答结尾、最终回归入口及其后完整内容生成一句短桥，再生成桥音频。不能朗读写作指导、复述下一句主线、增加商品事实。桥文本也要检查是否新增重复；必要时重新计算回归点或放弃桥，不能让桥和回归入口失配。
7. 桥音频在接入截止时间前就绪并验证有效，接在核心回答后，然后恢复主线。未及时就绪、失败或回归身份失效则直接回归；晚到桥丢弃，不能插进已经恢复的主线。
8. 核心回答、可选桥及恢复主线归属同一互动任务和播放占用；下一互动不能插在回答与桥或桥与回归之间。实际恢复主线成功才计任务完成。

最小验证：短回答无重复直接续播；短回答已覆盖一个单元跳过后续播；长回答桥按实际回归入口及时接入；桥超时不中断连续播放；切点顺延不复用旧具体桥；取消/换场晚桥丢弃；仅相似话题含新价格/条件不误跳；回答和桥之间不插下一问题。
