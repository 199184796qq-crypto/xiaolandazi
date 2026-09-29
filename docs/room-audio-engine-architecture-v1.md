# 房间实时合成音频引擎 V1

## 目标

直播间内部允许两路实时音源：A 为主线主播音频，B 为场控答疑、临时插播和实时 TTS；但对终端永远只输出一条连续房间音频流。

核心原则：

生成层可以多路，执行层内部双源，输出层永远单流。

## 核心结构

主线音源 A ─┐
             ├─> Room Audio Engine ─> Composite Stream Hub ─> 分发层 ─> 网页 / 盒子 / 小智
互动音源 B ─┘

Room Audio Engine 是每个直播间唯一的声音执行时钟。字幕状态、黄色预停、绿色接回、真实波形、计费时长和最终音频都以这一时钟为准。

## 内部标准音频格式

V1 统一采用：

- PCM S16LE
- 24 kHz
- 单声道
- 20 ms / frame
- 480 samples / frame
- 960 bytes / frame

后续设备侧需要 Opus、AAC 或其他格式时，由分发层做编码适配，Room Audio Engine 不关心终端类型。

## 房间状态机

IDLE
→ MAINLINE
→ PREPARING_INTERRUPT
→ ARMED
→ INTERRUPT
→ PREPARING_RESUME
→ RESUME
→ MAINLINE

任意运行态允许进入 PAUSED 或 ERROR。

状态说明：

| 状态 | 含义 |
| --- | --- |
| IDLE | 未输出声音 |
| MAINLINE | A 路主线输出 |
| PREPARING_INTERRUPT | B 路生成 / 预缓存 |
| ARMED | 已锁定安全切点，前端黄色高亮目标句 |
| INTERRUPT | B 路正在输出 |
| PREPARING_RESUME | 插播结束，正在确定接回点 |
| RESUME | 已切回主线，前端绿色标记接回句 |
| PAUSED | 房间音频时钟冻结 |
| ERROR | 音频执行异常保护 |

## 安全切换规则

### A → B

1. 互动文本和 TTS 在后台准备。
2. B 至少准备好首个完整语义句并达到最低缓存阈值。
3. 根据主线 timeline + safe_points 选择安全句末。
4. Engine 进入 ARMED，黄色只高亮目标句，不显示“停止前”文字。
5. A 必须真实输出完该句最后一个 PCM frame。
6. 下一帧开始由 B 输出。

禁止先停 A 再等 B。

### B → A

1. B 完整播放完成。
2. 按 resume_mode 计算接回位置。
3. Engine 进入 PREPARING_RESUME。
4. 接回句标记为绿色。
5. 下一帧切回 A。
6. Resume 完成后恢复 MAINLINE。

## 唯一时间轴

房间最终输出帧拥有连续 sequence、pts_ms、duration_ms 和 source。

终端本地 currentTime 不再作为业务状态主时钟。最终输出时间轴决定当前句、黄色预停句、绿色接回句、波形、有效播音计费时长和多终端同步位置。

## UI Sideband

前端以后不再根据任务时间自行猜切点，而读取 Engine 状态，包含：

- phase
- current_segment_id
- planned_cut_segment_id
- resume_segment_id
- output_pts_ms

黄色、绿色、当前句和声音都来自同一房间音频状态。

## 分发层

Room Audio Engine 只输出标准 Composite PCM Frames。

分发层负责：

- Web：WebSocket / AudioWorklet
- 盒子：设备协议编码
- 小智：小智协议适配
- 未来设备：新增 Adapter，不修改 Room Audio Engine

## 计费

AI 时长最终绑定 Room Audio Engine 的有效输出时间：

- RUNNING 且实际输出有效音频帧：计费
- PAUSED：不计费
- Core 掉线：不计费
- 没有有效输出：不计费

## 迁移顺序

### Phase 1：新引擎骨架

- 新增 Room Audio Engine
- 建立房间唯一 PTS / sequence
- 建立单一 Composite Stream 订阅
- 建立状态机和可观测快照
- 保留旧 AudioHub，不改变现网播放

### Phase 2：A 路迁入

- 主线 WAV 解码为 PCM frame
- A 路通过 Room Audio Engine 输出
- 网页测试端订阅单流

当前 V1 单流测试接口：

- GET /v1/rooms/{roomID}/audio-engine：读取房间引擎状态。
- GET /v1/rooms/{roomID}/composite.pcm：订阅房间最终 PCM 单流。
- composite.pcm 固定为 PCM S16LE / 24kHz / mono / 20ms，每帧 960 bytes。

### Phase 3：B 路迁入

- TTS 改为流式 PCM 输入
- B 路预缓存
- safe point 驱动 A → B
- resume_mode 驱动 B → A

### Phase 4：前端切单流

- 删除网页端 pause A / play B / resume A
- 字幕和波形改听 Engine 状态
- 多浏览器只接收同一 Composite Stream

### Phase 5：设备分发

- 盒子、小智接 Composite Stream Hub
- 各设备只做协议和编码适配

## V1 不变量

1. 一个直播间只有一个 Room Audio Engine。
2. 一个房间只有一条最终输出时间轴。
3. 同一时刻只能有一个前景人声源。
4. ARMED 目标句必须完整输出后才能切入 B。
5. B 未准备好时不得停止 A。
6. 终端不得自行决定业务切点。
7. safe_points / timeline / resume_mode 继续复用现有数据结构。
8. 新引擎验证完成前不删除旧 AudioHub。

## 当前落地进度（2026-09-28）

已完成：

- 新增 core-service/internal/roomaudio，建立房间唯一 sequence / output PTS。
- 建立 MAINLINE / PREPARING_INTERRUPT / ARMED / INTERRUPT / PREPARING_RESUME / RESUME / PAUSED 状态机。
- 建立 Composite PCM Frame 订阅机制。
- 新增 WAV 流式读取器，按 20ms / 960 bytes 输出 PCM S16LE 24k mono。
- 现有主线 A 已镜像进入 Room Audio Engine，不影响旧 AudioHub。
- 现有插播 B 已镜像进入同一个 Room Audio Engine，可先验证 A → B → A 单流。
- 旧 safe point 切换过程已同步到新引擎状态，后续黄色/绿色可以直接读取 Engine sideband。
- 新增 GET /v1/rooms/{roomID}/audio-engine。
- 新增 GET /v1/rooms/{roomID}/composite.pcm。
- Core 全量 go test ./... 已通过。

运行规则补充：

- B 路已经输出过有效 PCM 后，如果生成 WAV 的 data chunk 长度比实际 HTTP 音频体略长，流尾 EOF / UnexpectedEOF 视为“声音真实结束”，立即触发 B → A 接回。
- 不再因为这种 TTS/WAV 尾部长度偏差等待旧的 duration + 4s 兜底定时器。

尚未切换：

- 网页仍在使用旧 AudioHub 播放链路。
- B 路当前还是已有 WAV/TTS 结果镜像，尚未改为 TTS PCM 流直接预缓存。
- 新引擎尚未成为正式唯一输出源；当前处于并行镜像验证阶段。
