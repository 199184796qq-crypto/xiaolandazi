<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { session } from '../session'
import { analyzePlanDanmaku, recallPlanDanmaku, getPlanAnswerFlowSettings, savePlanAnswerFlowSettings, type PlanAnswerFlowSettings, type DanmakuRecall, type DanmakuTestResult } from '../api'
const props=defineProps<{planId:number;initialQuestion?:string;roomIntro?:string}>()
const emit=defineEmits<{consumeInitialQuestion:[]}>()
type History={text:string;feedback?:'good'|'bad'}
const question=ref(''),query=ref(''),history=ref<History[]>([]),result=ref<DanmakuTestResult|null>(null),manual=ref<DanmakuRecall|null>(null),error=ref(''),busy=ref(''),storageNotice=ref('')
const actor=computed(()=>`${session.bootstrap?.actor?.tenant_id||0}:${session.bootstrap?.actor?.user_id||0}`)
const key=computed(()=>`danmaku-test:v1:${actor.value}:${props.planId}`)
const feedback=computed(()=>history.value.find(h=>h.text===result.value?.question)?.feedback)
const intentJSON=computed(()=>result.value?.intent_model_json?.trim()||'')
let epoch=0
const flowSetting=ref<PlanAnswerFlowSettings|null>(null),flowBusy=ref(''),flowError=ref(''),flowNotice=ref('')
function checkedFlowSetting(value:PlanAnswerFlowSettings){
 if(typeof value?.enabled!=='boolean'||!Number.isSafeInteger(value.revision)||value.revision<0)throw new Error('未能确认服务器上的开关状态')
 return value
}
async function loadFlow(token=epoch,keepError=false){
 flowBusy.value='load';if(!keepError)flowError.value=''
 try{const value=checkedFlowSetting(await getPlanAnswerFlowSettings(props.planId));if(token===epoch)flowSetting.value=value}
 catch(e){if(token===epoch){flowSetting.value=null;if(!keepError)flowError.value=(e as Error).message||'读取开关失败，请重试'}}
 finally{if(token===epoch)flowBusy.value=''}
}
async function changeFlow(){
 if(flowBusy.value||!flowSetting.value)return
 const token=epoch,plan=props.planId,next={enabled:!flowSetting.value.enabled,revision:flowSetting.value.revision}
 flowBusy.value='save';flowError.value='';flowNotice.value=''
 try{const value=checkedFlowSetting(await savePlanAnswerFlowSettings(plan,next));if(token!==epoch)return;flowSetting.value=value;flowNotice.value=value.enabled?'已开启，仅本方案后续正式问答使用新流程。':'已关闭，本方案后续正式问答使用原流程。'}
 catch(e){if(token!==epoch)return;flowSetting.value=null;await loadFlow(token,true);if(token===epoch)flowError.value=((e as Error).message||'保存结果未确认')+(flowSetting.value?' 已刷新当前状态，请核对后重试。':' 当前状态未确认，请重新读取。')}
 finally{if(token===epoch)flowBusy.value=''}
}
function persist(){try{localStorage.setItem(key.value,JSON.stringify({question:question.value,history:history.value}))}catch{storageNotice.value='浏览器未能保存测试历史。'}}
function mark(value:'good'|'bad'){if(!result.value)return;const h=history.value.find(h=>h.text===result.value!.question);if(h)h.feedback=value;persist()}
function clearHistory(){history.value=[];persist()}
async function analyze(){if(busy.value||!question.value.trim())return;const token=epoch,text=question.value.trim(),intro=props.roomIntro||'';busy.value='analyze';error.value='';try{const out=await analyzePlanDanmaku(props.planId,text,intro);if(token!==epoch)return;result.value=out;manual.value=null;history.value=[{text},...history.value.filter(h=>h.text!==text)].slice(0,40);persist()}catch(e){if(token===epoch)error.value=(e as Error).message||'测试失败，请重试'}finally{if(token===epoch)busy.value=''}}
async function recall(){const text=query.value.trim();if(busy.value||!text)return;const token=epoch;busy.value='recall';error.value='';try{const out=await recallPlanDanmaku(props.planId,[text]);if(token===epoch)manual.value=out}catch(e){if(token===epoch)error.value=((e as Error).message||'查询失败，请重试').replace(/^语义召回[:：]/,'语义查询：')}finally{if(token===epoch)busy.value=''}}
function visibleQueryNotices(notices:string[]){return manual.value?notices.filter(n=>!n.startsWith('手动核对未分析对象归属')):notices}
watch([()=>props.planId,actor],()=>{epoch++;busy.value='';result.value=null;manual.value=null;query.value='';error.value='';storageNotice.value='';question.value='';history.value=[];flowSetting.value=null;flowBusy.value='';flowError.value='';flowNotice.value='';try{const saved=JSON.parse(localStorage.getItem(key.value)||'null');if(saved){question.value=String(saved.question||'').slice(0,1000);history.value=Array.isArray(saved.history)?saved.history.filter((h:History)=>typeof h?.text==='string').slice(0,40):[]}}catch{storageNotice.value='测试历史恢复失败，可重新输入。'}if(props.initialQuestion){question.value=props.initialQuestion.slice(0,1000);emit('consumeInitialQuestion')}void loadFlow(epoch)},{immediate:true})
watch(question,persist)
onBeforeUnmount(()=>epoch++)
const conditionLabels:Record<string,string>={region:'地区',time:'时间',amount:'金额',quantity:'数量',channel:'渠道',audience:'人群'}
</script>

<template>
<section class="danmaku-test">
 <section class="dt-panel dt-flow-control">
  <div class="dt-flow-row"><h3>正式互动使用新回答流程</h3><button type="button" role="switch" aria-label="正式互动使用新回答流程" :aria-checked="flowSetting?.enabled??false" :disabled="!!flowBusy||!flowSetting" :class="{'dt-flow-on':flowSetting?.enabled}" @click="changeFlow"><span class="dt-flow-track" aria-hidden="true"><span/></span>{{flowBusy==='save'?'保存中…':flowBusy==='load'?'读取中…':flowSetting?(flowSetting.enabled?'已开启':'已关闭'):'状态未确认'}}</button></div>
  <p v-if="flowNotice" class="dt-flow-notice" role="status">{{flowNotice}}</p>
  <div v-if="flowError" class="dt-flow-error"><p role="alert" class="dt-error">{{flowError}}</p><button type="button" :disabled="!!flowBusy" @click="loadFlow()">重新读取开关</button></div>
 </section>
 <section class="dt-panel dt-input">
  <header><h2>观众弹幕</h2></header>
  <div class="dt-input-grid"><input v-model="question" type="text" aria-label="输入弹幕" :disabled="!!busy" maxlength="1000" placeholder="例如：1号多少钱，新疆发吗？切小点" @keydown.enter="analyze"/><button class="dt-primary" :disabled="!!busy||!question.trim()" @click="analyze">{{busy==='analyze'?'分析中…':'分析意图'}}</button></div>
  <div v-if="history.length" class="dt-history"><span>历史</span><button v-for="h in history" :key="h.text" :disabled="!!busy" :class="{good:h.feedback==='good',bad:h.feedback==='bad'}" @click="question=h.text">{{h.text}}</button><button class="dt-link" :disabled="!!busy" @click="clearHistory">清空历史</button></div>
  <p v-if="error" role="alert" class="dt-error">{{error}}<span v-if="result"> 下方仍为上次成功的测试结果。</span></p><p v-if="storageNotice" class="dt-muted">{{storageNotice}}</p>
 </section>
 <div class="dt-results">
  <section class="dt-panel dt-intent"><h2>意图结果</h2>
   <template v-if="result"><p class="dt-meaning">{{result.intent.meaning}}</p><p class="dt-muted">类别：{{result.interaction_intent?.type||result.intent.category||result.intent.speech_act||'—'}}</p>
    <p v-if="result.intent.corrected&&result.intent.corrected!==result.question" class="dt-muted">理解后的表述：{{result.intent.corrected}}</p>
    <ol v-if="result.intent.asks.length" class="dt-asks"><li v-for="(a,i) in result.intent.asks" :key="i"><b>{{a.text}}</b><p>{{a.subjects?.length?a.subjects.join('、'):'对象未指定'}}<span v-if="a.attribute"> · {{a.attribute}}</span></p><p v-if="a.keywords?.length">关键词：{{a.keywords.join('、')}}</p><p v-if="a.conditions&&Object.keys(a.conditions).length">{{Object.entries(a.conditions).map(([k,v])=>`${conditionLabels[k]||k}：${v}`).join('；')}}</p></li></ol>
    <p v-for="(u,i) in result.intent.uncertainties" :key="i" class="dt-warning">待核对：{{u}}</p>
    <details v-if="result.context_words?.hits.length" class="dt-words" open><summary>命中的语境词 {{result.context_words.hits.length}}</summary><article v-for="(w,i) in result.context_words.hits" :key="i"><b>{{w.matched.join('、')}}</b>：{{w.meaning}}<p v-if="w.when">适用条件：{{w.when}}</p></article><p v-if="result.context_words.omitted">另有{{result.context_words.omitted}}条命中未加入本次上下文。</p></details>
    <details v-if="result.matched_qa?.some(g=>g.matches.length)" class="dt-words" open><summary>命中问答</summary><article v-for="(g,i) in result.matched_qa" :key="i"><template v-if="g.matches.length"><b>{{g.ask}}</b><article v-for="m in g.matches" :key="m.id" class="dt-evidence"><div class="dt-tags"><span class="dt-tag">{{m.scope}}</span><span>{{m.status}}</span></div><p>标准问题：{{m.question}}</p><p>标准回答：{{m.answer}}</p><p v-if="m.conditions">条件：{{m.conditions}}</p><p v-if="m.exceptions">例外：{{m.exceptions}}</p><p v-if="m.reason" class="dt-warning">{{m.reason}}</p></article></template></article></details>
    <h3>互动回答 <span v-if="result.action!=='answer'" class="dt-tag">{{result.action==='clarify'?'需要澄清':result.action==='skip'?'不单独口播':'资料不足'}}</span></h3><div class="dt-answer">{{result.action==='skip'?'本条不单独口播。':result.answer}}</div><div class="dt-feedback"><span>回答质量：</span><button :class="{good:feedback==='good'}" @click="mark('good')">可以</button><button :class="{bad:feedback==='bad'}" @click="mark('bad')">有问题</button><span class="dt-muted">仅记录本地测试评价</span></div>
    <details v-if="result.answer_model_output" class="dt-intent-json"><summary>互动输出（旧协议）</summary><pre class="dt-protocol-output" tabindex="0" aria-label="互动输出旧协议"><code>{{result.answer_model_output}}</code></pre></details>
    <section class="dt-intent-json"><h3>意图模型返回 JSON</h3><pre v-if="intentJSON" class="dt-json" tabindex="0" aria-label="意图模型返回 JSON"><code>{{intentJSON}}</code></pre><p v-else class="dt-muted">本次结果未包含模型原始 JSON，请重新分析后查看。</p></section>
   </template><p v-else class="dt-empty">输入一条弹幕后，查看模型如何理解并回答。</p>
  </section>
  <section class="dt-panel dt-recall"><h2>语义查询 <span class="dt-tag">{{manual?.hits.length??result?.recall.hits.length??0}}</span></h2>
   <div class="dt-manual"><input v-model="query" type="text" aria-label="语义查询" :disabled="!!busy" placeholder="输入想查询的内容" @keydown.enter="recall"/><div class="dt-manual-actions"><button :disabled="!!busy||!query.trim()" @click="recall">{{busy==='recall'?'查询中…':'查询'}}</button><button v-if="manual" class="dt-link" @click="manual=null">返回回答依据</button></div></div>
   <details v-if="manual?.matched_qa?.some(g=>g.matches.length)" class="dt-words" open><summary>问答核对</summary><article v-for="(g,i) in manual.matched_qa" :key="i"><template v-if="g.matches.length"><b>{{g.ask}}</b><article v-for="m in g.matches" :key="m.id" class="dt-evidence"><p>{{m.scope}} · {{m.status}}</p><p>标准问题：{{m.question}}</p><p>标准回答：{{m.answer}}</p><p v-if="m.conditions">条件：{{m.conditions}}</p><p v-if="m.exceptions">例外：{{m.exceptions}}</p><p v-if="m.reason" class="dt-warning">{{m.reason}}</p></article></template></article></details>
   <template v-if="manual||result"><p v-for="(n,i) in visibleQueryNotices((manual||result!.recall).notices)" :key="i" class="dt-warning">{{n}}</p>
    <article v-for="(g,i) in (manual||result!.recall).groups" :key="i" class="dt-recall-group"><p v-if="!g.hits.length" class="dt-muted">未召回可用的相关资料。</p>
     <article v-for="h in g.hits" :key="h.id" class="dt-evidence" :class="{excluded:h.condition_status==='不适用'}"><div class="dt-tags"><span class="dt-tag">{{(h.score*100).toFixed(1)}}%</span><span class="dt-tag">{{h.scope}}</span><span v-if="h.attribute" class="dt-tag">{{h.attribute}}</span><span>{{h.condition_status}}</span></div><p v-if="h.question">问题：{{h.question}}</p><p class="dt-fact">{{h.content}}</p><p v-if="h.conditions">条件：{{h.conditions}}</p><p v-if="h.exceptions">例外：{{h.exceptions}}</p><p v-if="h.original" class="dt-muted">原文：{{h.original}}</p></article>
    </article>
   </template><p v-else class="dt-empty">分析后按每个诉求展示相关资料，也可单独查询。</p>
  </section>
 </div>
</section>
</template>

<style scoped>
.dt-flow-control{display:grid;gap:12px}.dt-flow-row{display:flex;align-items:center;justify-content:space-between;gap:20px}.dt-flow-row button{display:flex;align-items:center;gap:10px;flex-shrink:0}.dt-flow-track{display:inline-flex;align-items:center;width:44px;height:26px;padding:3px;border-radius:20px;background:#c5cbd6}.dt-flow-track>span{width:20px;height:20px;border-radius:50%;background:#fff;transition:transform .15s}.dt-flow-on .dt-flow-track{background:#6d4aff}.dt-flow-on .dt-flow-track>span{transform:translateX(18px)}.danmaku-test .dt-flow-on{border-color:#c6b6ff;color:#6d4aff;background:#f7f4ff}.dt-flow-notice{color:#27834d}.dt-flow-error{display:grid;gap:10px;justify-items:start}@media(max-width:600px){.dt-flow-row{align-items:flex-start;flex-direction:column}}
.dt-intent-json{display:grid;gap:12px;min-width:0}.dt-json,.dt-protocol-output{margin:0;max-height:480px;overflow:auto;padding:18px;border-radius:12px;background:#111827;color:#e5e7eb;white-space:pre-wrap;overflow-wrap:anywhere;font:18px/1.6 ui-monospace,SFMono-Regular,Consolas,monospace}.dt-json code,.dt-protocol-output code{font:inherit;white-space:inherit}
.danmaku-test{display:grid;gap:20px;min-width:0;color:#27354e;font-size:18px}.danmaku-test *{box-sizing:border-box}.danmaku-test h2,.danmaku-test h3,.danmaku-test p{margin:0}.danmaku-test h2{font-size:22px}.danmaku-test h3{font-size:18px}.danmaku-test p{line-height:1.7;white-space:pre-wrap;overflow-wrap:anywhere}.danmaku-test button,.danmaku-test textarea{font:inherit;font-size:18px}.danmaku-test button{width:auto;min-height:44px;padding:10px 16px;border:1px solid #e1e5f2;border-radius:12px;background:#fff;color:#33415c;cursor:pointer}.danmaku-test button:disabled{opacity:.55;cursor:default}.danmaku-test button.dt-primary{background:#6d4aff;color:#fff;border-color:#6d4aff}.dt-panel{padding:24px;border:1px solid #e4e8f2;border-radius:18px;background:#fff;min-width:0}.dt-panel header{display:flex;justify-content:space-between;align-items:center;gap:12px;flex-wrap:wrap;margin-bottom:16px}.dt-input-grid{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:14px;align-items:center}.dt-input-grid input{width:100%;min-width:0;min-height:56px;padding:12px 16px;border:1px solid #e1e5f2;border-radius:12px;background:#fbfcff;color:#27354e;font-family:inherit;font-size:20px;line-height:1.4}.dt-input-grid input::placeholder{color:#8a95a7}.danmaku-test label{display:grid;gap:8px;min-width:0;font-size:18px}.danmaku-test textarea{width:100%;min-width:0;padding:14px;border:1px solid #e1e5f2;border-radius:12px;background:#fbfcff;color:#27354e;resize:vertical;line-height:1.6}.danmaku-test button:focus-visible,.danmaku-test textarea:focus-visible,.danmaku-test input:focus-visible{outline:3px solid #c2b5ff;outline-offset:2px}.dt-history{display:flex;flex-wrap:wrap;gap:8px;align-items:center;margin:18px 0}.dt-history button{border-radius:24px;padding:6px 12px;max-width:100%;overflow-wrap:anywhere;text-align:left}.dt-muted,.dt-panel header p,.dt-empty{color:#7b8796}.dt-input>.dt-muted{margin-top:14px}.dt-error{color:#b92323;margin-top:12px!important}.dt-warning{color:#8c641f}.dt-results{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:20px;align-items:start}.dt-intent,.dt-recall{display:grid;gap:16px}.dt-meaning{color:#4d5c72}.dt-asks{padding-left:25px;margin:0;display:grid;gap:10px}.dt-asks li p{color:#778196}.dt-answer{padding:18px;background:#fafbff;border:1px solid #e4e8f2;border-radius:12px;line-height:1.8;white-space:pre-wrap;overflow-wrap:anywhere}.dt-feedback,.dt-tags{display:flex;align-items:center;gap:8px;flex-wrap:wrap}.dt-tag{display:inline-block;padding:3px 10px;border-radius:20px;background:#f0ecff;color:#7355df;font-size:18px;font-weight:400}.danmaku-test .good{background:#eef8f0;color:#27834d;border-color:#b9dec2}.danmaku-test .bad{background:#fff1f1;color:#c63333;border-color:#ffc7c7}.dt-manual{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:12px;align-items:center}.dt-manual input{width:100%;min-width:0;min-height:56px;padding:12px 16px;border:1px solid #e1e5f2;border-radius:12px;background:#fbfcff;color:#27354e;font-family:inherit;font-size:18px;line-height:1.4}.dt-manual-actions{display:flex;align-items:center;gap:8px;flex-wrap:wrap}.dt-recall-group{display:grid;gap:12px}.dt-evidence{border:1px solid #e6e9f2;border-radius:14px;padding:16px;display:grid;gap:8px}.dt-evidence.excluded{background:#fafafa}.dt-fact{font-weight:600}.dt-words{padding:14px;background:#faf8ff;border-radius:12px}.dt-words article{margin-top:10px;line-height:1.7}.dt-words summary{cursor:pointer}.dt-empty{padding:20px 0}.danmaku-test .dt-link{background:transparent;border:0;color:#7b8796;padding:6px}.dt-feedback .dt-muted{font-size:18px}@media(max-width:1100px){.dt-results{grid-template-columns:minmax(0,1fr)}}@media(max-width:600px){.dt-panel{padding:16px}.dt-input-grid{grid-template-columns:minmax(0,1fr)}.dt-input-grid>button{justify-self:start}.dt-manual{grid-template-columns:minmax(0,1fr)}.dt-manual>button{justify-self:start}.dt-tags{gap:6px}.dt-feedback{gap:8px}}
</style>
