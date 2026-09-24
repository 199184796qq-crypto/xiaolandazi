<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import {
  activateLiveAgentConfigVersion,
  chatLiveRoomPolicyAgent,
  chatLiveAgent,
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
const input = ref('')
const enterToSend = ref(localStorage.getItem('live-agent-enter-to-send') !== 'false')
const defaultSettings: LiveAgentSettings = {
  tenant_id: 0,
  display_name: '小伴直播教练',
  role_name: '直播策略与场控 Agent',
  self_introduction: '我是小伴直播教练，是你的直播策略与场控 Agent。',
  mission: '我负责直播策略调教、主播训练、固定话术、声音配置和现场场控协作。终端用户的调整只写入当前直播间第3层策略，不修改系统层和行业层。',
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
const sending = ref(false)
const uploading = ref(false)
const documentInput = ref<HTMLInputElement | null>(null)
const audioInput = ref<HTMLInputElement | null>(null)
const referenceAudioInput = ref<HTMLInputElement | null>(null)
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

const versionLabel = computed(() => {
  if (activeMode.value === 'strategy') {
    if (roomPolicyDraft.value) return 'L3 草稿 V' + roomPolicyDraft.value.version_no
    if (roomPolicyActive.value) return 'L3 已发布 V' + roomPolicyActive.value.version_no
    return 'L3 未版本化'
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
      settingsError.value = err instanceof Error ? err.message : '读取直播间 L3 策略失败'
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

function setEnterToSend(event: Event) {
  const value = (event.target as HTMLInputElement).checked
  enterToSend.value = value
  localStorage.setItem('live-agent-enter-to-send', String(value))
}

function handleInputKeydown(event: KeyboardEvent) {
  if (event.key !== 'Enter' || event.isComposing) return
  if (enterToSend.value && !event.shiftKey) {
    event.preventDefault()
    send()
    return
  }
  if (!enterToSend.value && (event.ctrlKey || event.metaKey)) {
    event.preventDefault()
    send()
  }
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

async function persistInstruction(value: string) {
  const roomId = activeRoomId.value
  if (!roomId || activeMode.value === 'basic') return null
  const now = new Date().toISOString()
  const latest = latestConfig.value
  const payload: LiveAgentConfigInput = {}

  if (activeMode.value === 'strategy') {
    payload.layer3 = roomScopedRecord(latest?.layer3, roomId, 'strategy_entries', {
      text: value,
      created_at: now,
    })
  } else if (activeMode.value === 'script') {
    const normalized = value.replace(/\s/g, '')
    const executionMode =
      normalized.includes('100%原话') ||
      normalized.includes('照原文') ||
      normalized.includes('原话锁定')
        ? 'exact'
        : 'intent'
    payload.layer3 = roomScopedRecord(latest?.layer3, roomId, 'fixed_scripts', {
      text: value,
      execution_mode: executionMode,
      created_at: now,
    })
  } else if (activeMode.value === 'anchor') {
    payload.style_profile = roomScopedRecord(latest?.style_profile, roomId, 'training_entries', {
      text: value,
      created_at: now,
    })
  } else if (activeMode.value === 'voice') {
    payload.speech_config = roomScopedRecord(latest?.speech_config, roomId, 'instruction_entries', {
      text: value,
      created_at: now,
    })
  }

  const draft = await createLiveAgentConfigDraft(payload)
  configVersions.value = [draft, ...configVersions.value.filter((item) => item.id !== draft.id)]
  selectedVersionId.value = draft.id
  return draft
}

async function publishLatestDraft() {
  if (activeMode.value === 'strategy') {
    const draft = roomPolicyDraft.value
    const roomId = activeRoomId.value
    if (!draft || !roomId || settingsSaving.value || draft.conflicts.length) return
    settingsSaving.value = true
    settingsError.value = ''
    try {
      await publishLiveRoomPolicyVersion(roomId, draft.id)
      await refreshRoomPolicy()
      messages.value.push({
        role: 'agent',
        text: 'L3 草稿 V' + draft.version_no + ' 已发布，当前直播间后续运行会按新的有效策略加载。',
      })
    } catch (err) {
      settingsError.value = err instanceof Error ? err.message : '发布 L3 策略失败'
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

async function send() {
  const value = input.value.trim()
  const roomId = activeRoomId.value
  if (!value || sending.value) return
  if (activeMode.value === 'basic') {
    messages.value.push({
      role: 'agent',
      text: '基础身份请直接在上方“基础设置”里修改并保存。',
    })
    return
  }
  if (!roomId) {
    settingsError.value = '请先选择直播间'
    return
  }

  const history = messages.value.slice(-10)
  messages.value.push({ role: 'user', text: value })
  input.value = ''
  sending.value = true
  settingsError.value = ''
  try {
    if (activeMode.value === 'strategy') {
      const response = await chatLiveRoomPolicyAgent(roomId, {
        message: value,
        history,
      })
      messages.value.push({
        role: 'agent',
        text: response.draft
          ? response.reply + '\n\n已保存为 L3 草稿 V' + response.draft.version_no + '，发布后正式生效。'
          : response.reply,
      })
      if (response.draft) await refreshRoomPolicy()
    } else {
      const draft = await persistInstruction(value)
      const response = await chatLiveAgent(roomId, {
        message: value,
        history,
      })
      messages.value.push({
        role: 'agent',
        text: draft
          ? response.reply + '\n\n已保存为草稿 V' + draft.version_no + '，发布后正式生效。'
          : response.reply,
      })
    }
  } catch (err) {
    const message = err instanceof Error ? err.message : 'Agent 处理失败'
    settingsError.value = message
    messages.value.push({ role: 'agent', text: '处理失败：' + message })
  } finally {
    sending.value = false
  }
}

watch(activeRoomId, () => {
  void refreshRoomPolicy()
})

watch(
  activeRoomId,
  (roomId) => {
    if (roomId) {
      window.localStorage.setItem('system-agent-live-room-id', String(roomId))
    }
  },
  { immediate: true },
)

watch(
  activeMode,
  (mode) => {
    window.localStorage.setItem('system-agent-live-mode', mode)
  },
  { immediate: true },
)

onMounted(loadAgentSettings)
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
      </aside>

      <main class="live-strategy-agent">
        <header class="strategy-agent-head">
          <div>
            <span class="section-kicker">{{ settings.role_name }}</span>
            <h2>{{ settings.display_name }}</h2>
            <p>当前配置：{{ activeRoom?.name }} · 所有用户策略写入当前直播间第 3 层</p>
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
              :disabled="settingsSaving || (activeMode === 'strategy' && !!roomPolicyDraft?.conflicts.length)"
              @click="publishLatestDraft"
            >
              {{ activeMode === 'strategy' && roomPolicyDraft?.conflicts.length ? '存在冲突' : '发布草稿' }}
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

        <section v-show="activeMode !== 'basic'" class="strategy-chat">
          <div v-if="settingsError" class="inline-error strategy-inline-error">
            {{ settingsError }}
          </div>
          <article
            v-for="(message, index) in messages"
            :key="index"
            :class="['strategy-message', message.role]"
          >
            <strong>{{ message.role === 'agent' ? settings.display_name : '我' }}</strong>
            <p>{{ message.text }}</p>
          </article>
        </section>

        <footer v-if="activeMode !== 'basic'" class="strategy-composer">
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
            <input
              ref="referenceAudioInput"
              type="file"
              hidden
              accept="audio/*"
              @change="handleMediaUpload($event, 'voice_sample')"
            />
            <button type="button" :disabled="uploading" @click="documentInput?.click()">＋ 文案</button>
            <button type="button" :disabled="uploading" @click="audioInput?.click()">＋ 录音</button>
            <button type="button" :disabled="uploading" @click="referenceAudioInput?.click()">＋ 参考音</button>
            <span>{{ uploading ? '素材上传中…' : '配置写入 MySQL · 媒体走统一存储' }}</span>
            <label class="enter-send-toggle">
              <input
                type="checkbox"
                :checked="enterToSend"
                @change="setEnterToSend"
              />
              Enter 发送
            </label>
          </div>
          <div class="strategy-input-row">
            <textarea
              v-model="input"
              rows="3"
              placeholder="告诉 Agent 你想怎么调整这个直播间……"
              :disabled="sending"
              @keydown="handleInputKeydown"
            ></textarea>
            <button class="primary-button" type="button" :disabled="sending" @click="send">
              {{ sending ? '处理中…' : '发送' }}
            </button>
          </div>
        </footer>
      </main>
    </section>
  </div>
</template>
