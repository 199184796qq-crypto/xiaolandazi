# Unified work inbox / classified agent reminders (2026-09-24)

## 终端用户范围调整（2026-09-24）

按最新要求，终端用户（customer）不提供“我的待办”。电脑端撤掉统一入口、角标和智能体分类面板；用户手机端移除对应组件、数据适配和订阅启动。终端不再请求统一待办快照、明细或推送；旧电脑入口返回个人中心，直接调用待办接口按产品角色拒绝，不查询统计。销售和内部各岗位继续保留，代理档案本次不变。

客户的订单、售后、运维申请、资料补充、处理进度及确认动作保留在原业务页面。撤下提醒不删除任务或单据，不改财务、库存、分派或确认规则；内部人员仍能看到客户发起的工作。以下早期“用户手机端与销售手机端均有待办”的记录由本条覆盖。

## 本轮交付与实际验证

电脑端：功能入口右上角红底白字数字角标；左栏“我的待办”；底部智能体小球进入“对话 / 我的待办”，按待审核、待接单、待处理、待补充、待确认及业务部门筛选。用户手机端与销售手机端的智能体页面也已加入同一分类面板，读取同一个后端，不新建平行统计。

已接入收款审核与补件、充值/退款/奖励/时长申请、运维工单、维修退换受理与处理、设备入库与报废处置、物流发货与异常、到期拜访和回访。只读权限不产生可办理待审；共享岗位队列标明“岗位共享待办”。等待他人运输、外修等进度不一概算本人的待办。原统计卡不恢复。

部署验证：本轮 management-service.inbox-next.exe 已通过带备份的本地部署脚本替换管理服务，运行文件与候选文件哈希一致。部署后 5173、5174、5175、8080、8081 均返回200；统一待办快照、明细、SSE接口未登录均返回401；看门狗恢复运行且仅一个实例。

后端单元测试、隔离MySQL真实SQL与事务回归、跨部门导航权限测试、模拟浏览器分类/角标/分页/智能体输入/布局测试和三个前端类型检查、构建通过。浏览器使用模拟API和模拟推送；数据库测试使用自动清理的随机前缀隔离表，未操作真实客户收款、审批、库存或账号。真实员工凭证下的业务办理仍需现场验收。

本轮没有宣称所有业务自动接入：收益结算批次、未完整实现的现金交接/缴存、策略规则审批等仍需各自定义接点。明细当前跳转原业务页面，不自动打开指定单据弹窗或替人审批；手机端未有原生办理表单的业务明确提示电脑端处理。

## Confirmed product contract

Use one actionable inbox for navigation badges and agent classification. Badges contain white numeric text on a solid red round/capsule at the upper-right of a function entry. Zero is hidden. No KPI cards or general overview statistics are restored. Reading does not resolve a task. Counts are server-authorized and represent currently actionable work, not all visible records, historical volumes, or every state whose name contains 'pending'.

The desktop agent has Chat / My tasks panels. The same capability exists in the customer and sales mobile agent pages. Categories: review, accept, process, supplement, confirm. Categories can be filtered by business department. Details are server-paged (12 by default, at most 100). The desktop detail opens the existing business page; this is not a promise to automatically open its modal or perform the operation. Mobile support tasks link to the native support page; operations without a native mobile form explicitly direct the user to the desktop. Existing agent action tools and their confirmations remain separate.

## Implemented source mappings

- Financial receipts: pending or posting_failed, dashboard + receipt approval permission; when distinct-reviewer policy is active, exclude original submitter and latest supplemental submitter.
- Existing financial operation tasks: pending recharge, refund, reward and AI-time grant, each with its specific approval permission and the same configured reviewer policy.
- Receipt supplementation: needs_info belonging to the current customer or currently assigned salesperson; not posted or historical rejected tasks automatically.
- Support acceptance: pending queue for operational roles. Processing: accepted/in_progress assigned to the current operator, or within the existing manager scope. waiting_customer is directed back to the requesting customer/seller; awaiting_confirmation is for their completion confirmation, not for the operator to self-confirm.
- Repair/return: SUBMITTED for acceptance; PROCESSING/REPAIRING for local handling; excludes waiting on return transport/external repair. Requires the existing repair view/manage permissions.
- Inventory: INBOUND_PENDING equipment; SCRAP_PENDING disposal under the existing manage permissions. Available stock counts and arbitrary low-stock warnings are not tasks.
- Logistics: pending/ready_to_ship outbound handling, excluding customer/vendor return legs; exception/returned exception handling. In-transit shipments are not automatically local tasks.
- Sales: open leads with explicit visit/follow-up time reached, counted once even if both are due; latest scheduled customer contact under the current sales assignment. No fabricated SLA or overdue age for workflows without a defined deadline.

The mapping is explicit in internal/db/work_inbox.go. Closed records disappear because their real business states changed. Permission scopes and ownership follow existing server handlers; no permission is granted by a badge or agent prompt. The existing code sometimes models departmental queues rather than individual repair assignees: those groups are labelled shared queues, not individual assignments. Cross-user counts must not be summed as unique organization-wide task totals.

Not all existing/future modules are automatically admitted: settlement batch review/payment, cash custody/deposit (not yet implemented as a complete source workflow), learning-rule approvals and other domain-specific workflows require their own explicit state/permission mapping and regression cases. Neither an unimplemented source nor an unknown deadline is presented as a zero-work assertion. Empty states say 'within the integrated businesses'.

## Efficient and recoverable delivery

- work_inbox_revisions holds six bounded topic revision rows: access, finance, inventory, logistics, sales and support.
- Supported transactional writers bump affected revisions in the same transaction immediately before commit. A rollback rolls back the revision. This is a coalescing durable invalidation marker, not a detailed event replay journal; business ledgers remain the audit source.
- A legacy mutation middleware also invalidates relevant topics after successful HTTP actions. Legacy direct SQL writers not yet converted to transactional invalidation are additionally reconciled by bounded cache expiry. It is not claimed that every background writer already emits an atomic event.
- One worker per management process reads only these six rows every 2 seconds, not all business tables and not once per browser/button. No zero-database-polling claim is made.
- Cached counts are keyed by the exact authorized SQL predicate/metadata, source revision and access revision. Identical warehouse queues share counts; user-dependent predicates retain user-specific keys. Memory and Redis cache lifetime is five minutes; sales due-time scopes additionally change each minute. No business change invalidates unrelated topics.
- Concurrent rebuilds for the same key coalesce within a process; at most four local database count rebuilds run at once. Redis allows shared cache reuse across instances, but cross-instance rebuild locking is not claimed. Redis failure uses bounded timeouts/circuit delay and local caching.
- One inbox SSE connection per active application document serves all badges and the agent. No per-button stream and no reuse of the live-room public-chat channel. Auth and permissions are rechecked before data delivery and periodically when idle. Absolute snapshots replace counts; client generation and snapshot-epoch guards reject previous-user and late snapshot responses.
- Hidden mobile/desktop documents stop the stream; foreground re-syncs once. Fallback retries are infrequent. SSE heartbeats read no business counts. The old desktop receipt/ticket 15-second polling now follows shared inbox updates, with low-frequency failure fallback.
- Missing/invalid counts are shown as unavailable, never fabricated zeros. No amount, receipt, stock balance, role or financial policy is modified by querying reminders.

## Validation and deployment boundary

New unit tests cover same-key coalescing, identical-role cache sharing, self-review cache isolation, revisions, read-only roles, external role exclusion and unavailable-state handling. An explicitly opted-in random-prefixed MySQL fixture exercises actual predicates and paging, transactional invalidation/rollback/replay, financial handback, inventory/repair/logistics and due-lead deduplication. These tests do not operate on real customer/financial tables.

Browser tests use isolated Chrome profiles with all API and inbox EventSource data mocked. They exercise red/white anchored badges, zero/unavailable behavior, one shared stream, five classifications, department filtering, paginated details, agent input routing and both native mobile agent pages. They do not prove real-credential, real-money end-to-end acceptance.

Deployment to the local management service is a separate verified operation. Do not claim it occurred merely because source tests/builds passed. User-facing completion should state the actual latest health/build evidence and the coverage limits above.
