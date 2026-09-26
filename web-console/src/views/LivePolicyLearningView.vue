<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import {
  adoptLivePolicyLearningCandidate,
  getLivePolicyLearningCandidates,
  rejectLivePolicyLearningCandidate,
} from '../api'
import { session } from '../session'
import {
  canDelegateLivePolicyL3,
  canManageLivePolicyL1,
  canManageLivePolicyL2,
} from '../livePolicyAccess'
import type { LivePolicyLearningCandidate } from '../types'

type LearningFilter = 'pending' | 'adopted' | 'rejected' | 'all'
type LearningLayer = 'L1' | 'L2' | 'L3'

const items = ref<LivePolicyLearningCandidate[]>([])
const filter = ref<LearningFilter>('pending')
const loading = ref(false)
const error = ref('')
const success = ref('')
const targetLayers = reactive<Record<number, LearningLayer>>({})
const industryCodes = reactive<Record<number, string>>({})
const roomIds = reactive<Record<number, string>>({})
const reviewNotes = reactive<Record<number, string>>({})

const canL1 = computed(() => canManageLivePolicyL1(session.bootstrap))
const canL2 = computed(() => canManageLivePolicyL2(session.bootstrap))
const canL3 = computed(() => canDelegateLivePolicyL3(session.bootstrap))

function normalizeLayer(value: string): LearningLayer {
  if (value === 'L2' || value === 'L3') return value
  return 'L1'
}

function layerLabel(value: string) {
  if (value === 'L1') return '规则层'
  if (value === 'L2') return '行业层'
  if (value === 'L3') return '用户层'
  return value
}

function initializeCandidate(item: LivePolicyLearningCandidate) {
  if (!targetLayers[item.id]) targetLayers[item.id] = normalizeLayer(item.recommended_layer)
  if (industryCodes[item.id] === undefined) {
    industryCodes[item.id] = item.industry_code || 'general'
  }
  if (roomIds[item.id] === undefined) {
    roomIds[item.id] = item.room_id ? String(item.room_id) : ''
  }
  if (reviewNotes[item.id] === undefined) reviewNotes[item.id] = ''
}

async function load() {
  loading.value = true
  error.value = ''
  success.value = ''
  try {
    const result = await getLivePolicyLearningCandidates(filter.value)
    items.value = result.items
    items.value.forEach(initializeCandidate)
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取调教学习候选失败'
  } finally {
    loading.value = false
  }
}

function canAdopt(item: LivePolicyLearningCandidate) {
  const target = targetLayers[item.id] || normalizeLayer(item.recommended_layer)
  if (target === 'L1') return canL1.value
  if (target === 'L2') return canL2.value
  return canL3.value && Number(roomIds[item.id]) > 0
}

function adoptionHint(item: LivePolicyLearningCandidate) {
  const target = targetLayers[item.id] || normalizeLayer(item.recommended_layer)
  if (target === 'L1' && !canL1.value) return '当前账号没有规则层配置权限'
  if (target === 'L2' && !canL2.value) return '当前账号没有行业层配置权限'
  if (target === 'L3' && !canL3.value) {
    return '用户层需由客户本人，或具备行业层配置能力、用户层授权协助权限且已获客户授权的运维人员采纳'
  }
  if (target === 'L3' && Number(roomIds[item.id]) <= 0) return '请先填写目标直播间 ID'
  return ''
}

async function adopt(item: LivePolicyLearningCandidate) {
  if (!canAdopt(item) || loading.value) return
  const target = targetLayers[item.id]
  loading.value = true
  error.value = ''
  success.value = ''
  try {
    const result = await adoptLivePolicyLearningCandidate(item.id, {
      target_layer: target,
      industry_code: target === 'L2' ? industryCodes[item.id]?.trim() || 'general' : undefined,
      room_id: target === 'L3' ? Number(roomIds[item.id]) || undefined : undefined,
      review_note: reviewNotes[item.id]?.trim() || '',
    })
    const version = result.version || result.draft
    const message =
      target === 'L3' && result.published
        ? '已采纳并发布为用户层 V' + version.version_no + '，已立即生效；可在用户层版本历史中回滚。'
        : '已采纳为 ' +
          layerLabel(target) +
          ' 草稿 V' +
          version.version_no +
          '；仍需在对应策略工作台确认发布。'
    await load()
    success.value = message
  } catch (err) {
    error.value = err instanceof Error ? err.message : '采纳学习候选失败'
  } finally {
    loading.value = false
  }
}

function canReject(item: LivePolicyLearningCandidate) {
  if (item.recommended_layer === 'L1') return canL1.value
  return canL2.value
}

async function reject(item: LivePolicyLearningCandidate) {
  if (loading.value) return
  loading.value = true
  error.value = ''
  success.value = ''
  try {
    await rejectLivePolicyLearningCandidate(
      item.id,
      reviewNotes[item.id]?.trim() || '本次不沉淀为长期策略',
    )
    await load()
    success.value = '已拒绝该学习候选，不会进入策略规则。'
  } catch (err) {
    error.value = err instanceof Error ? err.message : '拒绝学习候选失败'
  } finally {
    loading.value = false
  }
}

function setFilter(value: LearningFilter) {
  if (filter.value === value) return
  filter.value = value
  void load()
}

function formatTime(value: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function handleLearningCreated() {
  if (filter.value === 'pending' || filter.value === 'all') {
    void load()
  }
}

onMounted(() => {
  window.addEventListener('live-policy-learning-created', handleLearningCreated)
  void load()
})

onBeforeUnmount(() => {
  window.removeEventListener('live-policy-learning-created', handleLearningCreated)
})
</script>

<template>
  <section class="policy-learning-page">
    <header class="learning-hero">
      <div>
        <span>智能体调教学习</span>
        <h2>把“聊到满意”沉淀成真正可复用的策略</h2>
        <p>
          智能体先判断这次经验应进入规则层、行业层还是用户层；人工确认后只生成草稿，不会自动发布。
        </p>
      </div>
      <div class="learning-layer-guide">
        <b>规则层</b><small>跨行业通用判断与表达</small>
        <b>行业层</b><small>行业知识与行业表达习惯</small>
        <b>用户层</b><small>客户 / 商品 / 活动 / 主播个性</small>
      </div>
    </header>

    <nav class="learning-filters">
      <button :class="{ active: filter === 'pending' }" @click="setFilter('pending')">待吸收</button>
      <button :class="{ active: filter === 'adopted' }" @click="setFilter('adopted')">已采纳</button>
      <button :class="{ active: filter === 'rejected' }" @click="setFilter('rejected')">已拒绝</button>
      <button :class="{ active: filter === 'all' }" @click="setFilter('all')">全部</button>
      <button class="refresh" :disabled="loading" @click="load">刷新</button>
    </nav>

    <p v-if="error" class="learning-message error">{{ error }}</p>
    <p v-if="success" class="learning-message success">{{ success }}</p>
    <p v-if="!error && loading && !items.length" class="learning-empty">正在读取调教学习记录…</p>
    <p v-else-if="!error && !items.length" class="learning-empty">当前没有这个状态的调教学习记录。</p>

    <div class="learning-list">
      <article v-for="item in items" :key="item.id" class="learning-card">
        <header>
          <div>
            <span class="candidate-id">#{{ item.id }}</span>
            <b>{{ item.rule_title }}</b>
          </div>
          <div class="badges">
            <span :class="['absorb-badge', { weak: !item.absorb_recommended }]">
              {{ item.absorb_recommended ? '建议吸收' : '建议人工判断' }}
            </span>
            <span class="layer-badge">建议 {{ layerLabel(item.recommended_layer) }}</span>
            <span>{{ item.confidence }}%</span>
          </div>
        </header>

        <section class="learning-source-grid">
          <div>
            <span>原始问题</span>
            <p>{{ item.question }}</p>
          </div>
          <div>
            <span>最终满意回复</span>
            <p>{{ item.final_reply }}</p>
          </div>
        </section>

        <section class="learning-reason">
          <span>智能体分层判断</span>
          <p>{{ item.recommendation_reason }}</p>
        </section>

        <section class="learning-rule">
          <span>准备沉淀的规则</span>
          <strong>{{ item.rule_title }}</strong>
          <p>{{ item.rule_text }}</p>
          <small>{{ item.execution_mode === 'verbatim' ? '固定原话' : '按意思执行' }}</small>
        </section>

        <div class="learning-meta">
          <span>来源：{{ layerLabel(item.source_layer) }}</span>
          <span v-if="item.industry_code">行业：{{ item.industry_code }}</span>
          <span v-if="item.room_id">直播间：{{ item.room_id }}</span>
          <span>{{ formatTime(item.created_at) }}</span>
        </div>

        <section v-if="item.status === 'pending'" class="learning-review">
          <label>
            <span>人工确认放到哪一层</span>
            <select v-model="targetLayers[item.id]">
              <option value="L1">规则层 · 系统通用原则</option>
              <option value="L2">行业层 · 行业表达规则</option>
              <option value="L3">用户层 · 直播间个性规则</option>
            </select>
          </label>

          <label v-if="targetLayers[item.id] === 'L2'">
            <span>行业代码</span>
            <input v-model="industryCodes[item.id]" placeholder="general" />
          </label>

          <label v-if="targetLayers[item.id] === 'L3'">
            <span>直播间 ID</span>
            <input v-model="roomIds[item.id]" inputmode="numeric" placeholder="请输入直播间 ID" />
          </label>

          <label class="review-note">
            <span>审核备注（可选）</span>
            <input v-model="reviewNotes[item.id]" placeholder="为什么采纳 / 为什么拒绝" />
          </label>

          <p v-if="adoptionHint(item)" class="adoption-hint">{{ adoptionHint(item) }}</p>

          <div class="learning-actions">
            <button class="reject" :disabled="loading || !canReject(item)" @click="reject(item)">
              不吸收
            </button>
            <button class="adopt" :disabled="loading || !canAdopt(item)" @click="adopt(item)">
              采纳为{{ layerLabel(targetLayers[item.id]) }}草稿
            </button>
          </div>
        </section>

        <footer v-else>
          <span>状态：{{ item.status === 'adopted' ? '已采纳' : '已拒绝' }}</span>
          <span v-if="item.adopted_version_id">草稿版本 ID：{{ item.adopted_version_id }}</span>
          <span v-if="item.review_note">{{ item.review_note }}</span>
        </footer>
      </article>
    </div>
  </section>
</template>

<style scoped>
.policy-learning-page {
  display: grid;
  gap: 22px;
  padding: 22px 26px 48px;
}
.learning-hero {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(320px, 0.52fr);
  gap: 24px;
  padding: 26px 30px;
  border: 1px solid #d7e0ff;
  border-radius: 24px;
  background: linear-gradient(135deg, #f7f9ff, #edf2ff);
}
.learning-hero span,
.learning-rule > span,
.learning-reason > span,
.learning-source-grid span,
.learning-review label > span {
  color: #5269c9;
  font-weight: 850;
}
.learning-hero h2 {
  margin: 8px 0;
  color: #1e2c46;
  font-size: 28px;
}
.learning-hero p {
  margin: 0;
  color: #61708c;
  font-size: 18px;
  line-height: 1.7;
}
.learning-layer-guide {
  display: grid;
  grid-template-columns: 88px minmax(0, 1fr);
  align-content: center;
  gap: 10px 14px;
  padding: 18px 20px;
  border-radius: 18px;
  background: rgba(255, 255, 255, 0.72);
}
.learning-layer-guide b {
  color: #405de0;
  font-size: 20px;
  white-space: nowrap;
}
.learning-layer-guide small {
  color: #53617a;
  font-size: 16px;
}
.learning-filters {
  display: flex;
  gap: 12px;
  align-items: center;
}
.learning-filters button,
.learning-actions button {
  min-height: 46px;
  padding: 0 20px;
  border: 1px solid #d8e0f2;
  border-radius: 14px;
  background: #fff;
  color: #41506a;
  font-size: 17px;
  font-weight: 800;
  cursor: pointer;
}
.learning-filters button.active,
.learning-actions .adopt {
  border-color: #9bafff;
  background: #eaf0ff;
  color: #3f59c9;
}
.learning-filters .refresh {
  margin-left: auto;
}
.learning-message,
.learning-empty {
  margin: 0;
  padding: 18px 22px;
  border-radius: 16px;
  font-size: 18px;
}
.learning-message.error { background: #fff0f1; color: #a6414c; }
.learning-message.success { background: #edf9f2; color: #327c55; }
.learning-empty { background: #f7f9fd; color: #75829a; text-align: center; }
.learning-list {
  display: grid;
  gap: 20px;
}
.learning-card {
  display: grid;
  gap: 18px;
  padding: 24px 26px;
  border: 1px solid #dfe5f1;
  border-radius: 22px;
  background: #fff;
  box-shadow: 0 10px 30px rgba(52, 74, 124, 0.06);
}
.learning-card > header {
  display: flex;
  justify-content: space-between;
  gap: 18px;
  align-items: center;
}
.learning-card > header > div:first-child {
  display: flex;
  gap: 12px;
  align-items: center;
  color: #22314d;
  font-size: 21px;
}
.candidate-id { color: #72809c; font-weight: 800; }
.badges {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.badges span {
  padding: 7px 11px;
  border-radius: 999px;
  background: #f0f3f9;
  color: #586781;
  font-weight: 800;
}
.badges .layer-badge { background: #edf1ff; color: #4962ca; }
.absorb-badge { background: #eaf8f0 !important; color: #39805a !important; }
.absorb-badge.weak { background: #fff5df !important; color: #8a611a !important; }
.learning-source-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
}
.learning-source-grid > div,
.learning-reason,
.learning-rule {
  padding: 18px 20px;
  border-radius: 16px;
  background: #f7f9fd;
}
.learning-source-grid p,
.learning-reason p,
.learning-rule p {
  margin: 8px 0 0;
  color: #394963;
  font-size: 17px;
  line-height: 1.7;
  white-space: pre-wrap;
}
.learning-rule strong {
  display: block;
  margin-top: 10px;
  color: #23334f;
  font-size: 19px;
}
.learning-rule small {
  display: inline-block;
  margin-top: 10px;
  color: #66738c;
  font-size: 16px;
}
.learning-meta,
.learning-card footer {
  display: flex;
  flex-wrap: wrap;
  gap: 14px;
  color: #71809b;
  font-size: 16px;
}
.learning-review {
  display: grid;
  grid-template-columns: repeat(3, minmax(180px, 1fr));
  gap: 14px;
  padding-top: 18px;
  border-top: 1px solid #edf0f6;
}
.learning-review label {
  display: grid;
  gap: 8px;
}
.learning-review select,
.learning-review input {
  min-height: 48px;
  border: 1px solid #d8e0ee;
  border-radius: 12px;
  padding: 0 14px;
  background: #fff;
  color: #2f3e58;
  font-size: 16px;
}
.review-note {
  grid-column: 1 / -1;
}
.adoption-hint {
  grid-column: 1 / -1;
  margin: 0;
  color: #98611d;
  font-size: 16px;
  font-weight: 750;
}
.learning-actions {
  grid-column: 1 / -1;
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}
.learning-actions .reject {
  color: #a54650;
  background: #fff6f7;
}
.learning-actions button:disabled,
.learning-filters button:disabled {
  cursor: not-allowed;
  opacity: 0.48;
}
@media (max-width: 1100px) {
  .learning-hero,
  .learning-source-grid,
  .learning-review {
    grid-template-columns: 1fr;
  }
  .review-note,
  .adoption-hint,
  .learning-actions {
    grid-column: 1;
  }
}
</style>
