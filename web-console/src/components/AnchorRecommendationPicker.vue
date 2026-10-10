<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { getRecommendedAnchors, importRecommendedAnchors, type RecommendedAnchor } from '../anchorRecommendations'
import { anchorPresenterAvatar } from '../anchorAvatar'
import type { LiveAnchorStyle } from '../types'
const props = defineProps<{ tenantId?: number; styles: LiveAnchorStyle[] }>()
const emit = defineEmits<{ close: []; imported: [items: LiveAnchorStyle[]] }>()
const items = ref<RecommendedAnchor[]>([]), loading = ref(false), pending = ref<number[]>([]), error = ref(''), notice = ref('')
const added = computed(() => new Set(props.styles.map(item => item.source_recommendation_id).filter(Boolean)))
let epoch = 0
watch(() => props.tenantId, async () => {
  const token = ++epoch; items.value = []; error.value = ''; notice.value = ''; pending.value = []; loading.value = true
  try { const result = await getRecommendedAnchors(); if (token === epoch) items.value = result.items }
  catch (cause) { if (token === epoch) error.value = cause instanceof Error ? cause.message : '读取系统推荐失败' }
  finally { if (token === epoch) loading.value = false }
}, { immediate: true })
onBeforeUnmount(() => { epoch++ })
async function add(item: RecommendedAnchor) {
  if (added.value.has(item.id) || pending.value.includes(item.id)) return
  const token = epoch, tenant = props.tenantId; pending.value = [...pending.value, item.id]; error.value = ''
  try {
    const result = await importRecommendedAnchors([item.id], tenant)
    if (token !== epoch) return
    emit('imported', result.items); notice.value = `“${item.name}”已加入你的主播列表。可继续勾选，再设为当前主播。`
  } catch (cause) { if (token === epoch) error.value = cause instanceof Error ? cause.message : '加入主播失败' }
  finally { if (token === epoch) pending.value = pending.value.filter(id => id !== item.id) }
}
</script>
<template>
  <Teleport to="body"><div class="anchor-recommendation-backdrop" @keydown.esc="!pending.length && emit('close')"><section class="anchor-recommendation-dialog" role="dialog" aria-modal="true" aria-labelledby="recommended-anchor-title">
    <header><div><h2 id="recommended-anchor-title">系统推荐主播</h2><p>勾选即可加入我的主播列表，再选择并设为当前主播。</p></div><button type="button" :disabled="!!pending.length" aria-label="关闭推荐主播" @click="emit('close')">×</button></header>
    <p v-if="error" role="alert" class="recommendation-error">{{ error }}</p><p v-if="notice" role="status" class="recommendation-notice">{{ notice }}</p>
    <p v-if="loading">正在寻找适合你的主播…</p><p v-else-if="!items.length" class="recommendation-empty">系统暂时还没有推荐主播，你也可以创建自己的主播。</p>
    <div class="recommended-anchor-grid"><label v-for="item in items" :key="item.id" class="recommended-anchor-card" :class="{ added: added.has(item.id) }">
      <img :src="anchorPresenterAvatar(item)" :alt="item.name + '头像'" width="68" height="68" />
      <div><h3>{{ item.name }}</h3><p v-if="item.introduction" class="recommendation-introduction">{{ item.introduction }}</p><p class="recommendation-summary">{{ item.summary }}</p><small>{{ pending.includes(item.id) ? '正在加入…' : added.has(item.id) ? '已添加到我的主播' : '勾选加入我的主播' }}</small></div>
      <input type="checkbox" :aria-label="`添加推荐主播“${item.name}”`" :checked="added.has(item.id) || pending.includes(item.id)" :disabled="added.has(item.id) || pending.includes(item.id)" @change="add(item)" />
    </label></div>
    <footer><button type="button" :disabled="!!pending.length" @click="emit('close')">完成，返回主播管理</button></footer>
  </section></div></Teleport>
</template>
<style scoped>
.anchor-recommendation-backdrop{position:fixed;inset:0;z-index:14000;display:grid;place-items:center;padding:24px;background:#25314b66;backdrop-filter:blur(5px)}.anchor-recommendation-dialog{box-sizing:border-box;width:min(880px,100%);max-height:90vh;overflow:auto;padding:26px;border:1px solid #dee4ff;border-radius:24px;background:#f9fbff;box-shadow:0 25px 85px #29355240}.anchor-recommendation-dialog header{display:flex;align-items:flex-start;justify-content:space-between;gap:18px}.anchor-recommendation-dialog h2{margin:0;color:#25365c}.anchor-recommendation-dialog header p{color:#8592af;line-height:1.7}.anchor-recommendation-dialog button{padding:10px 15px;border:1px solid #dbe2fb;border-radius:11px;background:white;color:#625cda;font:inherit;cursor:pointer}.recommended-anchor-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:15px;margin:18px 0}.recommended-anchor-card{display:grid;grid-template-columns:68px minmax(0,1fr) 20px;align-items:start;gap:13px;padding:18px;border:1px solid #e0e5f9;border-radius:18px;background:white;cursor:pointer}.recommended-anchor-card.added{border-color:#8d83ef;background:#f3f2ff}.recommended-anchor-card img{border-radius:50%;object-fit:cover}.recommended-anchor-card h3{margin:2px 0 9px;color:#28385e}.recommended-anchor-card p{overflow-wrap:anywhere;line-height:1.7;margin:8px 0;color:#687a9e}.recommended-anchor-card .recommendation-summary{color:#474d8a}.recommended-anchor-card small{color:#766cda}.recommended-anchor-card input{width:18px;height:18px;accent-color:#7164de}.recommendation-error{color:#c14159}.recommendation-notice{color:#278b72}.recommendation-empty{padding:32px;color:#8997af}.anchor-recommendation-dialog footer{text-align:right}.anchor-recommendation-dialog button:disabled{opacity:.55;cursor:default}@media(max-width:550px){.anchor-recommendation-backdrop{padding:10px}.anchor-recommendation-dialog{padding:18px}.recommended-anchor-grid{grid-template-columns:minmax(0,1fr)}}
</style>
