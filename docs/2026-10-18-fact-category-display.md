# 事实类型中文回显修复

用户反馈已采纳事实的类型下拉框回显 `appointment`、`environment`、`location`、`package`、`per_capita` 等英文标识，部分长名称被截断。

## 原因

实际运行的 Management SHA256 为 `84a05d59c010c418dce1c02da244a0cc526c891e1d7aa2243dcb85e8056758bd`。核对实际程序与其对应的 `E:/speech-generator-build-main` 源码，行业合并函数虽然读取 `plan_id`，同时还通过 query 中的 `tenant_id` 加载方案行业。前端只传方案 ID，客户身份鉴权仍成功，但行业合并被提前跳过，响应只有全局分类。事实中已有餐饮行业标识，因此原生 select 只能显示未匹配项的原始标识。

客户事实已保存的 category 没有丢失；本次不需要重新分类或修改数据库。原生单行 select 的展示空间也不足以完整显示较长中文名称。

## 修改

- 分类加载器同时传 `plan_id`、`tenant_id`。客户身份可从当前登录会话取租户，运维协助等场景显式使用当前目标直播间/方案所属租户。各调用点及重试、资料刷新、右键建议刷新均沿用当前范围。
- pending 合并、范围切换清空和迟到响应检查同时包含租户与方案，避免不同范围复用分类。
- 事实类型控件保留原生 select 和原逐条保存 API，用可换行中文覆盖层显示已选名称，文字为黑色 16px；选项、键盘焦点和可访问名称保留。
- 分类未加载/加载失败时禁用更改。真正未匹配的旧类别显示“未匹配分类”，保留原 category，并在提示中提供原标识；不会擅自改成“其它”。保存失败仍保留原选项及可重试错误。

本次保留 WorkBuddy 接入的逐条类型修改、版本校验及右键“建议分类”，也保留归属筛选/排序、事实正文搜索、四层详情、原位修改与新增。不会恢复已取消的批量类型或商品导入功能。

## 来源与验证

从实际线上 `20261018-merge-industry-fact-categories-0046` 取得文件清单，捕获 WorkBuddy 当前前端源文件原始字节；修复前重建的 49 个产物在 web、web-desktop 两个入口均 49/49 哈希相同。只将这些已经上线的前端改动捕获进基线提交，不带入其进行中的后端或提示词修改。WorkBuddy 的两个 E 盘开发目录均不写入。

- `npm run build`：类型检查和生产构建通过。
- `node scripts/test-fact-category-catalog.mjs`：全局回退、方案与租户 query、17 项模拟行业表、客户会话兜底、不同租户范围、pending 合并、迟到响应隔离、失败重试通过。
- `node scripts/test-formal-fact-type-display.mjs`：中文名称、旧值保留、加载/失败禁用、无自动写入、保存失败/重试及并发禁用、黑色16px与长标签完整换行通过。
- 完整 LiveStrategyView 浏览器回归：按实际全局/餐饮 JSON 模拟线上合并条件，刷新后中文回显、右键建议、逐条 CAS 保存、原位新增/修改四层业务、筛选排序搜索与切方案通过；320、390、1024、1600、2000px 均无页面横向溢出。

浏览器验证使用隔离模拟接口，没有写生产事实、调用真实模型/TTS或创建直播。上线只更新普通 web 与桌面前端入口和新哈希资源，继承发布时实际 current 的其余文件；不部署后端、不重启服务、不改配置。

原始证据在本聊天 `work/fact-category-label-fix-proof/`：`runtime-catalog-assembly.txt`、`baseline-source-reproduction.json`、`browser.log`、构建日志及后续发布核验报告。
