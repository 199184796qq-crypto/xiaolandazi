<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { createAdminAgent, getAdminAgents } from '../api'
import CredentialResultModal from '../components/CredentialResultModal.vue'
import DataListControls from '../components/DataListControls.vue'
import PaginationBar from '../components/PaginationBar.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import RegionSelect from '../components/RegionSelect.vue'
import { session } from '../session'
import type { AgentSummary, InitialCredential } from '../types'

const items = ref<AgentSummary[]>([])
const loading = ref(false)
const error = useFeedbackErrorRef()
const showCreate = ref(false)
const selectedAgent = ref<AgentSummary | null>(null)
const canCreateAgent = computed(
  () => session.bootstrap?.staff_access?.is_super_admin === true,
)

const viewMode = ref<'card' | 'table'>('card')
const search = ref('')
const statusFilter = ref('all')
const sortMode = ref('name-asc')
const page = ref(1)
const pageSize = ref(12)

const statusOptions = computed(() => [
  { label: '全部状态', value: 'all' },
  ...Array.from(new Set(items.value.map((item) => item.status).filter(Boolean))).map((value) => ({
    label: value,
    value,
  })),
])

const sortOptions = [
  { label: '名称 A-Z', value: 'name-asc' },
  { label: '名称 Z-A', value: 'name-desc' },
  { label: '终端数从高到低', value: 'customers-desc' },
  { label: '终端数从低到高', value: 'customers-asc' },
]

function statusLabel(value: string) {
  if (value === 'active') return '正常合作'
  if (value === 'disabled') return '已停用'
  if (value === 'pending') return '待处理'
  return value || '—'
}

function openAgentDetail(item: AgentSummary) {
  selectedAgent.value = item
}

const filteredItems = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  const result = items.value.filter((item) => {
    const matchesKeyword =
      !keyword ||
      [item.name, item.code, item.display_name, item.username, item.phone, item.email]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(keyword))
    const matchesStatus = statusFilter.value === 'all' || item.status === statusFilter.value
    return matchesKeyword && matchesStatus
  })

  return [...result].sort((a, b) => {
    if (sortMode.value === 'name-desc') return b.name.localeCompare(a.name, 'zh-CN')
    if (sortMode.value === 'customers-desc') return b.customer_count - a.customer_count
    if (sortMode.value === 'customers-asc') return a.customer_count - b.customer_count
    return a.name.localeCompare(b.name, 'zh-CN')
  })
})

const totalPages = computed(() => Math.max(1, Math.ceil(filteredItems.value.length / pageSize.value)))
const pagedItems = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredItems.value.slice(start, start + pageSize.value)
})

watch([search, statusFilter, sortMode, pageSize], () => {
  page.value = 1
})
watch(totalPages, (value) => {
  if (page.value > value) page.value = value
})

const code = ref('')
const name = ref('')
const username = ref('')
const displayName = ref('')
const phone = ref('')
const email = ref('')
const province = ref('')
const city = ref('')
const district = ref('')
const deliveryMethod = ref<'copy' | 'email'>('copy')

const credentialOpen = ref(false)
const createdCredential = ref<InitialCredential | null>(null)
const createdDisplayName = ref('')
const createdUsername = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    items.value = (await getAdminAgents()).items
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取代理列表失败'
  } finally {
    loading.value = false
  }
}

function openCreate() {
  error.value = ''
  code.value = ''
  name.value = ''
  username.value = ''
  displayName.value = ''
  phone.value = ''
  email.value = ''
  province.value = ''
  city.value = ''
  district.value = ''
  deliveryMethod.value = 'copy'
  showCreate.value = true
}

async function submitCreate() {
  error.value = ''
  if (
    !province.value.trim() ||
    !city.value.trim() ||
    !district.value.trim()
  ) {
    error.value = '省、市、区/县必须选择'
    return
  }
  if (deliveryMethod.value === 'email' && !email.value.trim()) {
    error.value = '选择邮件发送时必须填写邮箱'
    return
  }

  try {
    const result = await createAdminAgent({
      code: code.value.trim(),
      name: name.value.trim(),
      username: username.value.trim(),
      display_name: displayName.value.trim(),
      phone: phone.value.trim(),
      email: email.value.trim(),
      province: province.value.trim(),
      city: city.value.trim(),
      district: district.value.trim(),
      delivery_method: deliveryMethod.value,
    })

    createdDisplayName.value = result.item.display_name
    createdUsername.value = result.item.username
    createdCredential.value = result.credential
    showCreate.value = false
    credentialOpen.value = true
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '创建代理失败'
  }
}

function closeCredential() {
  credentialOpen.value = false
  createdCredential.value = null
  createdDisplayName.value = ''
  createdUsername.value = ''
}

onMounted(load)
</script>

<template>
  <div class="management-page">
    <ModulePageNav hub="agents" active-title="代理列表" />
    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">CHANNEL PARTNERS</p>
        <h2>代理管理</h2>
        <p>管理代理组织、代理管理员账号及终端规模。</p>
      </div>
      <button v-if="canCreateAgent" class="primary-button" type="button" @click="openCreate">
        新建代理
      </button>
    </section>

    <p v-if="error" class="auth-error">{{ error }}</p>

    <section class="settings-card">
      <div class="settings-card-header">
        <div>
          <span class="section-kicker">AGENTS</span>
          <h3>代理列表</h3>
        </div>
        <span>{{ loading ? '加载中...' : items.length + ' 个代理' }}</span>
      </div>

      <DataListControls
        v-model:view-mode="viewMode"
        v-model:search="search"
        v-model:status="statusFilter"
        v-model:sort="sortMode"
        v-model:page-size="pageSize"
        search-placeholder="代理名称 / 编码 / 管理员 / 手机号"
        :status-options="statusOptions"
        :sort-options="sortOptions"
      />

      <div v-if="viewMode === 'card'" class="agent-list">
        <article v-for="item in pagedItems" :key="item.organization_id" class="agent-row agent-card-clickable" role="button" tabindex="0" @click="openAgentDetail(item)" @keydown.enter="openAgentDetail(item)">
          <div class="agent-main">
            <strong>{{ item.name }}</strong>
            <span>{{ item.code }}</span>
          </div>
          <div>
            <span class="muted-label">管理员</span>
            <strong>{{ item.display_name }} · {{ item.username }}</strong>
            <span>{{ item.phone }}</span>
            <span v-if="item.email">{{ item.email }}</span>
          </div>
          <div>
            <span class="muted-label">终端数</span>
            <strong>{{ item.customer_count }}</strong>
          </div>
          <div>
            <span class="status-pill">{{ statusLabel(item.status) }}</span>
          </div>
        </article>

        <div v-if="!loading && filteredItems.length === 0" class="empty-state">
          没有符合当前搜索或筛选条件的代理。
        </div>
      </div>

      <div v-else class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>代理</th>
              <th>管理员</th>
              <th>联系方式</th>
              <th>终端数</th>
              <th>状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pagedItems" :key="item.organization_id">
              <td><strong>{{ item.name }}</strong><small>{{ item.code }}</small></td>
              <td><strong>{{ item.display_name }}</strong><small>@{{ item.username }}</small></td>
              <td><span>{{ item.phone }}</span><small>{{ item.email || '—' }}</small></td>
              <td>{{ item.customer_count }}</td>
              <td><span class="status-pill">{{ statusLabel(item.status) }}</span></td>
              <td><button class="text-action" type="button" @click="openAgentDetail(item)">查看详情</button></td>
            </tr>
          </tbody>
        </table>
        <div v-if="!loading && filteredItems.length === 0" class="empty-state">没有符合当前搜索或筛选条件的代理。</div>
      </div>

      <PaginationBar
        v-model:page="page"
        :total-pages="totalPages"
        :total="filteredItems.length"
        :page-size="pageSize"
      />
    </section>

    <div v-if="selectedAgent" class="modal-backdrop" @click.self="selectedAgent = null">
      <section class="modal-card agent-detail-modal">
        <div class="modal-header">
          <div>
            <p class="section-kicker">AGENT DETAIL</p>
            <h3>{{ selectedAgent.name }}</h3>
          </div>
          <button class="icon-button" type="button" @click="selectedAgent = null">×</button>
        </div>
        <div class="agent-detail-grid">
          <div><span>代理编码</span><strong>{{ selectedAgent.code }}</strong></div>
          <div><span>合作状态</span><strong>{{ statusLabel(selectedAgent.status) }}</strong></div>
          <div><span>管理员</span><strong>{{ selectedAgent.display_name }}</strong><small>@{{ selectedAgent.username }}</small></div>
          <div><span>联系电话</span><strong>{{ selectedAgent.phone || '—' }}</strong><small>{{ selectedAgent.email || '—' }}</small></div>
          <div><span>终端数量</span><strong>{{ selectedAgent.customer_count }}</strong></div>
          <div><span>组织 ID</span><strong>#{{ selectedAgent.organization_id }}</strong></div>
        </div>
        <div class="modal-actions agent-detail-actions">
          <RouterLink class="ghost-button" to="/agents/levels" @click="selectedAgent = null">查看等级</RouterLink>
          <RouterLink class="ghost-button" to="/agents/contracts" @click="selectedAgent = null">查看合同</RouterLink>
          <RouterLink class="ghost-button" to="/agents/exit" @click="selectedAgent = null">查看退出清算</RouterLink>
          <button class="primary-button" type="button" @click="selectedAgent = null">关闭</button>
        </div>
      </section>
    </div>

    <div v-if="showCreate" class="modal-backdrop" @click.self="showCreate = false">
      <form class="modal-card" @submit.prevent="submitCreate">
        <div class="modal-header">
          <div>
            <p class="section-kicker">NEW AGENT</p>
            <h3>新建代理</h3>
          </div>
          <button class="icon-button" type="button" @click="showCreate = false">×</button>
        </div>

        <div class="form-grid">
          <label>
            <span>代理名称</span>
            <input v-model="name" required placeholder="例如：南充XX科技" />
          </label>
          <label>
            <span>代理编码</span>
            <input v-model="code" required placeholder="例如：nanchong-001" />
          </label>
          <label>
            <span>登录账号</span>
            <input v-model="username" required placeholder="代理管理员登录账号" />
          </label>
          <label>
            <span>管理员名称</span>
            <input v-model="displayName" required placeholder="负责人名称" />
          </label>
          <label>
            <span>联系电话 <em class="required-mark">*</em></span>
            <input v-model="phone" type="text" required placeholder="联系电话" />
          </label>
          <label>
            <span>邮箱</span>
            <input
              v-model="email"
              type="email"
              placeholder="邮件交付初始密码时必填"
            />
          </label>

          <RegionSelect
            class="form-span-2"
            v-model:province="province"
            v-model:city="city"
            v-model:district="district"
          />

          <label class="form-span-2">
            <span>初始凭证交付方式</span>
            <select v-model="deliveryMethod" class="text-input">
              <option value="copy">创建后复制登录信息</option>
              <option value="email">发送到邮箱</option>
            </select>
          </label>
        </div>

        <div class="account-opening-note">
          <strong>密码规则</strong>
          <span>
            初始密码由系统随机生成，管理员不能手工设置。创建成功后只显示一次；代理首次登录必须立即修改密码。
          </span>
        </div>

        <div class="modal-actions">
          <button class="ghost-button" type="button" @click="showCreate = false">取消</button>
          <button class="primary-button" type="submit">创建代理</button>
        </div>
      </form>
    </div>

    <CredentialResultModal
      :open="credentialOpen"
      title="代理账号已创建"
      :display-name="createdDisplayName"
      :username="createdUsername"
      :credential="createdCredential"
      @close="closeCredential"
    />
  </div>
</template>