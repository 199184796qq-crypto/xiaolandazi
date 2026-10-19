# 给 WorkBuddy：session317 全场无声修复指导

2026-10-19。用户本轮要求是排查后指导 WorkBuddy；Codex 只整理文档，未修改业务源码、模板或服务器，未部署、重启、开播或调用模型/TTS。

## 已确认的根因

北京时间17:21:12.291—17:22:26.813，tenant14 / room29 / plan5，session317，run_id `4d8ff3527e58441552d05321b753c4c1`，历时74.522秒。

`live_p0_audit_events` 中本场有224条跳过记录，id4256—4479，原因全部相同：

> 首次开场避重准备未完成：缺少互动母版 first-live-opening.md

本场模型调用记录0条、音频归档0条；Core未收到非stop的主线Program POST。这224条是调用模型前的程序失败记录，不是224次模型请求或审核退回。

17:31再次只读确认，实际环境：

- Management PID1186931，17:17:05启动；SHA256 `5fd01ca02fd0e52983d01c940145d84ecc253ab80d30d189fcc4eacbdffb36fb`，构建记录c665542+dirty。
- `current`仍是`/opt/xiaolan/releases/20261019-rolling-scheduler-e93437b`，程序已经原地更新；发布目录名称不能代表当前程序源码。
- `ANCHOR_STYLE_PROMPT_DIR=/opt/xiaolan/current/configs/prompts/anchor-style`。
- 该目录缺`first-live-opening.md`，现场`MANIFEST.json`也没有它的条目。
- Core775073、小智网关775930保持。后续操作前必须重新核对，不能用本条历史PID/哈希强行覆盖新的并行更新。

证据目录：`E:\直播伴播-local\last-live-no-audio-20261019`。重点看`analysis.json`、`session317-skips.json`、`session317-evidence.json`、`handoff-recheck.json`，均已脱敏。

## 第一步：补齐母版，恢复首次开场入口

这一步是模板文件补发，现有加载器每次读取当前目录，无需重新编译或重启Management。

**源文件：**

`E:\直播伴播\configs\prompts\anchor-style\first-live-opening.md`

**生产目标：**

`/opt/xiaolan/current/configs/prompts/anchor-style/first-live-opening.md`

请按以下顺序处理：

1. 读取上述源文件原文，确认它是首次实时开场的避重说明，包含事实/P0边界，不新写占位文本。保留“每场首次开场实时生成”的用户要求。
2. 重新采集实际current、管理程序哈希和有效母版目录；与正在发布的一方交接，取得共享发布锁`/opt/xiaolan/deployments/xiaolan-release.lock`。沿用项目发布的会话/Core空闲检查，避免与另一方更新交叉。
3. 将这一个文件上传到暂存位置，核对SHA256、UTF-8、非空及服务运行用户可读；备份现场清单，以原子替换方式放入有效目录。不要整目录覆盖现场母版。
4. 基于**服务器现有**`MANIFEST.json`追加或更新`first-live-opening.md`的`path/sha256`条目。清单是数组；保留其他条目、生产独有母版与备份，不用E盘整份旧清单替换线上清单。记录此次新增文件及清单前后哈希。
5. 发布后从服务实际环境指向的目录再次读取，核对字节哈希与非空，确认清单中恰有一个对应条目，且哈希匹配。前端、现用二进制、P0母版和其他无关文件应保持本次操作前状态。
6. 由用户开始一场短测试，按下面的链路验收。session317已结束，不恢复旧游标、Redis状态或队列。

只读核验命令示例：

```bash
readlink -f /opt/xiaolan/current
systemctl show xiaolan-management.service -p MainPID -p ExecMainStartTimestamp
test -s /opt/xiaolan/current/configs/prompts/anchor-style/first-live-opening.md
sha256sum /opt/xiaolan/current/configs/prompts/anchor-style/first-live-opening.md
```

上述命令以再次确认环境仍指向该目录为前提。不要将“文件已上传”或healthz成功当成首次开场已经播出。

旧二进制发布教程的固定前驱/哈希已经过期。此处只需补缺失依赖，保留WorkBuddy当前二进制、四层/P0生成和腔调卡改动；不拿较早干净构建覆盖现用程序。

## 第二步：修复缺依赖时反复换任务的行为

这一部分需要后续源码交接、测试和单独发布，本文件没有代改。

当前链路是：

1. `httpapi/first_live_opening.go`中的`completeLiveCycleText`先准备历史、读取`first-live-opening.md`，失败返回`errFirstOpeningPreparation`。历史准备也可能经`speech_library.go`的`libraryTextSHA`先读到此依赖。
2. `httpapi/anchor_style_cycle.go`的`advanceAnchorCycle`把错误保存为`content_blocked`。
3. `httpapi/cycle_supply.go`的文字供料循环对正式场次的`*_blocked`调用`skipGeneration`，约300毫秒后继续换任务。
4. 文件对整场所有任务都缺，换卖点×角度不会恢复，因而产生224条跳过、0段正文。

### 2.1 开播前检查必要依赖

建议在`httpapi/live_runtime.go`的`liveRuntimeStart`中，已解析选定方案、确认实时模式后，**创建计时会话、启动Core及生成器之前**，检查本次路径必需母版可读、非空和配置可解析。也核对实际生效的覆盖路径，不只检查仓库默认目录。

- 此处必须包括`first-live-opening.md`，并由现有读取函数实际读一遍。
- 通过只读的启动材料准备函数检查，不能借预检初始化方案、创建模型会话或调付费模型/TTS。
- 依赖缺失返回可识别的准备失败及文件名；后台保留具体路径，前台给出清楚的缺项。不能先显示启动成功，让用户一直等。
- 模板预检不恢复旧场，也不改“首次实时、后续七三复用”的策略。
- `loadGenerationP0`的解析成功不等于词表渲染有效；预检应检查必要字段及实际非空的“不提/改写”结果。这是配置可用性核对，不在本修复中改变现有审核开关。

### 2.2 全局依赖错误不能按角度失败处理

建议增加可用`errors.Is/As`判断的依赖错误类型或哨兵，携带资源名和原因，不靠匹配中文错误字符串判断。

| 情况 | 处理 |
| --- | --- |
| 必要母版缺失、空文件、配置结构无效 | 启动失败或进入明确的全局准备失败；一次留档并通知，停止新文本供料，不换任务、不累计角度/P0屏蔽 |
| 历史库短暂读取超时 | 单独的技术错误，有限重试并退避，保留原任务；耗尽后明确报准备失败 |
| 两次首次开场都与历史重复 | 保持已有有界处理；不降级使用旧开场，不将它误认成缺模板 |
| 单条正文的内容或审核问题 | 保留对应的既有内容处理，不把所有失败都升级为全局故障 |

不要把所有`errFirstOpeningPreparation`都一概当成永久缺文件，其中还包含历史库读取错误。也不要通过清空Error、伪造采纳稿、跳过首次开场要求或反复重启来掩盖依赖缺失。

预检之后文件仍可能被删除：`completeLiveCycleText`的读取失败分支也必须保留，并让`cycleSupply`识别全局依赖错误，不能再次进入`skipGeneration`。启动后意外失败可接既有明确失败/收尾流程，展示原因并正确结算本场；不修改历史计费记录。本场没产出可播放内容，不能将计时74秒当成实际播出74秒。

### 2.3 拟修改文件，动手前重新登记

主目录统一为`E:\直播伴播`，以下均为相对它的路径：

- `management-service/internal/httpapi/live_runtime.go`：开播前依赖检查。
- `management-service/internal/httpapi/first_live_opening.go`：准备失败的明确分类。
- `management-service/internal/httpapi/anchor_style_cycle.go`：区分全局准备失败与内容暂停。
- `management-service/internal/httpapi/cycle_supply.go`：全局依赖错误不换任务重试。
- `management-service/internal/interactionpolicy/policy.go`：如需复用，可在这里提供结构化资源读取错误；保留显式目录不回退行为。
- 对应独立测试和发布依赖校验脚本。

这些文件如与另一方进行中的改动重叠，先按`协作交接.md`交接，不直接覆盖。

## 验收要求

先做不付费的程序回归：

1. 删除临时测试目录中的首次开场母版：开播准备明确失败；不创建计时会话、不请求Core start、不调用模型/TTS，不产生反复换角度记录。
2. 预检通过后模拟文件消失：读到明确依赖错误后停止文字供料，失败记录去重，不能循环224次；不增加内容违规、角度完成或送播配额。
3. 历史库暂时超时：验证有限重试和退避，保留原任务，与缺文件区分。
4. 全部依赖存在：首次开场仍走实时生成，重复检测上限仍保留；后续70%复用/30%更新策略、事实/P0/声音身份及WorkBuddy当前业务改动保持。
5. 发布包从完整母版目录生成清单，必要文件缺失/为空/JSON实际渲染为空即阻止发布；保留生产额外资产，不能用固定旧名单遗漏新母版。

用户开始新场后，再核对真实链路：

**首次正文生成完成 → 正式采纳 → TTS完成 → Core接收曲目 → Core开始及PCM进度。**

记录每步时间和耗时，确认没有新的缺依赖跳过。模型API成功、TTS成功、Core开始和完整PCM完成分别判断；不把后台计时或前端有缓存当作外接设备已经有声音。未做实际新场测试前，只能说文件/程序验证通过。

## 交接边界

本次建议的立即恢复动作仅补`first-live-opening.md`及其现场清单条目。停止无效换任务需另行源码修改、回归和发布。Codex没有实施这两步；也没有代WorkBuddy改审核开关、事实工厂、权重、客户事实或已保存方案。
