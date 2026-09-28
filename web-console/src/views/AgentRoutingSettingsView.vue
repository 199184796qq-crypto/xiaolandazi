<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  assistAgentRoutingConfig,
  deleteAgentUnderstandingPolicy,
  getAgentUnderstandingModels,
  getAgentUnderstandingPolicies,
  getAgentRoutingConfig,
  getAgentRoutingHistory,
  getEffectiveAgentUnderstandingPolicy,
  getSystemSettingsDashboard,
  getTenants,
  publishAgentRoutingConfig,
  rollbackAgentRoutingConfig,
  saveAgentRoutingDraft,
  saveAgentUnderstandingPolicy,
} from '../api'
import JsonTreeNode from '../components/JsonTreeNode.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import type {
  AgentPromptConfig,
  AgentPromptHistory,
  AgentUnderstandingEffectivePolicy,
  AgentUnderstandingMode,
  AgentUnderstandingPolicy,
  AgentUnderstandingPolicyInput,
  AgentUnderstandingScopeType,
  MembershipRoomLimitSetting,
  Tenant,
} from '../types'

const loading = ref(false)
const saving = ref(false)
const publishing = ref(false)
const assisting = ref(false)
const rollingBack = ref<number | null>(null)
const config = ref<AgentPromptConfig | null>(null)
const history = ref<AgentPromptHistory[]>([])
const jsonText = ref('')
const instruction = ref('')
const error = ref('')
const notice = ref('')
const editorView = ref<'json' | 'tree'>('json')
const treeExpandAll = ref(false)
const treeExpandToken = ref(0)
const understandingPolicies = ref<AgentUnderstandingPolicy[]>([])
const membershipPlans = ref<MembershipRoomLimitSetting[]>([])
const tenants = ref<Tenant[]>([])
const understandingModels = ref<string[]>([])
const understandingScopeType = ref<AgentUnderstandingScopeType>('system')
const understandingScopeId = ref(0)
const understandingSaving = ref(false)
const understandingDeleting = ref(false)
const effectiveTenantId = ref(0)
const effectivePolicy = ref<AgentUnderstandingEffectivePolicy | null>(null)
const understandingDraft = ref<AgentUnderstandingPolicyInput>(newUnderstandingDraft())

function newUnderstandingDraft(): AgentUnderstandingPolicyInput {
  return {
    scope_type: 'system',
    scope_id: 0,
    mode: 'model',
    provider: 'qwen',
    model: 'qwen3.8-flash',
    max_context_messages: 10,
    max_tokens: 900,
    timeout_ms: 12000,
    monthly_budget_tokens: 0,
    budget_fallback: 'program',
    min_confidence: 0.72,
    enabled: true,
  }
}

const selectedUnderstandingPolicy = computed(() =>
  understandingPolicies.value.find(
    (item) => item.scope_type === understandingScopeType.value && item.scope_id === understandingScopeId.value,
  ) || null,
)

const understandingModeHelp: Record<AgentUnderstandingMode, string> = {
  program: '零大模型理解费用。只按当前模块、正式数据、固定语法和上下文状态判断；不确定就澄清。',
  model: '复杂语义交给大模型理解，但只输出结构化意图；真实写入仍必须经过权限、确认和执行器。',
  auto: '程序先判断；高置信度直接执行理解结果，复杂或低置信度语句再调用大模型。',
}

const routingNotes: Record<string, string> = {
  schema_version: '配置结构版本。当前固定为 1，维护时不要随意修改。',
  fallback_intent: '规则和模型都无法可靠判断时使用的兜底模式，通常保持 chat。',
  model_enabled: '是否启用大模型做上下文语义分类。关闭后只使用结构化匹配规则。',
  min_model_confidence: '模型判断达到这个置信度后才采用，否则回退到 fallback_intent。',
  allow_natural_actions: '控制自然语言是否允许直接触发真实业务动作；属于高风险开关。',
  'allow_natural_actions.adopt': '允许“保存发布、就按这个”等自然语言直接进入真实采用流程。',
  'allow_natural_actions.execution': '允许自然语言直接进入正式回答/执行流程。',
  priority: '多个结构化规则同时命中时的处理顺序，越靠前优先级越高。',
  classifier_prompt: '结构规则无法确定时，交给大模型做聊天/学习/测试/执行/采用分类的核心说明。',
  intents: '各类意图的结构化匹配规则集合。',
  'intents.adopt': '确认采用当前学习成果，并进入真实保存/发布动作。',
  'intents.test': '只做模拟验证，不写正式记忆，也不触发播音。',
  'intents.learning': '用于纠正、补充、替换和继续打磨智能体学习候选。',
  'intents.execution': '用于要求系统现在回答真实弹幕或执行正式直播回答。',
  'intents.adopt.match': '采用意图的关键词和句式匹配配置。',
  'intents.test.match': '测试意图的关键词和句式匹配配置。',
  'intents.learning.match': '学习/纠正意图的关键词和句式匹配配置。',
  'intents.execution.match': '正式执行意图的关键词和句式匹配配置。',
  'intents.adopt.match.exact_any': '完全一致时命中，适合“采用、保存发布”等明确短指令。',
  'intents.test.match.contains_any': '文本中包含任一表达即可识别为测试意图。',
  'intents.learning.match.contains_any': '文本中包含任一表达即可优先识别为学习/纠正。',
  'intents.execution.match.contains_any': '文本中包含任一表达即可识别为正式回答/执行请求。',
}

const hasUnpublishedDraft = computed(() => {
  if (!config.value) return false
  return jsonText.value.trim() !== config.value.current_value.trim()
})

const parsedTree = computed(() => {
  try {
    const parsed = JSON.parse(jsonText.value)
    if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') return null
    return parsed as Record<string, unknown>
  } catch {
    return null
  }
})

const parsedBaselineTree = computed(() => {
  try {
    const parsed = JSON.parse(config.value?.default_value || '')
    if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') return null
    return parsed as Record<string, unknown>
  } catch {
    return null
  }
})

function prettyJSON(value: string) {
  const parsed = JSON.parse(value)
  if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
    throw new Error('顶层必须是 JSON 对象')
  }
  return JSON.stringify(parsed, null, 2)
}

function validateEditor(showNotice = true) {
  error.value = ''
  try {
    jsonText.value = prettyJSON(jsonText.value)
    if (showNotice) {
      notice.value = 'JSON 语法校验通过；结构规则将在保存草稿时由后端再次校验。'
    }
    return true
  } catch (value) {
    error.value = value instanceof Error ? 'JSON 无效：' + value.message : 'JSON 无效'
    return false
  }
}

function switchEditorView(view: 'json' | 'tree') {
  if (view === 'tree' && !validateEditor(false)) return
  editorView.value = view
}

function setTreeExpanded(expanded: boolean) {
  treeExpandAll.value = expanded
  treeExpandToken.value += 1
}

function loadSystemBaseline() {
  if (!config.value?.default_value) return
  try {
    jsonText.value = prettyJSON(config.value.default_value)
    editorView.value = 'tree'
    notice.value = '系统基础配置已载入编辑区，仅作为草稿；基础配置本身没有被修改。'
    error.value = ''
  } catch (value) {
    error.value = value instanceof Error ? value.message : '系统基础配置读取失败'
  }
}

function syncConfig(value: AgentPromptConfig) {
  config.value = value
  jsonText.value = value.draft_value || value.current_value
}

function policyInputFrom(value: AgentUnderstandingPolicy): AgentUnderstandingPolicyInput {
  return {
    scope_type: value.scope_type,
    scope_id: value.scope_id,
    mode: value.mode,
    provider: value.provider,
    model: value.model,
    max_context_messages: value.max_context_messages,
    max_tokens: value.max_tokens,
    timeout_ms: value.timeout_ms,
    monthly_budget_tokens: value.monthly_budget_tokens,
    budget_fallback: value.budget_fallback,
    min_confidence: value.min_confidence,
    enabled: value.enabled,
  }
}

function syncUnderstandingDraft() {
  if (understandingScopeType.value === 'system') understandingScopeId.value = 0
  const existing = selectedUnderstandingPolicy.value
  if (existing) {
    understandingDraft.value = policyInputFrom(existing)
    return
  }
  const systemPolicy = understandingPolicies.value.find((item) => item.scope_type === 'system' && item.scope_id === 0)
  const base = systemPolicy ? policyInputFrom(systemPolicy) : newUnderstandingDraft()
  understandingDraft.value = {
    ...base,
    scope_type: understandingScopeType.value,
    scope_id: understandingScopeId.value,
  }
}

function changeUnderstandingScope() {
  if (understandingScopeType.value === 'system') {
    understandingScopeId.value = 0
  } else if (understandingScopeType.value === 'membership') {
    understandingScopeId.value = membershipPlans.value[0]?.plan_id || 0
  } else {
    understandingScopeId.value = tenants.value[0]?.id || 0
  }
  syncUnderstandingDraft()
}

async function loadUnderstandingModels() {
  try {
    const result = await getAgentUnderstandingModels(understandingDraft.value.provider || 'qwen')
    understandingModels.value = (result.items || []).map((item) => item.id).filter(Boolean)
  } catch {
    understandingModels.value = understandingDraft.value.model ? [understandingDraft.value.model] : []
  }
}

async function refreshEffectivePolicy() {
  if (!effectiveTenantId.value) {
    effectivePolicy.value = (await getEffectiveAgentUnderstandingPolicy()).policy
    return
  }
  effectivePolicy.value = (await getEffectiveAgentUnderstandingPolicy(effectiveTenantId.value)).policy
}

async function loadUnderstanding() {
  const [policies, settings, tenantResult] = await Promise.all([
    getAgentUnderstandingPolicies(),
    getSystemSettingsDashboard(),
    getTenants(),
  ])
  understandingPolicies.value = policies.items || []
  membershipPlans.value = settings.membership_room_limits || []
  tenants.value = tenantResult.items || []
  if (understandingScopeType.value === 'membership' && !understandingScopeId.value) {
    understandingScopeId.value = membershipPlans.value[0]?.plan_id || 0
  }
  if (understandingScopeType.value === 'tenant' && !understandingScopeId.value) {
    understandingScopeId.value = tenants.value[0]?.id || 0
  }
  syncUnderstandingDraft()
  if (!effectiveTenantId.value && tenants.value.length) effectiveTenantId.value = tenants.value[0].id
  await Promise.all([loadUnderstandingModels(), refreshEffectivePolicy()])
}

async function saveUnderstanding() {
  if (understandingSaving.value || (understandingScopeType.value !== 'system' && !understandingScopeId.value)) return
  understandingSaving.value = true
  error.value = ''
  notice.value = ''
  try {
    understandingDraft.value.scope_type = understandingScopeType.value
    understandingDraft.value.scope_id = understandingScopeType.value === 'system' ? 0 : understandingScopeId.value
    await saveAgentUnderstandingPolicy(understandingDraft.value)
    await loadUnderstanding()
    notice.value = '智能体理解策略已保存，并立即用于后续新请求。程序理解和大模型理解不会改变原有权限、确认和执行器规则。'
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存智能体理解策略失败'
  } finally {
    understandingSaving.value = false
  }
}

async function removeUnderstandingOverride() {
  if (understandingDeleting.value || understandingScopeType.value === 'system' || !understandingScopeId.value) return
  understandingDeleting.value = true
  error.value = ''
  notice.value = ''
  try {
    await deleteAgentUnderstandingPolicy(understandingScopeType.value, understandingScopeId.value)
    await loadUnderstanding()
    notice.value = '覆盖策略已删除，该范围重新继承上一级理解策略。'
  } catch (value) {
    error.value = value instanceof Error ? value.message : '删除理解策略失败'
  } finally {
    understandingDeleting.value = false
  }
}

async function load() {
  loading.value = true
  error.value = ''
  notice.value = ''
  try {
    const [routing, versions] = await Promise.all([
      getAgentRoutingConfig(),
      getAgentRoutingHistory(),
    ])
    syncConfig(routing.config)
    history.value = versions.items
    await loadUnderstanding()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取智能体路由配置失败'
  } finally {
    loading.value = false
  }
}

async function saveDraft(showSuccess = true) {
  if (saving.value || !validateEditor(false)) return false
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await saveAgentRoutingDraft(jsonText.value)
    syncConfig(result.config)
    if (showSuccess) notice.value = '路由 JSON 草稿已保存，尚未发布。'
    return true
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存路由草稿失败'
    return false
  } finally {
    saving.value = false
  }
}

async function publish() {
  if (publishing.value) return
  publishing.value = true
  error.value = ''
  notice.value = ''
  try {
    if (!(await saveDraft(false))) return
    const result = await publishAgentRoutingConfig()
    syncConfig(result.config)
    history.value = (await getAgentRoutingHistory()).items
    notice.value =
      '智能体路由配置已发布为 V' +
      result.config.version +
      '，后续新请求立即使用。'
  } catch (value) {
    error.value = value instanceof Error ? value.message : '发布智能体路由配置失败'
  } finally {
    publishing.value = false
  }
}

async function askAssistant() {
  const task = instruction.value.trim()
  if (!task || assisting.value || !validateEditor(false)) return
  assisting.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await assistAgentRoutingConfig(task, jsonText.value)
    jsonText.value = prettyJSON(result.proposed_json)
    notice.value =
      '智能体已生成新的 JSON 草稿' +
      (result.model ? '（' + result.model + '）' : '') +
      '。请人工检查后再保存、发布。'
    instruction.value = ''
  } catch (value) {
    error.value = value instanceof Error ? value.message : '智能体生成路由草稿失败'
  } finally {
    assisting.value = false
  }
}

async function rollback(version: number) {
  if (rollingBack.value !== null) return
  rollingBack.value = version
  error.value = ''
  notice.value = ''
  try {
    const result = await rollbackAgentRoutingConfig(version)
    syncConfig(result.config)
    history.value = (await getAgentRoutingHistory()).items
    notice.value =
      '已回滚 V' +
      version +
      '，并作为新版本 V' +
      result.config.version +
      ' 发布。'
  } catch (value) {
    error.value = value instanceof Error ? value.message : '回滚路由版本失败'
  } finally {
    rollingBack.value = null
  }
}

onMounted(load)
</script>

<template>
  <div class="management-page agent-routing-page">
    <ModulePageNav context="workspace-auto" active-title="智能体理解配置" />

    <section class="feature-workspace-hero">
      <div>
        <h2>智能体理解配置</h2>
        <p>
          配置自然语言由程序理解、大模型理解或自动理解。理解引擎只负责“听懂”，真实动作始终经过权限、确认、执行器和审计。
        </p>
      </div>
      <div class="routing-hero-actions">
        <RouterLink class="ghost-button" to="/system/settings">返回系统设定</RouterLink>
        <button class="ghost-button" type="button" :disabled="loading" @click="load">
          {{ loading ? '读取中…' : '刷新' }}
        </button>
      </div>
    </section>

    <p v-if="error" class="auth-error">{{ error }}</p>
    <p v-if="notice" class="settings-success">{{ notice }}</p>

    <section v-if="config" class="routing-status-card">
      <div>
        <span>配置 Key</span>
        <code>{{ config.key }}</code>
      </div>
      <div>
        <span>已发布版本</span>
        <strong>V{{ config.version }}</strong>
      </div>
      <div>
        <span>编辑状态</span>
        <strong>{{ hasUnpublishedDraft ? '有未发布修改' : '与已发布版本一致' }}</strong>
      </div>
      <div>
        <span>运行状态</span>
        <strong>{{ config.enabled ? '启用' : '停用' }}</strong>
      </div>
    </section>

    <section class="system-settings-card understanding-card">
      <header class="understanding-head">
        <div>
          <h3>智能体理解引擎</h3>
          <p>统一决定自然语言由程序理解、大模型理解还是自动分流。理解层只输出结构化意图，不能绕过权限、确认、审批和正式执行器。</p>
        </div>
        <div class="understanding-head-actions">
          <button class="primary-button" type="button" :disabled="understandingSaving" @click="saveUnderstanding">
            {{ understandingSaving ? '保存中…' : '保存理解策略' }}
          </button>
          <button
            v-if="understandingScopeType !== 'system' && selectedUnderstandingPolicy"
            class="danger-button"
            type="button"
            :disabled="understandingDeleting"
            @click="removeUnderstandingOverride"
          >
            {{ understandingDeleting ? '删除中…' : '删除覆盖，继承上级' }}
          </button>
        </div>
      </header>

      <div class="understanding-scope-row">
        <label>
          <span>配置层级</span>
          <select v-model="understandingScopeType" @change="changeUnderstandingScope">
            <option value="system">系统默认</option>
            <option value="membership">会员级别</option>
            <option value="tenant">指定客户</option>
          </select>
        </label>
        <label v-if="understandingScopeType === 'membership'">
          <span>会员级别</span>
          <select v-model.number="understandingScopeId" @change="syncUnderstandingDraft">
            <option v-for="item in membershipPlans" :key="item.plan_id" :value="item.plan_id">
              {{ item.plan_name }}
            </option>
          </select>
        </label>
        <label v-else-if="understandingScopeType === 'tenant'">
          <span>客户</span>
          <select v-model.number="understandingScopeId" @change="syncUnderstandingDraft">
            <option v-for="item in tenants" :key="item.id" :value="item.id">
              {{ item.name }} · #{{ item.id }}
            </option>
          </select>
        </label>
        <div class="understanding-inherit-note">
          <strong>{{ selectedUnderstandingPolicy ? '当前层已有独立策略' : '当前层继承上级策略' }}</strong>
          <small>优先级：指定客户 ＞ 当前有效会员级别 ＞ 系统默认。会员覆盖失效后回到系统默认；停止合作或理解预算耗尽时自动降级到程序理解。</small>
        </div>
      </div>

      <div class="understanding-mode-grid">
        <button
          v-for="mode in (['program', 'model', 'auto'] as AgentUnderstandingMode[])"
          :key="mode"
          type="button"
          :class="['understanding-mode-card', { active: understandingDraft.mode === mode }]"
          @click="understandingDraft.mode = mode"
        >
          <strong>{{ mode === 'program' ? '程序理解' : mode === 'model' ? '大模型理解' : '自动理解' }}</strong>
          <small>{{ understandingModeHelp[mode] }}</small>
        </button>
      </div>

      <div class="understanding-fields">
        <label>
          <span>模型供应商</span>
          <select v-model="understandingDraft.provider" @change="loadUnderstandingModels">
            <option value="qwen">Qwen</option>
          </select>
          <small>理解层已抽象供应商接口；当前运行环境正式接入 Qwen，后续注册新供应商后可直接扩展。</small>
        </label>
        <label>
          <span>理解模型</span>
          <input v-model.trim="understandingDraft.model" list="understanding-models" placeholder="模型 ID" />
          <datalist id="understanding-models">
            <option v-for="item in understandingModels" :key="item" :value="item" />
          </datalist>
        </label>
        <label>
          <span>最大上下文消息数</span>
          <input v-model.number="understandingDraft.max_context_messages" type="number" min="1" max="30" />
        </label>
        <label>
          <span>单次最大 Token</span>
          <input v-model.number="understandingDraft.max_tokens" type="number" min="64" max="4000" step="64" />
        </label>
        <label>
          <span>超时（毫秒）</span>
          <input v-model.number="understandingDraft.timeout_ms" type="number" min="1000" max="60000" step="1000" />
        </label>
        <label>
          <span>最低置信度</span>
          <input v-model.number="understandingDraft.min_confidence" type="number" min="0.1" max="1" step="0.01" />
        </label>
        <label>
          <span>每月理解 Token 预算</span>
          <input v-model.number="understandingDraft.monthly_budget_tokens" type="number" min="0" step="10000" />
          <small>0 = 不设 Token 上限；超过预算自动降级为程序理解。</small>
        </label>
        <label>
          <span>预算耗尽后</span>
          <select v-model="understandingDraft.budget_fallback">
            <option value="program">降级为程序理解</option>
          </select>
        </label>
        <label class="understanding-enable-field">
          <span>当前策略</span>
          <button
            type="button"
            :class="['understanding-toggle', { active: understandingDraft.enabled }]"
            :disabled="understandingScopeType === 'system'"
            @click="understandingDraft.enabled = !understandingDraft.enabled"
          >
            {{ understandingScopeType === 'system' ? '系统默认永久启用' : understandingDraft.enabled ? '启用' : '停用并继承上级' }}
          </button>
        </label>
      </div>

      <div class="understanding-effective">
        <div class="understanding-effective-head">
          <div>
            <strong>客户最终生效策略</strong>
            <small>用于核对会员状态、客户覆盖和预算降级后的最终结果。</small>
          </div>
          <select v-model.number="effectiveTenantId" @change="refreshEffectivePolicy">
            <option :value="0">系统默认</option>
            <option v-for="item in tenants" :key="item.id" :value="item.id">{{ item.name }} · #{{ item.id }}</option>
          </select>
        </div>
        <div v-if="effectivePolicy" class="understanding-effective-grid">
          <div><span>最终模式</span><strong>{{ effectivePolicy.mode === 'program' ? '程序理解' : effectivePolicy.mode === 'model' ? '大模型理解' : '自动理解' }}</strong></div>
          <div><span>来源</span><strong>{{ effectivePolicy.resolved_from }}</strong></div>
          <div><span>模型</span><strong>{{ effectivePolicy.mode === 'program' ? '不调用模型' : effectivePolicy.model }}</strong></div>
          <div><span>合作状态</span><strong>{{ effectivePolicy.cooperation_status || '-' }}</strong></div>
          <div><span>本月已用</span><strong>{{ effectivePolicy.budget_used_tokens.toLocaleString() }} Token</strong></div>
          <div><span>预算状态</span><strong>{{ effectivePolicy.budget_exceeded ? '已耗尽，已降级' : effectivePolicy.monthly_budget_tokens ? effectivePolicy.budget_remaining_tokens.toLocaleString() + ' Token 可用' : '不限额' }}</strong></div>
        </div>
      </div>
    </section>

    <details v-if="config" class="system-settings-card routing-baseline-card">
      <summary>
        <div>
          <div class="routing-baseline-title">
            <strong>系统基础配置</strong>
            <span class="routing-baseline-lock">永久保留 · 不可删除</span>
          </div>
          <p>
            这是系统强制保留的最基础路由配置。发布、回滚、版本历史和后续维护都不会覆盖或删除这一份。
          </p>
        </div>
        <button class="ghost-button" type="button" @click.stop="loadSystemBaseline">
          载入到编辑区
        </button>
      </summary>
      <div class="routing-baseline-body">
        <JsonTreeNode
          v-if="parsedBaselineTree"
          name="系统基础配置"
          :value="parsedBaselineTree"
          path=""
          :notes="routingNotes"
        />
        <pre v-else>{{ config.default_value }}</pre>
      </div>
    </details>

    <section class="routing-maintenance-layout">
      <article class="system-settings-card routing-editor-card">
        <header>
          <div>
            <h3>高级意图路由 JSON</h3>
            <p>
              修改词库、意图优先级、模型分类说明、置信度和自然语言动作开关。保存草稿不会影响线上，发布后才生效。
            </p>
          </div>
          <div class="routing-editor-actions">
            <div class="routing-view-switch" aria-label="配置显示方式">
              <button
                type="button"
                :class="{ active: editorView === 'json' }"
                @click="switchEditorView('json')"
              >
                JSON 源码
              </button>
              <button
                type="button"
                :class="{ active: editorView === 'tree' }"
                @click="switchEditorView('tree')"
              >
                结构化树
              </button>
            </div>
            <template v-if="editorView === 'tree'">
              <button class="ghost-button compact-button" type="button" @click="setTreeExpanded(true)">
                全部展开
              </button>
              <button class="ghost-button compact-button" type="button" @click="setTreeExpanded(false)">
                全部收起
              </button>
            </template>
            <button class="ghost-button" type="button" @click="validateEditor()">
              格式化 / 校验
            </button>
            <button class="ghost-button" type="button" :disabled="saving" @click="saveDraft()">
              {{ saving ? '保存中…' : '保存草稿' }}
            </button>
            <button class="primary-button" type="button" :disabled="publishing" @click="publish">
              {{ publishing ? '发布中…' : '发布新版本' }}
            </button>
          </div>
        </header>
        <textarea
          v-if="editorView === 'json'"
          v-model="jsonText"
          class="routing-json-editor"
          spellcheck="false"
          aria-label="智能体路由 JSON"
        />
        <div v-else class="routing-tree-editor">
          <div class="routing-tree-note">
            结构化树用于查看层级和快速检查配置。需要修改时切回“JSON 源码”；两种视图读取的是同一份草稿。
          </div>
          <JsonTreeNode
            v-if="parsedTree"
            name="agent.routing.live_room"
            :value="parsedTree"
            :expand-token="treeExpandToken"
            :expand-all="treeExpandAll"
            path=""
            :notes="routingNotes"
          />
          <div v-else class="panel-loading">当前 JSON 无法解析，请切回源码修正后再查看结构树。</div>
        </div>
      </article>

      <aside class="system-settings-card routing-assistant-card">
        <div>
          <h3>智能体维护工具</h3>
          <p>
            用自然语言告诉智能体怎么调整。它只修改编辑区 JSON，不会自动保存，更不会自动发布。
          </p>
        </div>
        <textarea
          v-model="instruction"
          rows="9"
          placeholder="例如：把“记住这个”加入采用意图；测试相关表达保持不变。"
        />
        <button
          class="primary-button"
          type="button"
          :disabled="assisting || !instruction.trim()"
          @click="askAssistant"
        >
          {{ assisting ? '智能体生成中…' : '生成 JSON 草稿' }}
        </button>
        <small>智能体结果必须先通过 JSON 结构校验，再由维护人员人工确认和发布。</small>
      </aside>
    </section>

    <section class="system-settings-card">
      <header>
        <div>
          <h3>版本历史</h3>
          <p>每次正式发布都会生成版本。回滚也会形成新的已发布版本，便于完整审计。</p>
        </div>
      </header>
      <div v-if="history.length" class="routing-history-list">
        <article v-for="item in history" :key="item.version">
          <div class="routing-history-head">
            <div>
              <strong>V{{ item.version }}</strong>
              <span>{{ item.operation }}</span>
              <small>操作者 #{{ item.updated_by_user_id }}</small>
              <small>{{ new Date(item.created_at).toLocaleString() }}</small>
            </div>
            <button
              class="ghost-button"
              type="button"
              :disabled="rollingBack !== null || item.version === config?.version"
              @click="rollback(item.version)"
            >
              {{
                item.version === config?.version
                  ? '当前版本'
                  : rollingBack === item.version
                    ? '回滚中…'
                    : '回滚到此版本'
              }}
            </button>
          </div>
          <pre>{{ item.value }}</pre>
        </article>
      </div>
      <div v-else class="panel-loading">暂无版本历史。</div>
    </section>
  </div>
</template>

<style scoped>
.agent-routing-page {
  display: grid;
  gap: 18px;
}

.routing-hero-actions,
.routing-editor-actions {
  display: flex;
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
}

.routing-status-card {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  padding: 16px 18px;
  border: 1px solid rgba(94, 129, 255, 0.18);
  border-radius: 18px;
  background: rgba(255, 255, 255, 0.72);
}

.routing-status-card > div {
  display: grid;
  gap: 6px;
}

.routing-status-card span,
.routing-assistant-card small {
  color: var(--text-muted, #6f7893);
}

.routing-status-card code {
  overflow-wrap: anywhere;
}

.understanding-card {
  display: grid;
  gap: 18px;
  border-color: rgba(89, 105, 205, 0.22);
  background: linear-gradient(145deg, rgba(253, 254, 255, 0.98), rgba(244, 247, 255, 0.96));
  box-shadow: 0 14px 34px rgba(65, 78, 150, 0.08);
}

.understanding-head,
.understanding-effective-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
}

.understanding-head h3 {
  margin: 0 0 7px;
  color: #2d3857;
  font-size: 24px;
}

.understanding-head p,
.understanding-effective-head small,
.understanding-inherit-note small,
.understanding-fields label > small {
  color: #7b859d;
  line-height: 1.55;
}

.understanding-head p {
  margin: 0;
  max-width: 930px;
}

.understanding-head-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.understanding-scope-row {
  display: grid;
  grid-template-columns: minmax(180px, 240px) minmax(220px, 320px) minmax(0, 1fr);
  gap: 12px;
  align-items: end;
}

.understanding-scope-row label,
.understanding-fields label {
  display: grid;
  gap: 7px;
  min-width: 0;
}

.understanding-scope-row label > span,
.understanding-fields label > span {
  color: #59657f;
  font-size: 13px;
  font-weight: 850;
}

.understanding-scope-row select,
.understanding-fields input,
.understanding-fields select,
.understanding-effective-head select {
  width: 100%;
  min-height: 42px;
  box-sizing: border-box;
  border: 1px solid rgba(86, 105, 181, 0.2);
  border-radius: 11px;
  padding: 0 11px;
  background: #fff;
  color: #34405c;
  font: inherit;
  outline: none;
}

.understanding-scope-row select:focus,
.understanding-fields input:focus,
.understanding-fields select:focus,
.understanding-effective-head select:focus {
  border-color: #8796dc;
  box-shadow: 0 0 0 3px rgba(91, 107, 207, 0.08);
}

.understanding-inherit-note {
  display: grid;
  gap: 4px;
  min-height: 42px;
  padding: 10px 13px;
  border: 1px solid rgba(85, 105, 190, 0.12);
  border-radius: 11px;
  background: rgba(237, 241, 255, 0.7);
}

.understanding-inherit-note strong {
  color: #4a5bc0;
}

.understanding-mode-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.understanding-mode-card {
  display: grid;
  gap: 7px;
  min-height: 112px;
  padding: 16px;
  text-align: left;
  border: 1px solid rgba(91, 111, 175, 0.16);
  border-radius: 14px;
  background: rgba(255, 255, 255, 0.85);
  color: #45506b;
  font: inherit;
  cursor: pointer;
  transition: 0.16s ease;
}

.understanding-mode-card strong {
  font-size: 18px;
}

.understanding-mode-card small {
  color: #7a859c;
  line-height: 1.55;
}

.understanding-mode-card.active {
  border-color: #8b9aed;
  background: linear-gradient(145deg, #edf1ff, #dfe6ff);
  color: #4254c4;
  box-shadow: 0 10px 24px rgba(72, 87, 178, 0.16), inset 0 1px 0 rgba(255, 255, 255, 0.8);
  transform: translateY(-1px);
}

.understanding-fields {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
}

.understanding-enable-field {
  align-content: end;
}

.understanding-toggle {
  min-height: 42px;
  border: 1px solid #d6dce9;
  border-radius: 11px;
  background: #f4f6fb;
  color: #69748e;
  font: inherit;
  font-weight: 850;
  cursor: pointer;
}

.understanding-toggle.active {
  border-color: #98aae9;
  background: #e9f8ef;
  color: #23815a;
}

.understanding-effective {
  display: grid;
  gap: 12px;
  padding: 14px;
  border: 1px solid rgba(91, 111, 175, 0.15);
  border-radius: 14px;
  background: rgba(246, 248, 255, 0.8);
}

.understanding-effective-head > div {
  display: grid;
  gap: 4px;
}

.understanding-effective-head select {
  width: min(360px, 100%);
}

.understanding-effective-grid {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: 10px;
}

.understanding-effective-grid > div {
  display: grid;
  gap: 5px;
  padding: 11px 12px;
  border-radius: 11px;
  background: #fff;
  border: 1px solid rgba(91, 111, 175, 0.1);
}

.understanding-effective-grid span {
  color: #8a93a7;
  font-size: 12px;
}

.understanding-effective-grid strong {
  color: #3e4b68;
  overflow-wrap: anywhere;
}

.routing-baseline-card {
  overflow: hidden;
}

.routing-baseline-card > summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
  cursor: pointer;
  list-style: none;
}

.routing-baseline-card > summary::-webkit-details-marker {
  display: none;
}

.routing-baseline-title {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.routing-baseline-title strong {
  font-size: 20px;
  color: #303a59;
}

.routing-baseline-lock {
  display: inline-flex;
  align-items: center;
  min-height: 28px;
  padding: 0 10px;
  border-radius: 999px;
  background: rgba(76, 93, 187, 0.1);
  color: #5962bd;
  font-size: 13px;
  font-weight: 800;
}

.routing-baseline-card p {
  margin: 7px 0 0;
  color: #7a839c;
}

.routing-baseline-body {
  margin-top: 16px;
  max-height: 560px;
  overflow: auto;
  border: 1px solid rgba(91, 111, 175, 0.16);
  border-radius: 14px;
  background: rgba(247, 249, 255, 0.95);
}

.routing-baseline-body pre {
  margin: 0;
  padding: 16px;
  white-space: pre-wrap;
  word-break: break-word;
}

.routing-maintenance-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(320px, 390px);
  gap: 18px;
  align-items: start;
}

.routing-editor-card,
.routing-assistant-card {
  min-width: 0;
}

.routing-editor-card header {
  gap: 18px;
}

.routing-json-editor {
  width: 100%;
  min-height: 620px;
  resize: vertical;
  border: 1px solid rgba(91, 111, 175, 0.22);
  border-radius: 14px;
  padding: 16px;
  font-family: "Cascadia Code", "Consolas", monospace;
  font-size: 14px;
  line-height: 1.6;
  background: rgba(247, 249, 255, 0.95);
  color: #28334d;
  box-sizing: border-box;
}

.routing-view-switch {
  display: inline-flex;
  padding: 3px;
  border: 1px solid rgba(91, 104, 218, 0.16);
  border-radius: 12px;
  background: rgba(236, 239, 252, 0.78);
}

.routing-view-switch button {
  border: 0;
  border-radius: 9px;
  padding: 8px 13px;
  background: transparent;
  color: #66708d;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}

.routing-view-switch button.active {
  background: #5b5fdb;
  color: #fff;
  box-shadow: 0 4px 12px rgba(91, 95, 219, 0.2);
}

.compact-button {
  padding-inline: 12px;
}

.routing-tree-editor {
  min-height: 620px;
  max-height: 760px;
  overflow: auto;
  border: 1px solid rgba(91, 111, 175, 0.22);
  border-radius: 14px;
  background: rgba(247, 249, 255, 0.95);
}

.routing-tree-note {
  position: sticky;
  top: 0;
  z-index: 2;
  padding: 11px 14px;
  border-bottom: 1px solid rgba(91, 111, 175, 0.13);
  background: rgba(239, 242, 253, 0.96);
  color: #6f7893;
  font-size: 13px;
}

.routing-assistant-card {
  position: sticky;
  top: 84px;
  display: grid;
  gap: 14px;
}

.routing-assistant-card textarea {
  width: 100%;
  resize: vertical;
  box-sizing: border-box;
}

.routing-history-list {
  display: grid;
  gap: 12px;
}

.routing-history-list article {
  border: 1px solid rgba(91, 111, 175, 0.18);
  border-radius: 14px;
  padding: 14px;
  background: rgba(248, 250, 255, 0.8);
}

.routing-history-head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: center;
}

.routing-history-head > div {
  display: flex;
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
}

.routing-history-list pre {
  max-height: 320px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-word;
  margin: 12px 0 0;
  padding: 12px;
  border-radius: 10px;
  background: rgba(231, 236, 250, 0.7);
}

@media (max-width: 1100px) {
  .routing-maintenance-layout {
    grid-template-columns: 1fr;
  }

  .routing-assistant-card {
    position: static;
  }

  .routing-status-card {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .understanding-scope-row,
  .understanding-fields {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .understanding-inherit-note {
    grid-column: 1 / -1;
  }

  .understanding-effective-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

@media (max-width: 720px) {
  .understanding-head,
  .understanding-effective-head {
    display: grid;
  }

  .understanding-head-actions {
    justify-content: start;
  }

  .understanding-mode-grid,
  .understanding-scope-row,
  .understanding-fields,
  .understanding-effective-grid,
  .routing-status-card {
    grid-template-columns: 1fr;
  }

  .understanding-effective-head select {
    width: 100%;
  }
}
</style>
