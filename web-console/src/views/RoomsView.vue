<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { confirmAction } from '../uiFeedback'
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { createRoom, deleteRoom, getRooms } from '../api'
import DataListControls from '../components/DataListControls.vue'
import PaginationBar from '../components/PaginationBar.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import { session } from '../session'
import type { Room } from '../types'

const router = useRouter()

const rooms = ref<Room[]>([])
const loading = ref(false)
const error = useFeedbackErrorRef()
const showCreate = ref(false)
const submitting = ref(false)
const selectedTenantId = ref<number | undefined>(undefined)

const viewMode = ref<'card' | 'table'>('card')
const search = ref('')
const statusFilter = ref('all')
const sortMode = ref('online-desc')
const page = ref(1)
const pageSize = ref(12)

const statusOptions = [
  { label: '全部状态', value: 'all' },
  { label: '直播中', value: 'live' },
  { label: '连接中', value: 'connecting' },
  { label: '等待连接', value: 'pending' },
  { label: '未开播', value: 'offline' },
  { label: '连接异常', value: 'error' },
]

const sortOptions = [
  { label: '在线人数从高到低', value: 'online-desc' },
  { label: '在线人数从低到高', value: 'online-asc' },
  { label: '直播间名称 A-Z', value: 'name-asc' },
  { label: '直播间名称 Z-A', value: 'name-desc' },
]

const filteredRooms = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  const result = rooms.value.filter((room) => {
    const tenant = tenantName(room.tenant_id)
    const matchesKeyword =
      !keyword ||
      [roomTitle(room), room.external_room_id, room.platform, tenant]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(keyword))
    const matchesStatus = statusFilter.value === 'all' || room.status === statusFilter.value
    return matchesKeyword && matchesStatus
  })

  return [...result].sort((a, b) => {
    if (sortMode.value === 'online-asc') return (a.online_count || 0) - (b.online_count || 0)
    if (sortMode.value === 'name-asc') return roomTitle(a).localeCompare(roomTitle(b), 'zh-CN')
    if (sortMode.value === 'name-desc') return roomTitle(b).localeCompare(roomTitle(a), 'zh-CN')
    return (b.online_count || 0) - (a.online_count || 0)
  })
})

const totalPages = computed(() => Math.max(1, Math.ceil(filteredRooms.value.length / pageSize.value)))
const pagedRooms = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredRooms.value.slice(start, start + pageSize.value)
})

watch([search, statusFilter, sortMode, pageSize], () => {
  page.value = 1
})
watch(totalPages, (value) => {
  if (page.value > value) page.value = value
})

const form = reactive({
  tenant_id: 0,
  platform: 'douyin',
  external_room_id: '',
  name: '',
  collector_mode: 'auto',
})

const isAdmin = computed(() => session.bootstrap?.actor.role === 'platform_admin')
const isInternalViewer = computed(() => ['platform_admin', 'staff', 'sales_staff'].includes(session.bootstrap?.actor.role || ''))
const canManageRooms = computed(() => session.bootstrap?.actor.role === 'customer')
const tenants = computed(() => session.bootstrap?.tenants ?? [])

const liveCount = computed(() => rooms.value.filter((room) => room.status === 'live').length)
const waitingCount = computed(() => rooms.value.filter((room) => room.status !== 'live').length)
const totalOnline = computed(() =>
  rooms.value
    .filter((room) => room.status === 'live')
    .reduce((sum, room) => sum + (room.online_count || 0), 0),
)

function tenantName(tenantId: number) {
  return tenants.value.find((tenant) => tenant.id === tenantId)?.name || '未知终端'
}

function roomTitle(room: Room) {
  return room.name || '直播间 ' + room.external_room_id
}

function statusText(status: string) {
  if (status === 'live') return '直播中'
  if (status === 'connecting') return '连接中'
  if (status === 'pending') return '等待连接'
  if (status === 'offline') return '未开播'
  if (status === 'error') return '连接异常'
  return status || '未知'
}

async function loadRooms() {
  if (!session.bootstrap) return

  loading.value = true
  error.value = ''
  try {
    const response = await getRooms(isAdmin.value ? selectedTenantId.value : undefined)
    rooms.value = response.items
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取直播间失败'
  } finally {
    loading.value = false
  }
}

function openCreate() {
  if (isAdmin.value) return
  form.external_room_id = ''
  form.name = ''
  form.platform = 'douyin'
  form.collector_mode = 'auto'
  form.tenant_id = isAdmin.value ? (selectedTenantId.value || tenants.value[0]?.id || 0) : 0
  error.value = ''
  showCreate.value = true
}

async function submitCreate() {
  if (isAdmin.value || !form.external_room_id.trim() || submitting.value) return

  submitting.value = true
  error.value = ''
  try {
    await createRoom({
      tenant_id: isAdmin.value ? form.tenant_id : undefined,
      platform: form.platform,
      external_room_id: form.external_room_id.trim(),
      name: form.name.trim(),
      collector_mode: form.collector_mode,
    })
    showCreate.value = false
    await loadRooms()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '添加直播间失败'
  } finally {
    submitting.value = false
  }
}

async function removeRoom(room: Room, event: MouseEvent) {
  event.stopPropagation()
  if (isAdmin.value) return
  const confirmed = await confirmAction({ title: '删除直播间', message: '确定删除“' + roomTitle(room) + '”吗？删除后不可恢复。', confirmText: '确认删除', danger: true })
  if (!confirmed) return

  try {
    await deleteRoom(room.id)
    rooms.value = rooms.value.filter((item) => item.id !== room.id)
  } catch (err) {
    error.value = err instanceof Error ? err.message : '删除直播间失败'
  }
}

watch(
  () => session.bootstrap,
  (bootstrap) => {
    if (!bootstrap) return
    if (!bootstrap.actor || bootstrap.actor.role !== 'platform_admin') {
      selectedTenantId.value = undefined
    }
    loadRooms()
  },
  { immediate: true },
)

watch(selectedTenantId, () => {
  if (session.bootstrap && isAdmin.value) loadRooms()
})

let refreshTimer: number | undefined

onMounted(() => {
  refreshTimer = window.setInterval(() => {
    if (session.bootstrap && !loading.value && !showCreate.value) {
      loadRooms()
    }
  }, 5000)
})

onBeforeUnmount(() => {
  if (refreshTimer !== undefined) {
    window.clearInterval(refreshTimer)
  }
})
</script>

<template>
  <div class="rooms-page">
    <ModulePageNav
      :context="isInternalViewer ? 'live' : 'workspace-customer'"
      :active-title="isInternalViewer ? '直播间列表' : '直播运维'"
      :active-nav-title="isInternalViewer ? '直播间列表' : '直播运维'"
    />
    <section class="page-hero">
      <div>
        <p class="section-kicker">{{ isAdmin ? '全局直播间' : '我的直播间' }}</p>
        <h2>{{ isAdmin ? '管理所有终端的直播间' : '查看并管理你的直播间' }}</h2>
        <p>
          {{
            isAdmin
              ? '只读查看所有终端直播间的连接状态、在线情况和实时公屏。'
              : '这里仅展示当前终端名下的直播间和实时状态。'
          }}
        </p>
      </div>
      <button v-if="canManageRooms" class="primary-button" :disabled="!session.bootstrap" @click="openCreate">
        <span class="button-plus">＋</span>
        添加直播间
      </button>
    </section>

    <section class="stat-grid">
      <article class="stat-card">
        <span class="stat-label">房间总数</span>
        <strong>{{ rooms.length }}</strong>
        <small>当前可管理房间</small>
      </article>
      <article class="stat-card">
        <span class="stat-label">直播中</span>
        <strong class="success-number">{{ liveCount }}</strong>
        <small>正在产生实时事件</small>
      </article>
      <article class="stat-card">
        <span class="stat-label">等待 / 离线</span>
        <strong>{{ waitingCount }}</strong>
        <small>等待开播或接入采集</small>
      </article>
      <article class="stat-card">
        <span class="stat-label">当前在线</span>
        <strong>{{ totalOnline.toLocaleString() }}</strong>
        <small>所有可见房间合计</small>
      </article>
    </section>

    <section class="room-section">
      <div class="section-toolbar">
        <div>
          <h3>直播间列表</h3>
          <span>{{ rooms.length }} 个房间</span>
        </div>

        <div class="toolbar-actions">
          <label v-if="isAdmin" class="select-wrap">
            <span>终端</span>
            <select v-model="selectedTenantId">
              <option :value="undefined">全部终端</option>
              <option
                v-for="tenant in tenants"
                :key="tenant.id"
                :value="tenant.id"
              >
                {{ tenant.name }}
              </option>
            </select>
          </label>

          <button class="ghost-button" :disabled="loading" @click="loadRooms">
            {{ loading ? '刷新中…' : '刷新' }}
          </button>
        </div>
      </div>

      <DataListControls
        v-model:view-mode="viewMode"
        v-model:search="search"
        v-model:status="statusFilter"
        v-model:sort="sortMode"
        v-model:page-size="pageSize"
        search-placeholder="直播间名称 / 房间号 / 终端"
        :status-options="statusOptions"
        :sort-options="sortOptions"
      />

      <div v-if="error" class="inline-error">{{ error }}</div>

      <div v-if="loading && !rooms.length && viewMode === 'card'" class="room-grid">
        <div v-for="index in 6" :key="index" class="room-card skeleton-card"></div>
      </div>

      <div v-else-if="filteredRooms.length && viewMode === 'card'" class="room-grid">
        <article
          v-for="room in pagedRooms"
          :key="room.id"
          class="room-card"
          @click="router.push('/rooms/' + room.id)"
        >
          <div class="room-card-top">
            <div class="platform-icon">抖</div>
            <div class="room-heading">
              <strong>{{ roomTitle(room) }}</strong>
              <span>房间号 {{ room.external_room_id }}</span>
            </div>
            <span class="status-pill" :class="'status-' + room.status">
              <i></i>
              {{ statusText(room.status) }}
            </span>
          </div>

          <div class="room-card-metrics">
            <div>
              <span>在线人数</span>
              <strong>{{ room.online_count.toLocaleString() }}</strong>
            </div>
            <div>
              <span>平台</span>
              <strong>{{ room.platform === 'douyin' ? '抖音' : room.platform }}</strong>
            </div>
          </div>

          <div class="room-card-footer">
            <div>
              <span v-if="isAdmin" class="tenant-chip">{{ tenantName(room.tenant_id) }}</span>
              <span v-else class="muted-chip">终端直播间</span>
            </div>
            <div class="card-actions">
              <button
                v-if="!isAdmin"
                class="danger-link"
                title="删除直播间"
                @click="removeRoom(room, $event)"
              >
                删除
              </button>
              <button class="enter-link">进入公屏 →</button>
            </div>
          </div>
        </article>
      </div>

      <div v-else-if="filteredRooms.length && viewMode === 'table'" class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>直播间</th>
              <th v-if="isAdmin">终端</th>
              <th>平台</th>
              <th>在线人数</th>
              <th>状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="room in pagedRooms" :key="room.id">
              <td><strong>{{ roomTitle(room) }}</strong><small>房间号 {{ room.external_room_id }}</small></td>
              <td v-if="isAdmin">{{ tenantName(room.tenant_id) }}</td>
              <td>{{ room.platform === 'douyin' ? '抖音' : room.platform }}</td>
              <td>{{ room.online_count.toLocaleString() }}</td>
              <td><span class="status-pill" :class="'status-' + room.status">{{ statusText(room.status) }}</span></td>
              <td><button class="text-action" type="button" @click="router.push('/rooms/' + room.id)">进入</button></td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-else class="empty-state">
        <div class="empty-icon">▦</div>
        <h3>{{ rooms.length ? '没有符合筛选条件的直播间' : '还没有直播间' }}</h3>
        <p>{{ rooms.length ? '可以调整搜索、状态或终端筛选。' : (isAdmin ? '当前还没有终端直播间。管理员只负责查看运行状态。' : '添加第一个直播间，接入后就能在这里查看实时公屏。') }}</p>
        <button v-if="canManageRooms && !rooms.length" class="primary-button" @click="openCreate">＋ 添加直播间</button>
      </div>

      <PaginationBar
        v-model:page="page"
        :total-pages="totalPages"
        :total="filteredRooms.length"
        :page-size="pageSize"
      />
    </section>

    <div v-if="showCreate && !isAdmin" class="modal-backdrop" @click.self="showCreate = false">
      <form class="modal-card" @submit.prevent="submitCreate">
        <div class="modal-header">
          <div>
            <span class="section-kicker">新建连接</span>
            <h3>添加直播间</h3>
          </div>
          <button type="button" class="close-button" @click="showCreate = false">×</button>
        </div>

        <div class="form-stack">
          <label v-if="isAdmin">
            <span>所属终端</span>
            <select v-model="form.tenant_id" required>
              <option v-for="tenant in tenants" :key="tenant.id" :value="tenant.id">
                {{ tenant.name }}
              </option>
            </select>
          </label>

          <label>
            <span>直播平台</span>
            <select v-model="form.platform">
              <option value="douyin">抖音直播</option>
            </select>
          </label>

          <label>
            <span>直播间链接 / 房间号 <b>*</b></span>
            <input
              v-model="form.external_room_id"
              required
              autocomplete="off"
              placeholder="粘贴抖音直播分享链接，或直接输入房间号"
            />
            <small>系统会自动识别分享链接；只输入房间号时会自动补全抖音直播地址。</small>
          </label>

          <label>
            <span>房间名称</span>
            <input
              v-model="form.name"
              autocomplete="off"
              placeholder="例如：美妆旗舰店（可选）"
            />
          </label>
        </div>

        <div class="modal-actions">
          <button type="button" class="ghost-button" @click="showCreate = false">取消</button>
          <button class="primary-button" :disabled="submitting">
            {{ submitting ? '添加中…' : '确认添加' }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>