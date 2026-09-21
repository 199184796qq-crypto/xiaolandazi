<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { createRoom, deleteRoom, getRooms } from '../api'
import { session } from '../session'
import type { Room } from '../types'

const router = useRouter()

const rooms = ref<Room[]>([])
const loading = ref(false)
const error = ref('')
const showCreate = ref(false)
const submitting = ref(false)
const selectedTenantId = ref<number | undefined>(undefined)

const form = reactive({
  tenant_id: 0,
  platform: 'douyin',
  external_room_id: '',
  name: '',
  collector_mode: 'auto',
})

const isAdmin = computed(() => session.bootstrap?.actor.role === 'platform_admin')
const tenants = computed(() => session.bootstrap?.tenants ?? [])

const liveCount = computed(() => rooms.value.filter((room) => room.status === 'live').length)
const waitingCount = computed(() => rooms.value.filter((room) => room.status !== 'live').length)
const totalOnline = computed(() => rooms.value.reduce((sum, room) => sum + (room.online_count || 0), 0))

function tenantName(tenantId: number) {
  return tenants.value.find((tenant) => tenant.id === tenantId)?.name || '未知客户'
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
  form.external_room_id = ''
  form.name = ''
  form.platform = 'douyin'
  form.collector_mode = 'auto'
  form.tenant_id = isAdmin.value ? (selectedTenantId.value || tenants.value[0]?.id || 0) : 0
  error.value = ''
  showCreate.value = true
}

async function submitCreate() {
  if (!form.external_room_id.trim() || submitting.value) return

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
  const confirmed = window.confirm('确定删除“' + roomTitle(room) + '”吗？')
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
    <section class="page-hero">
      <div>
        <p class="section-kicker">{{ isAdmin ? '全局直播间' : '我的直播间' }}</p>
        <h2>{{ isAdmin ? '管理所有客户的直播间' : '查看并管理你的直播间' }}</h2>
        <p>
          {{
            isAdmin
              ? '添加房间、查看连接状态，并进入房间查看实时公屏。'
              : '这里仅展示当前客户名下的直播间和实时状态。'
          }}
        </p>
      </div>
      <button class="primary-button" :disabled="!session.bootstrap" @click="openCreate">
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
            <span>客户</span>
            <select v-model="selectedTenantId">
              <option :value="undefined">全部客户</option>
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

      <div v-if="error" class="inline-error">{{ error }}</div>

      <div v-if="loading && !rooms.length" class="room-grid">
        <div v-for="index in 6" :key="index" class="room-card skeleton-card"></div>
      </div>

      <div v-else-if="rooms.length" class="room-grid">
        <article
          v-for="room in rooms"
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
              <span v-else class="muted-chip">客户直播间</span>
            </div>
            <div class="card-actions">
              <button
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

      <div v-else class="empty-state">
        <div class="empty-icon">▦</div>
        <h3>还没有直播间</h3>
        <p>添加第一个直播间，接入后就能在这里查看实时公屏。</p>
        <button class="primary-button" @click="openCreate">＋ 添加直播间</button>
      </div>
    </section>

    <div v-if="showCreate" class="modal-backdrop" @click.self="showCreate = false">
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
            <span>所属客户</span>
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