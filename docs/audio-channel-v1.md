# 房间播音通道 V1：电脑真实播放闭环

日期：2026-09-24

## 本阶段边界

第一阶段只验证一件事：Core 把一个房间的播音任务交给独立播音分发层，电脑接收端实际播放，然后把真实播放器状态回传。

链路：

```text
Core
  -> audio-service（播音分发层）
      -> PC Receiver A
      -> PC Receiver B ...
  <- 房间参考接收端的统一播放状态
```

本阶段不接公屏决策、场控 Agent、真实 TTS、小智协议，也不写入客户/财务业务数据。

## 架构约束

- Core 不知道接收端是电脑、小智还是未来其他设备。
- Core 只认识 room_id / session_id / speech_task_id 和统一播放状态。
- TTS 接入前所有测试任务统一使用 `E:\直播伴播\测试素材\主要测试声音\跑山鸡.wav`；同一 SpeechTask 的音频只装载/引用一份，多个接收端共享同一 audio_url。
- 每个接收端独立接收，不允许一个慢接收端阻塞其他接收端。
- audio-service 选择一个参考接收端，只有参考端状态推进 Core；其他端的回执仅用于设备级观察。
- 回执顺序严格保持 READY -> PLAYING -> PROGRESS -> COMPLETED/FAILED。
- COMPLETED 的任务标记 terminal，接收端重连后不会把已播完任务重新播放。
- 播放过程中才进入同一房间的接收端，会拿到当前参考接收端推算出的 start_ms，并从当前房间进度接播，不从 WAV 的 0 秒重新开始。
- FAILED 的参考端释放参考资格，后续健康接收端可接替。
- 第一版任务和回执保存在进程内存，服务重启后清空；这是测试阶段刻意限制，不作为后续生产持久化设计。

## 服务

- Core：127.0.0.1:8081
- audio-service：127.0.0.1:8082
- 电脑接收端：127.0.0.1:5176

audio-service 的设备侧测试协议：
- GET /v1/rooms/{roomID}/stream?receiver_id=...：SSE 接收任务
- GET /v1/tasks/{taskID}/audio.wav：读取同一份 WAV
- POST /v1/tasks/{taskID}/events：回传 READY / PLAYING / PROGRESS / COMPLETED / FAILED

Core 开发测试入口只在 development 环境开放，并继续受 X-Core-Token 内部鉴权保护。

## 人工验收

1. 打开电脑接收端。
2. 测试房间保持默认 1001，点击“开始接听”。
3. 点击“发送测试声音”。
4. 电脑应实际播放 `跑山鸡.wav`，页面进度按 WAV 文件真实时长计算。
5. 页面应出现 READY、PLAYING、PROGRESS、COMPLETED。
6. “Core 已确认状态”最终变为 COMPLETED。
7. 若浏览器阻止自动播放，页面显示“播放当前任务”，点击一次即可；之后仍走同一回执链路。
8. 测晚加入接收端时，先让第一个页面播放数秒，再打开第二个浏览器并连接同一房间；第二个浏览器应直接接近第一个页面当前进度播放，而不是从 0 秒开始。两端仍使用相同 speech_task_id 与 audio_url，Core 只按参考端推进一次。

## 下一阶段

把固定测试 WAV 替换成已有主线/TTS 输出，仍然只提交统一 SpeechTask 给 audio-service。设备层不参与场控决策。小智以后作为 audio-service 下的另一种接收适配器接入，不修改 Core 的房间节目模型。
