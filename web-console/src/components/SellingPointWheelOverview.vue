<script setup lang="ts">
import { computed } from 'vue'
import type {
  AnchorStyleFactGapSuggestion,
  AnchorStyleSellingPointWheelDocument,
  AnchorStyleSellingPointWheelResponse,
} from '../types'

const props = withDefaults(defineProps<{
  wheel?: AnchorStyleSellingPointWheelResponse | null
  loading?: boolean
  error?: string
}>(), { wheel: null, loading: false, error: '' })

const emit = defineEmits<{
  refresh: []
  addFact: [suggestion: AnchorStyleFactGapSuggestion]
}>()

const parsed = computed<AnchorStyleSellingPointWheelDocument | null>(() => {
  const raw = props.wheel?.wheel_json?.trim()
  if (!raw) return null
  try {
    const value = JSON.parse(raw) as Partial<AnchorStyleSellingPointWheelDocument>
    if (!Array.isArray(value.卖点列表)) return null
    return {
      卖点列表: value.卖点列表,
      事实补充建议: Array.isArray(value.事实补充建议) ? value.事实补充建议 : [],
    }
  } catch {
    return null
  }
})

const points = computed(() => parsed.value?.卖点列表 || [])
const factGaps = computed(() => parsed.value?.事实补充建议 || [])
const angleCount = computed(() => points.value.reduce((sum, item) => sum + (Array.isArray(item.角度) ? item.角度.length : 0), 0))
</script>

<template>
  <section class="wheel-overview" aria-label="卖点与内容">
    <header class="wheel-overview-head">
      <div>
        <span>SELLING POINT WHEEL</span>
        <strong>卖点与内容</strong>
        <p>只显示当前方案已确认的事实结果；待补事实不会进入直播话术。</p>
      </div>
      <div class="wheel-overview-actions">
        <template v-if="wheel && parsed">
          <b>{{ points.length }} 个卖点</b>
          <b>{{ angleCount }} 条内容</b>
          <em>资产 #{{ wheel.asset_id }}</em>
        </template>
        <button type="button" :disabled="loading" @click="emit('refresh')">{{ loading ? '读取中…' : '刷新' }}</button>
      </div>
    </header>

    <div v-if="error" class="wheel-overview-error" role="alert">{{ error }}</div>
    <div v-else-if="loading && !wheel" class="wheel-overview-empty">
      <strong>正在读取当前轮盘…</strong>
    </div>
    <div v-else-if="wheel?.validation_error" class="wheel-overview-error" role="alert">
      当前轮盘格式需要重新生成：{{ wheel.validation_error }}
    </div>
    <div v-else-if="wheel && !parsed" class="wheel-overview-error" role="alert">
      当前轮盘JSON无法格式化显示，请重新初始化直播智能体。
    </div>
    <div v-else-if="!wheel" class="wheel-overview-empty">
      <strong>尚未生成轮盘池</strong>
      <span>点击上方“初始化直播智能体”后，系统才会根据当前事实包生成。</span>
    </div>

    <template v-else-if="parsed">
      <div v-if="wheel?.fact_package_stale" class="wheel-overview-stale" role="status">
        <strong>事实包已经变化</strong>
        <span>下面仍是上一次确认的轮盘。请点击“初始化直播智能体”重新生成，新事实才会进入轮盘。</span>
      </div>
      <div class="wheel-point-grid">
        <article v-for="(item, index) in points" :key="item.卖点名 + ':' + index" class="wheel-point-card">
          <header>
            <i>{{ String(index + 1).padStart(2, '0') }}</i>
            <div><strong>{{ item.卖点名 }}</strong><span>{{ item.角度?.length || 0 }} 条内容</span></div>
          </header>
          <dl>
            <div><dt>目标人群</dt><dd>{{ item.目标人群 || '事实包未提供明确人群' }}</dd></div>
            <div><dt>决策问题</dt><dd>{{ item.痛点 || '事实包未提供明确问题' }}</dd></div>
          </dl>
          <ol>
            <li v-for="(angle, angleIndex) in item.角度 || []" :key="angle + ':' + angleIndex">
              <span>{{ angleIndex + 1 }}</span><p>{{ angle }}</p>
            </li>
          </ol>
        </article>
      </div>

      <section class="wheel-fact-gaps">
        <header>
          <div>
            <span>UNCONFIRMED FACT GAPS</span>
            <strong>还能补充哪些事实</strong>
            <p>这是模型根据当前品类提出的核对问题，不是商品事实。只有你填写并保存后，重新初始化时才会进入新轮盘。</p>
          </div>
          <b>{{ factGaps.length }} 项建议</b>
        </header>
        <div v-if="factGaps.length" class="wheel-gap-grid">
          <article v-for="(gap, index) in factGaps" :key="gap.建议分类 + ':' + gap.待补事实 + ':' + index">
            <div class="wheel-gap-title">
              <span>待确认</span>
              <strong>{{ gap.主题 }}</strong>
              <em>{{ gap.适用范围 }}</em>
            </div>
            <p><b>建议事实名称：</b>{{ gap.待补事实 }}</p>
            <p><b>补充价值：</b>{{ gap.补充价值 }}</p>
            <ul><li v-for="question in gap.填写提示" :key="question">{{ question }}</li></ul>
            <button type="button" @click="emit('addFact', gap)">去补充这条事实</button>
          </article>
        </div>
        <div v-else class="wheel-gap-empty">当前模型没有提出新的高价值事实缺口。</div>
      </section>
    </template>
  </section>
</template>

<style scoped>
.wheel-overview{display:grid;gap:18px;margin-top:18px;padding:20px;border:1px solid #dce5f2;border-radius:18px;background:linear-gradient(145deg,#fff,#f7f9ff);box-shadow:0 12px 34px #21375c0b}
.wheel-overview-head,.wheel-fact-gaps>header{display:flex;align-items:flex-start;justify-content:space-between;gap:18px}.wheel-overview-head>div:first-child,.wheel-fact-gaps>header>div{display:grid;gap:5px}.wheel-overview-head span,.wheel-fact-gaps>header span{color:#6478ae;font-size:11px;font-weight:900;letter-spacing:.12em}.wheel-overview-head strong,.wheel-fact-gaps>header strong{color:#172544;font-size:20px}.wheel-overview-head p,.wheel-fact-gaps>header p{margin:0;color:#66748a;font-size:13px;line-height:1.65}
.wheel-overview-actions{display:flex;align-items:center;justify-content:flex-end;gap:8px;flex-wrap:wrap}.wheel-overview-actions b,.wheel-overview-actions em,.wheel-fact-gaps>header>b{padding:6px 9px;border-radius:999px;background:#edf2ff;color:#4963a0;font-size:12px;font-style:normal}.wheel-overview-actions button,.wheel-gap-grid button{border:1px solid #cad6ec;border-radius:9px;padding:7px 12px;color:#385484;background:#fff;font:inherit;font-size:12px;font-weight:800;cursor:pointer}.wheel-overview-actions button:disabled{opacity:.55;cursor:wait}
.wheel-overview-empty,.wheel-overview-error,.wheel-overview-stale{display:grid;gap:5px;padding:18px;border-radius:12px;background:#f2f5fb;color:#66748a}.wheel-overview-empty strong{color:#263959}.wheel-overview-error{border:1px solid #f1c9ce;background:#fff4f5;color:#9d3341}.wheel-overview-stale{border:1px solid #ead19a;background:#fff9e9;color:#79613b}.wheel-overview-stale strong{color:#6f4f18}
.wheel-point-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px}.wheel-point-card{min-width:0;padding:16px;border:1px solid #dfe7f3;border-radius:14px;background:#fff}.wheel-point-card>header{display:flex;align-items:center;gap:11px}.wheel-point-card>header>i{display:grid;place-items:center;width:34px;height:34px;border-radius:10px;background:linear-gradient(140deg,#5c88ff,#7356e8);color:#fff;font-size:12px;font-style:normal;font-weight:900}.wheel-point-card>header>div{display:grid;gap:2px}.wheel-point-card>header strong{color:#172544;font-size:16px}.wheel-point-card>header span{color:#8490a1;font-size:11px}.wheel-point-card dl{display:grid;gap:7px;margin:13px 0}.wheel-point-card dl>div{display:grid;grid-template-columns:70px 1fr;gap:8px;padding:8px 10px;border-radius:9px;background:#f6f8fc}.wheel-point-card dt{color:#738098;font-size:12px;font-weight:800}.wheel-point-card dd{margin:0;color:#35445f;font-size:12px;line-height:1.55}.wheel-point-card ol{display:grid;gap:7px;margin:0;padding:0;list-style:none}.wheel-point-card li{display:flex;align-items:flex-start;gap:8px}.wheel-point-card li>span{display:grid;place-items:center;flex:0 0 22px;width:22px;height:22px;border-radius:50%;background:#eef2ff;color:#536bd0;font-size:10px;font-weight:900}.wheel-point-card li p{margin:1px 0 0;color:#26354d;font-size:13px;line-height:1.55}
.wheel-fact-gaps{display:grid;gap:13px;padding-top:17px;border-top:1px solid #dfe6f2}.wheel-fact-gaps>header>b{white-space:nowrap;background:#fff2db;color:#95651d}.wheel-gap-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.wheel-gap-grid article{display:grid;gap:9px;padding:15px;border:1px dashed #e5c886;border-radius:13px;background:#fffbf1}.wheel-gap-title{display:flex;align-items:center;gap:8px;flex-wrap:wrap}.wheel-gap-title>span{padding:4px 7px;border-radius:999px;background:#ffe6ac;color:#875b16;font-size:10px;font-weight:900}.wheel-gap-title strong{color:#523f22}.wheel-gap-title em{margin-left:auto;color:#98784a;font-size:11px;font-style:normal}.wheel-gap-grid p{margin:0;color:#67573e;font-size:12px;line-height:1.6}.wheel-gap-grid ul{display:grid;gap:4px;margin:0;padding-left:18px;color:#715f44;font-size:12px;line-height:1.55}.wheel-gap-grid button{justify-self:start;border-color:#e0c078;color:#775214;background:#fff8e6}.wheel-gap-empty{padding:14px;border-radius:10px;background:#f3f6fb;color:#718096;font-size:13px}
@media(max-width:980px){.wheel-point-grid,.wheel-gap-grid{grid-template-columns:1fr}}@media(max-width:640px){.wheel-overview{padding:14px}.wheel-overview-head,.wheel-fact-gaps>header{display:grid}.wheel-overview-actions{justify-content:flex-start}.wheel-point-card dl>div{grid-template-columns:1fr}.wheel-gap-title em{width:100%;margin-left:0}}
</style>
