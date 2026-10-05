<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { getRoomEvents, getRooms } from '../api'
import DataListControls from '../components/DataListControls.vue'

import PaginationBar from '../components/PaginationBar.vue'
import type { Room, RoomEvent } from '../types'

const props = withDefaults(
  defineProps<{ focus?: 'monitor' | 'events' }>(),
  { focus: 'monitor' },
)

interface AggregatedEvent extends RoomEvent {
  room_name: string
  room_id: number
}

const rooms = ref<Room[]>([])
const events = ref<AggregatedEvent[]>([])
const loading = ref(false)
const error = useFeedbackErrorRef()
const search = ref('')
const statusFilter = ref('all')
const sortMode = ref('online-desc')
const page = ref(1)
const pageSize = ref(12)
const viewMode = ref<'card' | 'table'>('table')

const pageTitle = computed(() =>
  props.focus === 'events' ? '事件记录' : '运行监控',
)

const statusOptions = [
  { label: '全部状态', value: 'all' },
  { label: '直播中', value: 'live' },
  { label: '连接中', value: 'connecting' },
  { label: '等待连接', value: 'pending' },
  { label: '未开播', value: 'offline' },
  { label: '连接异常', value: 'error' },
]

const monitorSortOptions = [
  { label: '在线人数从高到低', value: 'online-desc' },
  { label: '在线人数从低到高', value: 'online-asc' },
  { label: '名称 A-Z', value: 'name-asc' },
]

const eventSortOptions = [
  { label: '最新事件', value: 'time-desc' },
  { label: '最早事件', value: 'time-asc' },
  { label: '直播间 A-Z', value: 'name-asc' },
]

const monitorItems = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  const result = rooms.value.filter((room) => {
    const matchesKeyword =
      !keyword ||
      [room.name, room.external_room_id, room.platform]
        .some((value) => String(value || '').toLowerCase().includes(keyword))
    const matchesStatus =
      statusFilter.value === 'all' || room.status === statusFilter.value
    return matchesKeyword && matchesStatus
  })

  return [...result].sort((a, b) => {
    if (sortMode.value === 'online-asc') {
      return (a.online_count || 0) - (b.online_count || 0)
    }
    if (sortMode.value === 'name-asc') {
      return (a.name || '').localeCompare(b.name || '', 'zh-CN')
    }
    return (b.online_count || 0) - (a.online_count || 0)
  })
})

const eventItems = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  const result = events.value.filter((event) => {
    const matchesKeyword =
      !keyword ||
      [
        event.room_name,
        event.event_type,
        JSON.stringify(event.payload || {}),
      ].some((value) => String(value || '').toLowerCase().includes(keyword))
    return matchesKeyword
  })

  return [...result].sort((a, b) => {
    if (sortMode.value === 'time-asc') {
      return new Date(a.occurred_at).getTime() - new Date(b.occurred_at).getTime()
    }
    if (sortMode.value === 'name-asc') {
      return a.room_name.localeCompare(b.room_name, 'zh-CN')
    }
    return new Date(b.occurred_at).getTime() - new Date(a.occurred_at).getTime()
  })
})

const activeItems = computed(() =>
  props.focus === 'events' ? eventItems.value : monitorItems.value,
)

const totalPages = computed(() =>
  Math.max(1, Math.ceil(activeItems.value.length / pageSize.value)),
)

const pagedMonitorItems = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return monitorItems.value.slice(start, start + pageSize.value)
})

const pagedEventItems = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return eventItems.value.slice(start, start + pageSize.value)
})

watch(
  () => props.focus,
  () => {
    search.value = ''
    statusFilter.value = 'all'
    sortMode.value = props.focus === 'events' ? 'time-desc' : 'online-desc'
    page.value = 1
    load()
  },
)

watch([search, statusFilter, sortMode, pageSize], () => {
  page.value = 1
})

watch(totalPages, (value) => {
  if (page.value > value) page.value = value
})

function roomName(room: Room) {
  return room.name || '直播间 ' + room.external_room_id
}

function statusText(value: string) {
  const map: Record<string, string> = {
    live: '直播中',
    connecting: '连接中',
    pending: '等待连接',
    offline: '未开播',
    error: '连接异常',
  }
  return map[value] || value
}

function eventLabel(value: string) {
  const map: Record<string, string> = {
    chat: '弹幕',
    member: '进房',
    like: '点赞',
    follow: '关注',
    gift: '礼物',
    room: '房间状态',
  }
  return map[value] || value
}

function eventSummary(event: AggregatedEvent) {
  const payload = event.payload as Record<string, unknown> | undefined
  if (!payload) return '—'
  for (const key of ['content', 'message', 'nickname', 'user_name', 'status']) {
    if (payload[key]) return String(payload[key])
  }
  const text = JSON.stringify(payload)
  return text.length > 100 ? text.slice(0, 100) + '…' : text
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const roomData = await getRooms()
    rooms.value = roomData.items

    if (props.focus === 'events') {
      const results = await Promise.all(
        roomData.items.slice(0, 50).map(async (room) => {
          try {
            const data = await getRoomEvents(room.id, 50)
            return data.items.map((event) => ({
              ...event,
              room_id: room.id,
              room_name: roomName(room),
            }))
          } catch {
            return [] as AggregatedEvent[]
          }
        }),
      )
      events.value = results.flat().slice(0, 1000)
    }
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取直播运维数据失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="management-page live-operations-page">


    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">{{ focus === 'events' ? 'LIVE EVENTS' : 'LIVE MONITOR' }}</p>
        <h2>{{ pageTitle }}</h2>
        <p v-if="focus === 'monitor'">集中查看所有直播间在线人数、连接状态和异常情况。</p>
        <p v-else>汇总各直播间最近事件，按时间和直播间快速定位运行问题。</p>
      </div>
      <button class="ghost-button" type="button" :disabled="loading" @click="load">
        {{ loading ? '刷新中...' : '刷新数据' }}
      </button>
    </section>

    <section class="settings-card feature-workspace-panel">
      <DataListControls
        v-model:view-mode="viewMode"
        v-model:search="search"
        v-model:status="statusFilter"
        v-model:sort="sortMode"
        v-model:page-size="pageSize"
        :search-placeholder="focus === 'events' ? '直播间 / 事件类型 / 内容' : '直播间名称 / 房间号 / 平台'"
        :status-options="focus === 'monitor' ? statusOptions : []"
        :sort-options="focus === 'monitor' ? monitorSortOptions : eventSortOptions"
        :show-view-toggle="false"
      />

      <p v-if="error" class="inline-error">{{ error }}</p>
      <div v-if="loading" class="panel-loading">正在读取数据...</div>

      <template v-else-if="focus === 'monitor'">
        <div v-if="viewMode === 'table'" class="data-table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>直播间</th><th>平台</th><th>在线人数</th><th>状态</th><th>最近事件</th><th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="room in pagedMonitorItems" :key="room.id">
                <td><strong>{{ roomName(room) }}</strong><small>{{ room.external_room_id }}</small></td>
                <td>{{ room.platform }}</td>
                <td>{{ (room.online_count || 0).toLocaleString() }}</td>
                <td><span class="status-pill" :class="'status-' + room.status">{{ statusText(room.status) }}</span></td>
                <td>{{ room.last_event_at ? new Date(room.last_event_at).toLocaleString('zh-CN') : '—' }}</td>
                <td><RouterLink class="text-action" :to="'/rooms/' + room.id">进入直播间</RouterLink></td>
              </tr>
            </tbody>
          </table>
        </div>

        <div v-else class="feature-record-grid">
          <article v-for="room in pagedMonitorItems" :key="room.id" class="feature-record-card">
            <header>
              <div><span>{{ room.external_room_id }}</span><h3>{{ roomName(room) }}</h3></div>
              <span class="status-pill" :class="'status-' + room.status">{{ statusText(room.status) }}</span>
            </header>
            <dl>
              <div><dt>平台</dt><dd>{{ room.platform }}</dd></div>
              <div><dt>在线人数</dt><dd>{{ (room.online_count || 0).toLocaleString() }}</dd></div>
              <div><dt>最近事件</dt><dd>{{ room.last_event_at ? new Date(room.last_event_at).toLocaleString('zh-CN') : '—' }}</dd></div>
            </dl>
            <footer><RouterLink class="text-action" :to="'/rooms/' + room.id">进入直播间</RouterLink></footer>
          </article>
        </div>
      </template>

      <template v-else>
        <div class="data-table-wrap">
          <table class="data-table">
            <thead>
              <tr><th>时间</th><th>直播间</th><th>事件类型</th><th>摘要</th><th>操作</th></tr>
            </thead>
            <tbody>
              <tr v-for="event in pagedEventItems" :key="event.room_id + '-' + event.id">
                <td>{{ new Date(event.occurred_at).toLocaleString('zh-CN') }}</td>
                <td>{{ event.room_name }}</td>
                <td><span class="status-pill">{{ eventLabel(event.event_type) }}</span></td>
                <td>{{ eventSummary(event) }}</td>
                <td><RouterLink class="text-action" :to="'/rooms/' + event.room_id">查看直播间</RouterLink></td>
              </tr>
            </tbody>
          </table>
        </div>
      </template>

      <div v-if="!loading && activeItems.length === 0" class="empty-state">暂无符合条件的数据。</div>

      <PaginationBar
        v-model:page="page"
        :total-pages="totalPages"
        :total="activeItems.length"
        :page-size="pageSize"
      />
    </section>
  </div>
</template>
