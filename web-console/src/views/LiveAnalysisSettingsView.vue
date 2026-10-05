<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  activateSpeechAnalysisProfile,
  createSpeechAnalysisProfile,
  getSpeechAnalysisProfiles,
} from '../api'
import { session } from '../session'
import ModulePageNav from '../components/ModulePageNav.vue'
import type { SpeechAnalysisProfile, SpeechAnalysisProfileInput } from '../types'

const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const profiles = ref<SpeechAnalysisProfile[]>([])
const selectedId = ref<number | null>(null)

const form = reactive<SpeechAnalysisProfileInput>({
  name: '',
  description: '',
  provider: 'dashscope',
  model: 'qwen3.8-flash',
  segment_system_prompt: '',
  segment_prompt_template: '',
  summary_system_prompt: '',
  summary_prompt_template: '',
  activate: true,
})

const canManage = computed(() => {
  if (session.bootstrap?.actor.role === 'platform_admin') return true
  const access = session.bootstrap?.staff_access
  return Boolean(access?.is_super_admin || access?.permissions.includes('liveanalysis.manage'))
})

const activeProfile = computed(() => profiles.value.find(item => item.status === 'active') || null)
const selectedProfile = computed(() => profiles.value.find(item => item.id === selectedId.value) || activeProfile.value || profiles.value[0] || null)

function formatTime(value?: string) {
  if (!value) return '—'
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}

function statusLabel(status: string) {
  if (status === 'active') return '当前启用'
  if (status === 'draft') return '草稿'
  if (status === 'inactive') return '历史版本'
  return status || '—'
}

function providerLabel(provider: string) {
  if (provider === 'dashscope') return '阿里云百炼'
  if (provider === 'openai-compatible' || provider === 'openai_compatible') return 'OpenAI 兼容接口'
  return provider || '—'
}

function copyProfileToForm(profile: SpeechAnalysisProfile | null) {
  if (!profile) return
  form.name = profile.name
  form.description = profile.description
  form.provider = profile.provider || 'dashscope'
  form.model = profile.model
  form.segment_system_prompt = profile.segment_system_prompt
  form.segment_prompt_template = profile.segment_prompt_template
  form.summary_system_prompt = profile.summary_system_prompt
  form.summary_prompt_template = profile.summary_prompt_template
  form.activate = true
}

function selectProfile(profile: SpeechAnalysisProfile) {
  selectedId.value = profile.id
  copyProfileToForm(profile)
  notice.value = ''
}

async function loadProfiles(preferId?: number) {
  loading.value = true
  error.value = ''
  try {
    const result = await getSpeechAnalysisProfiles()
    profiles.value = result.items || []
    const preferred = profiles.value.find(item => item.id === preferId)
      || profiles.value.find(item => item.status === 'active')
      || profiles.value[0]
      || null
    if (preferred) {
      selectedId.value = preferred.id
      copyProfileToForm(preferred)
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取直播分析配置失败'
  } finally {
    loading.value = false
  }
}

async function saveVersion(activate: boolean) {
  if (!canManage.value || saving.value) return
  error.value = ''
  notice.value = ''
  saving.value = true
  try {
    const created = await createSpeechAnalysisProfile({ ...form, activate })
    notice.value = activate
      ? `版本 V${created.version} 已保存并启用，之后的新分析任务将使用该版本。`
      : `版本 V${created.version} 已保存为草稿。`
    await loadProfiles(created.id)
  } catch (err) {
    error.value = err instanceof Error ? err.message : '保存配置失败'
  } finally {
    saving.value = false
  }
}

async function activateVersion(profile: SpeechAnalysisProfile) {
  if (!canManage.value || saving.value || profile.status === 'active') return
  if (!window.confirm(`确认启用 V${profile.version}「${profile.name}」？之后的新分析任务将使用这个版本。`)) return
  error.value = ''
  notice.value = ''
  saving.value = true
  try {
    const activated = await activateSpeechAnalysisProfile(profile.id)
    notice.value = `V${activated.version} 已启用。`
    await loadProfiles(activated.id)
  } catch (err) {
    error.value = err instanceof Error ? err.message : '启用配置失败'
  } finally {
    saving.value = false
  }
}

onMounted(() => loadProfiles())
</script>

<template>
  <main class="analysis-settings-page">
    <ModulePageNav context="workspace-auto" active-title="直播录音分析" />
    <section class="analysis-hero">
      <div>
        <p>LIVE ANALYSIS CONFIGURATION</p>
        <h1>直播分析配置</h1>
        <span>模型与分析提示词和业务代码隔离。员工通过版本迭代优化，新版本只影响之后新建的分析任务。</span>
      </div>
      <button type="button" :disabled="loading" @click="loadProfiles()">{{ loading ? '刷新中…' : '刷新' }}</button>
    </section>

    <div v-if="error" class="message error">{{ error }}</div>
    <div v-if="notice" class="message success">{{ notice }}</div>

    <section v-if="activeProfile" class="active-profile-card">
      <div>
        <span class="active-dot"></span>
        <div>
          <small>当前启用版本</small>
          <strong>V{{ activeProfile.version }} · {{ activeProfile.name }}</strong>
        </div>
      </div>
      <dl>
        <div><dt>模型供应商</dt><dd>{{ providerLabel(activeProfile.provider) }}</dd></div>
        <div><dt>模型</dt><dd>{{ activeProfile.model }}</dd></div>
        <div><dt>更新人</dt><dd>{{ activeProfile.updated_by_display_name || (activeProfile.updated_by_user_id ? `用户 #${activeProfile.updated_by_user_id}` : '系统默认') }}</dd></div>
        <div><dt>更新时间</dt><dd>{{ formatTime(activeProfile.updated_at) }}</dd></div>
      </dl>
    </section>

    <section class="analysis-layout">
      <aside class="version-panel">
        <header>
          <div><strong>版本记录</strong><small>旧版本保留，可随时重新启用</small></div>
        </header>
        <button
          v-for="profile in profiles"
          :key="profile.id"
          type="button"
          class="version-item"
          :class="{ selected: selectedId === profile.id, active: profile.status === 'active' }"
          @click="selectProfile(profile)"
        >
          <div>
            <strong>V{{ profile.version }} · {{ profile.name }}</strong>
            <span>{{ providerLabel(profile.provider) }} / {{ profile.model }}</span>
          </div>
          <small :class="profile.status">{{ statusLabel(profile.status) }}</small>
        </button>
        <p v-if="!loading && profiles.length === 0" class="empty">暂无配置版本</p>
      </aside>

      <section class="editor-panel">
        <header>
          <div>
            <strong>基于当前选中版本新建</strong>
            <small>保存时生成新版本，不覆盖历史。API Key 不在这里显示或修改。</small>
          </div>
          <button
            v-if="canManage && selectedProfile && selectedProfile.status !== 'active'"
            type="button"
            class="ghost"
            :disabled="saving"
            @click="activateVersion(selectedProfile)"
          >启用所选版本</button>
        </header>

        <div class="form-grid two">
          <label>
            <span>方案名称</span>
            <input v-model="form.name" :disabled="!canManage" maxlength="120" />
          </label>
          <label>
            <span>模型供应商</span>
            <select v-model="form.provider" :disabled="!canManage">
              <option value="dashscope">阿里云百炼</option>
              <option value="openai-compatible">OpenAI 兼容接口</option>
            </select>
          </label>
          <label class="wide">
            <span>版本说明</span>
            <input v-model="form.description" :disabled="!canManage" placeholder="例如：加强开场留人和逼单修改建议" />
          </label>
          <label class="wide">
            <span>模型名称</span>
            <input v-model="form.model" :disabled="!canManage" placeholder="例如 qwen3.8-flash" />
            <small>这里只配置模型名；供应商密钥仍由服务器环境变量管理。</small>
          </label>
        </div>

        <section class="prompt-block">
          <header><strong>分段分析</strong><small>先逐段检查这场直播的话术质量</small></header>
          <label>
            <span>系统提示词</span>
            <textarea v-model="form.segment_system_prompt" :disabled="!canManage" rows="5"></textarea>
          </label>
          <label>
            <span>分析任务模板</span>
            <textarea v-model="form.segment_prompt_template" :disabled="!canManage" rows="9"></textarea>
            <small v-pre>可用占位符：{{room_name}}、{{chunk_index}}、{{chunk_total}}、{{transcript}}。其中 {{transcript}} 必须保留。</small>
          </label>
        </section>

        <section class="prompt-block">
          <header><strong>汇总、优化与建议</strong><small>把所有分段分析整理成最终报告</small></header>
          <label>
            <span>系统提示词</span>
            <textarea v-model="form.summary_system_prompt" :disabled="!canManage" rows="5"></textarea>
          </label>
          <label>
            <span>汇总任务模板</span>
            <textarea v-model="form.summary_prompt_template" :disabled="!canManage" rows="9"></textarea>
            <small v-pre>可用占位符：{{room_name}}、{{analyses}}。其中 {{analyses}} 必须保留。</small>
          </label>
        </section>

        <footer v-if="canManage" class="editor-actions">
          <button type="button" class="secondary" :disabled="saving" @click="saveVersion(false)">保存为草稿版本</button>
          <button type="button" class="primary" :disabled="saving" @click="saveVersion(true)">{{ saving ? '保存中…' : '保存新版本并启用' }}</button>
        </footer>
        <p v-else class="readonly-tip">当前账号只有查看权限，不能修改或启用配置。</p>
      </section>
    </section>
  </main>
</template>

<style scoped>
.analysis-settings-page{display:grid;gap:16px;padding:20px;min-height:100%;color:#30384d}.analysis-hero{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;padding:22px 24px;border:1px solid rgba(95,113,180,.13);border-radius:18px;background:rgba(255,255,255,.82);box-shadow:0 12px 36px rgba(58,72,128,.06)}.analysis-hero p{margin:0 0 5px;color:#7180c0;font-size:11px;font-weight:900;letter-spacing:.12em}.analysis-hero h1{margin:0;font-size:24px}.analysis-hero span{display:block;margin-top:8px;color:#858fa6;font-size:12px;line-height:1.7}.analysis-hero button,.ghost,.secondary,.primary{border:0;border-radius:10px;padding:9px 13px;font:inherit;font-size:12px;font-weight:900;cursor:pointer}.analysis-hero button,.ghost{color:#5f6daf;background:#eef1ff}.message{padding:10px 13px;border-radius:10px;font-size:12px}.message.error{color:#b64b58;background:#fff0f2}.message.success{color:#397a67;background:#edf9f4}.active-profile-card{display:flex;align-items:center;justify-content:space-between;gap:24px;padding:15px 18px;border:1px solid rgba(75,160,132,.18);border-radius:16px;background:#f1faf6}.active-profile-card>div{display:flex;align-items:center;gap:10px}.active-dot{width:9px;height:9px;border-radius:50%;background:#47aa87;box-shadow:0 0 0 6px rgba(71,170,135,.1)}.active-profile-card small{display:block;color:#719b8d;font-size:10px}.active-profile-card strong{display:block;margin-top:2px;color:#397764;font-size:13px}.active-profile-card dl{display:flex;gap:26px;margin:0}.active-profile-card dl div{display:grid;gap:2px}.active-profile-card dt{color:#9ca5b5;font-size:9px}.active-profile-card dd{margin:0;color:#58647d;font-size:11px;font-weight:800}.analysis-layout{display:grid;grid-template-columns:minmax(250px,310px) minmax(0,1fr);gap:16px;align-items:start}.version-panel,.editor-panel{border:1px solid rgba(92,108,168,.12);border-radius:16px;background:rgba(255,255,255,.9);box-shadow:0 10px 30px rgba(54,68,120,.05)}.version-panel{padding:12px;display:grid;gap:8px}.version-panel>header,.editor-panel>header,.prompt-block>header{display:flex;align-items:center;justify-content:space-between;gap:10px}.version-panel>header{padding:5px 4px 9px}.version-panel header strong,.editor-panel>header strong,.prompt-block>header strong{display:block;font-size:12px}.version-panel header small,.editor-panel>header small,.prompt-block>header small{display:block;margin-top:3px;color:#9aa3b5;font-size:9px}.version-item{display:flex;align-items:center;justify-content:space-between;gap:8px;width:100%;padding:11px;border:1px solid transparent;border-radius:12px;text-align:left;background:#f8f9fc;cursor:pointer}.version-item.selected{border-color:rgba(98,114,210,.25);background:#f0f2ff;box-shadow:0 0 0 3px rgba(98,114,210,.05)}.version-item.active{background:#f1faf6}.version-item strong{display:block;color:#4e5870;font-size:10px}.version-item span{display:block;margin-top:4px;color:#959dad;font-size:9px}.version-item>small{padding:4px 6px;border-radius:999px;color:#8993a6;background:#eceef3;font-size:8px;font-weight:900;white-space:nowrap}.version-item>small.active{color:#347861;background:#dff4eb}.version-item>small.draft{color:#7b67a8;background:#eee8fa}.empty,.readonly-tip{margin:0;padding:12px;color:#9ba3b2;font-size:10px;text-align:center}.editor-panel{padding:17px;display:grid;gap:16px}.form-grid{display:grid;gap:12px}.form-grid.two{grid-template-columns:1fr 1fr}.form-grid .wide{grid-column:1/-1}label{display:grid;gap:6px}label>span{color:#616c84;font-size:10px;font-weight:900}input,select,textarea{width:100%;box-sizing:border-box;border:1px solid #e0e4ee;border-radius:9px;padding:9px 10px;color:#424c63;background:#fff;font:inherit;font-size:11px;outline:none}textarea{resize:vertical;line-height:1.65;font-family:ui-monospace,SFMono-Regular,Consolas,"Liberation Mono",monospace}input:focus,select:focus,textarea:focus{border-color:#9aa6e2;box-shadow:0 0 0 3px rgba(98,114,210,.07)}input:disabled,select:disabled,textarea:disabled{background:#f6f7fa;color:#858d9e}.form-grid small,label small{color:#9aa3b2;font-size:9px;line-height:1.55}.prompt-block{display:grid;gap:11px;padding:14px;border:1px solid rgba(95,110,171,.1);border-radius:13px;background:#fafbfe}.editor-actions{display:flex;justify-content:flex-end;gap:8px;padding-top:2px}.secondary{color:#5968aa;background:#edf0fb}.primary{color:#fff;background:#6474ce;box-shadow:0 7px 18px rgba(100,116,206,.18)}button:disabled{opacity:.55;cursor:not-allowed}@media(max-width:980px){.analysis-layout{grid-template-columns:1fr}.active-profile-card{align-items:flex-start;flex-direction:column}.active-profile-card dl{display:grid;grid-template-columns:1fr 1fr;gap:10px 24px;width:100%}}@media(max-width:640px){.analysis-settings-page{padding:12px}.analysis-hero{padding:16px}.form-grid.two{grid-template-columns:1fr}.form-grid .wide{grid-column:auto}.active-profile-card dl{grid-template-columns:1fr}.editor-actions{display:grid}.editor-actions button{width:100%}}
</style>
