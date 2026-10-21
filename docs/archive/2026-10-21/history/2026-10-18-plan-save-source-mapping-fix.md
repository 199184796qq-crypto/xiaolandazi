> **历史记录 · 2026-10-21 归档**
> 原位置：`docs/2026-10-18-plan-save-source-mapping-fix.md`。
> 原因：实施过程与测试历史，保持原始结论；现行入口链接最终方案。
> 下文保留当时原文；其中“当前”、PID、发布目录及待执行命令均指当时，不能直接用作今天的发布指令。归档不表示遗留问题已解决。
> 当前入口：[现行文档索引](../../../README.md)。

---

# 保存方案来源映射修复结果

日期：2026-10-18。接手基线 `0225813`，交接依据为 `E:\直播伴播\docs\archive\2026-10-21\history\2026-10-18-plan-save-source-mapping-fix-handoff.md`。本次修复源码并完成本地验收，尚未部署或触发生产内容更新。

## 修复后的行为

- 使用已有 `explicitPlanFactScope()` 和归属前缀常量识别人工归属，从实际分类/名称查找当前快照里的真实来源。
- 公共事实关联当前全部商品；商品事实仅关联指定商品记录 ID。明确归属不再进入模型范围重判，也不被相同文本合并扩大范围。
- 来源始终为真实快照编号，例如地址为 `knowledge:66`、1 号链接人均消费为 `knowledge:63`。删除 `facts:line:N` 冒充来源的兜底。
- 未匹配来源、同名歧义、归属商品失效或格式损坏时返回包含行号/标题的错误，不返回可被误存的部分结果；原来源完整性校验保留。
- 无人工归属的旧标题继续使用原来的范围分类流程。商品卡字段、福利、完整事实文本和各商品实际记录 ID 保持原有结构，不写死行业分类。

## 文件范围

| 文件（均位于 E:\直播伴播） | 修改内容 |
| --- | --- |
| `management-service/internal/httpapi/content_pool_sources.go` | 归属元信息解析、真实来源关联、明确归属保留及可定位错误 |
| `management-service/internal/httpapi/content_pool_generation.go` | 接收并传递解析错误，在范围模型调用前停止无效来源输入 |
| `management-service/internal/httpapi/content_pool_test.go` | 现有商品卡测试适配错误返回 |
| `management-service/internal/httpapi/live_plan_workspace_provider_probe_test.go` | 现有只读探针适配错误返回，未执行生产探针 |
| `management-service/internal/httpapi/content_pool_sources_test.go` | 公共/商品/旧格式、带引号分隔符的链接、非法归属、重复来源、改分类名称、人工范围保留与真实九条事实回放 |

WorkBuddy 已完成的保存入口及风险剔除业务代码未改动，其未提交的 `live_derived_safety_test.go` 及原待办/交接文档保留。

## 验收结果

- `go test ./internal/httpapi ./internal/contentpools ./internal/db -count=1 -json`：三个包通过，945 项用例/子用例通过、89 项跳过、零失败。包含 WorkBuddy 当前工作区中的新增风险测试。
- `go build ./management-service/...`：管理服务全部命令编译通过。
- 使用先前只读捕获的 9 条生产模型实际候选和来源快照离线回放：每条来源均与独立排查证据的真实编号一致，公共/商品归属保持；组装后的 `ValidateSet()` 通过。
- 明确归属测试以未配置数据库/模型的 Server 运行分类器，验证人工公共及商品范围不再触发模型重判或被旧缓存覆盖。
- `git diff --check` 通过。

完整回归日志：`E:\直播伴播-local\save-plan-investigation-20261018\source-mapping-regression.jsonl`。

真实资料留在 E 盘本地证据目录，不提交商户资料到 Git。单独复现回放的方法：

```powershell
Set-Location -LiteralPath 'E:\直播伴播\management-service'
$env:CONTENT_FACT_SCOPE_REPLAY_PATH = 'E:\直播伴播-local\save-plan-investigation-20261018\read-only-data.json'
go test ./internal/httpapi -run '^TestContentFactsProductionScopeReplay$' -count=1 -v
```

不设置该变量时，真实资料回放测试自动跳过；其它来源与范围回归正常执行。回放仅读取本地文件，不访问生产数据库或调用模型。

## 发布交接

源码修复与 WorkBuddy 已完成的“保存不等待内容更新”分别验收通过后，应按最新发布流程构建上线。本次没有发布，也没有为验收而写入商户资料、保存新版本或产生付费模型调用；线上点击保存仍需待发布后验证。

提交仅包含本次负责的源码、测试及排查/结果记录；WorkBuddy 的其它工作区内容保留。接手发布前重新核对主线提交、共享交接状态、实际线上版本及备份。
