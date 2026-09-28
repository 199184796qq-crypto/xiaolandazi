<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, ref, watch } from 'vue'
import { getAdminAuditLogs } from '../api'
import DataListControls from '../components/DataListControls.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import type { AdminAuditLog } from '../types'

const loading = ref(false)
const error = useFeedbackErrorRef()
const items = ref<AdminAuditLog[]>([])
const search = ref('')
const statusFilter = ref('all')
const actionFilter = ref('all')
const sourceFilter = ref('all')
const objectTypeFilter = ref('all')
const roomIdFilter = ref('')
const tenantIdFilter = ref('')
const fromFilter = ref('')
const toFilter = ref('')
const sortMode = ref('time-desc')
const page = ref(1)
const pageSize = ref(24)
const viewMode = ref<'card' | 'table'>('table')

const statusOptions = [
  { label: '全部结果', value: 'all' },
  { label: '成功', value: 'success' },
  { label: '失败', value: 'failed' },
]

const sortOptions = [
  { label: '最新操作', value: 'time-desc' },
  { label: '最早操作', value: 'time-asc' },
  { label: '经办账号 A-Z', value: 'actor-asc' },
]

const actionOptions = [
  { label: '全部动作', value: 'all' },
  { label: '连接直播间', value: 'room.monitor.connect' },
  { label: '断开直播间', value: 'room.monitor.disconnect' },
  { label: '删除直播间', value: 'room.delete' },
  { label: '启动 Agent', value: 'agent.runtime.start' },
  { label: '暂停 Agent', value: 'agent.runtime.pause' },
  { label: '继续 Agent', value: 'agent.runtime.resume' },
  { label: '停止 Agent', value: 'agent.runtime.stop' },
  { label: '切换 Agent 模式', value: 'agent.runtime.mode_update' },
  { label: '切换直播方案', value: 'agent.runtime.plan_update' },
  { label: 'Agent 自动停止', value: 'agent.runtime.auto_stop' },
  { label: '清理失效直播间运行', value: 'room.runtime.cleanup_missing' },
  { label: 'Core 服务失联', value: 'system.core.offline' },
  { label: 'Core 服务恢复', value: 'system.core.recovered' },
  { label: '基础采集自动恢复', value: 'room.monitor.auto_recovered' },
  { label: '发布直播策略', value: 'strategy.publish' },
  { label: '回滚直播策略', value: 'strategy.rollback' },
  { label: '开始智能体学习', value: 'agent.learning.session.create' },
  { label: '采用智能体学习', value: 'agent.learning.adopt' },
  { label: '停用智能体记忆', value: 'agent.memory.deactivate' },
  { label: '回滚智能体记忆', value: 'agent.memory.rollback' },
  { label: '绑定直播方案', value: 'agent.plan.bind_room' },
  { label: '解绑直播方案', value: 'agent.plan.unbind_room' },
  { label: '绑定播放设备', value: 'device.bind_room' },
  { label: '控制播放设备', value: 'device.control' },
  { label: '开始声音录制', value: 'room.recording.start' },
  { label: '停止声音录制', value: 'room.recording.stop' },
]

const sourceOptions = [
  { label: '全部来源', value: 'all' },
  { label: '终端用户', value: 'customer_web' },
  { label: '内部后台', value: 'internal_web' },
  { label: '代理后台', value: 'agent_web' },
  { label: 'Core / 系统', value: 'core_reconciler' },
  { label: 'Core 状态监控', value: 'management_core_watch' },
]

const objectTypeOptions = [
  { label: '全部对象', value: 'all' },
  { label: '直播间', value: 'room' },
  { label: 'Agent', value: 'agent' },
  { label: '策略版本', value: 'strategy_version' },
  { label: '智能体方案', value: 'agent_plan' },
  { label: '智能体记忆', value: 'agent_memory' },
  { label: '客户', value: 'customer' },
  { label: '设备', value: 'device' },
]

const filteredItems = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  const result = items.value.filter((item) => {
    const matchesKeyword =
      !keyword ||
      [
        item.actor_username,
        item.actor_role,
        item.source,
        item.action,
        item.target_username,
        item.object_type,
        item.object_id,
        item.object_name,
        item.reason,
        item.target_room_id,
        item.target_tenant_id,
        item.path,
        item.client_ip,
        item.result,
      ].some((value) => String(value || '').toLowerCase().includes(keyword))

    const success = item.result.startsWith('http_2') || item.result === 'success'
    const matchesStatus =
      statusFilter.value === 'all' ||
      (statusFilter.value === 'success' && success) ||
      (statusFilter.value === 'failed' && !success)

    return matchesKeyword && matchesStatus
  })

  return [...result].sort((a, b) => {
    if (sortMode.value === 'time-asc') {
      return new Date(a.occurred_at).getTime() - new Date(b.occurred_at).getTime()
    }
    if (sortMode.value === 'actor-asc') {
      return a.actor_username.localeCompare(b.actor_username, 'zh-CN')
    }
    return new Date(b.occurred_at).getTime() - new Date(a.occurred_at).getTime()
  })
})

const totalPages = computed(() =>
  Math.max(1, Math.ceil(filteredItems.value.length / pageSize.value)),
)

const pagedItems = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredItems.value.slice(start, start + pageSize.value)
})

watch([search, statusFilter, sortMode, pageSize], () => {
  page.value = 1
})

function resultLabel(value: string) {
  if (value.startsWith('http_2') || value === 'success') return '成功'
  if (value === 'started') return '处理中'
  return '失败 / ' + value
}

function actionLabel(value: string) {
  const map: Record<string, string> = {
    'room.create': '创建直播间',
    'room.monitor.connect': '连接直播间',
    'room.monitor.disconnect': '断开直播间',
    'room.delete': '删除直播间',
    'room.delete_non_cooperating': '运维删除不合作直播间',
    'room.runtime.cleanup_missing': '清理失效直播间运行',
    'system.core.offline': 'Core 服务失联',
    'system.core.recovered': 'Core 服务恢复',
    'room.monitor.auto_recovered': '基础采集自动恢复',
    'agent.runtime.start': '启动 Agent',
    'agent.runtime.pause': '暂停 Agent',
    'agent.runtime.resume': '继续 Agent',
    'agent.runtime.stop': '停止 Agent',
    'agent.runtime.mode_update': '切换 Agent 模式',
    'agent.runtime.plan_update': '切换直播方案',
    'agent.runtime.auto_stop': 'Agent 自动停止',
    'strategy.publish': '发布直播策略',
    'strategy.rollback': '回滚直播策略',
    'agent.learning.session.create': '开始智能体学习',
    'agent.learning.adopt': '采用智能体学习结果',
    'agent.memory.deactivate': '停用智能体记忆',
    'agent.memory.rollback': '回滚智能体记忆',
    'agent.plan.bind_room': '绑定直播智能体方案',
    'agent.plan.unbind_room': '解绑直播智能体方案',
    'device.bind_room': '绑定播放设备',
    'device.control': '控制播放设备',
    'room.recording.start': '开始声音录制',
    'room.recording.stop': '停止声音录制',
    'audit.list_view': '查询操作审计',
    'staff.group.create': '新增部门',
    'staff.group.update': '修改部门',
    'staff.role.create': '新增角色',
    'staff.role.update': '修改角色权限',
    'staff.employee.create': '新增员工',
    'staff.employee.disable': '停用员工',
    'staff.employee.role_update': '调整员工角色',
    'staff.approval_policy.update': '修改审批策略',
    'commercial.membership.create': '新增会员方案',
    'commercial.membership.draft_save': '保存会员草稿',
    'commercial.membership.publish': '发布会员方案',
    'finance.recharge.create': '发起充值',
    'finance.refund.create': '发起退款',
    'finance.reward.grant': '发放奖励',
    'agent.create': '新增代理',
  }
  return map[value] || value
}

function sourceLabel(value?: string) {
  const map: Record<string, string> = {
    customer_web: '终端用户',
    internal_web: '内部后台',
    agent_web: '代理后台',
    core_reconciler: 'Core / 系统',
    management_core_watch: 'Core 状态监控',
    web: '网页',
  }
  return map[value || ''] || value || '未知'
}

function actorLabel(item: AdminAuditLog) {
  if (item.actor_type === 'system' || item.actor_username === 'system') return '系统'
  return item.actor_username || '未知'
}

function objectLabel(item: AdminAuditLog) {
  if (item.object_name) return item.object_name
  if (item.target_username) return item.target_username
  if (item.target_room_id) return '直播间 #' + item.target_room_id
  if (item.object_id) return (item.object_type || '对象') + ' #' + item.object_id
  if (item.target_user_id) return '用户 #' + item.target_user_id
  return '—'
}

function stateLabel(value?: string) {
  if (!value) return ''
  try {
    const parsed = JSON.parse(value) as Record<string, unknown>
    return Object.entries(parsed)
      .map(([key, item]) => key + '=' + String(item))
      .join('，')
  } catch {
    return value
  }
}

function toRFC3339(value: string) {
  if (!value) return undefined
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return undefined
  return date.toISOString()
}

function parsePositiveID(value: string) {
  const parsed = Number.parseInt(value.trim(), 10)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await getAdminAuditLogs({
      search: search.value.trim() || undefined,
      action: actionFilter.value === 'all' ? undefined : actionFilter.value,
      result: statusFilter.value === 'all' ? undefined : statusFilter.value,
      source: sourceFilter.value === 'all' ? undefined : sourceFilter.value,
      object_type: objectTypeFilter.value === 'all' ? undefined : objectTypeFilter.value,
      room_id: parsePositiveID(roomIdFilter.value),
      tenant_id: parsePositiveID(tenantIdFilter.value),
      from: toRFC3339(fromFilter.value),
      to: toRFC3339(toFilter.value),
      page_size: 100,
    })
    items.value = data.items
    page.value = 1
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取审计日志失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="management-page audit-log-page">
    <ModulePageNav context="staff" active-title="操作审计" />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">OPERATION AUDIT</p>
        <h2>操作审计</h2>
        <p>统一查询直播间、Agent、客户、设备和后台业务的人工与系统操作，追溯谁在什么时间做了什么。</p>
      </div>
      <button class="ghost-button" type="button" :disabled="loading" @click="load">
        {{ loading ? '查询中...' : '查询审计' }}
      </button>
    </section>

    <section class="settings-card feature-workspace-panel">
      <DataListControls
        v-model:view-mode="viewMode"
        v-model:search="search"
        v-model:status="statusFilter"
        v-model:sort="sortMode"
        v-model:page-size="pageSize"
        search-placeholder="操作人 / 房间名 / 对象 / 原因 / ID / 路径 / IP"
        :status-options="statusOptions"
        :sort-options="sortOptions"
      />

      <div class="audit-filter-grid">
        <label>
          <span>动作</span>
          <select v-model="actionFilter">
            <option v-for="option in actionOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
          </select>
        </label>
        <label>
          <span>来源</span>
          <select v-model="sourceFilter">
            <option v-for="option in sourceOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
          </select>
        </label>
        <label>
          <span>对象类型</span>
          <select v-model="objectTypeFilter">
            <option v-for="option in objectTypeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
          </select>
        </label>
        <label>
          <span>直播间 ID</span>
          <input v-model="roomIdFilter" inputmode="numeric" placeholder="例如 15" />
        </label>
        <label>
          <span>客户 ID</span>
          <input v-model="tenantIdFilter" inputmode="numeric" placeholder="例如 14" />
        </label>
        <label>
          <span>开始时间</span>
          <input v-model="fromFilter" type="datetime-local" />
        </label>
        <label>
          <span>结束时间</span>
          <input v-model="toFilter" type="datetime-local" />
        </label>
      </div>

      <p v-if="error" class="inline-error">{{ error }}</p>
      <div v-if="loading" class="panel-loading">正在读取审计日志...</div>

      <div v-else class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>时间</th>
              <th>经办人</th>
              <th>操作</th>
              <th>对象</th>
              <th>原因 / 状态变化</th>
              <th>来源</th>
              <th>结果</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pagedItems" :key="item.id">
              <td>{{ new Date(item.occurred_at).toLocaleString('zh-CN') }}</td>
              <td>
                <strong>{{ actorLabel(item) }}</strong>
                <small v-if="item.actor_user_id">用户 #{{ item.actor_user_id }}</small>
                <small v-if="item.actor_role">{{ item.actor_role }}</small>
              </td>
              <td>{{ actionLabel(item.action) }}</td>
              <td>
                <strong>{{ objectLabel(item) }}</strong>
                <small v-if="item.object_type">{{ item.object_type }}<span v-if="item.object_id"> #{{ item.object_id }}</span></small>
                <small v-if="item.target_room_id">直播间 #{{ item.target_room_id }}</small>
                <small v-if="item.target_tenant_id">客户 #{{ item.target_tenant_id }}</small>
              </td>
              <td class="audit-change-cell">
                <span>{{ item.reason || '—' }}</span>
                <small v-if="item.before_state">前：{{ stateLabel(item.before_state) }}</small>
                <small v-if="item.after_state">后：{{ stateLabel(item.after_state) }}</small>
                <small v-if="item.runtime_session_id">Session #{{ item.runtime_session_id }}</small>
              </td>
              <td>
                <span>{{ sourceLabel(item.source) }}</span>
                <small v-if="item.client_ip">IP {{ item.client_ip }}</small>
                <small v-if="item.http_method || item.path">{{ item.http_method || '' }} {{ item.path || '' }}</small>
              </td>
              <td><span class="status-pill">{{ resultLabel(item.result) }}</span></td>
            </tr>
          </tbody>
        </table>
        <div v-if="filteredItems.length === 0" class="empty-state">暂无符合条件的审计记录。</div>
      </div>

      <PaginationBar
        v-model:page="page"
        :total-pages="totalPages"
        :total="filteredItems.length"
        :page-size="pageSize"
      />
    </section>
  </div>
</template>

<style scoped>
.audit-filter-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
  gap: 12px;
  margin: 14px 0 18px;
}

.audit-filter-grid label {
  display: grid;
  gap: 6px;
}

.audit-filter-grid label > span {
  font-size: 14px;
  opacity: 0.72;
}

.audit-filter-grid input,
.audit-filter-grid select {
  min-height: 42px;
  border-radius: 10px;
  border: 1px solid var(--line-color, rgba(148, 163, 184, 0.22));
  background: rgba(15, 23, 42, 0.36);
  color: inherit;
  padding: 0 12px;
}

.audit-change-cell {
  min-width: 220px;
}

.data-table td small {
  display: block;
  margin-top: 4px;
  opacity: 0.66;
}
</style>
