<script setup lang="ts">
import { computed } from 'vue'
import type { PoolStatus } from '../contentPools'
import uploadIllustration from '../assets/content-update-upload.png'
const props = defineProps<{ status: PoolStatus | null; error: string; submitting: boolean; preview: boolean }>()
const emit = defineEmits<{ update: []; refresh: []; preview: [value: boolean]; openSave: [] }>()
const title = computed(() => ({ needs_update: '资料有待更新内容', updating: '正在更新受影响内容', failed: '内容更新未完成', ready: '内容已更新，核对后保存智能体', synced: '商品专属内容与公共内容已保存' }[props.status?.state || 'needs_update']))
const guidance = computed(() => ({
  needs_update: '展开核对资料变化，点击“更新受影响内容”，完成后再保存智能体方案。',
  updating: '正在根据资料变化更新内容，完成后可展开核对并保存智能体方案。',
  failed: '查看更新结果与资料变化，重试更新后再核对并保存智能体方案。',
  ready: '下一步：展开核对内容，点击“去保存智能体”，再点击“保存智能体方案”。',
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
</script>
<template>
  <section class="pool-notice" aria-label="内容更新与保存引导">
    <div class="pool-banner">
      <div class="pool-art" aria-hidden="true"><img :src="uploadIllustration" alt="" /></div>
      <div class="pool-intro" role="status">
        <h2>{{ status ? title : '正在读取方案资料状态…' }}</h2>
        <p v-if="status">{{ guidance }}</p>
      </div>
      <div class="pool-actions">
        <button v-if="status?.state === 'ready'" class="pool-primary" type="button" @click="emit('openSave')">
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M7 18H5.5a4.5 4.5 0 0 1-.7-8.95 7 7 0 0 1 13.5-.55A4.8 4.8 0 0 1 18.5 18H17M12 21V10m-4 4 4-4 4 4" /></svg>
          去保存智能体
        </button>
        <button v-else-if="status && status.state !== 'synced'" class="pool-primary" type="button" :disabled="submitting || status.state === 'updating'" @click="emit('update')">
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m12 3-1.5 6.5L4 11l6.5 1.5L12 19l1.5-6.5L20 11l-6.5-1.5L12 3ZM4 3v4m-2-2h4m13 12v5m-2.5-2.5h5" /></svg>
          {{ submitting || status.state === 'updating' ? '更新中…' : status.state === 'failed' ? '重试更新' : '更新受影响内容' }}
        </button>
        <button class="pool-refresh" type="button" @click="emit('refresh')">
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 8a8 8 0 0 0-13.5-3L3 8m0-5v5h5M4 16a8 8 0 0 0 13.5 3L21 16m0 5v-5h-5" /></svg>
          刷新
        </button>
      </div>
      <div class="pool-content">
        <div v-if="error || status?.state === 'updating' || status?.state === 'failed' || status?.changes?.length" class="pool-progress">
          <p v-if="error" class="pool-error" role="alert">{{ error }}</p>
          <p v-if="status?.state === 'updating'">{{ status.job?.stage || '正在准备内容' }}</p>
          <p v-if="status?.state === 'failed'" class="pool-error">{{ status.job?.error }}</p>
          <p v-if="status?.changes?.length">累计 {{ status.changes.length }} 项待保存变化。重复修改合并显示，恢复原值后移除。</p>
        </div>
        <details v-if="groups.length" class="pool-pending"><summary>查看待更新资料<svg class="pool-chevron" viewBox="0 0 24 24" aria-hidden="true"><path d="m9 5 7 7-7 7" /></svg></summary><div v-for="group in groups" :key="group.label" class="pool-group">
          <b>{{ group.label }}</b><div v-for="row in group.rows" :key="row.id" class="pool-change"><span>{{ row.label }}</span><div><del>{{ row.before || '未设置' }}</del><span> → </span><ins>{{ row.after || '已移除' }}</ins></div></div>
        </div></details>
        <div class="pool-entries">
          <details v-if="status?.pools?.length" class="pool-entry pool-entry-content" :open="preview" @toggle="emit('preview', ($event.target as HTMLDetailsElement).open)">
            <summary><svg class="pool-entry-icon" viewBox="0 0 24 24" aria-hidden="true"><rect x="3" y="3" width="7" height="7" rx="1.5" /><rect x="14" y="3" width="7" height="7" rx="1.5" /><rect x="3" y="14" width="7" height="7" rx="1.5" /><rect x="14" y="14" width="7" height="7" rx="1.5" /></svg><span>查看公共池与商品专属池（{{ status.pools.length }} 个）</span><svg class="pool-chevron" viewBox="0 0 24 24" aria-hidden="true"><path d="m9 5 7 7-7 7" /></svg></summary>
            <div class="pool-entry-body"><div v-for="pool in status.pools" :key="pool.key" class="pool-group"><b>{{ pool.owner ? pool.label : '公共内容' }} · {{ pool.task_count }} 条</b><p>适用 {{ products(pool.products) }}{{ pool.reused ? ' · 本次复用原内容' : '' }}</p><ol><li v-for="task in pool.tasks || []" :key="task.id">{{ task.selling_point }} · {{ task.angle }}</li></ol></div></div>
          </details>
          <details class="pool-entry pool-entry-history" @toggle="emit('preview', ($event.target as HTMLDetailsElement).open || preview)">
            <summary><svg class="pool-entry-icon" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9" /><path d="M12 6v6h4" /></svg><span>查看最近资料变更记录</span><svg class="pool-chevron" viewBox="0 0 24 24" aria-hidden="true"><path d="m9 5 7 7-7 7" /></svg></summary>
            <div class="pool-entry-body"><div v-for="entry in status?.history || []" :key="entry.id" class="pool-group"><b>{{ entry.created_at }} UTC</b><p v-for="row in entry.changes" :key="row.id">{{ row.label }}：{{ row.before || '未设置' }} → {{ row.after || '已移除' }}</p></div><p v-if="!status?.history?.length" class="pool-empty">暂无已记录的资料变更。</p></div>
          </details>
        </div>
      </div>
      <small class="pool-note"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="10" /><path d="M12 11v6m0-10v.5" /></svg><span>资料变化会自动记录；点击更新时才调用模型。核对并保存智能体后，下次开播使用新版本，本场继续使用已保存快照。</span></small>
      <span class="pool-signature" aria-hidden="true">让内容更智能</span>
    </div>
  </section>
</template>
<style scoped>
.pool-notice{container-type:inline-size;position:relative;isolation:isolate;overflow:clip;margin:12px 0 20px;border:1px solid #dce5ff;border-radius:26px;background:linear-gradient(115deg,#f0f4ff 0%,#fbfdff 55%,#f3f7ff 100%);box-shadow:0 8px 28px #526de30a;color:#637496}
.pool-notice::before,.pool-notice::after{position:absolute;z-index:-1;bottom:-67px;width:480px;height:100px;border-radius:50%;content:'';pointer-events:none}
.pool-notice::before{left:-80px;background:linear-gradient(100deg,#dce5ffb3,#e8edff40);transform:rotate(8deg)}
.pool-notice::after{right:-100px;background:linear-gradient(100deg,#d8f3ff73,#dce5ffb3);transform:rotate(-8deg)}
.pool-banner{position:relative;display:grid;grid-template-columns:176px minmax(0,1fr) auto;gap:20px 24px;align-items:start;padding:30px 30px 28px;min-height:254px}
.pool-art{grid-column:1;grid-row:1 / span 3;align-self:center;margin:-12px -18px -12px -12px;pointer-events:none}
.pool-art img{display:block;width:100%;height:auto;filter:drop-shadow(0 12px 18px #7290e324)}
.pool-intro{grid-column:2;min-width:0;padding-top:3px}
.pool-intro h2{margin:0;color:#202e69;font-size:26px;font-weight:750;letter-spacing:-.5px;line-height:1.4;overflow-wrap:anywhere}
.pool-intro p{margin:12px 0 0;font-size:16px;line-height:1.8}
.pool-actions{grid-column:3;display:flex;align-items:center;gap:12px;padding-top:2px}
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
.pool-entries{display:grid;grid-template-columns:1.1fr 1fr;gap:20px;max-width:880px;align-items:start}
.pool-entry{min-width:0;border-radius:17px}
.pool-entry-content{background:linear-gradient(105deg,#eeebff,#f0eeff)}
.pool-entry-history{background:linear-gradient(105deg,#e7f7ee,#edf9f3)}
.pool-notice summary{display:flex;align-items:center;gap:16px;cursor:pointer;list-style:none;color:#364154;font-size:16px;line-height:1.5}
.pool-notice summary::-webkit-details-marker{display:none}
.pool-entry summary{min-height:65px;padding:16px 20px}
.pool-entry summary span{flex:1;min-width:0;overflow-wrap:anywhere}
.pool-entry-icon{width:29px;height:29px;flex-shrink:0;fill:none;stroke:#8e7bfc;stroke-width:2.5;stroke-linejoin:round;stroke-linecap:round;filter:drop-shadow(0 2px 1px #8e7bfc18)}
.pool-entry-history .pool-entry-icon{stroke:#36bc7b}
.pool-chevron{width:20px;height:20px;flex-shrink:0;margin-left:auto;fill:none;stroke:#8b96b5;stroke-width:1.8;stroke-linejoin:round;stroke-linecap:round;transition:transform .2s}
details[open]>summary .pool-chevron{transform:rotate(90deg)}
.pool-entry-body{padding:0 12px 12px;max-height:430px;overflow:auto}
.pool-pending{margin-bottom:16px;max-width:880px}
.pool-pending>summary{padding:10px 14px;border:1px solid #dfe6f8;border-radius:12px;background:#ffffff94;font-size:14px}
.pool-group{padding:14px;margin:10px 0 0;border:1px solid #e8edf7;border-radius:12px;background:#ffffffed;font-size:14px;line-height:1.7;overflow-wrap:anywhere}
.pool-group b{color:#36496c}
.pool-group p{margin:6px 0}
.pool-change{padding:8px 0;border-bottom:1px solid #e8ecf8}
.pool-change:last-child{border-bottom:0}
.pool-change div{margin-top:4px;white-space:pre-wrap}
.pool-change del{color:#8b7080}.pool-change ins{text-decoration:none;color:#326e55}
.pool-group ol{max-height:260px;overflow:auto;margin:8px 0 0;padding-left:24px}.pool-group li{padding:6px}
.pool-empty{padding:12px;margin:0;font-size:14px}
.pool-error{color:#aa3444}
.pool-note{grid-column:2 / -1;display:flex;align-items:flex-start;gap:10px;margin-top:2px;padding-right:170px;font-size:13px;line-height:1.8;color:#7d8dab}
.pool-note svg{width:20px;height:20px;flex-shrink:0;margin-top:2px;fill:#a1afca;stroke:#fff;stroke-width:1.8;stroke-linecap:round}
.pool-signature{position:absolute;bottom:38px;right:34px;color:#a6bfff;font:22px/1.4 KaiTi,STKaiti,cursive;letter-spacing:2px;transform:rotate(-10deg);pointer-events:none}
.pool-signature::after{display:block;width:130px;height:13px;margin-top:4px;border-top:2px solid #bdcfff;border-radius:50%;content:''}
.pool-notice button:focus-visible,.pool-notice summary:focus-visible{outline:3px solid #7b98ff;outline-offset:3px}
@container (min-width:1500px){.pool-banner{grid-template-columns:238px minmax(0,1fr) auto;gap:24px 30px;padding:38px 38px 32px;min-height:300px}.pool-intro h2{font-size:32px}.pool-intro p{font-size:18px}.pool-actions button{min-height:56px;font-size:18px;padding:13px 23px}.pool-entries{max-width:1020px}.pool-entry summary{min-height:74px;font-size:18px;padding:18px 24px}.pool-note{font-size:15px}.pool-signature{font-size:26px;bottom:48px}}
@container (max-width:1100px){.pool-banner{grid-template-columns:142px minmax(0,1fr);gap:18px 22px;padding:26px}.pool-art{grid-row:1 / span 3}.pool-intro{grid-column:2}.pool-actions{grid-column:2;grid-row:2;padding-top:0}.pool-content{grid-column:2}.pool-note{grid-column:2;padding-right:0}.pool-signature{display:none}.pool-entry summary{padding:14px;gap:10px;font-size:14px}}
@container (max-width:700px){.pool-banner{grid-template-columns:minmax(0,1fr);gap:18px;padding:22px}.pool-art{position:absolute;width:70px;right:12px;top:16px;margin:0;opacity:.65}.pool-intro{grid-column:1;padding-top:0}.pool-intro h2{min-height:64px;padding-right:60px;font-size:22px}.pool-intro p{font-size:14px;margin-top:10px}.pool-actions{grid-column:1;grid-row:auto;flex-wrap:wrap;gap:10px}.pool-actions button{font-size:14px;min-height:44px;padding:10px 15px}.pool-content,.pool-note{grid-column:1}.pool-entries{grid-template-columns:minmax(0,1fr);gap:12px}.pool-entry summary{min-height:58px;font-size:14px;padding:14px 16px}.pool-note{font-size:12px}.pool-group ol{padding-left:20px}}
@media (prefers-reduced-motion:reduce){.pool-actions button,.pool-chevron{transition:none}}
</style>
