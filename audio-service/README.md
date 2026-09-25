# audio-service

第一阶段“房间播音通道 V1”的独立播音分发层。

当前职责：
- Core 只提交房间级播音任务，不知道电脑、小智或其他接收端。
- 同一房间的同一条音频只生成/保存一次，可广播给多个接收端。
- 接收端通过 SSE 接收任务，通过 HTTP 回传 READY / PLAYING / PROGRESS / COMPLETED / FAILED。
- audio-service 选择一个房间播放参考端，只把该参考端的播放状态回传给 Core，避免多接收端重复推进房间主线。
- TTS 接入前，所有测试播音统一使用 `E:\直播伴播\测试素材\主要测试声音\跑山鸡.wav`；TTS、场控 Agent 和小智协议不在本阶段实现。

默认地址：`http://127.0.0.1:8082`

环境变量：
- `AUDIO_ADDR`：监听地址，默认 `127.0.0.1:8082`
- `AUDIO_PUBLIC_URL`：接收端可访问的地址，默认 `http://127.0.0.1:8082`
- `AUDIO_INTERNAL_TOKEN`：Core -> audio-service 内部调用令牌
- `CORE_INTERNAL_TOKEN`：audio-service -> Core 播放反馈回调令牌
- `AUDIO_TEST_WAV_PATH`：TTS 接入前的统一测试 WAV；默认 `E:\直播伴播\测试素材\主要测试声音\跑山鸡.wav`

第一阶段仅面向本机开发验证。接小智时在本服务下面增加设备协议适配，不改变 Core 的房间节目接口。
