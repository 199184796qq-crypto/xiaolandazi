# 小蓝搭子设备业务接口（一期）

本期仅完成语音转写、设备执行回执、拍照接收和业务结果回存。不调用通用大模型分析图片、不读取或扣减钱包；所有事件 `billing_mode=disabled`、`charged_beans=0`。以后计费由服务器统一接入，盒子不管理豆子。

## 鉴权与归属

内部接口必须携带 `X-Xiaozhi-Internal-Token`，不得把此 token 放入网页或固件；由可信设备网关/业务服务调用。

设备业务的租户由已登记 MAC 对应的 `device_hardware_profiles.claimed_tenant_id` 和 `inv_devices.current_customer_id` 双重一致校验取得。设备必须已认领、品质合格且处于已交付状态。房间只采用当前同租户有效绑定；未绑定设备的房间为空。上传和结果提交时再次检查归属，拒绝中途售后转移后的旧请求。不能通过请求 JSON 自报租户替代校验。

语音/照片/结果接口还需 `X-Device-MAC` 和 `X-Request-ID`。请求 ID 是 8–96 位字母数字或 `._:-`，建议随机 `ctl-...`。同设备、同租户、同 request ID、同业务类型唯一；同一语音和其照片使用同 request ID 但保存为两个关联业务事件。

## 设备网关接口

| 方法与路径 | 输入 | 返回 |
| --- | --- | --- |
| `POST /internal/v1/xiaozhi/voice` | multipart `file`，WAV 或 Ogg/Opus，最多 2 MB | `text, speaker_addressing, kind, request_id, event_id, status, billing_mode, charged_beans` |
| `POST /internal/v1/xiaozhi/capture` | multipart `file`，真实 JPEG、最多 2 MB、宽高均不超过 2048；可选 `question` 最多 512 字 | HTTP 202，`accepted=true, request_id, event_id, status=queued, billing_mode, charged_beans` |
| `POST /internal/v1/xiaozhi/command-events` | JSON `hardware_mac, request_id, action, operation, status, value, message` | `request_id, event_id, status, billing_mode, charged_beans` |
| `POST /internal/v1/xiaozhi/command-dispatch` | JSON `hardware_mac, request_id, action, operation`，不携带实际值 | `dispatch_allowed=true, request_id, event_id, status=executing, billing_mode, charged_beans` |

语音网关限制单句最多 8 秒；服务端整个转写链路最多 25 秒。临时录音显式 private ACL 写入 OSS，用 2 分钟签名 URL 交给现有 ASR，结束后删除。照片同样强制 private OSS，不依赖默认存储驱动；先上传，媒体资产与 queued 业务事件在一个事务中提交，失败清理对象。

回执 `action` 为 `volume`、`font_size`、`capture`；`operation` 为 `set/adjust/mute/unmute/query/reset`，根据 action 限定，capture 可空或 set。音量实际值为整数 0–100；字体实际值为 0/1/2（small/medium/large 也可接收并标准化）。非 capture 成功回执必须携带实际值。状态为 `succeeded/failed/timeout/rejected`，兼容固件 `success/error`。无法识别或中断时 action/operation 可以为空，但不能因此标记成功。

ASR 完成只进入 recognized，不代表设备执行成功；只有对应执行回执才进入 succeeded。重复相同回执返回原事件；不同回执冲突 HTTP 409。失败/超时/拒绝事件不能被迟到成功回执复活。相同上传 request ID 若数据不同会拒绝；正在处理时重试返回 HTTP 409，已经识别的语音返回原转写，已接收照片返回原 accepted 结果。

例外是纯唤醒“小蓝小蓝”等仅称呼语句：服务器将其原子记为 `status=succeeded, action=wake_greeting`，voice 返回 `kind=wake_greeting`。这是一次不执行硬件指令的问候，网关不得调用 dispatch/command-events；“小蓝小蓝音量调到30%”仍为 `kind=command, status=recognized`。空识别结果、无法识别的普通话语不会因此记为唤醒成功。

网关在下发硬件指令前必须调用 command-dispatch。服务器事务原子锁定 recognized → executing 并持久化动作；同 ID 再次下发一律 HTTP 409，不因网关重启或丢失回执重复执行相对调整。executing 状态的回执必须动作与操作匹配，才能落入终态；executing 不返回转写供再次执行。

## 后续业务处理接口

1. `GET /internal/v1/xiaozhi/business-events?tenant_id=25&limit=20&before_id=...`：内部 token，按租户读取 queued 环境快照；返回 event 字段和仅内部可见的 `hardware_mac`、`image_url`。limit 为 1–100，before_id 用于翻页。
2. 请求返回的 `image_url`，携带内部 token 和对应 `X-Device-MAC`：服务端核对事件与 MAC 当前归属后代理读取私有图片。不返回公开对象地址。
3. `POST /internal/v1/xiaozhi/business-results`：内部 token、对应 MAC/request headers；JSON `{ "status": "succeeded", "result_text": "业务处理结果" }`，也可 failed。文本最多 8192 字，只回存结果，不扣豆、不自动推送播报。同一结果可重试，终态不同结果拒绝。

照片回执仅表示拍摄/接收完成。`environment_snapshot` 保持 queued，直到业务服务明确提交结果；不会把照片上传成功当作环境分析成功。

## 用户查询

- `GET /api/v1/live/devices/{deviceID}/business-events?limit=20&before_id=...`：客户登录、当前设备归属校验、租户隔离；返回历史事件，不包含 MAC 或 OSS object key。
- `GET /api/v1/live/devices/{deviceID}/business-events/{eventID}/image`：客户登录、当前设备及事件同租户校验后代理读取 JPEG；private/no-store、不允许嗅探。

## 设备展示与名称

Provision 新增 `room_name` 和 `room_number`，后者是房间 `external_room_id`，不是数据库主键；数据来自同租户当前绑定的房间。

新设备默认名为“小蓝搭子”“小蓝搭子02”。旧默认名参与序号兼容，避免重复编号。数据库和网页保留已有名称；仅当旧名称与系统序号完全匹配、从未有 RENAMED 审计且无同租户目标名称冲突时，设备 Provision 返回安全新名展示别名。用户自定义名称不强制替换。

## 称呼偏好与声音属性

- `GET /api/v1/live/devices/{deviceID}/addressing`：返回 `{ "mode": "auto" }`，必须客户登录且库存当前客户、硬件已认领客户均为当前租户。
- `PUT /api/v1/live/devices/{deviceID}/addressing`：JSON `{ "mode": "auto|female|male|child|neutral" }`，仅当前归属客户可改自己的设备。改动有审计；设备转移不继承前客户偏好。

voice 顶层 `speaker_addressing` 总为 `female/male/child/neutral` 字符串。用户指定 female/male/child/neutral 时优先；auto 时仅对可用的非纯唤醒录音进行声音属性辅助判断。它不是人的真实性别、年龄或身份认证，不识别人，也不建立声纹库。不得根据 ASR 文字、自报身份或历史称呼猜测。

自动分析复用 DashScope key，通过 [阿里云官方 Qwen Omni 音频理解接口](https://help.aliyun.com/zh/model-studio/qwen-omni)，仅发送音频与固定声学判断提示。使用 `qwen3.8-omni-flash`、文本流式输出、关闭推理；Ogg 用 ffmpeg 内存解码为 16k 单声道 PCM WAV。分析与偏好查询合计最多 3 秒，低置信（成人音色 <0.88、儿童音色 <0.95）、多人、重噪声、少于1.5秒有效人声、静音、无效结果、未配置及任何调用失败一律 neutral。仅唤醒默认 neutral，避免对短音强判。模型自报 confidence 只是过滤提示，不是统计校准概率。

只保存当次音色标签及来源（preference/acoustic/neutral），不另存录音、身份、声纹或模型原始输出。临时 ASR 录音照旧在请求结束删除。问候与声音属性仍 `billing_mode=disabled, charged_beans=0`；网关根据该标签选择芊悦反馈文案，不在此接口生成语音。

## 配置

复用已有 `XIAOZHI_INTERNAL_TOKEN`、`DASHSCOPE_API_KEY`、可选 `DASHSCOPE_ASR_MODEL` / `DASHSCOPE_ASR_BASE_URL`，以及 `OSS_ENDPOINT`、`OSS_PUBLIC_ENDPOINT`、`OSS_BUCKET`、`OSS_ACCESS_KEY_ID`、`OSS_ACCESS_KEY_SECRET`。即使 `STORAGE_DRIVER=local`，只要 OSS 配置齐全，Registry 仍注册 OSS；设备录音和照片显式选择 OSS。

声音属性开关为 `DEVICE_ADDRESSING_MODEL`，空值关闭自动分析，启用可填官方 `qwen3.8-omni-flash`。可配 `DEVICE_ADDRESSING_BASE_URL`（兼容模式API根路径；推荐工作空间专属北京 Host）与 `DEVICE_ADDRESSING_FFMPEG`（默认 ffmpeg）。没有另一个音频 API key。服务不会因声音属性调用失败阻止已识别的设备指令。

语音请求处理总预算29秒（网关30秒），其中ASR最多25秒、可选称呼最多3秒，称呼分析额外为持久化提交预留最后1秒。成功结果带完整Content-Length并显式Flush后，才进行最多3秒的同步私有录音删除；删除耗时不会拖延已提交的成功JSON，也不创建无界后台goroutine。

迁移由 `MigrateDeviceProvisioning` 创建 `device_business_events`、`device_addressing_preferences`、`device_voice_addressing`。无需修改现有钱包、库存 SKU、归属或绑定。有限 fixture 白名单包含新增表；MySQL 集成测试使用随机前缀隔离表并自动清理，不创建真实测试设备。
