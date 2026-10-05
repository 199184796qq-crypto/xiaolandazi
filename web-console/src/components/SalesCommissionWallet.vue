<script setup lang="ts">
import { onMounted,ref,watch } from 'vue'
import { getSalesCommissionWallet,createSalesCommissionWithdrawal,money,commerceStatus } from '../commerce'
import type { CommerceEarning } from '../commerce'
import type { BeneficiaryWalletDashboard } from '../types'
import { confirmAction } from '../uiFeedback'
const props=defineProps<{period:string}>()
const data=ref<BeneficiaryWalletDashboard & {earnings:CommerceEarning[]}|null>(null),error=ref(''),busy=ref(false),yuan=ref<number>(0)
async function load(){try{data.value=await getSalesCommissionWallet(props.period)}catch(e){error.value=e instanceof Error?e.message:'读取提成失败'}}
async function withdraw(){const cents=Math.round(yuan.value*100);if(!Number.isSafeInteger(cents)||cents<=0){error.value='请输入有效提现金额';return}if(!await confirmAction({title:'提交销售提成提现',message:'确认申请提现 '+money(cents)+'？仅从已解冻余额中冻结，财务审核并实际打款后到账。',confirmText:'提交申请'}))return;busy.value=true;error.value='';try{await createSalesCommissionWithdrawal(cents);yuan.value=0;await load()}catch(e){error.value=e instanceof Error?e.message:'提现失败'}finally{busy.value=false}}
onMounted(load);watch(()=>props.period,load)
</script>
<template>
 <section class="settings-card commissions"><header><div><h3>我的成交提成</h3><p>成交后自动计提；账期按下单时规则。月度收益筛选不影响累计钱包余额。退款会冲回，欠扣金额需先抵扣。</p></div><button class="ghost-button" @click="load">刷新</button></header><p v-if="error" class="inline-error">{{error}}</p>
 <template v-if="data"><div class="balances"><div><span>可提现余额</span><strong>{{money(data.wallet.available_balance_cents)}}</strong></div><div><span>未解冻 / 提现审核中</span><strong>{{money(data.wallet.frozen_balance_cents)}}</strong></div></div>
 <form class="withdraw" @submit.prevent="withdraw"><label>提现金额（元） <input v-model.number="yuan" type="number" min="0.01" step="0.01" :max="Math.max(0,data.wallet.available_balance_cents/100)" required /></label><button class="primary-button" :disabled="busy||data.wallet.available_balance_cents<=0">{{busy?'处理中':'申请提现'}}</button></form>
 <details open><summary>{{period}}成交计提明细（最近500条）</summary><div class="data-table-wrap"><table class="data-table"><thead><tr><th>订单 / 商品</th><th>规则 / 版本</th><th>提成金额</th><th>状态</th><th>可提现时间</th></tr></thead><tbody><tr v-for="e in data.earnings" :key="e.id"><td>{{e.order_no}}<small>{{e.product_name}}</small></td><td>{{e.rule_name}} #{{e.rule_version_id}}</td><td>{{money(e.amount_cents)}}</td><td>{{commerceStatus(e.status)}}</td><td>{{e.available_at?new Date(e.available_at).toLocaleString('zh-CN'):'—'}}</td></tr></tbody></table></div><p v-if="!data.earnings.length">本期暂无新规则产生的成交提成。</p></details>
 <details><summary>提现记录</summary><div class="data-table-wrap"><table class="data-table"><thead><tr><th>提现单号</th><th>金额</th><th>状态</th><th>申请时间</th><th>说明</th></tr></thead><tbody><tr v-for="w in data.withdrawals" :key="w.id"><td>{{w.withdrawal_no}}</td><td>{{money(w.amount_cents)}}</td><td>{{commerceStatus(w.status)}}</td><td>{{new Date(w.requested_at).toLocaleString('zh-CN')}}</td><td>{{w.reject_reason||'—'}}</td></tr></tbody></table></div></details>
 </template></section>
</template>
<style scoped>
.commissions{padding:24px;margin:18px 0}.commissions header{display:flex;justify-content:space-between;gap:20px}.commissions p,.commissions small{color:#718097;line-height:1.7}.balances{display:flex;gap:40px;background:#f5f7ff;padding:20px;border-radius:16px}.balances div{display:grid;gap:8px}.balances strong{font-size:27px;color:#595ddd}.withdraw{display:flex;gap:16px;align-items:center;margin:20px 0}.withdraw input{padding:10px;border:1px solid #dde3f1;border-radius:10px;width:160px}small{display:block}details{margin-top:20px}summary{cursor:pointer;font-weight:600}td,th{padding:12px}
</style>
