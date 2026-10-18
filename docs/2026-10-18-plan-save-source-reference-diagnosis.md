# 保存方案“事实引用了快照之外的资料”：线上核验与来源映射根因

2026-10-18，Codex 根据用户截图及 WorkBuddy 的 `E:\直播伴播\docs\保存方案去阻塞-排查结论与待办.md` 只读排查。未改业务源码、生产数据或服务，未发起新模型调用。

## 结论

这次点击保存被前置的内容池更新任务拦住，尚未进入保存版本接口。内容更新失败的根因是：事实增加人工归属标签后，旧来源解析器没有适配新格式，把真实事实错误地转换成快照中不存在的临时来源编号。

这不是用户缺少事实资料，也不是发现了其它方案资料；本次九条事实的来源编号均因上述格式差异失配。不要靠删除事实或取消来源完整性校验解决。

## 实际请求和任务

对象：巴国布衣本地生活，`tenant_id=14`、`plan_id=5`、`room_id=29`。

- 北京时间 **14:38:28**，线上收到 `POST /api/v1/live-agent-plans/5/content-pools/update?room_id=29&tenant_id=14`，返回 HTTP 202，创建更新任务 **51**。
- 北京时间 **14:38:56**，任务 51 以“事实引用了快照之外的资料”失败，`result_set_id=0`。
- 这次操作之后没有对应的 `POST /api/v1/live-agent-plans/5/versions` 请求，保存前的等待逻辑已经抛错。
- 数据库时间以 UTC 记录，上述任务原始时间为 `06:38:28.284` 至 `06:38:56.646`，换算北京时间为上述时间。
- 任务期间记录到 38 次模型调用，18 次资料适用范围核对、20 次内容核对，记录状态全部为 `completed`。报错发生在随后的程序引用完整性核验。
- 之前已保存的最新内容池 **34**，其来源引用检查没有失配；本次失败未覆盖它。

## 来源映射为什么失配

生产模型记录 `23444` 等显示，实际输入已经采用带归属的事实行，例如：

```text
- [用户指定公共；location/地址信息] 顺庆区滨江中路四段368号明宇帝壹家1幢1号；……
```

快照中真实来源为 `knowledge:66`，其分类和名称组成的查找键为：

```text
location/地址信息
```

但是 `contentFactsFromSafety()` 取完整方括号内容作为键查找，拿到的是：

```text
用户指定公共；location/地址信息
```

键不相等，就走 `facts:line:<行号>` 兜底，并把这个临时事实编号同时当作来源编号。门店地址因此变成 `facts:line:10`；该编号并未存在于 `set.Sources.Rows`，随后 `ValidateSet()` 必然拦截。

同样的问题也影响商品归属，例如：

| 实际事实标题 | 应关联的真实来源 | 错误兜底来源 |
| --- | --- | --- |
| `用户指定公共；location/地址信息` | `knowledge:66` | `facts:line:10` |
| `用户指定商品="1号链接"；per_capita/10人餐人均消费` | `knowledge:63` | `facts:line:14` |

本次模型请求中的 9 个不同事实候选均含此类归属前缀，与当前快照直接查找均失败；去除程序归属前缀后，9 条均可以找到对应的真实来源。复核结果保存在下述 `source-mapping-proof.json`。

## 对应源码（0225813 版本，只读定位）

- `E:\直播伴播\management-service\internal\httpapi\live_agent_plan_fact_scope.go`：`renderFactScopePrefix()` 负责写入人工公共/商品归属前缀。
- `E:\直播伴播\management-service\internal\httpapi\content_pool_sources.go`：`contentFactsFromSafety()` 中的 `knowledge[header]` 及 `facts:line` 兜底，没有识别上述前缀。
- `E:\直播伴播\management-service\internal\httpapi\content_pool_generation.go`：`buildContentPools()` 组装事实及任务，结束时调用 `ValidateSet()`。
- `E:\直播伴播\management-service\internal\contentpools\pools.go`：`ValidateSet()` 检查事实的每个来源是否存在于快照，返回截图中的错误。
- `E:\直播伴播\web-console\src\views\LiveStrategyView.vue`：线上旧版 `savePlanOneClick()` 仍自动更新并等待内容池，因此更新失败会中止保存。

## 本地改动与线上版本

本地 `def8ea1` 已由 WorkBuddy 删除 `savePlanOneClick()` 的自动内容更新和等待，属于尚未发布的本地改动。线上发布目录仍为 `20261018-remove-navigation-tagline-0b90f61`，Management 实际运行提交为 `4884904e8b57cff69129a0687eb0efbb543a98b3`，PID `1085092`。

需要补充 WorkBuddy 原排查结论：`latestSavedContentPoolSet()` 在生产提交 `4884904` 中已经存在；它本身不是本次本地修复才加入的功能。当前真正阻塞用户的是线上前端前置更新，以及内容池来源映射缺陷。

直接发布本地前端的去阻塞修改可以移除这个前置更新，但不会修复单独更新内容池时的来源失配。因此应分别处理，不能把“保存入口不再等待更新”等同于“内容池更新已修好”。

## 建议后续处理与交接

1. 与 WorkBuddy 交接后，修复 `contentFactsFromSafety()` 的来源映射：识别归属元信息，关联到当前方案真实记录 ID，保留人工公共/商品归属。来源 ID 与事实临时 ID 分开，不能将 `facts:line` 冒充来源。
2. 兼容旧事实标题、新公共标题及新商品标题；遇到无法确定来源的行应给出可定位结果，不能绕开 `ValidateSet()` 或凭文本猜到其它方案。
3. 验证改分类、改名称、同名事实和商品归属时的关联稳定性；覆盖真实输入的九条事实，确保每个来源都属于当前快照。
4. 将 WorkBuddy 已完成的保存入口修改与来源映射修复分别验收，再按发布流程上线。不要重复改写 WorkBuddy 已完成的保存/风险处理代码。

本次是用户要求的原因排查，尚未实施以上业务修复或发布。WorkBuddy 的未提交测试修改和原待办文档保持原样。

## 证据位置

`E:\直播伴播-local\save-plan-investigation-20261018`：

- `read-only-data.json`：指定方案的任务、已保存内容池、来源快照、派生安全资产及这次模型请求记录；只读提取，包含商户资料。
- `source-mapping-proof.json`：9 条实际输入的标题、真实来源与错误兜底编号映射。
- `diagnose.py`、`analyze.py`：只读提取和离线分析脚本；不调用模型、不提交保存请求。
