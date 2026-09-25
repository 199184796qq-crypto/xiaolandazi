<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import SupportAssistantPicker from '../components/SupportAssistantPicker.vue'
import LiveVoiceCenter from '../components/LiveVoiceCenter.vue'
import {
  activateLiveAgentConfigVersion,
  createLiveAgentConfigDraft,
  getLiveAgentConfigVersions,
  getLiveAgentSettings,
  getLiveRoomPolicyContext,
  getRooms,
  publishLiveRoomPolicyVersion,
  updateLiveAgentSettings,
  uploadLiveMediaAsset,
} from '../api'
import type {
  LiveAgentConfigInput,
  LiveAgentConfigVersion,
  LiveAgentSettings,
  LiveAgentSettingsInput,
  LiveRoomPolicyContext,
  Room,
} from '../types'

const rooms = ref<Room[]>([])
const activeRoomId = ref<number | null>(null)
const activeMode = ref<'basic' | 'strategy' | 'anchor' | 'script' | 'voice'>('strategy')
const defaultSettings: LiveAgentSettings = {
  tenant_id: 0,
  display_name: '小伴直播教练',
  role_name: '直播策略与场控 Agent',
  self_introduction: '我是小伴直播教练，是你的直播策略与场控 Agent。',
  mission: '我负责直播策略调教、主播训练、固定话术、声音配置和现场场控协作。终端用户的策略调整只作用于当前直播间用户层，不修改规则层和行业层。',
  greeting: '你好，我是小伴直播教练。你可以直接告诉我想调整主播表达、固定话术、声音或直播策略。',
}
const settings = ref<LiveAgentSettings>({ ...defaultSettings })
const settingsDraft = ref<LiveAgentSettingsInput>({
  display_name: defaultSettings.display_name,
  role_name: defaultSettings.role_name,
  self_introduction: defaultSettings.self_introduction,
  mission: defaultSettings.mission,
  greeting: defaultSettings.greeting,
})
const settingsSaving = ref(false)
const settingsError = ref('')
const configVersions = ref<LiveAgentConfigVersion[]>([])
const roomPolicyContext = ref<LiveRoomPolicyContext | null>(null)
const selectedVersionId = ref<number | null>(null)
const uploading = ref(false)
const documentInput = ref<HTMLInputElement | null>(null)
const audioInput = ref<HTMLInputElement | null>(null)
const messages = ref<Array<{ role: 'agent' | 'user'; text: string }>>([
  { role: 'agent', text: defaultSettings.greeting },
])
const activeRoom = computed(() => rooms.value.find((item) => item.id === activeRoomId.value) || rooms.value[0])
const activeConfig = computed(
  () => configVersions.value.find((item) => item.lifecycle_status === 'active') || null,
)
const latestConfig = computed(
  () =>
    configVersions.value.find((item) => item.lifecycle_status === 'draft') ||
    activeConfig.value ||
    configVersions.value[0] ||
    null,
)
const roomPolicyDraft = computed(
  () => roomPolicyContext.value?.l3_versions.find((item) => item.lifecycle_status === 'draft') || null,
)
const roomPolicyActive = computed(
  () => roomPolicyContext.value?.l3_versions.find((item) => item.lifecycle_status === 'active') || null,
)

const activeModeLabel = computed(() => {
  if (activeMode.value === 'basic') return '基础设置'
  if (activeMode.value === 'strategy') return '用户层策略'
  if (activeMode.value === 'anchor') return '主播训练'
  if (activeMode.value === 'script') return '固定话术'
  return '声音配置'
})

const versionLabel = computed(() => {
  if (activeMode.value === 'strategy') {
    if (roomPolicyDraft.value) return '用户层草稿 V' + roomPolicyDraft.value.version_no
    if (roomPolicyActive.value) return '用户层已发布 V' + roomPolicyActive.value.version_no
    return '用户层未版本化'
  }
  const latest = latestConfig.value
  if (!latest) return '未版本化'
  if (latest.lifecycle_status === 'draft') return '草稿 V' + latest.version_no
  if (latest.lifecycle_status === 'active') return '已发布 V' + latest.version_no
  return 'V' + latest.version_no + ' · ' + latest.lifecycle_status
})

function settingsToInput(value: LiveAgentSettings): LiveAgentSettingsInput {
  return {
    display_name: value.display_name,
    role_name: value.role_name,
    self_introduction: value.self_introduction,
    mission: value.mission,
    greeting: value.greeting,
  }
}

async function refreshRoomPolicy() {
  const roomId = activeRoomId.value
  if (!roomId) {
    roomPolicyContext.value = null
    return
  }
  try {
    roomPolicyContext.value = await getLiveRoomPolicyContext(roomId)
  } catch (err) {
    if (!settingsError.value) {
      settingsError.value = err instanceof Error ? err.message : '读取直播间用户层策略失败'
    }
  }
}

async function loadAgentSettings() {
  settingsError.value = ''

  try {
    const roomData = await getRooms()
    rooms.value = roomData.items
    if (!activeRoomId.value || !rooms.value.some((room) => room.id === activeRoomId.value)) {
      activeRoomId.value = rooms.value[0]?.id || null
    }
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '读取直播间失败'
  }

  try {
    const value = await getLiveAgentSettings()
    settings.value = value
    settingsDraft.value = settingsToInput(value)
    messages.value = [{ role: 'agent', text: value.greeting }]
  } catch (err) {
    if (!settingsError.value) {
      settingsError.value = err instanceof Error ? err.message : '读取基础设置失败'
    }
  }

  await refreshRoomPolicy()
  await refreshAgentVersions()
}

async function refreshAgentVersions() {
  try {
    configVersions.value = await getLiveAgentConfigVersions()
    const selectedStillExists = configVersions.value.some((item) => item.id === selectedVersionId.value)
    if (!selectedStillExists) {
      selectedVersionId.value =
        configVersions.value.find((item) => item.lifecycle_status === 'active')?.id ||
        configVersions.value[0]?.id ||
        null
    }
  } catch (err) {
    if (!settingsError.value) {
      settingsError.value = err instanceof Error ? err.message : '读取配置版本失败'
    }
  }
}

async function saveAgentSettings() {
  settingsSaving.value = true
  settingsError.value = ''
  try {
    const value = await updateLiveAgentSettings(settingsDraft.value)
    settings.value = value
    settingsDraft.value = settingsToInput(value)
    await refreshAgentVersions()
    messages.value.push({
      role: 'agent',
      text: '基础设置已保存。以后问我“你是谁”“你叫什么”“你能做什么”，我都会按这套身份回答。',
    })
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '保存基础设置失败'
  } finally {
    settingsSaving.value = false
  }
}

function roomStatusLabel(status: string) {
  if (status === 'live') return '直播中'
  if (status === 'connecting') return '连接中'
  if (status === 'pending') return '等待连接'
  if (status === 'offline') return '未开播'
  if (status === 'error') return '连接异常'
  return status || '未知'
}

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
  return { ...(value as Record<string, unknown>) }
}

function asArray(value: unknown): unknown[] {
  return Array.isArray(value) ? [...value] : []
}

function roomScopedRecord(
  source: Record<string, unknown> | undefined,
  roomId: number,
  key: string,
  entry: Record<string, unknown>,
) {
  const root = asRecord(source)
  const roomMap = asRecord(root.rooms)
  const roomKey = String(roomId)
  const roomConfig = asRecord(roomMap[roomKey])
  return {
    ...root,
    rooms: {
      ...roomMap,
      [roomKey]: {
        ...roomConfig,
        [key]: [...asArray(roomConfig[key]), entry],
      },
    },
  }
}

async function publishLatestDraft() {
  if (activeMode.value === 'strategy') {
    const draft = roomPolicyDraft.value
    const roomId = activeRoomId.value
    if (!draft || !roomId || settingsSaving.value || (draft.conflicts?.length ?? 0)) return
    settingsSaving.value = true
    settingsError.value = ''
    try {
      await publishLiveRoomPolicyVersion(roomId, draft.id)
      await refreshRoomPolicy()
      messages.value.push({
        role: 'agent',
        text: '用户层草稿 V' + draft.version_no + ' 已发布，当前直播间后续运行会按新的有效策略加载。',
      })
    } catch (err) {
      settingsError.value = err instanceof Error ? err.message : '发布用户层策略失败'
    } finally {
      settingsSaving.value = false
    }
    return
  }

  const draft = configVersions.value.find((item) => item.lifecycle_status === 'draft')
  if (!draft || settingsSaving.value) return
  settingsSaving.value = true
  settingsError.value = ''
  try {
    await activateLiveAgentConfigVersion(draft.id)
    await refreshAgentVersions()
    messages.value.push({
      role: 'agent',
      text: '配置 V' + draft.version_no + ' 已发布，当前直播运行可按这个版本加载。',
    })
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '发布配置失败'
  } finally {
    settingsSaving.value = false
  }
}

async function activateSelectedVersion() {
  if (!selectedVersionId.value || selectedVersionId.value === activeConfig.value?.id) return
  settingsSaving.value = true
  settingsError.value = ''
  try {
    await activateLiveAgentConfigVersion(selectedVersionId.value)
    await refreshAgentVersions()
    const value = await getLiveAgentSettings()
    settings.value = value
    settingsDraft.value = settingsToInput(value)
    const active = activeConfig.value
    messages.value.push({
      role: 'agent',
      text: active ? `已切换到配置 V${active.version_no}。` : '配置版本已切换。',
    })
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '切换配置版本失败'
  } finally {
    settingsSaving.value = false
  }
}

async function handleMediaUpload(event: Event, assetType: 'document' | 'audio' | 'voice_sample') {
  const target = event.target as HTMLInputElement
  const file = target.files?.[0]
  target.value = ''
  const roomId = activeRoomId.value
  if (!file || !roomId || uploading.value) return

  uploading.value = true
  settingsError.value = ''
  try {
    const result = await uploadLiveMediaAsset(file, assetType, {
      metadata: { room_id: roomId, source: 'live_strategy_agent' },
    })
    const entry = {
      asset_id: result.asset.id,
      name: result.asset.original_name,
      asset_type: result.asset.asset_type,
      created_at: result.asset.created_at,
    }
    const latest = latestConfig.value
    const payload: LiveAgentConfigInput =
      assetType === 'document'
        ? { layer3: roomScopedRecord(latest?.layer3, roomId, 'attachments', entry) }
        : { speech_config: roomScopedRecord(latest?.speech_config, roomId, 'audio_assets', entry) }
    const draft = await createLiveAgentConfigDraft(payload)
    configVersions.value = [draft, ...configVersions.value.filter((item) => item.id !== draft.id)]
    selectedVersionId.value = draft.id
    messages.value.push({
      role: 'agent',
      text: `“${file.name}”已保存为媒体资产，并关联到草稿 V${draft.version_no}。`,
    })
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '上传媒体文件失败'
  } finally {
    uploading.value = false
  }
}

function notifySystemAgentContext() {
  window.dispatchEvent(
    new CustomEvent('system-agent-live-strategy-context', {
      detail: {
        room_id: activeRoomId.value || 0,
        mode: activeMode.value,
      },
    }),
  )
}

watch(activeRoomId, () => {
  void refreshRoomPolicy()
})

watch(
  activeRoomId,
  (roomId) => {
    if (roomId) window.localStorage.setItem('system-agent-live-room-id', String(roomId))
    notifySystemAgentContext()
  },
  { immediate: true },
)

watch(
  activeMode,
  (mode) => {
    window.localStorage.setItem('system-agent-live-mode', mode)
    notifySystemAgentContext()
  },
  { immediate: true },
)

onMounted(async () => {
  await loadAgentSettings()
})
</script>

<template>
  <div class="live-strategy-page">
    <ModulePageNav context="live" active-title="直播策略" active-nav-title="直播运维" />

    <section class="live-strategy-shell">
      <aside class="live-strategy-rooms">
        <div class="strategy-panel-title">
          <span class="section-kicker">MY ROOMS</span>
          <h2>我的直播间</h2>
        </div>
        <button
          v-for="room in rooms"
          :key="room.id"
          class="strategy-room-card"
          :class="{ active: activeRoomId === room.id }"
          type="button"
          @click="activeRoomId = room.id"
        >
          <span class="strategy-room-icon">播</span>
          <span>
            <strong>{{ room.name }}</strong>
            <small>{{ room.platform }} · {{ roomStatusLabel(room.status) }}</small>
          </span>
        </button>
        <section class="live-support-authorize-panel">
          <SupportAssistantPicker :room-id="activeRoomId" />
        </section>
      </aside>

      <main class="live-strategy-agent">
        <header class="strategy-agent-head">
          <div>
            <span class="section-kicker">{{ settings.role_name }}</span>
            <h2>{{ settings.display_name }}</h2>
            <p>当前直播间：{{ activeRoom?.name || '未选择' }} · {{ activeModeLabel }}</p>
          </div>
          <div class="strategy-version-actions">
            <span class="strategy-version">{{ versionLabel }}</span>
            <select
              v-if="activeMode !== 'strategy' && configVersions.length"
              v-model.number="selectedVersionId"
              class="strategy-version-select"
              :disabled="settingsSaving"
            >
              <option
                v-for="version in configVersions"
                :key="version.id"
                :value="version.id"
              >
                V{{ version.version_no }} · {{ version.lifecycle_status }}
              </option>
            </select>
            <button
              v-if="activeMode !== 'strategy' && selectedVersionId && selectedVersionId !== activeConfig?.id"
              class="strategy-version-button"
              type="button"
              :disabled="settingsSaving"
              @click="activateSelectedVersion"
            >
              切换版本
            </button>
            <button
              v-if="activeMode === 'strategy' ? !!roomPolicyDraft : latestConfig?.lifecycle_status === 'draft'"
              class="strategy-version-button primary"
              type="button"
              :disabled="settingsSaving || (activeMode === 'strategy' && !!roomPolicyDraft?.conflicts?.length)"
              @click="publishLatestDraft"
            >
              {{ activeMode === 'strategy' && roomPolicyDraft?.conflicts?.length ? '存在冲突' : '发布草稿' }}
            </button>
          </div>
        </header>

        <div class="strategy-mode-tabs">
          <button :class="{ active: activeMode === 'basic' }" @click="activeMode = 'basic'">基础设置</button>
          <button :class="{ active: activeMode === 'strategy' }" @click="activeMode = 'strategy'">策略调教</button>
          <button :class="{ active: activeMode === 'anchor' }" @click="activeMode = 'anchor'">主播训练</button>
          <button :class="{ active: activeMode === 'script' }" @click="activeMode = 'script'">固定话术</button>
          <button :class="{ active: activeMode === 'voice' }" @click="activeMode = 'voice'">声音配置</button>
        </div>

        <section v-if="activeMode === 'basic'" class="strategy-basic-settings">
          <div class="strategy-basic-settings-head">
            <div>
              <span class="section-kicker">AGENT IDENTITY</span>
              <h3>基础设置</h3>
              <p>定义这个 Agent 叫什么、是谁、负责什么。这里是当前客户共用设置，所有直播间统一使用。</p>
            </div>
            <button
              class="primary-button"
              type="button"
              :disabled="settingsSaving"
              @click="saveAgentSettings"
            >
              {{ settingsSaving ? '保存中…' : '保存基础设置' }}
            </button>
          </div>

          <div v-if="settingsError" class="inline-error">{{ settingsError }}</div>

          <div class="strategy-basic-grid">
            <label>
              <span>Agent 名称</span>
              <input
                v-model="settingsDraft.display_name"
                maxlength="128"
                placeholder="例如：小伴直播教练"
              />
            </label>
            <label>
              <span>身份定位</span>
              <input
                v-model="settingsDraft.role_name"
                maxlength="160"
                placeholder="例如：直播策略与场控 Agent"
              />
            </label>
            <label class="wide">
              <span>自我介绍</span>
              <textarea
                v-model="settingsDraft.self_introduction"
                rows="2"
                maxlength="600"
                placeholder="用户问“你是谁”时，优先按这里回答。"
              ></textarea>
            </label>
            <label class="wide">
              <span>主要职责与边界</span>
              <textarea
                v-model="settingsDraft.mission"
                rows="3"
                maxlength="1200"
                placeholder="说明能做什么、负责什么，以及不能越过哪些系统边界。"
              ></textarea>
            </label>
            <label class="wide">
              <span>首次问候语</span>
              <textarea
                v-model="settingsDraft.greeting"
                rows="2"
                maxlength="600"
                placeholder="进入直播策略 Agent 后第一句话。"
              ></textarea>
            </label>
          </div>

          <div class="strategy-identity-preview">
            <span>身份预览</span>
            <strong>{{ settingsDraft.display_name || '未命名 Agent' }}</strong>
            <p>{{ settingsDraft.self_introduction || '还没有填写自我介绍。' }}</p>
            <small>{{ settingsDraft.mission }}</small>
          </div>
        </section>

        <LiveVoiceCenter
          v-if="activeMode === 'voice'"
          :room-id="activeRoomId"
          :room-name="activeRoom?.name"
          @changed="refreshAgentVersions"
        />

        <section v-else-if="activeMode !== 'basic'" class="strategy-agent-workspace">
          <div v-if="settingsError" class="inline-error strategy-inline-error">{{ settingsError }}</div>
          <div class="strategy-agent-context-card">
            <span class="section-kicker">CURRENT CONTEXT</span>
            <strong>{{ activeModeLabel }}</strong>
            <small>使用页面底部小蓝输入框进行调整</small>
          </div>
          <div class="strategy-upload-row">
            <input
              ref="documentInput"
              type="file"
              hidden
              accept=".txt,.md,.doc,.docx,.pdf,text/plain,text/markdown,application/pdf"
              @change="handleMediaUpload($event, 'document')"
            />
            <input
              ref="audioInput"
              type="file"
              hidden
              accept="audio/*"
              @change="handleMediaUpload($event, 'audio')"
            />
            <button type="button" :disabled="uploading" @click="documentInput?.click()">＋ 文案</button>
            <button type="button" :disabled="uploading" @click="audioInput?.click()">＋ 录音</button>
            <span v-if="uploading">素材上传中…</span>
          </div>
        </section>
      </main>
    </section>
  </div>
</template>
