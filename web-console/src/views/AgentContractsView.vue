<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { confirmAction } from '../uiFeedback'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  createAdminAgentContract,
  deleteAdminAgentContractAttachment,
  getAdminAgentContracts,
  getAdminAgentLevels,
  getAdminAgents,
  transitionAdminAgentContract,
  uploadAdminAgentContractAttachments,
  updateAdminAgentContract,
} from '../api'
import DataListControls from '../components/DataListControls.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import { session } from '../session'
import type { AgentContract, AgentLevel, AgentSummary } from '../types'

const loading = ref(false)
const saving = ref(false)
const transitioning = ref<number | null>(null)
const error = useFeedbackErrorRef()
const contracts = ref<AgentContract[]>([])
const levels = ref<AgentLevel[]>([])
const agents = ref<AgentSummary[]>([])

const viewMode = ref<'card' | 'table'>('table')
const search = ref('')
const statusFilter = ref('all')
const sortMode = ref('created-desc')
const page = ref(1)
const pageSize = ref(12)

const editorOpen = ref(false)
const editing = ref<AgentContract | null>(null)
const snapshot = ref<AgentContract | null>(null)
const pendingFiles = ref<File[]>([])
const dragActive = ref(false)

const form = reactive({
  external_contract_no: '',
  agent_tenant_id: 0,
  parent_contract_id: 0,
  contract_type: 'cooperation',
  level_id: 0,
  starts_on: '',
  ends_on: '',
  contract_amount_yuan: '',
  note: '',
})

const canManage = computed(() => {
  const access = session.bootstrap?.staff_access
  return Boolean(
    session.bootstrap?.actor.role === 'platform_admin' ||
      access?.is_super_admin ||
      access?.permissions.includes('agent.contract.manage'),
  )
})

const statusOptions = [
  { label: '全部状态', value: 'all' },
  { label: '草稿', value: 'draft' },
  { label: '待签署', value: 'pending_signature' },
  { label: '已签署', value: 'signed' },
  { label: '生效中', value: 'active' },
  { label: '已到期', value: 'expired' },
  { label: '已终止', value: 'terminated' },
  { label: '已作废', value: 'void' },
]
const sortOptions = [
  { label: '最新创建', value: 'created-desc' },
  { label: '生效日期从近到远', value: 'starts-desc' },
  { label: '合同金额从高到低', value: 'amount-desc' },
  { label: '代理名称 A-Z', value: 'agent-asc' },
]

const filteredContracts = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  const items = contracts.value.filter((item) => {
    const matchesKeyword =
      !keyword ||
      [item.contract_no, item.external_contract_no, item.agent_name, item.level_name, item.note]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(keyword))
    const matchesStatus =
      statusFilter.value === 'all' || item.status === statusFilter.value
    return matchesKeyword && matchesStatus
  })

  return [...items].sort((a, b) => {
    if (sortMode.value === 'amount-desc') {
      return b.contract_amount_cents - a.contract_amount_cents
    }
    if (sortMode.value === 'agent-asc') {
      return a.agent_name.localeCompare(b.agent_name, 'zh-CN')
    }
    if (sortMode.value === 'starts-desc') {
      return new Date(b.starts_on).getTime() - new Date(a.starts_on).getTime()
    }
    return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
  })
})

const totalPages = computed(() =>
  Math.max(1, Math.ceil(filteredContracts.value.length / pageSize.value)),
)
const pagedContracts = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredContracts.value.slice(start, start + pageSize.value)
})

watch([search, statusFilter, sortMode, pageSize], () => {
  page.value = 1
})
watch(totalPages, (value) => {
  if (page.value > value) page.value = value
})

function money(cents: number) {
  return '¥' + (cents / 100).toLocaleString('zh-CN', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
}

function dateOnly(value?: string) {
  if (!value) return '长期'
  return new Date(value).toLocaleDateString('zh-CN')
}

function statusLabel(value: string) {
  return (
    {
      draft: '草稿',
      pending_signature: '待签署',
      signed: '已签署',
      active: '生效中',
      expired: '已到期',
      terminated: '已终止',
      void: '已作废',
    }[value] || value
  )
}

function typeLabel(value: string) {
  return (
    {
      cooperation: '合作合同',
      renewal: '续签合同',
      supplement: '补充协议',
    }[value] || value
  )
}

function resetForm() {
  editing.value = null
  Object.assign(form, {
    external_contract_no: '',
    agent_tenant_id: agents.value[0]?.organization_id || 0,
    parent_contract_id: 0,
    contract_type: 'cooperation',
    level_id: levels.value.find((item) => item.status === 'active')?.id || 0,
    starts_on: new Date().toISOString().slice(0, 10),
    ends_on: '',
    contract_amount_yuan: '',
    note: '',
  })
}

function openCreate() {
  resetForm()
  editorOpen.value = true
}

function openEdit(item: AgentContract) {
  if (!['draft', 'pending_signature'].includes(item.status)) return
  editing.value = item
  Object.assign(form, {
    external_contract_no: item.external_contract_no || '',
    agent_tenant_id: item.agent_tenant_id,
    parent_contract_id: item.parent_contract_id || 0,
    contract_type: item.contract_type,
    level_id: item.level_id || 0,
    starts_on: item.starts_on.slice(0, 10),
    ends_on: item.ends_on ? item.ends_on.slice(0, 10) : '',
    contract_amount_yuan: String(item.contract_amount_cents / 100),
    note: item.note,
  })
  editorOpen.value = true
}

function addPendingFiles(files: FileList | File[]) {
  const next = Array.from(files).filter((file) => /^image\/(jpeg|png|webp)$/.test(file.type))
  const seen = new Set(pendingFiles.value.map((file) => file.name + ':' + file.size + ':' + file.lastModified))
  for (const file of next) {
    const key = file.name + ':' + file.size + ':' + file.lastModified
    if (!seen.has(key) && pendingFiles.value.length < 30) {
      pendingFiles.value.push(file)
      seen.add(key)
    }
  }
}

function onAttachmentInput(event: Event) {
  const input = event.target as HTMLInputElement
  if (input.files) addPendingFiles(input.files)
  input.value = ''
}

function onAttachmentDrop(event: DragEvent) {
  dragActive.value = false
  if (event.dataTransfer?.files) addPendingFiles(event.dataTransfer.files)
}

function removePendingFile(index: number) {
  pendingFiles.value.splice(index, 1)
}

async function removeStoredAttachment(item: AgentContract, attachmentId: number) {
  if (!canManage.value || !['draft', 'pending_signature'].includes(item.status)) return
  error.value = ''
  try {
    await deleteAdminAgentContractAttachment(item.id, attachmentId)
    await load()
    if (editing.value?.id === item.id) {
      editing.value = contracts.value.find((contract) => contract.id === item.id) || null
    }
  } catch (value) {
    error.value = value instanceof Error ? value.message : '删除合同扫描件失败'
  }
}
function payload() {
  const amount = Number(form.contract_amount_yuan)
  return {
    external_contract_no: form.external_contract_no.trim(),
    agent_tenant_id: form.agent_tenant_id,
    parent_contract_id: form.parent_contract_id || undefined,
    contract_type: form.contract_type,
    level_id: form.level_id || undefined,
    starts_on: form.starts_on,
    ends_on: form.ends_on || undefined,
    contract_amount_cents: Number.isFinite(amount)
      ? Math.max(0, Math.round(amount * 100))
      : 0,
    note: form.note.trim(),
  }
}

async function saveContract() {
  saving.value = true
  error.value = ''
  try {
    const saved = editing.value
      ? await updateAdminAgentContract(editing.value.id, payload())
      : await createAdminAgentContract(payload())
    if (pendingFiles.value.length) {
      await uploadAdminAgentContractAttachments(saved.id, pendingFiles.value)
    }
    pendingFiles.value = []
    editorOpen.value = false
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存代理合同失败'
  } finally {
    saving.value = false
  }
}

function transitionOptions(item: AgentContract) {
  if (item.status === 'draft') {
    return [
      { value: 'pending_signature', label: '提交签署' },
      { value: 'void', label: '作废' },
    ]
  }
  if (item.status === 'pending_signature') {
    return [
      { value: 'draft', label: '退回草稿' },
      { value: 'signed', label: '确认签署' },
      { value: 'void', label: '作废' },
    ]
  }
  if (item.status === 'signed') {
    return [
      { value: 'active', label: '正式生效' },
      { value: 'terminated', label: '终止' },
    ]
  }
  if (item.status === 'active') {
    return [
      { value: 'terminated', label: '终止' },
    ]
  }
  return []
}

async function transition(item: AgentContract, target: string, label: string) {
  const confirmText =
    target === 'signed'
      ? '确认合同“' + item.contract_no + '”已经签署？签署后将固定当前等级政策快照，合同正文信息不可再修改。'
      : '确认将合同“' + item.contract_no + '”变更为“' + label + '”？'
  if (!(await confirmAction({
    title: target === 'signed' ? '确认合同签署' : '确认合同状态变更',
    message: confirmText,
    confirmText: target === 'signed' ? '确认已签署' : '确认变更',
    danger: target === 'terminated' || target === 'void',
  }))) return

  transitioning.value = item.id
  error.value = ''
  try {
    await transitionAdminAgentContract(item.id, target)
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '更新合同状态失败'
  } finally {
    transitioning.value = null
  }
}

function prettySnapshot(item: AgentContract) {
  try {
    const parsed = JSON.parse(item.level_snapshot_json || '{}')
    return Object.keys(parsed).length
      ? JSON.stringify(parsed, null, 2)
      : '合同尚未签署，当前没有固定的等级政策快照。'
  } catch {
    return item.level_snapshot_json || '—'
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [contractData, levelData, agentData] = await Promise.all([
      getAdminAgentContracts(),
      getAdminAgentLevels(),
      getAdminAgents(),
    ])
    contracts.value = contractData.items
    levels.value = levelData.levels
    agents.value = agentData.items
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取代理合同失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="management-page agent-contracts-page">
    <ModulePageNav context="agents" active-title="代理合同" />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">AGENT CONTRACTS</p>
        <h2>代理合同</h2>
        <p>合同必须绑定真实代理。签署时固定代理等级政策快照；已签合同不可删除或覆盖，只能续签、补充、到期或终止。</p>
      </div>
      <button v-if="canManage" class="primary-button" type="button" @click="openCreate">
        ＋ 新建合同
      </button>
    </section>

    <p v-if="error" class="inline-error">{{ error }}</p>

    <section class="settings-card feature-record-panel">
      <DataListControls
        v-model:view-mode="viewMode"
        v-model:search="search"
        v-model:status="statusFilter"
        v-model:sort="sortMode"
        v-model:page-size="pageSize"
        search-placeholder="合同编号 / 代理 / 等级 / 备注"
        :status-options="statusOptions"
        :sort-options="sortOptions"
      />

      <div v-if="loading" class="panel-loading">正在读取代理合同...</div>

      <div v-else-if="viewMode === 'table'" class="data-table-wrap">
        <table class="data-table agent-contract-table">
          <thead>
            <tr>
              <th>合同</th>
              <th>代理</th>
              <th>类型</th>
              <th>等级快照</th>
              <th>周期</th>
              <th>金额</th>
              <th>状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pagedContracts" :key="item.id">
              <td><strong>{{ item.contract_no }}</strong><small>{{ item.external_contract_no ? '纸质：' + item.external_contract_no : (item.note || '—') }}</small><small>{{ item.attachments?.length || 0 }} 页扫描件</small></td>
              <td>{{ item.agent_name }}</td>
              <td>{{ typeLabel(item.contract_type) }}</td>
              <td>
                <button class="text-action" type="button" @click="snapshot = item">
                  {{ item.level_name || '查看快照' }}
                </button>
              </td>
              <td><span>{{ dateOnly(item.starts_on) }}</span><small>至 {{ dateOnly(item.ends_on) }}</small></td>
              <td>{{ money(item.contract_amount_cents) }}</td>
              <td><span class="status-pill">{{ statusLabel(item.status) }}</span></td>
              <td>
                <div class="table-actions contract-actions">
                  <button
                    v-if="canManage && ['draft', 'pending_signature'].includes(item.status)"
                    class="text-action"
                    type="button"
                    @click="openEdit(item)"
                  >
                    编辑
                  </button>
                  <button
                    v-for="action in canManage ? transitionOptions(item) : []"
                    :key="action.value"
                    class="text-action"
                    type="button"
                    :disabled="transitioning === item.id"
                    @click="transition(item, action.value, action.label)"
                  >
                    {{ action.label }}
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="filteredContracts.length === 0" class="empty-state">暂无符合条件的代理合同。</div>
      </div>

      <div v-else class="feature-record-grid">
        <article v-for="item in pagedContracts" :key="item.id" class="feature-record-card agent-contract-card">
          <header>
            <div><span>{{ item.contract_no }}</span><h3>{{ item.agent_name }}</h3><small>{{ item.attachments?.length || 0 }} 页扫描件</small></div>
            <span class="status-pill">{{ statusLabel(item.status) }}</span>
          </header>
          <dl>
            <div><dt>合同类型</dt><dd>{{ typeLabel(item.contract_type) }}</dd></div>
            <div><dt>代理等级</dt><dd>{{ item.level_name || '未指定' }}</dd></div>
            <div><dt>生效日期</dt><dd>{{ dateOnly(item.starts_on) }}</dd></div>
            <div><dt>到期日期</dt><dd>{{ dateOnly(item.ends_on) }}</dd></div>
            <div><dt>合同金额</dt><dd>{{ money(item.contract_amount_cents) }}</dd></div>
            <div><dt>规则快照</dt><dd><button class="text-action" type="button" @click="snapshot = item">查看</button></dd></div>
          </dl>
          <footer>
            <button
              v-if="canManage && ['draft', 'pending_signature'].includes(item.status)"
              class="text-action"
              type="button"
              @click="openEdit(item)"
            >
              编辑
            </button>
            <button
              v-for="action in canManage ? transitionOptions(item) : []"
              :key="action.value"
              class="text-action"
              type="button"
              @click="transition(item, action.value, action.label)"
            >
              {{ action.label }}
            </button>
          </footer>
        </article>
      </div>

      <PaginationBar
        v-model:page="page"
        :total-pages="totalPages"
        :total="filteredContracts.length"
        :page-size="pageSize"
      />
    </section>

    <div v-if="editorOpen" class="feature-editor-backdrop" @click.self="editorOpen = false">
      <section class="feature-editor-panel agent-contract-editor">
        <header>
          <div>
            <span class="section-kicker">{{ editing ? 'EDIT DRAFT' : 'CREATE CONTRACT' }}</span>
            <h3>{{ editing ? '编辑合同草稿' : '新建代理合同' }}</h3>
          </div>
          <button class="icon-button" type="button" @click="editorOpen = false">×</button>
        </header>

        <div class="feature-editor-grid">
          <label>
            <span>系统合同编号</span>
            <input :value="editing?.contract_no || '保存草稿后由系统自动生成'" type="text" disabled />
          </label>
          <label>
            <span>纸质合同编号（可选）</span>
            <input v-model="form.external_contract_no" type="text" maxlength="96" placeholder="填写纸质合同或外部合同上的编号" />
          </label>
          <label>
            <span>代理</span>
            <select v-model.number="form.agent_tenant_id">
              <option v-for="agent in agents" :key="agent.organization_id" :value="agent.organization_id">
                {{ agent.name }}（{{ agent.code }}）
              </option>
            </select>
          </label>
          <label>
            <span>合同类型</span>
            <select v-model="form.contract_type">
              <option value="cooperation">合作合同</option>
              <option value="renewal">续签合同</option>
              <option value="supplement">补充协议</option>
            </select>
          </label>
          <label>
            <span>适用代理等级</span>
            <select v-model.number="form.level_id">
              <option :value="0">不指定</option>
              <option v-for="level in levels.filter((item) => item.status === 'active')" :key="level.id" :value="level.id">
                {{ level.name }}
              </option>
            </select>
          </label>
          <label v-if="form.contract_type === 'supplement'">
            <span>主合同</span>
            <select v-model.number="form.parent_contract_id">
              <option :value="0">请选择</option>
              <option
                v-for="contract in contracts.filter((item) => item.agent_tenant_id === form.agent_tenant_id && item.contract_type !== 'supplement')"
                :key="contract.id"
                :value="contract.id"
              >
                {{ contract.contract_no }}
              </option>
            </select>
          </label>
          <label><span>生效日期</span><input v-model="form.starts_on" type="date" /></label>
          <label><span>到期日期</span><input v-model="form.ends_on" type="date" /></label>
          <label><span>合同金额（元）</span><input v-model="form.contract_amount_yuan" type="number" min="0" step="0.01" /></label>
          <label class="feature-editor-wide"><span>合同备注</span><textarea v-model="form.note" rows="4"></textarea></label>
          <div
            class="feature-editor-wide contract-upload-zone"
            :class="{ active: dragActive }"
            @dragenter.prevent="dragActive = true"
            @dragover.prevent="dragActive = true"
            @dragleave.prevent="dragActive = false"
            @drop.prevent="onAttachmentDrop"
          >
            <div>
              <strong>合同扫描件 / 复印页</strong>
              <span>支持 JPG、PNG、WebP；可一次选择或直接拖拽多张图片，单张不超过 10MB。</span>
            </div>
            <label class="ghost-button contract-file-picker">
              选择图片
              <input type="file" accept="image/jpeg,image/png,image/webp" multiple @change="onAttachmentInput" />
            </label>
          </div>
          <div v-if="editing?.attachments?.length || pendingFiles.length" class="feature-editor-wide contract-attachment-grid">
            <article v-for="attachment in editing?.attachments || []" :key="'stored-' + attachment.id" class="contract-attachment-card">
              <a :href="attachment.file_url" target="_blank" rel="noreferrer">
                <img :src="attachment.file_url" :alt="attachment.file_name" />
              </a>
              <div><strong>第 {{ attachment.page_order }} 页</strong><span>{{ attachment.file_name }}</span></div>
              <button type="button" class="danger-link" @click="removeStoredAttachment(editing!, attachment.id)">删除</button>
            </article>
            <article v-for="(file, index) in pendingFiles" :key="'pending-' + file.name + index" class="contract-attachment-card pending">
              <div class="contract-pending-preview">待上传</div>
              <div><strong>新增第 {{ (editing?.attachments?.length || 0) + index + 1 }} 页</strong><span>{{ file.name }}</span></div>
              <button type="button" class="danger-link" @click="removePendingFile(index)">移除</button>
            </article>
          </div>
        </div>

        <p class="commercial-editor-note">合同签署后正文信息锁定。后续变化请新建续签合同或补充协议，历史合同不覆盖、不删除。</p>
        <p v-if="error" class="inline-error">{{ error }}</p>

        <footer>
          <button class="ghost-button" type="button" @click="editorOpen = false">取消</button>
          <button class="primary-button" type="button" :disabled="saving" @click="saveContract">
            {{ saving ? '保存中...' : '保存合同草稿' }}
          </button>
        </footer>
      </section>
    </div>

    <div v-if="snapshot" class="feature-editor-backdrop" @click.self="snapshot = null">
      <section class="feature-editor-panel agent-contract-snapshot">
        <header>
          <div>
            <span class="section-kicker">POLICY SNAPSHOT</span>
            <h3>{{ snapshot.contract_no }} · 等级政策快照</h3>
          </div>
          <button class="icon-button" type="button" @click="snapshot = null">×</button>
        </header>
        <pre>{{ prettySnapshot(snapshot) }}</pre>
        <footer><button class="primary-button" type="button" @click="snapshot = null">关闭</button></footer>
      </section>
    </div>
  </div>
</template>
