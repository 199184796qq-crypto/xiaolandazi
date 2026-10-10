<script setup lang="ts">
import { computed, nextTick, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { bindLiveDevice, getLiveDevices, getRooms, getDefaultDeviceName, claimLiveDevice, renameLiveDevice, unbindLiveDevice, getDeviceAddressing, saveDeviceAddressing } from '../api'
import type { DeviceAddressingMode } from '../api'
import { session } from '../session'
import { useFeedbackErrorRef } from '../uiFeedback'

import PaginationBar from '../components/PaginationBar.vue'
import type { LiveDevice, Room } from '../types'

const devices = ref<LiveDevice[]>([])
const rooms = ref<Room[]>([])
const selectedRoomByDevice = ref<Record<number, number | ''>>({})
const selectedRoleByDevice = ref<Record<number, 'primary' | 'listener'>>({})
const loading = ref(true)
const savingDeviceId = ref<number | null>(null)
const error = useFeedbackErrorRef('')
const search = ref('')
const page = ref(1)
const pageSize = 9
const isCustomer = computed(() => session.bootstrap?.actor.role === 'customer')
const orderStorageKey = computed(() => {
  const actor = session.bootstrap?.actor
  return actor?.user_id ? `xiaolan-ui:${actor.user_id}:${actor.tenant_id || 'staff'}:device-card-order` : ''
})
const deviceOrder = ref<number[]>([])
const orderNotice = ref('')
const draggingDeviceId = ref<number | null>(null)
const dropTargetId = ref<number | null>(null)
const showAdd = ref(false)
const bindingCode = ref('')
const deviceName = ref('')
const adding = ref(false)
const mutationBusy = computed(() => savingDeviceId.value !== null || adding.value)
const success = ref('')
const editingNames = ref<Record<number, string>>({})
const editingNameId = ref<number | null>(null)
const sortingDisabled = computed(() => !orderStorageKey.value || mutationBusy.value || editingNameId.value !== null)
const nameError = ref('')
let failedNameDraft = ''
let pageActive = true
let deviceRefreshEpoch = 0
const addressingModes = ref<Record<number, DeviceAddressingMode>>({})
const addressingLoading = new Set<number>()
async function loadAddressing(device: LiveDevice) {
  if (!isCustomer.value || addressingModes.value[device.id] || addressingLoading.has(device.id)) return
  addressingLoading.add(device.id)
  try { addressingModes.value[device.id] = (await getDeviceAddressing(device.id)).mode }
  catch (e) { error.value = e instanceof Error ? e.message : '读取回应称呼失败' }
  finally { addressingLoading.delete(device.id) }
}
async function saveAddressing(device: LiveDevice) {
  const mode = addressingModes.value[device.id]
  if (!mode || mutationBusy.value) return
  savingDeviceId.value = device.id; error.value = ''; success.value = ''
  try { addressingModes.value[device.id] = (await saveDeviceAddressing(device.id, mode)).mode; success.value = '回应称呼已保存，下次语音生效' }
  catch (e) { error.value = e instanceof Error ? e.message : '保存回应称呼失败' }
  finally { savingDeviceId.value = null }
}
let refreshTimer: ReturnType<typeof setInterval> | undefined

async function openAdd() {
  if (mutationBusy.value) return
  error.value = ''; success.value = ''
  try { deviceName.value = (await getDefaultDeviceName()).device_name; bindingCode.value = ''; showAdd.value = true }
  catch (e) { error.value = e instanceof Error ? e.message : '读取设备名称失败' }
}
async function addDevice() {
  if (mutationBusy.value || !/^\d{6}$/.test(bindingCode.value) || !deviceName.value.trim()) return
  adding.value = true; error.value = ''
  try { await claimLiveDevice(bindingCode.value, deviceName.value.trim()); showAdd.value = false; await load(); success.value = '设备已添加，请选择直播间' }
  catch (e) { error.value = e instanceof Error ? e.message : '添加设备失败' }
  finally { adding.value = false }
}
async function rename(device: LiveDevice, retry = false) {
  if (!isCustomer.value || editingNameId.value !== device.id || mutationBusy.value) return
  const name = (editingNames.value[device.id] || '').trim()
  if (!retry && failedNameDraft && name === failedNameDraft) return
  if (!name || Array.from(name).length > 32 || /[\r\n]/.test(name)) { nameError.value = '设备名称须为1～32字，不能为空'; return }
  if (name === device.device_name) { cancelDeviceName(device); return }
  deviceRefreshEpoch++
  savingDeviceId.value = device.id; error.value = ''; nameError.value = ''; success.value = ''
  try {
    const updated = await renameLiveDevice(device.id, name)
    if (!pageActive) return
    deviceRefreshEpoch++
    devices.value = devices.value.map(item => item.id === device.id ? { ...item, device_name: updated.device_name } : item)
    editingNames.value[device.id] = updated.device_name
    editingNameId.value = null
    success.value = '设备名称已保存'
  }
  catch (e) { if (pageActive) { failedNameDraft = name; nameError.value = e instanceof Error ? e.message : '保存名称失败'; error.value = nameError.value } }
  finally { savingDeviceId.value = null }
}

function cancelDeviceName(device: LiveDevice) {
  if (mutationBusy.value) return
  editingNameId.value = null
  editingNames.value[device.id] = device.device_name
  nameError.value = ''
  failedNameDraft = ''
}

function deviceNameKeydown(event: KeyboardEvent, device: LiveDevice) {
  if (event.isComposing || event.keyCode === 229) return
  if (event.key === 'Enter') { event.preventDefault(); void rename(device, true) }
  else if (event.key === 'Escape') { event.preventDefault(); cancelDeviceName(device) }
}
async function unbind(device: LiveDevice) {
  if (mutationBusy.value) return
  savingDeviceId.value = device.id; error.value = ''
  try { await unbindLiveDevice(device.id); await load(); success.value = '已解除直播间绑定' }
  catch (e) { error.value = e instanceof Error ? e.message : '解绑失败' }
  finally { savingDeviceId.value = null }
}

const roomNames = computed(() => {
  const result: Record<number, string> = {}
  for (const room of rooms.value) result[room.id] = room.name || '直播间 ' + room.external_room_id
  return result
})

function roomsForDevice(device: LiveDevice) {
  return rooms.value.filter((room) => room.tenant_id === device.tenant_id)
}
function primaryForRoom(roomId: number, exceptDeviceId = 0) {
  return devices.value.find((item) => item.id !== exceptDeviceId && item.room_id === roomId && item.binding_role === 'primary')
}
function bindingRoleLabel(role?: string) {
  return role === 'listener' ? '监听设备' : '主设备'
}
function selectedRoomId(device: LiveDevice) {
  return Number(selectedRoomByDevice.value[device.id] || 0)
}
function isCurrentRoomSelected(device: LiveDevice) {
  return Boolean(device.room_id && device.room_id === selectedRoomId(device))
}
function roomBindingActionLabel(device: LiveDevice) {
  if (isCurrentRoomSelected(device)) return '解除绑定'
  const roomId = selectedRoomId(device)
  return devices.value.some((item) => item.id !== device.id && item.tenant_id === device.tenant_id && item.room_id === roomId)
    ? '切换绑定' : '绑定'
}
async function applyRoomBinding(device: LiveDevice) {
  if (mutationBusy.value || !selectedRoomId(device)) return
  if (isCurrentRoomSelected(device)) await unbind(device)
  else await saveBinding(device)
}
function roomSelectionChanged(device: LiveDevice) {
  const roomId = Number(selectedRoomByDevice.value[device.id] || 0)
  if (!roomId) return
  if (device.room_id === roomId) {
    selectedRoleByDevice.value[device.id] = device.binding_role === 'listener' ? 'listener' : 'primary'
    return
  }
  selectedRoleByDevice.value[device.id] = primaryForRoom(roomId, device.id) ? 'listener' : 'primary'
}
function primaryRoleDisabled(device: LiveDevice) {
  const roomId = Number(selectedRoomByDevice.value[device.id] || 0)
  const primary = primaryForRoom(roomId, device.id)
  return Boolean(primary && !(device.room_id === roomId && device.binding_role === 'listener'))
}
function listenerRoleDisabled(device: LiveDevice) {
  const roomId = Number(selectedRoomByDevice.value[device.id] || 0)
  return !primaryForRoom(roomId, device.id)
}
const orderedDevices = computed(() => {
  const ranks = new Map(deviceOrder.value.map((id, index) => [id, index]))
  return [...devices.value].sort((a, b) => (ranks.get(a.id) ?? Number.MAX_SAFE_INTEGER) - (ranks.get(b.id) ?? Number.MAX_SAFE_INTEGER))
})
const filteredDevices = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  if (!keyword) return orderedDevices.value
  return orderedDevices.value.filter((device) =>
    [device.device_name, device.sku_code, workStatus(device), device.connection_status]
      .some((value) => String(value || '').toLowerCase().includes(keyword)),
  )
})
const totalPages = computed(() => Math.max(1, Math.ceil(filteredDevices.value.length / pageSize)))
const pagedDevices = computed(() => {
  const current = Math.min(page.value, totalPages.value)
  const start = (current - 1) * pageSize
  return filteredDevices.value.slice(start, start + pageSize)
})
watch(pagedDevices, (items) => { for (const device of items) void loadAddressing(device) })

function endCardDrag() {
  draggingDeviceId.value = null; dropTargetId.value = null
}
function readDeviceOrder() {
  endCardDrag(); orderNotice.value = ''
  try {
    const stored: unknown = JSON.parse(window.localStorage.getItem(orderStorageKey.value) || '[]')
    deviceOrder.value = Array.isArray(stored) && stored.length <= 20000
      ? [...new Set(stored.filter((id): id is number => typeof id === 'number' && Number.isSafeInteger(id) && id > 0))] : []
  } catch { deviceOrder.value = [] }
}
watch(orderStorageKey, readDeviceOrder, { immediate: true })
function moveCard(sourceId: number, targetId: number) {
  if (sortingDisabled.value || sourceId === targetId) return
  const ids = orderedDevices.value.map(device => device.id)
  const from = ids.indexOf(sourceId), to = ids.indexOf(targetId)
  if (from < 0 || to < 0) return
  ids.splice(from, 1); ids.splice(to, 0, sourceId)
  deviceOrder.value = ids
  try {
    window.localStorage.setItem(orderStorageKey.value, JSON.stringify(ids))
    orderNotice.value = '顺序已保存'
  } catch { orderNotice.value = '顺序已调整，浏览器未能保存，刷新后可能恢复原顺序。' }
  const position = filteredDevices.value.findIndex(device => device.id === sourceId)
  if (position >= 0) page.value = Math.floor(position / pageSize) + 1
}
function canMoveCard(deviceId: number, direction: -1 | 1) {
  const index = filteredDevices.value.findIndex(device => device.id === deviceId)
  return !sortingDisabled.value && index >= 0 && !!filteredDevices.value[index + direction]
}
function moveCardBy(deviceId: number, direction: -1 | 1) {
  if (!canMoveCard(deviceId, direction)) return
  const index = filteredDevices.value.findIndex(device => device.id === deviceId)
  moveCard(deviceId, filteredDevices.value[index + direction].id)
  void nextTick(() => document.getElementById('device-sort-' + deviceId)?.focus())
}
function cardOrderKeydown(event: KeyboardEvent, deviceId: number) {
  if (['ArrowLeft', 'ArrowUp', 'ArrowRight', 'ArrowDown'].includes(event.key)) {
    event.preventDefault(); moveCardBy(deviceId, event.key === 'ArrowLeft' || event.key === 'ArrowUp' ? -1 : 1)
  } else if (event.key === 'Escape') endCardDrag()
}
function startCardDrag(event: DragEvent, deviceId: number) {
  if (sortingDisabled.value || !event.dataTransfer) { event.preventDefault(); return }
  draggingDeviceId.value = deviceId; dropTargetId.value = null; deviceRefreshEpoch++
  event.dataTransfer.effectAllowed = 'move'
  event.dataTransfer.setData('text/plain', String(deviceId))
  const card = (event.currentTarget as HTMLElement).closest('.device-console-card')
  if (card) event.dataTransfer.setDragImage(card, 28, 20)
}
function dragOverCard(event: DragEvent, deviceId: number) {
  if (draggingDeviceId.value === null || sortingDisabled.value) return
  event.preventDefault()
  if (event.dataTransfer) event.dataTransfer.dropEffect = 'move'
  dropTargetId.value = draggingDeviceId.value === deviceId ? null : deviceId
}
function dropCard(event: DragEvent, deviceId: number) {
  if (draggingDeviceId.value === null) return
  event.preventDefault(); moveCard(draggingDeviceId.value, deviceId); endCardDrag()
}
function storedOrderChanged(event: StorageEvent) {
  if (event.key === orderStorageKey.value && draggingDeviceId.value === null) readDeviceOrder()
}

function formatHeartbeat(value?: string) {
  if (!value) return '暂未收到心跳'
  const diff = Math.max(0, Date.now() - new Date(value).getTime())
  if (diff < 60_000) return Math.max(1, Math.round(diff / 1000)) + ' 秒前'
  if (diff < 3_600_000) return Math.round(diff / 60_000) + ' 分钟前'
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}

function workStatus(device: LiveDevice) {
  if (device.display_status) return device.display_status
  if (device.work_status === 'working') return '工作中'
  if (device.work_status === 'stopped') return '已停止'
  return '待机'
}

function focusDeviceName(deviceId: number) {
  if (!isCustomer.value || mutationBusy.value) return
  const device = devices.value.find(item => item.id === deviceId)
  if (!device) return
  deviceRefreshEpoch++
  editingNames.value[deviceId] = device.device_name || '小蓝搭子'
  editingNameId.value = deviceId
  nameError.value = ''
  failedNameDraft = ''
  void nextTick(() => { const input = document.getElementById('device-name-' + deviceId) as HTMLInputElement | null; input?.focus(); input?.select() })
}

function roomStatusLabel(roomId?: number) {
  if (!roomId) return '未绑定'
  return rooms.value.find((room) => room.id === roomId)?.status === 'live' ? '直播中' : '未开播'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [deviceData, roomData] = await Promise.all([getLiveDevices(), getRooms()])
    devices.value = deviceData
    rooms.value = roomData.items
    const next: Record<number, number | ''> = {}
    for (const device of deviceData) {
      next[device.id] = device.room_id || ''
      selectedRoleByDevice.value[device.id] = device.binding_role === 'listener' ? 'listener' : 'primary'
      if (editingNameId.value !== device.id) editingNames.value[device.id] = device.device_name || '小蓝搭子'
    }
    selectedRoomByDevice.value = next
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取设备绑定失败'
  } finally {
    loading.value = false
  }
}

async function saveBinding(device: LiveDevice) {
  if (mutationBusy.value) return
  const roomId = selectedRoomByDevice.value[device.id]
  if (!roomId) return
  const requestedRole = selectedRoleByDevice.value[device.id] || 'primary'
  const occupied = primaryForRoom(Number(roomId), device.id)
  if (requestedRole === 'primary' && occupied && device.room_id === Number(roomId) && device.binding_role === 'listener') {
    if (!window.confirm('切换后“' + occupied.device_name + '”将变为监听设备，当前设备成为主设备。是否继续？')) return
  }
  savingDeviceId.value = device.id
  error.value = ''; success.value = ''
  try {
    const updated = await bindLiveDevice(device.id, Number(roomId), requestedRole)
    await load()
    success.value = updated.binding_role === 'listener'
      ? '已作为监听设备接入，可同步收听并调整本机音量、字体'
      : '已设为该直播间的主设备'
  } catch (err) {
    error.value = err instanceof Error ? err.message : '绑定设备失败'
  } finally {
    savingDeviceId.value = null
  }
}

onMounted(() => { window.addEventListener('storage', storedOrderChanged); void load(); refreshTimer = setInterval(async () => { if (mutationBusy.value || editingNameId.value !== null || draggingDeviceId.value !== null) return; const epoch = deviceRefreshEpoch; try { const snapshot = await getLiveDevices(); if (pageActive && epoch === deviceRefreshEpoch && !mutationBusy.value && editingNameId.value === null && draggingDeviceId.value === null) devices.value = snapshot } catch { /* Keep last snapshot until next poll. */ } }, 10000) })
onBeforeUnmount(() => { pageActive = false; deviceRefreshEpoch++; endCardDrag(); window.removeEventListener('storage', storedOrderChanged); if (refreshTimer) clearInterval(refreshTimer) })
</script>

<template>
  <div class="device-binding-page">


    <section class="feature-workspace-hero">
      <div>
        <span class="section-kicker">DEVICE BINDING</span>
        <h2>设备绑定</h2>
        <p>输入设备屏幕上的6位绑定码添加设备，再选择自己的直播间。</p>
      </div>
      <button v-if="isCustomer" class="primary-button" type="button" :disabled="mutationBusy" @click="openAdd">添加设备</button>
    </section>

    <div v-if="error" class="inline-error">{{ error }}</div>
    <p v-if="success" role="status">{{ success }}</p>
    <form v-if="showAdd" class="device-binding-card" @submit.prevent="addDevice">
      <h3>添加设备</h3>
      <label>6位设备绑定码 <input v-model="bindingCode" type="text" inputmode="numeric" maxlength="6" pattern="[0-9]{6}" autocomplete="off" required placeholder="查看设备屏幕" /></label>
      <label>设备名称 <input v-model="deviceName" maxlength="32" required /></label>
      <div class="device-binding-control"><button class="primary-button" :disabled="mutationBusy || !/^\d{6}$/.test(bindingCode) || !deviceName.trim()">{{ adding ? '添加中…' : '确认添加' }}</button><button type="button" class="ghost-button" :disabled="mutationBusy" @click="showAdd = false">取消</button></div>
    </form>
    <div v-if="loading" class="detail-loading">正在读取设备状态…</div>

    <div v-else class="device-binding-list-shell">
      <div class="scalable-list-toolbar">
        <input v-model="search" type="search" placeholder="搜索设备名称 / 型号 / 状态" @input="page = 1" />
        <span>{{ filteredDevices.length }} / {{ devices.length }} 台</span>
        <small class="device-order-notice" role="status">{{ orderNotice || '拖动卡片上的排序手柄可调整顺序' }}</small>
      </div>
      <section class="device-binding-grid">
      <div v-if="!devices.length" class="screen-empty">
        <strong>当前账号名下还没有可绑定设备</strong>
        <span>点击“添加设备”，输入设备屏幕上的6位绑定码。</span>
      </div>

      <article v-for="device in pagedDevices" :key="device.id" class="device-console-card" :class="{ 'is-dragging': draggingDeviceId === device.id, 'is-drop-target': dropTargetId === device.id }" :data-device-id="device.id" @dragover="dragOverCard($event, device.id)" @drop="dropCard($event, device.id)">
        <div class="device-card-order-controls" role="group" :aria-label="'设备 ' + device.id + ' 排序'">
          <button :id="'device-sort-' + device.id" type="button" class="device-card-drag-handle" :draggable="!sortingDisabled" :disabled="sortingDisabled" :aria-label="'拖动设备 ' + device.id + ' 排序'" title="拖动排序，也可使用方向键调整" @dragstart="startCardDrag($event, device.id)" @dragend="endCardDrag" @keydown="cardOrderKeydown($event, device.id)">
            <svg viewBox="0 0 20 20" aria-hidden="true"><circle cx="7" cy="4" r="1.5"/><circle cx="13" cy="4" r="1.5"/><circle cx="7" cy="10" r="1.5"/><circle cx="13" cy="10" r="1.5"/><circle cx="7" cy="16" r="1.5"/><circle cx="13" cy="16" r="1.5"/></svg>
            <span>拖动排序</span>
          </button>
          <div class="device-card-order-arrows">
            <button type="button" :disabled="!canMoveCard(device.id, -1)" :aria-label="'前移设备 ' + device.id" title="向前移动" @click="moveCardBy(device.id, -1)">←</button>
            <button type="button" :disabled="!canMoveCard(device.id, 1)" :aria-label="'后移设备 ' + device.id" title="向后移动" @click="moveCardBy(device.id, 1)">→</button>
          </div>
        </div>
        <header class="device-console-header">
          <span class="device-console-mark" aria-hidden="true">设</span>
          <div class="device-console-title">
            <div>
              <input v-if="isCustomer && editingNameId === device.id" :id="'device-name-' + device.id" v-model="editingNames[device.id]" class="device-title-name-input" aria-label="设备名称" maxlength="32" :disabled="savingDeviceId === device.id" @blur="rename(device)" @keydown.stop="deviceNameKeydown($event, device)" />
              <h3 v-else>{{ device.device_name || '小蓝搭子' }}</h3>
              <button v-if="isCustomer && editingNameId !== device.id" type="button" aria-label="编辑设备名称" :disabled="mutationBusy" @click="focusDeviceName(device.id)">
                <svg viewBox="0 0 24 24"><path d="m4 16.5-.7 4.2 4.2-.7L19 8.5 15.5 5 4 16.5Zm13-13 3.5 3.5 1-1a2.5 2.5 0 0 0-3.5-3.5l-1 1Z"/></svg>
              </button>
            </div>
            <span v-if="editingNameId === device.id && nameError" class="device-title-name-error" role="alert">{{ nameError }}</span>
            <span v-else-if="editingNameId === device.id && savingDeviceId === device.id" class="device-title-name-status" role="status">正在保存…</span>
            <p>终端 #{{ device.id }} · {{ device.sku_code }} · 最后心跳 {{ formatHeartbeat(device.last_heartbeat_at) }}</p>
            <small>智能守护直播间，让互动更简单</small>
          </div>
          <span class="device-console-status" :class="{ online: device.connection_status === 'online' }">
            <i></i>{{ device.connection_status === 'online' ? '在线' : '离线' }}
          </span>
        </header>

        <section class="device-console-overview">
          <div class="device-console-overview-icon" aria-hidden="true">
            <svg viewBox="0 0 24 24"><path d="M3 12h3l2-7 4 14 3-10 2 6h4"/></svg>
          </div>
          <div class="device-console-state">
            <span>工作状态</span>
            <strong>{{ workStatus(device) }}</strong>
            <small v-if="device.stop_reason">停止原因：{{ device.stop_reason }}</small>
          </div>
          <div class="device-console-hint" :class="{ online: device.connection_status === 'online' }">
            <span aria-hidden="true">◉</span>
            <div>
              <strong>{{ device.connection_status === 'online' ? '设备连接正常' : '设备当前未连接' }}</strong>
              <small>{{ device.connection_status === 'online' ? '正在同步直播间音频与设置' : '请检查网络或设备状态' }}</small>
            </div>
          </div>
        </section>

        <section class="device-console-room">
          <span class="device-console-room-icon" aria-hidden="true">LIVE</span>
          <div class="device-console-room-picker">
            <label v-if="isCustomer" :for="'device-room-' + device.id">当前直播间</label>
            <span v-else>当前直播间</span>
            <select v-if="isCustomer" :id="'device-room-' + device.id" v-model="selectedRoomByDevice[device.id]" :disabled="mutationBusy" @change="roomSelectionChanged(device)">
              <option disabled value="">选择直播间</option>
              <option v-if="device.room_id && !roomsForDevice(device).some((room) => room.id === device.room_id)" :value="device.room_id">{{ roomNames[device.room_id] || ('直播间 ' + device.room_id) }}</option>
              <option v-for="room in roomsForDevice(device)" :key="room.id" :value="room.id">{{ room.name || '直播间 ' + room.external_room_id }} · {{ room.status === 'live' ? '直播中' : '未开播' }}</option>
            </select>
            <strong v-else>{{ device.room_id ? roomNames[device.room_id] || ('直播间 ' + device.room_id) : '暂未绑定' }}</strong>
            <RouterLink v-if="isCustomer ? selectedRoomId(device) : device.room_id" :to="{ name: 'room-detail', params: { id: isCustomer ? selectedRoomId(device) : device.room_id } }" aria-label="进入所选直播间">
              <svg viewBox="0 0 24 24"><path d="M14 4h6v6M20 4l-9 9M19 13v6a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1h6"/></svg>
            </RouterLink>
          </div>
          <button v-if="isCustomer" type="button" class="device-console-room-action" :class="{ unbind: isCurrentRoomSelected(device) }" :disabled="mutationBusy || !selectedRoomId(device)" @click="applyRoomBinding(device)">{{ savingDeviceId === device.id ? '处理中…' : roomBindingActionLabel(device) }}</button>
          <div class="device-console-role">
            <svg viewBox="0 0 24 24"><rect x="3" y="4" width="18" height="14" rx="2"/><path d="M8 22h8M12 18v4"/></svg>
            <strong>{{ device.room_id ? bindingRoleLabel(device.binding_role) : '暂未绑定设备' }}</strong>
          </div>
        </section>

        <section v-if="isCustomer" class="device-console-form">
          <div class="device-console-field-row">
            <span class="device-console-field-icon" aria-hidden="true">
              <svg viewBox="0 0 24 24"><path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4"/></svg>
            </span>
            <label :for="'device-addressing-' + device.id">唤醒称呼</label>
            <select :id="'device-addressing-' + device.id" v-model="addressingModes[device.id]" :disabled="!addressingModes[device.id] || mutationBusy">
              <option disabled value="">读取回应称呼…</option>
              <option value="auto">自动称呼（听不准用中性回应）</option>
              <option value="female">固定称呼：靓女</option>
              <option value="male">固定称呼：帅哥</option>
              <option value="child">固定称呼：小伙伴</option>
              <option value="neutral">中性回应：我在</option>
            </select>
            <button class="device-console-save" type="button" :disabled="!addressingModes[device.id] || mutationBusy" @click="saveAddressing(device)">
              <svg viewBox="0 0 24 24"><path d="M5 3h12l2 2v16H5zM8 3v6h8V3M8 21v-8h8v8"/></svg>
              保存称呼
            </button>
          </div>

          <p class="device-console-info">
            <span aria-hidden="true">i</span>
            唤醒：小蓝，小蓝 · 声音：芊悦。自动称呼仅估计声音特征，不识别个人身份；仅唤醒词时用中性回应，固定称呼不受限制。
          </p>

          <div class="device-console-binding-row">
            <span class="device-console-field-icon" aria-hidden="true">
              <svg viewBox="0 0 24 24"><path d="M5 9v6M9 6v12M13 3v18M17 8v8M21 10v4"/></svg>
            </span>
            <label :for="'device-role-' + device.id">设备角色</label>
            <select :id="'device-role-' + device.id" v-model="selectedRoleByDevice[device.id]" aria-label="设备角色" :disabled="!isCurrentRoomSelected(device) || mutationBusy">
              <option value="primary" :disabled="primaryRoleDisabled(device)">主设备</option>
              <option value="listener" :disabled="listenerRoleDisabled(device)">监听设备</option>
            </select>
            <button type="button" class="device-console-save" :disabled="!isCurrentRoomSelected(device) || mutationBusy" @click="saveBinding(device)">{{ savingDeviceId === device.id ? '保存中…' : '保存角色' }}</button>
          </div>
        </section>

        <footer v-if="isCustomer" class="device-console-note">
          <span aria-hidden="true">💡</span>
          <p>新绑定时，没有主设备就自动成为主设备；已有主设备就自动成为监听设备。<br />要切换主设备，请在已绑定的监听设备上选择“主设备”并保存角色。</p>
          <small v-if="device.room_id">{{ roomStatusLabel(device.room_id) }}</small>
        </footer>
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

<style scoped>
.device-card-order-controls{position:relative;z-index:1;display:flex;align-items:center;justify-content:space-between;gap:10px}
.device-card-order-controls button{display:inline-flex;align-items:center;justify-content:center;min-height:28px;padding:4px 7px;border:1px solid #dce3f6;border-radius:8px;background:#f7f9ff;color:#7685ad;font:inherit;font-size:12px;line-height:1.2}
.device-card-drag-handle{gap:4px;cursor:grab;user-select:none}
.device-card-drag-handle:active{cursor:grabbing}
.device-card-drag-handle svg{width:16px;height:16px;fill:currentColor;pointer-events:none}
.device-card-order-arrows{display:flex;gap:5px}
.device-card-order-arrows button{width:30px;cursor:pointer}
.device-card-order-controls button:hover:not(:disabled){color:#5563df;background:#edf1ff;border-color:#aab6f2}
.device-card-order-controls button:focus-visible{outline:2px solid #7384ed;outline-offset:2px}
.device-card-order-controls button:disabled{opacity:.4;cursor:default}
.device-console-card.is-dragging{opacity:.5}
.device-console-card.is-drop-target{outline:2px dashed #6878ee;outline-offset:2px}
.device-order-notice{color:#7988a7;font-size:12px}
@media(max-width:760px){.device-card-order-controls button{min-height:36px;font-size:13px}.device-card-order-arrows button{width:38px}}
.device-binding-grid {
  grid-template-columns: minmax(0, 1fr);
}

.device-console-card {
  position: relative;
  isolation: isolate;
  display: grid;
  gap: 18px;
  overflow: hidden;
  width: 100%;
  padding: 30px;
  border: 1px solid rgba(184, 203, 239, .78);
  border-radius: 30px;
  color: #17233f;
  font-size: calc(1em + 1px);
  background:
    radial-gradient(circle at 84% 6%, rgba(113, 152, 255, .18), transparent 27%),
    linear-gradient(145deg, rgba(255, 255, 255, .98), rgba(244, 248, 255, .96));
  box-shadow: 0 24px 64px rgba(57, 86, 150, .12), inset 0 1px rgba(255, 255, 255, .95);
}

.device-console-card::before {
  content: "";
  position: absolute;
  z-index: -1;
  top: -90px;
  right: -50px;
  width: 420px;
  height: 260px;
  border-radius: 48%;
  transform: rotate(-13deg);
  background: rgba(255, 255, 255, .34);
}

.device-console-header {
  display: grid;
  grid-template-columns: 118px minmax(0, 1fr) auto;
  align-items: center;
  gap: 22px;
}

.device-console-mark {
  display: grid;
  width: 118px;
  height: 118px;
  place-items: center;
  border: 12px solid rgba(224, 228, 255, .9);
  border-radius: 32px;
  color: #fff;
  background: linear-gradient(145deg, #8ba0ff 0%, #5d5df3 72%);
  font-size: 50px;
  font-weight: 950;
  box-shadow: 0 14px 32px rgba(81, 81, 230, .25), inset 0 1px rgba(255, 255, 255, .45);
}

.device-console-title { min-width: 0; }
.device-console-title > div { display: flex; align-items: center; gap: 10px; }
.device-console-title h3 { margin: 0; color: #121d38; font-size: calc(clamp(26px, 2.4vw, 40px) + 1px); line-height: 1.12; letter-spacing: -.035em; }
.device-console-title button { display: grid; width: 38px; height: 38px; place-items: center; padding: 0; border: 0; border-radius: 10px; color: #8592af; background: transparent; cursor: pointer; }
.device-console-title button:hover { color: #5965ef; background: #edf0ff; }
.device-console-title button svg { width: 24px; height: 24px; fill: currentColor; }
.device-console-title p { margin: 10px 0 6px; overflow: hidden; color: #6e7d9d; font-size: 19px; font-weight: 650; text-overflow: ellipsis; white-space: nowrap; }
.device-console-title small { color: #8190ad; font-size: 18px; }

.device-console-status {
  display: inline-flex;
  align-items: center;
  gap: 12px;
  align-self: start;
  margin-top: 7px;
  padding: 16px 24px;
  border: 1px solid #dce3ef;
  border-radius: 999px;
  color: #71809c;
  background: rgba(238, 242, 249, .86);
  font-size: 21px;
  font-weight: 900;
}
.device-console-status i { width: 13px; height: 13px; border-radius: 50%; background: #aab4c6; }
.device-console-status.online { color: #158b68; background: rgba(229, 250, 243, .88); }
.device-console-status.online i { background: #2fc69a; box-shadow: 0 0 0 6px rgba(47, 198, 154, .1); }

.device-console-overview,
.device-console-room {
  display: grid;
  align-items: center;
  border: 1px solid rgba(204, 215, 237, .86);
  background: rgba(255, 255, 255, .58);
  box-shadow: inset 0 1px rgba(255, 255, 255, .95), 0 10px 25px rgba(67, 93, 147, .05);
}

.device-console-overview { grid-template-columns: 88px minmax(210px, 1fr) minmax(300px, 1.1fr); gap: 24px; padding: 26px 30px; border-radius: 26px; }
.device-console-overview-icon { display: grid; width: 82px; height: 82px; place-items: center; border-radius: 50%; color: #6070ee; background: linear-gradient(145deg, #edf0ff, #e1e5ff); }
.device-console-overview-icon svg { width: 50px; height: 50px; fill: none; stroke: currentColor; stroke-width: 2.4; stroke-linecap: round; stroke-linejoin: round; }
.device-console-state { display: grid; gap: 6px; }
.device-console-state span { color: #6f7e9c; font-size: 21px; }
.device-console-state strong { color: #0f1b38; font-size: 32px; }
.device-console-state small { color: #8b96ab; font-size: 14px; }
.device-console-hint { display: flex; align-items: center; gap: 17px; justify-self: stretch; min-height: 86px; padding: 16px 26px; border-radius: 999px; color: #73819d; background: linear-gradient(135deg, rgba(247, 250, 255, .88), rgba(230, 239, 255, .88)); }
.device-console-hint > span { display: grid; width: 52px; height: 52px; place-items: center; color: #7c9bef; font-size: 26px; }
.device-console-hint > div { display: grid; gap: 4px; }
.device-console-hint strong { font-size: 18px; }
.device-console-hint small { color: #8793aa; font-size: 16px; }
.device-console-hint.online { color: #187e64; background: linear-gradient(135deg, #f4fffb, #e7faf4); }
.device-console-hint.online > span { color: #2dbd91; }

.device-console-room { grid-template-columns: 88px minmax(0, 1fr) minmax(250px, .75fr); gap: 24px; padding: 28px 30px; border-radius: 26px; }
.device-console-room-icon { display: grid; width: 82px; height: 82px; place-items: center; border-radius: 28px; color: #fff; background: linear-gradient(145deg, #ff6791, #f03c72); font-size: 20px; font-weight: 950; box-shadow: 0 12px 25px rgba(238, 64, 116, .2); }
.device-console-room > div:nth-child(2) { display: grid; grid-template-columns: auto 1fr; align-items: center; gap: 5px 13px; min-width: 0; }
.device-console-room > div:nth-child(2) span { grid-column: 1 / -1; color: #7885a0; font-size: 20px; }
.device-console-room > div:nth-child(2) strong { overflow: hidden; color: #101d3a; font-size: 30px; text-overflow: ellipsis; white-space: nowrap; }
.device-console-room > div:nth-child(2) a { display: grid; width: 32px; height: 32px; place-items: center; border-radius: 9px; color: #5a65ef; }
.device-console-room > div:nth-child(2) a:hover { background: #eceeff; }
.device-console-room > div:nth-child(2) svg { width: 22px; height: 22px; fill: none; stroke: currentColor; stroke-width: 2.3; stroke-linecap: round; stroke-linejoin: round; }
.device-console-role { display: grid; grid-template-columns: auto 1fr auto; align-items: center; gap: 17px; min-height: 68px; padding-left: 32px; border-left: 1px solid #dfe5f0; color: #64728f; }
.device-console-role svg { width: 36px; height: 36px; fill: none; stroke: currentColor; stroke-width: 2.2; stroke-linecap: round; stroke-linejoin: round; }
.device-console-role strong { color: #66738e; font-size: 23px; }
.device-console-role span { font-size: 45px; font-weight: 300; line-height: 1; }

.device-console-form { display: grid; gap: 16px; padding: 22px; border-radius: 26px; background: rgba(255, 255, 255, .68); box-shadow: inset 0 1px rgba(255, 255, 255, .96); }
.device-console-field-row,
.device-console-binding-row { display: grid; grid-template-columns: 62px 120px minmax(0, 1fr) 205px; align-items: center; gap: 16px; }
.device-console-field-icon { display: grid; width: 56px; height: 56px; place-items: center; border-radius: 18px; color: #5962ef; background: linear-gradient(145deg, #eff1ff, #e4e8ff); }
.device-console-field-icon svg { width: 28px; height: 28px; fill: none; stroke: currentColor; stroke-width: 2.2; stroke-linecap: round; stroke-linejoin: round; }
.device-console-field-row label,
.device-console-binding-row label { color: #28344d; font-size: 19px; font-weight: 750; }
.device-console-field-row input,
.device-console-field-row select,
.device-console-binding-row select { width: 100%; min-width: 0; min-height: 58px; border: 1px solid #cbd6ea; border-radius: 16px; padding: 0 20px; color: #18233e; background: rgba(255, 255, 255, .9); font: inherit; font-size: 19px; outline: none; }
.device-console-field-row input:focus,
.device-console-field-row select:focus,
.device-console-binding-row select:focus { border-color: #7e8df3; box-shadow: 0 0 0 4px rgba(91, 103, 238, .1); }
.device-console-save { display: inline-flex; min-height: 58px; align-items: center; justify-content: center; gap: 10px; border: 1px solid #cdd4fb; border-radius: 16px; color: #5661ec; background: linear-gradient(145deg, #fbfbff, #f1f3ff); font: inherit; font-size: 18px; font-weight: 850; box-shadow: 0 8px 18px rgba(75, 86, 213, .08); cursor: pointer; }
.device-console-save svg { width: 24px; height: 24px; fill: none; stroke: currentColor; stroke-width: 2.1; stroke-linecap: round; stroke-linejoin: round; }
.device-console-save:disabled { opacity: .5; cursor: not-allowed; }
.device-console-info { display: flex; align-items: flex-start; gap: 14px; margin: 0 0 8px 78px; padding: 15px 20px; border-radius: 18px; color: #6f7d98; background: linear-gradient(135deg, rgba(244, 248, 255, .92), rgba(237, 243, 253, .86)); font-size: 16px; line-height: 1.65; }
.device-console-info > span { display: grid; width: 24px; height: 24px; flex: 0 0 24px; place-items: center; border: 2px solid #5d7cf1; border-radius: 50%; color: #5d7cf1; font-size: 15px; font-weight: 900; line-height: 1; }
.device-console-binding-row { grid-template-columns: 62px 120px minmax(0, 1fr) 260px; margin-top: 2px; }

.device-console-actions { display: grid; grid-template-columns: 1fr 1fr; gap: 18px; margin-top: 10px; }
.device-console-actions button { display: flex; min-height: 92px; align-items: center; justify-content: center; gap: 18px; border: 0; border-radius: 22px; font: inherit; cursor: pointer; }
.device-console-actions button:disabled { opacity: .5; cursor: not-allowed; }
.device-console-actions button > span:last-child { display: grid; gap: 4px; text-align: left; }
.device-console-actions button strong { font-size: 21px; }
.device-console-actions button small { font-size: 15px; font-weight: 600; }
.device-console-role-save { color: #5261e9; background: linear-gradient(135deg, #eef0ff, #e5e9ff); }
.device-console-role-save > span:first-child { display: grid; width: 48px; height: 48px; place-items: center; border-radius: 50%; color: #6568ef; background: rgba(255, 255, 255, .72); }
.device-console-role-save small { color: #7983a3; }
.device-console-unbind { color: #f04475; background: linear-gradient(135deg, #fff0f5, #ffe4ed); }
.device-console-unbind svg { width: 48px; height: 48px; padding: 11px; border-radius: 50%; fill: none; stroke: currentColor; stroke-width: 2.2; stroke-linecap: round; stroke-linejoin: round; background: rgba(255, 255, 255, .7); }
.device-console-unbind small { color: #9a7181; }

.device-console-note { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 16px; padding: 18px 28px; border-radius: 22px; color: #667692; background: linear-gradient(135deg, rgba(239, 246, 255, .95), rgba(231, 238, 253, .88)); }
.device-console-note > span { font-size: 28px; }
.device-console-note p { margin: 0; font-size: 16px; font-weight: 650; line-height: 1.55; }
.device-console-note small { padding: 7px 11px; border-radius: 999px; color: #657497; background: rgba(255, 255, 255, .64); font-weight: 800; }

/* Desktop cards intentionally use one quarter of a wide workspace. */
@media (min-width: 1001px) {
  .device-binding-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
  .device-console-card { align-content: start; gap: 9px; padding: 13px; border-radius: 17px; box-shadow: 0 12px 30px rgba(57, 86, 150, .1), inset 0 1px rgba(255, 255, 255, .95); }
  .device-console-card::before { top: -55px; right: -40px; width: 220px; height: 145px; }

  .device-console-header { grid-template-columns: 54px minmax(0, 1fr) auto; gap: 9px; }
  .device-console-mark { width: 54px; height: 54px; border-width: 6px; border-radius: 15px; font-size: 25px; box-shadow: 0 8px 17px rgba(81, 81, 230, .2), inset 0 1px rgba(255, 255, 255, .45); }
  .device-console-title > div { gap: 4px; }
  .device-console-title h3 { font-size: 17px; }
  .device-console-title button { width: 23px; height: 23px; border-radius: 6px; }
  .device-console-title button svg { width: 14px; height: 14px; }
  .device-console-title p { margin: 4px 0 2px; font-size: 11px; }
  .device-console-title small { font-size: 11px; }
  .device-console-status { gap: 5px; margin-top: 2px; padding: 6px 9px; font-size: 11px; }
  .device-console-status i { width: 6px; height: 6px; }
  .device-console-status.online i { box-shadow: 0 0 0 3px rgba(47, 198, 154, .1); }

  .device-console-overview { grid-template-columns: 40px minmax(0, 1fr); gap: 8px; padding: 10px; border-radius: 13px; }
  .device-console-overview-icon { width: 38px; height: 38px; }
  .device-console-overview-icon svg { width: 24px; height: 24px; }
  .device-console-state { gap: 2px; }
  .device-console-state span { font-size: 11px; }
  .device-console-state strong { font-size: 18px; }
  .device-console-state small { font-size: 10px; }
  .device-console-hint { grid-column: 1 / -1; gap: 7px; min-height: 38px; padding: 7px 10px; border-radius: 10px; }
  .device-console-hint > span { width: 25px; height: 25px; font-size: 15px; }
  .device-console-hint > div { gap: 1px; }
  .device-console-hint strong { font-size: 11px; }
  .device-console-hint small { font-size: 10px; }

  .device-console-room { grid-template-columns: 40px minmax(0, 1fr); gap: 8px; padding: 10px; border-radius: 13px; }
  .device-console-room-icon { width: 38px; height: 38px; border-radius: 12px; font-size: 9px; box-shadow: 0 6px 13px rgba(238, 64, 116, .17); }
  .device-console-room > div:nth-child(2) { gap: 2px 5px; }
  .device-console-room > div:nth-child(2) span { font-size: 11px; }
  .device-console-room > div:nth-child(2) strong { font-size: 15px; }
  .device-console-room > div:nth-child(2) a { width: 22px; height: 22px; border-radius: 6px; }
  .device-console-room > div:nth-child(2) svg { width: 13px; height: 13px; }
  .device-console-role { grid-column: 1 / -1; min-height: 31px; gap: 8px; padding: 7px 0 0; border-top: 1px solid #dfe5f0; border-left: 0; }
  .device-console-role svg { width: 18px; height: 18px; }
  .device-console-role strong { font-size: 12px; }
  .device-console-role span { font-size: 23px; }

  .device-console-form { gap: 8px; padding: 9px; border-radius: 13px; }
  .device-console-field-row,
  .device-console-binding-row { grid-template-columns: 30px 55px minmax(0, 1fr) 79px; gap: 6px; }
  .device-console-field-icon { width: 28px; height: 28px; border-radius: 8px; }
  .device-console-field-icon svg { width: 15px; height: 15px; }
  .device-console-field-row label,
  .device-console-binding-row label { font-size: 11px; }
  .device-console-field-row input,
  .device-console-field-row select,
  .device-console-binding-row select { min-height: 32px; padding: 0 7px; border-radius: 8px; font-size: 11px; }
  .device-console-field-row input:focus,
  .device-console-field-row select:focus,
  .device-console-binding-row select:focus { box-shadow: 0 0 0 2px rgba(91, 103, 238, .1); }
  .device-console-save { min-height: 32px; gap: 4px; padding: 0 5px; border-radius: 8px; font-size: 11px; }
  .device-console-save svg { width: 13px; height: 13px; }
  .device-console-info { gap: 6px; margin: 0; padding: 7px 9px; border-radius: 9px; font-size: 10px; line-height: 1.45; }
  .device-console-info > span { width: 14px; height: 14px; flex-basis: 14px; border-width: 1px; font-size: 10px; }
  .device-console-binding-row { grid-template-columns: 30px 55px minmax(0, 1fr) 86px; }

  .device-console-actions { gap: 8px; margin-top: 2px; }
  .device-console-actions button { min-height: 46px; gap: 7px; border-radius: 11px; }
  .device-console-actions button strong { font-size: 12px; }
  .device-console-actions button small { display: none; }
  .device-console-role-save > span:first-child { width: 24px; height: 24px; font-size: 9px; }
  .device-console-unbind svg { width: 24px; height: 24px; padding: 5px; }

  .device-console-note { grid-template-columns: auto minmax(0, 1fr); gap: 7px; padding: 8px 10px; border-radius: 11px; }
  .device-console-note > span { font-size: 15px; }
  .device-console-note p { font-size: 10px; line-height: 1.4; }
  .device-console-note small { display: none; }
}

@media (min-width: 1450px) {
  .device-binding-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); }
}

@media (min-width: 1850px) {
  .device-binding-grid { grid-template-columns: repeat(4, minmax(0, 1fr)); }
}

@media (max-width: 1000px) {
  .device-console-card { padding: 22px; border-radius: 24px; }
  .device-console-header { grid-template-columns: 88px minmax(0, 1fr) auto; gap: 16px; }
  .device-console-mark { width: 88px; height: 88px; border-width: 9px; border-radius: 25px; font-size: 39px; }
  .device-console-title p { font-size: 15px; }
  .device-console-title small { font-size: 15px; }
  .device-console-status { padding: 12px 17px; font-size: 17px; }
  .device-console-overview { grid-template-columns: 70px minmax(150px, .7fr) minmax(250px, 1fr); }
  .device-console-overview-icon,
  .device-console-room-icon { width: 66px; height: 66px; }
  .device-console-overview-icon svg { width: 40px; height: 40px; }
  .device-console-room { grid-template-columns: 70px minmax(0, 1fr) 210px; }
  .device-console-field-row,
  .device-console-binding-row { grid-template-columns: 54px 105px minmax(0, 1fr) 170px; gap: 12px; }
  .device-console-field-icon { width: 50px; height: 50px; }
  .device-console-binding-row { grid-template-columns: 54px 105px minmax(0, 1fr) 210px; }
}

@media (max-width: 760px) {
  .device-console-card { gap: 14px; padding: 16px; border-radius: 20px; }
  .device-console-header { grid-template-columns: 64px minmax(0, 1fr); }
  .device-console-mark { width: 64px; height: 64px; border-width: 7px; border-radius: 19px; font-size: 29px; }
  .device-console-title h3 { font-size: 23px; }
  .device-console-title p { margin: 5px 0 0; font-size: 13px; white-space: normal; }
  .device-console-title small { display: none; }
  .device-console-title button { width: 30px; height: 30px; }
  .device-console-title button svg { width: 19px; height: 19px; }
  .device-console-status { grid-column: 1 / -1; justify-self: start; margin-top: 0; padding: 8px 13px; font-size: 14px; }
  .device-console-status i { width: 9px; height: 9px; }
  .device-console-overview { grid-template-columns: 54px minmax(0, 1fr); gap: 13px; padding: 17px; border-radius: 18px; }
  .device-console-overview-icon { width: 50px; height: 50px; }
  .device-console-overview-icon svg { width: 31px; height: 31px; }
  .device-console-state span { font-size: 15px; }
  .device-console-state strong { font-size: 23px; }
  .device-console-hint { grid-column: 1 / -1; min-height: 0; padding: 12px 16px; border-radius: 16px; }
  .device-console-hint > span { width: 34px; height: 34px; }
  .device-console-hint strong { font-size: 15px; }
  .device-console-hint small { font-size: 13px; }
  .device-console-room { grid-template-columns: 54px minmax(0, 1fr); gap: 13px; padding: 17px; border-radius: 18px; }
  .device-console-room-icon { width: 50px; height: 50px; border-radius: 17px; font-size: 13px; }
  .device-console-room > div:nth-child(2) span { font-size: 14px; }
  .device-console-room > div:nth-child(2) strong { font-size: 21px; }
  .device-console-role { grid-column: 1 / -1; min-height: 48px; padding: 12px 0 0; border-top: 1px solid #e0e6f1; border-left: 0; }
  .device-console-role svg { width: 27px; height: 27px; }
  .device-console-role strong { font-size: 17px; }
  .device-console-role span { font-size: 33px; }
  .device-console-form { gap: 13px; padding: 14px; border-radius: 18px; }
  .device-console-field-row,
  .device-console-binding-row { grid-template-columns: 44px minmax(0, 1fr); gap: 9px 11px; }
  .device-console-field-icon { width: 42px; height: 42px; border-radius: 13px; }
  .device-console-field-icon svg { width: 23px; height: 23px; }
  .device-console-field-row label,
  .device-console-binding-row label { font-size: 16px; }
  .device-console-field-row input,
  .device-console-field-row select,
  .device-console-binding-row select,
  .device-console-save { grid-column: 1 / -1; min-height: 48px; border-radius: 13px; font-size: 16px; }
  .device-console-info { margin-left: 0; padding: 12px 14px; border-radius: 14px; font-size: 13px; }
  .device-console-actions { grid-template-columns: 1fr; gap: 10px; }
  .device-console-actions button { min-height: 74px; border-radius: 17px; }
  .device-console-actions button strong { font-size: 18px; }
  .device-console-actions button small { font-size: 13px; }
  .device-console-note { grid-template-columns: auto minmax(0, 1fr); padding: 14px 16px; border-radius: 16px; }
  .device-console-note p { font-size: 13px; }
  .device-console-note small { display: none; }
}
/* Keep room selection and its single binding action together on every card. */
.device-console-room { grid-template-columns: 88px minmax(0, 1fr) auto; }
.device-console-room-picker label { grid-column: 1 / -1; color: #7885a0; font-size: 20px; }
.device-console-room-picker select { width: 100%; min-width: 0; min-height: 52px; padding: 0 32px 0 12px; border: 1px solid #d3ddf1; border-radius: 12px; color: #17233f; background: #f7f9ff; font: inherit; font-size: 25px; font-weight: 800; cursor: pointer; }
.device-console-room-picker select:focus-visible { outline: 2px solid #7e8df3; outline-offset: 2px; }
.device-console-room-picker select:disabled { cursor: not-allowed; opacity: .6; }
.device-console-room-action { align-self: end; min-width: 116px; min-height: 52px; padding: 10px 16px; border: 1px solid #cdd4fb; border-radius: 12px; color: #5661ec; background: #eef1ff; font: inherit; font-size: 19px; font-weight: 850; white-space: nowrap; cursor: pointer; }
.device-console-room-action.unbind { border-color: #ffd1df; color: #f04475; background: #fff0f5; }
.device-console-room-action:disabled { opacity: .5; cursor: not-allowed; }
.device-console-role { grid-column: 1 / -1; grid-template-columns: auto minmax(0, 1fr); min-height: 0; padding: 14px 0 0; border-top: 1px solid #dfe5f0; border-left: 0; }
.device-console-binding-row { grid-template-columns: 62px 120px minmax(0, 1fr) 205px; }
@media (min-width: 1001px) {
  .device-console-room { grid-template-columns: 40px minmax(0, 1fr) auto; }
  .device-console-room-picker label { font-size: 12px; }
  .device-console-room-picker select { min-height: 36px; padding: 0 5px; border-radius: 8px; font-size: 16px; }
  .device-console-room-action { min-width: 84px; min-height: 36px; padding: 7px 10px; border-radius: 8px; font-size: 13px; }
  .device-console-role { padding-top: 7px; }
  .device-console-binding-row { grid-template-columns: 30px 55px minmax(0, 1fr) 79px; }
}
@media (max-width: 1000px) {
  .device-console-room { grid-template-columns: 70px minmax(0, 1fr) auto; }
  .device-console-room-picker select { font-size: 22px; }
  .device-console-binding-row { grid-template-columns: 54px 105px minmax(0, 1fr) 170px; }
}
@media (max-width: 760px) {
  .device-console-room { grid-template-columns: 54px minmax(0, 1fr) auto; gap: 9px; }
  .device-console-room-picker label { font-size: 14px; }
  .device-console-room-picker select { min-height: 44px; padding: 0 6px; font-size: 16px; }
  .device-console-room-action { min-width: 84px; min-height: 44px; padding: 8px; font-size: 14px; }
  .device-console-role { padding-top: 12px; }
  .device-console-binding-row { grid-template-columns: 44px minmax(0, 1fr); }
}
.device-console-title .device-title-name-input { min-width: 0; width: 100%; max-width: 100%; min-height: 40px; padding: 5px 8px; border: 1px solid #929bff; border-radius: 10px; color: #121d38; background: #fff; font: inherit; font-size: clamp(18px, 2.2vw, 30px); font-weight: 750; box-shadow: 0 0 0 3px #e9ecff; }
.device-title-name-error, .device-title-name-status { display: block; margin-top: 6px; font-size: 13px; line-height: 1.4; }
.device-title-name-error { color: #c13b59; }
.device-title-name-status { color: #69739a; }
.device-console-title button:disabled { opacity: .5; cursor: wait; }
@media (max-width: 760px) { .device-console-title .device-title-name-input { min-height: 34px; padding: 4px 6px; font-size: 18px; } }
</style>
