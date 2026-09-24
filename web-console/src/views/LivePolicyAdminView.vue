<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import {
  chatLivePolicyAdminAgent,
  getLivePolicyAdminContext,
  getLivePolicyIndustries,
  publishLivePolicyAdminVersion,
  rollbackLivePolicyAdminVersion,
} from '../api'
import { session } from '../session'
import type {
  LivePolicyContext,
  LivePolicyIndustry,
  LivePolicyVersion,
} from '../types'

type Layer = 'L1' | 'L2'
type ChatMessage = { role: 'agent' | 'user'; text: string }
type ChatFontSize = 'small' | 'medium' | 'large'

const activeLayer = ref<Layer>('L1')
const industries = ref<LivePolicyIndustry[]>([])
const selectedIndustry = ref('general')
const context = ref<LivePolicyContext | null>(null)
const loading = ref(false)
const sending = ref(false)
const error = ref('')
const input = ref('')
const chatScrollRef = ref<HTMLElement | null>(null)
const chatPanelRef = ref<HTMLElement | null>(null)
const composerRef = ref<HTMLElement | null>(null)
const chatFontSize = ref<ChatFontSize>('medium')
const composerPosition = ref({ x: 0, y: 0 })
const composerWidth = ref(0)
const composerFloatingReady = ref(false)
const composerDragging = ref(false)
const chatPanelHeight = ref(0)
let composerPointerID: number | null = null
let composerDragStartX = 0
let composerDragStartY = 0
let composerDragOriginX = 0
let composerDragOriginY = 0
const messages = ref<ChatMessage[]>([
  {
    role: 'agent',
    text: '这里是管理端策略助手。请选择第一层系统规则或第二层行业规则，再直接告诉我你想怎么调整。',
  },
])

const isPlatformAdmin = computed(() => session.bootstrap?.actor.role === 'platform_admin')
const permissions = computed(() => session.bootstrap?.staff_access?.permissions ?? [])
const canManageL2 = computed(
  () =>
    isPlatformAdmin.value ||
    session.bootstrap?.staff_access?.is_super_admin === true ||
    permissions.value.includes('*') ||
    permissions.value.includes('livepolicy.manage_l2'),
)
const canManageCurrent = computed(
  () => activeLayer.value === 'L1' ? isPlatformAdmin.value : canManageL2.value,
)
const activeVersion = computed(() => context.value?.active ?? null)
const draftVersion = computed(
  () => context.value?.versions.find((item) => item.lifecycle_status === 'draft') ?? null,
)
const selectedIndustryName = computed(
  () => industries.value.find((item) => item.code === selectedIndustry.value)?.name || '通用',
)
const scopeTitle = computed(() =>
  activeLayer.value === 'L1'
    ? 'L1 · 系统规则'
    : 'L2 · ' + selectedIndustryName.value + '行业规则',
)
const versionLabel = computed(() => {
  if (draftVersion.value) return '草稿 V' + draftVersion.value.version_no
  if (activeVersion.value) return '已发布 V' + activeVersion.value.version_no
  return '尚未发布'
})
const visibleRules = computed(() => {
  const source = draftVersion.value || activeVersion.value
  return source?.rules ?? []
})
const draftHasConflicts = computed(() => (draftVersion.value?.conflicts.length ?? 0) > 0)
const chatFontClass = computed(() => 'font-' + chatFontSize.value)
const composerPositionStyle = computed(() => {
  if (!composerFloatingReady.value) return undefined
  return {
    left: composerPosition.value.x + 'px',
    top: composerPosition.value.y + 'px',
    width: composerWidth.value + 'px',
  }
})
const chatPanelStyle = computed(() =>
  chatPanelHeight.value > 0
    ? { height: chatPanelHeight.value + 'px' }
    : undefined,
)

function setChatFontSize(size: ChatFontSize) {
  chatFontSize.value = size
  window.localStorage.setItem('live-policy-chat-font-size', size)
  void keepComposerInViewport()
}

function saveComposerPosition() {
  window.localStorage.setItem('live-policy-composer-position', JSON.stringify(composerPosition.value))
}

function clampComposerPosition(x: number, y: number) {
  const composer = composerRef.value
  if (!composer) return { x, y }
  const rect = composer.getBoundingClientRect()
  const sideGap = 12
  const topGap = 12
  const bottomGap = 24
  const width = composerWidth.value || rect.width
  const minX = sideGap
  const maxX = Math.max(minX, window.innerWidth - width - sideGap)
  const minY = topGap
  const maxY = Math.max(minY, window.innerHeight - rect.height - bottomGap)
  return {
    x: Math.min(Math.max(x, minX), maxX),
    y: Math.min(Math.max(y, minY), maxY),
  }
}

function syncChatPanelToComposer() {
  const panel = chatPanelRef.value
  if (!panel || !composerFloatingReady.value) return
  const panelRect = panel.getBoundingClientRect()
  const workspaceRect = panel.parentElement?.getBoundingClientRect()
  const gap = 12
  const minHeight = 320
  const requestedHeight = composerPosition.value.y - panelRect.top - gap
  const maxHeight = workspaceRect
    ? Math.max(minHeight, workspaceRect.bottom - panelRect.top)
    : Math.max(minHeight, requestedHeight)
  chatPanelHeight.value = Math.min(
    Math.max(requestedHeight, minHeight),
    maxHeight,
  )
}

function moveComposerDrag(event: PointerEvent) {
  if (!composerDragging.value || composerPointerID !== event.pointerId) return
  const next = clampComposerPosition(
    composerDragOriginX + event.clientX - composerDragStartX,
    composerDragOriginY + event.clientY - composerDragStartY,
  )
  composerPosition.value = next
  syncChatPanelToComposer()
}

function stopComposerDrag(event?: PointerEvent) {
  if (!composerDragging.value) return
  if (event && composerPointerID !== event.pointerId) return
  composerDragging.value = false
  composerPointerID = null
  window.removeEventListener('pointermove', moveComposerDrag)
  window.removeEventListener('pointerup', stopComposerDrag)
  window.removeEventListener('pointercancel', stopComposerDrag)
  saveComposerPosition()
}

function startComposerDrag(event: PointerEvent) {
  const target = event.target as HTMLElement | null
  if (target?.closest('textarea, button, input, select, a')) return
  if (event.pointerType === 'mouse' && event.button !== 0) return
  event.preventDefault()
  composerDragging.value = true
  composerPointerID = event.pointerId
  composerDragStartX = event.clientX
  composerDragStartY = event.clientY
  composerDragOriginX = composerPosition.value.x
  composerDragOriginY = composerPosition.value.y
  window.addEventListener('pointermove', moveComposerDrag)
  window.addEventListener('pointerup', stopComposerDrag)
  window.addEventListener('pointercancel', stopComposerDrag)
}

async function initializeComposerPosition() {
  await nextTick()
  const composer = composerRef.value
  const panel = chatPanelRef.value
  if (!composer) return

  const composerRect = composer.getBoundingClientRect()
  const panelRect = panel?.getBoundingClientRect()
  composerWidth.value = Math.min(
    Math.max(320, (panelRect?.width ?? composerRect.width) - 36),
    window.innerWidth - 24,
  )

  let x = panelRect ? panelRect.left + 18 : composerRect.left
  let y = Math.min(composerRect.top, window.innerHeight - composerRect.height - 24)
  const storedPosition = window.localStorage.getItem('live-policy-composer-position')
  if (storedPosition) {
    try {
      const parsed = JSON.parse(storedPosition) as { x?: unknown; y?: unknown }
      if (typeof parsed.x === 'number' && typeof parsed.y === 'number') {
        x = parsed.x
        y = parsed.y
      }
    } catch {
      window.localStorage.removeItem('live-policy-composer-position')
    }
  }

  composerPosition.value = { x, y }
  composerFloatingReady.value = true
  await nextTick()
  composerPosition.value = clampComposerPosition(x, y)
  syncChatPanelToComposer()
}

async function keepComposerInViewport() {
  if (!composerFloatingReady.value) return
  const panelRect = chatPanelRef.value?.getBoundingClientRect()
  if (panelRect) {
    composerWidth.value = Math.min(Math.max(320, panelRect.width - 36), window.innerWidth - 24)
  }
  await nextTick()
  composerPosition.value = clampComposerPosition(composerPosition.value.x, composerPosition.value.y)
  syncChatPanelToComposer()
}

async function scrollChatToBottom() {
  await nextTick()
  const element = chatScrollRef.value
  if (!element) return
  element.scrollTo({
    top: element.scrollHeight,
    behavior: 'smooth',
  })
}

async function loadIndustries() {
  const response = await getLivePolicyIndustries()
  industries.value = response.items
  if (!industries.value.some((item) => item.code === selectedIndustry.value)) {
    selectedIndustry.value = industries.value[0]?.code || 'general'
  }
}

async function loadContext() {
  loading.value = true
  error.value = ''
  try {
    context.value = await getLivePolicyAdminContext(
      activeLayer.value,
      activeLayer.value === 'L2' ? selectedIndustry.value : undefined,
    )
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取策略配置失败'
  } finally {
    loading.value = false
  }
}

async function selectLayer(layer: Layer) {
  if (activeLayer.value === layer) return
  activeLayer.value = layer
  messages.value = [
    {
      role: 'agent',
      text:
        layer === 'L1'
          ? '当前切换到第一层系统全局规则。这里的规则会约束所有用户。'
          : '当前切换到第二层行业默认规则。选择行业后，我只会修改该行业的默认业务规则。',
    },
  ]
  await loadContext()
}

async function selectIndustry(code: string) {
  if (selectedIndustry.value === code && activeLayer.value === 'L2') return
  selectedIndustry.value = code
  activeLayer.value = 'L2'
  messages.value = [
    {
      role: 'agent',
      text: '当前调教：' + selectedIndustryName.value + '行业默认规则。不会读取或修改任何客户直播间。',
    },
  ]
  await loadContext()
}

async function send() {
  const value = input.value.trim()
  if (!value || sending.value || !canManageCurrent.value) return
  const history = messages.value.slice(-10)
  messages.value.push({ role: 'user', text: value })
  input.value = ''
  sending.value = true
  error.value = ''
  void keepComposerInViewport()
  void scrollChatToBottom()
  try {
    const response = await chatLivePolicyAdminAgent({
      layer: activeLayer.value,
      industry_code: activeLayer.value === 'L2' ? selectedIndustry.value : undefined,
      message: value,
      history,
    })
    messages.value.push({
      role: 'agent',
      text: response.reply + (response.draft ? '\n\n已生成草稿 V' + response.draft.version_no + '，发布后才会正式生效。' : ''),
    })
    void scrollChatToBottom()
    if (response.draft) await loadContext()
  } catch (err) {
    const message = err instanceof Error ? err.message : '策略 Agent 处理失败'
    error.value = message
    messages.value.push({ role: 'agent', text: '处理失败：' + message })
    void scrollChatToBottom()
  } finally {
    sending.value = false
    void keepComposerInViewport()
    void scrollChatToBottom()
  }
}

async function publishDraft() {
  const draft = draftVersion.value
  if (!draft || draftHasConflicts.value || !canManageCurrent.value) return
  loading.value = true
  error.value = ''
  try {
    await publishLivePolicyAdminVersion(draft.id)
    messages.value.push({
      role: 'agent',
      text: '草稿 V' + draft.version_no + ' 已发布。新启动的直播运行会加载这个版本。',
    })
    await loadContext()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '发布失败'
  } finally {
    loading.value = false
  }
}

async function rollback(version: LivePolicyVersion) {
  if (version.lifecycle_status === 'active' || !canManageCurrent.value || loading.value) return
  loading.value = true
  error.value = ''
  try {
    const result = await rollbackLivePolicyAdminVersion(version.id)
    messages.value.push({
      role: 'agent',
      text: '已从 V' + version.version_no + ' 创建并发布回滚版本 V' + result.version_no + '。',
    })
    await loadContext()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '回滚失败'
  } finally {
    loading.value = false
  }
}

function handleKeydown(event: KeyboardEvent) {
  if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
    event.preventDefault()
    void send()
  }
}

watch(
  [activeLayer, selectedIndustry],
  ([layer, industryCode]) => {
    window.localStorage.setItem(
      'system-agent-live-policy-context',
      JSON.stringify({
        layer,
        industry_code: layer === 'L2' ? industryCode : '',
      }),
    )
  },
  { immediate: true },
)

onMounted(async () => {
  const storedFontSize = window.localStorage.getItem('live-policy-chat-font-size')
  if (storedFontSize === 'small' || storedFontSize === 'medium' || storedFontSize === 'large') {
    chatFontSize.value = storedFontSize
  }
  try {
    await loadIndustries()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取行业目录失败'
  }
  await loadContext()
  await initializeComposerPosition()
  window.addEventListener('resize', keepComposerInViewport)
})

onBeforeUnmount(() => {
  window.removeEventListener('pointermove', moveComposerDrag)
  window.removeEventListener('pointerup', stopComposerDrag)
  window.removeEventListener('pointercancel', stopComposerDrag)
  window.removeEventListener('resize', keepComposerInViewport)
})
</script>

<template>
  <div class="live-strategy-page live-policy-admin-page">
    <ModulePageNav context="live" active-title="直播策略" active-nav-title="直播运维" />

    <section class="live-strategy-shell">
      <header class="live-policy-compact-head">
        <div class="live-policy-compact-title">
          <strong>小伴策略助手</strong>
          <span>{{ scopeTitle }}</span>
        </div>
        <div class="strategy-version-actions">
          <span class="strategy-version">{{ versionLabel }}</span>
          <button
            v-if="draftVersion"
            class="strategy-version-button primary"
            type="button"
            :disabled="loading || draftHasConflicts || !canManageCurrent"
            @click="publishDraft"
          >
            {{ draftHasConflicts ? '存在冲突' : '发布草稿' }}
          </button>
        </div>
      </header>

      <aside class="live-strategy-rooms live-policy-scope-panel">
        <div class="strategy-panel-title">
          <span class="section-kicker">POLICY LAYERS</span>
          <h2>策略层</h2>
        </div>

        <button
          class="strategy-room-card live-policy-layer-card"
          :class="{ active: activeLayer === 'L1' }"
          type="button"
          @click="selectLayer('L1')"
        >
          <span class="strategy-room-icon">1</span>
          <span>
            <strong>第一层 · 系统规则</strong>
            <small>强制边界 · 全局生效</small>
          </span>
        </button>

        <button
          class="strategy-room-card live-policy-layer-card"
          :class="{ active: activeLayer === 'L2' }"
          type="button"
          @click="selectLayer('L2')"
        >
          <span class="strategy-room-icon">2</span>
          <span>
            <strong>第二层 · 行业规则</strong>
            <small>行业默认 · L3 可覆盖业务项</small>
          </span>
        </button>

        <div v-if="activeLayer === 'L2'" class="live-policy-industry-list">
          <span class="live-policy-side-label">行业目录</span>
          <button
            v-for="industry in industries"
            :key="industry.code"
            type="button"
            :class="{ active: selectedIndustry === industry.code }"
            @click="selectIndustry(industry.code)"
          >
            <span>{{ industry.name }}</span>
            <small>{{ industry.code }}</small>
          </button>
        </div>
      </aside>

      <main class="live-strategy-agent">
        <div v-if="!canManageCurrent" class="live-policy-permission-note">
          {{ activeLayer === 'L1' ? 'L1 只允许超级系统管理员修改和发布。' : '当前账号只有查看行业策略的权限。' }}
        </div>
        <div v-if="error" class="inline-error strategy-inline-error">{{ error }}</div>

        <section class="live-policy-workspace">
          <div
            ref="chatPanelRef"
            :class="[
              'live-policy-chat-panel',
              chatFontClass,
              { 'is-thinking': sending },
            ]"
            :style="chatPanelStyle"
          >
            <div class="live-policy-chat-head">
              <div>
                <span class="section-kicker">AGENT CONVERSATION</span>
                <strong>和小伴聊规则</strong>
              </div>
              <div class="live-policy-chat-tools">
                <div class="live-policy-font-switch" aria-label="调整对话文字大小">
                  <button
                    type="button"
                    :class="{ active: chatFontSize === 'small' }"
                    title="较小文字"
                    @click="setChatFontSize('small')"
                  >A−</button>
                  <button
                    type="button"
                    :class="{ active: chatFontSize === 'medium' }"
                    title="标准文字"
                    @click="setChatFontSize('medium')"
                  >A</button>
                  <button
                    type="button"
                    :class="{ active: chatFontSize === 'large' }"
                    title="较大文字"
                    @click="setChatFontSize('large')"
                  >A+</button>
                </div>
                <span :class="['live-policy-agent-state', { active: sending }]">
                  <i></i>
                  {{ sending ? 'Agent 正在思考' : 'Agent 已就绪' }}
                </span>
              </div>
            </div>

            <div ref="chatScrollRef" class="strategy-chat live-policy-chat">
              <article
                v-for="(message, index) in messages"
                :key="index"
                :class="['strategy-message', message.role]"
              >
                <strong>{{ message.role === 'agent' ? '小伴策略助手' : '我' }}</strong>
                <p>{{ message.text }}</p>
              </article>

              <article v-if="sending" class="strategy-message agent live-policy-thinking-message">
                <strong>小伴策略助手</strong>
                <div class="live-policy-thinking-line">
                  <span></span>
                  <span></span>
                  <span></span>
                  <p>正在理解你的要求并整理规则草稿</p>
                </div>
              </article>
            </div>

            <div class="live-policy-composer-slot">
              <footer
                ref="composerRef"
                :class="[
                  'strategy-composer',
                  'live-policy-composer',
                  {
                    'is-floating': composerFloatingReady,
                    'is-dragging': composerDragging,
                  },
                ]"
                :style="composerPositionStyle"
                @pointerdown="startComposerDrag"
              >
                <div class="live-policy-composer-drag-handle" title="拖动输入框">
                  <span></span><span></span><span></span>
                </div>
                <div class="strategy-input-row">
                  <textarea
                    v-model="input"
                    rows="3"
                    :disabled="sending || !canManageCurrent"
                    :placeholder="
                      canManageCurrent
                        ? activeLayer === 'L1'
                          ? '告诉 Agent 要如何调整系统全局强制规则……'
                          : '告诉 Agent 要如何调整这个行业的默认业务规则……'
                        : '当前账号没有该层修改权限'
                    "
                    @keydown="handleKeydown"
                  ></textarea>
                  <button
                    class="primary-button"
                    type="button"
                    :disabled="sending || !canManageCurrent"
                    @click="send"
                  >
                    {{ sending ? '处理中…' : '发送' }}
                  </button>
                </div>
                <small>Agent 只生成草稿，不会直接改变正式运行规则；发布后才生效。</small>
              </footer>
            </div>
          </div>

          <aside class="live-policy-rules-panel">
            <div class="live-policy-rules-head">
              <div>
                <span class="section-kicker">CURRENT RULES</span>
                <strong>{{ draftVersion ? '草稿规则' : '当前生效规则' }}</strong>
              </div>
              <small>{{ visibleRules.length }} 条</small>
            </div>

            <div v-if="visibleRules.length" class="live-policy-rule-list">
              <article v-for="rule in visibleRules" :key="rule.key">
                <div>
                  <strong>{{ rule.title || rule.key }}</strong>
                  <span :class="['live-policy-mode', rule.execution_mode]">
                    {{ rule.execution_mode === 'verbatim' ? '固定原话' : '按意思生成' }}
                  </span>
                </div>
                <p>{{ rule.execution_mode === 'verbatim' && rule.fixed_text ? rule.fixed_text : rule.text }}</p>
                <small>{{ rule.key }}</small>
              </article>
            </div>
            <div v-else class="empty-state">当前层还没有发布规则，可以直接在中间告诉 Agent 你想建立什么规则。</div>

            <div v-if="draftVersion?.conflicts.length" class="live-policy-conflicts">
              <strong>草稿暂不能发布</strong>
              <p v-for="conflict in draftVersion.conflicts" :key="conflict.code + conflict.key">
                {{ conflict.message }}
              </p>
            </div>

            <details v-if="context?.versions.length" class="live-policy-version-history">
              <summary>历史版本（{{ context.versions.length }}）</summary>
              <div v-for="version in context.versions" :key="version.id" class="live-policy-version-row">
                <span>V{{ version.version_no }} · {{ version.lifecycle_status }}</span>
                <button
                  v-if="version.lifecycle_status !== 'active'"
                  type="button"
                  :disabled="loading || !canManageCurrent"
                  @click="rollback(version)"
                >
                  回滚到此版本
                </button>
              </div>
            </details>
          </aside>
        </section>
      </main>
    </section>
  </div>
</template>
