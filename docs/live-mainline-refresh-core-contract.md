# Core 主线成品安全替换接口

这是管理侧内容更新 worker 的内部接口，不对客户端开放。全部请求需要 `X-Core-Token`、`tenant_id`，并固定访问直播间所属 Core 节点；不得把进程内节目基线查询故障转移到另一节点。

## 接口

- `GET /internal/v1/rooms/{roomID}/audio/program`：读取当前实际播出的节目快照。
- `POST .../audio/program/refresh`：验证音频地址、时间轴和安全点后暂存完整的新播放列表。
- `POST .../audio/program/refresh/renew`：重新核验管理侧授权后，只续期，不重新签名、下载或生成音频。
- `POST .../audio/program/refresh/cancel`：撤回待应用请求，不中断当前声音，也不清除已排队声音。

首次 refresh 请求：

```json
{
  "job_id": "content-refresh-job-123",
  "expected_program_id": "core-program-15-...",
  "expected_version_id": 100,
  "expected_generation": 0,
  "generation": 1,
  "new_version_id": 101,
  "new_version_no": 8,
  "valid_until": "2026-10-04T10:00:30Z",
  "tracks": [
    {
      "id": "A",
      "audio_url": "https://example.invalid/immutable-prepared-a.wav",
      "duration_ms": 60000,
      "text": "已准备好的文稿",
      "timeline": [],
      "safe_points": []
    }
  ]
}
```

`tracks` 结构与已有正式节目 start 接口一致，实际生产数据必须提供完整、有效的时间轴与安全点。管理 worker 负责把约 25% 新片段与保留的 75% 原声音拼接并验证完成，再提交全部轨道；Core 不调用模型、不承担片段合成。原轨道 ID 应保持稳定。

renew 只发送 `{job_id, expected_program_id, valid_until}`；cancel 只发送 `{job_id, expected_program_id}`。

## 安全边界与幂等

generation 从 0 开始，每次要求新 generation 等于基线加 1。节目 ID、当前版本 ID、generation 任一不符，或者已有另一任务待应用，返回 409。同一 job 的同一原始载荷可重试；不同载荷不得复用 job ID。续期时间不参与载荷摘要。

接受请求不会重建 program、session、StartedAt，也不会调用开播、计费或停止流程。当前完整音轨播完后，已提前选定的下一条旧音轨仍会播完，之后才使用新列表。暂停或互动插入不会迫使新列表提前应用。因此“每两小时更新”是生成周期，不保证两小时时刻立即切换，实际生效还需等待安全边界。

成功发布新曲后，快照才更新 `generation`、`version_id`、`version_no` 和 `last_applied_job_id`；排队阶段仅显示 `pending_generation`、`pending_job_id`。worker 必须确认 `last_applied_job_id`，不能把 HTTP 200 的排队成功当作播出完成。

请求的有效期必须在未来 60 秒内，建议 30 秒，并每 5 秒由管理侧核验授权、模式、房间状态、源数据及版本基线后调用 renew。到曲目边界若请求过期，Core 撤回新内容并继续旧列表。过期或取消的 job 不能再排入同一节目，应由管理侧终止或重新创建任务。cancel 还能阻止正在探测音频的旧请求稍后重新排入。

Core 不持有管理侧授权数据库。续期核验与实际曲目边界之间仍存在短暂竞态，不能宣称严格实时撤权；短有效期限制该窗口。已经进入应用边界的取消可能返回 409，已经播出的内容不会回滚。

Core 停播会清除待更新；重启后节目进程状态丢失，不自动开播或恢复旧任务。管理 worker 必须重新检查实际 program 和持久版本基线。

## 管理侧 adapter

`management-service/internal/coreclient/program_refresh.go` 提供 `GetRoomProgramSnapshot`、`RefreshRoomProgram`、`RenewRoomProgramRefresh`、`CancelRoomProgramRefresh`。409 对应 `ErrProgramRefreshConflict`，404 对应 `ErrProgramRefreshNotFound`。清理任务遇到 404 可确认房间已不存在；409 必须先核对最新快照，避免把新节目或已经使用的音频误删。
