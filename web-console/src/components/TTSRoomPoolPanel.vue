<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { getRooms } from '../api'
import type { Room } from '../types'
import { getTTSRoomPolicy, setTTSRoomPolicy, ttsPoolOptions, type TTSRoomPolicy, type TTSPoolID } from '../ttsPools'
const rooms = ref<Room[]>([]), selected = ref(''), policy = ref<TTSRoomPolicy | null>(null), allowed = ref<TTSPoolID[]>([])
const loading = ref(false), saving = ref(false), error = ref(''), notice = ref('')
let epoch = 0, disposed = false
const room = computed(() => rooms.value.find(r => `${r.tenant_id}:${r.id}` === selected.value))
async function loadRooms() {
  loading.value = true; error.value = ''
  try { const data = await getRooms(); if (!disposed) { rooms.value = data.items; if (!selected.value && rooms.value[0]) selected.value = `${rooms.value[0].tenant_id}:${rooms.value[0].id}` } }
  catch(e) { if (!disposed) error.value = e instanceof Error ? e.message : '读取直播间失败' }
  finally { if (!disposed) loading.value = false }
  if (!disposed && selected.value) await loadPolicy()
}
async function loadPolicy() {
  const target = room.value, token = ++epoch; policy.value = null; allowed.value = []; notice.value = ''; error.value = ''
  if (!target) return
  loading.value = true
  try {
    const result = await getTTSRoomPolicy(target.tenant_id, target.id)
    if (result.tenant_id !== target.tenant_id || result.room_id !== target.id) throw Error('直播间语音池回执不匹配')
    if (!disposed && token === epoch) { policy.value = result; allowed.value = [...result.allowed_pools] }
  } catch(e) { if (!disposed && token === epoch) error.value = e instanceof Error ? e.message : '读取直播间语音池失败' }
  finally { if (!disposed && token === epoch) loading.value = false }
}
async function save() {
  if (!policy.value || !allowed.value.length || saving.value || loading.value) return
  const token = epoch, input = { ...policy.value, allowed_pools: [...allowed.value] }; saving.value = true; error.value = ''; notice.value = ''
  try {
    const result = await setTTSRoomPolicy(input)
    if (result.tenant_id !== input.tenant_id || result.room_id !== input.room_id || result.revision !== input.revision + 1 || [...result.allowed_pools].sort().join() !== [...input.allowed_pools].sort().join()) throw Error('保存回执不匹配，请重新读取确认')
    if (!disposed && token === epoch) { policy.value = result; allowed.value = [...result.allowed_pools]; notice.value = '已保存。后续申请使用新分配，已开始合成的请求继续完成。' }
  } catch(e) { if (!disposed && token === epoch) error.value = e instanceof Error ? e.message : '保存失败，请重新读取后重试' }
  finally { if (!disposed && token === epoch) saving.value = false }
}
onMounted(() => void loadRooms())
onBeforeUnmount(() => { disposed = true; ++epoch })
</script>
<template>
  <section class="room-pool-panel" aria-label="直播间语音池分配">
    <h2>直播间语音池分配</h2>
    <p>标准对应阿里账号池，高仿真对应火山账号池。用户在方案中只看到“标准”和“高仿真”；后台分配不会替换方案音色。</p>
    <label>直播间<select v-model="selected" aria-label="分配语音池的直播间" :disabled="saving" @change="loadPolicy"><option v-if="!rooms.length" value="">暂无直播间</option><option v-for="r in rooms" :key="`${r.tenant_id}:${r.id}`" :value="`${r.tenant_id}:${r.id}`">{{ r.name }} · 账号 {{ r.tenant_id }} · 直播间 {{ r.id }}</option></select></label>
    <fieldset :disabled="loading || saving || !policy"><legend>允许使用的语音池</legend><label v-for="p in ttsPoolOptions" :key="p.id" class="choice"><input v-model="allowed" type="checkbox" :value="p.id" />{{ p.name }}</label></fieldset>
    <p>未单独分配的直播间默认允许两个池。实际合成还需对应账号和可用音色。</p>
    <p v-if="error" class="error" role="alert">{{ error }}</p><p v-if="notice" class="notice" role="status">{{ notice }}</p>
    <div class="buttons"><button type="button" :disabled="loading || saving" @click="loadRooms">重新读取</button><button type="button" class="primary" :disabled="loading || saving || !policy || !allowed.length" @click="save">{{ saving ? '保存中…' : '保存直播间分配' }}</button></div>
  </section>
</template>
<style scoped>
.choice{white-space:nowrap;flex-wrap:nowrap}.choice input{flex:0 0 20px;width:20px;height:20px;margin:0;padding:0}
.room-pool-panel{margin:20px 0;padding:24px;background:white;border:1px solid #e2e6f3;border-radius:18px;font-size:16px;color:#273750}.room-pool-panel h2{font-size:21px;margin:0 0 12px}p{line-height:1.7;color:#6c7890}label{display:grid;gap:8px}select,button{font:inherit;min-width:0;box-sizing:border-box;padding:12px;border:1px solid #d9dff0;border-radius:10px;background:white}select{width:100%}fieldset{margin:20px 0;padding:12px;border:1px solid #e2e6f3;border-radius:10px}.choice{display:inline-flex;align-items:center;margin:8px 28px 8px 0;gap:8px}.buttons{display:flex;justify-content:flex-end;gap:12px;flex-wrap:wrap}button{cursor:pointer;color:#596a91}button.primary{background:#6657eb;color:white}button:disabled{opacity:.5;cursor:default}.error{color:#ba2741}.notice{color:#16805c}@media(max-width:700px){.room-pool-panel{padding:16px}}
</style>
