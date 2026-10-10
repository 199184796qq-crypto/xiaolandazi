<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { anchorPresenterAvatar } from '../anchorAvatar'
import { getRecommendationAdmin, getRecommendationSources, previewRecommendation, publishRecommendation, withdrawRecommendation, type AnchorSource, type RecommendedAnchor, type RecommendationDraft } from '../anchorRecommendations'
const sources = ref<AnchorSource[]>([]), recommendations = ref<RecommendedAnchor[]>([]), search = ref(''), draft = ref<RecommendationDraft | null>(null)
const loading = ref(false), busy = ref(false), error = ref(''), notice = ref(''), selectedSource = ref<AnchorSource | null>(null)
const summaryLength = computed(() => Array.from(draft.value?.summary || '').length)
let epoch = 0, loadNo = 0
onBeforeUnmount(() => { epoch++ })
async function load() {
  const token = epoch, number = ++loadNo; loading.value = true; error.value = ''
  try { const [a, b] = await Promise.all([getRecommendationSources(search.value), getRecommendationAdmin()]); if (token === epoch && number === loadNo) { sources.value = a.items; recommendations.value = b.items } }
  catch (cause) { if (token === epoch && number === loadNo) error.value = cause instanceof Error ? cause.message : '读取失败' }
  finally { if (token === epoch && number === loadNo) loading.value = false }
}
async function promote(source: AnchorSource) {
  if (busy.value) return
  const token = epoch; busy.value = true; error.value = ''; notice.value = ''; selectedSource.value = source; draft.value = null
  try { const result = await previewRecommendation(source); if (token === epoch) { draft.value = result; if (result.validation_error) error.value = result.validation_error } }
  catch (cause) { if (token === epoch) error.value = cause instanceof Error ? cause.message : '生成简介失败，请重试' }
  finally { if (token === epoch) busy.value = false }
}
async function publish() {
  if (!draft.value || busy.value || summaryLength.value !== 50) return
  const token = epoch; busy.value = true; error.value = ''
  try {
    const item = await publishRecommendation(draft.value)
    if (token !== epoch) return
    draft.value = null; selectedSource.value = null; notice.value = `“${item.name}”已发布为系统推荐，所有用户都能在主播管理中查看并添加。`
    await load()
  } catch (cause) { if (token === epoch) error.value = cause instanceof Error ? cause.message : '发布失败' }
  finally { if (token === epoch) busy.value = false }
}
async function withdraw(item: RecommendedAnchor) {
  if (busy.value) return
  const token = epoch; busy.value = true; error.value = ''
  try { await withdrawRecommendation(item); if (token === epoch) { notice.value = `“${item.name}”已下架，用户已经添加的主播仍可使用。`; await load() } }
  catch (cause) { if (token === epoch) error.value = cause instanceof Error ? cause.message : '下架失败' }
  finally { if (token === epoch) busy.value = false }
}
onMounted(load)
</script>
<template>
  <div class="anchor-recommendation-admin">
    <header class="recommendation-admin-heading"><div><small>ANCHOR LIBRARY</small><h1>系统推荐主播</h1><p>将用户的优秀主播风格提升为系统推荐，供所有用户挑选。</p></div><button type="button" :disabled="loading || busy" @click="load">刷新</button></header>
    <p v-if="error" role="alert" class="recommendation-admin-error">{{ error }}</p><p v-if="notice" role="status" class="recommendation-admin-notice">{{ notice }}</p>
    <section v-if="selectedSource" class="recommendation-draft" aria-label="发布系统推荐主播">
      <header><h2>{{ busy && !draft ? '正在生成50字特征简介…' : '校对推荐主播' }}</h2><button type="button" :disabled="busy" @click="draft = null; selectedSource = null">取消</button></header>
      <p>来源：{{ selectedSource.tenant_name }} · {{ selectedSource.name }}。发布后向全体用户展示头像、介绍与特征简介。</p>
      <template v-if="draft"><div class="draft-name"><img :src="anchorPresenterAvatar(draft)" :alt="draft.name + '头像'" width="76" height="76" /><label>推荐名称<input v-model="draft.name" maxlength="80" :disabled="busy" /></label></div>
        <label>主播介绍<textarea v-model="draft.introduction" rows="2" maxlength="500" :disabled="busy" /></label>
        <label>风格特征简介 <span>{{ summaryLength }}/50字</span><textarea v-model="draft.summary" rows="3" :disabled="busy" /></label>
        <p v-if="summaryLength !== 50" class="recommendation-admin-error">请校对为50字（含标点），再发布哦。</p>
        <details><summary>查看模型原始输出</summary><pre>{{ draft.raw_text }}</pre></details>
        <div class="recommendation-draft-actions"><button type="button" :disabled="busy" @click="promote(selectedSource!)">重新生成简介</button><button type="button" class="recommendation-primary" :disabled="busy || summaryLength !== 50 || !draft.name.trim()" @click="publish">{{ busy ? '发布中…' : '发布为系统推荐' }}</button></div>
      </template><button v-else-if="!busy" type="button" @click="promote(selectedSource!)">重试生成简介</button>
    </section>
    <section class="recommendation-admin-section"><h2>用户的主播风格</h2><form class="recommendation-source-search" @submit.prevent="load"><input v-model="search" aria-label="搜索用户或主播" maxlength="80" placeholder="搜索用户名称 / 主播名称" /><button type="submit" :disabled="loading || busy">搜索</button></form>
      <p v-if="loading">正在读取主播库…</p><p v-else-if="!sources.length">没有找到主播。用户创建并保存风格后，会出现在这里。</p>
      <div class="recommendation-admin-grid"><article v-for="source in sources" :key="source.id"><img :src="anchorPresenterAvatar(source)" :alt="source.name + '头像'" width="68" height="68" /><div><h3>{{ source.name }}</h3><small>{{ source.tenant_name }} · 用户 #{{ source.tenant_id }}</small><p>{{ source.description || '尚未填写介绍' }}</p><button type="button" :disabled="busy || !source.has_style" @click="promote(source)">{{ source.has_style ? '设为系统推荐' : '等待风格分析' }}</button></div></article></div>
    </section>
    <section class="recommendation-admin-section"><h2>已发布的推荐</h2><p>更新推荐会保存新版本，已加入用户列表的主播保持各自配置。</p><p v-if="!recommendations.length">还没有发布推荐主播。</p>
      <div class="recommendation-admin-grid"><article v-for="item in recommendations" :key="item.id"><img :src="anchorPresenterAvatar(item)" :alt="item.name + '头像'" width="68" height="68" /><div><h3>{{ item.name }} <small>{{ item.status === 'active' ? '推荐中' : '已下架' }} · v{{ item.version_no }}</small></h3><p>{{ item.introduction }}</p><p class="recommendation-summary">{{ item.summary }}</p><button v-if="item.status === 'active'" type="button" :disabled="busy" @click="withdraw(item)">下架推荐</button></div></article></div>
    </section>
  </div>
</template>
<style scoped>
.anchor-recommendation-admin{padding:24px;display:grid;gap:20px;color:#344663;background:#f7f9ff;min-height:80vh}.recommendation-admin-heading,.recommendation-draft header{display:flex;justify-content:space-between;align-items:center;gap:20px}.recommendation-admin-heading small{color:#919bc0;letter-spacing:2px}.recommendation-admin-heading h1{margin:8px 0;color:#26375b}.recommendation-admin-heading p,.recommendation-admin-section>p{color:#8593b0;line-height:1.7}.anchor-recommendation-admin button{padding:10px 16px;border:1px solid #d9def5;border-radius:11px;color:#635cd1;background:white;font:inherit;cursor:pointer}.anchor-recommendation-admin button:disabled{opacity:.5;cursor:default}.anchor-recommendation-admin input,.anchor-recommendation-admin textarea{box-sizing:border-box;width:100%;padding:12px;border:1px solid #dce2f7;border-radius:11px;background:#fff;color:inherit;font:inherit;line-height:1.6}.recommendation-admin-section,.recommendation-draft{padding:24px;border:1px solid #e0e6fa;border-radius:21px;background:#fff}.recommendation-source-search{display:flex;gap:12px;max-width:660px;margin:18px 0}.recommendation-source-search button{flex-shrink:0}.recommendation-admin-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(300px,1fr));gap:18px}.recommendation-admin-grid article{display:grid;grid-template-columns:68px minmax(0,1fr);align-items:start;gap:16px;padding:20px;border:1px solid #e1e6f7;border-radius:16px;background:#fbfcff}.recommendation-admin-grid img,.draft-name img{border-radius:50%;object-fit:cover}.recommendation-admin-grid h3{margin:0 0 10px;color:#293a62}.recommendation-admin-grid p{line-height:1.8;overflow-wrap:anywhere;color:#8190ad}.recommendation-admin-grid small{color:#8b83b9;font-size:12px}.recommendation-admin-grid .recommendation-summary{color:#59557c}.recommendation-draft{background:#f6f4ff}.recommendation-draft label{display:block;margin:15px 0;font-weight:700}.recommendation-draft label span{font-size:13px;color:#8d82bd}.draft-name{display:flex;align-items:center;gap:20px}.draft-name label{flex:1}.recommendation-draft-actions{display:flex;justify-content:flex-end;gap:12px;margin-top:20px}.anchor-recommendation-admin .recommendation-primary{color:#fff;background:linear-gradient(120deg,#738ded,#695adb);border-color:transparent;font-weight:700}.recommendation-draft pre{white-space:pre-wrap;overflow-wrap:anywhere}.recommendation-admin-error{color:#c1475e;line-height:1.7}.recommendation-admin-notice{color:#25836a;line-height:1.7}@media(max-width:600px){.anchor-recommendation-admin{padding:12px}.recommendation-admin-section,.recommendation-draft{padding:16px}.recommendation-admin-grid{grid-template-columns:minmax(0,1fr)}}
</style>
