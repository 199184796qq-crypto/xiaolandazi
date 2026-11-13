# 直播改写链路上线步骤

本次交付是相对线上 Management 源码的 16 文件补丁。Git 里的 `main` 缺少线上已有的依赖。待发布包用 `management.patch` 和 `manifest.json` 复建。只更新 `bin/management-service` 和版本元数据，只重启 `xiaolan-management.service`。

北京时间 2026-11-14 00:57:05，生产已切到 `20261114-rewrite-align-74b97b3`。发布依据为 Git 交付提交 `403b013465b0673e28b3807a1347133796dfb219`。

## 1. 核对线上和候选来源

在发布工作站拉取交付分支，并记下实际提交 SHA。

```bash
git clone --branch release/rewrite-design-alignment-20261114 \
  https://github.com/199184796qq-crypto/xiaolandazi.git rewrite-release
git -C rewrite-release rev-parse HEAD
ssh -o BatchMode=yes xiaolan-prod 'cd /opt/xiaolan/current && pwd -P && sha256sum bin/management-service bin/core-service bin/xiaozhi-gateway'
```

本次发布前核对的目录为 `/opt/xiaolan/releases/20261114-restore-835244c`，Management SHA-256 为 `835244cd868cf09c17857de4c81eb8de1a4ecb9a969c3a16bba39a20184e7a2a`。发布当天重新取三服务 PID、运行二进制、完整目录指纹、systemd、Nginx、保护表和业务空闲状态。若当前 Management 指纹已经换版，以最新线上准确源码重新合入和验证。

## 2. 复建 Management 并验证

以下命令在具有 Go 1.27.1、SSH 和仓库访问权限的 Bash 环境执行。

```bash
delivery="$(realpath rewrite-release/docs/releases/2026-11-14-rewrite-design-alignment)"
build_dir="$(mktemp -d)"
scp xiaolan-prod:/opt/xiaolan/deployments/20261113-rewrite-fact-projection-835244c/fixed-runtime-source.tar.gz \
  "$build_dir/predecessor.tar.gz"
echo "396888d1354f6048081f1638e67af14cde0c0919cd172d503e02f3f259022b8c  $build_dir/predecessor.tar.gz" | sha256sum -c -
echo "e96178d340affc266e1a2733db4837402bd072108f3b0d635f6b10e3c5bfd2ae  $delivery/management.patch" | sha256sum -c -
tar -xzf "$build_dir/predecessor.tar.gz" -C "$build_dir"
git -c core.autocrlf=false -C "$build_dir" apply --check --whitespace=error-all "$delivery/management.patch"
git -c core.autocrlf=false -C "$build_dir" apply --whitespace=error-all "$delivery/management.patch"
(
  cd "$build_dir/management-service"
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
    go build -buildvcs=false -trimpath -o "$build_dir/management-service.linux-amd64" ./cmd/management
)
echo "74b97b31d328279e7bb47f66499930cb3b16f7c9dad9571307cf3cc4f811d4f8  $build_dir/management-service.linux-amd64" | sha256sum -c -
```

按 `manifest.json` 核对应用补丁后的 16 个文件摘要。复跑改写、方案保存及场次缓存定向测试。真实 Redis 测试使用隔离实例，生产资料仅复用已批准的本地回放素材。保存准确源码归档、补丁、Git SHA、二进制 SHA-256 和测试结果，作为本次发布证据。

## 3. 创建完整候选目录并激活

沿用上次仅 Management 的发布方式。上次的 `E:/直播伴播-local/rewrite-fact-projection-release-20261113/preflight/capture.py`、`remote_capture.py`、`prepare_release.py`、`deploy.py` 可作为本轮脚本的模板。里面绑定了上次的发布 ID、基线和元数据名，先使用本轮最新指纹及候选包适配和验证。服务器当前的 `/opt/xiaolan/scripts/deploy-release-guarded.sh` 面向整包，调用会重启三个服务。

1. 使用唯一且非空的新发布 ID，例如 `20261114-rewrite-align-74b97b3`。在服务器用 `mktemp -d /tmp/xiaolan-publish.XXXXXXXX` 创建上传暂存目录。上传候选程序、源码归档、版本元数据和发布工具，核对 SHA-256。
2. 发布程序获取 `/opt/xiaolan/deployments/xiaolan-release.lock`。锁内再次核对实际 `current`、三服务 PID 和二进制、配置、保护表与前端指纹。确认所有直播、暂停场次、Core 播放和采集录制均已空闲。无法核实或者遇到并发发布时结束本次激活。
3. 从锁内确认的当前完整运行目录复制到唯一的 `/opt/xiaolan/releases/.staging-<发布ID>.*`。复制继承文件时保留权限和所有者。只在暂存目录覆盖 Management 程序及 `VERSION`、`VERSION.json` 和本次来源索引。比对候选和当前目录文件清单，允许差异仅为这几个路径。保留四端前端、插件、配置、采集器和其他二进制。
4. 将通过校验的候选目录重命名为 `/opt/xiaolan/releases/<发布ID>`。再检查运行仍空闲且 `current` 未移动。用同目录临时符号链接原子切换 `current`，只重启 `xiaolan-management.service`。

北京时间 2026-11-14 00:57:05，生产已切到 `20261114-rewrite-align-74b97b3`。发布依据为 Git 交付提交 `403b013465b0673e28b3807a1347133796dfb219`。全过程保留前一发布目录和事故恢复备份。任何清理只处理本次经过路径校验的暂存文件。

## 4. 发布后检查和回退

- 确认 `current` 指向新目录，Management 新 PID 的 `/proc/<PID>/exe` SHA-256 等于候选。Core 和小智进程 PID 与二进制指纹和切换前相同。验证本机 8080、8081、8083 的 `/healthz` 全部返回 200，核对启动日志，确认主播风格插件加载正常。
- 运行 `scripts/test-production.ps1` 检查桌面、客户移动端、销售端和公开代理。复核前端及运行资源指纹、保护表及 Nginx。可在后续获准的测试场观察 `rewrite facts projected`、`script analysis done` 和 `script chain picked`，验证事实刷新、同场复用及下播清理。
- 如果激活或冒烟失败，在共享发布锁内确认 `current` 仍是本次候选、现场仍空闲，再原子切回刚才记录的完整前驱并只重启 Management。然后复验三服务和公网入口。将候选、错误日志和回退回执保留在本次发布证据目录。

## 生产发布回执

- Management PID 为 `2871811`，运行二进制 SHA-256 为 `74b97b31d328279e7bb47f66499930cb3b16f7c9dad9571307cf3cc4f811d4f8`。启动日志确认加载了 5 个主播风格插件，并选用 Redis 缓存。Core 和小智网关沿用原进程。
- 三项本机健康检查均为 HTTP 200。公网桌面、客户移动端、销售端、公开 API 和鉴权冒烟通过。6,376 个前端文件和 41 张保护表在切换前后核对一致。
- 精确源码归档和发布包存于 `/opt/xiaolan/deployments/20261114-rewrite-align-74b97b3/`。上一完整发布目录为 `/opt/xiaolan/releases/20261114-restore-835244c`。本次验证覆盖上线状态和离线回放，真实直播改写、供应商 token 与成稿仍待实播验证。
