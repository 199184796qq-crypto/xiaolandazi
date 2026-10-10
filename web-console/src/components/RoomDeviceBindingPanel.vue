<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { bindLiveDevice, getLiveDevices } from '../api'
import type { LiveDevice } from '../types'

const props = defineProps<{ roomId: number; tenantId: number; available: boolean; runtimeBusy: boolean; canEdit: boolean }>()
const emit = defineEmits<{ changed: [] }>()
const devices = ref<LiveDevice[]>([])
const selectedId = ref<number | ''>('')
const selectedRole = ref<'primary' | 'listener'>('primary')
const roles = ref<Record<number, string>>({})
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const notice = ref('')
let epoch = 0, readNo = 0, alive = true
let timer: ReturnType<typeof setInterval> | undefined
const bound = computed(() => devices.value.filter(d => d.room_id === props.roomId))
const primary = computed(() => bound.value.find(d => d.binding_role === 'primary'))
const choices = computed(() => devices.value.filter(d => d.room_id !== props.roomId))
const selected = computed(() => choices.value.find(d => d.id === selectedId.value))
const disabled = computed(() => busy.value || !props.available || props.runtimeBusy || !props.canEdit)
const disabledHint = computed(() => !props.canEdit ? '当前账号可查看设备绑定' : props.runtimeBusy ? '先停止AI直播任务，再绑定或切换设备角色。' : !props.available ? '服务连接恢复后即可绑定。' : '')

async function load(showLoading = false) {
  if (!props.tenantId || !props.roomId) return
  const scope = epoch, request = ++readNo
  if (showLoading) loading.value = true
  try {
    const result = await getLiveDevices()
    if (!alive || scope !== epoch || request !== readNo) return
    devices.value = result.filter(d => d.tenant_id === props.tenantId && d.lifecycle_status !== 'SCRAPPED')
    roles.value = Object.fromEntries(bound.value.map(d => [d.id, d.binding_role]))
    if (!selectedId.value) selectedRole.value = primary.value ? 'listener' : 'primary'
    if (selectedId.value && !selected.value) selectedId.value = ''
    error.value = ''
  } catch (e) {
    if (alive && scope === epoch && request === readNo) error.value = e instanceof Error ? e.message : '读取设备失败，请刷新重试'
  } finally {
    if (alive && scope === epoch && request === readNo) loading.value = false
  }
}

async function bind(device: LiveDevice, role: string, isNew = false) {
  if (disabled.value || device.tenant_id !== props.tenantId) return
  const scope = epoch
  const previousPrimary = primary.value
  ++readNo
  busy.value = true; error.value = ''; notice.value = ''
  try {
    const updated = await bindLiveDevice(device.id, props.roomId, role, role === 'primary')
    if (!alive || scope !== epoch) return
    // The server commits both roles in one transaction. Reflect its committed
    // result immediately, then refresh all bindings and the primary status card.
    devices.value = devices.value.map(d => d.id === updated.id ? updated : updated.binding_role === 'primary' && d.room_id === props.roomId && d.binding_role === 'primary' ? { ...d, binding_role: 'listener' } : d)
    roles.value = Object.fromEntries(bound.value.map(d => [d.id, d.binding_role]))
    if (isNew) { selectedId.value = ''; selectedRole.value = primary.value ? 'listener' : 'primary' }
    notice.value = updated.binding_role === 'primary' && previousPrimary && previousPrimary.id !== updated.id
      ? `${updated.device_name || updated.sn}已设为主设备，${previousPrimary.device_name || previousPrimary.sn}自动切换为监听。`
      : updated.binding_role === 'primary' ? '已绑定为本房间主设备。' : '已绑定为监听设备。'
    emit('changed')
    await load()
  } catch (e) {
    if (!alive || scope !== epoch) return
    roles.value = Object.fromEntries(bound.value.map(d => [d.id, d.binding_role]))
    error.value = e instanceof Error ? e.message : '绑定失败，请重试'
  } finally { if (alive && scope === epoch) busy.value = false }
}
function chooseDevice() { notice.value = ''; error.value = ''; selectedRole.value = primary.value ? 'listener' : 'primary' }
watch(() => [props.roomId, props.tenantId] as const, () => {
  ++epoch; ++readNo; devices.value = []; selectedId.value = ''; selectedRole.value = 'primary'; roles.value = {}; busy.value = false; notice.value = ''; error.value = ''
  void load(true)
}, { immediate: true })
watch(() => props.available, value => { if (value && !busy.value) void load() })
onMounted(() => { timer = setInterval(() => { if (!busy.value && props.available) void load() }, 10000) })
onBeforeUnmount(() => { alive = false; ++epoch; ++readNo; if (timer) clearInterval(timer) })
</script>

<template>
  <section class="room-device-bindings" aria-label="直播间设备选择与绑定">
    <header><strong>设备选择与绑定</strong><button type="button" :disabled="loading || busy" @click="load(true)">刷新</button></header>
    <div v-if="loading && !devices.length" class="room-device-binding-hint" role="status">正在读取你的设备…</div>
    <div v-for="device in bound" :key="device.id" class="room-device-binding-row bound" :data-device-id="device.id">
      <select :aria-label="'已绑定设备：' + (device.device_name || device.sn)" disabled><option>{{ device.device_name || device.sn }} · {{ device.sn }}</option></select>
      <span class="room-device-bound-label">已绑定</span>
      <select v-model="roles[device.id]" :aria-label="(device.device_name || device.sn) + '的角色'" :disabled="disabled" @change="bind(device, roles[device.id] || device.binding_role)">
        <option value="primary">主设备</option><option value="listener" :disabled="device.binding_role === 'primary'">监听</option>
      </select>
    </div>
    <div class="room-device-binding-row new">
      <select v-model="selectedId" aria-label="选择设备" :disabled="disabled || loading" @change="chooseDevice">
        <option value="">{{ choices.length ? '选择一台设备' : devices.length ? '暂无其它可绑定设备' : '请选择设备' }}</option>
        <option v-for="device in choices" :key="device.id" :value="device.id">{{ device.device_name || device.sn }} · {{ device.sn }}{{ device.room_id ? '（已绑定其他房间）' : '' }}</option>
      </select>
      <button class="room-device-bind-button" type="button" :disabled="disabled || !selected" @click="selected && bind(selected, selectedRole, true)">{{ busy ? '绑定中…' : '绑定' }}</button>
      <select v-model="selectedRole" aria-label="新设备角色" :disabled="disabled || !selected">
        <option value="primary">主设备</option><option value="listener" :disabled="!primary">监听</option>
      </select>
    </div>
    <p v-if="selected?.room_id" class="room-device-binding-hint">绑定后，这台设备会从原直播间移到当前直播间。</p>
    <p v-else-if="primary && !disabledHint" class="room-device-binding-hint">选择其它设备为主设备，原主设备会自动变为监听。</p>
    <p v-if="disabledHint" class="room-device-binding-hint">{{ disabledHint }}</p>
    <p v-if="notice" class="room-device-binding-notice" role="status">{{ notice }}</p>
    <p v-if="error" class="room-device-binding-error" role="alert">{{ error }}</p>
  </section>
</template>

<style scoped>
.room-device-bindings{width:100%;min-width:0;padding-bottom:12px;margin-bottom:9px;border-bottom:1px solid #e0e7f7;box-sizing:border-box}.room-device-bindings header{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-bottom:9px}.room-device-bindings header strong{font-size:12px;color:#52618a}.room-device-bindings header button{border:0;background:transparent;color:#7165d5;padding:3px 0;font:inherit;font-size:11px;cursor:pointer}.room-device-binding-row{display:grid;grid-template-columns:minmax(0,1fr) 54px 84px;gap:6px;align-items:center;margin-top:7px}.room-device-binding-row select,.room-device-bind-button{width:100%;min-width:0;box-sizing:border-box;height:34px;padding:0 7px;border:1px solid #d9e1f6;border-radius:9px;background:#fff;color:#52618a;font:inherit;font-size:11px;text-overflow:ellipsis}.room-device-binding-row select{padding-right:3px}.room-device-bind-button{border-color:transparent;background:linear-gradient(125deg,#648dff,#7460e6);color:#fff;font-weight:700;cursor:pointer;padding:0 3px}.room-device-bound-label{text-align:center;font-size:10px;color:#24a984;background:#ecfbf5;border-radius:7px;padding:5px 0}.room-device-binding-row.bound select:first-child:disabled{opacity:1;color:#596786;background:#f5f8ff}.room-device-binding-row select:focus-visible,.room-device-bind-button:focus-visible{outline:2px solid #8175ec;outline-offset:2px}.room-device-binding-row select:disabled,.room-device-bind-button:disabled{opacity:.55;cursor:default}.room-device-binding-hint,.room-device-binding-notice,.room-device-binding-error{font-size:10px;line-height:1.5;overflow-wrap:anywhere;margin:8px 0 0}.room-device-binding-hint{color:#8792ad}.room-device-binding-notice{color:#22967c}.room-device-binding-error{color:#cf5673}@media(max-width:380px){.room-device-binding-row{grid-template-columns:minmax(0,1fr) 50px 78px;gap:5px}}
</style>
