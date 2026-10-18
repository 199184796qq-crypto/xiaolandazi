# 给 WorkBuddy：A+C 主线 token 优化已上线，二进制发布实操与回滚

北京时间 **2026-10-19 00:29:34—00:29:37** 已完成发布。当前版本 `20261019-token-ac-565d63f`，只替换 Management 二进制并重启 Management。本文是已完成回执及操作教程，当前不用重复部署。

## 本次发布是什么

用户交接 `docs/2026-10-19-给Codex-主线token优化A+C二进制发布交接.md`，代码改动 `7c4405f6fddf5d4bba58432d2d3bcee88d837d09`。构建固定在包含该改动和交接文档的最新提交 `565d63fe0e16a7a27f27ebf04096a0af66c42c53`，因此二进制的 VCS 版本显示 **565d63f**，不是 7c4405f；二者业务 Go 代码相同。

本次与此前运行的 9698c4e 相比，Management 源码只变了两个文件：

- `management-service/internal/speechbase/render.go`：生成侧 P0 视图去掉范围、条件、正则、排除表述的独立元信息字段；保留适用规则的类别、处理方式、相关表述、规则说明及非空所需依据。规则的适用范围仍由程序过滤，程序 P0 扫描器没有改动。这个函数是共享渲染层，不能把影响范围解释成只有某一条主线调用。
- `management-service/internal/httpapi/text_generation.go`：`renderCycleTextMessages` 把开场及讲解回归母版移到 user 的 `环节指令（仅本段生效，不作为商品事实）` 字段；该渲染层的 system 不随这些环节改变。

**本轮没有修复交接所述 prepend 层 3/4 条消息跳变。** 本次验证的是渲染层两条消息中的 system 稳定，不能据此声称最终发给供应商的所有 system 消息已经稳定，或缓存命中率一定提升。该遗留现象及其根因需后续独立核对，未混入本次发布。

## 当前生产状态

| 项目 | 实际值 |
| --- | --- |
| `current` | `/opt/xiaolan/releases/20261019-token-ac-565d63f` |
| 完整前驱/回滚目录 | `/opt/xiaolan/releases/20261019-prompt-boundaries-957e99b` |
| Management 程序源码 | `565d63fe0e16a7a27f27ebf04096a0af66c42c53`，构建元数据 `vcs.modified=false` |
| Management PID | `1128401` |
| Management SHA256 | `dd495622c0ef4565bb9cc4d450ce9e4ce57134084fea694686492a6443d0057b` |
| Core / 小智网关 PID | `775073` / `775930`，均未重启 |
| 完整发布目录 | 3686 文件；仅替换管理程序，新增 `TOKEN-AC-RELEASE-SOURCE.json`；3684 个旧文件哈希一致 |
| 母版 | 169 份现场母版/备份及 MANIFEST 全部原字节保留，继承00:08刚发布的授权句和审核规则删除结果 |
| 前端 | 2815 文件保持；普通/桌面端仍为已部署 f6e3e0c 字节，手机客户及销售端也原样继承 |

没有替商家保存或初始化方案，没有开播、发弹幕或调用付费模型/TTS；配置、模型池及13类客户方案资产在本次切换前后指纹一致。

## 实际验证到什么程度

1. **固定干净源码的完整 Management 模块测试**：`go test ./... -count=1 -json`，1742 项用例/子用例通过、110 项环境或付费探针跳过、0 失败。没有声称本次重跑了 Core、网关或前端全套测试；它们不在本次发布范围。
2. **Linux 交叉编译**：Go 1.27.1，`linux/amd64`，`CGO_ENABLED=0`，`-trimpath -ldflags '-s -w'`。ELF 格式、完整 VCS 提交、`vcs.modified=false` 和二进制 SHA 均核验。
3. **实际生产母版的隔离输入契约测试**：通过 Go overlay 加载临时验收测试，不修改业务源码或干净构建目录。`ordinary_food` 适用的9条规则均保留，逐条核对类别、处理方式、相关词表、说明与非空依据；`all` 和 `*` 范围也核对。该测试没有调用模型。
4. **P0 体积实测**：相同现场母版、相同 ordinary_food 范围，旧生成视图3150字符/6576 UTF-8字节，新视图2212字符/4954字节，字符减少约29.8%。这是相同输入的离线序列化结果，不能直接当成模型 token 降幅或费用降幅；与 session314 旧现场的3460字符也不是同一母版快照。
5. **环节契约**：开场、普通 body、closing、讲解回归四种上下文的渲染层 system 逐字相同；开场与讲解回归的指令在 user，普通/收口没有误带。没有验证供应商缓存命中率或真实口播质量。
6. **发布保护**：共享发布锁、三次会话/Core声音空闲、完整前驱指纹、三服务程序/Nginx基线通过；切换后169母版清单、13类客户资产、系统设置/模型池及Core/网关保持。
7. **独立线上检查**：重新抓取实际进程/全部3686文件；7项公网入口及当前资源字节、11项真实方法未登录401通过。标准 `scripts/test-production.ps1` 不跳过销售/小智，桌面、手机客户、管理API、Core代理、小智健康及OTA、销售端和销售API均通过。
8. **完整前端保护**：2815文件与原完整备份逐项核对哈希、大小、权限。E盘权威指针已更新到本次清单，完整备份继续复用原 `20261018-handoff-errors-9698c4e/deployed-frontends.tar.gz`，不拿本地旧前端产物覆盖生产。

缓存命中率、开场/主线/讲解回归真实话术、退回率要由用户之后的新场验证；本次没有为发布自动生成付费测试话术。后续统计需按同模型、同环节和输入规模比较 `cached_input_tokens / input_tokens`，不要把首次请求和热缓存混为一组，也不要用字符数代替 token 数。

## WorkBuddy 下次怎样做这种二进制发布

### 1. 先分清发布类型，再固定源码

本次 Go 函数变化必须重新编译并重启 Management；上一次纯 MD 变更可以现读、不重启，两种结论不能互相套用。

从 `E:\直播伴播` 开始，先读 `AGENTS.md`、`协作交接.md`，查看 Git 状态和对方登记。未提交差异不覆盖，修改同文件先交接。固定本次完整提交，而不是从另一个旧工作副本直接拿程序。

本次使用本机隔离构建目录 `E:\直播伴播-local\token-optimization-release-20261019\build-source`：

```powershell
git clone --shared --no-checkout 'E:/直播伴播' 'E:/直播伴播-local/token-optimization-release-20261019/build-source'
git -C 'E:/直播伴播-local/token-optimization-release-20261019/build-source' checkout --detach 565d63f
```

这是从权威 E 盘源码导出的固定构建输入，不作为另一份日常工作区；不要在里面继续修改业务。需要修源码时回到 `E:\直播伴播` 按交接规则处理，再提交、重新固定和验证。

### 2. 测试与 Linux 构建

以下是本次执行过的关键命令，产物均留在外部证据目录：

```powershell
Set-Location -LiteralPath 'E:/直播伴播-local/token-optimization-release-20261019/build-source/management-service'
go test ./... -count=1 -json > 'E:/直播伴播-local/token-optimization-release-20261019/management-tests.jsonl'
if ($LASTEXITCODE -ne 0) { throw 'Management tests failed' }

# 在专门的构建进程中设置目标环境，不把 GOOS=linux 留给后面的本机测试进程。
$env:GOOS='linux'
$env:GOARCH='amd64'
$env:CGO_ENABLED='0'
go build -trimpath -ldflags '-s -w' -o 'E:/直播伴播-local/token-optimization-release-20261019/management-service' ./cmd/management
if ($LASTEXITCODE -ne 0) { throw 'Linux build failed' }
go version -m 'E:/直播伴播-local/token-optimization-release-20261019/management-service'
```

不要用 `-AllowDirty` 或跳过失败测试冒充已验证构建。此次只发布管理程序，所以没有运行会打包替换三个前端的整包发布脚本；前端不变由现场完整指纹和备份核验保证。

### 3. 抓生产实际基线，构造二进制白名单增量

本次工具与证据目录：`E:\直播伴播-local\token-optimization-release-20261019`。

- `capture_baseline.py`：读实际 current、全部文件SHA、三服务PID/程序及Nginx指纹。
- `prepare_binary_release.py`：核对干净源码、完整测试日志、构建元数据、前端完整备份和范围；打包 **`bin/management-service` + `TOKEN-AC-RELEASE-SOURCE.json`** 两项，不打包母版或前端。
- `upload_and_deploy.py`：上传并核对全部文件SHA，再调用服务器部署脚本。
- `deploy_release.py`：共享锁、只读数据库保护、三次空闲守卫，以实际 current 建新目录，原子替换管理程序并切换，**只重启 Management**；健康失败时自动切回完整前驱并重启Management。
- `verify_production.py`：重新抓生产独立验收，核对公网/鉴权/前端完整备份并更新指针。

本次包：`20261019-token-ac-565d63f.tar.gz`；SHA256 `49d863cbcf52744a65888e33eccb2fc487b3f06b400ff37e79e752f2dfe549b7`。

下面用于说明**已经执行的步骤，当前不应再次运行部署**：

```powershell
Set-Location -LiteralPath 'E:/直播伴播-local/token-optimization-release-20261019'
python -X utf8 capture_baseline.py
# 完成固定源码测试、Linux构建和生产母版输入契约核验
python -X utf8 prepare_binary_release.py
python -X utf8 upload_and_deploy.py
python -X utf8 verify_production.py

Set-Location -LiteralPath 'E:/直播伴播'
& './scripts/test-production.ps1'
```

工具带本次来源、前驱、目标目录和文件白名单断言。下次换任务要新建证据目录、重新抓现场并调整范围/版本后重新核对，不能复用本次过期基线或强行绕过断言。尤其不要用旧 `prepare_deploy.py` 的母版 diff 断言和仓库136项母版去覆盖现场169项。

### 4. 切换后核对真实运行程序

必须同时核对 `current`、服务 MainPID、`/proc/<PID>/exe` SHA及VCS、健康接口。仅上传成功或 current 名字更新不代表新进程已经启动。保护Core/网关和Nginx，不能顺手重启其它服务。

新目录继承原发布的完整文件，变化文件通过临时文件与 `os.replace` 替换；不能直接写硬链接原文件，否则会污染回滚目录。完整前驱不删除，清理时还要保护其它进程仍引用的发布目录。

## 本次回滚

完整前驱 `20261019-prompt-boundaries-957e99b` 已保留，包含00:08的最新母版；回滚管理程序不应退回更早、会恢复旧授权句的整包。本次没有实际执行人工回滚。

需要回滚时先取得该操作授权并确认空闲。专用脚本验证当前确为本次版本、管理进程与文件未被后续更新、Core/网关与Nginx未变、前驱哈希完整，持同一发布锁并检查三次空闲后切回前驱，**仅重启Management**：

```powershell
ssh xiaolan-prod 'sudo -n python3 /opt/xiaolan/deployments/20261019-token-ac-565d63f/rollback_binary.py /opt/xiaolan/deployments/20261019-token-ac-565d63f'
```

该脚本如发现已有新发布或进程变化会拒绝，先重新评估，不能盲目改断言。旧通用 `scripts/server/rollback-release.sh` 会重启Core和网关，不适合直接用于本次“仅管理程序回滚”。

成功后看服务器同目录 `rollback-report.json`，重抓生产实际基线，跑标准冒烟并核对前端完整备份，再更新 E 盘 `frontend-preservation/latest.json`、旧发现入口、AGENTS 和协作交接。不能只改 current 却不确认运行中的程序。

## 证据与交接

本机：`E:\直播伴播-local\token-optimization-release-20261019`，重点文件 `build-source.json`、`management-tests.jsonl`、`linux-build-metadata.txt`、`token-contract-test.jsonl`、`token-contract-results.json`、`package.json`、`production-baseline.json`、`deployment-report.json`、`production-after.json`、`production-verification.json`、`standard-production-smoke.log`。

服务器原始回执：`/opt/xiaolan/deployments/20261019-token-ac-565d63f.json`；持久工具与核验附件：`/opt/xiaolan/deployments/20261019-token-ac-565d63f/`。

权威前端指针：`E:\直播伴播-local\frontend-preservation\latest.json`；本次核验清单：`E:\直播伴播-local\frontend-preservation\20261019-token-ac-565d63f\manifest.json`。

本轮文档编辑结束，已交接给 WorkBuddy 查阅。遗留 prepend 问题保持独立，不将文档交接当成已实施修复；后续同文件工作先登记。
