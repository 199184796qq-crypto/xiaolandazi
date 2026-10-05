# 债务补排闭环生产验收

- 验收时间：2026-10-02（Asia/Shanghai）
- 生产版本：`20261002-012426-debt-backlog-f08be7f4eddd-dirty`
- Git 基线：`f08be7f4eddd`
- 上一生产版本：`20261001-184202-f08be7f4eddd-dirty`
- 验收结论：技术闭环通过；真实直播间当前伴播运行态均为 `stopped`，未主动启动或注入播报，待下一次正常启动伴播时做无侵入观察。

## 本次闭环范围

1. 问题债务或互动债务达到阈值、互动队列为空且两分钟未完成互动时，重新选择真实未处理事件补入统一决策队列。
2. 同一真实事件在等待、执行或已完成后不会被重复补排。
3. 互动任务只有在插播完成且主线恢复后才完成决策并核销问题债务。
4. 音频安全切点按接收端与 Core PCM 镜像中更慢的有效主线游标计算，避免把缓冲差异误判为可用切点。
5. 最终下发前再次校验切点领先量；超过 35 秒时返回 409 并继续排队，不再形成 `switch_at_ms is too far ahead` 的 502。
6. Management 收到安全切点失效的 409 后释放已领取任务，允许后续重排。
7. 小智网关的 TTS start/stop 按 PCM 输入突发和空闲间隔判定，避免 ffmpeg 缓冲导致复合音频空档测试失效。

## 自动化验证

- Core：`go test -count=1 ./...` 通过。
- Management：`go test -count=1 ./...` 通过。
- Xiaozhi gateway：`go test -count=1 ./...` 通过；复合音频空档用例额外连续运行 5 次通过。
- Customer mobile：`pnpm check`、`pnpm build` 通过。
- Sales mobile：`pnpm check`、`pnpm build` 通过。
- Web console：`pnpm build` 通过；只有既有的动态导入和 chunk 体积提示。
- Windows 环境未执行 Go race detector：当前 CGO 关闭，`-race` 不可用。

## 生产验证

- `current` 已指向 `/opt/xiaolan/releases/20261002-012426-debt-backlog-f08be7f4eddd-dirty`。
- `xiaolan-core.service`、`xiaolan-management.service`、`xiaolan-xiaozhi.service` 均为 `active`。
- Core、Management、Xiaozhi 健康检查均返回 200。
- 桌面端、客户移动端、销售移动端、Management API、Core 代理、Xiaozhi health/OTA、Sales API 全量烟测通过。
- 部署后的 Core 二进制已确认包含 `question_debt_backlog`、切点失效 409 文案及 unsafe switch lead 防线。
- 发布后稳定性观察窗口内没有新的 panic、fatal、5xx、`switch_at_ms is too far ahead` 或 unsafe switch lead 日志。
- Core 与 Management 均明确记录 `semantic embedding disabled`；本次验收没有擅自开启向量服务。

## 生产内存态检查

- 房间 15：伴播 `stopped/live_finished`，问题债务和互动债务均为 1，队列为空。
- 房间 24：采集处于直播状态但伴播 `stopped`，问题债务和互动债务均为 1，队列为空。
- 以上结果符合安全闸门：债务补排只处理 `working` 的付费伴播运行态，停止态不产生任务、不触发语音。

## 下一次正常开播的无侵入观察点

无需人工造弹幕或注入音频。伴播由业务正常启动后，只观察以下日志和状态：

1. 债务达到阈值且队列空闲两分钟后出现 `trigger=question_debt_backlog` 或对应真实互动任务。
2. 任务领取后若切点过期，Management 释放任务且没有 502。
3. 插播完成、主线恢复后，决策离队并记录 ANSWER pin，问题债务进入冷却或被核销。
4. 同一事件不重复补排。

## 回滚

如出现新版本异常，回滚目标为：

`/opt/xiaolan/releases/20261001-184202-f08be7f4eddd-dirty`
