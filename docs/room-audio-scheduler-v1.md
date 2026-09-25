# 房间双声音调度器 V1

日期：2026-09-24

## 目标

直播间内部可以由大量长度不一、来源不同的音频片段组成：

- 本地主线长音频；
- 本地主线短 Segment；
- 缓存音频；
- 临时生成文件；
- 实时 TTS 音频流；
- 回答、桥接、成交、欢迎等互动音频。

这些来源对房间外部必须统一表现成一条连续声音。设备只跟随“房间当前输出”，不参与业务仲裁。

## 核心结构

每个房间只有两个准备槽和一个输出焦点：

~~~text
Core Room Audio Scheduler

Slot A ──┐
         ├──> Single Room Output ──> audio-service ──> adapters/devices
Slot B ──┘
~~~

任意时刻：

- A/B 可以同时准备；
- A/B 可以同时 READY；
- 只能一个槽 PLAYING；
- 当前播放槽不可原地覆盖；
- 非播放槽可以立即装载下一段。

A/B 不是固定的“主线通道/回答通道”，而是双缓冲槽，角色会轮换。

## 标准插话

~~~text
A: MAINLINE / PLAYING
B: INTERACTION TTS / PREPARING

Control/Monitor:
  给出主线安全停点 + 语义 Resume Cursor

B TTS READY
  ↓
Scheduler 等 A 到安全停点
  ↓
原子切换 A -> B
  ↓
A 空出来，准备 Resume Cursor 对应的新主线段
  ↓
B 播完
  ↓
原子切换 B -> A
~~~

Agent 不直接 stop/play 设备。Agent 只提供：

- 为什么需要切；
- 当前主线允许在哪里停；
- 互动内容；
- 互动后主线从哪个语义位置继续。

单路播放约束、READY 校验、过期切换、槽代际和迟到回调防护全部由 Scheduler 保证。

## Segment 与 Source 解耦

Scheduler 只认识 Segment，不关心它如何产生。

SourceKind：

- LOCAL_FILE
- GENERATED_FILE
- CACHED_AUDIO
- TTS_STREAM

因此以后接真实 TTS 流，不需要改变切换状态机。

## generation 防止迟到结果污染

同一个槽会不断复用。一次很慢的旧 TTS 可能在该槽已经装入新任务后才返回 READY。

所以每次 Prepare 都增加 generation。旧 generation 的 READY / PROGRESS 回调一律拒绝。

## 房间连续时间轴

房间外监听不按“某个 WAV 从 0 秒开始”理解，而读取：

- 当前 Slot；
- 当前 Segment；
- generation；
- output sequence；
- 当前 Segment position；
- server sampled_at。

新设备加入时直接从房间当前输出点接入。跨 A/B 切换时 output sequence 增长，所有适配器跟随同一切换。

## 测试阶段

正式 TTS 接入前仍统一使用：

E:\直播伴播\测试素材\主要测试声音\跑山鸡.wav

长时间测试不能使用浏览器本地 loop。应让 Core/Scheduler 把同一测试 WAV 当成连续的新 Segment，A/B 两槽交替 Prepare -> READY -> PLAY，模拟真实几小时直播的连续小段输出。

## 与旧 Audio Focus / POC 的关系

保留：

- Agent 不直接控制播放器；
- 主线使用语义 Resume Cursor；
- 默认在完整 Segment / Safe Point 软切；
- 旧上下文和过期任务必须失效。

升级：

- 从 SpeechTask 队列升级为房间连续输出状态机；
- 从任务完成后再决定下一条升级为双缓冲提前准备；
- 从某个播放器 currentTime 升级为房间唯一输出时钟；
- Source 与设备协议解耦，为网页、PC、小智和未来适配器共用。
