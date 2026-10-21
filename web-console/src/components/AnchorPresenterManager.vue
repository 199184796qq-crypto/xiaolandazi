<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import type { LiveAnchorStyle } from '../types'
import { anchorAvatarOptions, anchorAvatarType, anchorAvatarURL, anchorPresenterAvatar, type AnchorAvatarType } from '../anchorAvatar'
const props = defineProps<{ styles: LiveAnchorStyle[]; selectedId: number | null; currentId?: number; busy: boolean; bindingBusy?: boolean; editingId: number | null; savingNameId: number | null; avatarSavingIds: number[]; descriptionSavingIds?: number[]; nameDraft: string; hasSelection: boolean; hasPlan: boolean }>()
const emit = defineEmits<{
  create: []; select: [id: number]; rename: [style: LiveAnchorStyle]; saveName: [style: LiveAnchorStyle]; cancelName: [];
  'update:nameDraft': [value: string]; avatar: [style: LiveAnchorStyle, kind: AnchorAvatarType]; description: [style: LiveAnchorStyle, text: string]; remove: [style: LiveAnchorStyle]; bind: []; plugins: []
  uploadAvatar: [style: LiveAnchorStyle, file: File]; recommendations: []; unbind: [style: LiveAnchorStyle]
  sourceMode: [style: LiveAnchorStyle, mode: 'card' | 'sample']
}>()
const orderedStyles = computed(() => [...props.styles].sort((a, b) => Number(b.id === props.currentId) - Number(a.id === props.currentId)))
const nameInput = ref<HTMLInputElement[]>([])
watch(() => props.editingId, async id => { if (id !== null) { await nextTick(); nameInput.value[0]?.focus(); nameInput.value[0]?.select() } })
const avatarMenuId = ref<number | null>(null)
const descriptionEditId = ref<number | null>(null)
const descriptionDraft = ref('')
function chooseAvatar(style: LiveAnchorStyle, kind: AnchorAvatarType) {
  avatarMenuId.value = null
  emit('avatar', style, kind)
}
function editDescription(style: LiveAnchorStyle) { descriptionEditId.value = style.id; descriptionDraft.value = Array.from(style.description || '').slice(0, 50).join('') }
function descriptionInput(event: Event) {
  const input = event.target as HTMLInputElement
  descriptionDraft.value = Array.from(input.value).slice(0, 50).join('')
  input.value = descriptionDraft.value
}
function saveDescription(style: LiveAnchorStyle) {
  if (descriptionEditId.value !== style.id) return
  emit('description', style, descriptionDraft.value.trim())
  descriptionEditId.value = null
}
function uploadAvatar(style: LiveAnchorStyle, event: Event) {
  const input = event.target as HTMLInputElement, file = input.files?.[0]
  if (file) emit('uploadAvatar', style, file)
  input.value = ''
}
function changeSourceMode(style: LiveAnchorStyle, event: Event) {
  const mode = (event.target as HTMLSelectElement).value === 'card' ? 'card' : 'sample'
  emit('sourceMode', style, mode)
}
</script>
<template>
  <aside class="presenter-manager">
    <header class="presenter-manager-header">
      <span class="manager-people" aria-hidden="true"><svg viewBox="0 0 48 48"><circle cx="19" cy="15" r="9"/><path d="M4 42v-4a15 15 0 0 1 30 0v4Z"/><circle cx="35" cy="15" r="7" opacity=".65"/><path d="M37 28a13 13 0 0 1 9 13v1h-9V28Z" opacity=".65"/></svg></span>
      <div class="manager-heading"><h3>主播管理</h3><p>创建和管理你的专属主播</p></div>
      <button type="button" class="manager-create" :disabled="busy" @click="emit('create')">＋新建主播</button>
    </header>
    <button type="button" class="manager-recommendations" @click="emit('recommendations')">✦ 系统推荐主播 <span>查看并添加 ›</span></button>
    <div v-if="!styles.length" class="manager-empty">还没有保存主播。</div>
    <article v-for="style in orderedStyles" :key="style.id" :data-presenter-id="style.id" class="presenter-card" :class="{ selected: selectedId === style.id, 'is-current': currentId === style.id }">
      <div class="presenter-avatar-column">
      <button type="button" class="presenter-select" :aria-label="`选择主播“${style.name}”`" :aria-pressed="selectedId === style.id" @click="emit('select', style.id)">
        <img :src="anchorPresenterAvatar(style)" :alt="`${style.name}的头像`" width="76" height="76" />
        <span class="avatar-voice-badge" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M4 10v4m4-7v10m4-14v18m4-14v10m4-7v4"/></svg></span>
      </button>
      <button type="button" class="avatar-menu-button" :aria-label="`选择主播“${style.name}”的头像`" :aria-expanded="avatarMenuId === style.id" :disabled="avatarSavingIds.includes(style.id)" @click="avatarMenuId = avatarMenuId === style.id ? null : style.id">{{ avatarSavingIds.includes(style.id) ? '保存中…' : '选择 ▾' }}</button>
      <div v-if="avatarMenuId === style.id" class="avatar-image-menu" aria-label="可选主播头像" @keydown.esc="avatarMenuId = null">
        <button v-for="option in anchorAvatarOptions" :key="option.value" type="button" :aria-label="option.label" :aria-pressed="anchorAvatarType(style.avatar_type) === option.value" @click="chooseAvatar(style, option.value)"><img :src="anchorAvatarURL(option.value)" :alt="option.label" width="48" height="48" /><span>{{ option.label }}</span></button>
      </div>
      <label class="avatar-upload-button">{{ avatarSavingIds.includes(style.id) ? '上传中…' : '上传头像' }}<input type="file" accept="image/jpeg,image/png" :aria-label="`上传主播“${style.name}”的头像`" :disabled="avatarSavingIds.includes(style.id)" @change="uploadAvatar(style, $event)" /></label>
      </div>
      <div class="presenter-copy">
        <div class="presenter-name-row">
          <button type="button" class="presenter-check" :class="{ checked: currentId === style.id }" :aria-label="currentId === style.id ? `当前使用主播“${style.name}”，点击查看` : `选择主播“${style.name}”`" :aria-pressed="selectedId === style.id" @click="emit('select', style.id)"><svg v-if="currentId === style.id" viewBox="0 0 24 24" aria-hidden="true"><path d="m5 12 4 4 10-10"/></svg></button>
          <input v-if="editingId === style.id" ref="nameInput" :value="nameDraft" maxlength="80" aria-label="修改主播名称" :disabled="savingNameId === style.id" @input="emit('update:nameDraft', ($event.target as HTMLInputElement).value)" @blur="emit('saveName', style)" @keydown.enter.stop.prevent="emit('saveName', style)" @keydown.esc.stop.prevent="emit('cancelName')" />
          <button v-else type="button" class="presenter-name" @click="emit('select', style.id)">{{ style.name }}</button>
          <button v-if="editingId !== style.id" type="button" class="presenter-rename" :aria-label="`修改主播“${style.name}”的名称`" @click="emit('rename', style)"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m4 15 11-11 5 5-11 11-6 1Z"/><path d="m13 6 5 5" fill="none" stroke="white" stroke-width="1.5"/></svg></button>
        </div>
        <div class="presenter-meta"><span>{{ style.sample_count }} 份样本 · {{ style.training_count }} 次训练</span></div>
        <label class="presenter-source-mode"><span>风格</span><select :value="style.style_source_mode || 'sample'" :disabled="busy" :aria-label="`主播“${style.name}”的风格来源`" @change="changeSourceMode(style, $event)"><option value="card">风格卡</option><option value="sample">风格样本</option></select></label>
        <button v-if="currentId === style.id" type="button" class="presenter-unbind" :aria-label="`取消使用主播“${style.name}”`" :disabled="!hasPlan || busy || bindingBusy" @click.stop="emit('unbind', style)">{{ bindingBusy ? '处理中…' : '取消使用' }}</button><small v-else-if="style.source_recommendation_id" class="presenter-origin">来自系统推荐</small>
      </div>
      <button type="button" class="presenter-remove" :aria-label="`删除主播“${style.name}”`" @click="emit('remove', style)">删除</button>
      <div class="presenter-description">
        <input v-if="descriptionEditId === style.id" :value="descriptionDraft" aria-label="修改主播特征介绍，最多50字" @input="descriptionInput" @blur="saveDescription(style)" @keydown.enter.prevent="saveDescription(style)" @keydown.esc.prevent="descriptionEditId = null" />
        <template v-else><p :title="style.description">{{ style.description || '未设置' }}</p><button type="button" class="presenter-rename" aria-label="修改主播特征介绍" :disabled="descriptionSavingIds?.includes(style.id)" @click="editDescription(style)">✎</button></template>
      </div>
    </article>
    <button v-if="hasSelection && selectedId !== currentId" type="button" class="manager-bind" :disabled="!hasPlan || busy || bindingBusy" @click="emit('bind')"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m5 18 11-11 3 3L8 21Zm12-16 1 3 3 1-3 1-1 3-1-3-3-1 3-1ZM6 2l1 3 3 1-3 1-1 3-1-3-3-1 3-1Z"/></svg>{{ bindingBusy ? '处理中…' : hasPlan ? '设为当前智能体主播' : '先选择智能体方案' }}</button>
    <button v-if="hasSelection" type="button" class="manager-plugins" @click="emit('plugins')"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9 3 1-2h4l1 2 2 1 2-1 2 3-1 2v3l2 1v4l-2 1-1 2 1 2-3 2-2-1h-3l-1 2H9l-1-2-2-1-2 1-2-3 1-2V9L1 8V5l2-1 2-2 2 1Z"/><circle cx="12" cy="12" r="4"/></svg><span>当前系统的个性插件</span><svg class="plugin-arrow" viewBox="0 0 24 24" aria-hidden="true"><path d="m9 5 7 7-7 7"/></svg></button>
  </aside>
</template>
<style scoped>
.presenter-manager{min-width:0;align-self:start;position:sticky;top:16px;padding:18px;border:1px solid #dfe2ff;border-radius:24px;background:radial-gradient(ellipse at 48% 8%,#e9eaffb8,transparent 48%),linear-gradient(140deg,#fff,#f6f8ff 65%,#f2f4ff);box-shadow:0 12px 28px #808bd011;display:grid;gap:18px;container-type:inline-size;color:#17233e}.presenter-manager-header{display:flex;align-items:center;gap:10px;padding:6px 0 12px}.manager-people{flex:0 0 34px;width:34px;height:34px;color:#7364e9}.manager-people svg{width:100%;fill:currentColor}.manager-heading{min-width:0;flex:1}.manager-heading h3{margin:0;font-size:20px;line-height:1.3;font-weight:900;color:#17233e}.manager-heading p{margin:6px 0 0;font-size:16px;color:#929bb2;line-height:1.5}.manager-create{flex:none;padding:13px 12px;border:0;border-radius:14px;color:#fff;background:linear-gradient(125deg,#6198ff,#6863de 70%,#5e55cc);font:inherit;font-size:16px;font-weight:850;cursor:pointer;box-shadow:0 10px 20px #5f6cd824}.presenter-card{display:grid;grid-template-columns:68px minmax(0,1fr) auto;gap:10px;align-items:center;border:1px solid #dfe3f5;border-radius:18px;padding:16px 12px;background:#ffffffa3;box-shadow:0 8px 20px #9ca6df13}.presenter-card.selected{border:1.5px solid #8276f4;background:linear-gradient(130deg,#f0f2ff,#f9faff);box-shadow:0 10px 20px #8276f413}.presenter-select{position:relative;width:68px;height:68px;padding:0;border:3px solid #c4cdf9;border-radius:50%;background:#d4dafb;cursor:pointer}.presenter-select img{display:block;width:100%;height:100%;border-radius:50%;object-fit:cover}.avatar-voice-badge{position:absolute;right:-5px;bottom:-3px;display:grid;place-items:center;width:24px;height:24px;border-radius:50%;background:linear-gradient(125deg,#9982ff,#6456ed);box-shadow:0 2px 5px #665de630}.avatar-voice-badge svg{width:16px;height:16px;fill:none;stroke:#fff;stroke-width:2;stroke-linecap:round}.presenter-copy{min-width:0;display:grid;gap:8px}.presenter-name-row{display:flex;align-items:center;gap:5px;min-width:0}.presenter-check{display:grid;place-items:center;flex:0 0 20px;height:20px;padding:0;border:1px solid #bfc7e6;border-radius:50%;background:white;color:white;cursor:pointer}.presenter-check.checked{border-color:transparent;background:linear-gradient(135deg,#7a70ef,#5464d8)}.presenter-check svg{width:14px;height:14px;fill:none;stroke:currentColor;stroke-width:3;stroke-linecap:round;stroke-linejoin:round}.presenter-name{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font:inherit;font-size:18px;font-weight:900;color:#17233e;border:0;background:none;padding:0;cursor:pointer}.presenter-rename{flex:none;width:21px;height:24px;border:0;padding:2px;background:none;color:#7464e6;cursor:pointer}.presenter-rename svg{width:100%;height:100%;fill:currentColor}.presenter-name-row input{min-width:0;width:100%;padding:4px;border:1px solid #9180ef;border-radius:6px;font:inherit;font-size:16px}.presenter-copy p{margin:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:16px;color:#949eb3;line-height:1.5}.presenter-meta{font-size:16px;color:#98a2b6}.presenter-remove{padding:4px 0;align-self:center;font:inherit;font-size:16px;font-weight:850;color:#ce4557;border:0;background:none;cursor:pointer}.presenter-avatar-choice{display:flex;gap:6px;align-items:center;color:#8590ac;font-size:16px}.presenter-avatar-choice select{min-width:0;max-width:104px;padding:3px 6px;border:1px solid #dce1f5;border-radius:7px;color:#5f5e99;background:#fff;font:inherit;font-size:16px;cursor:pointer}.presenter-avatar-choice small{font-size:16px}.manager-bind{display:flex;align-items:center;justify-content:center;gap:9px;min-height:49px;padding:12px 8px;border:0;border-radius:17px;color:#fff;background:linear-gradient(120deg,#5994fa,#6965de 70%,#6964df);box-shadow:0 12px 22px #7275de25;font:inherit;font-size:17px;font-weight:850;cursor:pointer}.manager-bind svg{width:23px;height:23px;fill:currentColor}.manager-plugins{display:flex;align-items:center;gap:10px;padding:16px 12px;border:0;border-radius:17px;background:linear-gradient(120deg,#f1f3fc,#f5f6ff);color:#596584;font:inherit;font-size:16px;font-weight:800;cursor:pointer}.manager-plugins svg{width:23px;height:23px;fill:none;stroke:currentColor;stroke-width:1.6}.manager-plugins .plugin-arrow{width:18px;height:18px;margin-left:auto;stroke-width:2.5;stroke-linecap:round;stroke-linejoin:round}.manager-empty{padding:20px 10px;color:#8a94a9;font-size:16px;line-height:1.8}button:disabled,select:disabled{opacity:.5;cursor:default}button:focus-visible,select:focus-visible{outline:3px solid #a2a9fe;outline-offset:3px}@container(max-width:310px){.presenter-manager-header{flex-wrap:wrap}.manager-create{margin-left:auto;padding:10px;font-size:16px}.manager-heading h3{font-size:18px}.presenter-card{grid-template-columns:54px minmax(0,1fr);padding:14px 10px}.presenter-select{width:54px;height:54px}.presenter-remove{grid-column:2;justify-self:end;margin-top:-3px}.presenter-name{font-size:17px}.manager-bind{font-size:16px}.manager-plugins{font-size:16px}}@media(max-width:980px){.presenter-manager{position:static}}@media(prefers-reduced-motion:reduce){*{scroll-behavior:auto}}
</style>

<style scoped>
.presenter-card{align-items:start}.presenter-avatar-column{position:relative;display:grid;justify-items:center;gap:7px}.avatar-menu-button{border:1px solid #dce1f7;border-radius:7px;background:#fff;color:#7166d5;font-size:16px;padding:3px 7px;cursor:pointer}.avatar-image-menu{position:absolute;top:100%;left:0;z-index:30;display:grid;grid-template-columns:repeat(2,1fr);gap:8px;width:160px;padding:10px;background:#fff;border:1px solid #dce1f7;border-radius:12px;box-shadow:0 12px 32px #535b9630}.avatar-image-menu button{display:grid;place-items:center;gap:4px;background:#f4f5ff;border:1px solid transparent;border-radius:9px;padding:5px;cursor:pointer;color:#6a658f;font-size:16px}.avatar-image-menu button[aria-pressed=true]{border-color:#7e6fee}.avatar-image-menu img{border-radius:50%;object-fit:cover}.presenter-copy{padding-top:12px}.presenter-description{grid-column:1/-1;display:flex;align-items:center;gap:6px;min-width:0;color:#8d98ae;font-size:16px}.presenter-description p{flex:1;min-width:0;margin:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.presenter-description input{width:100%;min-width:0;box-sizing:border-box;padding:7px;border:1px solid #aaa0ee;border-radius:7px;color:#53617e;font:inherit}.presenter-remove{align-self:start;padding-top:16px}
@container(max-width:310px){.presenter-description{grid-row:2}.presenter-remove{grid-column:2;grid-row:3;padding:0;margin:0}}
</style>
<style scoped>.manager-recommendations{display:flex;justify-content:space-between;gap:10px;padding:14px 12px;border:1px solid #dfdafa;border-radius:14px;background:#f0edff;color:#6454c8;font:inherit;font-size:16px;font-weight:750;cursor:pointer}.manager-recommendations span{font-size:16px;color:#9690c3}.avatar-upload-button{position:relative;display:block;text-align:center;margin-top:8px;font-size:16px;color:#7667da;cursor:pointer}.avatar-upload-button input{position:absolute;inset:0;opacity:0;width:100%;cursor:pointer}.presenter-current{color:#2c967c;font-size:16px}.presenter-origin{color:#8c80c0;font-size:16px}.presenter-description p{white-space:normal;overflow-wrap:anywhere;line-height:1.7;display:-webkit-box;-webkit-line-clamp:3;-webkit-box-orient:vertical;overflow:hidden}</style>

<style scoped>
.presenter-card.is-current{border:2px solid #20a67d;background:linear-gradient(130deg,#effbf6,#fafffd);box-shadow:0 8px 20px #20a67d18}
.presenter-card.is-current .presenter-check.checked{background:#20a67d}
.presenter-unbind{justify-self:start;padding:6px 14px;min-height:36px;border:1px solid #a4dec9;border-radius:999px;background:#e3f7ee;color:#15825f;font:inherit;font-size:16px;font-weight:700;cursor:pointer}
.presenter-unbind:hover:not(:disabled){background:#cff0e2;border-color:#20a67d}
.presenter-source-mode{display:flex;align-items:center;gap:8px;min-width:0;font-size:16px;color:#5f6882}
.presenter-source-mode span{flex:none;font-weight:800}
.presenter-source-mode select{min-width:0;max-width:150px;min-height:34px;padding:5px 8px;border:1px solid #dce1f7;border-radius:8px;background:#fff;color:#5f5e99;font:inherit;font-size:16px;cursor:pointer}
.presenter-card,.presenter-card p,.presenter-card small,.presenter-card span,.presenter-card button,.presenter-card label,.presenter-card input,.presenter-card select{font-size:16px}
@container(max-width:310px){.manager-recommendations{flex-wrap:wrap}.manager-recommendations span{margin-left:auto}.presenter-meta{line-height:1.6}}
</style>
