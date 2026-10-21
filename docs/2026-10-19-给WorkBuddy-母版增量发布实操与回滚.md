# 给 WorkBuddy：本次母版增量发布实操与回滚

本次已经部署完成。北京时间 **2026-10-19 00:08:28—00:08:30**，生产切换为 `20261019-prompt-boundaries-957e99b`。本文件是发布回执与操作交接，不需要再次部署。

## 实际上线内容

输入交接是 `E:\直播伴播\docs\archive\2026-10-21\history\2026-10-19-给Codex-准发布交接-删授权句与宗教规则.md`，基线提交 `957e99b227887ca2129030a4d43373937f549eb4`。从生产现有字节构造增量，完成 6 文件、7 处定点删改：

| 母版，均位于 `configs/prompts/anchor-style/` | 本次变化 |
| --- | --- |
| `text-generation-foundation.md` | 删末尾主观口味授权句及整段“肯定好吃”授权，保留客观事实约束 |
| `text-generation-final-boundary.md` | 删“肯定好吃”授权尾句 |
| `content-result-plan-probe.md` | 删主观判断授权短语，保留规划与事实边界 |
| `content-result-speech-probe.md` | 删主观判断授权短语，保留正文与事实边界 |
| `text-simple-facts-probe-plain-claims.md` | 删“主观推荐可以说肯定好吃” |
| `compliance/audit-four-layer.md` | 删原第 8、9 条国家/宗教规则，保留其它通用与品类规则 |

另更新 `MANIFEST.json` 中这 6 项 SHA256。清单顺序及其它条目原字节保持。169 份现场文件仍完整：138 份正常文件、31 份备份；其余 163 份内容不变，两个生产独有文件及两份独立修改母版全部保留。`compliance/L1/p0-hard-block.md` 本次未修改，它此前的删除结果保持。

当前生产状态：

| 项目 | 实际值 |
| --- | --- |
| `current` | `/opt/xiaolan/releases/20261019-prompt-boundaries-957e99b` |
| 完整前驱/回滚目录 | `/opt/xiaolan/releases/20261018-handoff-errors-9698c4e` |
| Management 程序源码 | `9698c4efcbd17e7545401431291d4480e12e98e7`，本次没有替换二进制 |
| Management PID / SHA256 | `1115478` / `0ff49ea00ed66c0bd1f194b29d0ddf4b62cab0a7b6fd13810687443edd394af1` |
| Core / 小智网关 PID | `775073` / `775930`，均未重启 |
| 普通/桌面前端来源 | 保持 `f6e3e0ca665a67b6c1a077f8f4d4d7d599c83cd3` 的已部署字节 |
| 发布目录文件数 | 3685；仅 7 文件变化，另外 3678 个文件哈希一致 |

**发布名里的 `957e99b` 指母版删改交接来源，不代表正在运行的 Management 二进制已经变成该提交。**

## 为什么可以不重启

现场进程配置已核实：`ANCHOR_STYLE_PROMPT_DIR=/opt/xiaolan/current/configs/prompts/anchor-style`。生成侧 `management-service/internal/interactionpolicy/policy.go` 的 `Read` 和审核侧 `management-service/internal/httpapi/anchor_style_prompt_asset.go` 的加载器会在新请求时 `os.ReadFile`，没有把这几份正文永久缓存进进程。

因此在完整准备好的新目录与旧目录之间原子切换 `current`，后续新请求会读取新母版，不必重启三项服务。已发出的请求保留自己当时的提示词；数据库里的已保存方案、事实和旧话术记录不会自动重写或删除。本次未清理缓存、未代商家重新保存/初始化方案。

**清理旧发布时必须保留进程仍引用的目录。** 当前 Management 的 `/proc/1115478/exe` 仍指向前驱目录的程序；不能因为 `current` 已切换就删掉该前驱。服务未来正常重启后才会通过 `current` 启动同字节的程序，本次没有为此重启。

## 本次实际怎么部署

源码唯一目录仍是 `E:\直播伴播`。本次临时工具、原文、差异和回执统一放在 `E:\直播伴播-local\prompt-permission-release-20261019`，不把旧 C 盘副本作为代码或发布来源。

1. 阅读 `AGENTS.md`、`协作交接.md` 和本次交接文档，核对 `git status`、HEAD，登记文件负责人。保留另一方的 6 份原未跟踪讨论文档，不代提交。此次只领取共享记录、AGENTS 和这份操作文档，没有改 Go/前端源码。
2. 用 SSH 别名 `xiaolan-prod` 抓取实际 `current`、3685 个文件哈希、三服务 PID/程序哈希和 Nginx 文件哈希；读取完整现场母版及实际母版环境路径。仅保存公开路径和指纹，不输出服务凭据。
3. 逐项核对现有 169 项 MANIFEST 与磁盘文件一致。从交接文档提取 old/new，逐处要求 old **精确出现一次**才替换；发生偏差立即停止重新核对，不模糊匹配、不用仓库全文覆盖。生成 6 份改后文件和只更新六项哈希的 MANIFEST，打包为 7 文件增量。
4. 用待发布完整母版目录执行相关现有测试。目录保留 `configs/prompts/anchor-style` 后缀，以满足现有路径契约断言。最终 29 项通过、0 失败、0 跳过。首次使用随意命名的临时目录时，一个风格母版测试因路径后缀断言失败；修正隔离目录结构后通过，没有为此修改业务源码或模板内容。
5. 上传增量及守卫脚本，核对上传前后 SHA256。包哈希为 `f5bbde65f103ce54729b366904d78a3ddb6d24097aeba85d946eaee37109bfef`。
6. 服务器取得共享锁 `/opt/xiaolan/deployments/xiaolan-release.lock`，重新核验生产基线、三服务和 Nginx；只读指纹保护系统设置、模型池及 13 类客户方案资产。三次核验全部会话为 0、3 个 Core 房间空闲；不为发布停止用户直播。
7. 以实际旧发布为基础建立完整新目录，使用硬链接继承原文件；7 个变化文件通过临时文件加 `os.replace` 换入，**不直接写硬链接原文件**，避免同时污染旧发布。新目录的完整哈希必须等于“旧基线 + 7 文件白名单”，MANIFEST 必须包含并匹配全部 169 项。
8. 所有检查通过后原子切换 `current`。不执行 `systemctl restart`。重新检查健康接口、母版实际加载路径、完整文件清单、进程、Nginx、配置和客户资产。切换后检查抛错时，脚本自动切回前驱并核验。
9. 从本机再次独立抓取生产指纹；核验公网、鉴权和完整前端备份，更新 E 盘权威备份指针及旧发现入口。保存回执、补共享交接，再提交本次文档。

本次使用的本机命令如下，**仅为说明已经执行的流程；当前已上线，不要原样重复运行**：

```powershell
Set-Location -LiteralPath 'E:\直播伴播-local\prompt-permission-release-20261019'
python -X utf8 capture_baseline.py
python -X utf8 inspect_production.py
python -X utf8 prepare_patch.py
# 在发布前完成待发布母版的相关测试和差异核对
python -X utf8 upload_and_deploy.py
python -X utf8 verify_production.py

Set-Location -LiteralPath 'E:\直播伴播'
& '.\scripts\test-production.ps1'
```

工具分工：`prepare_patch.py` 严格生成本次 7 文件包；`upload_and_deploy.py` 上传并校验；`deploy_prompt.py` 在服务器持锁、做保护、准备新目录并切换；`verify_production.py` 独立核验并更新备份指针。脚本均保留本次固定来源、前驱和目标目录断言，防止误用于其它批次。

下次发布应新建证据目录，重新抓取现场，调整本次范围、old/new 和发布 ID 后再审核执行；不能沿用本次过期基线，也不能直接运行旧的全量 `prepare_deploy.py`。涉及二进制、前端、运行时不现读的配置时，另按对应构建与发布流程处理，不能照搬“无需重启”的结论。

## 核验结果与证据

- 待发布母版的 29 项相关现有测试通过。日志：`staged-prompt-tests.jsonl`；不是全服务全量回归，也没有重新构建程序和前端。
- 三次空闲守卫、3 个服务健康检查、全目录哈希、169 项母版清单和实际进程加载目录核对通过。
- 7 项公网入口/当前资源字节核对、11 项真实方法未登录 401 检查通过。标准 `scripts/test-production.ps1` 不跳过销售/小智，桌面端、手机客户、销售端、管理 API、Core 代理、小智健康及 OTA 均通过。
- 2815 个完整前端文件与现有完整备份的哈希、大小、权限逐项一致；本次复用相同的完整备份包，不复制旧前端覆盖生产。Nginx、系统设置/模型池、13 类客户资产均一致，三服务 PID/程序哈希均一致。
- E 盘权威指针：`E:\直播伴播-local\frontend-preservation\latest.json`。新核验清单：`E:\直播伴播-local\frontend-preservation\20261019-prompt-boundaries-957e99b\manifest.json`；完整备份仍是其中指向的 `20261018-handoff-errors-9698c4e\deployed-frontends.tar.gz`。
- 本次未开启直播、未发弹幕、未调用付费模型或 TTS。部署核验通过不代表已实播验收，也不能据此声称所有违禁表达或高退回率已经根治。

完整本机证据：`E:\直播伴播-local\prompt-permission-release-20261019`。重点文件是 `production-baseline.json`、`production-prompts.json`、`before/`、`after/`、`semantic-deletions.diff`、`patch.json`、`patch.tar.gz`、`deployment-report.json`、`production-after.json`、`production-verification.json` 和 `standard-production-smoke.log`。

服务器持久证据：`/opt/xiaolan/deployments/20261019-prompt-boundaries-957e99b/`，包含原始基线、补丁清单、实际部署脚本、原 7 文件备份与部署回执。不得把该目录中的凭据或私有配置另行输出；本次保存的回执仅包含指纹与必要路径。

## 回滚方法

本次**未执行实际回滚**。完整前驱目录与原 7 文件压缩备份已保留，部署脚本包含自动恢复及人工回滚分支。确需回滚时先确认用户授权与场次空闲；若已有后续发布、文件更新或进程变化，守卫会拒绝，先重新评估，不能强行绕过。

本次对应的人工回滚命令如下，从本机 PowerShell 执行；脚本会取得相同发布锁、验证当前确是本次版本、前驱哈希和空闲状态，然后原子切回前驱，**不重启服务、不改客户数据**：

```powershell
ssh xiaolan-prod 'sudo -n python3 /opt/xiaolan/deployments/20261019-prompt-boundaries-957e99b/deploy_prompt.py /opt/xiaolan/deployments/20261019-prompt-boundaries-957e99b rollback'
```

成功回执在同一服务器证据目录的 `rollback-report.json`。随后重抓实际基线、执行 `E:\直播伴播\scripts\test-production.ps1`，核对前端完整备份，把 E 盘 `latest.json` 及旧发现入口指向经重新核验的前驱清单，更新 `AGENTS.md`、`协作交接.md`。不要手工只还原某一份母版却留下新 MANIFEST，也不要用原仓库 136 项清单覆盖现场。

本次源码及操作文档编辑结束，已交接给 WorkBuddy 查阅。后续要改相同文件，按共享交接规则重新登记。
