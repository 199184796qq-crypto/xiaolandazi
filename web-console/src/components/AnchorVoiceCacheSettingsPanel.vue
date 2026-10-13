<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getAnchorVoiceCachePolicy, updateAnchorVoiceCachePolicy, getUserVoiceCachePolicies, updateUserVoiceCachePolicy, type UserVoiceCachePolicy } from '../api'
const hours = ref(5), loading = ref(false), saving = ref(false), error = ref(''), notice = ref(''), loaded = ref(false)
const mainlineReuse = ref(false), interactionReuse = ref(false)
const users = ref<UserVoiceCachePolicy[]>([]), search = ref(''), page = ref(1)
const selected = ref<UserVoiceCachePolicy | null>(null), userHours = ref(5), userMainline = ref(false), userInteraction = ref(false), userError = ref('')
const filtered = computed(() => users.value.filter(user => (user.display_name + ' ' + user.username).toLowerCase().includes(search.value.trim().toLowerCase())))
const pages = computed(() => Math.max(1, Math.ceil(filtered.value.length / 12)))
const visible = computed(() => filtered.value.slice((page.value - 1) * 12, page.value * 12))
function validHours(value: number) { return Number.isInteger(value) && value >= 1 && value <= 168 }
function apply(value: Awaited<ReturnType<typeof getAnchorVoiceCachePolicy>>) {
  hours.value = value.retention_hours; mainlineReuse.value = value.mainline_cache_reuse_enabled; interactionReuse.value = value.interaction_cache_reuse_enabled
}
async function load() {
  loading.value = true; loaded.value = false; error.value = ''
  try {
    const [defaults, people] = await Promise.all([getAnchorVoiceCachePolicy(), getUserVoiceCachePolicies()])
    apply(defaults); users.value = people.items; page.value = Math.min(page.value, pages.value); loaded.value = true
  } catch (value) { error.value = value instanceof Error ? value.message : '读取声音缓存设置失败' }
  finally { loading.value = false }
}
async function save() {
  if (!validHours(hours.value)) { error.value = '保留时间须为1～168小时的整数'; return }
  saving.value = true; error.value = ''; notice.value = ''
  try {
    apply(await updateAnchorVoiceCachePolicy(hours.value, { mainline_cache_reuse_enabled: mainlineReuse.value, interaction_cache_reuse_enabled: interactionReuse.value }))
    await load(); notice.value = '平台默认设置已保存。单独设置的用户继续使用自己的设置。'
  } catch (value) { error.value = value instanceof Error ? value.message : '保存平台默认设置失败' }
  finally { saving.value = false }
}
function edit(user: UserVoiceCachePolicy) {
  selected.value = user; userHours.value = user.retention_hours; userMainline.value = user.mainline_cache_reuse_enabled; userInteraction.value = user.interaction_cache_reuse_enabled; userError.value = ''
}
async function saveUser(reset = false) {
  const user = selected.value
  if (!user || saving.value) return
  if (!reset && !validHours(userHours.value)) { userError.value = '保留时间须为1～168小时的整数'; return }
  saving.value = true; userError.value = ''; notice.value = ''
  try {
    await updateUserVoiceCachePolicy(user.tenant_id, reset ? { using_default: true } : { mainline_cache_reuse_enabled: userMainline.value, interaction_cache_reuse_enabled: userInteraction.value, retention_hours: userHours.value })
    selected.value = null; await load(); notice.value = (user.display_name || user.username) + (reset ? '已恢复平台默认设置。' : '的声音缓存设置已保存。')
  } catch (value) { userError.value = value instanceof Error ? value.message : '保存用户设置失败' }
  finally { saving.value = false }
}
onMounted(load)
</script>

<template>
  <section id="anchor-voice-cache" class="cache-settings">
    <header><div><h2>用户话术与声音缓存</h2><p>按用户控制主线和互动的成品复用，以及云端声音保留时间。</p></div><button type="button" :disabled="loading || saving" @click="load">{{ loading ? '读取中…' : '刷新设置' }}</button></header>
    <p v-if="error" class="error" role="alert">{{ error }}</p><p v-if="notice" class="success" role="status">{{ notice }}</p>
    <section class="default-settings">
      <h3>平台默认设置</h3><p>未单独设置的用户使用这里的配置。</p>
      <form @submit.prevent="save">
        <label class="toggle"><input v-model="mainlineReuse" type="checkbox" :disabled="!loaded || saving">主线话术与声音缓存复用</label>
        <label class="toggle"><input v-model="interactionReuse" type="checkbox" :disabled="!loaded || saving">互动话术与声音缓存复用</label>
        <label>声音保留时间（小时）<input v-model.number="hours" type="number" min="1" max="168" step="1" :disabled="!loaded || saving"></label>
        <button type="submit" :disabled="!loaded || saving">{{ saving ? '保存中…' : '保存平台默认' }}</button>
      </form>
    </section>
    <div class="user-toolbar"><h3>用户单独设置</h3><label>查找用户<input v-model="search" type="search" placeholder="用户名称或账号" @input="page = 1"></label><span>共{{ filtered.length }}位用户</span></div>
    <p v-if="loading">正在读取用户设置…</p>
    <div v-else-if="loaded" class="table-wrap">
      <table><thead><tr><th>用户</th><th>配置来源</th><th>主线缓存</th><th>互动缓存</th><th>声音保留时间</th><th>操作</th></tr></thead>
        <tbody><tr v-for="user in visible" :key="user.user_id"><td><strong>{{ user.display_name || user.username }}</strong><small>{{ user.username }}</small></td><td>{{ user.using_default ? '平台默认' : '单独设置' }}</td><td><span class="badge" :class="{ enabled: user.mainline_cache_reuse_enabled }">{{ user.mainline_cache_reuse_enabled ? '已开启' : '已关闭' }}</span></td><td><span class="badge" :class="{ enabled: user.interaction_cache_reuse_enabled }">{{ user.interaction_cache_reuse_enabled ? '已开启' : '已关闭' }}</span></td><td>{{ user.retention_hours }}小时</td><td><button type="button" :disabled="saving" @click="edit(user)">设置缓存</button></td></tr>
        <tr v-if="!visible.length"><td colspan="6">暂无符合条件的用户</td></tr></tbody>
      </table>
      <nav v-if="pages > 1" aria-label="用户列表分页"><button :disabled="page <= 1" @click="page--">上一页</button><span>第{{ page }}页，共{{ pages }}页</span><button :disabled="page >= pages" @click="page++">下一页</button></nav>
    </div>
    <p class="note">关闭复用后，后续请求重新生成话术与声音。用户设置适用于其账号下的直播间；声音保留时间影响新生成的声音、字幕和时间轴文件，已有声音按原时间到期。互动缓存还受回答有效期限制。</p>
    <div v-if="selected" class="dialog-backdrop" @click.self="!saving && (selected = null)">
      <section class="cache-dialog" role="dialog" aria-modal="true" aria-labelledby="cache-user-title" @keydown.esc="!saving && (selected = null)">
        <h3 id="cache-user-title">{{ selected.display_name || selected.username }} · 缓存设置</h3>
        <p>{{ selected.using_default ? '当前沿用平台默认，保存后将使用单独设置。' : '当前使用单独设置。' }}</p>
        <form @submit.prevent="saveUser()"><label class="toggle"><input v-model="userMainline" type="checkbox" :disabled="saving">主线话术与声音缓存复用</label><label class="toggle"><input v-model="userInteraction" type="checkbox" :disabled="saving">互动话术与声音缓存复用</label><label>声音保留时间（小时）<input v-model.number="userHours" type="number" min="1" max="168" step="1" :disabled="saving"></label><p v-if="userError" class="error" role="alert">{{ userError }}</p><div class="dialog-actions"><button type="button" :disabled="saving" @click="selected = null">取消</button><button type="button" :disabled="saving || selected.using_default" @click="saveUser(true)">恢复平台默认</button><button type="submit" class="primary" :disabled="saving">{{ saving ? '保存中…' : '保存用户设置' }}</button></div></form>
      </section>
    </div>
  </section>
</template>

<style scoped>
.cache-settings{padding:24px;margin:20px 0;background:#fff;border:1px solid #dce6e8;border-radius:16px;color:#203859}header,.user-toolbar,nav,.dialog-actions{display:flex;gap:16px;align-items:center;justify-content:space-between;flex-wrap:wrap}h2,h3{margin:0 0 10px}p{color:#61748c;line-height:1.7}form{display:flex;gap:18px;flex-wrap:wrap;align-items:end}label{display:grid;gap:8px}input,button{padding:10px 14px;border:1px solid #cedddd;border-radius:9px;font:inherit}button{cursor:pointer;color:#4256ad;background:#f6f7ff}button:disabled{opacity:.5;cursor:default}.toggle{display:flex;align-items:center;gap:9px;align-self:center}.toggle input{width:18px;height:18px;accent-color:#6255df}.default-settings{padding:20px;background:#f6f8ff;border-radius:12px;margin:20px 0}.user-toolbar{margin-bottom:16px}.table-wrap{overflow:auto}table{width:100%;border-collapse:collapse;min-width:760px}th,td{padding:16px 12px;text-align:left;border-bottom:1px solid #e9edf5;vertical-align:top}th{background:#f5f7fd}small{display:block;color:#7a889f;margin-top:5px}.badge{display:inline-block;padding:5px 10px;background:#edf0f5;color:#68778b;border-radius:8px}.badge.enabled{background:#e4f8ef;color:#13815e}.note{font-size:14px}.error{color:#b32d41}.success{color:#13815e;background:#eaf8f0;padding:12px;border-radius:8px}nav{justify-content:flex-end;padding:16px 0}.dialog-backdrop{position:fixed;inset:0;z-index:3000;background:#16243d66;display:flex;align-items:center;justify-content:center;padding:20px}.cache-dialog{background:#fff;border-radius:18px;padding:28px;width:min(540px,100%);max-height:90vh;overflow:auto;box-shadow:0 20px 80px #182b4b30}.cache-dialog form{display:grid}.dialog-actions{justify-content:flex-end;margin-top:12px}.primary{background:#6255df;color:#fff;border-color:#6255df}@media(max-width:700px){.cache-settings{padding:16px}.default-settings{padding:16px}.user-toolbar label{width:100%}}
</style>
