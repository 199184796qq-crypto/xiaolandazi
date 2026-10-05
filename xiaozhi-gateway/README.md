# xiaozhi-gateway

小智 ESP32 设备协议适配层。它属于设备/声音分发层，不把小智协议侵入 Core。

## 当前接口

- `POST /xiaozhi/ota/`：设备启动配置，返回 WebSocket 地址和设备专属 token。
- `GET /xiaozhi/v1/`：小智 WebSocket v1。
- `GET /healthz`：健康检查。
- `GET /internal/v1/bindings`：查看绑定，需要 `X-Xiaozhi-Internal-Token`。
- `PUT /internal/v1/bindings/{deviceID}`：绑定设备到直播间，JSON 为 `{"room_id":15}`。
- `DELETE /internal/v1/bindings/{deviceID}`：解除绑定。

## 声音链路

`Room Audio Engine -> /v1/rooms/{roomID}/composite.pcm -> xiaozhi-gateway -> FFmpeg/libopus -> WebSocket binary Opus -> ESP32-S3`

音频连接建立后，网关同时轮询 Core 的 `/v1/rooms/{roomID}/audio-engine` 快照；当当前语音段变化时，向小智发送 `tts/sentence_start` 文本事件。这样文字走 WebSocket 文本帧，音频仍走原有的二进制 Opus 帧，不改变 PCM 音频格式。

Core 继续只维护房间唯一最终音频时间轴，不感知终端是网页、盒子还是小智。

## 设备语音控制（小蓝搭子）

新版固件在 hello 中声明 `features.device_control=true`（摄像头同时声明
`features.camera_capture=true`）。旧固件保持下行播放，不会无意义上传 ASR
或等待它无法执行的控制命令。

本地提示音增强固件额外声明 `features.device_feedback=true`。只有此能力开启
才发送 `control_feedback`，包含同一 request_id、phase
（greeting/accepted/completed/failed）、addressing（female/male/child/neutral）、
message 和 listen_after。称呼来自 management 返回的 speaker_addressing，
无法确认时中性称呼；网关不推断身份、不维护声纹库。

“小蓝小蓝帮我音量调整到30%”完整录音后直接进入命令，不先播问候打断
后半句。只有 management 返回专门的 kind=wake_greeting、status=succeeded
且确属纯唤醒文本时，才问候“有什么吩咐”并 listen_after=true，旧录音已
终结，后续录音使用独立新 request_id；问候不下发 device_control，也不
重复写已持久化的无操作事件。普通 kind=command 必须 status=recognized。

accepted 只在持久 dispatch 闸门通过后发送。completed 需真实合法设备 ACK
和同 request_id/event_id、succeeded、disabled/0 的持久事件回执，未知命令、
超时、拒绝及回执未确认只能 failed，不能冒充成功。屏幕显示实际值；查询
completed 附 prompt=info，拍照接收成功附 prompt=capture_completed，不表示
环境处理已经成功。固件播放固定本地 Ogg 资产，不逐次调用云 TTS。
增强固件的终态先发送 control_feedback，再发送 control_status/done 或 error，
使提示队列在录音处理门禁解除前先接管 Busy；旧固件仍仅收终态屏幕消息。

固件回报 `control_feedback_playback` 的 request_id/phase/state=start|stop。
只接受网关近期发出的对应提示，临时屏蔽该设备房间下行，与录音屏蔽相互
独立；单次提示最多 15 秒，缺 stop 自动恢复。问候 stop 后最多保留 2 秒
保护新 listen.start 交接。取消、断连清理本机屏蔽，不暂停 Core 或其他终端。

设备用 `listen/start` 开始录音，携带 8～96 位 `A-Za-z0-9_-` 的随机
`request_id`；Opus 二进制帧只在录音窗口内接收；`listen/stop` 提交语音。
`listen/detect` 是唤醒通知，start 必须在麦克风帧前发送。
录音最多 8 秒、256 KiB、每包 8 KiB；同 MAC 同时只有一条指令，至少间隔
2 秒、每分钟最多 10 条。只对已经认领的设备开启云端语音识别。

录音期间仅屏蔽该终端下行音频，Core 的 PCM 时间轴持续读取，不调用停止
或暂停业务接口。录音结束即恢复播放，不在 ASR/拍照等待期间丢弃播报。
`abort` 取消当前本机指令且不关闭 WebSocket，`goodbye` 才关闭常驻连接。

网关按 Opus TOC 的真实时长封装完整 Ogg（CRC、48 kHz granule、mono
OpusHead/OpusTags），调用 management 的内部 voice 接口转写；网关没有
云端 ASR 密钥。management 目前是异步短录音 ASR，gateway 最多等 30 秒，
不保证实时响应。取消、失败和超时不会执行晚到的转写结果。只有返回状态
`recognized`、`billing_mode=disabled` 且 0 扣豆的结果才可进入指令解析；
终态缓存即使带旧文本也不能重新执行。解析后再调用持久 command-dispatch
闸门，原子 recognized→executing，仅首次 dispatch_allowed=true 可下发；
重复/网络失败不重发，网关重启也不能重复相对调节。该调用异步，不阻塞 abort。

确定性白名单支持音量固定值、大/小一点、静音、取消静音、查询，以及字体
小/中/大档、放大/缩小、恢复默认、查询。100%/最大音量首次只给警告，需
明确说“确认最大音量”。“调整声音”等含糊话语、业务指令不执行，不转
Core/业务 LLM。字号 set 为 0/1/2，adjust 为 ±1。

`device_control` 带相同 request_id、action、operation 和可选 value。
只有对应 `device_control_ack` 的 success 和合法实际值才返回
`control_status/done`；固定值与目标不符、缺实际值、旧 ACK、超时均不能
冒充成功。普通命令 ACK 超时 8 秒，拍照 ACK 最多 45 秒。事件写入内部
command-events，有限重试失败有脱敏日志，设备控制不执行豆扣费。

“小蓝，看下环境”等拍照命令给该设备发短期 HMAC upload_url，绑定 MAC、
Client、request_id 和 provision 得到的 device/tenant，最多 120 秒。
`POST /xiaozhi/v1/capture?grant=...` 只接受 ≤2 MiB、≤2048×2048 的完整
JPEG；重新检查归属和活跃指令，再代理到内部 capture。客户端 tenant、
MAC 和 question 元数据不是身份凭据；不泄漏 internal token、不接受外部
上传 URL。同图网络重试复用接收回执，不重复上传；换图/取消/归属变化/过期
均拒绝。capture ACK 还需网关实际接收回执，不相信客户端自报 event_id。
“照片已上传”不代表环境识图业务已处理成功。

内部接口合同：

- `POST /internal/v1/xiaozhi/voice`：multipart file=Ogg，设备 MAC/request_id
  头；返回 text/kind/status/speaker_addressing/request_id/event_id，
  billing_mode=disabled、charged_beans=0，正常指令 recognized，纯唤醒 succeeded。
- `POST /internal/v1/xiaozhi/capture`：multipart file=JPEG，同样鉴权头；返回
  accepted/request_id/event_id，charged_beans 必须为 0。
- `POST /internal/v1/xiaozhi/command-events`：JSON hardware_mac/request_id、
  action/operation、终态 succeeded/failed/timeout/rejected 和实际 value；
  返回匹配 request_id/event_id/status 和 disabled/0 回执。
- `POST /internal/v1/xiaozhi/command-dispatch`：JSON hardware_mac/request_id、
  action/operation（无实际 value），首次返回 dispatch_allowed=true、
  executing/request_id/event_id、disabled/0；其余全部 fail closed。

这些接口仅使用已有 `XIAOZHI_MANAGEMENT_BASE_URL`、
`XIAOZHI_INTERNAL_TOKEN`，公网签名及地址沿用 `XIAOZHI_DEVICE_SECRET`、
`XIAOZHI_PUBLIC_WS_URL`，无需在网关新增云 ASR 密钥。

当前第一阶段只接小智 WebSocket 二进制协议 v1。官方固件 v1 使用原始 Opus WebSocket binary frame；v2/v3 后续在真机需要时增加封装，不修改 Core。

## 设备绑定

设备在 OTA 请求和 WebSocket 握手中携带 `Device-Id`、`Client-Id`。OTA 返回的 token 由服务端基于两者签名，WebSocket 建连时用 `Authorization: Bearer <token>` 校验。

绑定文件应放在 release 目录外，例如：

`/opt/xiaolan/state/xiaozhi-bindings.json`

这样切换 release 不会丢设备与直播间绑定。

## 生产地址

- OTA：`https://www.xiaolandaizi.cn/xiaozhi/ota/`
- WebSocket：`wss://www.xiaolandaizi.cn/xiaozhi/v1/`
