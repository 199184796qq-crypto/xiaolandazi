# 保存方案修复上线与交接

2026-10-18，用户明确授权“部署上线”。本次发布已完成，供 WorkBuddy 接续查阅；后续源码仍统一在 `E:\直播伴播`。

## 已部署版本

- 生产目录：`/opt/xiaolan/releases/20261018-plan-save-source-mapping-cd389df`。
- 源码：`cd389dfb70eb53a92b6aae5b4eff68cd08b6f493`，GitHub `main` 已推送。
- 北京时间：15:11:54—15:11:57（UTC 07:11:54—07:11:57）。
- Management：PID `1098707`，程序 SHA256 `c27de1b8b79afb889337a7586ac8a9383f4776f5077d934ee687c87be1ae6653`。
- 前端普通入口和桌面入口使用同一源码构建，实际桌面主资源为 `index-DOnYOXdo.js` / `index-DkCuoEhW.css`。

## 本次行为

1. 保存按钮直接请求版本保存，不再因为内容池 `needs_update` / `failed` 强制先更新内容并等待。这是 WorkBuddy 已提交的前后端保存调整，本次一起发布。
2. 内容池生成正确剥离人工归属标题，按真实分类/名称回查来源，使用 `knowledge:N`；公共和商品人工归属保持。删除 `facts:line:N` 假来源兜底，缺失/歧义返回可定位错误。`ValidateSet()` 完整性核对继续保留。
3. WorkBuddy 已提交的风险事实剔除与保存回执同时包含；来源修复未改其风险处理代码。`latestSavedContentPoolSet()` 在旧线上已有，不能把它描述成本次新增。

用户刷新页面后可重新点击“保存方案”。本次未代用户保存新版本；实际登录保存产生的新版本、风险回执以及后续初始化/直播效果，分别按真实操作和日志验收。

## 发布保护与核验

以实际发布前的 `20261018-remove-navigation-tagline-0b90f61` 完整文件与运行程序为基线，持共享发布锁，三次核对所有运行会话及 Core 音频空闲后切换；未停止客户直播。

仅替换管理程序、`web/index.html`、`web-desktop/index.html`，添加40个哈希资源及来源说明。保留所有旧资源和回滚目录。发布后共有3642文件，3598个原有文件哈希未变。

Core PID775073、网关PID775930及其程序未变；Nginx、系统设置、模型池、13类方案资产发布前后指纹一致。管理服务、Core、网关健康检查通过。

12个公网入口/资源核对通过，11个接口按真实方法核对未登录401，包含事实类型保存/建议、带方案分类读取、版本保存、内容池更新和模型池入口。此项是路由/鉴权检查，不冒充登录业务保存。

完整前端备份共2775文件，哈希、大小和权限与实际生产一致：

- `E:\直播伴播-local\frontend-preservation\20261018-plan-save-source-mapping-cd389df\deployed-frontends.tar.gz`
- 权威指针：`E:\直播伴播-local\frontend-preservation\latest.json`
- 旧C盘发现入口已指向同一份E盘备份。

## 验证范围

- 三个 Go 包回归：945项通过、89跳过、零失败；实际9条原输入离线来源及范围回放通过。日志在 `E:\直播伴播-local\save-plan-investigation-20261018\source-mapping-regression.jsonl`。
- 管理服务全部命令编译、Linux部署程序构建、前端类型检查/生产构建通过。
- 使用真实 `LiveStrategyView.vue` 与模拟接口：内容池待更新、失败均直接保存；不调用内容池更新；重复点击只请求一次；失败可重试；方案/租户/直播间及实时参数载荷正确；390px无横向溢出，无页面异常。
- 无生产商户事实/商品/方案版本写入，无真实模型或TTS付费调用。

首次上传在切换前被包范围检查拦截：打包包含了字节完全相同的旧图片子目录，范围白名单不接受。精简为本次三个替换文件和40个新增资源后重新核验并发布；首次没有切换或重启。相应过程证据保留。

## 证据与协作

完整证据：`E:\直播伴播-local\plan-save-deploy-20261018`，包括 `deployment-report.json`、`production-verification.json`、前后生产指纹、构建包、浏览器验证与备份元数据。

WorkBuddy未提交的 `live_derived_safety_test.go` 与两份原待办/交接文档均保留，没有代提交。未修改其它业务源码。本次收尾仅提交本部署说明、`AGENTS.md` 与共享交接记录；后续接手同文件仍按 `E:\直播伴播\协作交接.md` 登记。

回滚基线为 `/opt/xiaolan/releases/20261018-remove-navigation-tagline-0b90f61`，程序原PID1085092对应旧构建4884904。后续若需回滚，仍须取得共享锁、重新检查实际current及运行空闲，再原子切换并重启Management；不能照旧指纹覆盖并行发布。
