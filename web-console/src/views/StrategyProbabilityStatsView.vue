<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'

import { getRooms, getRoomStrategyStats, updateRoomStrategyWeight } from '../api'
import type {
  Room,
  RoomInteractionWindowStat,
  RoomStrategyProbabilityStat,
  RoomStrategyStageStats,
} from '../types'

const rooms = ref<Room[]>([])
const selectedRoomId = ref<number | null>(null)
const stats = ref<RoomStrategyStageStats | null>(null)
const loadingRooms = ref(false)
const loadingStats = ref(false)
const error = ref('')
const streamState = ref<'connecting' | 'online' | 'reconnecting' | 'offline'>('offline')
const savingWeightKey = ref('')
const weightNotice = ref('')
const weightError = ref('')
const localWeights = ref<Record<string, number>>({})
let statsSource: EventSource | null = null

const selectedRoom = computed(
  () => rooms.value.find((room) => room.id === selectedRoomId.value) || null,
)

type DisplayStrategyStat = RoomStrategyProbabilityStat & {
  enabled: boolean
  configured_probability: number
}

const categoryOrder = ['interrupt', 'resume', 'addressing']
const allStrategyItems = computed<DisplayStrategyStat[]>(() => {
  return (stats.value?.items || []).map((item) => ({
    ...item,
    enabled: item.enabled !== false,
    configured_probability: Number(item.configured_probability || 0),
  }))
})

const groupedStats = computed(() => categoryOrder.map((category) => ({
  category,
  label: categoryLabel(category),
  items: allStrategyItems.value.filter(
    (item) => String(item.category || '').toLowerCase() === category,
  ),
})))

const interactionItems = computed<RoomInteractionWindowStat[]>(() => stats.value?.interaction_items || [])

function weightIdentity(category: string, key: string) {
  return String(category || '').toLowerCase() + ':' + String(key || '')
}

function syncLocalWeights(next: RoomStrategyStageStats | null) {
  if (!next || savingWeightKey.value) return
  const values: Record<string, number> = {}
  for (const item of next.items || []) {
    values[weightIdentity(item.category, item.key)] = Number(item.configured_probability || 0)
  }
  for (const item of next.interaction_items || []) {
    values[weightIdentity('interaction', item.key)] = Number(item.configured_weight || 0)
  }
  localWeights.value = values
}

function applyStats(next: RoomStrategyStageStats) {
  stats.value = next
  syncLocalWeights(next)
}

function weightValue(category: string, key: string, fallback: number) {
  const identity = weightIdentity(category, key)
  const value = localWeights.value[identity]
  return Number.isFinite(value) ? value : Number(fallback || 0)
}

function minimumWeight(category: string, key: string) {
  const normalized = String(category || '').toLowerCase()
  if (normalized === 'interrupt') return 10
  if (normalized === 'resume') return 20
  if (normalized === 'interaction') return key === 'reply_chat' ? 20 : 5
  return 0
}

function updateLocalWeight(category: string, key: string, event: Event) {
  const input = event.target as HTMLInputElement | null
  if (!input) return
  localWeights.value = {
    ...localWeights.value,
    [weightIdentity(category, key)]: Number(input.value || 0),
  }
}

async function saveWeight(category: string, key: string) {
  const roomId = selectedRoomId.value
  if (!roomId || savingWeightKey.value) return
  const identity = weightIdentity(category, key)
  const value = Math.max(0, Math.min(100, Math.round(Number(localWeights.value[identity] || 0))))
  savingWeightKey.value = identity
  weightNotice.value = ''
  weightError.value = ''
  try {
    await updateRoomStrategyWeight(roomId, { category, key, value })
    await refreshStats(true)
    weightNotice.value = category === 'interaction'
      ? '互动倾向已热更新到 Core；最长等待保底不变。'
      : '策略权重已热更新到 Core；归一化后的真实比例会在卡片中实时刷新。'
  } catch (err) {
    weightError.value = err instanceof Error ? err.message : '策略权重保存失败'
    await refreshStats(true)
  } finally {
    savingWeightKey.value = ''
  }
}

function categoryLabel(category: string) {
  switch (String(category || '').toLowerCase()) {
    case 'interrupt':
      return '打断策略'
    case 'resume':
      return '回归策略'
    case 'addressing':
      return '称呼策略'
    default:
      return category || '其它策略'
  }
}

function probability(value: number) {
  const number = Number(value || 0)
  const rounded = Math.round(number * 100) / 100
  return rounded.toFixed(Number.isInteger(rounded) ? 0 : 2) + '%'
}

function formatTime(value?: string) {
  if (!value) return '—'
  if (new Date(value).getUTCFullYear() <= 1) return '—'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return '—'
  return parsed.toLocaleString()
}

function interactionStateLabel(state: string) {
  switch (String(state || '').toLowerCase()) {
    case 'pending':
      return '已有事件，等待触发'
    case 'cooldown':
      return '冷却中'
    default:
      return '等待事件'
  }
}

function durationLabel(value: number) {
  const seconds = Math.max(0, Math.round(Number(value || 0)))
  if (seconds < 60) return seconds + ' 秒'
  if (seconds % 60 === 0) return seconds / 60 + ' 分钟'
  return Math.floor(seconds / 60) + '分' + (seconds % 60) + '秒'
}

async function refreshStats(silent = false) {
  const roomId = selectedRoomId.value
  if (!roomId) {
    stats.value = null
    return
  }
  if (!silent) loadingStats.value = true
  try {
    applyStats(await getRoomStrategyStats(roomId))
    error.value = ''
  } catch (err) {
    if (!silent) {
      error.value = err instanceof Error ? err.message : '读取策略真实概率失败'
    }
  } finally {
    if (!silent) loadingStats.value = false
  }
}

function stopRealtime() {
  const source = statsSource
  statsSource = null
  source?.close()
  streamState.value = 'offline'
}

function connectRealtime() {
  stopRealtime()
  const roomId = selectedRoomId.value
  if (!roomId) return

  streamState.value = 'connecting'
  const source = new EventSource(
    '/api/v1/rooms/' + roomId + '/strategy-stats/stream',
    { withCredentials: true },
  )
  statsSource = source

  source.onopen = () => {
    if (statsSource !== source) return
    streamState.value = 'online'
    error.value = ''
  }
  source.addEventListener('stats', (event) => {
    if (statsSource !== source) return
    try {
      applyStats(JSON.parse((event as MessageEvent).data) as RoomStrategyStageStats)
      streamState.value = 'online'
      error.value = ''
    } catch {
      error.value = '实时统计数据格式异常'
    }
  })
  source.onerror = () => {
    if (statsSource !== source) return
    streamState.value = 'reconnecting'
  }
}

async function loadRooms() {
  loadingRooms.value = true
  try {
    const response = await getRooms()
    rooms.value = response.items || []
    const remembered = Number(window.localStorage.getItem('system-agent-live-room-id') || 0)
    if (remembered && rooms.value.some((room) => room.id === remembered)) {
      selectedRoomId.value = remembered
    } else {
      selectedRoomId.value = rooms.value[0]?.id || null
    }
    await refreshStats()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取直播间失败'
  } finally {
    loadingRooms.value = false
  }
}

watch(selectedRoomId, (roomId) => {
  if (roomId) {
    window.localStorage.setItem('system-agent-live-room-id', String(roomId))
  }
  void refreshStats()
  connectRealtime()
})

onMounted(() => {
  void loadRooms()
})

onBeforeUnmount(() => {
  stopRealtime()
})
</script>

<template>
  <main class="probability-page">
    <header class="probability-head">
      <div>
        <span>INTERNAL RUNTIME VIEW</span>
        <h1>策略运行监控</h1>
        <p>这里可以边监控边拉动测试权重。互动滑杆调整任务优先倾向，保留时间窗与最长等待；打断、回归、称呼会热更新到 Core 并显示真实运行变化。</p>
      </div>
      <RouterLink to="/operations/live/strategy">返回直播策略</RouterLink>
    </header>

    <section class="probability-toolbar">
      <label>
        <span>直播间</span>
        <select v-model="selectedRoomId" :disabled="loadingRooms || !rooms.length">
          <option v-if="!rooms.length" :value="null">
            {{ loadingRooms ? '正在读取直播间…' : '暂无直播间' }}
          </option>
          <option v-for="room in rooms" :key="room.id" :value="room.id">
            {{ room.name }} · {{ room.status }}
          </option>
        </select>
      </label>
      <div class="probability-toolbar-room">
        <strong>{{ selectedRoom?.name || '未选择直播间' }}</strong>
        <span v-if="selectedRoom">房间 ID {{ selectedRoom.id }} · {{ selectedRoom.platform }}</span>
      </div>
      <div class="probability-live-state" :class="streamState">
        <i></i>
        <span>
          {{
            streamState === 'online'
              ? '实时推送已连接'
              : streamState === 'connecting'
                ? '正在连接实时统计'
                : streamState === 'reconnecting'
                  ? '实时连接重连中'
                  : '实时统计未连接'
          }}
        </span>
      </div>
      <button type="button" :disabled="loadingStats || !selectedRoomId" @click="refreshStats()">
        {{ loadingStats ? '刷新中…' : '立即刷新' }}
      </button>
    </section>

    <div v-if="error" class="probability-error">{{ error }}</div>
    <div v-if="weightError" class="probability-error">{{ weightError }}</div>
    <div v-if="weightNotice" class="probability-notice">{{ weightNotice }}</div>

    <section v-if="stats?.stage_id" class="probability-stage">
      <div>
        <span>阶段开始</span>
        <strong>{{ formatTime(stats.started_at) }}</strong>
      </div>
      <div>
        <span>最近更新</span>
        <strong>{{ formatTime(stats.updated_at) }}</strong>
      </div>
      <div>
        <span>概率决策次数</span>
        <strong>{{ stats.decision_count }}</strong>
      </div>
      <div>
        <span>统计方式</span>
        <strong>{{ streamState === 'online' ? '实时推送' : '等待实时连接' }}</strong>
      </div>
    </section>

    <section v-if="interactionItems.length" class="probability-category interaction-category">
      <header class="probability-category-head">
        <div>
          <span>互动策略</span>
          <strong>{{ interactionItems.length }} 个时间窗</strong>
        </div>
        <p>不再按单事件抽概率；有事件后按聚合、冷却和最长等待形成口播任务。</p>
      </header>
      <div class="interaction-grid">
        <article
          v-for="item in interactionItems"
          :key="item.key"
          :class="'state-' + item.state"
        >
          <header>
            <div>
              <span>{{ interactionStateLabel(item.state) }}</span>
              <h2>{{ item.name || item.key }}</h2>
            </div>
            <div class="interaction-pending">
              <small>待处理事件</small>
              <strong>{{ item.pending_count }}</strong>
            </div>
          </header>
          <dl>
          <div class="weight-tuner" :class="{ saving: savingWeightKey === weightIdentity('interaction', item.key) }">
            <div>
              <span>互动倾向</span>
              <b>{{ weightValue('interaction', item.key, item.configured_weight) }}</b>
              <small>实时 {{ item.effective_weight }} · 队列优先 {{ item.effective_priority }}</small>
            </div>
            <input
              type="range"
              :min="minimumWeight('interaction', item.key)"
              max="100"
              step="1"
              :value="weightValue('interaction', item.key, item.configured_weight)"
              :disabled="item.state === 'disabled' || Boolean(savingWeightKey)"
              @input="updateLocalWeight('interaction', item.key, $event)"
              @change="saveWeight('interaction', item.key)"
            />
          </div>
            <div>
              <dt>本场累计事件</dt>
              <dd>{{ item.total_events }}</dd>
            </div>
            <div>
              <dt>已形成口播</dt>
              <dd>{{ item.emitted_count }}</dd>
            </div>
            <div>
              <dt>最长等待</dt>
              <dd>{{ durationLabel(item.max_wait_seconds) }}</dd>
            </div>
            <div>
              <dt>最小间隔</dt>
              <dd>{{ durationLabel(item.min_interval_seconds) }}</dd>
            </div>
            <div>
              <dt>上次口播聚合</dt>
              <dd>{{ item.last_mission_event_count || '—' }}</dd>
            </div>
            <div>
              <dt>下一触发时间</dt>
              <dd>{{ formatTime(item.next_due_at) }}</dd>
            </div>
            <div>
              <dt>最近事件价值</dt>
              <dd>
                {{
                  Number(item.last_event_value || 0) > 0
                    ? Number(item.last_event_value || 0).toFixed(1) + ' · ' + (item.last_value_level || '—')
                    : '—'
                }}
              </dd>
            </div>
            <div>
              <dt>互动预算</dt>
              <dd>
                {{
                  item.last_budget_level
                    ? item.last_budget_level + ' · ' + (item.last_budget_allowed ? '已放行' : '继续聚合')
                    : '—'
                }}
              </dd>
            </div>
          </dl>
          <footer>
            <span>上次形成口播：{{ formatTime(item.last_emitted_at) }}</span>
            <small v-if="item.last_decision_reason">{{ item.last_decision_reason }}</small>
          </footer>
        </article>
      </div>
    </section>

    <section v-if="allStrategyItems.length" class="probability-categories">
      <section v-for="group in groupedStats" :key="group.category" class="probability-category">
        <header class="probability-category-head">
          <div>
            <span>{{ group.label }}</span>
            <strong>{{ group.items.length }} 个策略</strong>
          </div>
        </header>
        <div class="probability-grid">
          <article
            v-for="item in group.items"
            :key="item.category + ':' + item.key"
            :class="{ 'is-unseen': item.samples === 0, 'is-disabled': !item.enabled }"
          >
            <header>
              <div>
                <span>
                  {{ item.enabled ? (item.samples > 0 ? '本阶段已评估' : '本阶段未评估') : '已停用' }}
                </span>
                <h2>{{ item.name || item.key }}</h2>
              </div>
              <div class="probability-current">
                <small>最后真实概率</small>
                <strong>{{ item.samples > 0 ? probability(item.last_probability) : '—' }}</strong>
              </div>
            </header>
            <dl>
            <div class="weight-tuner" :class="{ saving: savingWeightKey === weightIdentity(group.category, item.key) }">
              <div>
                <span>{{ group.category === 'addressing' ? '称呼权重' : '测试权重' }}</span>
                <b>{{ weightValue(group.category, item.key, item.configured_probability) }}</b>
                <small>{{ item.samples > 0 ? '实时 ' + probability(item.last_probability) : '等待下一次决策' }}</small>
              </div>
              <input
                type="range"
                :min="minimumWeight(group.category, item.key)"
                max="100"
                step="1"
                :value="weightValue(group.category, item.key, item.configured_probability)"
                :disabled="!item.enabled || Boolean(savingWeightKey)"
                @input="updateLocalWeight(group.category, item.key, $event)"
                @change="saveWeight(group.category, item.key)"
              />
            </div>
              <div>
                <dt>配置概率</dt>
                <dd>{{ probability(item.configured_probability) }}</dd>
              </div>
              <div>
                <dt>阶段平均</dt>
                <dd>{{ item.samples > 0 ? probability(item.average_probability) : '—' }}</dd>
              </div>
              <div>
                <dt>最低 / 最高</dt>
                <dd>
                  {{
                    item.samples > 0
                      ? probability(item.minimum_probability) + ' / ' + probability(item.maximum_probability)
                      : '—'
                  }}
                </dd>
              </div>
              <div>
                <dt>评估 / 命中</dt>
                <dd>{{ item.samples }} / {{ item.hit_count }}</dd>
              </div>
              <template v-if="group.category === 'resume'">
                <div>
                  <dt>有资格 / 已选</dt>
                  <dd>{{ Number(item.eligible_count || 0) }} / {{ Number(item.selected_count || 0) }}</dd>
                </div>
                <div>
                  <dt>连续未选 / 覆盖欠账</dt>
                  <dd>{{ Number(item.consecutive_miss || 0) }} / {{ Number(item.coverage_debt || 0) }}</dd>
                </div>
                <div>
                  <dt>有效权重</dt>
                  <dd>{{ Number(item.effective_weight || 0) || '—' }}</dd>
                </div>
                <div>
                  <dt>重复惩罚 / 多样性</dt>
                  <dd>
                    {{
                      item.samples > 0
                        ? Number(item.repeat_penalty || 1).toFixed(2) + ' / ' + Number(item.diversity_boost || 1).toFixed(2)
                        : '—'
                    }}
                  </dd>
                </div>
              </template>
            </dl>
            <footer>
              {{
                item.samples > 0
                  ? '最后评估：' + formatTime(item.last_evaluated_at)
                    + (group.category === 'resume' && item.last_selected_at ? ' · 上次选中：' + formatTime(item.last_selected_at) : '')
                  : '等待本阶段首次参与决策'
              }}
            </footer>
          </article>
        </div>
      </section>
    </section>

    <section v-if="!interactionItems.length && !allStrategyItems.length && !loadingStats && !error" class="probability-empty">
      <strong>当前阶段还没有策略统计</strong>
      <span>直播间进入运行后，这里会显示互动时间窗和其它策略的实时运行数据。</span>
    </section>
  </main>
</template>

<style scoped>
.probability-page{display:grid;align-content:start;gap:18px;padding:32px;background:linear-gradient(180deg,#f7f9ff,#f2f5fb);color:#35415a}
.probability-head{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;padding:24px 26px;border:1px solid #e0e6f1;border-radius:20px;background:#fff;box-shadow:0 12px 32px rgba(53,67,111,.06)}
.probability-head>div{display:grid;gap:6px}.probability-head span{color:#7e89a0;font-size:11px;font-weight:900;letter-spacing:.12em}.probability-head h1{margin:0;font-size:28px}.probability-head p{margin:0;color:#7d889c;font-size:14px;line-height:1.6}.probability-head>a{padding:9px 13px;border:1px solid #d9e0ef;border-radius:10px;background:#f7f9ff;color:#5767c7;font-weight:850;text-decoration:none;white-space:nowrap}
.probability-toolbar{display:grid;grid-template-columns:minmax(260px,360px) minmax(0,1fr) auto auto;align-items:end;gap:14px;padding:18px 20px;border:1px solid #e0e6f1;border-radius:16px;background:#fff}
.probability-toolbar label{display:grid;gap:7px}.probability-toolbar label>span{color:#77839a;font-size:12px;font-weight:850}.probability-toolbar select{width:100%;min-height:42px;padding:0 12px;border:1px solid #d9e0eb;border-radius:10px;background:#fff;color:#35415a;font:inherit}
.probability-toolbar-room{display:grid;gap:4px}.probability-toolbar-room strong{font-size:16px}.probability-toolbar-room span{color:#9099aa;font-size:12px}.probability-toolbar button{min-height:42px;padding:0 16px;border:0;border-radius:10px;background:#5869d8;color:#fff;font:inherit;font-weight:900;cursor:pointer}.probability-toolbar button:disabled{opacity:.55;cursor:not-allowed}
.probability-live-state{display:flex;align-items:center;gap:7px;min-height:42px;padding:0 12px;border:1px solid #e0e5ee;border-radius:10px;background:#f8f9fc;color:#7e899e;font-size:11px;font-weight:850;white-space:nowrap}.probability-live-state i{width:8px;height:8px;border-radius:50%;background:#a0a8b8}.probability-live-state.online{border-color:#cfe8dc;background:#f2fbf6;color:#398060}.probability-live-state.online i{background:#48a778;box-shadow:0 0 0 4px rgba(72,167,120,.12)}.probability-live-state.connecting,.probability-live-state.reconnecting{color:#6573bd}.probability-live-state.connecting i,.probability-live-state.reconnecting i{background:#6e7ddd}
.probability-error,.probability-empty,.probability-notice{padding:18px 20px;border-radius:14px;background:#fff;color:#8c96a7}.probability-error{background:#fff0f1;color:#b74b56}.probability-notice{border:1px solid #cae8d9;background:#f1fbf5;color:#397b5c;font-size:12px;font-weight:800}.probability-empty{display:grid;gap:5px;text-align:center}.probability-empty strong{color:#536078}.probability-empty span{font-size:13px}
.probability-stage{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px}.probability-stage>div{display:grid;gap:5px;padding:15px 17px;border:1px solid #e1e6ef;border-radius:14px;background:#fff}.probability-stage span{color:#8b95a7;font-size:11px}.probability-stage strong{font-size:15px}
.probability-categories{display:grid;gap:18px}.probability-category{display:grid;gap:10px}.probability-category-head{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:0 2px}.probability-category-head>div{display:flex;align-items:center;gap:10px}.probability-category-head span{color:#35415a;font-size:16px;font-weight:950}.probability-category-head strong{padding:4px 8px;border-radius:999px;background:#e9edfb;color:#6875b7;font-size:10px}.probability-category-head p{margin:0;color:#8a94a7;font-size:11px}
.interaction-category{padding:18px 20px;border:1px solid #dfe5ef;border-radius:16px;background:#fff}.interaction-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:14px}.interaction-grid article{display:grid;gap:14px;padding:18px;border:1px solid #e0e6f0;border-radius:14px;background:#fbfcff}.interaction-grid article.state-pending{border-color:#d8def7;background:#f8f9ff}.interaction-grid article.state-cooldown{background:#fafbfc}.interaction-grid article>header{display:flex;align-items:flex-start;justify-content:space-between;gap:12px}.interaction-grid article header span{color:#7f8ba1;font-size:10px;font-weight:900}.interaction-grid h2{margin:4px 0 0;font-size:16px}.interaction-pending{display:grid;justify-items:end;gap:3px}.interaction-pending small{color:#8d97a9;font-size:10px}.interaction-pending strong{color:#5366d3;font-size:26px;line-height:1;font-variant-numeric:tabular-nums}.interaction-grid dl{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px;margin:0}.interaction-grid dl>div{display:grid;gap:3px;padding:10px;border-radius:10px;background:#f4f6fb}.interaction-grid dt{color:#9099aa;font-size:10px}.interaction-grid dd{margin:0;color:#536078;font-size:13px;font-weight:850}.interaction-grid footer{padding-top:10px;border-top:1px solid #edf0f4;color:#9aa3b3;font-size:10px}
.probability-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:14px}.probability-grid article{display:grid;gap:14px;padding:18px;border:1px solid #e0e6f0;border-radius:16px;background:#fff;box-shadow:0 7px 20px rgba(54,69,115,.045)}.probability-grid article.is-unseen{border-style:dashed;background:#fbfcff}.probability-grid article.is-disabled{opacity:.58;filter:saturate(.65)}
.probability-grid article>header{display:flex;align-items:flex-start;justify-content:space-between;gap:12px}.probability-grid article>header>div:first-child{min-width:0}.probability-grid article header span{color:#7e89a0;font-size:10px;font-weight:900}.probability-grid h2{margin:4px 0 0;overflow:hidden;font-size:16px;text-overflow:ellipsis;white-space:nowrap}.probability-current{display:grid;justify-items:end;gap:3px}.probability-current small{color:#8d97a9;font-size:10px}.probability-current strong{color:#5366d3;font-size:26px;line-height:1;font-variant-numeric:tabular-nums}
.weight-tuner{display:grid;gap:8px;padding:10px 11px;border:1px solid #e3e8f3;border-radius:11px;background:#f8faff;transition:.18s ease}.weight-tuner.saving{opacity:.65}.weight-tuner>div{display:flex;align-items:baseline;gap:7px}.weight-tuner span{color:#7d88a1!important;font-size:10px!important;letter-spacing:0!important}.weight-tuner b{color:#4f61cd;font-size:18px;font-variant-numeric:tabular-nums}.weight-tuner small{margin-left:auto;color:#98a1b2;font-size:9px}.weight-tuner input[type=range]{width:100%;accent-color:#5b6bd8;cursor:pointer}.weight-tuner input[type=range]:disabled{cursor:not-allowed;opacity:.45}
.probability-grid dl{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px;margin:0}.probability-grid dl>div{display:grid;gap:3px;padding:10px;border-radius:10px;background:#f7f9fd}.probability-grid dt{color:#9099aa;font-size:10px}.probability-grid dd{margin:0;color:#536078;font-size:13px;font-weight:850}.probability-grid footer{padding-top:10px;border-top:1px solid #edf0f4;color:#9aa3b3;font-size:10px}
@media(max-width:1100px){.probability-grid,.interaction-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.probability-stage{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:720px){.probability-page{padding:16px}.probability-head,.probability-toolbar{grid-template-columns:1fr;display:grid}.probability-category-head{align-items:flex-start;flex-direction:column}.probability-grid,.interaction-grid,.probability-stage{grid-template-columns:1fr}}
</style>
