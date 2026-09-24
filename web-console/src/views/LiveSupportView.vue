<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import { session } from '../session'
import { canDelegateLivePolicyL3, canManageLivePolicyL1 } from '../livePolicyAccess'
import {
  activateLiveOpsSupportConfigVersion,
  createLiveOpsAnchorTraining,
  createLiveOpsSupportVoiceProfile,
  getLiveOpsSupportAuthorizations,
  getLiveOpsSupportVoiceProfiles,
  getLiveRoomPolicyContext,
  getRooms,
  publishLiveRoomPolicyVersion,
  uploadLiveOpsSupportMediaAsset,
} from '../api'
import type {
  LiveRoomPolicyContext,
  LiveSupportAuthorization,
  LiveSupportCapability,
  Room,
  VoiceProfile,
} from '../types'

type SupportMode = 'strategy' | 'anchor' | 'voice'

const props = withDefaults(defineProps<{ embedded?: boolean }>(), {
  embedded: false,
})

const rooms = ref<Room[]>([])
const authorizations = ref<LiveSupportAuthorization[]>([])
const activeRoomId = ref<number | null>(null)
const activeMode = ref<SupportMode>('strategy')
const loading = ref(false)
const error = ref('')
const policyContext = ref<LiveRoomPolicyContext | null>(null)

const anchorText = ref('')
const anchorAssetIds = ref<number[]>([])
const anchorAssetNames = ref<string[]>([])
const anchorDraftId = ref<number | null>(null)
const anchorDraftVersion = ref<number | null>(null)
const anchorUploading = ref(false)
const anchorDocumentInput = ref<HTMLInputElement | null>(null)
const anchorAudioInput = ref<HTMLInputElement | null>(null)

const voiceSampleInput = ref<HTMLInputElement | null>(null)
const voiceUploading = ref(false)
const voiceSampleAssetId = ref<number | null>(null)
const voiceSampleName = ref('')
const voiceName = ref('')
const voiceProvider = ref('')
const voiceID = ref('')
const voiceProfiles = ref<VoiceProfile[]>([])
const voiceSaving = ref(false)

const canDelegateL3 = computed(() => canDelegateLivePolicyL3(session.bootstrap))
const isL1Configurator = computed(() => canManageLivePolicyL1(session.bootstrap))
const eligibleAuthorizations = computed(() => authorizations.value.filter(
  (item) => item.status === 'active' && (item.capability !== 'l3_policy' || canDelegateL3.value),
))
const authorizedRoomIds = computed(
  () => new Set(eligibleAuthorizations.value.map((item) => item.room_id)),
)
const supportRooms = computed(() =>
  rooms.value.filter((room) => authorizedRoomIds.value.has(room.id)),
)
const activeRoom = computed(
  () => supportRooms.value.find((room) => room.id === activeRoomId.value) || null,
)
const activeCapabilities = computed(() => {
  const result = new Set<LiveSupportCapability>()
  const roomId = activeRoomId.value
  if (!roomId) return result
  for (const item of eligibleAuthorizations.value) {
    if (item.room_id !== roomId || item.status !== 'active') continue
    result.add(item.capability as LiveSupportCapability)
  }
  return result
})
const canStrategy = computed(() => activeCapabilities.value.has('l3_policy'))
const canAnchor = computed(() => activeCapabilities.value.has('anchor_training'))
const canVoice = computed(() => activeCapabilities.value.has('voice_clone'))
const policyDraft = computed(
  () => policyContext.value?.l3_versions.find((item) => item.lifecycle_status === 'draft') || null,
)

function capabilityLabel(capability: LiveSupportCapability) {
  if (capability === 'l3_policy') return 'L3策略'
  if (capability === 'anchor_training') return '主播训练'
  return '声音复刻'
}

function roomStatusLabel(status: string) {
  if (status === 'live') return '直播中'
  if (status === 'connecting') return '连接中'
  if (status === 'offline') return '未开播'
  if (status === 'error') return '连接异常'
  return status || '未知'
}

function chooseAllowedMode() {
  if (activeMode.value === 'strategy' && canStrategy.value) return
  if (activeMode.value === 'anchor' && canAnchor.value) return
  if (activeMode.value === 'voice' && canVoice.value) return
  if (canStrategy.value) activeMode.value = 'strategy'
  else if (canAnchor.value) activeMode.value = 'anchor'
  else activeMode.value = 'voice'
}

async function loadSupportWorkspace() {
  loading.value = true
  error.value = ''
  try {
    const [roomResult, authorizationResult] = await Promise.all([
      getRooms(),
      getLiveOpsSupportAuthorizations(),
    ])
    rooms.value = roomResult.items
    authorizations.value = authorizationResult.items
    if (!supportRooms.value.some((room) => room.id === activeRoomId.value)) {
      activeRoomId.value = supportRooms.value[0]?.id || null
    }
    chooseAllowedMode()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取客户授权工作台失败'
  } finally {
    loading.value = false
  }
}

async function refreshPolicy() {
  const roomId = activeRoomId.value
  if (!roomId || !canStrategy.value) {
    policyContext.value = null
    return
  }
  try {
    policyContext.value = await getLiveRoomPolicyContext(roomId)
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取客户 L3 策略失败'
  }
}

async function refreshVoiceProfiles() {
  const roomId = activeRoomId.value
  if (!roomId || !canVoice.value) {
    voiceProfiles.value = []
    return
  }
  try {
    const result = await getLiveOpsSupportVoiceProfiles(roomId)
    voiceProfiles.value = result.items
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取客户声音档案失败'
  }
}

async function publishPolicyDraft() {
  const roomId = activeRoomId.value
  const draft = policyDraft.value
  if (!roomId || !draft || (draft.conflicts?.length ?? 0) || !canStrategy.value) return
  loading.value = true
  error.value = ''
  try {
    await publishLiveRoomPolicyVersion(roomId, draft.id)
    await refreshPolicy()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '发布客户 L3 策略失败'
  } finally {
    loading.value = false
  }
}

async function uploadAnchorAsset(event: Event, type: 'document' | 'audio') {
  const roomId = activeRoomId.value
  const target = event.target as HTMLInputElement
  const file = target.files?.[0]
  target.value = ''
  if (!roomId || !file || anchorUploading.value || !canAnchor.value) return
  anchorUploading.value = true
  error.value = ''
  try {
    const result = await uploadLiveOpsSupportMediaAsset(roomId, file, type)
    anchorAssetIds.value.push(result.asset.id)
    anchorAssetNames.value.push(result.asset.original_name)
  } catch (err) {
    error.value = err instanceof Error ? err.message : '上传主播训练素材失败'
  } finally {
    anchorUploading.value = false
  }
}

async function saveAnchorTraining() {
  const roomId = activeRoomId.value
  if (!roomId || !canAnchor.value || (!anchorText.value.trim() && !anchorAssetIds.value.length)) return
  loading.value = true
  error.value = ''
  try {
    const result = await createLiveOpsAnchorTraining(roomId, {
      text: anchorText.value.trim(),
      asset_ids: anchorAssetIds.value,
    })
    anchorDraftId.value = result.version_id
    anchorDraftVersion.value = result.version_no
    anchorText.value = ''
    anchorAssetIds.value = []
    anchorAssetNames.value = []
  } catch (err) {
    error.value = err instanceof Error ? err.message : '保存主播训练草稿失败'
  } finally {
    loading.value = false
  }
}

async function publishAnchorTraining() {
  const roomId = activeRoomId.value
  const versionId = anchorDraftId.value
  if (!roomId || !versionId || !canAnchor.value) return
  loading.value = true
  error.value = ''
  try {
    await activateLiveOpsSupportConfigVersion(roomId, versionId)
    anchorDraftId.value = null
    anchorDraftVersion.value = null
  } catch (err) {
    error.value = err instanceof Error ? err.message : '发布主播训练配置失败'
  } finally {
    loading.value = false
  }
}

async function uploadVoiceSample(event: Event) {
  const roomId = activeRoomId.value
  const target = event.target as HTMLInputElement
  const file = target.files?.[0]
  target.value = ''
  if (!roomId || !file || voiceUploading.value || !canVoice.value) return
  voiceUploading.value = true
  error.value = ''
  try {
    const result = await uploadLiveOpsSupportMediaAsset(roomId, file, 'voice_sample')
    voiceSampleAssetId.value = result.asset.id
    voiceSampleName.value = result.asset.original_name
  } catch (err) {
    error.value = err instanceof Error ? err.message : '上传声音样本失败'
  } finally {
    voiceUploading.value = false
  }
}

async function saveVoiceProfile() {
  const roomId = activeRoomId.value
  if (
    !roomId ||
    !canVoice.value ||
    voiceSaving.value ||
    !voiceName.value.trim() ||
    !voiceProvider.value.trim() ||
    !voiceSampleAssetId.value
  ) {
    return
  }
  voiceSaving.value = true
  error.value = ''
  try {
    await createLiveOpsSupportVoiceProfile(roomId, {
      name: voiceName.value.trim(),
      provider: voiceProvider.value.trim(),
      voice_id: voiceID.value.trim(),
      sample_asset_id: voiceSampleAssetId.value,
      clone_status: 'pending',
    })
    voiceName.value = ''
    voiceID.value = ''
    voiceSampleAssetId.value = null
    voiceSampleName.value = ''
    await refreshVoiceProfiles()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '创建声音复刻档案失败'
  } finally {
    voiceSaving.value = false
  }
}

watch(activeRoomId, async (roomId) => {
  if (roomId) {
    window.localStorage.setItem('system-agent-live-support-room-id', String(roomId))
  } else {
    window.localStorage.removeItem('system-agent-live-support-room-id')
  }
  window.dispatchEvent(
    new CustomEvent('system-agent-live-support-context', {
      detail: { room_id: roomId || 0, mode: activeMode.value },
    }),
  )
  chooseAllowedMode()
  anchorDraftId.value = null
  anchorDraftVersion.value = null
  await Promise.all([refreshPolicy(), refreshVoiceProfiles()])
})

watch(
  activeMode,
  (mode) => {
    window.localStorage.setItem('system-agent-live-support-mode', mode)
    window.dispatchEvent(
      new CustomEvent('system-agent-live-support-context', {
        detail: { room_id: activeRoomId.value || 0, mode },
      }),
    )
    if (mode === 'strategy') void refreshPolicy()
    if (mode === 'voice') void refreshVoiceProfiles()
  },
  { immediate: true },
)

onMounted(async () => {
  await loadSupportWorkspace()
  await Promise.all([refreshPolicy(), refreshVoiceProfiles()])
})
</script>

<template>
  <div class="live-strategy-page">
    <ModulePageNav v-if="!props.embedded" context="live" active-title="客户协助" active-nav-title="直播运维" />

    <section class="live-strategy-shell live-support-workspace">
      <aside class="live-strategy-rooms">
        <div class="strategy-panel-title">
          <span class="section-kicker">AUTHORIZED ROOMS</span>
          <h2>客户授权直播间</h2>
          <p>这里只显示客户明确授权给你的直播间。</p>
        </div>

        <button
          v-for="room in supportRooms"
          :key="room.id"
          class="strategy-room-card"
          :class="{ active: activeRoomId === room.id }"
          type="button"
          @click="activeRoomId = room.id"
        >
          <span class="strategy-room-icon">协</span>
          <span>
            <strong>{{ room.name }}</strong>
            <small>{{ room.platform }} · {{ roomStatusLabel(room.status) }}</small>
          </span>
        </button>

        <div v-if="!loading && !supportRooms.length" class="empty-state">
          暂无客户授权的直播间。
        </div>
      </aside>

      <main class="live-strategy-agent">
        <header class="strategy-agent-head">
          <div>
            <span class="section-kicker">AUTHORIZED CUSTOMER SUPPORT</span>
            <h2>客户直播协助</h2>
            <p v-if="activeRoom">当前直播间：{{ activeRoom.name }}。所有操作都会记录员工与客户授权链路。</p>
            <p v-else>等待客户授权指定直播间后即可协助。</p>
          </div>
          <div v-if="activeRoom" class="live-support-capability-tags">
            <span
              v-for="capability in Array.from(activeCapabilities)"
              :key="capability"
            >
              {{ capabilityLabel(capability) }}
            </span>
          </div>
        </header>

        <div v-if="activeRoom" class="strategy-mode-tabs">
          <button
            v-if="canStrategy"
            :class="{ active: activeMode === 'strategy' }"
            @click="activeMode = 'strategy'"
          >L3策略调教</button>
          <button
            v-if="canAnchor"
            :class="{ active: activeMode === 'anchor' }"
            @click="activeMode = 'anchor'"
          >主播训练</button>
          <button
            v-if="canVoice"
            :class="{ active: activeMode === 'voice' }"
            @click="activeMode = 'voice'"
          >声音复刻</button>
        </div>

        <div v-if="error" class="inline-error strategy-inline-error">{{ error }}</div>
        <div v-if="isL1Configurator" class="live-policy-permission-note">
          当前账号可配置 L1 / L2，不能代维护客户 L3；主播训练与声音复刻仍分别以客户授权为准。
        </div>

        <section
          v-if="activeRoom && activeMode === 'strategy' && canStrategy"
          class="live-support-editor live-support-system-agent-panel"
        >
          <div class="live-support-editor-head">
            <div>
              <span class="section-kicker">L3 POLICY SUPPORT</span>
              <h3>客户 L3 策略调教</h3>
              <p>直接使用页面底部的系统智能体描述要怎么调整。智能体只会写入当前客户授权直播间的 L3 草稿，不会越权修改客户其它配置。</p>
            </div>
          </div>
          <div class="live-policy-system-agent-hint">
            <span>统一智能体入口</span>
            <strong>在底部输入框里直接说：“给当前客户直播间新增一条……规则”</strong>
            <small>当前授权直播间会自动带入系统智能体上下文。</small>
          </div>
          <div v-if="policyDraft" class="live-support-draft-actions">
            <span>L3 草稿 V{{ policyDraft.version_no }}</span>
            <button
              class="primary-button"
              type="button"
              :disabled="loading || !!policyDraft.conflicts?.length"
              @click="publishPolicyDraft"
            >
              {{ policyDraft.conflicts?.length ? '存在冲突' : '发布客户 L3' }}
            </button>
          </div>
        </section>

        <section v-else-if="activeRoom && activeMode === 'anchor' && canAnchor" class="live-support-editor">
          <div class="live-support-editor-head">
            <div>
              <span class="section-kicker">ANCHOR TRAINING</span>
              <h3>主播训练</h3>
              <p>可录入真人主播文案，也可以上传训练录音或文档。只写入当前授权直播间。</p>
            </div>
          </div>
          <textarea
            v-model="anchorText"
            rows="8"
            placeholder="粘贴真人主播口播样本、说话习惯、节奏要求等……"
          ></textarea>
          <div class="strategy-upload-row">
            <input
              ref="anchorDocumentInput"
              hidden
              type="file"
              accept=".txt,.md,.doc,.docx,.pdf,text/plain,text/markdown,application/pdf"
              @change="uploadAnchorAsset($event, 'document')"
            />
            <input
              ref="anchorAudioInput"
              hidden
              type="file"
              accept="audio/*"
              @change="uploadAnchorAsset($event, 'audio')"
            />
            <button type="button" :disabled="anchorUploading" @click="anchorDocumentInput?.click()">＋ 文案素材</button>
            <button type="button" :disabled="anchorUploading" @click="anchorAudioInput?.click()">＋ 训练录音</button>
            <span>{{ anchorUploading ? '上传中…' : anchorAssetNames.join('、') || '尚未上传素材' }}</span>
          </div>
          <div class="live-support-editor-actions">
            <button class="primary-button" type="button" :disabled="loading" @click="saveAnchorTraining">
              生成训练草稿
            </button>
            <button
              v-if="anchorDraftId"
              class="primary-button"
              type="button"
              :disabled="loading"
              @click="publishAnchorTraining"
            >
              发布训练 V{{ anchorDraftVersion }}
            </button>
          </div>
        </section>

        <section v-else-if="activeRoom && activeMode === 'voice' && canVoice" class="live-support-editor">
          <div class="live-support-editor-head">
            <div>
              <span class="section-kicker">VOICE CLONE</span>
              <h3>声音复刻</h3>
              <p>客户已授权声音复刻时，运维人员可代上传声音样本并建立克隆档案。</p>
            </div>
          </div>
          <input
            ref="voiceSampleInput"
            hidden
            type="file"
            accept="audio/*"
            @change="uploadVoiceSample"
          />
          <div class="strategy-upload-row">
            <button type="button" :disabled="voiceUploading" @click="voiceSampleInput?.click()">
              ＋ 上传声音样本
            </button>
            <span>{{ voiceUploading ? '上传中…' : voiceSampleName || '尚未上传声音样本' }}</span>
          </div>
          <div class="strategy-basic-grid">
            <label>
              <span>声音名称</span>
              <input v-model="voiceName" placeholder="例如：老板本人声音" />
            </label>
            <label>
              <span>声音服务商</span>
              <input v-model="voiceProvider" placeholder="填写实际接入的声音服务商" />
            </label>
            <label class="wide">
              <span>Voice ID（如果服务商已经返回）</span>
              <input v-model="voiceID" placeholder="可先留空，后续训练完成再补" />
            </label>
          </div>
          <div class="live-support-editor-actions">
            <button
              class="primary-button"
              type="button"
              :disabled="voiceSaving || !voiceSampleAssetId"
              @click="saveVoiceProfile"
            >
              {{ voiceSaving ? '保存中…' : '建立声音复刻档案' }}
            </button>
          </div>
          <div v-if="voiceProfiles.length" class="live-support-voice-list">
            <article v-for="profile in voiceProfiles" :key="profile.id">
              <strong>{{ profile.name }}</strong>
              <span>{{ profile.provider }} · {{ profile.clone_status }}</span>
            </article>
          </div>
        </section>

        <div v-else-if="!activeRoom" class="empty-state live-support-empty">
          客户授权后，这里才会出现对应直播间和授权能力。
        </div>
      </main>
    </section>
  </div>
</template>
