# 桌面商品卡字段改名恢复

用户要求点商品卡修改笔后也能修改字段名称。当前后台和普通 web 已包含 2feb70f 的 field_labels 支持，但实际 web-desktop 源码为 WorkBuddy 快照加正式保存修复 64e6064，未包含四个前端接入点。

本次以实际部署的 64e6064155d247ef70b93f403afba4d8cbf733cb 为独立桌面基线，只恢复 LiveProductLinkCards.vue、LiveStrategyView.vue、api.ts 和 types.ts 的字段名编辑接入。保留 selling_points 类型、WorkBuddy 页面以及 64e6064 正式方案保存流程；不把这份桌面快照整树合入 main。

- 规格、销售价、数量、适用的修改笔同时打开名称与内容；名称按商品保存，刷新保留，未改商品沿用默认名称。
- 名称限制 1—32 字且不含换行或控制字符。取消不提交；保存失败保留输入。标签与内容之间移动焦点不触发提前保存。
- 使用现有 field_labels、expected_version_no 和租户参数；内部字段键与生成管线不变，后台不改、不重启。
- 字段文字和编辑控件至少 16px；1600、1024、390、320px 验证无横向溢出。

验证：生产构建及类型检查通过；20 项既有前端测试通过；真实 LiveStrategyView 合成 API 浏览器验证四字段改名、刷新、取消、失败保留、每商品隔离及四种宽度；原正式方案保存的动态版本、失败、迟到响应和手机回执回归通过。没有修改生产商品资料，没有调用模型或 TTS。

证据目录：C:/Users/19918/Documents/Codex/2026-10-14/d-myskill-skill/work/product-field-label-desktop-proof。发布仅增加 web-desktop 哈希资源并替换其入口，继承实际 current 的其他文件；完整校验、备份及上线状态以该目录的发布报告为准。
