import assert from 'node:assert/strict'
import {createServer} from 'vite'
import vue from '@vitejs/plugin-vue'
import {createRequire} from 'node:module'
import {existsSync} from 'node:fs'
import {fileURLToPath} from 'node:url'
const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url)),{chromium}=require('playwright-core')
const root=fileURLToPath(new URL('..',import.meta.url))
const server=await createServer({root,configFile:false,logLevel:'error',plugins:[vue(),{name:'commerce-fixture',configureServer(s){s.middlewares.use(async(req,res,next)=>{
 if(req.url!='/__commerce.html')return next()
 res.setHeader('Content-Type','text/html;charset=utf-8');res.end(await s.transformIndexHtml(req.url,`<!doctype html><html><meta charset="utf-8"><div id="app-global-breadcrumbs"></div><div id="app-global-switches"></div><div id="app"></div><script type="module">
 import {createApp,h} from 'vue';import {createRouter,createMemoryHistory} from 'vue-router';import Rules from '/src/components/CommerceRulesPanel.vue';import Wallet from '/src/components/SalesCommissionWallet.vue';import Marketing from '/src/views/MarketingDesignView.vue';import {applySession} from '/src/session.ts';import {feedbackState,resolveConfirm} from '/src/uiFeedback.ts';import '/src/style.css';
 window.fixture={calls:[],rules:[],feedbackState,resolveConfirm};
 const campaign={id:1,code:'welcome',name:'开户一元时长',status:'inactive',sort_order:1,display_locations:['backoffice'],controls:{audience:'new_within_days',new_account_days:7,max_claims:1,max_units:3,benefit_key:'new-account-welcome',require_phone:true},items:[{target_type:'time_card',target_id:1,pricing_mode:'fixed',fixed_price_cents:100,quantity:3,package_months:1,discount_bps:10000}]};
 window.fetch=async(url,o={})=>{let input=o.body?JSON.parse(o.body):null;window.fixture.calls.push({url,method:o.method||'GET',input});
 if(url==='/api/v1/finance/commerce-rules')return Response.json({items:window.fixture.rules,targets:[{id:1,kind:'time_card',name:'测试时长卡'},{id:1,kind:'campaign',name:campaign.name}]});
 if(url.endsWith('/commerce-rules/drafts')){window.fixture.rules.push({...input,id:1,status:'draft'});return Response.json({id:1})}
 if(url.endsWith('/commerce-rules/1/publish')){window.fixture.rules[0].status='published';return Response.json({ok:true})}
 if(url.startsWith('/api/v1/sales/commission-wallet'))return Response.json({wallet:{available_balance_cents:100,frozen_balance_cents:200},ledger:[],withdrawals:[],earnings:[{id:1,order_no:'TEST-1',product_name:'测试时长卡',rule_name:'月度提成',rule_version_id:1,amount_cents:100,status:'available',created_at:'2026-10-01',available_at:'2026-11-15T00:00:00+08:00'}]});
 if(url==='/api/v1/sales/commission-withdrawals')return Response.json({id:1,status:'reviewing'});
 if(url.includes('/marketing-campaigns'))return Response.json({items:[campaign]});
 if(url.includes('/time-cards'))return Response.json({items:[{id:1,name:'测试时长卡',active_version:{price_cents:10000,duration_seconds:3600,participates_referral:true,participates_sales_commission:true}}]});
 return Response.json({items:[]})};
 applySession({actor:{user_id:900,role:'platform_admin'},tenants:[],environment:'test',staff_access:{is_super_admin:true,permissions:[],role_codes:[],group_ids:[],permission_scopes:{},permission_group_ids:{},managed_group_ids:[]}});
 const router=createRouter({history:createMemoryHistory(),routes:[{path:'/:pathMatch(.*)*',component:Rules}]});await router.push('/commercial/marketing');await router.isReady();
 createApp({render:()=>h('main',[h(Rules),h(Wallet,{period:'2026-10'}),h(Marketing)])}).use(router).mount('#app');
 </script></html>`))
})}}],server:{host:'127.0.0.1',port:18604,strictPort:true}})
await server.listen();const browser=await chromium.launch({headless:true,executablePath:[process.env.CHROME_PATH,'C:/Program Files/Google/Chrome/Application/chrome.exe'].find(x=>x&&existsSync(x))})
try{
const page=await browser.newPage({viewport:{width:1700,height:1100}}),errors=[];page.on('pageerror',e=>{errors.push(e.message);console.log('PAGE ERROR',e.message)});await page.goto('http://127.0.0.1:18604/__commerce.html');await page.getByText('TEST-1').waitFor();
await page.getByRole('button',{name:'＋ 新建计提规则'}).click();const modal=page.locator('.commerce-editor');await modal.getByText('规则名称',{exact:true}).locator('..').locator('input').fill('活动销售提成');await modal.getByText('作用范围',{exact:true}).locator('..').locator('select').selectOption('campaign');await modal.getByText('具体商品 / 活动',{exact:true}).locator('..').locator('select').selectOption('1');await modal.getByText('计提方式',{exact:true}).locator('..').locator('select').selectOption('percent');await modal.getByText('比例（%）',{exact:true}).locator('..').locator('input').fill('10');await modal.getByRole('button',{name:'保存草稿'}).click();await page.getByText('已保存草稿，发布前不会影响成交计提。').waitFor();
let calls=await page.evaluate(()=>window.fixture.calls),saved=calls.find(c=>c.url.endsWith('/drafts')).input;assert.equal(saved.rate_bps,1000);assert.equal(saved.scope_type,'campaign');assert.equal(saved.month_lag,1);assert.equal(saved.release_day,15);assert.equal(calls.filter(c=>c.url.endsWith('/publish')).length,0);
await page.getByRole('button',{name:'审核并发布'}).click();assert.equal(await page.evaluate(()=>window.fixture.feedbackState.confirmOpen),true);await page.evaluate(()=>window.fixture.resolveConfirm(true));await page.getByText('规则已发布，只影响后续新订单。').waitFor();
const wallet=page.locator('.commissions');await wallet.locator('input').fill('1');await wallet.getByRole('button',{name:'申请提现'}).click();await page.evaluate(()=>window.fixture.resolveConfirm(true));await page.waitForTimeout(100);calls=await page.evaluate(()=>window.fixture.calls);assert.equal(calls.find(c=>c.url==='/api/v1/sales/commission-withdrawals').input.amount_cents,100);
await page.getByText('开户一元时长',{exact:true}).first().click();await page.getByText('测试时长卡',{exact:true}).last().click();assert.ok((await page.locator('.marketing-plan-body').textContent()).includes('限领1次'));assert.ok((await page.locator('.marketing-plan-body').textContent()).includes('¥1.00'));assert.ok((await page.locator('.marketing-plan-body').textContent()).includes('销售提成'));
await page.screenshot({path:'../artifacts/commerce-ui.png',fullPage:true});assert.deepEqual(errors,[]);console.log('PASS finance draft/publication confirmation, 1-month/15th schedule, exact 1-yuan withdrawal, marketing eligibility/cents/participation rendering; all API writes mocked.')
}finally{await browser.close();await server.close()}
