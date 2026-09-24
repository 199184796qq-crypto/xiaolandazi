<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { bindLiveDevice, getLiveDevices, getRooms } from '../api'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import type { LiveDevice, Room } from '../types'

const devices = ref<LiveDevice[]>([])
const rooms = ref<Room[]>([])
const selectedRoomByDevice = ref<Record<number, number | ''>>({})
const loading = ref(true)
const savingDeviceId = ref<number | null>(null)
const error = ref('')
const search = ref('')
const page = ref(1)
const pageSize = 9

const roomNames = computed(() => {
  const result: Record<number, string> = {}
  for (const room of rooms.value) result[room.id] = room.name || '直播间 ' + room.external_room_id
  return result
})

function roomsForDevice(device: LiveDevice) {
  return rooms.value.filter((room) => room.tenant_id === device.tenant_id)
}
const filteredDevices = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  if (!keyword) return devices.value
  return devices.value.filter((device) =>
    [device.sn, device.sku_code, workStatus(device), device.connection_status]
      .some((value) => String(value || '').toLowerCase().includes(keyword)),
  )
})
const totalPages = computed(() => Math.max(1, Math.ceil(filteredDevices.value.length / pageSize)))
const pagedDevices = computed(() => {
  const current = Math.min(page.value, totalPages.value)
  const start = (current - 1) * pageSize
  return filteredDevices.value.slice(start, start + pageSize)
})

function formatHeartbeat(value?: string) {
  if (!value) return '暂未收到心跳'
  const diff = Math.max(0, Date.now() - new Date(value).getTime())
  if (diff < 60_000) return Math.max(1, Math.round(diff / 1000)) + ' 秒前'
  if (diff < 3_600_000) return Math.round(diff / 60_000) + ' 分钟前'
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}

function workStatus(device: LiveDevice) {
  if (device.work_status === 'working') return '工作中'
  if (device.work_status === 'stopped') return '已停止'
  return '待机'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [deviceData, roomData] = await Promise.all([getLiveDevices(), getRooms()])
    devices.value = deviceData
    rooms.value = roomData.items
    const next: Record<number, number | ''> = {}
    for (const device of deviceData) next[device.id] = device.room_id || ''
    selectedRoomByDevice.value = next
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取设备绑定失败'
  } finally {
    loading.value = false
  }
}

async function saveBinding(device: LiveDevice) {
  const roomId = selectedRoomByDevice.value[device.id]
  if (!roomId) return
  savingDeviceId.value = device.id
  error.value = ''
  try {
    await bindLiveDevice(device.id, Number(roomId), 'primary')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '绑定设备失败'
  } finally {
    savingDeviceId.value = null
  }
}

onMounted(load)
</script>

<template>
  <div class="device-binding-page">
    <ModulePageNav context="live" active-title="设备绑定" active-nav-title="直播运维" />

    <section class="feature-workspace-hero">
      <div>
        <span class="section-kicker">DEVICE BINDING</span>
        <h2>设备绑定</h2>
        <p>管理终端名下设备与直播间的工作关系。设备所有权和库存生命周期仍由设备库存体系负责，这里只处理直播运行绑定。</p>
      </div>
    </section>

    <div v-if="error" class="inline-error">{{ error }}</div>
    <div v-if="loading" class="detail-loading">正在读取设备状态…</div>

    <div v-else class="device-binding-list-shell">
      <div class="scalable-list-toolbar">
        <input v-model="search" type="search" placeholder="搜索设备 SN / SKU / 状态" @input="page = 1" />
        <span>{{ filteredDevices.length }} / {{ devices.length }} 台</span>
      </div>
      <section class="device-binding-grid">
      <div v-if="!devices.length" class="screen-empty">
        <strong>当前账号名下还没有可绑定设备</strong>
        <span>设备完成交付并绑定到当前客户后，会自动出现在这里。</span>
      </div>

      <article v-for="device in pagedDevices" :key="device.id" class="device-binding-card">
        <div class="device-binding-head">
          <span class="device-binding-icon">设</span>
          <div>
            <strong>{{ device.sn }}</strong>
            <small>终端 #{{ device.tenant_id }} · {{ device.sku_code }} · 最后心跳 {{ formatHeartbeat(device.last_heartbeat_at) }}</small>
          </div>
          <span
            class="device-online-pill"
            :class="{ offline: device.connection_status !== 'online' }"
          >
            {{ device.connection_status === 'online' ? '在线' : '离线' }}
          </span>
        </div>

        <div class="device-binding-state-row">
          <span>工作状态</span>
          <strong>{{ workStatus(device) }}</strong>
          <small v-if="device.stop_reason">停止原因：{{ device.stop_reason }}</small>
        </div>

        <div class="device-binding-meta">
          <span>当前直播间</span>
          <strong>{{ device.room_id ? roomNames[device.room_id] || ('直播间 ' + device.room_id) : '未绑定' }}</strong>
        </div>

        <div class="device-binding-control">
          <select v-model="selectedRoomByDevice[device.id]">
            <option value="">选择直播间</option>
            <option v-for="room in roomsForDevice(device)" :key="room.id" :value="room.id">
              {{ room.name || '直播间 ' + room.external_room_id }} · {{ room.status === 'live' ? '直播中' : '未开播' }}
            </option>
          </select>
          <button
            type="button"
            class="ghost-button"
            :disabled="!selectedRoomByDevice[device.id] || savingDeviceId === device.id"
            @click="saveBinding(device)"
          >
            {{ savingDeviceId === device.id ? '保存中…' : device.room_id ? '更换绑定' : '绑定直播间' }}
          </button>
        </div>
      </article>
      </section>
      <PaginationBar
        :page="Math.min(page, totalPages)"
        :total-pages="totalPages"
        :total="filteredDevices.length"
        :page-size="pageSize"
        @update:page="page = $event"
      />
    </div>
  </div>
</template>
