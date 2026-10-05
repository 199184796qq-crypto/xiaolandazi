<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { adjustLiveOpsRoomQuota, getLiveOpsRoomQuotas } from '../api'

import PaginationBar from '../components/PaginationBar.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import { session } from '../session'
import type { LiveOpsRoomQuotaSummary } from '../types'
import { useFeedbackErrorRef } from '../uiFeedback'

const items = ref<LiveOpsRoomQuotaSummary[]>([])
const loading = ref(false)
const saving = ref(false)
const error = useFeedbackErrorRef()
const search = ref('')
const page = ref(1)
const pageSize = ref(20)
const editing = ref<LiveOpsRoomQuotaSummary | null>(null)
const nextLimit = ref(3)
const reason = ref('')

const canManage = computed(() => {
  const access = session.bootstrap?.staff_access
  if (session.bootstrap?.actor.role === 'platform_admin') return true
  return Boolean(
    access &&
      (access.is_super_admin ||
        access.permissions.includes('liveops.room_quota.manage')),
  )
})

const filtered = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  if (!keyword) return items.value
  return items.value.filter((item) =>
    [
      item.display_name,
      item.username,
      item.phone,
      item.parent_org_name,
      item.membership_name,
    ].some((value) => String(value || '').toLowerCase().includes(keyword)),
  )
})

const totalPages = computed(() =>
  Math.max(1, Math.ceil(filtered.value.length / pageSize.value)),
)

const pagedItems = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filtered.value.slice(start, start + pageSize.value)
})

function usagePercent(item: LiveOpsRoomQuotaSummary) {
  if (item.room_limit <= 0) return item.current_room_count > 0 ? 100 : 0
  return Math.min(100, Math.round((item.current_room_count / item.room_limit) * 100))
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await getLiveOpsRoomQuotas()
    items.value = data.items
    if (page.value > totalPages.value) page.value = totalPages.value
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取客户直播间配额失败'
  } finally {
    loading.value = false
  }
}

function openAdjust(item: LiveOpsRoomQuotaSummary) {
  editing.value = item
  nextLimit.value = item.room_limit
  reason.value = ''
}

function closeAdjust() {
  if (saving.value) return
  editing.value = null
}

async function submitAdjust() {
  if (!editing.value) return
  if (!Number.isInteger(nextLimit.value) || nextLimit.value < 0) {
    error.value = '客户直播间配额必须是大于或等于 0 的整数'
    return
  }
  if (nextLimit.value > 10) {
    error.value = '每个客户最多允许 10 个直播间'
    return
  }
  if (nextLimit.value > editing.value.membership_room_limit) {
    error.value = `该客户当前会员等级最多允许 ${editing.value.membership_room_limit} 个直播间`
    return
  }
  if (nextLimit.value < editing.value.current_room_count) {
    error.value = '新配额不能低于该客户当前已有直播间数量'
    return
  }
  if (!reason.value.trim()) {
    error.value = '请填写调整原因'
    return
  }
  saving.value = true
  error.value = ''
  try {
    const updated = await adjustLiveOpsRoomQuota(editing.value.tenant_id, {
      room_limit: nextLimit.value,
      reason: reason.value.trim(),
    })
    const index = items.value.findIndex(
      (item) => item.tenant_id === updated.tenant_id,
    )
    if (index >= 0) items.value[index] = updated
    editing.value = null
  } catch (value) {
    error.value = value instanceof Error ? value.message : '调整客户直播间配额失败'
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="room-quota-page">
    <ModulePageNav context="workspace-auto" active-title="客户直播间额度" />

    <section class="room-quota-hero">
      <div class="room-quota-hero-icon">额</div>
      <div>
        <span>ROOM CAPACITY</span>
        <h1>直播间数量管理</h1>
        <p>统一管理客户最多可创建的直播间数量。默认 3 个；直播运维可按业务调整，但不得超过当前会员等级上限，平台绝对上限 10 个。</p>
      </div>
      <button class="room-quota-refresh" type="button" :disabled="loading" @click="load">
        {{ loading ? '刷新中...' : '刷新数据' }}
      </button>
    </section>

    <section class="room-quota-card">
      <header>
        <div>
          <span>CUSTOMER ROOM QUOTAS</span>
          <h2>客户直播间配额</h2>
        </div>
        <input
          v-model="search"
          class="room-quota-search"
          type="search"
          placeholder="搜索客户名称、账号、手机号、所属代理或会员"
          @input="page = 1"
        />
      </header>

      <div class="room-quota-table-wrap">
        <table class="room-quota-table">
          <thead>
            <tr>
              <th>客户</th>
              <th>所属</th>
              <th>会员等级</th>
              <th>当前直播间</th>
              <th>当前配额</th>
              <th>剩余可开</th>
              <th>使用情况</th>
              <th>最近调整</th>
              <th v-if="canManage">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pagedItems" :key="item.tenant_id">
              <td>
                <div class="quota-terminal">
                  <strong>{{ item.display_name || item.username }}</strong>
                  <small>{{ item.username }}{{ item.phone ? ' · ' + item.phone : '' }}</small>
                </div>
              </td>
              <td>{{ item.parent_org_name || '平台直营' }}</td>
              <td>
                <div class="quota-membership">
                  <strong>{{ item.membership_name || '无会员' }}</strong>
                  <small>会员上限 {{ item.membership_room_limit }} 个</small>
                </div>
              </td>
              <td><b>{{ item.current_room_count }}</b> 个</td>
              <td><b>{{ item.room_limit }}</b> 个</td>
              <td>
                <span :class="['quota-remaining', { empty: item.remaining_slots === 0 }]">
                  {{ item.remaining_slots }} 个
                </span>
              </td>
              <td>
                <div class="quota-usage">
                  <span><i :style="{ width: usagePercent(item) + '%' }"></i></span>
                  <small>{{ usagePercent(item) }}%</small>
                </div>
              </td>
              <td>
                <div v-if="item.last_adjusted_at" class="quota-last-change">
                  <strong>{{ item.last_operator_name || '内部员工' }}</strong>
                  <small>{{ new Date(item.last_adjusted_at).toLocaleString('zh-CN') }}</small>
                  <small>{{ item.last_reason || '—' }}</small>
                </div>
                <span v-else class="quota-default-label">系统默认 3 个</span>
              </td>
              <td v-if="canManage">
                <button class="quota-adjust-button" type="button" @click="openAdjust(item)">调整配额</button>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="loading" class="room-quota-empty">正在读取客户直播间配额...</div>
        <div v-else-if="!pagedItems.length" class="room-quota-empty">没有匹配的客户。</div>
      </div>

      <PaginationBar
        :page="page"
        :total-pages="totalPages"
        :total="filtered.length"
        :page-size="pageSize"
        @update:page="page = $event"
      />
    </section>

    <div v-if="editing" class="quota-modal-backdrop" @click.self="closeAdjust">
      <section class="quota-modal">
        <header>
          <div><span>ADJUST CUSTOMER QUOTA</span><h3>调整客户直播间配额</h3></div>
          <button type="button" @click="closeAdjust">×</button>
        </header>
        <div class="quota-modal-body">
          <div class="quota-target">
            <strong>{{ editing.display_name || editing.username }}</strong>
            <span>当前已创建 {{ editing.current_room_count }} 个 · 当前配额 {{ editing.room_limit }} 个 · {{ editing.membership_name || '无会员' }}上限 {{ editing.membership_room_limit }} 个</span>
          </div>
          <label>
            <span>新的直播间配额 *</span>
            <input
              v-model.number="nextLimit"
              type="number"
              :min="editing.current_room_count"
              :max="Math.min(editing.membership_room_limit, 10)"
              step="1"
            />
            <small>不能低于当前已有 {{ editing.current_room_count }} 个，也不能超过当前会员等级上限 {{ editing.membership_room_limit }} 个；平台绝对上限为 10 个。</small>
          </label>
          <label>
            <span>调整原因 *</span>
            <textarea v-model="reason" rows="4" placeholder="例如：客户新增直播场景，经运维负责人确认调整配额"></textarea>
          </label>
        </div>
        <footer>
          <button type="button" class="quota-cancel" @click="closeAdjust">取消</button>
          <button type="button" class="quota-confirm" :disabled="saving" @click="submitAdjust">
            {{ saving ? '保存中...' : '确认调整' }}
          </button>
        </footer>
      </section>
    </div>
  </div>
</template>

<style scoped>
.room-quota-page{display:grid;gap:20px;padding-bottom:28px}.room-quota-hero,.room-quota-card,.room-quota-metrics{margin:0 24px;border:1px solid #e1e6ef;border-radius:22px;background:#fff;box-shadow:0 14px 36px rgba(47,57,89,.05)}.room-quota-hero{display:grid;grid-template-columns:74px minmax(0,1fr) auto;gap:20px;align-items:center;padding:28px 32px;background:linear-gradient(120deg,#fff 0%,#fbfbff 65%,#f0f2ff 100%)}.room-quota-hero-icon{display:grid;place-items:center;width:68px;height:68px;border-radius:19px;color:#fff;background:linear-gradient(145deg,#676cef,#5459cf);font-size:28px;font-weight:900;box-shadow:0 14px 28px rgba(86,91,207,.2)}.room-quota-hero span,.room-quota-card header span,.quota-modal header span{color:#929caf;font-size:11px;font-weight:800;letter-spacing:.14em}.room-quota-hero h1,.room-quota-card h2,.quota-modal h3{margin:5px 0}.room-quota-hero h1{font-size:28px}.room-quota-hero p{margin:0;color:#788397;line-height:1.7}.room-quota-refresh,.quota-adjust-button,.quota-confirm,.quota-cancel{min-height:40px;border-radius:10px;padding:0 15px;font:inherit;font-weight:800;cursor:pointer}.room-quota-refresh,.quota-cancel{border:1px solid #dfe4ed;color:#596478;background:#fff}.quota-adjust-button{border:1px solid #d9ddf8;color:#565bc8;background:#f5f5ff}.quota-confirm{border:0;color:#fff;background:#5d62d8}.room-quota-metrics{display:grid;grid-template-columns:repeat(4,1fr);overflow:hidden}.room-quota-metrics article{display:grid;gap:5px;padding:20px 26px}.room-quota-metrics article:not(:last-child){border-right:1px solid #e9ecf2}.room-quota-metrics span{color:#7e899b;font-size:12px;font-weight:700}.room-quota-metrics strong{font-size:28px}.room-quota-metrics small{color:#9aa3b2}.room-quota-card{padding:0 0 16px;overflow:hidden}.room-quota-card>header{display:flex;align-items:center;justify-content:space-between;gap:20px;padding:22px 26px;border-bottom:1px solid #e9ecf2}.room-quota-card h2{font-size:20px}.room-quota-search{width:min(430px,46vw);min-height:42px;border:1px solid #dfe4ec;border-radius:11px;padding:0 13px;font:inherit;color:#394457;background:#fbfcfe}.room-quota-table-wrap{overflow:auto}.room-quota-table{width:100%;border-collapse:collapse;min-width:1320px}.room-quota-table th{padding:13px 18px;text-align:left;color:#818b9d;background:#fafbfe;font-size:11px;font-weight:800}.room-quota-table td{padding:15px 18px;border-top:1px solid #edf0f5;color:#4c586c;font-size:12px;vertical-align:middle}.room-quota-table tbody tr{transition:.15s ease}.room-quota-table tbody tr:hover{background:#edf1f7}.quota-terminal,.quota-membership,.quota-last-change{display:grid;gap:3px}.quota-terminal strong,.quota-membership strong,.quota-last-change strong{color:#303b4d}.quota-terminal small,.quota-membership small,.quota-last-change small{color:#929bab}.quota-remaining{display:inline-flex;padding:5px 9px;border-radius:999px;color:#08795a;background:#e9faf3;font-weight:800}.quota-remaining.empty{color:#a96b00;background:#fff4db}.quota-usage{display:flex;align-items:center;gap:9px;min-width:145px}.quota-usage>span{width:100px;height:7px;border-radius:999px;background:#eceff5;overflow:hidden}.quota-usage i{display:block;height:100%;border-radius:inherit;background:#6268da}.quota-usage small{min-width:34px;color:#858fa1}.quota-default-label{color:#969faf}.room-quota-empty{padding:42px;text-align:center;color:#939cab}.quota-modal-backdrop{position:fixed;z-index:2600;inset:0;display:grid;place-items:center;padding:22px;background:rgba(29,35,52,.34);backdrop-filter:blur(2px)}.quota-modal{width:min(620px,100%);border-radius:20px;background:#fff;box-shadow:0 26px 75px rgba(28,35,55,.24);overflow:hidden}.quota-modal>header{display:flex;align-items:center;justify-content:space-between;padding:20px 24px;border-bottom:1px solid #e9ecf2}.quota-modal>header>button{border:0;background:transparent;color:#8993a4;font-size:26px;cursor:pointer}.quota-modal-body{display:grid;gap:17px;padding:22px 24px}.quota-target{display:grid;gap:4px;padding:14px;border-radius:12px;background:#f6f7fb}.quota-target span{color:#7f899b;font-size:12px}.quota-modal label{display:grid;gap:7px}.quota-modal label>span{color:#5e697c;font-size:12px;font-weight:800}.quota-modal input,.quota-modal textarea{box-sizing:border-box;width:100%;border:1px solid #dfe4ec;border-radius:10px;padding:10px 12px;font:inherit;color:#384457}.quota-modal input{min-height:44px}.quota-modal textarea{resize:vertical}.quota-modal label small{color:#929bab}.quota-modal>footer{display:flex;justify-content:flex-end;gap:10px;padding:17px 24px;border-top:1px solid #e9ecf2}@media(max-width:900px){.room-quota-hero{grid-template-columns:60px 1fr}.room-quota-hero-icon{width:56px;height:56px}.room-quota-refresh{grid-column:1/-1}.room-quota-metrics{grid-template-columns:repeat(2,1fr)}.room-quota-card>header{align-items:stretch;flex-direction:column}.room-quota-search{width:100%}}
</style>
