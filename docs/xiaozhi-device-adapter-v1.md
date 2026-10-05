# 小智设备接入小蓝搭子 V1

## 目标

把 ESP32-S3 小智设备接入 `www.xiaolandaizi.cn`，作为直播间声音分发终端。

原则：

- Core 不识别“小智”这种设备类型，只负责房间唯一最终声音。
- 小智协议放在独立 `xiaozhi-gateway` 适配层。
- 一个小智设备第一阶段绑定一个直播间。
- 设备端播放的是 Room Audio Engine 的最终合成声音，不重新做场控决策。

## 当前链路

```text
ESP32-S3
  -> HTTPS POST /xiaozhi/ota/
  <- websocket.url + token + version=1
  -> WSS /xiaozhi/v1/
  <-> xiaozhi-gateway :8083
  -> Core /v1/rooms/{roomID}/composite.pcm
  -> PCM S16LE / 24kHz / mono
  -> FFmpeg libopus / 60ms
  -> WebSocket binary raw Opus
  -> ESP32-S3 speaker
```

## 服务端接口

公网：

- `https://www.xiaolandaizi.cn/xiaozhi/ota/`
- `wss://www.xiaolandaizi.cn/xiaozhi/v1/`
- `https://www.xiaolandaizi.cn/xiaozhi/healthz`

本机内部：

- `GET http://127.0.0.1:8083/healthz`
- `GET /internal/v1/bindings`
- `PUT /internal/v1/bindings/{deviceID}`
- `DELETE /internal/v1/bindings/{deviceID}`

内部绑定接口要求请求头 `X-Xiaozhi-Internal-Token`。

## 鉴权

设备 OTA 请求必须带：

- `Device-Id`
- `Client-Id`

OTA 根据 `XIAOZHI_DEVICE_SECRET` 对 `Device-Id + Client-Id` 生成专属 token。

设备连接 WebSocket 时使用：

- `Authorization: Bearer <token>`
- `Protocol-Version: 1`
- `Device-Id`
- `Client-Id`

生产密钥由首次部署脚本自动生成到 `/etc/xiaolan/xiaozhi.env`，不进入 Git 和 release。

## 协议范围

V1 先启用官方 WebSocket 二进制协议 version 1：

- JSON hello / TTS 状态；
- binary frame 为原始 Opus packet；
- 下行 24kHz mono；
- Opus frame duration 60ms。

官方文档说明协议还存在 v2/v3 封装；第一阶段先由 OTA 返回 `websocket.version=1` 固定设备使用 v1。真机如出现固件忽略该配置，再补 v2/v3 封装，不改 Core。

参考：

- https://github.com/78/xiaozhi-esp32/blob/main/docs/websocket.md
- https://github.com/78/xiaozhi-esp32/blob/main/main/Kconfig.projbuild

## 设备与直播间绑定

V1 先用 gateway 自己的持久化绑定表，文件放：

`/opt/xiaolan/state/xiaozhi-bindings.json`

原因是设备协议身份是 MAC / Device-Id，而现有 Management 的直播设备使用库存 `inv_devices.id`，两者目前没有稳定的一对一协议身份字段。

生产绑定助手：

```powershell
.\scripts\bind-xiaozhi-device.ps1 -DeviceId "aa:bb:cc:dd:ee:ff" -RoomId 15
```

解除：

```powershell
.\scripts\bind-xiaozhi-device.ps1 -DeviceId "aa:bb:cc:dd:ee:ff" -RoomId 15 -Action unbind
```

后续正式产品化应把 Device-Id/MAC 映射回 Management 的库存设备 ID，复用 `live_device_room_bindings`，避免形成两套长期业务绑定状态。

## 部署组成

新增：

- `xiaozhi-gateway/cmd/xiaozhi`
- `deploy/systemd/xiaolan-xiaozhi.service`
- `configs/xiaozhi.env.example`

生产 release 增加：

- `bin/xiaozhi-gateway`
- `systemd/xiaolan-xiaozhi.service`

部署脚本会：

1. 检查 FFmpeg 和 libopus；
2. 初始化 `/etc/xiaolan/xiaozhi.env`；
3. 初始化 `/opt/xiaolan/state`；
4. 安装/启用 `xiaolan-xiaozhi.service`；
5. 同 Core / Management 一起重启和健康检查；
6. 回滚到不含 gateway 的旧 release 时自动停止 gateway。

## 当前自动验证

已经覆盖：

- OTA 返回 WSS、token、version=1；
- token 与 Device-Id / Client-Id 绑定；
- 设备绑定文件持久化；
- Ogg 页面拆出原始 Opus packet；
- 模拟 Core 输出 PCM；
- 模拟小智 WebSocket v1 客户端完成 hello；
- 实际调用本机 FFmpeg/libopus；
- 收到 TTS start、binary Opus、TTS stop；
- `go test ./...` 通过；
- `go vet ./...` 通过；
- 本机构建通过；
- Bash 部署/回滚/绑定脚本语法通过；
- PowerShell 发布/状态/日志/绑定脚本语法通过。

## 2026-10-01 生产落地状态

已完成第一阶段生产旁路上线，不切换现有 Core / Management release：

- `xiaolan-xiaozhi.service` 已启用并为 active；
- 生产 gateway 当前使用旁路目录 `/opt/xiaolan/xiaozhi-sidecar/current/xiaozhi-gateway`，后续正式 release 会由发布脚本切回 `/opt/xiaolan/current/bin/xiaozhi-gateway`；
- 部署二进制 SHA-256：`16ab00de4df1cbe25f526ba5b75a25d20b684d44a2fceb412b67bc286a02d543`；
- 服务器 `/usr/bin/ffmpeg` 已确认具备 `libopus`；
- `127.0.0.1:8083/healthz` 已返回 `status=ok`、`ffmpeg_available=true`；
- 主域 Nginx 已增加 `/xiaozhi/healthz`、`/xiaozhi/ota/`、`/xiaozhi/v1/`；
- Nginx 修改前备份：`/etc/nginx/xiaolan-backups/xiaolan.20261001T145709Z`；
- `nginx -t` 通过并 reload 成功；
- 公网 `https://www.xiaolandaizi.cn/xiaozhi/healthz` 已通过；
- 公网 OTA POST 已返回 `wss://www.xiaolandaizi.cn/xiaozhi/v1/`、非空 token、`version=1`；
- 电脑端、客户手机端、Management API、Core proxy 原有生产冒烟仍全部通过。

## 仍需真机完成

1. 确认手上 `merged-binary.bin` 是否把 OTA URL 固化为官方地址。
2. 如是，需要编译一版 OTA URL 为：
   `https://www.xiaolandaizi.cn/xiaozhi/ota/`
3. 从设备串口日志读取真实 Device-Id / Client-Id。
4. 把 Device-Id 绑定到测试直播间。
5. 真机验证 WSS 握手、Opus 播放、断线重连。
6. 确认设备是否需要“常驻播音接收模式”；官方聊天固件通常按会话打开音频通道，直播终端可能需要固件侧自动建连/保活改造。
7. 真机稳定后再接 Management 正式设备身份与状态灯。
