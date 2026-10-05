# 自定义音稿发布后主播模式无法开始

## 现象
- 在智能体方案中上传“自定义音稿”。
- 自定义音稿完成识别/时间轴/SRT/音色处理后，替换为正式主线。
- 将智能体方案发布到指定直播间。
- 进入该直播间，选择“主播模式”，点击“开始”后没有正常启动/没有声音反馈。

## 预期
- 发布后的当前直播间版本中，CUSTOM 自定义音稿应作为正式主线。
- 自定义音频原始文件应持久保存到 OSS，并在发布版本中保留可解析的 audio_asset_id / audio_url / timeline / srt / safe_points / manifest。
- Core 在主播模式开始时应读取当前直播间已发布版本，加载 CUSTOM 主线资产，建立主线 Program 并开始播放。

## 本次排查范围
1. 核对直播间当前绑定方案、运行方案与已发布版本。
2. 核对发布版本是否包含 variant_key=CUSTOM 且 is_formal=true。
3. 核对 CUSTOM 版本字段中的 audio_asset_id、audio_url、duration、timeline、srt、safe_points、asset_manifest。
4. 核对 media_assets 记录及 OSS object key / storage driver / bucket。
5. 核对主播模式“开始”请求与 Core 日志，确认是否出现版本读取、媒体资产下载、音频解码、主线 Program 构建失败。
6. 在确认真实故障点前不修改运行逻辑。

## 2026-10-01 排查结论

### 发布与资产状态
- 房间：15
- 方案：2
- 当前发布版本：V3（version_id=9）
- 发布版本包含 6 个 variant，其中 CUSTOM 位于第 6 个。
- CUSTOM：is_formal=true，title=“自定义音稿”。
- audio_asset_id=19。
- audio_url=/api/v1/live/media-assets/19/content。
- audio_duration_ms=458455（约 7 分 38 秒）。
- timeline=49 段。
- safe_points=49 个。
- SRT 已保存。
- asset_manifest 已保存。

### OSS 资产
- media_assets.id=19。
- original_name=“样本声音.wav”。
- storage_driver=oss。
- OSS bucket 已配置。
- object_key=tenants/14/audio/2026/10/156ec8eab98cb42e744bc2cd.wav。
- size_bytes=80871502（约 77.1MB）。
- duration_ms=458455。
- status=active。
- purpose=live_agent_custom_mainline_audio。
- room_id=15，plan_id=2。

### 根因
- Management 在主播模式开始时会为已发布正式音轨签发 OSS URL，并调用 Core 的 audio/program/start。
- Core StartProgram() 会先调用 audiohub.ProbeExternalWAV() 检查每条音轨。
- audiohub 当前 maxAudioProbeBytes = 32 << 20，即 32MB。
- 本次自定义音稿约 77.1MB，因此探测阶段会返回 audio source is too large，主线 Program 无法创建。
- 这与前端/Management 已放宽到 200MB 不一致，是旧的 Core 32MB 限制未同步升级导致。
- 真正播放镜像阶段使用 HTTP 流式读取 WAV（roomaudio.StreamWAVPCM），不是必须整文件载入内存，因此根因主要位于“启动前探测”而非播放流本身。

### 伴随问题
- 直播详情页 startCompanionRuntime() 会把后端错误写入 runtimeError，但当前界面反馈不够醒目，用户体感为“点击开始没反应”。
- 后续修复应同时处理：
  1. Core 不再用“完整下载音频”方式探测长 WAV；优先使用已发布版本里的可信 duration_ms，或改为 Range/头部探测。
  2. 保留对 WAV 格式、timeline、safe_points 的有效校验。
  3. 启动失败时把 Core 返回的具体原因明确显示到开始按钮附近/全局提示。
