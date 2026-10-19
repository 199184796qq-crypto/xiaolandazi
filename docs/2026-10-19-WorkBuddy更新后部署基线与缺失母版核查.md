# WorkBuddy更新后的部署基线与缺失母版核查

日期：2026-10-19。来源目录：`E:\直播伴播`。用户要求后续部署先核对实际服务器，不能覆盖WorkBuddy刚发布的内容。本轮仅只读核查和记录，没有部署、重启、上传/改写生产文件、改客户资料或调用模型/TTS。

## 实际运行基线

北京时间12:52:11捕获：

| 项目 | 现场结果 |
| --- | --- |
| current | `/opt/xiaolan/releases/20261019-rolling-scheduler-e93437b` |
| Management PID | `1170098` |
| Management VCS | `1752bb6fa614cbc95c4c64db0e73c1b2f8f42f0a` |
| 构建工作树 | `vcs.modified=true` |
| Management SHA256 | `b9ec504447dcf57df95164187e743ca6fdaf41b3a4fa93717d240497d6c94c8b` |
| Core / 网关 PID | `775073` / `775930`，保持 |
| 发布文件数 | `3689` |
| Nginx | 与前次基线一致 |

目录名称没有改变，但程序实际已变；不能用旧release名称代表二进制来源。VCS指向1752bb6也不能证明等同该干净提交，因为构建带未提交改动。当前本地还存在WorkBuddy对P0、正文生成、风格提取及其测试的未提交变化，本轮未修改、未代提交。

对照此前滚动调度发布捕获，变化为`bin/management-service`、`configs/prompts/anchor-style/style-meta-prompt-extraction.md`，另新增两份`.bak-20261019`备份，未见删除。现场风格提取母版与E盘未提交文件哈希一致，与HEAD不同；后续必须保留，不能被仓库旧母版回退。没有把“所有本地未提交改动都一定编进了该程序”当结论。

## 两份缺失依赖

实际进程的`ANCHOR_STYLE_PROMPT_DIR`明确指向`/opt/xiaolan/current/configs/prompts/anchor-style`。

1. 该目录缺`first-live-opening.md`。二进制含`completeLiveCycleText`和`firstOpeningDirective`。对应`interactionpolicy.Read`在显式目录下不回退其它安装，首次开场路径会返回缺母版/避重准备失败。长期库计算规则指纹也读取此母版。
2. 该目录缺`compliance/L1/p0-generation.md`。二进制含`loadGenerationP0`；进程没有设置`ANCHOR_STYLE_P0_GENERATION_PATH`，工作目录和current下的相应文件都不存在。该加载路径会返回生成侧母版缺失。

同时，服务器`p0-hard-block.md`、`mainline-text-task.md`仍与HEAD相同，与WorkBuddy当前未提交内容不同。这不是本次替换它们的授权；应由WorkBuddy交代哪些变更已完成、哪些仍在试验，再做完整依赖核对。

上述为文件存在性、程序符号及加载器路径检查；未启动直播或付费复现，不声称已观察到某场真实错误，也未检查实际话术效果。

## 后续发布执行规则

- 以**发布时服务器实际current及文件哈希**为基线；不能复用本聊天以前的二进制或整包直接覆盖。
- 先接收WorkBuddy的本次构建源码和母版交接，将完成的源改动提交并整合到主线；不要把未知dirty构建当成干净1752bb6，也不要直接用干净1752bb6覆盖它。
- 继承实际current的完整前端、母版、Nginx关联文件、模型池和客户资产，只对本次授权差异发布。补齐新母版并更新真实MANIFEST；生产独立母版继续保留。
- 构建、完整测试、实际母版依赖加载、接口冒烟都通过；存在新SQL时补隔离集成测试。只看进程启动或healthz不能证明正文生成可用。
- 发布锁内重新核对current、PID、程序及文件哈希，并检查直播/Core空闲；若WorkBuddy并行更新，先重新整合，不能忽略守卫强制切换。
- 回滚也必须保留本次WorkBuddy更新的真实前驱；不能回到本聊天09:06捕获的老程序而丢失新改动。

## 证据

`E:\直播伴播-local\predeploy-workbuddy-check-20261019`：

- `production-actual.json`：完整文件、Nginx及三服务程序指纹。
- `summary.json`：与此前捕获的差异摘要，生产写入次数0。
- `prompt-comparison.json`：两份新文件缺失，现用母版与工作树/HEAD的哈希关系。
- `loader-paths.json`：允许展示的加载路径与存在性，不含凭据。

这份捕获是12:52的观察，不是永久可复用的发布许可；执行任何后续发布前重新采样。
