<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { session } from '../session'
import { getPlanQAPairs, preparePlanQAPairs, commitPlanQAPairs, organizePlanQAPairs, type QAEntry, type QAPage, type QAProposal, type QACommit, type MaterialObjectTarget } from '../api'

const props=defineProps<{planId:number}>()
const emit=defineEmits<{test:[question:string]}>()
const empty=():QAPage=>({revision:'',object_revision:'',items:[],links:[],stores:[]})
const page=ref<QAPage>(empty()),raw=ref(''),defaultOwners=ref<string[]>(['plan']),preview=ref<QAEntry[]>([]),proposals=ref<QAProposal[]>([]),choices=ref<Record<string,string>>({}),questions=ref<string[]>([])
const inputMode=ref<'single'|'batch'>('single'),question=ref(''),answer=ref('')
const busy=ref(false),loaded=ref(false),error=ref(''),notice=ref(''),search=ref(''),ownerFilter=ref('all'),pending=ref<QACommit|null>(null),pendingRaw=ref(''),storageNotice=ref('')
const duplicateSelection=ref<Record<string,string>>({})
const actor=computed(()=>`${session.bootstrap?.actor?.tenant_id||0}:${session.bootstrap?.actor?.user_id||0}`)
const storageKey=computed(()=>`qa-pairs:v1:${actor.value}:${props.planId}`)
let epoch=0
const clone=<T,>(x:T):T=>JSON.parse(JSON.stringify(x))
const options=computed(()=>[
 {key:'plan',label:'全局',target:{kind:'plan'} as MaterialObjectTarget},
 ...page.value.links.map(l=>({key:`product:${l.id}`,label:`${l.link_key}号链接${l.product_name?` · ${l.product_name}`:''}`,target:{kind:'product',link_id:l.id} as MaterialObjectTarget})),
 ...page.value.stores.map(s=>({key:`store:${s.name}`,label:s.name,target:{kind:'store',store_name:s.name} as MaterialObjectTarget})),
])
function targetKey(t:MaterialObjectTarget){return t.kind==='plan'?'plan':t.kind==='product'?`product:${t.link_id}`:`store:${t.store_name}`}
function ownerLabel(ts:MaterialObjectTarget[]){return ts.length?ts.map(t=>options.value.find(o=>o.key===targetKey(t))?.label||'对象已变化').join('、'):'待归属'}
function selectedTargets(keys:string[]){return keys.map(k=>options.value.find(o=>o.key===k)?.target).filter((t):t is MaterialObjectTarget=>!!t)}
function toggleOwner(keys:string[],key:string,checked:boolean){if(key==='plan')return checked?['plan']:[];return checked?[...keys.filter(k=>k!=='plan'&&k!==key),key]:keys.filter(k=>k!==key)}
function setEntryOwner(e:QAEntry,key:string,checked:boolean){e.object_targets=selectedTargets(toggleOwner(e.object_targets.map(targetKey),key,checked));if(!e.object_targets.length)e.enabled=false;invalidate()}
function invalidate(){proposals.value=[];choices.value={}}
const visible=computed(()=>page.value.items.filter(e=>{
 const matchesOwner=ownerFilter.value==='all'||ownerFilter.value==='pending'&&!e.object_targets.length||e.object_targets.some(t=>targetKey(t)===ownerFilter.value)
 return matchesOwner&&`${e.question} ${e.answer} ${e.aliases.join(' ')} ${ownerLabel(e.object_targets)}`.toLocaleLowerCase().includes(search.value.trim().toLocaleLowerCase())
}))
const keyQuestion=(s:string)=>s.toLocaleLowerCase().replace(/[\s\p{P}]/gu,'')
const duplicates=computed(()=>{
 const groups=new Map<string,QAEntry[]>()
 for(const e of preview.value){const k=keyQuestion(e.question)+'|'+e.object_targets.map(targetKey).sort().join('|');if(!groups.has(k))groups.set(k,[]);groups.get(k)!.push(e)}
 return [...groups.entries()].filter(([,rows])=>rows.length>1).map(([key,rows])=>({key,rows}))
})
function keepDuplicate(key:string){const id=duplicateSelection.value[key];if(!id){error.value='请选择要保留的问答';return};const group=duplicates.value.find(g=>g.key===key);if(!group)return;const remove=new Set(group.rows.filter(e=>e.client_id!==id).map(e=>e.client_id));preview.value=preview.value.filter(e=>!remove.has(e.client_id));invalidate();notice.value='已保留选中问答，核对后保存。'}
function persist(){try{localStorage.setItem(storageKey.value,JSON.stringify({raw:raw.value,inputMode:inputMode.value,question:question.value,answer:answer.value,owners:defaultOwners.value,preview:preview.value,pending:pending.value,pendingRaw:pendingRaw.value}))}catch{storageNotice.value='浏览器未能保存未确认内容，请保留原文。'}}
function inputSnapshot(){return inputMode.value==='batch'?raw.value:JSON.stringify({question:question.value,answer:answer.value})}
function returnToSingleInput(){if(preview.value.length&&!confirm('切换填写方式会清除未保存预览，原输入仍保留。继续？'))return;inputMode.value='single';preview.value=[];questions.value=[];invalidate()}
async function run(fn:()=>Promise<void>){if(busy.value)return;const token=epoch;busy.value=true;error.value='';notice.value='';try{await fn()}catch(e){if(token===epoch)error.value=e instanceof Error?e.message:'操作失败，请保留输入重试'}finally{if(token===epoch)busy.value=false}}
async function load(){const token=epoch;try{const out=await getPlanQAPairs(props.planId);if(token!==epoch)return;page.value=out;loaded.value=true;error.value=''}catch(e){if(token===epoch)error.value=e instanceof Error?e.message:'读取问答失败'}}

function parseInput(){
 if(inputMode.value==='single'){
  if(!question.value.trim())throw Error('请填写问题');if(!answer.value.trim())throw Error('请填写回答')
  if([...question.value].length>300||[...answer.value].length>2000)throw Error('问题最多300字，回答最多2000字')
  return [{revision:0,client_id:crypto.randomUUID(),question:question.value,answer:answer.value,aliases:[],object_targets:clone(selectedTargets(defaultOwners.value)),enabled:defaultOwners.value.length>0,original:`问题：${question.value}\n回答：${answer.value}`}] satisfies QAEntry[]
 }
 if(!raw.value.trim())throw Error('请填写问题和回答');if([...raw.value].length>20000)throw Error('一次最多20000字')
 const rows:QAEntry[]=[];let row:QAEntry|null=null,field=''
 for(const line of raw.value.replaceAll('\r\n','\n').split('\n')){
  const m=line.trim().match(/^(问题|回答|相似问法|适用条件|例外)\s*[:：]\s*(.*)$/)
  if(m?.[1]==='问题'){row={revision:0,client_id:crypto.randomUUID(),question:m[2]||'',answer:'',aliases:[],object_targets:clone(selectedTargets(defaultOwners.value)),enabled:defaultOwners.value.length>0,original:line};rows.push(row);field='问题';continue}
  if(!row){if(line.trim())throw Error('标准格式以“问题：”开始；大白话描述请先整理');continue}
  row.original+='\n'+line
  if(m){field=m[1]!;const value=m[2]||'';if(field==='回答'){if(row.answer)throw Error('出现重复回答标记，请核对边界');row.answer=value}else if(field==='相似问法')row.aliases=value.split(/[|｜;；]/).map(x=>x.trim()).filter(Boolean);else if(field==='适用条件')row.conditions=value;else if(field==='例外')row.exceptions=value;continue}
  if(field==='回答')row.answer+='\n'+line;else if(line.trim()){if(field==='适用条件')row.conditions+='\n'+line;else if(field==='例外')row.exceptions+='\n'+line;else throw Error('问答边界不明确，请核对问题和回答标记')}
 }
 if(!rows.length||rows.length>100)throw Error('请一次保存1至100条问答')
 for(const e of rows){e.answer=e.answer.replace(/\n+$/,'');if(!e.question.trim()||!e.answer.trim())throw Error('每条都需要完整问题和回答；不会自动补写答案')}
 return rows
}
async function commit(){const request=pending.value;if(!request)return;const token=epoch,plan=props.planId,snapshot=pendingRaw.value,ownsEditor=request.items.some(item=>preview.value.some(e=>e.client_id===item.client_id));persist();const out=await commitPlanQAPairs(plan,clone(request));if(token!==epoch)return;page.value={...page.value,...out};pending.value=null;if(ownsEditor){preview.value=[];invalidate();questions.value=[];if(inputSnapshot()===snapshot){if(inputMode.value==='batch')raw.value='';else{question.value='';answer.value=''}}}notice.value='保存完成。已入库问答可在弹幕测试中使用。';persist()}
async function save(){await run(async()=>{
 if(pending.value){await commit();return}
 if(!preview.value.length)preview.value=parseInput()
 if(duplicates.value.length){notice.value='本次存在重复问题，请先选择要保留的条目。';return}
 const token=epoch,items=clone(preview.value),out=await preparePlanQAPairs(props.planId,items);if(token!==epoch)return
 proposals.value=out.proposals;preview.value=out.proposals.map(p=>p.item)
 if(out.proposals.some(p=>p.conflicts.length&&!p.identical&&!choices.value[p.item.client_id!])){notice.value='已有回答，请选择使用本次或保留库中，再保存。';return}
 pending.value={operation_id:crypto.randomUUID(),revision:out.revision,object_revision:out.object_revision,items:clone(preview.value),choices:clone(choices.value)};pendingRaw.value=inputSnapshot();await commit()
 })}
async function organize(){await run(async()=>{if([...raw.value].length>8000)throw Error('大白话整理一次最多8000字');const token=epoch,out=await organizePlanQAPairs(props.planId,raw.value);if(token!==epoch)return;preview.value=out.items.map(e=>({...e,client_id:crypto.randomUUID(),revision:0}));questions.value=out.questions||[];invalidate();notice.value='已整理为建议。请核对答案和归属后保存，尚未入库。';persist()})}
function edit(e:QAEntry){if((raw.value.trim()||question.value.trim()||answer.value.trim()||preview.value.length)&&!confirm('当前输入或预览尚未保存，切换编辑会替换它们。继续？'))return;raw.value='';question.value='';answer.value='';preview.value=[{...clone(e),client_id:crypto.randomUUID()}];invalidate();notice.value=e.object_targets.length>1?`修改会同时影响：${ownerLabel(e.object_targets)}。`:'修改后点击保存。'}
async function toggle(e:QAEntry){await run(async()=>{const item={...clone(e),enabled:!e.enabled,client_id:crypto.randomUUID()};if(item.enabled&&!item.object_targets.length)throw Error('请先编辑并完成归属');const token=epoch,p=await preparePlanQAPairs(props.planId,[item]);if(token!==epoch)return;pending.value={operation_id:crypto.randomUUID(),revision:p.revision,object_revision:p.object_revision,items:[item],choices:{[item.client_id]:'replace'}};pendingRaw.value='';await commit()})}
async function remove(e:QAEntry){if(!confirm(`删除“${e.question}”？归属：${ownerLabel(e.object_targets)}。`))return;await run(async()=>{pending.value={operation_id:crypto.randomUUID(),revision:page.value.revision,object_revision:page.value.object_revision,items:[],choices:{},delete_id:e.id};pendingRaw.value='';await commit()})}
function restart(){if(!confirm('上次请求可能已产生向量费用。重新发起可能再次计费；建议先刷新核对库中结果。仍要重新发起？'))return;pending.value=null;notice.value='已解除上次请求关联。核对后点击保存重新发起。';persist()}
function dateValue(s?:string){if(!s)return '';const d=new Date(s);return Number.isNaN(d.valueOf())?'':new Date(d.valueOf()-d.getTimezoneOffset()*60000).toISOString().slice(0,16)}
function setDate(e:QAEntry,key:'valid_from'|'valid_until',value:string){e[key]=value?new Date(value).toISOString():'';invalidate()}
function entryStatus(e:QAEntry){if(!e.object_targets.length)return '待归属';if(!e.enabled)return '停用';if(e.valid_until&&Date.now()>=Date.parse(e.valid_until))return '已到期';if(e.valid_from&&Date.now()<Date.parse(e.valid_from))return '待生效';return '启用'}
watch([()=>props.planId,actor],()=>{epoch++;busy.value=false;loaded.value=false;page.value=empty();raw.value='';question.value='';answer.value='';inputMode.value='single';defaultOwners.value=['plan'];preview.value=[];pending.value=null;pendingRaw.value='';questions.value=[];error.value='';notice.value='';storageNotice.value='';search.value='';ownerFilter.value='all';invalidate();try{const saved=JSON.parse(localStorage.getItem(storageKey.value)||'null');if(saved){raw.value=String(saved.raw||'').slice(0,20000);question.value=String(saved.question||'').slice(0,300);answer.value=String(saved.answer||'').slice(0,2000);inputMode.value=saved.inputMode==='batch'||!saved.inputMode&&!!raw.value?'batch':'single';preview.value=Array.isArray(saved.preview)?saved.preview.slice(0,100):[];defaultOwners.value=Array.isArray(saved.owners)?saved.owners:['plan'];pending.value=saved.pending||null;pendingRaw.value=String(saved.pendingRaw||'')}}catch{storageNotice.value='未保存内容恢复失败，请重新填写。'}void load()},{immediate:true})
watch([raw,question,answer,inputMode,preview,defaultOwners,pending],persist,{deep:true})
onBeforeUnmount(()=>epoch++)
</script>

<template>
<section class="qa-page">
 <header><div><h2>问答对</h2><p>保存认可的问题和回答，用于理解相似问法并组织回答。</p></div><span>已保存 {{page.items.length}} 条</span><button :disabled="busy" @click="load">刷新</button></header>
 <p v-if="error" class="qa-error" role="alert">{{error}}</p><p v-if="notice" class="qa-notice" role="status">{{notice}}</p><p v-if="storageNotice" class="qa-warning">{{storageNotice}}</p>
 <p v-if="pending" class="qa-warning">上次保存结果待确认。点击“重试上次保存”会沿用原请求，不重复发起向量。<button :disabled="busy" @click="restart">重新发起保存…</button></p>
 <section class="qa-panel">
  <details class="qa-targets"><summary>适用范围：{{ownerLabel(selectedTargets(defaultOwners))}}</summary><div class="qa-options"><label v-for="o in options" :key="o.key"><input type="checkbox" :checked="defaultOwners.includes(o.key)" :disabled="busy||!!pending" @change="defaultOwners=toggleOwner(defaultOwners,o.key,($event.target as HTMLInputElement).checked)"/>{{o.label}}</label><p>全局适用于全部链接；不选归属则保存为待归属，不用于回答。</p></div></details>
  <template v-if="inputMode==='single'">
   <label for="qa-question">问题<input id="qa-question" v-model="question" type="text" :disabled="busy||!!pending" maxlength="300" placeholder="观众会怎样问？" @input="preview=[];invalidate()"/></label>
   <label for="qa-answer">回答<input id="qa-answer" v-model="answer" type="text" :disabled="busy||!!pending" maxlength="2000" placeholder="填写认可的标准回答" @input="preview=[];invalidate()"/></label>
  </template>
  <label v-else for="qa-text">批量粘贴或大白话描述<textarea id="qa-text" v-model="raw" :disabled="busy||!!pending" maxlength="20000" rows="7" placeholder="问题：观众会怎样问？&#10;回答：填写认可的标准回答。&#10;&#10;也可以粘贴大白话描述，再点击整理。" @input="preview=[];invalidate()"/></label>
  <div class="qa-actions"><button class="primary" :disabled="busy||!loaded||(!(inputMode==='single'?question.trim()||answer.trim():raw.trim())&&!preview.length&&!pending)" @click="save">{{busy?'处理中…':pending?'重试上次保存':'保存'}}</button><button v-if="inputMode==='batch'" :disabled="busy||!loaded||!!pending||!raw.trim()" @click="organize">将大白话整理为问答</button><button v-if="inputMode==='batch'" :disabled="busy||!!pending" @click="returnToSingleInput">返回逐条填写</button></div>
 </section>
 <section v-for="g in duplicates" :key="g.key" class="qa-panel qa-duplicate"><h3>重复问题 · 选择保留一条</h3><label v-for="e in g.rows" :key="e.client_id" class="qa-choice"><input v-model="duplicateSelection[g.key]" type="radio" :name="g.key" :value="e.client_id"/><span><b>{{e.question}}</b><br/>{{e.answer}}</span></label><button @click="keepDuplicate(g.key)">保留</button></section>
 <section v-if="preview.length" class="qa-panel qa-preview"><h3>核对问答 {{preview.length}}</h3><p v-for="(q,i) in questions" :key="i" class="qa-warning">待补充：{{q}}</p>
  <article v-for="(e,i) in preview" :key="e.client_id" class="qa-entry">
   <label>问题 {{i+1}}<input v-model="e.question" :disabled="busy||!!pending" maxlength="300" @input="invalidate"/></label><label>标准回答<textarea v-model="e.answer" :disabled="busy||!!pending" maxlength="2000" rows="3" @input="invalidate"/></label>
   <details class="qa-targets"><summary>归属：{{ownerLabel(e.object_targets)}}</summary><div class="qa-options"><label v-for="o in options" :key="o.key"><input type="checkbox" :checked="e.object_targets.some(t=>targetKey(t)===o.key)" :disabled="busy||!!pending" @change="setEntryOwner(e,o.key,($event.target as HTMLInputElement).checked)"/>{{o.label}}</label></div></details>
   <label class="qa-check"><input v-model="e.enabled" type="checkbox" :disabled="busy||!!pending||!e.object_targets.length" @change="invalidate"/>启用此问答（未确认归属不会供答）</label>
   <details><summary>相似问法、条件和有效期</summary><div class="qa-advanced"><label>相似问法（每行一条）<textarea :value="e.aliases.join('\n')" :disabled="busy||!!pending" rows="2" @change="e.aliases=($event.target as HTMLTextAreaElement).value.split('\n').map(x=>x.trim()).filter(Boolean);invalidate()"/></label><label>适用条件<textarea v-model="e.conditions" :disabled="busy||!!pending" maxlength="1000" rows="2" @input="invalidate"/></label><label>例外<textarea v-model="e.exceptions" :disabled="busy||!!pending" maxlength="1000" rows="2" @input="invalidate"/></label><label>开始生效<input type="datetime-local" :value="dateValue(e.valid_from)" :disabled="busy||!!pending" @change="setDate(e,'valid_from',($event.target as HTMLInputElement).value)"/></label><label>到期时间<input type="datetime-local" :value="dateValue(e.valid_until)" :disabled="busy||!!pending" @change="setDate(e,'valid_until',($event.target as HTMLInputElement).value)"/></label></div></details>
   <div v-for="p in proposals.filter(p=>p.item.client_id===e.client_id&&p.conflicts.length)" :key="p.item.client_id" class="qa-conflict"><p>{{p.identical?'库中已有相同记录，无需重复入库。':'库中已有回答，请选择本次保存的处理方式。'}}</p><article v-for="old in p.conflicts" :key="old.id"><b>库中 · {{ownerLabel(old.object_targets)}}</b><p>{{old.question}}</p><p>{{old.answer}}</p><p v-if="old.conditions||old.exceptions">条件：{{old.conditions||'无'}}；例外：{{old.exceptions||'无'}}</p></article><div v-if="!p.identical" class="qa-actions"><label class="qa-choice"><input v-model="choices[e.client_id!]" type="radio" :name="e.client_id" value="replace"/>使用本次回答</label><label class="qa-choice"><input v-model="choices[e.client_id!]" type="radio" :name="e.client_id" value="keep"/>保留库中回答</label></div></div>
   <button :disabled="busy||!!pending" class="danger" @click="preview.splice(i,1);invalidate()">移除此条预览</button>
  </article><button class="primary" :disabled="busy||!!duplicates.length||!!pending" @click="save">核对后保存</button>
 </section>
 <section class="qa-panel"><header><h3>已保存问答</h3><span>{{visible.length}} 条</span></header><div class="qa-filters"><input v-model="search" aria-label="搜索问答" placeholder="搜索问题、答案、归属"/><select v-model="ownerFilter" aria-label="按对象查看问答"><option value="all">全部对象</option><option v-for="o in options" :key="o.key" :value="o.key">{{o.label}}</option><option value="pending">待归属</option></select></div>
  <p v-if="!loaded" class="qa-muted">{{error?'读取失败，请刷新后操作。':'读取中…'}}</p><p v-else-if="!visible.length" class="qa-muted">暂无符合条件的问答。</p>
  <article v-for="e in visible" :key="e.id" class="qa-entry"><div class="qa-entry-header"><span>{{ownerLabel(e.object_targets)}}</span><span class="qa-status">{{entryStatus(e)}}</span></div><h3>问：{{e.question}}</h3><p class="qa-answer">答：{{e.answer}}</p><details v-if="e.aliases.length||e.conditions||e.exceptions||e.original||e.valid_from||e.valid_until"><summary>相似问法、条件与原文</summary><p v-if="e.aliases.length">相似问法：{{e.aliases.join('；')}}</p><p v-if="e.conditions">条件：{{e.conditions}}</p><p v-if="e.exceptions">例外：{{e.exceptions}}</p><p v-if="e.valid_from||e.valid_until">有效期：{{e.valid_from||'即刻'}} 至 {{e.valid_until||'长期'}}</p><p v-if="e.original" class="qa-muted">原文：{{e.original}}</p></details><div class="qa-actions"><button :disabled="busy||!!pending" @click="edit(e)">编辑</button><button :disabled="busy||!!pending" @click="toggle(e)">{{e.enabled?'停用':'启用'}}</button><button :disabled="busy" @click="emit('test',e.question)">去测试</button><button class="danger" :disabled="busy||!!pending" @click="remove(e)">删除</button></div></article>
 </section>
</section>
</template>

<style scoped>
.qa-page{display:grid;gap:18px;font-size:18px;color:#293750;min-width:0}.qa-page *{box-sizing:border-box}.qa-page h2,.qa-page h3,.qa-page p{margin:0;font-size:18px}.qa-page p{line-height:1.7;white-space:pre-wrap;overflow-wrap:anywhere}.qa-page header{display:flex;gap:14px;align-items:center;flex-wrap:wrap}.qa-page header>div{flex:1;min-width:0}.qa-page header p{color:#7b8796;margin-top:8px}.qa-panel{padding:22px;border:1px solid #e4e8f2;border-radius:18px;background:#fff;display:grid;gap:16px;min-width:0}.qa-page button,.qa-page input,.qa-page select,.qa-page textarea{font:inherit;font-size:18px;color:inherit}.qa-page button{width:auto;padding:10px 16px;min-height:44px;border:1px solid #e1e5f2;border-radius:11px;background:#fff;cursor:pointer}.qa-page button.primary{background:#6d4aff;border-color:#6d4aff;color:#fff}.qa-page button.danger{color:#c63e3e}.qa-page button:disabled{opacity:.5;cursor:default}.qa-page label{display:grid;gap:8px;min-width:0}.qa-page input:not([type=checkbox]):not([type=radio]),.qa-page textarea,.qa-page select{width:100%;min-width:0;border:1px solid #e1e5f2;border-radius:11px;background:#fbfcff;padding:12px;line-height:1.6}.qa-page textarea{resize:vertical}.qa-actions{display:flex;flex-wrap:wrap;gap:10px;align-items:center}.qa-muted{color:#7b8796}.qa-error{color:#b92323}.qa-notice{color:#278052}.qa-warning{color:#8c641f}.qa-entry{display:grid;gap:12px;border:1px solid #e5e8f1;border-radius:13px;padding:18px;min-width:0}.qa-entry-header{display:flex;gap:10px;justify-content:space-between;flex-wrap:wrap;color:#7355df}.qa-status{color:#718095}.qa-answer{line-height:1.8}.qa-filters{display:grid;grid-template-columns:minmax(0,1fr) minmax(180px,280px);gap:12px}.qa-targets summary,.qa-page summary{cursor:pointer;line-height:1.6;overflow-wrap:anywhere}.qa-options,.qa-advanced{display:grid;gap:12px;padding:14px;margin-top:10px;border:1px solid #e5e8f1;border-radius:10px}.qa-options{max-height:320px;overflow:auto}.qa-options label,.qa-page .qa-check,.qa-page .qa-choice{display:flex;align-items:flex-start;gap:10px;line-height:1.6}.qa-page input[type=checkbox],.qa-page input[type=radio]{width:22px;height:22px;min-width:22px;flex:0 0 22px;margin:3px 0 0;accent-color:#7355ff}.qa-page input[type=radio]{appearance:none;border:1px solid #9ca4b5;border-radius:4px;background:#fff;display:grid;place-content:center}.qa-page input[type=radio]:checked{border-color:#7355ff;background:#7355ff}.qa-page input[type=radio]:checked:before{content:'✓';font-size:17px;color:white}.qa-conflict,.qa-duplicate{padding:16px;background:#fff8f7;border:1px solid #f4cccc;border-radius:12px;display:grid;gap:12px}.qa-conflict article{background:#fff;padding:14px;border-radius:10px}.qa-page :is(button,input,textarea,select,summary):focus-visible{outline:3px solid #c2b5ff;outline-offset:2px}@media(max-width:650px){.qa-panel{padding:16px}.qa-filters{grid-template-columns:minmax(0,1fr)}.qa-entry{padding:14px}.qa-actions button{max-width:100%;white-space:normal}}
</style>
