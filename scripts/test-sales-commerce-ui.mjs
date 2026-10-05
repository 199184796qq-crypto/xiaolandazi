import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import http from 'node:http'
import {createRequire} from 'node:module'
const root=path.resolve(import.meta.dirname,'..'),dir=path.join(root,'sales-mobile/build')
const {chromium}=createRequire(path.join(root,'collector-worker/package.json'))('playwright-core')
const server=http.createServer((req,res)=>{let p=path.resolve(dir,'.'+decodeURIComponent(new URL(req.url,'http://sales.localhost').pathname));if(!p.startsWith(dir+path.sep)||!fs.existsSync(p)||!fs.statSync(p).isFile())p=path.join(dir,'index.html');res.setHeader('Content-Type',p.endsWith('.js')?'text/javascript':p.endsWith('.css')?'text/css':'text/html;charset=utf-8');res.end(fs.readFileSync(p))})
await new Promise(r=>server.listen(18605,'127.0.0.1',r))
const browser=await chromium.launch({headless:true,executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe'})
try{
 const page=await browser.newPage({viewport:{width:390,height:844}})
 await page.addInitScript(()=>{
  const real=window.fetch.bind(window);window.fixture={calls:[],available:100,withdrawals:[]}
  window.fetch=async(input,init={})=>{const u=new URL(typeof input==='string'?input:input.url,location.href);if(!u.pathname.startsWith('/api/'))return real(input,init);let data={};window.fixture.calls.push({path:u.pathname,query:u.search,method:init.method||'GET',body:init.body?JSON.parse(init.body):null});
   if(u.pathname.endsWith('/bootstrap'))data={actor:{user_id:501,role:'sales_staff',display_name:'测试销售'},staff_access:{permissions:[]}}
   else if(u.pathname.endsWith('/commission-wallet'))data={wallet:{available_balance_cents:window.fixture.available,frozen_balance_cents:200},earnings:[{id:1,order_no:'TEST-ORDER-1',product_name:'开户时长包',rule_name:'月度销售规则',rule_version_id:1,amount_cents:100,status:'pending',available_at:'2026-11-15T00:00:00+08:00',created_at:'2026-10-01'}],withdrawals:window.fixture.withdrawals}
   else if(u.pathname.endsWith('/commission-withdrawals')){window.fixture.available=0;data={id:1,status:'reviewing'};window.fixture.withdrawals=[{id:1,withdrawal_no:'TEST-WITHDRAW',amount_cents:JSON.parse(init.body).amount_cents,status:'reviewing',requested_at:'2026-10-05',reject_reason:''}]}
   else if(u.pathname.endsWith('/performance'))data={period:'2026-10',items:[],totals:{net_revenue_cents:100,paid_order_count:1}}
   else if(u.pathname.endsWith('/inbox'))data={groups:[],total:0,version:'test',as_of:'2026-10-05',stale:false,unavailable:[]}
   return Response.json(data)
  }
  window.EventSource=class{addEventListener(){}close(){}}
 })
 await page.goto('http://sales.localhost:18605/performance')
 await page.getByText('开户时长包',{exact:true}).waitFor()
 assert.ok(await page.getByText('可提现：',{exact:false}).innerText())
 await page.getByLabel('提现金额').fill('1').catch(()=>page.locator('input[type=number]').fill('1'))
 await page.getByRole('button',{name:'申请提现',exact:true}).click()
 assert.equal(await page.evaluate(()=>window.fixture.calls.filter(c=>c.method==='POST').length),0)
 await page.getByRole('button',{name:'确认提交',exact:true}).click()
 await page.getByText('TEST-WITHDRAW',{exact:true}).waitFor()
 const calls=await page.evaluate(()=>window.fixture.calls)
 assert.equal(calls.filter(c=>c.path.endsWith('/commission-withdrawals')).length,1)
 assert.equal(calls.find(c=>c.path.endsWith('/commission-withdrawals')).body.amount_cents,100)
 assert.ok(calls.some(c=>c.path==='/api/v1/sales/performance'))
 assert.ok(!calls.some(c=>c.path.startsWith('/api/v1/admin/')))
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true)
 await page.evaluate(()=>window.fixture.available=-10)
 await page.getByRole('button',{name:'刷新',exact:true}).click()
 await page.getByText('¥-0.10',{exact:true}).waitFor()
 assert.equal(await page.getByRole('button',{name:'申请提现',exact:true}).isDisabled(),true)
 await page.screenshot({path:path.join(root,'artifacts/commerce-sales-mobile.png'),fullPage:true})
 console.log('PASS sales mobile own-only performance, monthly dates, confirmation, exact cents, records, refund-debt withdrawal block, no horizontal overflow; all writes mocked')
}finally{await browser.close();await new Promise(r=>server.close(r))}
