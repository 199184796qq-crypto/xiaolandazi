import { computed, onScopeDispose, ref, watch, type Ref } from 'vue'
import { getLiveAgentPlanVersions, getLiveAnchorStyle } from './api'
import { getCurrentAnchor } from './anchorRecommendations'
import type { LiveAgentPlanVersion, LiveAgentVoiceIdentity } from './types'

export function useRoomSpeechIdentity(input: {
  roomId: number
  tenantId: Ref<number>
  planId: Ref<number>
  sessionId: Ref<number>
  playbackVersionId: Ref<number>
  allowed: Ref<boolean>
  publishedVersion: Ref<LiveAgentPlanVersion | null>
}) {
  const presenterName = ref('')
  const voiceIdentity = ref<LiveAgentVoiceIdentity | null>(null)
  const presenterStatus = ref('未设置')
  const voiceStatus = ref('未设置')
  const voiceSource = ref('当前方案保存的声音配置')
  let epoch = 0
  onScopeDispose(() => { epoch += 1 })

  watch(() => [input.allowed.value, input.tenantId.value, input.planId.value,
    input.sessionId.value, input.playbackVersionId.value, input.publishedVersion.value] as const,
  async (scope, previous) => {
    const [allowed, tenantId, planId, , versionId, published] = scope
    const token = ++epoch
    const sameScope = previous && previous.slice(0, 5).every((value, index) => value === scope[index])
    if (!sameScope) {
      presenterName.value = ''
      voiceIdentity.value = null
    }
    if (!allowed) {
      presenterStatus.value = voiceStatus.value = '暂无查看权限'
      return
    }
    if (!tenantId || !planId) {
      presenterStatus.value = voiceStatus.value = '未设置'
      return
    }
    presenterStatus.value = voiceStatus.value = '读取中…'
    const current = () => epoch === token
    const validVersion = (version?: LiveAgentPlanVersion | null) => version?.tenant_id === tenantId && version.plan_id === planId && version.room_id === input.roomId
    await Promise.allSettled([
      (async () => {
        try {
          const binding = await getCurrentAnchor(planId, tenantId)
          if (!current()) return
          const presenter = binding.style_id ? await getLiveAnchorStyle(binding.style_id, tenantId) : null
          if (!current()) return
          presenterName.value = presenter?.tenant_id === tenantId ? presenter.name.trim() : ''
          presenterStatus.value = '未设置'
        } catch {
          if (current()) { presenterName.value = ''; presenterStatus.value = '暂未读取' }
        }
      })(),
      (async () => {
        try {
          let version = validVersion(published) ? published : null
          if (versionId && version?.id !== versionId) {
            const history = await getLiveAgentPlanVersions(planId, input.roomId, tenantId)
            version = history.items.find(item => item.id === versionId && validVersion(item)) || null
          }
          if (!current()) return
          // Never display a newly saved voice as the voice of an older playing program.
          voiceIdentity.value = version?.voice_identity || null
          voiceStatus.value = versionId && !version ? '暂未读取' : '未设置'
          voiceSource.value = versionId ? '正在播放的方案版本使用的声音配置' : '当前方案保存的声音配置'
        } catch {
          if (current()) { voiceIdentity.value = null; voiceStatus.value = '暂未读取' }
        }
      })(),
    ])
  }, { immediate: true })

  return {
    presenterLabel: computed(() => presenterName.value || presenterStatus.value),
    voiceLabel: computed(() => voiceIdentity.value?.name?.trim() || voiceStatus.value),
    voiceTitle: computed(() => [voiceSource.value, voiceIdentity.value?.model?.trim()].filter(Boolean).join(' · ')),
  }
}
