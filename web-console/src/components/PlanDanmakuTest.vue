<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { session } from '../session'
import { analyzePlanDanmaku, recallPlanDanmaku, type DanmakuRecall, type DanmakuTestResult } from '../api'
const props=defineProps<{planId:number}>()
type History={text:string;feedback?:'good'|'bad'}
const question=ref(''),roomIntro=ref(''),query=ref(''),history=ref<History[]>([]),result=ref<DanmakuTestResult|null>(null),manual=ref<DanmakuRecall|null>(null),error=ref(''),busy=ref(''),storageNotice=ref('')
const actor=computed(()=>`${session.bootstrap?.actor?.tenant_id||0}:${session.bootstrap?.actor?.user_id||0}`)
const key=computed(()=>`danmaku-test:v1:${actor.value}:${props.planId}`)
const feedback=computed(()=>history.value.find(h=>h.text===result.value?.question)?.feedback)
let epoch=0
function persist(){try{localStorage.setItem(key.value,JSON.stringify({question:question.value,roomIntro:roomIntro.value,history:history.value}))}catch{storageNotice.value='浏览器未能保存测试历史。'}}
function mark(value:'good'|'bad'){if(!result.value)return;const h=history.value.find(h=>h.text===result.value!.question);if(h)h.feedback=value;persist()}
function clearHistory(){history.value=[];persist()}
async function analyze(){if(busy.value||!question.value.trim())return;const token=epoch,text=question.value.trim();busy.value='analyze';error.value='';try{const out=await analyzePlanDanmaku(props.planId,text,roomIntro.value);if(token!==epoch)return;result.value=out;manual.value=null;history.value=[{text},...history.value.filter(h=>h.text!==text)].slice(0,40);persist()}catch(e){if(token===epoch)error.value=(e as Error).message||'测试失败，请重试'}finally{if(token===epoch)busy.value=''}}
async function recall(){const queries=query.value.split('\n').map(q=>q.trim()).filter(Boolean);if(busy.value||!queries.length)return;if(queries.length>10){error.value='请一次查询不超过10条。';return};const token=epoch;busy.value='recall';error.value='';try{const out=await recallPlanDanmaku(props.planId,queries);if(token===epoch)manual.value=out}catch(e){if(token===epoch)error.value=(e as Error).message||'召回失败，请重试'}finally{if(token===epoch)busy.value=''}}
watch([()=>props.planId,actor],()=>{epoch++;busy.value='';result.value=null;manual.value=null;query.value='';error.value='';storageNotice.value='';question.value='';roomIntro.value='';history.value=[];try{const saved=JSON.parse(localStorage.getItem(key.value)||'null');if(saved){question.value=String(saved.question||'').slice(0,1000);roomIntro.value=String(saved.roomIntro||'').slice(0,2000);history.value=Array.isArray(saved.history)?saved.history.filter((h:History)=>typeof h?.text==='string').slice(0,40):[]}}catch{storageNotice.value='测试历史恢复失败，可重新输入。'}},{immediate:true})
watch([question,roomIntro],persist)
onBeforeUnmount(()=>epoch++)
const conditionLabels:Record<string,string>={region:'地区',time:'时间',amount:'金额',quantity:'数量',channel:'渠道',audience:'人群'}
</script>

<template>
<section class="danmaku-test">
 <section class="dt-panel dt-input">
  <header><h2>观众弹幕</h2><p>意图分析 → 语义召回 → 互动回答</p></header>
  <div class="dt-input-grid"><label>输入弹幕<textarea v-model="question" :disabled="!!busy" maxlength="1000" placeholder="例如：1号多少钱，新疆发吗？切小点" rows="3"/></label><label>直播间介绍（可选）<textarea v-model="roomIntro" :disabled="!!busy" maxlength="2000" placeholder="填写业务背景，帮助理解当前语境" rows="3"/></label><button class="dt-primary" :disabled="!!busy||!question.trim()" @click="analyze">{{busy==='analyze'?'分析中…':'分析意图'}}</button></div>
  <div v-if="history.length" class="dt-history"><span>历史</span><button v-for="h in history" :key="h.text" :disabled="!!busy" :class="{good:h.feedback==='good',bad:h.feedback==='bad'}" @click="question=h.text">{{h.text}}</button><button class="dt-link" :disabled="!!busy" @click="clearHistory">清空历史</button></div>
  <p class="dt-muted">使用本方案已入库的资料及已保存的语境词。点击后调用模型；测试回答只在此展示。</p>
  <p v-if="error" role="alert" class="dt-error">{{error}}<span v-if="result"> 下方仍为上次成功的测试结果。</span></p><p v-if="storageNotice" class="dt-muted">{{storageNotice}}</p>
 </section>
 <div class="dt-results">
  <section class="dt-panel dt-intent"><h2>意图结果</h2>
   <template v-if="result"><p class="dt-result-question">本次弹幕：{{result.question}}</p><p class="dt-meaning">{{result.intent.meaning}}</p>
    <p v-if="result.intent.corrected&&result.intent.corrected!==result.question" class="dt-muted">理解后的表述：{{result.intent.corrected}}</p>
    <ol v-if="result.intent.asks.length" class="dt-asks"><li v-for="(a,i) in result.intent.asks" :key="i"><b>{{a.text}}</b><p>{{a.subjects?.length?a.subjects.join('、'):'对象未指定'}}<span v-if="a.attribute"> · {{a.attribute}}</span></p><p v-if="a.conditions&&Object.keys(a.conditions).length">{{Object.entries(a.conditions).map(([k,v])=>`${conditionLabels[k]||k}：${v}`).join('；')}}</p></li></ol>
    <p v-for="(u,i) in result.intent.uncertainties" :key="i" class="dt-warning">待核对：{{u}}</p>
    <details v-if="result.context_words?.hits.length" class="dt-words" open><summary>命中的语境词 {{result.context_words.hits.length}}</summary><article v-for="(w,i) in result.context_words.hits" :key="i"><b>{{w.matched.join('、')}}</b>：{{w.meaning}}<p v-if="w.when">适用条件：{{w.when}}</p></article><p v-if="result.context_words.omitted">另有{{result.context_words.omitted}}条命中未加入本次上下文。</p></details>
    <h3>互动回答 <span v-if="result.action!=='answer'" class="dt-tag">{{result.action==='clarify'?'需要澄清':'资料不足'}}</span></h3><div class="dt-answer">{{result.answer}}</div><div class="dt-feedback"><span>回答质量：</span><button :class="{good:feedback==='good'}" @click="mark('good')">可以</button><button :class="{bad:feedback==='bad'}" @click="mark('bad')">有问题</button><span class="dt-muted">仅记录本地测试评价</span></div>
   </template><p v-else class="dt-empty">输入一条弹幕后，查看模型如何理解并回答。</p>
  </section>
  <section class="dt-panel dt-recall"><h2>语义召回 <span class="dt-tag">{{result?.recall.hits.length||0}}</span></h2>
   <div class="dt-manual"><label>核对召回<textarea v-model="query" :disabled="!!busy" rows="2" placeholder="一个或多个查询，每行一条，查看本方案资料"/></label><button :disabled="!!busy||!query.trim()" @click="recall">{{busy==='recall'?'召回中…':'召回'}}</button></div>
   <p v-if="manual" class="dt-warning">下方为手动核对结果，未重新生成左侧回答。<button class="dt-link" @click="manual=null">返回回答依据</button></p>
   <template v-if="manual||result"><p v-for="(n,i) in (manual||result!.recall).notices" :key="i" class="dt-warning">{{n}}</p>
    <article v-for="(g,i) in (manual||result!.recall).groups" :key="i" class="dt-recall-group"><h3>{{i+1}}. {{g.ask.text}}</h3><p v-if="g.notice" class="dt-warning">{{g.notice}}</p><p v-if="!g.hits.length" class="dt-muted">未召回可用的相关资料。</p>
     <article v-for="h in g.hits" :key="h.id" class="dt-evidence" :class="{excluded:h.condition_status==='不适用'}"><div class="dt-tags"><span class="dt-tag">{{(h.score*100).toFixed(1)}}%</span><span class="dt-tag">{{h.scope}}</span><span v-if="h.attribute" class="dt-tag">{{h.attribute}}</span><span>{{h.condition_status}}</span></div><p v-if="h.question">问题：{{h.question}}</p><p class="dt-fact">{{h.content}}</p><p v-if="h.conditions">条件：{{h.conditions}}</p><p v-if="h.exceptions">例外：{{h.exceptions}}</p><p v-if="h.original" class="dt-muted">原文：{{h.original}}</p></article>
    </article>
   </template><p v-else class="dt-empty">分析后按每个诉求展示相关资料，也可单独核对召回。</p>
  </section>
 </div>
</section>
</template>

<style scoped>
.danmaku-test{display:grid;gap:20px;min-width:0;color:#27354e;font-size:18px}.danmaku-test *{box-sizing:border-box}.danmaku-test h2,.danmaku-test h3,.danmaku-test p{margin:0}.danmaku-test h2{font-size:22px}.danmaku-test h3{font-size:18px}.danmaku-test p{line-height:1.7;white-space:pre-wrap;overflow-wrap:anywhere}.danmaku-test button,.danmaku-test textarea{font:inherit;font-size:18px}.danmaku-test button{width:auto;min-height:44px;padding:10px 16px;border:1px solid #e1e5f2;border-radius:12px;background:#fff;color:#33415c;cursor:pointer}.danmaku-test button:disabled{opacity:.55;cursor:default}.danmaku-test button.dt-primary{background:#6d4aff;color:#fff;border-color:#6d4aff}.dt-panel{padding:24px;border:1px solid #e4e8f2;border-radius:18px;background:#fff;min-width:0}.dt-panel header{display:flex;justify-content:space-between;align-items:center;gap:12px;flex-wrap:wrap;margin-bottom:16px}.dt-input-grid{display:grid;grid-template-columns:minmax(0,2fr) minmax(0,1fr) auto;gap:14px;align-items:end}.danmaku-test label{display:grid;gap:8px;min-width:0;font-size:18px}.danmaku-test textarea{width:100%;min-width:0;padding:14px;border:1px solid #e1e5f2;border-radius:12px;background:#fbfcff;color:#27354e;resize:vertical;line-height:1.6}.danmaku-test button:focus-visible,.danmaku-test textarea:focus-visible{outline:3px solid #c2b5ff;outline-offset:2px}.dt-history{display:flex;flex-wrap:wrap;gap:8px;align-items:center;margin:18px 0}.dt-history button{border-radius:24px;padding:6px 12px;max-width:100%;overflow-wrap:anywhere;text-align:left}.dt-muted,.dt-panel header p,.dt-empty{color:#7b8796}.dt-input>.dt-muted{margin-top:14px}.dt-error{color:#b92323;margin-top:12px!important}.dt-warning{color:#8c641f}.dt-results{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:20px;align-items:start}.dt-intent,.dt-recall{display:grid;gap:16px}.dt-result-question{color:#778196}.dt-meaning{color:#4d5c72}.dt-asks{padding-left:25px;margin:0;display:grid;gap:10px}.dt-asks li p{color:#778196}.dt-answer{padding:18px;background:#fafbff;border:1px solid #e4e8f2;border-radius:12px;line-height:1.8;white-space:pre-wrap;overflow-wrap:anywhere}.dt-feedback,.dt-tags{display:flex;align-items:center;gap:8px;flex-wrap:wrap}.dt-tag{display:inline-block;padding:3px 10px;border-radius:20px;background:#f0ecff;color:#7355df;font-size:18px;font-weight:400}.danmaku-test .good{background:#eef8f0;color:#27834d;border-color:#b9dec2}.danmaku-test .bad{background:#fff1f1;color:#c63333;border-color:#ffc7c7}.dt-manual{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:12px;align-items:end}.dt-recall-group{display:grid;gap:12px}.dt-evidence{border:1px solid #e6e9f2;border-radius:14px;padding:16px;display:grid;gap:8px}.dt-evidence.excluded{background:#fafafa}.dt-fact{font-weight:600}.dt-words{padding:14px;background:#faf8ff;border-radius:12px}.dt-words article{margin-top:10px;line-height:1.7}.dt-words summary{cursor:pointer}.dt-empty{padding:20px 0}.danmaku-test .dt-link{background:transparent;border:0;color:#7b8796;padding:6px}.dt-feedback .dt-muted{font-size:18px}@media(max-width:1100px){.dt-input-grid{grid-template-columns:minmax(0,1fr) minmax(0,1fr)}.dt-input-grid>button{justify-self:start}.dt-results{grid-template-columns:minmax(0,1fr)}}@media(max-width:600px){.dt-panel{padding:16px}.dt-input-grid{grid-template-columns:minmax(0,1fr)}.dt-manual{grid-template-columns:minmax(0,1fr)}.dt-manual>button{justify-self:start}.dt-tags{gap:6px}.dt-feedback{gap:8px}}
</style>
