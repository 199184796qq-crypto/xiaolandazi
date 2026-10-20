<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { bindLiveDevice, getLiveDevices, unbindLiveDevice } from '../api'
import type { LiveDevice } from '../types'

const props = defineProps<{ roomId: number; tenantId: number; available: boolean; runtimeBusy: boolean; canEdit: boolean }>()
const emit = defineEmits<{ changed: [] }>()
type DeviceRow = { key: number; deviceId: number | ''; role: 'primary' | 'listener' }
const devices = ref<LiveDevice[]>([])
const rows = ref<DeviceRow[]>([])
const loading = ref(false)
const busyKey = ref<number | null>(null)
const error = ref('')
const notice = ref('')
let epoch = 0, readNo = 0, nextKey = 0, alive = true
let timer: ReturnType<typeof setInterval> | undefined
const busy = computed(() => busyKey.value !== null)
const bound = computed(() => devices.value.filter(d => d.room_id === props.roomId))
const primary = computed(() => bound.value.find(d => d.binding_role === 'primary'))
const disabled = computed(() => busy.value || !props.available || props.runtimeBusy || !props.canEdit)
const disabledHint = computed(() => !props.canEdit ? '当前账号可查看设备绑定' : props.runtimeBusy ? '先停止AI直播任务，再绑定、解绑或切换角色。' : !props.available ? '服务连接恢复后即可操作。' : '')
const hasMovingSelection = computed(() => rows.value.some(row => { const d = deviceFor(row); return d?.room_id && d.room_id !== props.roomId }))
function deviceFor(row: DeviceRow) { return devices.value.find(d => d.id === row.deviceId) }
function isBound(row: DeviceRow) { return deviceFor(row)?.room_id === props.roomId }
function defaultRole(device?: LiveDevice): DeviceRow['role'] {
  return device?.room_id === props.roomId ? device.binding_role === 'listener' ? 'listener' : 'primary' : primary.value ? 'listener' : 'primary'
}
function newRow(device?: LiveDevice): DeviceRow { return { key: ++nextKey, deviceId: device?.id || '', role: defaultRole(device) } }
function choicesFor(row: DeviceRow) { return devices.value.filter(d => !rows.value.some(other => other.key !== row.key && other.deviceId === d.id)) }
function syncRows() {
  if (!devices.value.length) { rows.value = []; return }
  if (rows.value.length > devices.value.length) rows.value = rows.value.slice(0, devices.value.length)
  // Restore every saved binding on entry or a room/tenant reset. Extra selectors stay manual.
  if (!rows.value.length) {
    const savedBindings = [...bound.value].sort((a, b) => Number(b.binding_role === 'primary') - Number(a.binding_role === 'primary'))
    rows.value = savedBindings.length ? savedBindings.map(newRow) : [newRow(devices.value.length === 1 ? devices.value[0] : undefined)]
  }
  for (const row of rows.value) {
    const device = deviceFor(row)
    if (!device) { row.deviceId = ''; row.role = defaultRole(); continue }
    if (device.room_id === props.roomId) row.role = defaultRole(device)
    else if (!primary.value) row.role = 'primary'
  }
  if (devices.value.length === 1) rows.value = [rows.value.find(row => row.deviceId === devices.value[0]?.id) || newRow(devices.value[0])]
}
async function load(showLoading = false) {
  if (!props.tenantId || !props.roomId) return
  const scope = epoch, request = ++readNo
  if (showLoading) loading.value = true
  try {
    const result = await getLiveDevices()
    if (!alive || scope !== epoch || request !== readNo) return
    devices.value = result.filter(d => d.tenant_id === props.tenantId && d.lifecycle_status !== 'SCRAPPED')
    syncRows(); error.value = ''
  } catch (e) {
    if (alive && scope === epoch && request === readNo) error.value = e instanceof Error ? e.message : '读取设备失败，请刷新重试'
  } finally { if (alive && scope === epoch && request === readNo) loading.value = false }
}
function chooseDevice(row: DeviceRow) { row.role = defaultRole(deviceFor(row)); notice.value = ''; error.value = '' }
function addRow() {
  if (busy.value || loading.value || rows.value.length >= devices.value.length) return
  const hiddenBinding = bound.value.find(d => !rows.value.some(row => row.deviceId === d.id))
  rows.value.push(newRow(hiddenBinding)); notice.value = ''; error.value = ''
}
function removeRow(row: DeviceRow) {
  if (busy.value || loading.value || rows.value.length <= 1) return
  rows.value = rows.value.filter(item => item.key !== row.key); notice.value = ''; error.value = ''
}
async function applyBinding(row: DeviceRow, unbind = false) {
  const device = deviceFor(row)
  if (disabled.value || !device || device.tenant_id !== props.tenantId || (unbind && !isBound(row))) return
  const scope = epoch, previousPrimary = primary.value
  ++readNo; busyKey.value = row.key; error.value = ''; notice.value = ''
  try {
    const updated = unbind ? await unbindLiveDevice(device.id) : await bindLiveDevice(device.id, props.roomId, row.role, row.role === 'primary')
    if (!alive || scope !== epoch) return
    devices.value = devices.value.map(d => d.id === updated.id ? updated : !unbind && updated.binding_role === 'primary' && d.room_id === props.roomId && d.binding_role === 'primary' ? { ...d, binding_role: 'listener' } : d)
    syncRows(); emit('changed')
    await load()
    if (!alive || scope !== epoch) return
    notice.value = unbind ? '已解除这台设备与当前直播间的绑定。'
      : updated.binding_role === 'primary' && previousPrimary && previousPrimary.id !== updated.id
        ? `${updated.device_name || updated.sn}已设为主设备，${previousPrimary.device_name || previousPrimary.sn}自动切换为监听。`
        : updated.binding_role === 'primary' ? '已绑定为本房间主设备。' : '已绑定为监听设备。'
  } catch (e) {
    if (!alive || scope !== epoch) return
    syncRows(); error.value = e instanceof Error ? e.message : unbind ? '解绑失败，请重试' : '绑定失败，请重试'
  } finally { if (alive && scope === epoch) busyKey.value = null }
}
function roleChanged(row: DeviceRow) { if (isBound(row) && row.role !== deviceFor(row)?.binding_role) void applyBinding(row) }
function listenerDisabled(row: DeviceRow) { return !primary.value || isBound(row) && deviceFor(row)?.binding_role === 'primary' }
watch(() => [props.roomId, props.tenantId] as const, () => {
  ++epoch; ++readNo; devices.value = []; rows.value = []; busyKey.value = null; notice.value = ''; error.value = ''; void load(true)
}, { immediate: true })
watch(() => props.available, value => { if (value && !busy.value) void load() })
onMounted(() => { timer = setInterval(() => { if (!busy.value && props.available) void load() }, 10000) })
onBeforeUnmount(() => { alive = false; ++epoch; ++readNo; if (timer) clearInterval(timer) })
</script>

<template>
  <section v-if="devices.length" class="room-device-bindings" aria-label="直播间设备选择与绑定">
    <header><strong>设备选择与绑定</strong><button type="button" :disabled="loading || busy" @click="load(true)">刷新</button></header>
    <div v-for="(row, index) in rows" :key="row.key" class="room-device-binding-row" :class="{ bound: isBound(row), single: devices.length === 1 }" :data-device-id="row.deviceId || undefined">
      <template v-if="devices.length > 1"><button v-if="index === 0" class="room-device-row-control add" type="button" aria-label="增加设备选择行" title="增加一行设备选择" :disabled="busy || loading || rows.length >= devices.length" @click="addRow">＋</button><span v-else class="room-device-row-spacer" aria-hidden="true"></span></template>
      <select v-model="row.deviceId" :aria-label="'选择设备 ' + (index + 1)" :disabled="busy || loading" @change="chooseDevice(row)">
        <option v-if="devices.length > 1 || !row.deviceId" value="">选择一台设备</option>
        <option v-for="device in choicesFor(row)" :key="device.id" :value="device.id">{{ device.device_name || device.sn }} · {{ device.sn }}{{ device.room_id && device.room_id !== roomId ? '（其它房间）' : '' }}</option>
      </select>
      <button class="room-device-bind-button" :class="{ unbind: isBound(row) }" type="button" :disabled="disabled || !deviceFor(row)" @click="applyBinding(row, isBound(row))">{{ busyKey === row.key ? '处理中…' : isBound(row) ? '解绑' : '绑定' }}</button>
      <select v-model="row.role" :aria-label="(deviceFor(row)?.device_name || '设备 ' + (index + 1)) + '的角色'" :disabled="disabled || !deviceFor(row)" @change="roleChanged(row)">
        <option value="primary">主设备</option><option value="listener" :disabled="listenerDisabled(row)">监听</option>
      </select>
      <button v-if="devices.length > 1" class="room-device-row-control remove" type="button" :aria-label="'减少设备选择行 ' + (index + 1)" title="减少选择行；解除绑定请点解绑" :disabled="busy || loading || rows.length <= 1" @click="removeRow(row)">－</button>
    </div>
    <p v-if="hasMovingSelection" class="room-device-binding-hint">点击绑定后，所选设备会从原直播间移到当前直播间。</p>
    <p v-if="disabledHint" class="room-device-binding-hint">{{ disabledHint }}</p>
    <p v-if="notice" class="room-device-binding-notice" role="status">{{ notice }}</p>
    <p v-if="error" class="room-device-binding-error" role="alert">{{ error }}</p>
  </section>
  <p v-else-if="error" class="room-device-binding-error" role="alert">{{ error }}</p>
</template>

<style scoped>
.room-device-bindings{width:100%;min-width:0;padding-bottom:12px;margin-bottom:9px;border-bottom:1px solid #e0e7f7;box-sizing:border-box}.room-device-bindings header{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-bottom:9px}.room-device-bindings header strong{font-size:12px;color:#52618a}.room-device-bindings header button{border:0;background:transparent;color:#7165d5;padding:3px 0;font:inherit;font-size:11px;cursor:pointer}.room-device-binding-row{display:grid;grid-template-columns:24px minmax(0,1fr) 48px 78px 24px;gap:5px;align-items:center;margin-top:7px}.room-device-binding-row.single{grid-template-columns:minmax(0,1fr) 54px 84px}.room-device-binding-row select,.room-device-bind-button{width:100%;min-width:0;box-sizing:border-box;height:34px;padding:0 6px;border:1px solid #d9e1f6;border-radius:9px;background:#fff;color:#52618a;font:inherit;font-size:11px;text-overflow:ellipsis}.room-device-binding-row select{padding-right:3px}.room-device-bind-button{border-color:transparent;background:linear-gradient(125deg,#648dff,#7460e6);color:#fff;font-weight:700;cursor:pointer;padding:0 2px}.room-device-bind-button.unbind{border:1px solid #bdebdc;background:#ecfbf5;color:#229b7d}.room-device-row-control{width:24px;height:30px;padding:0;border:1px solid #dcdff7;border-radius:8px;background:#f4f2ff;color:#7968d8;font:inherit;font-size:16px;line-height:1;cursor:pointer}.room-device-row-control.remove{background:#fff5f8;border-color:#f2dce3;color:#c87991}.room-device-binding-row select:focus-visible,.room-device-bind-button:focus-visible,.room-device-row-control:focus-visible{outline:2px solid #8175ec;outline-offset:2px}.room-device-binding-row select:disabled,.room-device-bind-button:disabled,.room-device-row-control:disabled{opacity:.45;cursor:default}.room-device-binding-hint,.room-device-binding-notice,.room-device-binding-error{font-size:10px;line-height:1.5;overflow-wrap:anywhere;margin:8px 0 0}.room-device-binding-hint{color:#8792ad}.room-device-binding-notice{color:#22967c}.room-device-binding-error{color:#cf5673}@media(max-width:380px){.room-device-binding-row{grid-template-columns:22px minmax(0,1fr) 46px 73px 22px;gap:4px}.room-device-row-control{width:22px}.room-device-binding-row.single{grid-template-columns:minmax(0,1fr) 50px 78px}}
</style>
