<script setup lang="ts">
import { computed } from 'vue'
import type { PoolStatus } from '../contentPools'
import { poolSourceText, poolTaskSources, type PoolSourceLocation } from '../contentPoolSources'
import uploadIllustration from '../assets/content-update-upload.png'
import NaturalContentCorrection from './NaturalContentCorrection.vue'
const props = withDefaults(defineProps<{ planId?: number; roomId?: number; tenantId?: number; status: PoolStatus | null; error: string; submitting: boolean; preview: boolean; saving?: boolean; saveDisabled?: boolean; saveHint?: string; saveError?: string; saveNotice?: string; locationError?: string; compact?: boolean; hideSaveAction?: boolean }>(), { saving: false, saveDisabled: false, saveHint: '', saveError: '', saveNotice: '', locationError: '', compact: false, hideSaveAction: false })
const emit = defineEmits<{ update: []; refresh: []; preview: [value: boolean]; save: []; source: [value: PoolSourceLocation]; editTask: [poolKey: string, taskID: string] }>()
const needsUpdate = computed(() => props.status?.state === 'needs_update' || props.status?.state === 'failed')
const updating = computed(() => props.submitting || props.status?.state === 'updating')
const actionDisabled = computed(() => !props.status || !!props.error || props.saving || updating.value || (!needsUpdate.value && props.saveDisabled))
const actionLabel = computed(() => props.saving ? '保存中…' : updating.value ? '更新中…' : needsUpdate.value ? props.status?.state === 'failed' ? '重试更新' : '更新受影响内容' : '保存智能体')
const disabledHint = computed(() => {
  if (props.saving) return '正在把方案收好，稍等一下哦～'
  if (updating.value) return '内容正在认真准备中，完成后就能保存啦～'
  if (!props.status) return '稍等一下，正在读取方案资料～'
  if (props.error) return '资料状态暂时没读到，点一下“刷新”再试试吧～'
  return actionDisabled.value ? props.saveHint || '方案还在准备中，完成后就能保存啦～' : ''
})
function primaryAction() {
  if (actionDisabled.value) return
  if (needsUpdate.value) emit('update')
  else emit('save')
}
const title = computed(() => ({ needs_update: '资料有待更新内容', updating: '正在更新受影响内容', failed: '内容更新未完成', ready: '内容已更新，核对后保存智能体', synced: '商品专属内容与公共内容已保存' }[props.status?.state || 'needs_update']))
const compactStateText = computed(() => ({ needs_update: '有受影响内容待更新', updating: '正在更新内容', failed: '更新未完成', ready: '内容已更新，待保存方案', synced: '内容与已保存版本一致' }[props.status?.state || 'needs_update']))
const guidance = computed(() => ({
  needs_update: '展开核对资料变化，点击“更新受影响内容”，完成后再保存智能体方案。',
  updating: '正在根据资料变化更新内容，完成后可展开核对并保存智能体方案。',
  failed: '查看更新结果与资料变化，重试更新后再核对并保存智能体方案。',
  ready: '展开核对更新内容，确认后点击“保存智能体”，下次开播使用新版本。',
  synced: '公共内容与商品专属内容已保存，下次开播将使用已保存版本。',
}[props.status?.state || 'needs_update']))
const groups = computed(() => {
  const result = new Map<string, { label: string; rows: NonNullable<PoolStatus['changes']> }>()
  for (const row of props.status?.changes || []) {
    const key = row.area === 'products' ? `products:${row.products?.join(',')}` : row.area
    const label = row.area === 'products' ? (props.status?.products || []).filter(p => row.products?.includes(p.id)).map(p => `${p.link}号 ${p.name}`).join('、') || '商品资料' : ({ benefits: '福利活动', knowledge: '公共资料', miner: '卖点与角度' }[row.area] || '其他资料')
    if (!result.has(key)) result.set(key, { label, rows: [] })
    result.get(key)!.rows.push(row)
  }
  return [...result.values()]
})
function products(ids: number[]) { return (props.status?.products || []).filter(p => ids.includes(p.id)).map(p => `${p.link}号`).join('、') }
const taskIcons = {
  price: 'm3 3 8 0 10 10-8 8L3 11V3Zm4 4h.01M10 10l6 6m0-6-6 6',
  package: 'm12 3 9 5v8l-9 5-9-5V8l9-5Zm-9 5 9 5 9-5M12 13v8M7.5 5.5l9 5',
  delivery: 'M3 5h11v12H3V5Zm11 5h4l3 4v3h-7M7 19a2 2 0 1 0 0-4 2 2 0 0 0 0 4Zm11 0a2 2 0 1 0 0-4 2 2 0 0 0 0 4Z',
  audience: 'M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8ZM2 21v-2a7 7 0 0 1 14 0v2M16 4a4 4 0 0 1 0 8m3 3a6 6 0 0 1 3 5v1',
  safety: 'm12 3 9 4v5c0 5-9 9-9 9s-9-4-9-9V7l9-4Zm-4 9 3 3 5-6',
  service: 'M20 11a8 8 0 0 0-16 0M4 11H2v6h4v-6H4Zm16 0h2v6h-4v-6h2Zm0 6v2a2 2 0 0 1-2 2h-6',
  time: 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20ZM12 6v6l4 2',
  general: 'M5 3h10l4 4v14H5V3Zm10 0v5h4M9 12h6m-6 4h6',
}
function taskAppearance(title: string) {
  const kind = /价格|规格|性价比|划算|多少钱/.test(title) ? 'price'
    : /包装|口味|真空|礼盒/.test(title) ? 'package'
    : /发货|配送|物流|快递|新鲜/.test(title) ? 'delivery'
    : /售后|退货|保障/.test(title) ? 'service'
    : /安全|品质|健康|检测/.test(title) ? 'safety'
    : /人群|用户|儿童|老人|送礼/.test(title) ? 'audience'
    : /时效|时间|保质|期限/.test(title) ? 'time' : 'general'
  return { kind, path: taskIcons[kind] }
}
</script>
<template>
  <section class="pool-notice" :class="{ 'is-compact': compact }" aria-label="内容更新与保存引导">
    <div class="pool-banner">
      <div v-if="!compact" class="pool-art" aria-hidden="true"><img :src="uploadIllustration" alt="" /></div>
      <div class="pool-intro" role="status">
        <h2>{{ compact ? '内容任务' : status ? title : '正在读取方案资料状态…' }}</h2>
        <p v-if="status">{{ compact ? compactStateText : guidance }}</p>
      </div>
      <div class="pool-actions">
        <div v-if="needsUpdate || updating || !hideSaveAction" class="pool-main-action">
          <button class="pool-primary" type="button" :disabled="actionDisabled" :aria-busy="saving || updating" :aria-describedby="disabledHint ? 'pool-primary-hint' : undefined" @click="primaryAction">
            <svg v-if="needsUpdate || updating" viewBox="0 0 24 24" aria-hidden="true"><path d="m12 3-1.5 6.5L4 11l6.5 1.5L12 19l1.5-6.5L20 11l-6.5-1.5L12 3ZM4 3v4m-2-2h4m13 12v5m-2.5-2.5h5" /></svg>
            <svg v-else viewBox="0 0 24 24" aria-hidden="true"><path d="M7 18H5.5a4.5 4.5 0 0 1-.7-8.95 7 7 0 0 1 13.5-.55A4.8 4.8 0 0 1 18.5 18H17M12 21V10m-4 4 4-4 4 4" /></svg>
            {{ actionLabel }}
          </button>
          <p v-if="disabledHint" id="pool-primary-hint" class="pool-action-hint" role="status"><span aria-hidden="true">✦</span>{{ disabledHint }}</p>
          <p v-else-if="saveNotice" class="pool-action-hint" role="status">{{ saveNotice }}</p>
          <p v-if="saveError" class="pool-action-error" role="alert">{{ saveError }}</p>
        </div>
        <button class="pool-refresh" type="button" :disabled="saving" @click="emit('refresh')">
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 8a8 8 0 0 0-13.5-3L3 8m0-5v5h5M4 16a8 8 0 0 0 13.5 3L21 16m0 5v-5h-5" /></svg>
          刷新
        </button>
      </div>
      <div class="pool-content" :class="{ 'pool-content-expanded': preview && status?.pools?.length }">
        <div v-if="error || status?.state === 'updating' || status?.state === 'failed' || status?.changes?.length" class="pool-progress">
          <p v-if="error" class="pool-error" role="alert">{{ error }}</p>
          <p v-if="status?.state === 'updating'">{{ status.job?.stage || '正在准备内容' }}</p>
          <p v-if="status?.state === 'failed'" class="pool-error">{{ status.job?.error }}</p>
          <p v-if="status?.changes?.length">累计 {{ status.changes.length }} 项待保存变化。重复修改合并显示，恢复原值后移除。</p>
        </div>
        <details v-if="status?.notices?.length" class="pool-exclusions">
          <summary>未入池提示（{{ status.notices.length }} 条）· 合格内容可继续保存</summary>
          <p>以下内容留在池外，不影响其他合格内容。可根据原因修改资料后重新生成。</p>
          <article v-for="(item, index) in status.notices" :key="index">
            <strong>{{ item.pool }}</strong><p v-if="item.content">{{ item.content }}</p><p>{{ item.reason }}</p>
          </article>
          <button v-if="status.notices.some(item => item.retryable)" type="button" :disabled="saving || updating" @click="emit('update')">重试未完成内容</button>
        </details>
        <details v-if="groups.length" class="pool-pending"><summary>查看待更新资料<svg class="pool-chevron" viewBox="0 0 24 24" aria-hidden="true"><path d="m9 5 7 7-7 7" /></svg></summary><div v-for="group in groups" :key="group.label" class="pool-group">
          <b>{{ group.label }}</b><div v-for="row in group.rows" :key="row.id" class="pool-change"><span>{{ row.label }}</span><div><del>{{ row.before || '未设置' }}</del><span> → </span><ins>{{ row.after || '已移除' }}</ins></div></div>
        </div></details>
        <div class="pool-entries">
          <details v-if="status?.pools?.length" class="pool-entry pool-entry-content" :open="preview" @toggle="emit('preview', ($event.target as HTMLDetailsElement).open)">
            <summary class="pool-preview-header">
              <span v-if="!compact" class="pool-preview-symbol"><svg viewBox="0 0 24 24" aria-hidden="true"><rect x="3" y="3" width="7" height="7" rx="1.5" /><rect x="14" y="3" width="7" height="7" rx="1.5" /><rect x="3" y="14" width="7" height="7" rx="1.5" /><rect x="14" y="14" width="7" height="7" rx="1.5" /></svg></span>
              <span class="pool-preview-heading"><strong>查看公共池与商品专属池（{{ status.pools.length }} 个）</strong><span class="pool-preview-description">以下是模型整理的内容任务。可修改任务，也可展开引用资料，跳到原资料核对；修改后需保存智能体。</span><span v-if="status.set_id !== status.saved_set_id" class="pool-preview-unsaved"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9" /><path d="M12 11v6m0-10v.1" /></svg>当前展示待保存内容；下次开播前请核对并保存。</span></span>
              <span v-if="!compact" class="pool-preview-art" aria-hidden="true"><span class="pool-preview-paper"><i></i><i></i><i></i></span><span class="pool-preview-paper"><i></i><i></i><i></i></span><span class="pool-preview-sticker">资料更清晰<br />开播更轻松</span></span>
              <svg class="pool-chevron" viewBox="0 0 24 24" aria-hidden="true"><path d="m9 5 7 7-7 7" /></svg>
            </summary>
            <div class="pool-entry-body">
              <p v-if="locationError" class="pool-error" role="alert">{{ locationError }}</p>
              <section v-for="pool in status.pools" :key="pool.key" class="pool-preview-group" :aria-label="pool.owner ? pool.label : '公共内容'">
                <header class="pool-preview-group-header">
                  <svg class="pool-group-symbol" viewBox="0 0 24 24" aria-hidden="true"><path d="M5 3h10l4 4v14H5V3Zm10 0v5h4M9 12h6m-6 4h6" /></svg>
                  <div><h3>{{ pool.owner ? pool.label : '公共内容' }} · {{ pool.task_count }} 条</h3><p>适用 {{ products(pool.products) || '暂无关联商品' }}<span v-if="pool.reused"> · 本次复用原内容</span></p></div>
                  <span class="pool-group-tip"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="10" cy="10" r="7" /><path d="m15 15 6 6" /></svg>内容已自动整理，你可以随时修改和完善</span>
                </header>
                <ol class="pool-task-cards"><li v-for="(task, index) in pool.tasks || []" :key="task.id" class="pool-task-card" :class="`pool-task-${taskAppearance(task.selling_point).kind}`">
                  <div class="pool-task-number"><strong>{{ String(index + 1).padStart(2, '0') }}</strong><span>适用 {{ products(pool.products) || '暂无关联商品' }}</span></div>
                  <svg class="pool-task-symbol" viewBox="0 0 24 24" aria-hidden="true"><path :d="taskAppearance(task.selling_point).path" /></svg>
                  <div class="pool-task-content"><h4>{{ task.selling_point }}<small v-if="task.disabled"> · 已停用，保留记录</small></h4><p class="pool-task-angle">{{ task.angle }}</p>
                  <NaturalContentCorrection v-if="planId && roomId" :plan-id="planId" :room-id="roomId" :tenant-id="tenantId" :set-id="status.set_id" :pool-key="pool.key" :task-id="task.id" :selected-text="task.angle" :disabled="saving || updating || !['ready','synced'].includes(status.state)" @saved="emit('refresh')" />
                  <details class="pool-task-sources">
                    <summary><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9 13 6-6a3 3 0 0 1 4 4l-8 8a5 5 0 0 1-7-7l8-8a7 7 0 0 1 10 10l-9 9" /></svg>引用资料（{{ poolTaskSources(status, pool, task).length }} 处）<svg class="pool-source-chevron" viewBox="0 0 24 24" aria-hidden="true"><path d="m9 5 7 7-7 7" /></svg></summary>
                    <p v-if="!task.source_ids?.length">这条旧任务没有记录引用编号，无法定位来源。</p>
                    <article v-for="source in poolTaskSources(status, pool, task)" :key="source.id">
                      <button v-if="source.location" type="button" class="pool-source-button" @click="emit('source', source.location)">{{ source.label }} · 查看出处</button>
                      <strong v-else>{{ source.label }}</strong>
                      <span v-if="source.state === 'changed'" class="pool-source-state">原资料已变化，需更新内容</span>
                      <span v-else-if="source.state === 'removed'" class="pool-source-state">原资料已移除，需更新内容</span>
                      <p v-if="source.saved"><small>生成这份内容时的原资料</small><span class="pool-source-text">{{ poolSourceText(source.saved) }}</span></p>
                      <p v-if="source.state === 'changed' && source.current"><small>当前原资料</small><span class="pool-source-text">{{ poolSourceText(source.current) }}</span></p>
                      <p v-if="source.state === 'unavailable'">旧记录缺少完整来源信息；可修改这条任务或重新更新内容。</p>
                    </article>
                  </details>
                  </div>
                  <button type="button" class="pool-task-edit" :disabled="saving || updating || !['ready', 'synced'].includes(status.state)" @click="emit('editTask', pool.key, task.id)"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m16 3 5 5-12 12-6 1 1-6L16 3Zm-3 3 5 5M3 21h18" /></svg>修改这条</button>
                </li></ol>
              </section>
            </div>
          </details>
        </div>
      </div>
      <details v-if="status?.corrections?.length" class="pool-entry correction-history">
        <summary>已保存的自然语言纠正（{{ status.corrections.length }} 条）</summary>
        <article v-for="item in [...status.corrections].reverse()" :key="item.id" class="pool-change">
          <div><b>{{ item.content.理解说明 }}</b><p>适用 {{ products(item.products) }}</p><p>你的原话：{{ item.feedback }}</p>
          <p v-if="item.content.事实修正">事实修正：{{ item.content.事实修正 }}</p><p v-if="item.content.表达限制">表达限制：{{ item.content.表达限制 }}</p><p v-if="item.content.展开指导">展开指导：{{ item.content.展开指导 }}</p><p v-if="item.content.取消说明">本次停用：{{ item.content.取消说明 }}</p>
          <NaturalContentCorrection v-if="planId && roomId" :plan-id="planId" :room-id="roomId" :tenant-id="tenantId" :product-scope="item.products" :selected-text="[item.content.事实修正,item.content.表达限制,item.content.展开指导].filter(Boolean).join('；') || item.original" :disabled="saving || updating" @saved="emit('refresh')" />
          </div>
        </article>
      </details>
      <small class="pool-note"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="10" /><path d="M12 11v6m0-10v.5" /></svg><span>资料变化会自动记录；点击更新时才调用模型。核对并保存智能体后，下次开播使用新版本，本场继续使用已保存快照。</span></small>
      <span v-if="!compact" class="pool-signature" aria-hidden="true">让内容更智能</span>
    </div>
  </section>
</template>
<style scoped>
.pool-exclusions{margin:12px 0;padding:14px 18px;border:1px solid #f2dbaf;border-radius:14px;background:#fff8eb;color:#8d631e;font-size:16px;line-height:1.7}.pool-exclusions summary{font-weight:650;cursor:pointer}.pool-exclusions article{padding:10px 0;border-top:1px solid #f2e4c9;overflow-wrap:anywhere}.pool-exclusions p{margin:6px 0}.pool-exclusions button{padding:9px 16px;border:1px solid #e0c793;border-radius:10px;background:white;color:inherit;font:inherit;cursor:pointer}
.pool-notice{container-type:inline-size;position:relative;isolation:isolate;overflow:clip;margin:12px 0 20px;border:1px solid #dce5ff;border-radius:26px;background:linear-gradient(115deg,#f0f4ff 0%,#fbfdff 55%,#f3f7ff 100%);box-shadow:0 8px 28px #526de30a;color:#637496}
.pool-notice::before,.pool-notice::after{position:absolute;z-index:-1;bottom:-67px;width:480px;height:100px;border-radius:50%;content:'';pointer-events:none}
.pool-notice::before{left:-80px;background:linear-gradient(100deg,#dce5ffb3,#e8edff40);transform:rotate(8deg)}
.pool-notice::after{right:-100px;background:linear-gradient(100deg,#d8f3ff73,#dce5ffb3);transform:rotate(-8deg)}
.pool-banner{position:relative;display:grid;grid-template-columns:176px minmax(0,1fr) auto;gap:20px 24px;align-items:start;padding:30px 30px 28px;min-height:254px}
.pool-art{grid-column:1;grid-row:1 / span 3;align-self:start;margin:-12px -18px -12px -12px;pointer-events:none}
.pool-art img{display:block;width:100%;height:auto;filter:drop-shadow(0 12px 18px #7290e324)}
.pool-intro{grid-column:2;min-width:0;padding-top:3px}
.pool-intro h2{margin:0;color:#202e69;font-size:26px;font-weight:750;letter-spacing:-.5px;line-height:1.4;overflow-wrap:anywhere}
.pool-intro p{margin:12px 0 0;font-size:16px;line-height:1.8}
.pool-actions{grid-column:3;display:flex;align-items:flex-start;gap:12px;padding-top:2px}
.pool-main-action{width:224px;max-width:100%;min-width:0;display:grid;gap:10px}.pool-main-action .pool-primary{box-sizing:border-box;width:224px;max-width:100%;height:58px;min-height:58px;padding:10px 12px;font-size:16px}.pool-action-hint,.pool-action-error{margin:0;padding:10px 12px;border-radius:12px;background:#eeefff;color:#6a71a1;font-size:13px;line-height:1.7;overflow-wrap:anywhere}.pool-action-hint span{margin-right:6px;color:#8a83ed}.pool-action-error{background:#fff1f4;color:#aa3444}
.pool-actions button{display:inline-flex;align-items:center;justify-content:center;gap:10px;min-height:48px;padding:11px 18px;border-radius:14px;font:inherit;font-size:16px;font-weight:650;white-space:nowrap;cursor:pointer;transition:box-shadow .2s,transform .2s}
.pool-primary{border:1px solid #6689ff;background:linear-gradient(110deg,#7b9dfb,#375df9);color:#fff;box-shadow:0 5px 12px #4770ec30,inset 0 1px 0 #ffffff35}
.pool-refresh{border:1px solid #dce4ff;background:#ffffffdb;color:#304178;box-shadow:0 3px 8px #415fab0b}
.pool-actions button:hover:not(:disabled){transform:translateY(-1px);box-shadow:0 7px 16px #4770ec26}
.pool-actions button:disabled{opacity:.6;cursor:wait}
.pool-actions svg{width:24px;height:24px;flex-shrink:0;fill:none;stroke:currentColor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}
.pool-refresh svg{color:#5376ee}
.pool-content{grid-column:2 / -1;min-width:0}
.pool-progress{margin-bottom:14px;font-size:14px;line-height:1.7}
.pool-progress p{margin:4px 0}
.pool-entries{display:grid;grid-template-columns:minmax(0,1fr);align-items:start}
.pool-entry{min-width:0;border-radius:17px}
.pool-entry-content{background:linear-gradient(105deg,#eeebff,#f0eeff)}
.pool-notice summary{display:flex;align-items:center;gap:16px;cursor:pointer;list-style:none;color:#364154;font-size:16px;line-height:1.5}
.pool-notice summary::-webkit-details-marker{display:none}
.pool-entry>summary{min-height:65px;padding:16px 20px}
.pool-entry-icon{width:29px;height:29px;flex-shrink:0;fill:none;stroke:#8e7bfc;stroke-width:2.5;stroke-linejoin:round;stroke-linecap:round;filter:drop-shadow(0 2px 1px #8e7bfc18)}
.pool-chevron{width:20px;height:20px;flex-shrink:0;margin-left:auto;fill:none;stroke:#8b96b5;stroke-width:1.8;stroke-linejoin:round;stroke-linecap:round;transition:transform .2s}
details[open]>summary .pool-chevron{transform:rotate(90deg)}
.pool-entry-body{padding:0 12px 12px}
.pool-pending{margin-bottom:16px}
.pool-pending>summary{padding:10px 14px;border:1px solid #dfe6f8;border-radius:12px;background:#ffffff94;font-size:14px}
.pool-group{padding:14px;margin:10px 0 0;border:1px solid #e8edf7;border-radius:12px;background:#ffffffed;font-size:14px;line-height:1.7;overflow-wrap:anywhere}
.pool-group b{color:#36496c}
.pool-group p{margin:6px 0}
.pool-change{padding:8px 0;border-bottom:1px solid #e8ecf8}
.pool-change:last-child{border-bottom:0}
.pool-change div{margin-top:4px;white-space:pre-wrap}
.pool-change del{color:#8b7080}.pool-change ins{text-decoration:none;color:#326e55}
.pool-content-expanded{grid-column:1 / -1}
.pool-entry-content{background:transparent}
.pool-entry-content>.pool-preview-header{position:relative;isolation:isolate;overflow:hidden;gap:20px;min-height:124px;padding:24px;border-radius:22px;background:linear-gradient(115deg,#eee9ff,#f0efff 55%,#e6e4ff);color:#172750}
.pool-preview-symbol{display:grid;place-items:center;flex:0 0 72px;width:72px;height:72px;border-radius:22px;background:linear-gradient(135deg,#b393ff,#7e60ff);box-shadow:inset 0 1px 2px #ffffff80}
.pool-preview-symbol svg{width:40px;height:40px;fill:none;stroke:#fff;stroke-width:2;stroke-linejoin:round}
.pool-preview-heading{position:relative;z-index:1;flex:1;min-width:0}
.pool-preview-heading strong{display:block;font-size:24px;line-height:1.4;letter-spacing:-.3px}
.pool-preview-description{display:block;margin-top:10px;color:#64739a;font-size:14px;line-height:1.7}
.pool-preview-unsaved{display:flex;align-items:flex-start;gap:8px;margin-top:8px;font-size:13px;line-height:1.6;color:#7078ab}
.pool-preview-unsaved svg{flex-shrink:0;width:19px;height:19px;fill:none;stroke:#7c62fb;stroke-width:2;stroke-linecap:round}
.pool-preview-art{position:relative;align-self:stretch;flex:0 0 190px;min-height:92px;margin:-24px 0;pointer-events:none;opacity:.8}
.pool-preview-paper{position:absolute;top:16px;right:24px;width:82px;height:123px;padding:27px 14px;border:2px solid #ffffffbb;border-radius:14px;background:linear-gradient(135deg,#c9bfff,#9d8af5);box-shadow:9px 6px 0 #af9eee55;transform:rotate(14deg)}
.pool-preview-paper:nth-child(2){top:53px;right:-14px;background:linear-gradient(135deg,#fff,#dadcff);transform:rotate(12deg)}
.pool-preview-paper i{display:block;height:8px;margin-bottom:13px;border-radius:6px;background:#ffffffbd}
.pool-preview-paper:nth-child(2) i{background:#b8b5ff80}
.pool-preview-sticker{position:absolute;top:48px;left:0;padding:10px 13px;border:2px solid #ffffffb3;border-radius:17px;background:#e4deff;color:#7560df;font-size:14px;font-weight:700;line-height:1.6;box-shadow:0 5px 10px #8573ce12;transform:rotate(-12deg)}
.pool-entry-content>.pool-entry-body{padding:0}
.pool-preview-group{margin-top:14px;min-width:0}
.pool-preview-group-header{display:flex;align-items:center;gap:16px;padding:18px 24px;border:1px solid #e8e9ff;border-radius:20px;background:#ffffffed}
.pool-group-symbol{flex-shrink:0;width:37px;height:42px;padding:7px;border-radius:10px;fill:none;stroke:#8e79fa;stroke-width:2;stroke-linejoin:round;stroke-linecap:round;background:#f1ecff}
.pool-preview-group-header h3{margin:0;font-size:19px;color:#172750;line-height:1.5}
.pool-preview-group-header p{margin:3px 0 0;font-size:14px;line-height:1.6}
.pool-group-tip{display:flex;align-items:center;gap:8px;margin-left:auto;padding:9px 13px;border:1px solid #e6e4ff;border-radius:13px;background:#f4f3ff;color:#747ca6;font-size:13px;line-height:1.5}
.pool-group-tip svg{flex-shrink:0;width:19px;height:19px;fill:none;stroke:currentColor;stroke-width:1.8;stroke-linecap:round}
.pool-task-cards{display:grid;gap:14px;margin:14px 0 0;padding:0;list-style:none}
.pool-task-card{--task-color:#8a69e9;--task-dark:#6f4cc6;--task-soft:#f1ebff;display:grid;grid-template-columns:136px 38px minmax(0,1fr) auto;gap:18px;align-items:start;min-width:0;padding:18px;border:1px solid #e7e9fa;border-radius:22px;background:linear-gradient(110deg,#fff,#ffffffec);box-shadow:0 5px 15px #7497f008;overflow-wrap:anywhere}
.pool-task-price{--task-color:#9374f3;--task-dark:#6640da;--task-soft:#eee8ff}
.pool-task-package{--task-color:#ff9b3e;--task-dark:#d67622;--task-soft:#fff3e5}
.pool-task-delivery,.pool-task-safety{--task-color:#16c586;--task-dark:#00a971;--task-soft:#e5faf2}
.pool-task-audience{--task-color:#5b9cff;--task-dark:#4485e4;--task-soft:#eaf3ff}
.pool-task-service{--task-color:#e585ad;--task-dark:#c65e8e;--task-soft:#fff0f6}
.pool-task-time{--task-color:#54b6cc;--task-dark:#368ca3;--task-soft:#e9f8fc}
.pool-task-number{display:flex;flex-direction:column;align-items:center;justify-content:center;gap:12px;align-self:stretch;min-height:116px;padding:18px 10px;border-radius:18px;background:var(--task-soft);color:var(--task-dark);text-align:center;line-height:1.5}
.pool-task-number strong{font-size:40px;line-height:1.1;font-weight:750}
.pool-task-number span{font-size:13px}
.pool-task-symbol{width:38px;height:38px;padding:9px;margin-top:6px;border-radius:12px;background:var(--task-color);color:white;fill:none;stroke:currentColor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}
.pool-task-content{position:relative;min-width:0;padding:5px 0 0 7px}
.pool-task-content::before{position:absolute;top:8px;left:-67px;bottom:8px;width:1px;background:var(--task-color);opacity:.3;content:''}
.pool-task-content h4{margin:0;color:#172750;font-size:20px;line-height:1.5}
.pool-task-angle{margin:8px 0 10px;color:#657599;font-size:15px;line-height:1.7;white-space:pre-wrap}
.pool-task-edit{display:inline-flex;align-items:center;justify-content:center;align-self:center;gap:9px;width:144px;min-height:46px;margin:0 8px 0 12px;padding:10px 16px;border:1px solid transparent;border-radius:14px;background:var(--task-soft);color:var(--task-dark);font:inherit;font-size:15px;font-weight:650;cursor:pointer}
.pool-task-edit svg{width:22px;height:22px;fill:none;stroke:currentColor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}
.pool-task-edit:hover:not(:disabled){border-color:var(--task-color)}
.pool-task-edit:disabled{opacity:.5;cursor:default}
.pool-source-button{padding:5px 10px;margin:6px 8px 4px 0;border:1px solid #ccd7f3;border-radius:8px;background:#f6f8ff;color:#4b65b7;font:inherit;cursor:pointer}
.pool-source-button:disabled{opacity:.5;cursor:default}
.pool-task-sources{margin:6px 0 0;font-size:13px}.pool-task-sources>summary{display:inline-flex;min-height:auto;max-width:100%;gap:8px;padding:7px 12px;border-radius:10px;background:var(--task-soft);color:var(--task-dark);font-size:14px}
.pool-task-sources>summary svg{width:20px;height:20px;flex-shrink:0;fill:none;stroke:currentColor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}
.pool-source-chevron{transition:transform .2s}.pool-task-sources[open]>summary .pool-source-chevron{transform:rotate(90deg)}
.pool-task-sources article{padding:10px;margin:8px 0;background:#f7f9fd;border:1px solid #e0e6f3;border-radius:10px}
.pool-task-sources small{display:block;color:#7d8dab}.pool-source-text{display:block;white-space:pre-wrap}.pool-source-state{color:#986128}
.pool-error{color:#aa3444}
.pool-note{grid-column:2 / -1;display:flex;align-items:flex-start;gap:10px;margin-top:2px;padding-right:170px;font-size:13px;line-height:1.8;color:#7d8dab}
.pool-note svg{width:20px;height:20px;flex-shrink:0;margin-top:2px;fill:#a1afca;stroke:#fff;stroke-width:1.8;stroke-linecap:round}
.pool-signature{position:absolute;bottom:18px;right:34px;color:#a6bfff;font:22px/1.4 KaiTi,STKaiti,cursive;letter-spacing:2px;transform:rotate(-10deg);pointer-events:none}
.pool-signature::after{display:block;width:130px;height:13px;margin-top:4px;border-top:2px solid #bdcfff;border-radius:50%;content:''}
.pool-notice button:focus-visible,.pool-notice summary:focus-visible{outline:3px solid #7b98ff;outline-offset:3px}
@container (min-width:1500px){.pool-banner{grid-template-columns:238px minmax(0,1fr) auto;gap:24px 30px;padding:38px 38px 32px;min-height:300px}.pool-intro h2{font-size:32px}.pool-intro p{font-size:18px}.pool-actions button{min-height:56px;font-size:18px;padding:13px 23px}.pool-note{font-size:15px}.pool-signature{font-size:26px;bottom:24px}.pool-preview-heading strong{font-size:28px}.pool-preview-description{font-size:16px}.pool-task-card{grid-template-columns:160px 42px minmax(0,1fr) auto;gap:22px;padding:20px}.pool-task-number strong{font-size:48px}.pool-task-symbol{width:42px;height:42px}.pool-task-content::before{left:-80px}.pool-task-edit{width:160px}}
@container (max-width:1100px){.pool-banner{grid-template-columns:142px minmax(0,1fr);gap:18px 22px;padding:26px}.pool-art{grid-row:1 / span 3}.pool-intro{grid-column:2}.pool-actions{grid-column:2;grid-row:2;padding-top:0}.pool-content{grid-column:2}.pool-content-expanded{grid-column:1 / -1}.pool-note{grid-column:2;padding-right:0}.pool-signature{display:none}.pool-entry-content>.pool-preview-header{padding:20px;gap:14px}.pool-preview-art{display:none}.pool-preview-heading strong{font-size:22px}.pool-group-tip{display:none}.pool-task-card{grid-template-columns:100px 32px minmax(0,1fr);gap:14px;padding:16px}.pool-task-number{grid-row:1 / 3}.pool-task-number strong{font-size:34px}.pool-task-symbol{width:32px;height:32px;padding:7px}.pool-task-content{padding-left:0}.pool-task-content::before{left:-60px}.pool-task-edit{grid-column:3;justify-self:start;margin:0;align-self:start}.pool-task-content h4{font-size:18px}}
@container (max-width:700px){.pool-banner{grid-template-columns:minmax(0,1fr);gap:18px;padding:22px}.pool-art{position:absolute;width:70px;right:12px;top:16px;margin:0;opacity:.65}.pool-intro{grid-column:1;padding-top:0}.pool-intro h2{min-height:64px;padding-right:60px;font-size:22px}.pool-intro p{font-size:14px;margin-top:10px}.pool-actions{grid-column:1;grid-row:auto;flex-wrap:wrap;gap:10px}.pool-actions button{font-size:14px;min-height:44px;padding:10px 15px}.pool-content,.pool-note{grid-column:1}.pool-note{font-size:12px}.pool-entry-content>.pool-preview-header{align-items:flex-start;gap:10px;padding:16px;border-radius:18px}.pool-preview-symbol{flex-basis:42px;width:42px;height:42px;border-radius:13px}.pool-preview-symbol svg{width:25px;height:25px}.pool-preview-heading strong{font-size:18px}.pool-preview-description{font-size:12px;margin-top:7px}.pool-preview-unsaved{font-size:12px}.pool-preview-header>.pool-chevron{width:15px;height:15px;margin-top:12px}.pool-preview-group-header{gap:10px;padding:14px;border-radius:16px}.pool-preview-group-header h3{font-size:16px}.pool-preview-group-header p{font-size:12px}.pool-task-card{grid-template-columns:46px minmax(0,1fr);gap:10px;padding:14px;border-radius:18px}.pool-task-number{grid-row:auto;grid-column:1;align-self:start;min-height:46px;padding:10px 4px;border-radius:12px}.pool-task-number strong{font-size:24px}.pool-task-number span{display:none}.pool-task-symbol{grid-column:1;grid-row:2;margin:0;justify-self:center}.pool-task-content{grid-column:2;grid-row:1 / 3;padding:0}.pool-task-content::before{display:none}.pool-task-content h4{font-size:17px}.pool-task-angle{font-size:13px}.pool-task-edit{grid-column:2;width:130px;min-height:42px;font-size:13px}.pool-task-sources>summary{font-size:12px;gap:4px;padding:7px 8px}.pool-task-sources article{padding:8px;margin:8px 0}.pool-source-button{max-width:100%;white-space:normal;overflow-wrap:anywhere}}
@media (prefers-reduced-motion:reduce){.pool-actions button,.pool-chevron{transition:none}}
@container (max-width:700px){.pool-task-card{grid-template-rows:46px auto auto}}
.pool-notice.is-compact{border:1px solid #e1e7f0;border-radius:10px;background:#fff;box-shadow:none;color:#6c7a90;margin-bottom:16px}
.pool-notice.is-compact::before,.pool-notice.is-compact::after{display:none}.is-compact .pool-banner{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:14px;min-height:0;padding:16px}
.is-compact .pool-intro{grid-column:1;padding:0}.is-compact .pool-intro h2{font-size:16px;font-weight:650;color:#30415b;letter-spacing:0}.is-compact .pool-intro p{margin:3px 0 0;font-size:12px;line-height:1.6;color:#7b8799}
.is-compact .pool-actions{grid-column:2;align-items:center;gap:8px;padding:0;flex-wrap:wrap}.is-compact .pool-actions button{min-height:36px;padding:7px 11px;border-radius:7px;font-size:12px;box-shadow:none;transform:none;gap:6px}.is-compact .pool-actions svg{width:17px;height:17px}.is-compact .pool-main-action{width:auto}.is-compact .pool-main-action .pool-primary{width:auto;height:auto;min-height:36px;background:#4a60de}.is-compact .pool-action-hint{max-width:280px;padding:6px 8px;font-size:11px}
.is-compact .pool-content{grid-column:1/-1}.is-compact .pool-progress{font-size:12px;margin-bottom:10px}.is-compact .pool-note{font-size:11px;padding:12px 16px}.is-compact .pool-exclusions{margin-bottom:12px}.is-compact .pool-exclusions>summary{padding:9px 12px;font-size:12px;border-radius:7px}
.is-compact .pool-entry-content>.pool-preview-header{min-height:0;padding:12px 14px;border:1px solid #e1e7f0;border-radius:8px;background:#f7f9fc;gap:10px}.is-compact .pool-preview-heading strong{font-size:14px;font-weight:650;letter-spacing:0}.is-compact .pool-preview-description{margin-top:4px;font-size:12px}.is-compact .pool-preview-unsaved{margin-top:5px;font-size:11px}
.is-compact .pool-preview-group-header{padding:12px 0;background:transparent}.is-compact .pool-preview-group-header h3{font-size:15px;font-weight:650}.is-compact .pool-group-symbol,.is-compact .pool-group-tip{display:none}.is-compact .pool-task-cards{display:grid;grid-template-columns:minmax(0,1fr);gap:10px}.is-compact .pool-task-card{grid-template-columns:52px minmax(0,1fr) auto;gap:12px;min-height:0;padding:12px;border:1px solid #e5eaf2;border-radius:8px;background:#fff;box-shadow:none}.is-compact .pool-task-symbol{display:none}.is-compact .pool-task-content h4{font-size:14px;font-weight:650}.is-compact .pool-task-number strong{font-size:15px}.is-compact .pool-task-angle{font-size:13px}.is-compact .pool-task-edit{font-size:11px}
.is-compact .pool-intro,.is-compact .pool-actions{grid-row:1}.is-compact .pool-intro h2{min-height:0;padding:0}.is-compact .pool-actions{justify-self:end}
.is-compact .pool-preview-group-header{border:0;border-radius:0;box-shadow:none}
.is-compact .pool-task-card{grid-template-rows:auto}.is-compact .pool-task-number{grid-column:1;grid-row:1;align-self:start;min-height:0;padding:8px 4px;border-radius:6px}.is-compact .pool-task-number span{display:none}
.is-compact .pool-task-content{grid-column:2;grid-row:1;padding:0}.is-compact .pool-task-content::before{display:none}.is-compact .pool-task-edit{grid-column:3;grid-row:1;justify-self:end;align-self:start;width:auto;min-height:32px;margin:0;padding:6px 10px;border-radius:6px}.is-compact .pool-task-edit svg{width:16px;height:16px}
@container(max-width:700px){.is-compact .pool-banner{grid-template-columns:minmax(0,1fr);padding:12px}.is-compact .pool-actions{grid-column:1;grid-row:2;justify-self:start}.is-compact .pool-task-card{grid-template-columns:32px minmax(0,1fr)}.is-compact .pool-task-edit{grid-column:2;grid-row:2;justify-self:start}}
</style>
