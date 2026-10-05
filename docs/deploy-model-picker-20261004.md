# 模型列表弹窗选择器（2026-10-04）

已上线版本：`20261004-model-picker-v2`，基于 `20261004-speech-model-discovery-v1`。

“刷新模型列表”位于模型 ID 标题旁。点击打开弹窗并获取当前服务商列表，顶部搜索、单选表格、每页 10 个。勾选暂存于弹窗，点击“选中”才回填模型 ID。取消、关闭、Esc 不修改原值。关闭后迟到的响应不重新打开弹窗。不自动保存或切换默认模型，保留手动输入。

实现：`web-console/src/views/SpeechModelsView.vue`、`web-console/src/components/ModelPickerDialog.vue`。

验证：

- `pnpm --filter web-console build` 成功（原有 Node 版本与大包警告仍在）。
- `node scripts/test-model-picker.mjs` 使用隔离浏览器与本地模拟数据通过：23 个模型、3 页、搜索重置页码、单选、确认回填、取消和 Esc、延迟响应、异常、空列表、手填、移动端宽度。
- 桌面与手机截图保存在 `artifacts/model-picker-desktop.png`、`artifacts/model-picker-mobile.png`。
- 部署包仅含当前前端依赖图，过滤历史资源。服务器核对全部非目标 JS（忽略构建引用哈希）和 CSS 与线上版本一致，只允许 SpeechModelsView 改动。
- 原 HTML 中仅更新主模块路径，保留其余页面入口和旧资源。服务未重启，Management/Core/Xiaozhi PID 不变，密钥及默认模型配置摘要不变。
- 公网首页、主 JS、模型选择页面 JS/CSS 与已发布文件逐字节一致。

回滚可切换 `/opt/xiaolan/current` 至上一版本，不需要数据库恢复。备份与源码清单位于 `/opt/xiaolan/deployments/20261004-model-picker-v2-backup`。

本次没有调用真实服务商生成文字，也没有修改客户直播间或智能体模型。
