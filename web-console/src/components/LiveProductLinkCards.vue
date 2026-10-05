<script setup lang="ts">
import { ref, watch } from 'vue'
import type { LiveAgentPlanBenefit, LiveAgentPlanProductAttribute, LiveAgentPlanProductLink } from '../types'

type ProductField = 'link_key' | 'product_name' | 'spec' | 'daily_price' | 'quantity' | 'audience'
type ProductAttributeDraft = { code: string; label: string; value: string; unit?: string; display_type?: string; display_priority?: number }
const props = defineProps<{
  items: LiveAgentPlanProductLink[]
  benefits?: LiveAgentPlanBenefit[]
  saveField: (item: LiveAgentPlanProductLink, field: ProductField, value: string) => Promise<void>
  removeItem: (item: LiveAgentPlanProductLink) => Promise<void>
	addAttribute: (item: LiveAgentPlanProductLink, draft: ProductAttributeDraft) => Promise<void>
	saveAttribute: (item: LiveAgentPlanProductLink, attribute: LiveAgentPlanProductAttribute, draft: ProductAttributeDraft) => Promise<void>
	removeAttribute: (item: LiveAgentPlanProductLink, attribute: LiveAgentPlanProductAttribute) => Promise<void>
}>()
const fields: { key: ProductField; label: string; path: string }[] = [
  { key: 'spec', label: '规格', path: 'm12 3 9 5v8l-9 5-9-5V8zM3 8l9 5 9-5M12 13v8M7.5 5.5l9 5V14' },
  { key: 'daily_price', label: '日常价', path: 'M20 3h-7L3 13l8 8L21 11V4a1 1 0 0 0-1-1zM17 7h.01' },
  { key: 'quantity', label: '数量', path: 'm12 3 9 5-9 5-9-5zM3 12l9 5 9-5M3 16l9 5 9-5' },
  { key: 'audience', label: '适用', path: 'M12 7a3 3 0 1 1-6 0 3 3 0 0 1 6 0ZM3 21v-3a6 6 0 0 1 12 0v3zM16 4a3 3 0 0 1 0 6M18 14a5 5 0 0 1 3 4v3' },
]
const labels: Record<ProductField, string> = { link_key: '链接编号', product_name: '商品名称', spec: '规格', daily_price: '日常价', quantity: '数量', audience: '适用' }
const edit = ref<{ item: LiveAgentPlanProductLink; field: ProductField; draft: string } | null>(null)
const busy = ref(false)
const message = ref<{ id: number; text: string; error?: boolean } | null>(null)
const expandedItems = ref<Set<number>>(new Set())
const attributeEdit = ref<{
	itemId: number
	attribute: LiveAgentPlanProductAttribute | null
	draft: ProductAttributeDraft
} | null>(null)
const vFocus = { mounted: (element: HTMLInputElement | HTMLTextAreaElement) => { element.focus(); element.select() } }

function isEditing(item: LiveAgentPlanProductLink, field: ProductField) {
  return edit.value?.item.id === item.id && edit.value.field === field
}

async function saveEdit() {
  if (busy.value) return false
  const current = edit.value
  if (!current) return true
  const value = current.draft.trim()
  if ((current.field === 'link_key' || current.field === 'product_name') && !value) {
    message.value = { id: current.item.id, text: labels[current.field] + '不能为空', error: true }
    return false
  }
  if (value === String(current.item[current.field] || '').trim()) {
    edit.value = null
    return true
  }
  busy.value = true
  message.value = { id: current.item.id, text: '正在保存…' }
  try {
    await props.saveField(current.item, current.field, value)
    edit.value = null
    message.value = { id: current.item.id, text: '已自动保存' }
    return true
  } catch (error) {
    message.value = { id: current.item.id, text: error instanceof Error ? error.message : '保存失败，请重试', error: true }
    return false
  } finally {
    busy.value = false
  }
}

async function startEdit(item: LiveAgentPlanProductLink, field: ProductField) {
  if (busy.value || isEditing(item, field)) return
  if (edit.value && !await saveEdit()) return
  // Use the latest returned version when moving between fields on the same card.
  const latest = props.items.find((candidate) => candidate.id === item.id)
  if (!latest) return
  edit.value = { item: { ...latest }, field, draft: String(latest[field] || '') }
  message.value = null
}

function cancelEdit() {
  if (busy.value) return
  edit.value = null
  message.value = null
}

function toggleAttributes(item: LiveAgentPlanProductLink) {
	const next = new Set(expandedItems.value)
	if (next.has(item.id)) {
		next.delete(item.id)
		if (attributeEdit.value?.itemId === item.id) attributeEdit.value = null
	} else {
		next.add(item.id)
	}
	expandedItems.value = next
}

function attributeValue(attribute: LiveAgentPlanProductAttribute) {
	return attribute.value + (attribute.unit || '')
}

function startAddAttribute(item: LiveAgentPlanProductLink) {
	if (busy.value) return
	const next = new Set(expandedItems.value)
	next.add(item.id)
	expandedItems.value = next
	attributeEdit.value = {
		itemId: item.id,
		attribute: null,
		draft: { code: '', label: '', value: '', unit: '', display_type: 'text', display_priority: ((item.attributes || []).length + 1) * 10 },
	}
	message.value = null
}

function startEditAttribute(item: LiveAgentPlanProductLink, attribute: LiveAgentPlanProductAttribute) {
	if (busy.value) return
	attributeEdit.value = {
		itemId: item.id,
		attribute: { ...attribute },
		draft: {
			code: attribute.code,
			label: attribute.label,
			value: attribute.value,
			unit: attribute.unit || '',
			display_type: attribute.display_type || 'text',
			display_priority: attribute.display_priority,
		},
	}
	message.value = null
}

function cancelAttributeEdit() {
	if (!busy.value) attributeEdit.value = null
}

async function submitAttribute(item: LiveAgentPlanProductLink) {
	const current = attributeEdit.value
	if (busy.value || !current || current.itemId !== item.id) return
	const label = current.draft.label.trim()
	const value = current.draft.value.trim()
	if (!label || !value) {
		message.value = { id: item.id, text: '个性属性名称和值不能为空', error: true }
		return
	}
	const code = (current.draft.code.trim() || label.replace(/\s+/g, '_')).toLowerCase()
	const draft = { ...current.draft, code, label, value, unit: current.draft.unit?.trim() || '' }
	busy.value = true
	message.value = { id: item.id, text: '正在保存个性属性…' }
	try {
		if (current.attribute) await props.saveAttribute(item, current.attribute, draft)
		else await props.addAttribute(item, draft)
		attributeEdit.value = null
		message.value = { id: item.id, text: '个性属性已保存，并会进入话术事实' }
	} catch (error) {
		message.value = { id: item.id, text: error instanceof Error ? error.message : '保存个性属性失败', error: true }
	} finally {
		busy.value = false
	}
}

async function deleteAttribute(item: LiveAgentPlanProductLink, attribute: LiveAgentPlanProductAttribute) {
	if (busy.value || !window.confirm(`确定删除个性属性“${attribute.label}”吗？删除后不会再进入话术生成。`)) return
	busy.value = true
	message.value = { id: item.id, text: '正在删除个性属性…' }
	try {
		await props.removeAttribute(item, attribute)
		if (attributeEdit.value?.attribute?.id === attribute.id) attributeEdit.value = null
		message.value = { id: item.id, text: '个性属性已删除干净' }
	} catch (error) {
		message.value = { id: item.id, text: error instanceof Error ? error.message : '删除个性属性失败', error: true }
	} finally {
		busy.value = false
	}
}

function linkedBenefits(item: LiveAgentPlanProductLink) {
  return (props.benefits || []).filter((benefit) => benefit.status === 'active' && benefit.link_key === item.link_key)
}

function benefitSummary(item: LiveAgentPlanBenefit) {
  return [item.activity_price && `活动价 ${item.activity_price}`, item.gift && `赠品 ${item.gift}`, item.activity]
    .filter(Boolean)
    .join(' · ')
}

async function deleteItem(item: LiveAgentPlanProductLink) {
  if (busy.value) return
  const benefitCount = linkedBenefits(item).length
  const benefitWarning = benefitCount ? `，并同步停用 ${benefitCount} 条链接专属福利` : ''
  if (!window.confirm(`确定删除“${item.link_key} · ${item.product_name || '未填写商品名称'}”吗？删除后当前方案将不再使用这个商品链接${benefitWarning}。`)) return
  busy.value = true
  message.value = { id: item.id, text: '正在删除…' }
  try {
    await props.removeItem(item)
    if (edit.value?.item.id === item.id) edit.value = null
    message.value = null
  } catch (error) {
    message.value = { id: item.id, text: error instanceof Error ? error.message : '删除失败，请重试', error: true }
  } finally {
    busy.value = false
  }
}

watch(() => props.items.map((item) => item.id), (ids) => {
  if (edit.value && !ids.includes(edit.value.item.id)) edit.value = null
	if (attributeEdit.value && !ids.includes(attributeEdit.value.itemId)) attributeEdit.value = null
	expandedItems.value = new Set([...expandedItems.value].filter((id) => ids.includes(id)))
})
</script>

<template>
  <div class="product-cards">
    <article v-for="item in items" :key="item.id" class="product-card">
      <header class="product-card-header">
        <div class="product-card-link">
          <input v-if="isEditing(item, 'link_key') && edit" v-model="edit.draft" v-focus :disabled="busy" maxlength="64" :aria-label="'编辑' + item.link_key + '的链接编号'" @blur="saveEdit" @keydown.enter.prevent="saveEdit" @keydown.esc.prevent="cancelEdit" />
          <span v-else>{{ item.link_key }}</span>
          <button type="button" class="product-edit" :disabled="busy" :aria-label="'修改' + item.link_key + '的链接编号'" title="修改链接编号" @mousedown.prevent @click="startEdit(item, 'link_key')"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m15 5 4 4M4 20l4-1L20 7a2.8 2.8 0 0 0-4-4L4 15z" /></svg></button>
        </div>
        <span class="product-status" :class="{ inactive: item.status !== 'active' }"><i></i>{{ item.status === 'active' ? '生效中' : '未启用' }}</span>
      </header>
      <div class="product-name">
        <textarea v-if="isEditing(item, 'product_name') && edit" v-model="edit.draft" v-focus :disabled="busy" maxlength="255" rows="2" :aria-label="'编辑' + item.link_key + '的商品名称'" @blur="saveEdit" @keydown.enter.exact.prevent="saveEdit" @keydown.esc.prevent="cancelEdit" />
        <strong v-else>{{ item.product_name || '未填写商品名称' }}</strong>
        <button type="button" class="product-edit" :disabled="busy" :aria-label="'修改' + item.link_key + '的商品名称'" title="修改商品名称" @mousedown.prevent @click="startEdit(item, 'product_name')"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m15 5 4 4M4 20l4-1L20 7a2.8 2.8 0 0 0-4-4L4 15z" /></svg></button>
      </div>
      <small class="product-version">V{{ item.version_no }}</small>
      <dl class="product-details">
        <div v-for="field in fields" :key="field.key">
          <dt><svg viewBox="0 0 24 24" aria-hidden="true"><path :d="field.path" /></svg><span>{{ field.label }}</span></dt>
          <dd>
            <textarea v-if="isEditing(item, field.key) && edit" v-model="edit.draft" v-focus :disabled="busy" :maxlength="field.key === 'audience' ? 512 : 255" rows="2" :aria-label="'编辑' + item.link_key + '的' + field.label" @blur="saveEdit" @keydown.enter.exact.prevent="saveEdit" @keydown.esc.prevent="cancelEdit" />
            <span v-else>{{ item[field.key] || '—' }}</span>
            <button type="button" class="product-edit" :disabled="busy" :aria-label="'修改' + item.link_key + '的' + field.label" :title="'修改' + field.label" @mousedown.prevent @click="startEdit(item, field.key)"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m15 5 4 4M4 20l4-1L20 7a2.8 2.8 0 0 0-4-4L4 15z" /></svg></button>
          </dd>
        </div>
      </dl>
	  <section class="product-personalized" :class="{ expanded: expandedItems.has(item.id) }">
		<button type="button" class="product-personalized-toggle" :aria-expanded="expandedItems.has(item.id)" :aria-controls="'product-attributes-' + item.id" @click="toggleAttributes(item)">
		  <span><b>个性属性</b><small>{{ (item.attributes || []).length ? `${item.attributes.length} 项 · 按商品品类反推` : '暂无 · 可手动添加' }}</small></span>
		  <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg>
		</button>
		<div v-if="expandedItems.has(item.id)" :id="'product-attributes-' + item.id" class="product-personalized-body">
		  <div v-if="!(item.attributes || []).length && attributeEdit?.itemId !== item.id" class="product-attribute-empty">这个商品还没有个性属性。可添加本品类独有、并能作为话术事实的信息。</div>
		  <dl v-if="(item.attributes || []).length" class="product-attribute-list">
			<div v-for="attribute in item.attributes" :key="attribute.id">
			  <dt>{{ attribute.label }}</dt>
			  <dd><span>{{ attributeValue(attribute) }}</span><div><button type="button" class="product-edit" :disabled="busy" :aria-label="'修改个性属性' + attribute.label" @click="startEditAttribute(item, attribute)"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m15 5 4 4M4 20l4-1L20 7a2.8 2.8 0 0 0-4-4L4 15z" /></svg></button><button type="button" class="product-attribute-delete" :disabled="busy" :aria-label="'删除个性属性' + attribute.label" @click="deleteAttribute(item, attribute)"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16M9 7V4h6v3M7 7l1 13h8l1-13" /></svg></button></div></dd>
			</div>
		  </dl>
		  <form v-if="attributeEdit?.itemId === item.id" class="product-attribute-form" @submit.prevent="submitAttribute(item)">
			<strong>{{ attributeEdit.attribute ? '修改个性属性' : '添加个性属性' }}</strong>
			<label><span>属性名称</span><input v-model="attributeEdit.draft.label" v-focus :disabled="busy" maxlength="96" placeholder="如：压榨工艺、面料、功率" /></label>
			<label><span>属性值</span><textarea v-model="attributeEdit.draft.value" :disabled="busy" maxlength="2000" rows="2" placeholder="填写可核实、可用于话术的商品事实" /></label>
			<label class="product-attribute-unit"><span>单位（可选）</span><input v-model="attributeEdit.draft.unit" :disabled="busy" maxlength="48" placeholder="如 L、W、%" /></label>
			<div><button type="button" :disabled="busy" @click="cancelAttributeEdit">取消</button><button type="submit" class="primary" :disabled="busy">保存属性</button></div>
		  </form>
		  <button v-else type="button" class="product-attribute-add" :disabled="busy" @click="startAddAttribute(item)">＋ 添加个性属性</button>
		  <p>仅个性属性放在这里；规格、价格、数量、适用仍在上方通用字段。保存后自动成为话术事实。</p>
		</div>
	  </section>
      <section v-if="linkedBenefits(item).length" class="product-benefits" aria-label="当前有效福利事实">
        <header><strong>当前福利事实</strong><span>会随商品事实一起进入话术生成</span></header>
        <div v-for="benefit in linkedBenefits(item)" :key="benefit.id">
          <b>{{ benefitSummary(benefit) || benefit.key }}</b>
          <small v-if="benefit.ends_at">有效至 {{ new Date(benefit.ends_at).toLocaleString('zh-CN', { hour12: false }) }}</small>
        </div>
      </section>
      <footer class="product-card-footer">
        <span role="status" :class="{ error: message?.id === item.id && message.error }">{{ message?.id === item.id ? message.text : '通用字段常显，个性属性可展开' }}</span>
        <button type="button" class="product-delete" :disabled="busy" :aria-label="'删除' + item.link_key" @mousedown.prevent @click="deleteItem(item)"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7M14 10v7" /></svg>删除</button>
      </footer>
    </article>
  </div>
</template>

<style scoped>
.product-cards { display:grid; grid-template-columns:repeat(3,minmax(0,1fr)); gap:16px; min-width:0; }
.product-card { --accent:#4d62ff; --tint:#edf0ff; position:relative; display:flex; flex-direction:column; gap:8px; min-width:0; padding:16px; border:1px solid #dfe5ff; border-radius:14px; background:linear-gradient(135deg,#fff,#fcfdff); box-shadow:0 5px 16px rgba(67,82,158,.05); transition:transform .2s ease,box-shadow .2s ease,border-color .2s ease; }
.product-card:nth-child(even) { --accent:#9450ed; --tint:#f3edff; }
.product-card::before { content:''; position:absolute; left:0; top:18px; width:4px; height:100px; max-height:35%; border-radius:0 4px 4px 0; background:var(--accent); }
.product-card:hover,.product-card:focus-within { transform:translateY(-4px); border-color:var(--accent); box-shadow:0 0 0 1px var(--accent),0 0 20px color-mix(in srgb,var(--accent) 22%,transparent),0 12px 26px rgba(67,82,158,.13); }
.product-card-header { display:flex; justify-content:space-between; align-items:center; gap:8px; }
.product-card-link { display:flex; align-items:center; gap:3px; min-width:0; padding:5px 8px; border-radius:10px; background:var(--tint); color:var(--accent); font-size:15px; font-weight:850; }
.product-card-link>span { overflow-wrap:anywhere; }
.product-status { display:inline-flex; align-items:center; gap:5px; padding:5px 8px; border-radius:10px; background:var(--tint); color:var(--accent); font-size:13px; white-space:nowrap; }
.product-status i { width:6px; height:6px; border-radius:50%; background:currentColor; }
.product-status.inactive { color:#7d8798; background:#f1f3f7; }
.product-name { display:flex; align-items:flex-start; gap:4px; min-width:0; margin-top:5px; }
.product-name strong { flex:1; min-width:0; color:#172440; font-size:17px; line-height:1.5; font-weight:850; overflow-wrap:anywhere; }
.product-version { color:#909bb3; font-size:13px; }
.product-details { display:grid; gap:8px; margin:5px 0 0; min-width:0; }
.product-details>div { display:grid; grid-template-columns:75px minmax(0,1fr); gap:6px; align-items:center; box-sizing:border-box; min-height:46px; padding:6px 9px; border-radius:10px; background:linear-gradient(135deg,#f7f8ff,#f2f4fc); }
.product-details dt { display:flex; align-items:center; gap:8px; color:#34415c; font-size:13px; white-space:nowrap; }
.product-details dt svg { width:20px; height:20px; flex:0 0 20px; }
.product-details dd { display:flex; align-items:flex-start; gap:3px; min-width:0; margin:0; color:#24334f; font-size:14px; line-height:1.5; overflow-wrap:anywhere; }
.product-details dd>span { flex:1; min-width:0; padding:4px 6px; border-radius:7px; background:rgba(229,234,252,.55); }
.product-personalized { overflow:hidden; border:1px solid #e5e9f5; border-radius:11px; background:#fbfcff; }
.product-personalized.expanded { border-color:color-mix(in srgb,var(--accent) 28%,#e5e9f5); }
.product-personalized-toggle { display:flex; align-items:center; justify-content:space-between; gap:10px; width:100%; padding:10px 11px; border:0; background:transparent; color:#34415c; text-align:left; cursor:pointer; }
.product-personalized-toggle>span { display:flex; align-items:baseline; gap:8px; min-width:0; }
.product-personalized-toggle b { color:var(--accent); font-size:14px; white-space:nowrap; }
.product-personalized-toggle small { overflow:hidden; color:#8791a5; font-size:12px; text-overflow:ellipsis; white-space:nowrap; }
.product-personalized-toggle>svg { width:18px; height:18px; flex:0 0 18px; transition:transform .18s ease; }
.product-personalized.expanded .product-personalized-toggle>svg { transform:rotate(180deg); }
.product-personalized-body { display:grid; gap:8px; padding:0 10px 10px; border-top:1px solid #edf0f7; }
.product-attribute-empty { margin-top:9px; padding:9px; border-radius:8px; background:#f5f7fc; color:#778297; font-size:13px; line-height:1.55; }
.product-attribute-list { display:grid; gap:6px; margin:9px 0 0; }
.product-attribute-list>div { display:grid; grid-template-columns:minmax(76px,.4fr) minmax(0,1fr); gap:8px; align-items:center; min-height:38px; padding:6px 8px; border-radius:8px; background:var(--tint); }
.product-attribute-list dt { color:#56627a; font-size:13px; overflow-wrap:anywhere; }
.product-attribute-list dd { display:flex; align-items:center; justify-content:space-between; gap:5px; min-width:0; margin:0; color:#24334f; font-size:14px; font-weight:650; }
.product-attribute-list dd>span { min-width:0; overflow-wrap:anywhere; }
.product-attribute-list dd>div { display:flex; flex:0 0 auto; }
.product-attribute-delete { display:inline-flex; align-items:center; justify-content:center; width:24px; height:24px; padding:4px; border:0; border-radius:6px; background:transparent; cursor:pointer; }
.product-attribute-delete svg { width:15px; height:15px; stroke:#c85b6b; }
.product-attribute-delete:hover { background:#ffedf0; }
.product-attribute-form { display:grid; grid-template-columns:1fr 1fr; gap:8px; margin-top:9px; padding:10px; border:1px solid color-mix(in srgb,var(--accent) 25%,#e2e6f2); border-radius:9px; background:#fff; }
.product-attribute-form>strong,.product-attribute-form>label:nth-of-type(2),.product-attribute-form>div { grid-column:1/-1; }
.product-attribute-form>strong { color:var(--accent); font-size:14px; }
.product-attribute-form label { display:grid; gap:4px; color:#667087; font-size:12px; }
.product-attribute-form input,.product-attribute-form textarea { padding:6px 7px; }
.product-attribute-form>div { display:flex; justify-content:flex-end; gap:7px; }
.product-attribute-form>div button,.product-attribute-add { padding:6px 9px; border:1px solid #dce2ef; border-radius:7px; background:#fff; color:#5e6980; font:inherit; font-size:13px; cursor:pointer; }
.product-attribute-form>div button.primary { border-color:var(--accent); background:var(--accent); color:#fff; }
.product-attribute-add { justify-self:start; border-color:color-mix(in srgb,var(--accent) 35%,#dce2ef); color:var(--accent); }
.product-personalized-body>p { margin:0; color:#9099aa; font-size:12px; line-height:1.5; }
.product-benefits { display:grid; gap:7px; padding:10px; border:1px solid color-mix(in srgb,var(--accent) 18%,#e5eaf5); border-radius:11px; background:color-mix(in srgb,var(--tint) 58%,#fff); }
.product-benefits>header { display:flex; align-items:baseline; justify-content:space-between; gap:8px; }
.product-benefits>header strong { color:var(--accent); font-size:14px; }
.product-benefits>header span { color:#7c879c; font-size:12px; text-align:right; }
.product-benefits>div { display:grid; gap:2px; padding:7px 8px; border-radius:8px; background:rgba(255,255,255,.76); }
.product-benefits b { color:#33415d; font-size:14px; line-height:1.45; }
.product-benefits small { color:#8b95a8; font-size:12px; }
svg { fill:none; stroke:var(--accent); stroke-width:1.8; stroke-linecap:round; stroke-linejoin:round; }
.product-edit { display:inline-flex; align-items:center; justify-content:center; flex:0 0 24px; width:24px; height:24px; padding:4px; border:0; border-radius:6px; background:transparent; cursor:pointer; }
.product-edit svg { width:16px; height:16px; }
.product-edit:hover { background:var(--tint); }
.product-card input,.product-card textarea { flex:1; width:100%; min-width:0; box-sizing:border-box; padding:5px 6px; border:1px solid var(--accent); border-radius:6px; background:#fff; color:#24334f; font:inherit; line-height:1.5; resize:vertical; }
.product-card input:focus,.product-card textarea:focus { outline:2px solid var(--tint); }
.product-card-footer { display:flex; align-items:center; justify-content:space-between; gap:8px; margin-top:auto; padding-top:8px; }
.product-card-footer>span { color:#8994aa; font-size:12px; line-height:1.5; }
.product-card-footer>span.error { color:#c84858; }
.product-delete { display:inline-flex; align-items:center; gap:4px; flex-shrink:0; padding:4px 6px; border:1px solid #f2d9df; border-radius:6px; background:#fff8fa; color:#c85b6b; font:inherit; font-size:13px; cursor:pointer; }
.product-delete svg { width:14px; height:14px; stroke:currentColor; }
.product-delete:hover { background:#ffedf0; border-color:#de9aa7; }
button:disabled { cursor:wait; opacity:.5; }
button:focus-visible { outline:2px solid var(--accent); outline-offset:2px; }
@media(max-width:1200px) { .product-cards { grid-template-columns:repeat(2,minmax(0,1fr)); } }
@media(max-width:760px) { .product-cards { grid-template-columns:minmax(0,1fr); } }
@media(prefers-reduced-motion:reduce) { .product-card { transition:none; } .product-card:hover,.product-card:focus-within { transform:none; } }
</style>
