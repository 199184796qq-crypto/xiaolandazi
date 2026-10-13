# 声音模型接入与页面设计

2026-10-10 02:40（北京时间）已上线 `20261010-cosyvoice-libraries-v2`。

## 使用方式

刷新声音管理页，在顶部“大模型”中选择：

- Qwen Audio：保留已有官方音色、上传声音，以及声音档案上的模型选择；另加入 Qwen 3.0 Flash 选项。
- CosyVoice 3.5 Flash：专属声音库，支持上传录音复刻，也支持通过文字描述设计声音。
- CosyVoice 3.5 Plus：独立声音库，创建方式相同。

切换顶部模型只切换展示的声音库。试听不会修改直播间声音；点击“使用这个声音”后才保存所选声音及其对应模型绑定。

CosyVoice 3.5 无预置官方音色，因此官方声音区显示说明和创建入口；没有把 Qwen 或 CosyVoice v3 的音色冒充为 v3.5 音色。Flash 与 Plus 的音色 ID 不能混用，个人声音需按所选模型重新创建。既有自定义声音名额规则保留，所有模型共用。

## 界面预览

以下图片使用测试数据展示布局，不是生产声音列表。

- [Qwen 声音管理](C:/Users/19918/Documents/Codex/2026-10-09/zh/outputs/声音模型接入/Qwen声音管理.png)
- [CosyVoice 官方声音区](C:/Users/19918/Documents/Codex/2026-10-09/zh/outputs/声音模型接入/CosyVoice官方声音区.png)
- [CosyVoice 声音设计](C:/Users/19918/Documents/Codex/2026-10-09/zh/outputs/声音模型接入/CosyVoice声音设计.png)
- [CosyVoice 我的声音](C:/Users/19918/Documents/Codex/2026-10-09/zh/outputs/声音模型接入/CosyVoice我的声音.png)

## 验证结果

- Management 全量 Go 测试、相关包 `go vet`、网页类型检查和生产构建通过。
- 浏览器测试通过：Qwen兼容、模型切换不修改当前声音、Flash/Plus库隔离、上传目标模型、声音设计失败重试、试听不应用、所选绑定应用、其他直播间保留、页面选择记忆、窄屏布局。
- 原有上传声音与房间模型选择记忆回归测试通过。
- 阿里真实接口联调：两个模型均完成声音设计 → 合成 → 复刻 → 复刻音色合成，返回有效 WAV。复刻合成时发送四川话指令；接口成功不代表已经完成方言听感验收。联调创建的远端临时音色已清理，没有改动原声音档案。
- Management、Core、Gateway 健康检查均为 HTTP 200；公网桌面 HTML 与四个入口 JS/CSS 的哈希与发布产物一致。
- 发布前后原声音档案、模型绑定、房间发布关系及声音配置摘要一致，Core 与 Gateway PID 保留；完整母版目录共 74 个文件，哈希清单校验通过。

## 发布信息

- 基于 `20261010-021000-text-model-isolation`，保留已上线的文字模型隔离及互动纠正历史功能。
- 源码分支：`codex/cosyvoice-model-library`。
- 发布源码提交：`865c86810234b7827c28260c046af08a08f2234c`。
- 本次更新管理服务和桌面网页，增加 CosyVoice 表达母版；没有代替用户选择新的生产声音，也没有启动直播。
- 生产包 SHA256：`34511303f6528e64f1077cec7b448bbfabc0914ddb9e54c4184858c1ce56ab0e`。

官方说明：[CosyVoice 3.5 Flash](https://help.aliyun.com/zh/model-studio/cosyvoice-v3-5-flash)、[声音设计](https://help.aliyun.com/zh/model-studio/voice-design-user-guide)。
