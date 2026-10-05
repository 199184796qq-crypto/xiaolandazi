<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getLiveOpsSupportAuthorizations, getRoom } from '../api'
import { hasRoomStrategyAuthorization } from '../liveSupportAccess'
import { session } from '../session'
import type { Room } from '../types'
import LiveContentPolicyPanel from '../components/LiveContentPolicyPanel.vue'
import LiveStrategyView from './LiveStrategyView.vue'

const route = useRoute()
const router = useRouter()
const room = ref<Room | null>(null)
const loading = ref(false)
const error = ref('')
const workspaceMode = ref<'menu' | 'plan' | 'operations'>('menu')
let disposed = false
let requestSerial = 0

async function checkAuthorization(reset = false) {
  const roomId = Number(route.params.roomId)
  const requestId = ++requestSerial
  if (reset) {
    room.value = null
    workspaceMode.value = 'menu'
  }
  loading.value = true
  try {
    const authorizations = await getLiveOpsSupportAuthorizations()
    if (disposed || requestId !== requestSerial) return
    if (!hasRoomStrategyAuthorization(authorizations.items || [], session.bootstrap?.actor.user_id || 0, roomId)) {
      room.value = null
      error.value = '客户未授权或已撤销此直播间的协助权限，无法查看和修改直播策略。'
      return
    }
    if (!room.value || room.value.id !== roomId) {
      const value = await getRoom(roomId)
      if (disposed || requestId !== requestSerial) return
      room.value = value
    }
    error.value = ''
  } catch (err) {
    if (disposed || requestId !== requestSerial) return
    room.value = null
    error.value = err instanceof Error ? err.message : '校验客户授权失败'
  } finally {
    if (!disposed && requestId === requestSerial) loading.value = false
  }
}

watch(() => route.params.roomId, () => void checkAuthorization(true), { immediate: true })
const timer = window.setInterval(() => void checkAuthorization(), 15000)
onBeforeUnmount(() => {
  disposed = true
  requestSerial++
  window.clearInterval(timer)
})
</script>

<template>
  <div class="room-support-workspace">
    <header class="room-support-header">
      <div>
        <span class="section-kicker">客户授权协助</span>
        <h2>{{ room ? room.name + ' · 运维协助' : '运维协助' }}</h2>
        <p>先选择本次要处理的业务。智能体方案与运营配置相互独立，客户撤销授权后访问权限立即失效。</p>
      </div>
      <div class="room-support-header-actions">
        <button v-if="workspaceMode !== 'menu'" class="ghost-button" type="button" @click="workspaceMode = 'menu'">返回功能选择</button>
        <button class="ghost-button" type="button" @click="router.push('/operations/support')">返回协助申请</button>
      </div>
    </header>
    <p v-if="error" class="inline-error">{{ error }}</p>
    <p v-else-if="loading && !room" class="panel-loading">正在校验客户授权…</p>
    <section v-else-if="room && workspaceMode === 'menu'" class="room-support-choice" aria-labelledby="room-support-choice-title">
      <header>
        <span>WORKSPACE</span>
        <h3 id="room-support-choice-title">请选择要进入的功能</h3>
        <p>进入后只处理当前“{{ room.name }}”直播间已授权的内容。</p>
      </header>
      <div class="room-support-choice-grid">
        <button type="button" class="room-support-choice-card plan" @click="workspaceMode = 'plan'">
          <span class="room-support-choice-icon">策</span>
          <span class="room-support-choice-copy">
            <strong>智能体方案</strong>
            <small>管理直播方案、话术、声音、版本及发布关系</small>
          </span>
          <b>进入</b>
        </button>
        <button type="button" class="room-support-choice-card operations" @click="workspaceMode = 'operations'">
          <span class="room-support-choice-icon">运</span>
          <span class="room-support-choice-copy">
            <strong>运营配置</strong>
            <small>设置直播模式、内容生命周期和自动更新策略</small>
          </span>
          <b>进入</b>
        </button>
      </div>
    </section>
    <LiveStrategyView
      v-else-if="room && workspaceMode === 'plan'"
      :key="room.id"
      support-session
      :support-tenant-id="room.tenant_id"
      :support-room-id="room.id"
    />
    <LiveContentPolicyPanel
      v-else-if="room && workspaceMode === 'operations'"
      :room-id="room.id"
    />
  </div>
</template>

<style scoped>
.room-support-workspace{display:grid;gap:18px;min-width:0}
.room-support-header{display:flex;align-items:center;justify-content:space-between;gap:20px;padding:20px 24px;border:1px solid #dfe5f1;border-radius:18px;background:linear-gradient(135deg,#fff,#f2f5ff)}
.room-support-header h2{margin:6px 0;color:#24314c;font-size:24px}
.room-support-header p{margin:0;color:#738199;font-size:14px;line-height:1.6}
.room-support-header button{flex-shrink:0}
.room-support-header-actions{display:flex;align-items:center;justify-content:flex-end;gap:10px;flex-wrap:wrap}
.room-support-choice{display:grid;gap:22px;padding:28px;border:1px solid #dfe5f4;border-radius:22px;background:linear-gradient(145deg,#fff 0%,#f5f7ff 100%);box-shadow:0 16px 38px rgba(50,67,124,.07)}
.room-support-choice>header{display:grid;gap:7px}
.room-support-choice>header span{color:#7180d3;font-size:11px;font-weight:950;letter-spacing:.14em}
.room-support-choice>header h3{margin:0;color:#24314c;font-size:26px}
.room-support-choice>header p{margin:0;color:#7d899f;font-size:14px}
.room-support-choice-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:18px}
.room-support-choice-card{display:grid;grid-template-columns:62px minmax(0,1fr) auto;align-items:center;gap:16px;min-height:150px;padding:24px;border:1px solid #dbe2f1;border-radius:20px;background:#fff;color:#2d3a55;text-align:left;cursor:pointer;box-shadow:0 10px 24px rgba(48,65,120,.06);transition:transform .18s ease,border-color .18s ease,box-shadow .18s ease}
.room-support-choice-card:hover,.room-support-choice-card:focus-visible{transform:translateY(-2px);border-color:#8795e8;box-shadow:0 16px 32px rgba(68,84,168,.14);outline:none}
.room-support-choice-card.plan{background:linear-gradient(145deg,#fff,#eef2ff)}
.room-support-choice-card.operations{background:linear-gradient(145deg,#fff,#f5f7fb)}
.room-support-choice-icon{display:grid;width:58px;height:58px;place-items:center;border-radius:17px;background:linear-gradient(145deg,#e2e8ff,#cfd9ff);color:#4f63d4;font-size:22px;font-weight:950}
.room-support-choice-card.operations .room-support-choice-icon{background:linear-gradient(145deg,#e9f6f1,#d7ede5);color:#378066}
.room-support-choice-copy{display:grid;gap:7px;min-width:0}
.room-support-choice-copy strong{font-size:21px;color:#26334e}
.room-support-choice-copy small{color:#758197;font-size:13px;line-height:1.6}
.room-support-choice-card b{color:#5c6ed4;font-size:13px;white-space:nowrap}
.room-support-choice-card.operations b{color:#3d806a}
@media(max-width:760px){.room-support-header{align-items:flex-start;flex-direction:column}.room-support-header-actions{justify-content:flex-start}.room-support-choice{padding:20px}.room-support-choice-grid{grid-template-columns:1fr}.room-support-choice-card{grid-template-columns:54px minmax(0,1fr) auto;padding:20px}.room-support-choice-icon{width:50px;height:50px}}
</style>
