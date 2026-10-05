# 运维 / 客户直播间界面统一

## 范围

- 继续复用 `RoomDetailView.vue`，所有身份采用客户侧的合成口播字幕、公屏 / 事件聚合 / 互动执行布局。
- 关闭仅因运维身份而出现的旧分轨口播、独立 Agent 思考 / 待打断队列和重复事件聚合面板。
- 停止未启用旧面板时的运维专用 speech-missions 轮询。
- 保留已有客户授权、服务端逐请求权限检查及授权撤销清理。未授权运维不得查看私有方案和配置。
- 未修改营销资格、价格、佣金规则、财务记录、模型配置或直播调度器。

## 验证

- `node web-console/scripts/test-room-parity.mjs`（在 web-console 目录运行）：客户 / 已授权运维模块、标签、字幕一致；未授权保护；空执行队列及 1 执行 + 10 等待；旧面板及无用轮询不出现。
- `test-live-support-access.mjs`、`test-ops-rooms.mjs`、`test-model-picker.mjs` 均通过。
- `vite.room-parity.config.ts` 隔离工作区未上线的风格评测 UI，并按已部署源码摘要校验。
- `prepare-room-parity.mjs` 校验其余运行时前端源码及 CSS 规则未改变；仅 Vue scope / 动画 scope 标识随目标组件改变。

## 发布

- 版本：`20261004-room-parity-v1`，基线：`20261004-ops-rooms-v1`。
- 只更新 web / web-desktop 活跃资源，不重启任何服务，不修改数据库。
- 发布前后管理、Core、小智 PID 均一致；环境、模型密钥与默认配置、内容策略、方案绑定及发布关系摘要一致。
- 公网 27 个活跃资源逐文件验证通过，三项服务均 active。
- 回滚保留基线版本；部署快照：`/opt/xiaolan/deployments/20261004-room-parity-v1-backup`。
