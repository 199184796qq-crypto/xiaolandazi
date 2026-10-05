<script setup lang="ts">
import { computed,onMounted,reactive,ref } from 'vue'
import { getCommerceRules,saveCommerceDraft,publishCommerceRule,rewardText,money,commerceStatus } from '../commerce'
import type { CommerceRule,CommerceTarget } from '../commerce'
import { session } from '../session'
import { confirmAction } from '../uiFeedback'
const rules=ref<CommerceRule[]>([]),targets=ref<CommerceTarget[]>([]),error=ref(''),notice=ref(''),busy=ref(false),opened=ref(false),search=ref(''),channel=ref('all')
const canManage=computed(()=>session.bootstrap?.actor.role==='platform_admin'||session.bootstrap?.staff_access?.is_super_admin||session.bootstrap?.staff_access?.permissions.includes('finance.settlement_rules.manage'))
const defaults=():CommerceRule=>({name:'',channel:'sales',scope_type:'product',product_type:'time_card',target_id:0,mode:'off',amount_cents:0,rate_bps:0,minimum_paid_cents:0,cap_cents:0,pending_days:0,schedule_mode:'monthly',release_day:15,month_lag:1,window_end_day:0,tiers:[]})
const form=reactive(defaults())
const fixedYuan=ref(0),percent=ref(0),minimumYuan=ref(0),capYuan=ref(0),tierRows=ref<{yuan:number;percent:number}[]>([])
const visible=computed(()=>rules.value.filter(r=>(channel.value==='all'||r.channel===channel.value)&&(!search.value||[r.name,scopeLabel(r)].join(' ').includes(search.value))))
const options=computed(()=>targets.value.filter(t=>t.kind===(form.scope_type==='campaign'?'campaign':form.product_type)))
function scopeLabel(r:CommerceRule){if(r.scope_type==='default')return '所有商品（默认）';if(r.scope_type==='category')return {time_card:'全部时长卡',membership:'全部会员',device:'全部设备'}[r.product_type]||r.product_type;return targets.value.find(t=>t.id===r.target_id&&t.kind===(r.scope_type==='campaign'?'campaign':r.product_type))?.name||'#'+r.target_id}
function schedule(r:CommerceRule){return r.schedule_mode==='monthly'?'成交后第'+r.month_lag+'个月 '+r.release_day+'日解冻'+(r.pending_days?'，至少冻结'+r.pending_days+'天':''):'冻结'+r.pending_days+'天后可提现'}
function edit(r?:CommerceRule){Object.assign(form,defaults(),r||{});fixedYuan.value=form.amount_cents/100;percent.value=form.rate_bps/100;minimumYuan.value=form.minimum_paid_cents/100;capYuan.value=form.cap_cents/100;tierRows.value=form.tiers.map(t=>({yuan:t.minimum_paid_cents/100,percent:t.rate_bps/100}));opened.value=true;error.value=''}
async function load(){try{const d=await getCommerceRules();rules.value=d.items;targets.value=d.targets||[]}catch(e){error.value=e instanceof Error?e.message:'加载失败'}}
async function save(){if(busy.value)return;busy.value=true;error.value='';try{await saveCommerceDraft({...form,product_type:form.scope_type==='default'||form.scope_type==='campaign'?'':form.product_type,target_id:form.scope_type==='default'||form.scope_type==='category'?0:form.target_id,amount_cents:Math.round(fixedYuan.value*100),rate_bps:Math.round(percent.value*100),minimum_paid_cents:Math.round(minimumYuan.value*100),cap_cents:Math.round(capYuan.value*100),tiers:tierRows.value.map(t=>({minimum_paid_cents:Math.round(t.yuan*100),rate_bps:Math.round(t.percent*100)}))});opened.value=false;notice.value='已保存草稿，发布前不会影响成交计提。';await load()}catch(e){error.value=e instanceof Error?e.message:'保存失败'}finally{busy.value=false}}
async function publish(r:CommerceRule){if(!r.id||busy.value)return;if(!await confirmAction({title:'发布成交计提规则',message:r.name+'：'+scopeLabel(r)+'；'+rewardText(r)+'；'+schedule(r)+'。仅新订单生效，历史和待支付订单不变。确认发布？',confirmText:'确认发布'}))return;busy.value=true;try{await publishCommerceRule(r.id);notice.value='规则已发布，只影响后续新订单。';await load()}catch(e){error.value=e instanceof Error?e.message:'发布失败'}finally{busy.value=false}}
onMounted(load)
</script>
<template>
 <section class="settings-card commerce-rules">
  <header><div><h3>成交提成与用户分佣</h3><p>活动 ＞ 指定商品 ＞ 商品类别 ＞ 默认规则；更具体的“不参与”规则优先。免费订单不计提，单项奖励不超过实付。</p><p>财务已发布规则优先；没有新规则时沿用商品参与开关和已有计提。已有订单锁定下单时规则。每月15日提上月：账期延后1个月，开放日15日，解冻后可累计提现。</p></div><button v-if="canManage" class="primary-button" @click="edit()">＋ 新建计提规则</button></header>
  <div class="filters"><input v-model="search" placeholder="搜索规则 / 商品 / 活动" /><select v-model="channel"><option value="all">销售提成与用户分佣</option><option value="sales">销售提成</option><option value="referral">用户分佣</option></select><button class="ghost-button" @click="load">刷新</button></div>
  <p v-if="error" class="inline-error">{{error}}</p><p v-if="notice" class="inline-success">{{notice}}</p>
  <div class="data-table-wrap"><table class="data-table"><thead><tr><th>规则 / 版本</th><th>对象</th><th>范围</th><th>计提方式</th><th>开放提现</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="r in visible" :key="r.id"><td>{{r.name}}<small>#{{r.id}}</small></td><td>{{r.channel==='sales'?'销售提成':'用户分佣'}}</td><td>{{scopeLabel(r)}}</td><td>{{rewardText(r)}}<small>门槛{{money(r.minimum_paid_cents)}} · {{r.cap_cents?'封顶'+money(r.cap_cents):'无额外封顶'}}</small></td><td>{{schedule(r)}}</td><td>{{commerceStatus(r.status||'')}}</td><td><button v-if="canManage" class="ghost-button" @click="edit(r)">另存草稿</button><button v-if="canManage&&r.status==='draft'" class="primary-button" :disabled="busy" @click="publish(r)">审核并发布</button></td></tr></tbody></table></div><p v-if="!rules.length">尚未配置新计提规则；保留已有业务规则，不会自动启用比例。</p>
  <Teleport to="body"><div v-if="opened" class="modal-backdrop" @click.self="opened=false"><form class="modal-card commerce-editor" @submit.prevent="save"><div class="modal-header"><h3>计提规则草稿</h3><button type="button" class="close-button" @click="opened=false">×</button></div><p v-if="error" class="inline-error">{{error}}</p>
   <div class="grid">
    <label><span>规则名称</span><input v-model="form.name" maxlength="120" required /></label>
    <label><span>计提对象</span><select v-model="form.channel"><option value="sales">销售人员</option><option value="referral">邀请用户（直接推荐人）</option></select></label>
    <label><span>作用范围</span><select v-model="form.scope_type" @change="form.target_id=0"><option value="product">指定商品</option><option value="campaign">指定营销活动</option><option value="category">商品类别</option><option value="default">所有商品默认</option></select></label>
    <label v-if="form.scope_type==='product'||form.scope_type==='category'"><span>商品类型</span><select v-model="form.product_type" @change="form.target_id=0"><option value="time_card">时长卡</option><option value="membership">会员</option><option value="device">设备</option></select></label>
    <label v-if="form.scope_type==='product'||form.scope_type==='campaign'"><span>具体商品 / 活动</span><select v-model.number="form.target_id" required><option :value="0">请选择</option><option v-for="t in options" :key="t.id" :value="t.id">{{t.name}} #{{t.id}}</option></select></label>
    <label><span>计提方式</span><select v-model="form.mode"><option value="off">不参与 / 不计提</option><option value="fixed">固定金额（每个成交商品包）</option><option value="percent">实付比例</option><option value="tier">实付金额阶梯比例</option></select></label>
    <label v-if="form.mode==='fixed'"><span>提成金额（元）</span><input v-model.number="fixedYuan" type="number" min="0.01" max="100000000" step="0.01" required /></label>
    <label v-if="form.mode==='percent'"><span>比例（%）</span><input v-model.number="percent" type="number" min="0.01" max="100" step="0.01" required /></label>
    <label><span>最低实付（元）</span><input v-model.number="minimumYuan" type="number" min="0" step="0.01" /></label><label><span>单项封顶（元，0不限）</span><input v-model.number="capYuan" type="number" min="0" step="0.01" /></label>
    <label><span>至少冻结天数</span><input v-model.number="form.pending_days" type="number" min="0" max="365" required /></label><label><span>提现周期</span><select v-model="form.schedule_mode"><option value="monthly">每月指定日解冻</option><option value="immediate">冻结期结束后即可提现</option></select></label>
    <label v-if="form.schedule_mode==='monthly'"><span>账期延后（月）</span><input v-model.number="form.month_lag" type="number" min="1" max="12" required /></label><label v-if="form.schedule_mode==='monthly'"><span>每月开放日</span><input v-model.number="form.release_day" type="number" min="1" max="28" required /></label>
   </div>
   <div v-if="form.mode==='tier'"><p>按订单实付所属档位计提，门槛须递增，不累进。</p><div v-for="(t,i) in tierRows" :key="i" class="filters"><input v-model.number="t.yuan" type="number" min="0" step="0.01" placeholder="门槛（元）" required /><input v-model.number="t.percent" type="number" min="0.01" max="100" step="0.01" placeholder="比例%" required /><button type="button" @click="tierRows.splice(i,1)">移除</button></div><button type="button" class="ghost-button" @click="tierRows.push({yuan:0,percent:0})">添加档位</button></div>
   <p>退款按原规则比例冲回；已提现的退款形成应扣余额，后续收入先抵扣。这里保存为草稿，不立即改变现有活动。</p>
   <div class="modal-actions"><button class="ghost-button" type="button" @click="opened=false">取消</button><button class="primary-button" :disabled="busy">保存草稿</button></div>
  </form></div></Teleport>
 </section>
</template>
<style scoped>
.commerce-rules{padding:24px;margin:16px 0}.commerce-rules header{display:flex;gap:20px;justify-content:space-between;align-items:start}.commerce-rules p{color:#718097;line-height:1.7}.filters{display:flex;gap:12px;flex-wrap:wrap;margin:16px 0}.commerce-rules small{display:block;color:#8791a5;margin-top:6px}.commerce-editor{width:min(850px,95vw);max-height:90vh;overflow:auto;padding:24px}.grid{display:grid;grid-template-columns:1fr 1fr;gap:18px}.grid label{display:grid;gap:8px}input,select{padding:10px;border:1px solid #dde3f1;border-radius:10px;min-width:0}table{width:100%}td,th{padding:14px;text-align:left} @media(max-width:700px){.grid{grid-template-columns:1fr}.commerce-rules header{display:block}}
</style>
