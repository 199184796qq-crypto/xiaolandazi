# 给 WorkBuddy：代码统一整理与后续协作交接

交接日期：2026-10-18。用户要求 Codex 与 WorkBuddy 整理合并代码，后续统一回到 `E:\直播伴播` 工作。该目录现已恢复并与 GitHub 主线同步，可以从这里继续开发。

## 1. 现在以哪里为准

| 项目 | 当前约定 |
| --- | --- |
| 唯一主工作目录 | `E:\直播伴播` |
| 工作分支 | `main`，跟踪 `origin/main` |
| GitHub 仓库 | `https://github.com/199184796qq-crypto/xiaolandazi.git` |
| 整理完成提交 | `f30cfc76165aadf643fd4d4647dde97a11537540`（`f30cfc7`，已推送） |
| 临时文件、测试证据、构建产物 | `E:\直播伴播-local` |
| 需要优先阅读的协作约定 | `E:\直播伴播\AGENTS.md` 顶部“唯一主工作目录”一节 |

本次整理完成时，主目录工作区干净，`main` 与 `origin/main` 一致。本说明后续单独提交只更新文档；接手时请用下面的命令查看实际最新版本，不要把主线退回到本文记录的提交。

```powershell
Set-Location -LiteralPath 'E:\直播伴播'
git status --short --branch
git log -1 --format="%H %s"
```

`AGENTS.md` 顶部的新约定覆盖下方历史记录中的“在 Codex 副本开发”“不写入 E 盘”等旧约定。

## 2. 这次具体整理了什么

1. 以双方已整合到 GitHub 的 `origin/main` 为恢复基线，提交为 `3bc513a9a7e0c4a01ad603b4fb77e1c901cdcc10`。将主目录切回 `main` 并快进到该基线，再提交本次整理。
2. 恢复原目录中 961 个被标记删除的旧中间态文件。恢复后的基线有 2754 个跟踪文件，其中管理服务 `internal` 下有 1126 个 Go 文件；整理提交新增本交接文档后，跟踪文件为 2755 个。
3. 原目录两处前端修改、旧二进制、设计文档和 WorkBuddy 原交接文档均先备份。根据 WorkBuddy 交接中对旧中间态的确认，没有用这些内容覆盖已整合主线。
4. 原 `refactor/speech-generator` 的 4 个独有提交、`refactor/speech-generator-v2` 和旧 `main` 均保留，并建立归档分支及完整 Git 历史备份。
5. 为其它工作副本保存源码压缩包、未提交补丁和逐文件哈希。旧 `E:\speech-generator-build` 已释放对 `main` 的占用，改为分离 HEAD；其源文件内容保持。
6. 全部 Go 服务构建时发现主线 `audio-service/cmd/audio/main.go` 重复定义了两份完全相同的 `unregisterReceiver`。仅删除一份重复定义，消除编译错误，保留原行为。
7. 将最新完整前端备份与权威指针放到 E 盘，并核验哈希。旧 C 盘备份发现入口也已指向同一份 E 盘备份。

这里的“统一”指将双方已经整合的主线恢复到用户指定目录。其它副本中尚未发布、尚未验证的草稿保留在原处及备份中，没有把所有脏文件直接混进主线。如后续要继续某份草稿，请先与当前 `main` 比较，再从 E 盘主目录实施。

## 3. 旧目录后续用途

下列目录只用于历史或草稿核对，不再作为日常开发、构建发布的来源：

- `E:\speech-generator-build`
- `E:\speech-generator-build-main`
- `C:\Users\19918\Documents\Codex\2026-10-09\zh\work\model-pool-source`
- `C:\Users\19918\Documents\Codex\2026-10-09\zh\work\main-merge-20261014`

前三处原有源文件字节均已与备份逐项核验，保持不变。C 盘整合副本已切到归档分支并同步本次主目录约定。旧目录没有整体删除，也没有执行全盘清理。

## 4. 备份与核验材料

整理备份根目录：`E:\直播伴播-local\code-unification-20261018`。

| 路径（相对此备份根目录） | 内容 |
| --- | --- |
| `original` | 整理前 E 盘主目录的源码快照、清单和补丁 |
| `workbuddy-build`、`workbuddy-main`、`workbuddy-codex`、`integrated` | 各副本源码快照、逐文件哈希及未提交补丁 |
| `original-untracked` | 原主目录额外文件的原件备份，包含旧二进制、设计及交接资料 |
| `original-history.bundle` | 整理前完整 Git 历史 |
| `unified-history.bundle` | 整理完成提交时的完整 Git 历史 |
| `WorkBuddy交接原文.md` | WorkBuddy 提供的原始交接文档 |
| `inventory.json`、`primary-restoration.json` | 整理前盘点与恢复记录 |
| `final-verification.json` | 整理完成时的版本、工作区、构建及浏览器核验结果 |
| `baseline-comparison.json`、`go-test-summary.json` | 既有测试失败与基线比较 |
| `production-unchanged.json` | 本次整理前后线上资源及运行进程比较 |

各源码快照目录中的 `source-before.tar.gz`、`manifest.json`、`tracked.patch`、`staged.patch` 和 `status.json` 用于追溯文件及修改。压缩包成员内容已按清单核验。

已保留的归档分支：`archive/e-before-unification-20261018`、`archive/main-before-unification-20261018`、`archive/refactor-before-unification-20261018`、`archive/refactor-v2-before-unification-20261018`。

完整前端备份的最新入口：`E:\直播伴播-local\frontend-preservation\latest.json`。本次复制核验的压缩包包含 2735 个前端文件，SHA256 为 `b21f498d9131a8b8f664c27236c4e7f302df77c82f1a614619e2b34cc46b0ee1`；以后发布应更新该入口。

## 5. 验证结果及遗留项

| 检查 | 结果 |
| --- | --- |
| `web-console`、`customer-mobile`、`sales-mobile`、`device-simulator` | 四个前端构建均通过 |
| Management、Core、Audio、Supervisor、Xiaozhi Gateway | 全部 Go 服务编译通过 |
| Audio 全量测试 | 删除重复函数后通过 |
| 事实类型组件与完整方案页 | 模拟接口浏览器检查通过，覆盖分类读取/保存/失败重试、建议分类、筛选排序搜索、原位新增修改及四层详情 |
| 页面宽度 | 2000、1600、1024、390、320 五种宽度检查通过 |
| 管理服务全量测试 | 1641 项通过、109 项跳过、30 项失败（含子用例，涉及 28 个顶层测试） |

管理服务失败集中在 `internal/agentgateway` 与 `internal/decisionexecutor`。在迁移前未改动的原主线副本重跑对应包，失败集合完全一致，因此没有因本次整理新增失败。详见 `baseline-comparison.json`；不能据此宣称管理服务全量测试通过。

旧 `web-console/scripts/test-unified-plan-save.mjs` 仍等待当前界面已经移除的“更新受影响内容”按钮，测试超时，记录在 `plan-save-test.log`。这份测试需要另行适配当前界面；本次没有为通过旧测试而修改保存方案业务。

上述页面核验使用模拟接口，没有写生产资料或调用付费模型，不能视为真实商户直播或真实模型的端到端验收。

## 6. 用户所指“保存方案”按钮的前后端位置

以下行号对应整理完成提交 `f30cfc7`，后续可能移动，以函数名为准。这里指页面顶部的“保存方案”按钮。

| 层次 | 源码位置 | 入口 |
| --- | --- | --- |
| 前端按钮 | `E:\直播伴播\web-console\src\views\LiveStrategyView.vue:6506` | `plan-save-button`，点击 `savePlanOneClick()` |
| 一键保存流程 | 同文件 `:4085` | `savePlanOneClick()`，需要时先更新受影响内容，再保存版本 |
| 构造保存参数与处理回执 | 同文件 `:3897`、`:4014` | `saveFullShowVersion()`，调用版本 API |
| 前端 API | `E:\直播伴播\web-console\src\api.ts:1551` | `createLiveAgentPlanVersion()` |
| 后端路由 | `E:\直播伴播\management-service\internal\httpapi\server.go:662` | `POST /api/v1/live-agent-plans/{planID}/versions` |
| 后端保存处理 | `E:\直播伴播\management-service\internal\httpapi\live_agent_plan_versions.go:265` | `liveAgentPlanVersionCreate()` |
| 保存时采集生成方案 | `E:\直播伴播\management-service\internal\httpapi\anchor_generation_scheme.go:48` | `captureRealtimeScheme()` |
| 保存时采集商品上下文 | `E:\直播伴播\management-service\internal\httpapi\anchor_cycle_product_focus.go:694` | `captureRealtimeProductFocus()` |
| 数据库存储 | `E:\直播伴播\management-service\internal\db\live_agent_plan_versions.go:115` | `CreateLiveAgentPlanVersion()` |

本次只整理目录、保留代码、补齐编译及核验，没有修改保存方案的审核、校验、事实整理、生成或调度业务。此前 WorkBuddy 交接中提到的“保存方案去掉审核/校验、直接落库”尚未执行，后续以用户明确任务为准。

## 7. 源码与线上版本要分别确认

本次整理没有部署、重启服务、开播或改写商户资料。`f30cfc7` 是整理完成的源码提交，不代表所有代码已部署到生产。

整理完成时记录的线上状态：

- 发布目录：`/opt/xiaolan/releases/20261018-remove-navigation-tagline-0b90f61`。
- Management：源码提交 `4884904e8b57cff69129a0687eb0efbb543a98b3`，PID `1085092`。
- Core：PID `775073`；Xiaozhi Gateway：PID `775930`。
- 前端资源、服务程序及 Nginx 配置与整理前一致。

这些是交接时的快照，后续操作前需要重新读取实际运行状态。尤其不能拿旧副本后端覆盖线上已经恢复的事实类型保存与建议分类接口。

## 8. 双方接下来怎么协作

1. 开始任务先进入 `E:\直播伴播`，阅读最新 `AGENTS.md`，确认分支、提交、工作区和对方正在修改的文件。
2. 正式修改、测试和提交从主目录开始；临时隔离及产物放 `E:\直播伴播-local`，完成后将经核验的源码合回主线。
3. 遇到未提交内容，先看差异并保留对方工作；不要用 `reset --hard`、`clean -fd` 或整树复制来同步目录。
4. 需要旧草稿时，从归档、快照或补丁里逐项比对后接入，避免旧实现覆盖新的分类、事实、商品池或保存方案逻辑。
5. 提交说明区分“源码已提交”“构建通过”“已部署”和“真实业务验收”；每次发布按最新协作约定执行并更新 E 盘备份指针。

接手的第一步是确认当前 E 盘主线和用户下一项任务，不需要重新执行本次目录恢复。
