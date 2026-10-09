<script setup lang="ts">
import { computed } from 'vue'
import type { PoolStatus } from '../contentPools'
const props = defineProps<{ status: PoolStatus | null; error: string; submitting: boolean; preview: boolean }>()
const emit = defineEmits<{ update: []; refresh: []; preview: [value: boolean] }>()
const title = computed(() => ({ needs_update: '资料有待更新内容', updating: '正在更新受影响内容', failed: '内容更新未完成', ready: '内容已更新，核对后保存智能体', synced: '商品专属内容与公共内容已保存' }[props.status?.state || 'needs_update']))
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
  <section class="pool-notice" role="status">
    <div class="pool-head"><strong>{{ status ? title : '正在读取方案资料状态…' }}</strong><div>
      <button v-if="status && status.state !== 'synced'" type="button" :disabled="submitting || status.state === 'updating' || status.state === 'ready'" @click="emit('update')">{{ submitting || status.state === 'updating' ? '更新中…' : status.state === 'failed' ? '重试更新' : status.state === 'ready' ? '等待保存智能体' : '更新受影响内容' }}</button>
      <button type="button" @click="emit('refresh')">刷新</button>
    </div></div>
    <p v-if="error" class="pool-error">{{ error }}</p>
    <p v-if="status?.state === 'updating'">{{ status.job?.stage || '正在准备内容' }}</p>
    <p v-if="status?.state === 'failed'" class="pool-error">{{ status.job?.error }}</p>
    <p v-if="status?.changes?.length">累计 {{ status.changes.length }} 项待保存变化。重复修改合并显示，恢复原值后移除。</p>
    <details v-if="groups.length"><summary>查看待更新资料</summary><div v-for="group in groups" :key="group.label" class="pool-group">
      <b>{{ group.label }}</b><div v-for="row in group.rows" :key="row.id" class="pool-change"><span>{{ row.label }}</span><div><del>{{ row.before || '未设置' }}</del><span> → </span><ins>{{ row.after || '已移除' }}</ins></div></div>
    </div></details>
    <details v-if="status?.pools?.length" :open="preview" @toggle="emit('preview', ($event.target as HTMLDetailsElement).open)"><summary>查看公共池与商品专属池（{{ status.pools.length }} 个）</summary>
      <div v-for="pool in status.pools" :key="pool.key" class="pool-group"><b>{{ pool.owner ? pool.label : '公共内容' }} · {{ pool.task_count }} 条</b><p>适用 {{ products(pool.products) }}{{ pool.reused ? ' · 本次复用原内容' : '' }}</p><ol><li v-for="task in pool.tasks || []" :key="task.id">{{ task.selling_point }} · {{ task.angle }}</li></ol></div>
    </details>
    <details @toggle="emit('preview', ($event.target as HTMLDetailsElement).open || preview)"><summary>查看最近资料变更记录</summary><div v-for="entry in status?.history || []" :key="entry.id" class="pool-group"><b>{{ entry.created_at }} UTC</b><p v-for="row in entry.changes" :key="row.id">{{ row.label }}：{{ row.before || '未设置' }} → {{ row.after || '已移除' }}</p></div><p v-if="!status?.history?.length">暂无已记录的资料变更。</p></details>
    <small>资料变化会自动记录；点击更新时才调用模型。核对并保存智能体后，下次开播使用新版本，本场继续使用已保存快照。</small>
  </section>
</template>
<style scoped>
.pool-notice{padding:18px 22px;border:1px solid #d9e3ff;background:#f1f5ff;border-radius:14px;color:#4c5b7b;margin:12px 0 20px}.pool-head{display:flex;justify-content:space-between;gap:12px;align-items:center;flex-wrap:wrap}.pool-head strong{font-size:16px}.pool-head button{border:1px solid #b8c6ff;background:white;color:#5753cf;border-radius:8px;padding:8px 14px;margin-left:8px;cursor:pointer}.pool-head button:disabled{opacity:.6;cursor:default}.pool-notice p{margin:8px 0}.pool-notice details{margin:12px 0}.pool-notice summary{cursor:pointer}.pool-group{background:white;padding:12px;margin:10px 0;border-radius:8px}.pool-change{padding:8px 0;border-bottom:1px solid #e8ecf8;overflow-wrap:anywhere}.pool-change div{margin-top:4px;white-space:pre-wrap}.pool-change del{color:#8b7080}.pool-change ins{text-decoration:none;color:#326e55}.pool-group ol{max-height:260px;overflow:auto;padding-left:24px}.pool-group li{padding:6px}.pool-error{color:#aa3444}.pool-notice small{line-height:1.6;display:block}
</style>
