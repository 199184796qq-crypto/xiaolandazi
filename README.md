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