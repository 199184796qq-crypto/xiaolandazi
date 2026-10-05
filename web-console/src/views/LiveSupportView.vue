<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import {
  acceptLiveOpsSupportRequest,
  getLiveOpsSupportRequests,
  rejectLiveOpsSupportRequest,
} from '../api'
import type { LiveSupportRequest } from '../types'

const router = useRouter()

const requests = ref<LiveSupportRequest[]>([])
const loading = ref(false)
const busyRequestId = ref<number | null>(null)
const error = ref('')
const notice = ref('')

const activeRequests = computed(() =>
  requests.value
    .filter((item) => item.status === 'pending' || item.status === 'accepted')
    .sort((a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime()),
)

function statusLabel(status: string) {
  if (status === 'pending') return '待连接'
  if (status === 'accepted') return '已授权'
  if (status === 'rejected') return '已拒绝'
  if (status === 'cancelled') return '已取消'
  return status
}

function formatTime(value?: string) {
  if (!value) return ''
  const time = new Date(value)
  if (Number.isNaN(time.getTime())) return ''
  return time.toLocaleString('zh-CN', { hour12: false })
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const result = await getLiveOpsSupportRequests()
    requests.value = result.items || []
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取运维协助申请失败'
  } finally {
    loading.value = false
  }
}

async function connectRequest(item: LiveSupportRequest) {
  if (busyRequestId.value) return
  busyRequestId.value = item.id
  error.value = ''
  notice.value = ''
  try {
    let target = item
    if (item.status === 'pending') {
      target = await acceptLiveOpsSupportRequest(item.id, '运维人员已连接客户直播智能体工作台')
      await load()
      target = requests.value.find((candidate) => candidate.id === item.id) || target
    }
    if (target.status !== 'accepted') {
      error.value = '这条协助申请当前不能连接。'
      return
    }
    await router.push('/operations/live/rooms/' + target.room_id + '/strategy')
  } catch (err) {
    error.value = err instanceof Error ? err.message : '连接客户协助工作台失败'
  } finally {
    busyRequestId.value = null
  }
}

async function rejectRequest(item: LiveSupportRequest) {
  if (item.status !== 'pending' || busyRequestId.value) return
  if (!window.confirm('确定拒绝这条运维协助申请吗？')) return
  busyRequestId.value = item.id
  error.value = ''
  try {
    await rejectLiveOpsSupportRequest(item.id, '运维人员拒绝本次协助申请')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '拒绝协助申请失败'
  } finally {
    busyRequestId.value = null
  }
}

onMounted(() => void load())
</script>

<template>
  <div class="live-support-session-page">
    <section class="live-support-request-head">
        <div>
          <span>OPERATIONS SUPPORT</span>
          <h2>运维协助申请</h2>
          <p>客户提交当前直播间的协助申请后，接受申请即可进入该直播间的智能体方案界面。授权不扩展到客户的其它直播间。</p>
        </div>
        <button type="button" :disabled="loading" @click="load">{{ loading ? '刷新中…' : '刷新申请' }}</button>
    </section>

    <p v-if="error" class="live-support-message error">{{ error }}</p>
    <p v-if="notice" class="live-support-message success">{{ notice }}</p>

    <section class="live-support-request-list">
        <div v-if="!loading && !activeRequests.length" class="live-support-request-empty">
          <strong>当前没有待处理或已授权的协助申请</strong>
          <span>客户选择运维人员并提交授权后，会出现在这里。</span>
        </div>

        <article v-for="item in activeRequests" :key="item.id" class="live-support-request-card">
          <div class="live-support-request-avatar">客</div>
          <div class="live-support-request-main">
            <header>
              <div>
                <strong>客户 #{{ item.tenant_id }}</strong>
                <span>{{ item.room_name || ('直播间 #' + item.room_id) }}</span>
              </div>
              <em :class="item.status">{{ statusLabel(item.status) }}</em>
            </header>
            <p>
              仅维护此直播间已授权的智能体方案、方案内容以及直播间与方案的绑定关系。
            </p>
            <small>
              申请时间 {{ formatTime(item.requested_at) }}
              <template v-if="item.decided_at"> · 授权时间 {{ formatTime(item.decided_at) }}</template>
            </small>
          </div>
          <div class="live-support-request-actions">
            <button
              class="primary"
              type="button"
              :disabled="busyRequestId === item.id"
              @click="connectRequest(item)"
            >
              {{ busyRequestId === item.id ? '连接中…' : '连接' }}
            </button>
            <button
              v-if="item.status === 'pending'"
              type="button"
              :disabled="busyRequestId === item.id"
              @click="rejectRequest(item)"
            >拒绝</button>
          </div>
        </article>
    </section>
  </div>
</template>

<style scoped>
.live-support-session-page{display:grid;gap:18px;min-width:0}
.live-support-request-head,.live-support-session-bar{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;padding:20px 22px;border:1px solid #dfe5f1;border-radius:18px;background:linear-gradient(135deg,#fff,#f6f8ff);box-shadow:0 10px 28px rgba(49,64,117,.055)}
.live-support-request-head>div,.live-support-session-bar>div{display:grid;gap:5px;min-width:0}
.live-support-request-head span,.live-support-session-bar span{color:#7481ce;font-size:11px;font-weight:950;letter-spacing:.11em}
.live-support-request-head h2,.live-support-session-bar strong{margin:0;color:#24314c;font-size:24px;line-height:1.25}
.live-support-request-head p,.live-support-session-bar small{margin:0;color:#7f8aa0;font-size:13px;line-height:1.65}
.live-support-request-head>button,.live-support-session-bar>button{flex:0 0 auto;min-height:40px;padding:8px 14px;border:1px solid #d8dfed;border-radius:10px;background:#fff;color:#56627b;font:inherit;font-size:13px;font-weight:900;cursor:pointer}
.live-support-message{margin:0;padding:10px 13px;border-radius:10px;font-size:13px}
.live-support-message.error{background:#fff1f2;color:#b64f5a}
.live-support-message.success{background:#eef8f2;color:#2c7a52}
.live-support-request-list{display:grid;gap:12px}
.live-support-request-empty{display:grid;gap:7px;place-items:center;min-height:220px;padding:30px;border:1px dashed #d2d9e7;border-radius:18px;background:#fafbfe;text-align:center}
.live-support-request-empty strong{color:#546078;font-size:17px}
.live-support-request-empty span{color:#929caf;font-size:13px}
.live-support-request-card{display:grid;grid-template-columns:56px minmax(0,1fr) auto;gap:16px;align-items:center;padding:16px 18px;border:1px solid #dfe5f1;border-radius:16px;background:#fff;box-shadow:0 7px 20px rgba(48,63,109,.045)}
.live-support-request-avatar{display:grid;width:52px;height:52px;place-items:center;border-radius:15px;background:#edf1ff;color:#5a6bd2;font-size:21px;font-weight:950}
.live-support-request-main{display:grid;gap:7px;min-width:0}
.live-support-request-main header{display:flex;align-items:center;justify-content:space-between;gap:12px}
.live-support-request-main header>div{display:flex;align-items:baseline;gap:10px;min-width:0}
.live-support-request-main strong{color:#303d57;font-size:17px}
.live-support-request-main header span{color:#748097;font-size:13px}
.live-support-request-main em{padding:5px 9px;border-radius:999px;background:#f0f3f8;color:#788397;font-size:11px;font-style:normal;font-weight:900}
.live-support-request-main em.pending{background:#fff5d9;color:#a46c09}
.live-support-request-main em.accepted{background:#eaf8ef;color:#2d8057}
.live-support-request-main p{margin:0;color:#667289;font-size:13px;line-height:1.6}
.live-support-request-main small{color:#98a1b0;font-size:11px}
.live-support-request-actions{display:flex;gap:8px;align-items:center}
.live-support-request-actions button{min-width:74px;min-height:38px;padding:7px 12px;border:1px solid #d8dfec;border-radius:9px;background:#fff;color:#5a667d;font:inherit;font-size:12px;font-weight:900;cursor:pointer}
.live-support-request-actions button.primary{border-color:#5c6fd7;background:#5c6fd7;color:#fff}
.live-support-request-actions button:disabled{opacity:.5;cursor:default}
@media(max-width:860px){
  .live-support-request-card{grid-template-columns:48px minmax(0,1fr)}
  .live-support-request-actions{grid-column:2}
  .live-support-request-head,.live-support-session-bar{display:grid}
}
</style>
