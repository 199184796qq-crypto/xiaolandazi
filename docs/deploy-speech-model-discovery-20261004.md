# 模型列表选择功能上线

- 2026-10-04 20:03，版本 `20261004-speech-model-discovery-v1`。
- 基础版本 `20261004-speech-models-v3`；发布管理服务和桌面网页。
- 新接口 `POST /api/v1/system/speech-models/models`，平台管理员/超级管理员权限，no-store；只读取列表，不执行生成或修改配置。
- UI：获取/刷新列表、搜索名称及 ID、下拉选择、继续手动填写；地址/协议/密钥变化使旧列表失效，异步旧响应不会应用。
- OpenAI Chat/Responses 使用认证头 GET models；Gemini 使用 x-goog-api-key，过滤 generateContent；Anthropic 使用 x-api-key/version。原生协议有分页，失败不回显上游正文或密钥。
- 管理服务 SHA256 `ccc1e2382e153bec115c875111c45069e83c3372de5b9b39106b428a2fdbbdf2`。
- 桌面入口 `index-B4uJztIB.js`；模型页 `SpeechModelsView-D3KNTUSs.js`、`SpeechModelsView-c9bYoq9k.css`。主入口去除资源 hash 引用后与基础版本完全相同；现有其它页面内容、样式和 HTML 附加脚本保留。
- 所有管理服务 Go 测试、限定包 vet、Vue 类型检查和网页构建通过。新增列表测试覆盖四协议、认证头、完整生成地址转列表地址、空模型 ID、排序去重、原生分页、过滤、重复游标、错误/超大响应、密钥不泄露、公网约束及权限。
- 公网新列表接口未登录返回 401；模型页面 JS 可获取，选择器 CSS 返回 200；管理服务健康 ok，三项服务均 active。
- 现有 1 组配置、revision 1、1 个加密凭据、默认 ID 空均保持。发布前后配置/密钥 hash、直播间/授权/策略/绑定/发布/方案音频快照一致。
- Core PID 191105、设备网关 PID 191108 未变化；菜籽油直播间监听继续开启，音频 idle。没有重启 Core/网关、启动直播、变更默认模型或生成声音。
- 环境文件全部保持，没有更换加密主密钥；新增功能无数据库迁移。
- 备份 `/opt/xiaolan/deployments/20261004-speech-model-discovery-v1-backup`；数据库仅备份，不自动恢复。
- 外部真实模型列表请求未代用户执行；协议与分页验证使用本地 TLS fixtures。用户在页面点击获取后使用其配置的实际接口。
