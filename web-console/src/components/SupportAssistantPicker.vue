<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import {
  createLiveRoomSupportRequest,
  getLiveRoomSupportAuthorizations,
  getLiveRoomSupportRequests,
  getLiveSupportStaff,
} from '../api'
import type {
  LiveSupportAuthorization,
  LiveSupportCapability,
  LiveSupportRequest,
  LiveSupportStaff,
} from '../types'

const props = defineProps<{ roomId: number | null }>()

const staff = ref<LiveSupportStaff[]>([])
const authorizations = ref<LiveSupportAuthorization[]>([])
const requests = ref<LiveSupportRequest[]>([])
const error = ref('')
const success = ref('')
const loading = ref(false)
const saving = ref(false)
const selectedStaffId = ref<number | null>(null)
const selectedCapabilities = ref<LiveSupportCapability[]>([])
const selectedIndex = ref(0)
const carousel = ref<HTMLElement | null>(null)

const capabilityOptions: Array<{ code: LiveSupportCapability; label: string }> = [
  { code: 'l3_policy', label: '用户层策略' },
  { code: 'anchor_training', label: '主播训练' },
  { code: 'voice_clone', label: '声音复刻' },
]

const selectedStaff = computed(
  () => staff.value.find((item) => item.user_id === selectedStaffId.value) || null,
)

const selectedRequest = computed(() =>
  requests.value
    .filter((item) => item.staff_user_id === selectedStaffId.value)
    .sort((a, b) => new Date(b.requested_at).getTime() - new Date(a.requested_at).getTime())[0] || null,
)

const activeCapabilities = computed(() =>
  selectedStaffId.value ? currentCapabilities(selectedStaffId.value) : [],
)

function capabilityLabel(code: string) {
  return capabilityOptions.find((item) => item.code === code)?.label || code
}

function initials(item: LiveSupportStaff) {
  return (item.display_name || item.username || '协').slice(0, 1)
}

function allowed(item: LiveSupportStaff | null, capability: LiveSupportCapability) {
  return Boolean(item?.allowed_capabilities?.includes(capability))
}

function currentCapabilities(staffUserId: number) {
  return authorizations.value
    .filter((item) => item.staff_user_id === staffUserId && item.status === 'active')
    .map((item) => item.capability as LiveSupportCapability)
}

function chooseStaff(item: LiveSupportStaff, index = staff.value.findIndex((candidate) => candidate.user_id === item.user_id)) {
  selectedStaffId.value = item.user_id
  selectedIndex.value = Math.max(0, index)
  const authorized = currentCapabilities(item.user_id).filter((capability) => allowed(item, capability))
  const pending = requests.value.find(
    (request) => request.staff_user_id === item.user_id && request.status === 'pending',
  )?.capabilities || []
  selectedCapabilities.value = (pending.length ? pending : authorized).filter((capability) =>
    allowed(item, capability),
  )
}

function toggleCapability(capability: LiveSupportCapability) {
  if (!allowed(selectedStaff.value, capability)) return
  const next = new Set(selectedCapabilities.value)
  if (next.has(capability)) next.delete(capability)
  else next.add(capability)
  selectedCapabilities.value = [...next]
}

async function load() {
  if (!props.roomId) {
    staff.value = []
    authorizations.value = []
    requests.value = []
    return
  }
  loading.value = true
  error.value = ''
  try {
    const [staffResponse, authorizationResponse, requestResponse] = await Promise.all([
      getLiveSupportStaff(),
      getLiveRoomSupportAuthorizations(props.roomId),
      getLiveRoomSupportRequests(props.roomId),
    ])
    staff.value = staffResponse.items || []
    authorizations.value = authorizationResponse.items || []
    requests.value = requestResponse.items || []
    if (staff.value.length) {
      const previousIndex = Math.max(0, staff.value.findIndex((item) => item.user_id === selectedStaffId.value))
      chooseStaff(staff.value[previousIndex] || staff.value[0], previousIndex)
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取协助员失败'
  } finally {
    loading.value = false
  }
}

function scrollToStaff(index: number) {
  if (!staff.value.length) return
  const next = Math.max(0, Math.min(index, staff.value.length - 1))
  chooseStaff(staff.value[next], next)
  nextTick(() => {
    const child = carousel.value?.children.item(next) as HTMLElement | null
    child?.scrollIntoView({ behavior: 'smooth', inline: 'center', block: 'nearest' })
  })
}

function handleCarouselScroll() {
  const element = carousel.value
  if (!element || !staff.value.length) return
  const width = element.clientWidth || 1
  const index = Math.max(0, Math.min(Math.round(element.scrollLeft / width), staff.value.length - 1))
  if (index !== selectedIndex.value) chooseStaff(staff.value[index], index)
}

function requestStatusLabel(value: string) {
  if (value === 'pending') return '申请中'
  if (value === 'accepted') return '已接受'
  if (value === 'rejected') return '已拒绝'
  if (value === 'cancelled') return '已取消'
  return value
}

async function submitRequest() {
  const roomId = props.roomId
  const employee = selectedStaff.value
  if (!roomId || !employee || saving.value) return
  if (!selectedCapabilities.value.length) {
    error.value = '请至少选择一项协助权限'
    return
  }
  saving.value = true
  error.value = ''
  success.value = ''
  try {
    await createLiveRoomSupportRequest(roomId, employee.user_id, selectedCapabilities.value)
    success.value = '协助申请已发送给 ' + (employee.display_name || employee.username)
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '提交协助申请失败'
  } finally {
    saving.value = false
  }
}

watch(() => props.roomId, () => void load())
onMounted(() => void load())
</script>

<template>
  <section class="support-picker">
    <div class="support-picker-head">
      <div>
        <span class="section-kicker">OPERATIONS SUPPORT</span>
        <strong>运维协助</strong>
      </div>
      <span v-if="selectedRequest" class="support-request-state" :class="selectedRequest.status">
        {{ requestStatusLabel(selectedRequest.status) }}
      </span>
    </div>

    <p v-if="error" class="inline-error">{{ error }}</p>
    <p v-if="success" class="support-success">{{ success }}</p>

    <div v-if="staff.length" class="support-carousel-shell">
      <button type="button" class="support-carousel-arrow" :disabled="selectedIndex <= 0" aria-label="上一位协助员" @click="scrollToStaff(selectedIndex - 1)">‹</button>
      <div ref="carousel" class="support-carousel" @scroll.passive="handleCarouselScroll">
        <article
          v-for="(item, index) in staff"
          :key="item.user_id"
          class="support-staff-card"
          :class="{ active: selectedStaffId === item.user_id }"
          @click="chooseStaff(item, index)"
        >
          <div class="support-avatar">
            <img v-if="item.avatar_url" :src="item.avatar_url" alt="" />
            <span v-else>{{ initials(item) }}</span>
          </div>
          <strong class="support-staff-name">{{ item.display_name || item.username }}</strong>
        </article>
      </div>
      <button type="button" class="support-carousel-arrow" :disabled="selectedIndex >= staff.length - 1" aria-label="下一位协助员" @click="scrollToStaff(selectedIndex + 1)">›</button>
    </div>

    <p v-else class="support-picker-empty">{{ roomId ? '暂无可申请的运维协助人员' : '请先选择直播间' }}</p>

    <div v-if="staff.length" class="support-carousel-dots" aria-label="协助员位置">
      <button
        v-for="(item, index) in staff"
        :key="item.user_id"
        type="button"
        :class="{ active: index === selectedIndex }"
        :aria-label="'查看 ' + (item.display_name || item.username)"
        @click="scrollToStaff(index)"
      ></button>
    </div>

    <section v-if="selectedStaff" class="support-scope-panel">
      <div class="support-scope-title">
        <div>
          <strong>授权范围</strong>
        </div>
        <span>{{ selectedCapabilities.length }}/{{ capabilityOptions.length }} 已选择</span>
      </div>
      <div class="support-scope-options">
        <label
          v-for="option in capabilityOptions"
          :key="option.code"
          class="support-scope-option"
          :class="{
            disabled: !allowed(selectedStaff, option.code),
            selected: selectedCapabilities.includes(option.code),
          }"
        >
          <input
            type="checkbox"
            :checked="selectedCapabilities.includes(option.code)"
            :disabled="!allowed(selectedStaff, option.code)"
            @change="toggleCapability(option.code)"
          />
          <span class="support-scope-checkmark" aria-hidden="true">✓</span>
          <strong>{{ option.label }}</strong>
        </label>
      </div>
    </section>

    <div v-if="activeCapabilities.length" class="support-active-authority">
      <span>当前已授权</span>
      <strong>{{ activeCapabilities.map(capabilityLabel).join(' · ') }}</strong>
    </div>

    <button
      v-if="staff.length"
      type="button"
      class="support-apply-button"
      :disabled="!roomId || !selectedStaff || !selectedCapabilities.length || saving || selectedRequest?.status === 'pending'"
      @click="submitRequest"
    >
      {{ saving ? '申请提交中…' : selectedRequest?.status === 'pending' ? '申请已提交' : '申请协助' }}
    </button>
  </section>
</template>

<style scoped>
.support-picker{display:grid;gap:18px;min-width:0}
.support-picker-head{display:flex;align-items:center;justify-content:space-between;gap:12px}
.support-picker-head>div{display:grid;gap:4px}
.support-picker-head strong{font-size:24px;color:#1d2940;line-height:1.2}
.support-request-state{padding:5px 10px;border-radius:999px;background:#f1f4fa;color:#66758e;font-size:12px;font-weight:800}
.support-request-state.pending{background:#fff5d9;color:#a66b00}
.support-request-state.accepted{background:#e8f8ef;color:#278253}
.support-request-state.rejected{background:#fff0f1;color:#b74a55}
.support-success{margin:0;padding:8px 10px;border-radius:10px;background:#eef9f3;color:#26784d;font-size:13px}
.support-carousel-shell{display:grid;grid-template-columns:40px minmax(0,1fr) 40px;align-items:center;gap:10px;min-width:0}
.support-carousel{display:flex;overflow-x:auto;scroll-snap-type:x mandatory;scrollbar-width:none;gap:12px;padding:6px;overscroll-behavior-x:contain;min-width:0}
.support-carousel::-webkit-scrollbar{display:none}
.support-staff-card{flex:0 0 100%;scroll-snap-align:center;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:12px;min-height:186px;padding:20px 18px;border:1px solid #dfe6f3;border-radius:22px;background:linear-gradient(180deg,#fff 0%,#fbfcff 100%);cursor:pointer;transition:.18s ease;box-sizing:border-box;overflow:hidden}
.support-staff-card.active{border-color:#7483ea;box-shadow:0 0 0 3px #6374ec14,0 14px 30px #4e63bb16;transform:translateY(-1px)}
.support-carousel-arrow{width:40px;height:48px;border:1px solid #d8e0ef;border-radius:14px;background:#fff;color:#5567ce;font-size:26px;cursor:pointer;box-shadow:0 6px 18px #5263a50c}
.support-carousel-arrow:disabled{opacity:.35;cursor:default}
.support-avatar{width:96px;height:96px;border-radius:26px;display:grid;place-items:center;overflow:hidden;background:linear-gradient(135deg,#e8ecff,#dbe6ff);color:#4c5fd4;font-size:36px;font-weight:900}
.support-avatar img{width:100%;height:100%;object-fit:cover}
.support-staff-name{max-width:100%;font-size:21px;color:#1e2b43;line-height:1.25;text-align:center;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.support-carousel-dots{display:flex;justify-content:center;gap:6px}
.support-carousel-dots button{width:6px;height:6px;padding:0;border:0;border-radius:999px;background:#cbd3e4;cursor:pointer}
.support-carousel-dots button.active{width:20px;background:#6675dd}
.support-scope-panel{display:grid;gap:16px;padding:18px;border-radius:18px;background:#fff;border:1px solid #e1e7f3}
.support-scope-title{display:flex;align-items:center;justify-content:space-between;gap:14px}
.support-scope-title>div{display:grid;gap:3px;min-width:0}
.support-scope-title strong{font-size:19px;color:#202b42;line-height:1.2}
.support-scope-title small{color:#8b96a8;font-size:13px}
.support-scope-title>span{flex:0 0 auto;padding:5px 9px;border-radius:999px;background:#f0f3ff;color:#6572c9;font-size:12px;font-weight:800}
.support-scope-options{display:grid;grid-template-columns:1fr;gap:10px;min-width:0}
.support-scope-option{position:relative;display:flex;align-items:center;gap:12px;min-width:0;min-height:58px;padding:12px 14px;border:1px solid #e3e8f4;border-radius:14px;background:#f8faff;color:#3d4a62;cursor:pointer;box-sizing:border-box;transition:.16s ease}
.support-scope-option:hover{border-color:#b9c4f4;background:#fbfcff}
.support-scope-option.selected{border-color:#7b88e8;background:#f1f3ff;box-shadow:0 0 0 2px #6f7be80f}
.support-scope-option.disabled{opacity:.42;cursor:not-allowed}
.support-scope-option input{position:absolute;opacity:0;pointer-events:none}
.support-scope-checkmark{width:24px;height:24px;flex:0 0 24px;border:1.5px solid #c8d0df;border-radius:8px;display:grid;place-items:center;background:#fff;color:transparent;font-size:15px;font-weight:900;transition:.16s ease}
.support-scope-option.selected .support-scope-checkmark{border-color:#6676de;background:#6676de;color:#fff}
.support-scope-option strong{min-width:0;font-size:16px;line-height:1.25;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.support-active-authority{display:flex;align-items:center;justify-content:center;gap:8px;color:#758196;font-size:12px}
.support-active-authority strong{color:#5363be}
.support-apply-button{justify-self:stretch;min-height:48px;border:0;border-radius:14px;background:#5968d8;color:#fff;font-size:15px;font-weight:900;cursor:pointer;box-shadow:0 10px 24px #5367ca2b}
.support-apply-button:disabled{opacity:.55;cursor:not-allowed;box-shadow:none}
.support-picker-empty{margin:0;color:#8a95a8;font-size:13px;text-align:center}
@media(max-width:720px){
  .support-carousel{padding:4px}
  .support-staff-card{min-height:164px;gap:10px;padding:16px 14px}
  .support-avatar{width:82px;height:82px;border-radius:22px;font-size:31px}
  .support-staff-name{font-size:19px}
  .support-carousel-shell{grid-template-columns:32px minmax(0,1fr) 32px;gap:6px}
  .support-carousel-arrow{width:32px;height:44px}
  .support-scope-title{align-items:flex-start}
}
</style>
