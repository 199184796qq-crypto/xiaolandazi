<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { confirmAction, showToast } from '../uiFeedback'
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { createRoom, deleteRoom, getLiveOpsSupportAuthorizations, getRoomCustomerContact, getRooms, getTenants, setRoomMonitor } from '../api'
import RoomListIcon from '../components/RoomListIcon.vue'
import DataListControls from '../components/DataListControls.vue'
import PaginationBar from '../components/PaginationBar.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import { session } from '../session'
import { coreRuntime } from '../coreRuntime'
import { canDeleteOperationsRoom, hasRoomStrategyAuthorization } from '../liveSupportAccess'
import { canDelegateLivePolicyL3 } from '../livePolicyAccess'
import type { LiveSupportAuthorization, Room, Tenant } from '../types'

const router = useRouter()

const rooms = ref<Room[]>([])
const loading = ref(false)
const error = useFeedbackErrorRef()
const tenantDirectoryError = ref('')
const showCreate = ref(false)
const submitting = ref(false)
const selectedTenantId = ref<number | undefined>(undefined)
const monitorBusyIds = ref<number[]>([])
const supportAuthorizations = ref<LiveSupportAuthorization[]>([])
const deletingRoomIds = ref<number[]>([])
const contactRoomId = ref<number | null>(null)
const contactLoading = ref(false), contactError = ref(''), contactPhone = ref('')
let contactRequest = 0

function closeContact() {
  contactRoomId.value = null; contactPhone.value = ''; contactError.value = ''; contactLoading.value = false
  contactRequest++
}
async function toggleContact(room: Room) {
  if (contactRoomId.value === room.id) { closeContact(); return }
  closeContact()
  contactRoomId.value = room.id
  contactLoading.value = true
  const sequence = contactRequest
  try {
    const contact = await getRoomCustomerContact(room.id)
    if (sequence === contactRequest && contactRoomId.value === room.id) contactPhone.value = contact.phone
  } catch (err) {
    if (sequence === contactRequest) contactError.value = err instanceof Error ? err.message : '读取电话号码失败'
  } finally {
    if (sequence === contactRequest) contactLoading.value = false
  }
}
function dismissOverlays(event: Event) {
  const target = event.target as Element | null
  if (!target?.closest('.ops-customer-contact')) closeContact()
  for (const menu of document.querySelectorAll<HTMLDetailsElement>('.ops-more[open]')) {
    if (!menu.contains(target as Node)) menu.open = false
  }
}
function closeOverlaysOnEscape(event: KeyboardEvent) {
  if (event.key !== 'Escape') return
  closeContact()
  for (const menu of document.querySelectorAll<HTMLDetailsElement>('.ops-more[open]')) {
    menu.open = false
    menu.querySelector<HTMLElement>('summary')?.focus()
  }
}
function closeActionMenu(event: MouseEvent) {
  const menu = (event.target as Element).closest('details')
  if (menu) menu.open = false
}
function customerName(room: Room) {
  return room.customer_name?.trim() || tenants.value.find(t => t.id === room.tenant_id)?.name || '客户 #' + room.tenant_id
}

const viewMode = ref<'card' | 'table'>('card')
const search = ref('')
const statusFilter = ref('all')
const sortMode = ref('name-asc')
const page = ref(1)
const pageSize = ref(12)

const statusOptions = [
  { label: '全部状态', value: 'all' },
  { label: '直播中', value: 'live' },
  { label: '连接中', value: 'connecting' },
  { label: '等待连接', value: 'pending' },
  { label: '未开播', value: 'offline' },
  { label: '已停止', value: 'stopped' },
  { label: '设备离线', value: 'device_offline' },
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
      [roomTitle(room), room.id, room.external_room_id, room.platform, room.tenant_id, tenant, room.customer_name]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(keyword))
    const matchesStatus = statusFilter.value === 'all' || effectiveStatus(room) === statusFilter.value
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
  closeContact()
})
watch(page, closeContact)
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
const effectiveViewMode = computed(() => isInternalViewer.value ? 'table' : viewMode.value)
const canManageRooms = computed(() => session.bootstrap?.actor.role === 'customer')
const coreActionsAvailable = computed(() => coreRuntime.phase === 'online')

function coreIsOffline() {
  return coreRuntime.phase === 'offline'
}
const canControlMonitoring = computed(() => {
  const bootstrap = session.bootstrap
  if (!bootstrap) return false
  if (bootstrap.actor.role === 'customer' || bootstrap.actor.role === 'platform_admin') {
    return true
  }
  const access = bootstrap.staff_access
  return Boolean(
    access &&
      (access.is_super_admin || access.permissions.includes('liveops.configure')),
  )
})

function canDeleteRoom(_room: Room) {
  const bootstrap = session.bootstrap
  if (!bootstrap) return false
  if (bootstrap.actor.role === 'customer') return true
  return canDeleteOperationsRoom(bootstrap)
}

function canOpenRoomStrategy(room: Room) {
  return hasRoomStrategyAuthorization(supportAuthorizations.value, session.bootstrap?.actor.user_id || 0, room.id, room.tenant_id)
}

async function loadSupportAuthorizations() {
  supportAuthorizations.value = []
  if (!canDelegateLivePolicyL3(session.bootstrap) && !isAdmin.value) return
  try {
    supportAuthorizations.value = (await getLiveOpsSupportAuthorizations()).items || []
  } catch {
    // Failed authorization refresh must never leave a stale strategy entry visible.
  }
}

function cooperationText(room: Room) {
  return room.cooperation_status === 'non_cooperating' ? '不合作' : '合作中'
}
const tenants = ref<Tenant[]>([])

function tenantName(tenantId: number) {
  return tenants.value.find((tenant) => tenant.id === tenantId)?.name || '未知终端'
}

function roomTitle(room: Room) {
  return room.name || '直播间 ' + room.external_room_id
}

function effectiveStatus(room: Room) {
  if (coreRuntime.phase === 'offline') return 'core_unavailable'
  if (coreRuntime.phase === 'recovering' || coreRuntime.phase === 'checking') return 'core_recovering'
  if (!room.monitor_enabled) return 'stopped'
  return room.status
}

function statusText(room: Room) {
  const status = effectiveStatus(room)
  if (status === 'live') return '直播中'
  if (status === 'connecting') return '连接中'
  if (status === 'pending') return '等待连接'
  if (status === 'offline') return '未开播'
  if (status === 'stopped') return '已停止'
  if (status === 'device_offline') return '设备离线'
  if (status === 'error') return '连接异常'
  if (status === 'core_unavailable') return 'Core异常'
  if (status === 'core_recovering') return '状态同步中'
  return status || '未知'
}

function statusDescription(room: Room) {
  const status = effectiveStatus(room)
  if (status === 'live') return '一切准备就绪，正在直播'
  if (status === 'connecting') return '设备正在建立连接'
  if (status === 'pending') return '等待直播平台开播'
  if (status === 'stopped') return '直播间当前已停止'
  if (status === 'device_offline') return '直播设备当前离线'
  if (status === 'error') return '连接异常，请检查设备'
  if (status === 'core_unavailable') return '服务暂不可用，请稍后重试'
  if (status === 'core_recovering') return '正在同步直播间状态'
  return '直播间当前未开播'
}

function roomMark(room: Room) {
  return Array.from(roomTitle(room).trim())[0] || '播'
}

async function copyRoomNumber(room: Room, event: MouseEvent) {
  event.stopPropagation()
  try {
    await navigator.clipboard.writeText(room.external_room_id)
  } catch {
    // Clipboard permissions vary by browser; the room number remains selectable beside this control.
  }
}

function roomCacheKey() {
  const actorID = session.bootstrap?.actor.user_id || 0
  const scope = isAdmin.value ? (selectedTenantId.value || 'all') : (session.bootstrap?.actor.tenant_id || 'self')
  return 'livecompanion.rooms-cache.v1:' + actorID + ':' + scope
}

function saveRoomCache(items: Room[]) {
  try {
    window.localStorage.setItem(roomCacheKey(), JSON.stringify(items))
  } catch {
    // Room cache is only a degraded-mode convenience.
  }
}

function restoreRoomCache() {
  try {
    const raw = window.localStorage.getItem(roomCacheKey())
    if (!raw) return
    const cached = JSON.parse(raw) as Room[]
    if (Array.isArray(cached)) rooms.value = cached
  } catch {
    // Ignore malformed or unavailable browser storage.
  }
}

function monitorBusy(roomID: number) {
  return monitorBusyIds.value.includes(roomID)
}

async function changeMonitoring(room: Room, enabled: boolean, event: MouseEvent) {
  event.stopPropagation()
  if (!canControlMonitoring.value || !coreActionsAvailable.value || monitorBusy(room.id)) return
  monitorBusyIds.value = [...monitorBusyIds.value, room.id]
  error.value = ''
  try {
    await setRoomMonitor(room.id, enabled)
    await loadRooms()
  } catch (err) {
    error.value = err instanceof Error
      ? err.message
      : enabled
        ? '连接直播间失败'
        : '停止直播间失败'
  } finally {
    monitorBusyIds.value = monitorBusyIds.value.filter((id) => id !== room.id)
  }
}

async function loadTenantDirectory() {
  const bootstrap = session.bootstrap
  if (!bootstrap) {
    tenants.value = []
    tenantDirectoryError.value = ''
    return
  }

  if (!isAdmin.value) {
    tenants.value = bootstrap.tenants
    tenantDirectoryError.value = ''
    return
  }

  try {
    tenantDirectoryError.value = ''
    const response = await getTenants()
    tenants.value = response.items
  } catch (err) {
    tenants.value = []
    tenantDirectoryError.value =
      err instanceof Error ? err.message : '读取终端目录失败'
  }
}

async function loadRooms() {
  if (!session.bootstrap) return
  if (coreIsOffline()) {
    if (!rooms.value.length) restoreRoomCache()
    return
  }

  loading.value = true
  error.value = ''
  try {
    const [response] = await Promise.all([
      getRooms(isAdmin.value ? selectedTenantId.value : undefined),
      loadSupportAuthorizations(),
    ])
    rooms.value = response.items
    saveRoomCache(response.items)
  } catch (err) {
    if (!rooms.value.length) restoreRoomCache()
    if (!coreIsOffline()) {
      error.value = err instanceof Error ? err.message : '读取直播间失败'
    }
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
  if (!canDeleteRoom(room) || deletingRoomIds.value.includes(room.id)) return

  const isOperationsCleanup = session.bootstrap?.actor.role !== 'customer'
  const message = isOperationsCleanup
    ? '确定永久删除直播间“' + roomTitle(room) + '”吗？将停止运行、解除设备与方案关系并释放资源，仅保留业务记录和操作审计，不能恢复。'
    : '确定删除“' + roomTitle(room) + '”吗？删除后不可恢复。'
  const confirmed = await confirmAction({
    title: isOperationsCleanup ? '永久删除直播间' : '删除直播间',
    message,
    confirmText: '确认删除',
    danger: true,
  })
  if (!confirmed) return

  deletingRoomIds.value = [...deletingRoomIds.value, room.id]
  try {
    const result = await deleteRoom(room.id)
    rooms.value = rooms.value.filter((item) => item.id !== room.id)
    if (contactRoomId.value === room.id) closeContact()
    supportAuthorizations.value = supportAuthorizations.value.filter((item) => item.room_id !== room.id)
    saveRoomCache(rooms.value)
    if (result?.status === 'deletion_pending') {
      showToast('删除任务已受理', result.message, 'info', 7000)
      await loadRooms()
    } else {
      showToast('直播间已永久删除', '资源已释放，相关业务记录与操作审计继续保留。', 'success')
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : '删除直播间失败'
  } finally {
    deletingRoomIds.value = deletingRoomIds.value.filter((id) => id !== room.id)
  }
}

watch(
  () => session.bootstrap,
  (bootstrap) => {
    closeContact()
    if (!bootstrap) return
    if (!bootstrap.actor || bootstrap.actor.role !== 'platform_admin') {
      selectedTenantId.value = undefined
    }
    void loadTenantDirectory()
    loadRooms()
  },
  { immediate: true },
)

watch(selectedTenantId, () => {
  if (session.bootstrap && isAdmin.value) loadRooms()
})

watch(
  () => coreRuntime.recoverySerial,
  () => {
    if (session.bootstrap) void loadRooms()
  },
)

let refreshTimer: number | undefined

onMounted(() => {
  window.addEventListener('pointerdown', dismissOverlays)
  window.addEventListener('keydown', closeOverlaysOnEscape)
  refreshTimer = window.setInterval(() => {
    if (session.bootstrap && !loading.value && !showCreate.value) {
      loadRooms()
    }
  }, 5000)
})

onBeforeUnmount(() => {
  window.removeEventListener('pointerdown', dismissOverlays)
  window.removeEventListener('keydown', closeOverlaysOnEscape)
  closeContact()
  if (refreshTimer !== undefined) {
    window.clearInterval(refreshTimer)
  }
})
</script>

<template>
  <div class="rooms-page" :class="{ 'ops-rooms-page': isInternalViewer }">
    <ModulePageNav
      context="live"
      active-title="直播间列表"
      active-nav-title="直播间列表"
    />
    <section class="feature-workspace-hero" :class="{ 'ops-room-hero': isInternalViewer }">
      <div v-if="isInternalViewer" class="ops-hero-icon"><RoomListIcon name="video" /></div>
      <div>
        <p v-if="!isInternalViewer" class="section-kicker">我的直播间</p>
        <h2>{{ isInternalViewer ? '直播间管理' : '查看并管理你的直播间' }}</h2>
        <p>
          {{
            isInternalViewer
              ? '实时监控直播状态，快速进入和管理'
              : '这里仅展示当前终端名下的直播间和实时状态。'
          }}
        </p>
      </div>
      <button v-if="isInternalViewer" class="ghost-button ops-refresh" :disabled="loading || !coreActionsAvailable" @click="loadRooms">{{ loading ? '刷新中…' : '刷新列表' }}</button>
      <button v-if="canManageRooms" class="primary-button" :disabled="!session.bootstrap || !coreActionsAvailable" @click="openCreate">
        <span class="button-plus">＋</span>
        添加直播间
      </button>
    </section>

    <section class="room-section">
      <div v-if="!isInternalViewer || isAdmin" class="section-toolbar">
        <div v-if="!isInternalViewer">
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

          <button v-if="!isInternalViewer" class="ghost-button" :disabled="loading || !coreActionsAvailable" @click="loadRooms">
            {{ loading ? '刷新中…' : '刷新' }}
          </button>
        </div>
      </div>

      <DataListControls
        v-if="isInternalViewer"
        v-model:view-mode="viewMode"
        v-model:search="search"
        v-model:status="statusFilter"
        v-model:sort="sortMode"
        v-model:page-size="pageSize"
        search-placeholder="直播间名称 / 房间号 / 客户名称"
        :status-options="statusOptions"
        :sort-options="sortOptions"
        :show-view-toggle="false"
      />

      <div v-if="tenantDirectoryError" class="inline-error">{{ tenantDirectoryError }}</div>
      <div v-if="error" class="inline-error">{{ error }}</div>

      <div v-if="loading && !rooms.length" class="panel-loading">正在读取直播间…</div>

      <div v-else-if="filteredRooms.length && effectiveViewMode === 'card'" class="room-grid">
        <article
          v-for="room in pagedRooms"
          :key="room.id"
          class="room-card room-card-premium"
          @click="router.push('/rooms/' + room.id)"
        >
          <span
            v-if="room.cooperation_status === 'non_cooperating'"
            class="room-cooperation-corner"
            :title="room.cooperation_note || '商户已标记不合作'"
          >
            不合作
          </span>
          <div class="room-card-top">
            <div class="platform-icon room-brand-mark">{{ roomMark(room) }}</div>
            <div class="room-heading">
              <div class="room-title-line">
                <strong>{{ roomTitle(room) }}</strong>
                <button
                  class="room-edit-button"
                  type="button"
                  title="管理直播间"
                  aria-label="管理直播间"
                  @click.stop="router.push('/rooms/' + room.id)"
                >
                  <svg viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M4 16.75V20h3.25L17.8 9.45l-3.25-3.25L4 16.75Zm16.7-10.04a1 1 0 0 0 0-1.42l-2-2a1 1 0 0 0-1.42 0l-1.56 1.56 3.25 3.25 1.73-1.39Z" />
                  </svg>
                </button>
              </div>
              <div class="room-number-line">
                <span>房间号 {{ room.external_room_id }}</span>
                <button type="button" title="复制房间号" aria-label="复制房间号" @click="copyRoomNumber(room, $event)">
                  <svg viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M8 7V5a3 3 0 0 1 3-3h8a3 3 0 0 1 3 3v8a3 3 0 0 1-3 3h-2v3a3 3 0 0 1-3 3H6a3 3 0 0 1-3-3v-8a3 3 0 0 1 3-3h2Zm3-2v3h3a3 3 0 0 1 3 3v2h2V5h-8Zm3 6H6v8h8v-8Z" />
                  </svg>
                </button>
              </div>
              <span class="room-management-chip">直播间管理</span>
            </div>
            <div class="room-status-block">
              <span class="status-pill" :class="'status-' + effectiveStatus(room)">
                <i></i>
                {{ statusText(room) }}
              </span>
              <small>{{ statusDescription(room) }}</small>
            </div>
          </div>

          <div class="room-card-metrics">
            <div class="room-people-metric">
              <span class="room-metric-icon room-metric-people" aria-hidden="true">
                <svg viewBox="0 0 24 24">
                  <path d="M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8Zm6.5-.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7ZM2 19.2C2 15.8 5.1 13 9 13s7 2.8 7 6.2V21H2v-1.8Zm14.1-5.7c3.4.2 5.9 2.5 5.9 5.4V21h-4v-1.8c0-2.1-.7-4-1.9-5.7Z" />
                </svg>
              </span>
              <div><span>在线人数</span><strong>{{ room.online_count.toLocaleString() }}</strong></div>
              <svg class="room-metric-sparkline" viewBox="0 0 120 34" preserveAspectRatio="none" aria-hidden="true">
                <path d="M1 32C19 31 23 22 38 24c13 2 19-13 34-8 14 5 22-14 47-14" />
              </svg>
            </div>
            <div>
              <span class="room-metric-icon room-metric-platform" aria-hidden="true">
                <svg viewBox="0 0 24 24">
                  <path d="m12 2 9 5-9 5-9-5 9-5Zm-7.8 9L12 15.35 19.8 11 21 13l-9 5-9-5 1.2-2Zm0 5L12 20.35 19.8 16 21 18l-9 5-9-5 1.2-2Z" />
                </svg>
              </span>
              <div><span>平台</span><strong>{{ room.platform === 'douyin' ? '抖音' : room.platform }}</strong></div>
            </div>
          </div>

          <div class="room-card-footer">
            <div>
              <span v-if="isAdmin" class="tenant-chip">{{ tenantName(room.tenant_id) }}</span>
              <span v-else class="muted-chip">
                <svg viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M9.2 8.2a5.4 5.4 0 0 0 0 7.6l1.4-1.4a3.4 3.4 0 0 1 0-4.8L9.2 8.2Zm-2.8-2.8a9.4 9.4 0 0 0 0 13.2l1.4-1.4a7.4 7.4 0 0 1 0-10.4L6.4 5.4Zm11.2 0-1.4 1.4a7.4 7.4 0 0 1 0 10.4l1.4 1.4a9.4 9.4 0 0 0 0-13.2Zm-2.8 2.8-1.4 1.4a3.4 3.4 0 0 1 0 4.8l1.4 1.4a5.4 5.4 0 0 0 0-7.6ZM12 10a2 2 0 1 0 0 4 2 2 0 0 0 0-4Zm-1 5.8V22h2v-6.2h-2Z" />
                </svg>
                终端直播间
              </span>
              <span
                v-if="room.recharge_dormant_90_days && room.cooperation_status !== 'non_cooperating'"
                class="room-recharge-warning-chip"
                title="仅作业务提醒，不自动改变合作状态"
              >
                90天+未充值
              </span>
            </div>
            <div class="card-actions">
              <button
                v-if="canControlMonitoring"
                class="room-action-button room-action-connect"
                type="button"
                :disabled="!coreActionsAvailable || room.monitor_enabled || monitorBusy(room.id)"
                @click="changeMonitoring(room, true, $event)"
              >
                <span class="room-action-symbol" aria-hidden="true">
                  <svg viewBox="0 0 24 24"><path d="M10.6 13.4a1 1 0 0 1 0-1.4l3.6-3.6a3 3 0 0 1 4.2 4.2l-2.1 2.1a3 3 0 0 1-4.2 0 1 1 0 0 1 1.4-1.4 1 1 0 0 0 1.4 0l2.1-2.1a1 1 0 0 0-1.4-1.4L12 13.4a1 1 0 0 1-1.4 0Zm2.8-2.8a1 1 0 0 1 0 1.4l-3.6 3.6a3 3 0 0 1-4.2-4.2l2.1-2.1a3 3 0 0 1 4.2 0 1 1 0 1 1-1.4 1.4 1 1 0 0 0-1.4 0L7 12.8a1 1 0 0 0 1.4 1.4l3.6-3.6a1 1 0 0 1 1.4 0Z" /></svg>
                </span>
                {{ monitorBusy(room.id) && !room.monitor_enabled ? '连接中…' : '连接' }}
              </button>
              <button
                v-if="canControlMonitoring"
                class="room-action-button room-action-stop"
                type="button"
                :disabled="!coreActionsAvailable || !room.monitor_enabled || monitorBusy(room.id)"
                @click="changeMonitoring(room, false, $event)"
              >
                <span class="room-action-symbol room-action-stop-symbol" aria-hidden="true">
                  <svg viewBox="0 0 24 24"><rect x="6" y="6" width="12" height="12" rx="1.5" /></svg>
                </span>
                {{ monitorBusy(room.id) && room.monitor_enabled ? '停止中…' : '停止' }}
              </button>
              <button
                v-if="canDeleteRoom(room)"
                class="room-action-button room-action-delete"
                type="button"
                :disabled="deletingRoomIds.includes(room.id)"
                title="删除直播间"
                @click="removeRoom(room, $event)"
              >
                <span class="room-action-symbol" aria-hidden="true">
                  <svg viewBox="0 0 24 24"><path d="M8 4V2h8v2h5v2h-2l-1 16H6L5 6H3V4h5Zm2 4v10h2V8h-2Zm4 0v10h2V8h-2Z" /></svg>
                </span>
                删除
              </button>
              <button
                class="room-action-button room-action-enter"
                type="button"
                @click.stop="router.push('/rooms/' + room.id)"
              >
                进入公屏 <span class="room-action-arrow" aria-hidden="true">→</span>
              </button>
            </div>
          </div>
        </article>
      </div>

      <div v-else-if="filteredRooms.length && effectiveViewMode === 'table'" class="data-table-wrap" :class="{ 'ops-table-wrap': isInternalViewer }">
        <table class="data-table" :class="{ 'ops-room-table': isInternalViewer }">
          <thead>
            <tr>
              <th>直播间</th>
              <th v-if="isInternalViewer">客户</th>
              <th>平台</th>
              <th>在线人数</th>
              <th>状态</th>
              <th v-if="isInternalViewer">合作</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="room in pagedRooms" :key="room.id">
              <td><div class="ops-room-identity"><span v-if="isInternalViewer" class="ops-room-mark">{{ roomMark(room) }}</span><div><strong>{{ roomTitle(room) }}</strong><div class="ops-room-number"><small>房间号 {{ room.external_room_id }}</small><button v-if="isInternalViewer" type="button" aria-label="复制房间号" title="复制房间号" @click="copyRoomNumber(room, $event)"><RoomListIcon name="copy" /></button></div></div></div></td>
              <td v-if="isInternalViewer" class="ops-customer-cell"><div class="ops-customer-contact"><div class="ops-customer-name"><strong>{{ customerName(room) }}</strong><button class="ops-phone-button" type="button" :aria-expanded="contactRoomId === room.id" :aria-label="`查看${customerName(room)}的电话号码`" title="查看电话号码" @click="toggleContact(room)"><RoomListIcon name="phone" /></button></div><small>客户 #{{ room.tenant_id }}</small><div v-if="contactRoomId === room.id" class="ops-phone-panel" role="status"><small>客户联系电话</small><strong v-if="contactLoading">正在读取…</strong><span v-else-if="contactError" class="ops-phone-error">{{ contactError }}</span><strong v-else>{{ contactPhone || '该客户未填写电话号码' }}</strong><button type="button" aria-label="关闭电话号码" @click="closeContact">×</button></div></div></td>
              <td><div class="ops-platform"><span v-if="isInternalViewer && room.platform === 'douyin'" class="ops-douyin-mark" aria-hidden="true">♪</span>{{ room.platform === 'douyin' ? '抖音' : room.platform }}</div></td>
              <td><div class="ops-online-count"><span v-if="isInternalViewer" class="ops-people-icon"><RoomListIcon name="users" /></span><div><strong>{{ room.online_count.toLocaleString() }}</strong><small v-if="isInternalViewer">人在线</small></div></div></td>
              <td><span class="status-pill" :class="'status-' + effectiveStatus(room)"><i v-if="isInternalViewer" />{{ statusText(room) }}</span></td>
              <td v-if="isInternalViewer">
                <span
                  class="room-cooperation-table-badge"
                  :class="{ danger: room.cooperation_status === 'non_cooperating' }"
                  :title="room.cooperation_note || ''"
                >
                  <i v-if="isInternalViewer" />
                  {{ cooperationText(room) }}
                </span>
                <small v-if="room.recharge_dormant_90_days">90天+未充值</small>
              </td>
              <td>
                <div v-if="isInternalViewer" class="ops-table-actions">
                  <button class="ops-enter-button" type="button" @click="router.push('/rooms/' + room.id)">进入直播间 <RoomListIcon name="arrow" /></button>
                  <details v-if="canOpenRoomStrategy(room) || canControlMonitoring || canDeleteRoom(room)" class="ops-more" name="ops-room-actions">
                    <summary :aria-label="`更多操作：${roomTitle(room)}`" title="更多操作"><RoomListIcon name="more" /></summary>
                    <div class="ops-action-menu">
                      <button v-if="canOpenRoomStrategy(room)" type="button" @click="closeActionMenu($event); router.push('/operations/live/rooms/' + room.id + '/strategy')"><RoomListIcon name="strategy" />直播策略</button>
                      <button v-if="canControlMonitoring" type="button" :disabled="!coreActionsAvailable || room.monitor_enabled || monitorBusy(room.id)" @click="closeActionMenu($event); changeMonitoring(room, true, $event)"><RoomListIcon name="link" />{{ monitorBusy(room.id) && !room.monitor_enabled ? '连接中…' : '重新连接' }}</button>
                      <button v-if="canControlMonitoring" type="button" :disabled="!coreActionsAvailable || !room.monitor_enabled || monitorBusy(room.id)" @click="closeActionMenu($event); changeMonitoring(room, false, $event)"><RoomListIcon name="stop" />{{ monitorBusy(room.id) && room.monitor_enabled ? '停止中…' : '停止直播' }}</button>
                      <button v-if="canDeleteRoom(room)" class="ops-delete-button" type="button" :disabled="deletingRoomIds.includes(room.id)" @click="closeActionMenu($event); removeRoom(room, $event)"><RoomListIcon name="trash" />删除直播间</button>
                    </div>
                  </details>
                </div>
                <div v-else class="room-table-actions">
                  <button
                    v-if="canControlMonitoring"
                    class="room-action-button room-action-connect compact"
                    type="button"
                    :disabled="!coreActionsAvailable || room.monitor_enabled || monitorBusy(room.id)"
                    @click="changeMonitoring(room, true, $event)"
                  ><span class="room-action-symbol" aria-hidden="true">⛓</span>连接</button>
                  <button
                    v-if="canControlMonitoring"
                    class="room-action-button room-action-stop compact"
                    type="button"
                    :disabled="!coreActionsAvailable || !room.monitor_enabled || monitorBusy(room.id)"
                    @click="changeMonitoring(room, false, $event)"
                  ><span class="room-action-symbol room-action-stop-symbol" aria-hidden="true">■</span>停止</button>
                  <button
                    v-if="canDeleteRoom(room)"
                    class="room-action-button room-action-delete compact"
                    type="button"
                    :disabled="deletingRoomIds.includes(room.id)"
                    @click="removeRoom(room, $event)"
                  ><span class="room-action-symbol" aria-hidden="true">×</span>删除</button>
                  <button
                    class="room-action-button room-action-enter compact"
                    type="button"
                    @click="router.push('/rooms/' + room.id)"
                  >进入 <span class="room-action-arrow" aria-hidden="true">→</span></button>
                  <button
                    v-if="canOpenRoomStrategy(room)"
                    class="room-action-button room-action-enter compact"
                    type="button"
                    @click="router.push('/operations/live/rooms/' + room.id + '/strategy')"
                  >直播策略</button>
                </div>
              </td>
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

<style scoped>
.ops-rooms-page{color:#202a44;gap:24px}.ops-room-hero.feature-workspace-hero{padding:22px 4px!important;min-height:100px!important;border:0!important;background:transparent!important;box-shadow:none!important;display:flex;align-items:center;justify-content:flex-start;gap:22px!important}.ops-room-hero .ops-hero-icon{width:68px;height:68px;border-radius:20px;display:grid;place-items:center;color:white;background:linear-gradient(150deg,#b5b7ff,#5b60ff);box-shadow:0 10px 26px #6166e929,inset 0 1px 0 #ffffff80;flex-shrink:0}.ops-hero-icon svg{width:34px;height:34px}.ops-room-hero h2{font-size:30px!important;line-height:1.3!important;margin:0!important;letter-spacing:-.02em}.ops-room-hero.feature-workspace-hero>div>p{display:block!important;margin:6px 0 0!important;font-size:17px!important;color:#8b95ad}.ops-refresh{margin-left:auto;font-size:15px!important;padding:12px 22px!important}.ops-rooms-page .room-section{padding:0!important;overflow:visible!important;border-radius:22px!important;border:1px solid #e8edf8!important;background:linear-gradient(135deg,#fff,#f9fbff)!important;box-shadow:0 12px 34px #425b9709!important}.ops-rooms-page .section-toolbar{padding:18px 24px 0;justify-content:flex-end}.ops-rooms-page :deep(.data-list-controls){padding:18px 26px;margin:0;border:0;border-bottom:1px solid #edf1f9;background:transparent;border-radius:22px 22px 0 0}.ops-rooms-page :deep(.data-list-controls label>span){font-size:13px!important;color:#8b95ad}.ops-rooms-page :deep(.data-list-controls input),.ops-rooms-page :deep(.data-list-controls select){font-size:16px!important;min-height:42px;border-radius:12px}.ops-table-wrap{overflow:visible!important;border-radius:0!important}.ops-room-table{table-layout:auto;width:100%;min-width:1040px;border-collapse:collapse}.ops-room-table thead th{background:#f6f8fe!important;padding:23px 26px!important;color:#8b93a7!important;font-size:16px!important;font-weight:700!important;white-space:nowrap;border-bottom:1px solid #edf1fa}.ops-room-table td{padding:28px 26px!important;vertical-align:middle;font-size:17px!important;border-bottom:1px solid #edf1fa!important}.ops-room-table tr:last-child td{border-bottom:0!important}.ops-room-table td strong{font-size:19px!important;font-weight:750;line-height:1.5}.ops-room-table td small{display:block;font-size:14px!important;color:#929db4!important;line-height:1.6;margin-top:3px!important}.ops-room-identity{display:flex;align-items:center;gap:16px;min-width:230px}.ops-room-mark{display:grid;place-items:center;width:54px;height:54px;flex-shrink:0;border-radius:16px;color:#c77604;background:linear-gradient(145deg,#ffe5bc,#fff0d9);font-size:26px;font-weight:800}.ops-room-number{display:flex;align-items:center;gap:10px;white-space:nowrap}.ops-room-number button{border:0;padding:3px;background:none;color:#8d98b5;cursor:pointer;display:flex}.ops-room-number svg{width:18px;height:18px}.ops-customer-cell{position:relative;min-width:170px}.ops-customer-contact{position:relative}.ops-customer-name{display:flex;align-items:center;gap:9px}.ops-customer-name strong{max-width:220px;overflow-wrap:anywhere}.ops-phone-button{border:1px solid #e4e8ff;background:#f3f5ff;color:#777cdd;width:32px;height:32px;border-radius:10px;display:grid;place-items:center;cursor:pointer;flex-shrink:0}.ops-phone-button[aria-expanded=true]{background:#e8eaff;border-color:#afb5fa}.ops-phone-button svg{width:17px;height:17px}.ops-phone-panel{position:absolute;z-index:60;top:calc(100% + 12px);left:0;min-width:220px;max-width:280px;padding:18px 36px 18px 18px;border:1px solid #dfe5fc;border-radius:16px;background:#fff;box-shadow:0 14px 40px #5267b22b}.ops-phone-panel strong{display:block;color:#5f65ca;overflow-wrap:anywhere;font-size:18px!important}.ops-phone-panel button{position:absolute;right:8px;top:6px;border:0;background:transparent;font-size:22px;color:#8792aa;cursor:pointer}.ops-phone-error{color:#bf586a;font-size:14px}.ops-platform{display:flex;align-items:center;gap:10px;white-space:nowrap}.ops-douyin-mark{width:40px;height:40px;border-radius:11px;background:#080a11;color:white;display:grid;place-items:center;font-size:35px;font-weight:900;text-shadow:-2px -1px #00e6df,2px 1px #ff3163}.ops-online-count{display:flex;align-items:center;gap:12px}.ops-people-icon{display:grid;place-items:center;width:42px;height:42px;border-radius:13px;background:#eeeefe;color:#6268e8;flex-shrink:0}.ops-people-icon svg{width:25px;height:25px}.ops-online-count strong{font-size:27px!important;line-height:1.2}.ops-room-table .status-pill,.ops-room-table .room-cooperation-table-badge{padding:10px 14px!important;display:inline-flex;align-items:center;gap:9px;font-size:16px!important;font-weight:750;border:0!important;border-radius:13px!important;white-space:nowrap;width:auto!important;line-height:1.4}.ops-room-table .status-pill i,.ops-room-table .room-cooperation-table-badge i{width:10px;height:10px;flex-shrink:0;border-radius:50%;background:currentColor;box-shadow:0 0 0 4px #ffffff60}.ops-room-table .status-live{color:#18865f;background:#e5f7ee}.ops-room-table .room-cooperation-table-badge{color:#129797;background:#e2f6f9}.ops-room-table .room-cooperation-table-badge.danger{color:#c76472;background:#fff0f3}.ops-table-actions{display:flex;align-items:center;gap:14px;white-space:nowrap}.ops-enter-button{border:0;border-radius:13px;background:linear-gradient(135deg,#7072ff,#5555e6);box-shadow:0 9px 24px #5758e329;color:#fff;padding:15px 20px;font-size:17px;font-weight:750;display:flex;align-items:center;gap:12px;cursor:pointer}.ops-enter-button svg{width:22px;height:22px}.ops-more{position:relative}.ops-more summary{display:grid;place-items:center;cursor:pointer;list-style:none;width:54px;height:54px;border:1px solid #cdd2ff;border-radius:13px;background:#f1f2ff;color:#666bef}.ops-more summary::-webkit-details-marker{display:none}.ops-more summary svg{width:25px;height:25px}.ops-more[open] summary{background:#e9ebff;border-color:#aeb5ff}.ops-action-menu{position:absolute;top:calc(100% + 10px);right:0;z-index:50;min-width:206px;padding:12px;background:#fff;border:1px solid #ecedf8;border-radius:18px;box-shadow:0 20px 45px #43538422;display:grid;gap:4px}.ops-action-menu button{display:flex;align-items:center;gap:13px;width:100%;padding:12px 13px;border:0;border-radius:10px;background:transparent;text-align:left;color:#596782;font:inherit;font-size:16px;cursor:pointer}.ops-action-menu button svg{width:21px;height:21px}.ops-action-menu button:hover:not(:disabled){background:#f2f4ff;color:#6166d9}.ops-action-menu button:disabled{opacity:.4;cursor:not-allowed}.ops-action-menu .ops-delete-button{color:#f04e5c;border-top:1px solid #edf0fa;border-radius:0;margin-top:5px;padding-top:15px}.ops-rooms-page :deep(.pagination-bar){border-top:1px solid #edf1f9;padding:24px 28px;margin:0;min-height:88px}.ops-rooms-page :deep(.pagination-bar-summary){font-size:16px!important;color:#919bb1}.ops-rooms-page :deep(.pagination-bar button){font-size:15px!important;min-height:40px;border-radius:11px}.ops-rooms-page button:focus-visible,.ops-more summary:focus-visible{outline:3px solid #b3bcff;outline-offset:3px}
@media(max-width:1300px){.ops-room-table td,.ops-room-table thead th{padding-left:18px!important;padding-right:18px!important}.ops-room-identity{min-width:200px}.ops-enter-button{font-size:15px;padding:14px}.ops-table-actions{gap:9px}}
@media(max-width:1050px){.ops-table-wrap{overflow-x:auto!important;padding-bottom:220px;margin-bottom:-220px}.ops-room-hero.feature-workspace-hero{flex-wrap:wrap}.ops-room-hero h2{font-size:26px!important}.ops-room-hero.feature-workspace-hero>div>p{font-size:15px!important}.ops-hero-icon{width:54px!important;height:54px!important}.ops-refresh{padding:10px 16px!important}.ops-rooms-page :deep(.data-list-controls){padding:16px}}
</style>
