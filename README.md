# 直播伴播

AI 直播销售伴播系统。当前项目采用明确的三层结构：

1. **Core Service**：无界面核心业务服务，负责真实直播业务逻辑。
2. **Management Service**：管理/API 服务，负责身份、客户范围、权限和网页数据接口。
3. **Web Console**：统一网页控制台。平台管理员和客户共用同一套页面，仅数据范围不同。

## 当前目录

~~~text
E:\直播伴播

├─ core-service
│  ├─ collector       # 直播平台采集适配层
│  ├─ room            # 房间核心数据
│  ├─ events          # 公屏事件与实时事件总线
│  └─ httpapi         # 仅供 Management 使用的内部 API
│
├─ management-service
│  ├─ auth            # 用户 / scope
│  ├─ db              # 客户与账号
│  ├─ coreclient      # Core Service 客户端
│  └─ httpapi         # Web Console API
│
├─ collector-worker   # Playwright 浏览器采集 Worker（Core 子进程）
├─ web-console        # Vue 3 + TypeScript + Vite
├─ device-simulator   # 未来盒子协议模拟器
├─ docker             # MySQL / Redis
├─ configs            # Core / Management 配置样例
├─ data
├─ docs
└─ scripts
~~~

## 服务边界

~~~text
直播平台
   ↓
Core Service
   ↓
MySQL / Redis
   ↑
Management Service
   ↑ HTTP / SSE
Web Console
~~~

Web Console **不直接访问 Core Service**。

Core Service 不知道 Vue、客户页面或平台管理页面是什么，只处理：

- Room
- Collector
- RoomEvent
- RoomState
- Agent
- SpeechTask
- Device

Management Service 负责：

- 用户身份
- Tenant
- 权限范围
- 平台管理员 / 客户 scope
- 房间管理 API
- 实时公屏转发

## 当前已跑通

- Docker / MySQL / Redis
- Core Service Windows/Linux amd64/Linux arm64 构建
- Management Service Windows/Linux amd64/Linux arm64 构建
- Management → Core → MySQL 房间增删查
- 开发期公屏事件写入与读取
- 平台管理员 scope
- 客户 tenant scope
- Web Console 生产构建
- 房间卡片
- 添加 / 删除房间
- 房间详情
- 实时公屏 SSE
- 真实抖音 WSS 采集（Playwright Worker → Go protobuf）

## 下一阶段

下一阶段只在 Core Service 中实现真实抖音 Collector：

~~~text
房间号
  ↓
Douyin Collector
  ↓
RoomEvent
  ↓
Core Event Bus
  ↓
Management SSE
  ↓
Web Console 实时公屏
~~~

采集层计划保留双内核：

- Lightweight Collector：大量房间主路径
- Browser/CDP Collector：兼容 / 降级路径

上层只认统一 RoomEvent。

## 当前扩展能力（2026）

项目已扩展为完整的 AI 直播伴播运行平台。

新增模块：

- `audio-service`：声音分发服务，负责 Core 音频任务向多终端广播。
- `semantic`：语义智能引擎，负责语义追踪、问题聚类、Embedding/RAG 接入。
- `device`：终端设备管理体系，支持 PC、盒子、小智 ESP32-S3。

## 小智设备接入链路

```text
小智 ESP32-S3
    ↓
www.xiaolandaizi.cn
    ↓
小智网关
    ↓
Audio Service
    ↓
Core 直播声音流
```

设备生命周期：

```text
注册
 ↓
绑定直播间
 ↓
Heartbeat 心跳
 ↓
接收声音任务
 ↓
解绑/注销
```

## Semantic Engine

语义处理链路：

```text
直播公屏
 ↓
事件分析
 ↓
问题聚类
 ↓
语义召回
 ↓
RAG上下文
 ↓
场控 Agent
 ↓
TTS 输出
```

设计原则：

- 不阻塞直播采集
- AI 服务异常可降级
- 模型供应商可替换
- Core 与具体模型解耦

## 当前推进方向

- 向量数据库生产化
- 真实直播数据灰度验证
- 语义记忆优化
- RAG 精准召回
- 小智 ESP32-S3 真机联调
- 阿里云生产部署完善

## 小智设备能力规划

小智设备收到用户语音后，优先识别设备控制指令，不进入直播智能体业务链路，不消耗 AI 时长。

详细方案见：`docs/小智设备控制Agent_音量与字体控制方案.md`

### 第一阶段能力

| 能力 | 示例指令 | 协议 action |
| --- | --- | --- |
| 音量控制 | 声音大一点 / 静音 / 最大声音 | `volume` |
| 字体大小控制 | 字体大一点 / 恢复默认字体 | `font_size` |

统一控制协议：

```json
{
  "type": "device_control",
  "action": "volume",
  "value": 70
}
```

### 架构原则

- 设备控制指令在网关侧完成 ASR + 意图识别，由 Device Control Agent 直接下发设备执行。
- 设备端只负责：麦克风采集、控制指令执行、播放声音、UI 显示。
- Core 只负责：直播业务、智能体、TTS、直播策略。设备控制不进入直播智能体。

### 后续预留能力

调节亮度、查询设备状态、切换声音、WiFi 配置、重启设备、开始/停止智能体，均走统一 `device_control` protocol。
