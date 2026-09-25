// Stateful browser workflow tests. Every /api request is intercepted in a fresh
// Chrome profile. No live login, customer mutation, email, payment or recording.
const {spawn, spawnSync} = require('node:child_process');
const fs = require('node:fs/promises');
const path = require('node:path');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const profile = path.join(root, 'data', 'tmp', 'sales-actions-' + process.pid);
const delay = ms => new Promise(r => setTimeout(r, ms));
let child, socket, commandId = 0;
const waiting = new Map(), exceptions = [];
function send(method, params = {}) {
 return new Promise((resolve,reject) => {
  const id=++commandId, timer=setTimeout(()=>{waiting.delete(id);reject(new Error('CDP timeout: '+method));},15000);
  waiting.set(id,{resolve,reject,timer});socket.send(JSON.stringify({id,method,params}));
 });
}
async function evaluate(expression) {
 const r=await send('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});
 if(r.exceptionDetails)throw new Error(r.exceptionDetails.exception?.description||r.exceptionDetails.text);
 return r.result.value;
}
async function until(expression) {
 for(let n=0;n<70;n++){const r=await evaluate(expression);if(r)return r;await delay(150);}
 throw new Error('UI condition: '+expression+'\n'+await evaluate('document.body.innerText.slice(-1800)'));
}
async function click(label, scope='document') {
 await evaluate(`(()=>{const e=Array.from(${scope}.querySelectorAll('button')).find(e=>e.textContent.trim()===${JSON.stringify(label)});if(!e||e.disabled)throw new Error('button unavailable: '+${JSON.stringify(label)});e.click()})()`);
}
async function fill(label, value, event='input') {
 await evaluate(`(()=>{const l=Array.from(document.querySelectorAll('.sb-dialog label')).find(e=>e.textContent.trim().startsWith(${JSON.stringify(label)}));const e=l?.querySelector('input,textarea,select');if(!e)throw new Error('field missing');e.value=${JSON.stringify(value)};e.dispatchEvent(new Event(${JSON.stringify(event)},{bubbles:true}))})()`);
}
async function rowAction(name, action) {
 await evaluate(`(()=>{const row=Array.from(document.querySelectorAll('.sales-business tbody tr')).find(e=>e.textContent.includes(${JSON.stringify(name)}));const b=Array.from(row?.querySelectorAll('button')||[]).find(e=>e.textContent.trim()===${JSON.stringify(action)});if(!b||b.disabled)throw new Error('row action missing');b.click()})()`);
 await until('!!document.querySelector(".sb-dialog")');
}
async function submit() {
 await evaluate(`(()=>{const f=document.querySelector('.sb-dialog form');if(!f.checkValidity())throw new Error('invalid test form');f.requestSubmit()})()`);
}
async function statusAll() {
 await evaluate(`(()=>{const e=document.querySelector('.data-list-controls .data-filter-select select');e.value='all';e.dispatchEvent(new Event('change',{bubbles:true}))})()`);
 await delay(250);
}
function installMock(role) {
 const real=window.fetch.bind(window), requests=window.__salesTestRequests=[];
 const now='2026-09-24T04:00:00Z';
 const leads=Array.from({length:14},(_,i)=>({id:i+1,lead_no:'TEST-LEAD-'+(i+1),owner_sales_staff_id:1,business_name:'测试商家'+String(i+1).padStart(2,'0'),contact_name:'测试联系人',phone:'1380013'+String(i).padStart(4,'0'),wechat:'',email:'',industry_name:'餐饮',province:'四川省',city:'南充市',district:'顺庆区',address:'测试地址',source_type:'self_developed',stage:'new',status:'open',lost_reason:'',created_at:now,updated_at:now}));
 const activities=new Map(leads.map(l=>[l.id,[{id:1,lead_id:l.id,sales_staff_id:1,sales_display_name:'原销售',activity_type:'created',outcome:'',content:'建立测试顾客',occurred_at:now,created_at:now}]]));
 const handoffs=[{id:1,handoff_no:'TEST-HO-001',customer_name:'测试客户',customer_phone:'13800138000',sales_display_name:'测试销售',status:'pending',summary:'请联调设备',accepted_by_name:'',created_at:now}];
 const events=[],history=[];
 const contactRecords=[{id:2,tenant_id:601,customer_name:'测试客户01',customer_username:'customer01',customer_phone:'13800138000',followup_type:'phone',content:'测试售后使用反馈',created_at:'2026-09-24T05:00:00Z'},{id:1,tenant_id:601,customer_name:'测试客户01',customer_username:'customer01',customer_phone:'13800138000',followup_type:'visit',content:'测试付款前沟通',created_at:'2026-09-24T01:00:00Z'}];
 const customers=Array.from({length:14},(_,i)=>({user_id:501+i,tenant_id:601+i,username:'customer'+String(i+1).padStart(2,'0'),display_name:i===1?'':'测试客户'+String(i+1).padStart(2,'0')+(i===0?' · 用于验证长名称换行和布局的商家'.repeat(3):''),phone:i===1?'':'1380013'+String(i).padStart(4,'0'),email:'',province:'四川省',city:'南充市',district:'顺庆区',address:i===0?'用于检查完整资料的较长详细地址':'',qualified:i===0,collection_status:i===0?'posted':'awaiting_payment',status:i===13?'disabled':'active',source_type:i%2===0?'sales_lead':'direct',created_at:now}));
 const people=[{staff_id:1,user_id:101,display_name:'离职销售',username:'former',status:'disabled',customer_count:2,lead_count:3},{staff_id:2,user_id:102,display_name:'接手销售',username:'next',status:'active',customer_count:0,lead_count:0}];
 const permissions=role==='ops'?['liveops.configure']:role==='manager'?['sales.view_all','sales.assignment.manage']:['sales.customer.view_assigned'];
 const bootstrap={actor:{user_id:101,username:'isolated_test',display_name:'测试账号',phone:'13800138000',province:'四川省',city:'南充市',district:'顺庆区',role:role==='seller'?'sales_staff':'staff',must_change_password:false},staff_access:{is_super_admin:false,primary_group_id:10,primary_group_code:role==='ops'?'live_operations':'sales',primary_group_name:role==='ops'?'营销运维部':'销售部',role_codes:[role==='ops'?'live_operations_staff':role==='manager'?'sales_manager':'sales_staff'],permissions,permission_scopes:Object.fromEntries(permissions.map(x=>[x,'group'])),permission_group_ids:{},group_ids:[10],managed_group_ids:role==='manager'?[10]:[]},permissions,rooms:[],capabilities:[],settings:{}};
 function page(items,u){const p=Number(u.searchParams.get('page')||1),s=Number(u.searchParams.get('page_size')||12);return {items:items.slice((p-1)*s,p*s),total:items.length,page:p,page_size:s,total_pages:Math.max(1,Math.ceil(items.length/s))};}
 window.fetch=async(input,init={})=>{
  const u=new URL(typeof input==='string'?input:input.url,location.href);
  if(!u.pathname.startsWith('/api/'))return real(input,init);
  const method=init.method||'GET',body=init.body?JSON.parse(init.body):null,p=u.pathname;
  requests.push({path:p,query:u.search,method,body});
  let data={items:[],capabilities:[],commands:[],departments:[],tools:[],navigation:[],sections:[],permissions:[]},status=200,m;
  if(p==='/api/v1/bootstrap')data=bootstrap;
  else if(p==='/api/v1/system/public-config')data={site_name:'测试系统',internal_agent_name:'小蓝工作搭子',client_agent_name:'小蓝直播搭子'};
  else if(p==='/api/v1/sales/customers')data={items:customers};
  else if(p==='/api/v1/sales/followups'){
   if(method==='POST'){data={...body,id:contactRecords.length+1,customer_name:'测试客户01',customer_username:'customer01',created_at:'2026-09-24T06:00:00Z'};contactRecords.unshift(data);status=201;}
   else data={items:contactRecords,summary:{total_count:contactRecords.length,due_count:0,overdue_count:0}};
  }
  else if((m=p.match(/^\/api\/v1\/customer-business\/customers\/(\d+)\/recognition$/))){
   const customer=customers.find(c=>c.tenant_id===Number(m[1]));
   if(window.__failRecognition){window.__failRecognition=false;status=503;data={error:'测试财务认定暂不可用'};}
   else data={tenant_id:customer?.tenant_id,qualified:customer?.qualified===true,confirmed_at:customer?.qualified?'2026-09-24T03:00:00Z':undefined,collection_status:customer?.collection_status,confirmed_amount_cents:customer?.qualified?100:0,pending_count:0};
  }
  else if(p==='/api/v1/sales/customers/page'){
   if(window.__salesFailCustomerPage){window.__salesFailCustomerPage=false;status=503;data={error:'测试网络故障，请重试'};}
   else{const s=u.searchParams.get('status'),source=u.searchParams.get('source'),term=u.searchParams.get('search')||'';
    let filtered=customers.filter(c=>(!s||s==='all'||c.status===s)&&(!source||source==='all'||c.source_type===source)&&[c.display_name,c.username,c.phone,c.city].some(v=>v.includes(term)));
    if(u.searchParams.get('sort')==='name-desc')filtered=filtered.slice().reverse();
    data={...page(filtered,u),summary:{total_count:14,active_count:13,disabled_count:1,qualified_count:1,unconfirmed_count:13,pending_receipt_count:0}};
   }
  }
  else if(p==='/api/v1/sales/leads'){
   if(method==='POST'){data={...leads[0],...body,id:15,lead_no:'TEST-NEW',status:'open'};leads.unshift(data);activities.set(15,[]);status=201;}
   else if(window.__salesFailNextList){window.__salesFailNextList=false;status=503;data={error:'测试网络故障，请重试'};}
   else{const state=u.searchParams.get('status'),term=u.searchParams.get('search')||'';const filtered=leads.filter(l=>(!state||state==='all'||l.status===state)&&l.business_name.includes(term));data={...page(filtered,u),summary:{total_count:leads.length,open_count:leads.filter(l=>l.status==='open').length,won_count:leads.filter(l=>l.status==='won').length,lost_count:leads.filter(l=>l.status==='lost').length,visit_due_count:0,followup_due_count:0}};}
  }else if((m=p.match(/^\/api\/v1\/sales\/leads\/(\d+)(?:\/(activities|lost|convert))?$/))){
   const id=Number(m[1]),lead=leads.find(l=>l.id===id),action=m[2];
   if(!lead){status=404;data={error:'not found'};}
   else if(action==='activities'){
    if(method==='POST'){data={...body,id:100,lead_id:id,sales_staff_id:1,sales_display_name:'测试账号',created_at:now};activities.get(id).push(data);lead.stage=body.stage;lead.next_followup_at=body.next_followup_at;status=201;}
    else data=page(activities.get(id),u);
   }else if(action==='lost'){
    if(!body.reason?.trim()){status=400;data={error:'失败原因必填'};}else{lead.status='lost';lead.stage='lost';lead.lost_reason=body.reason;data=lead;}
   }else if(action==='convert'){
    lead.status='registered';lead.stage='registered';lead.converted_user_id=501;lead.converted_tenant_id=601;data={lead,customer:{username:body.username,display_name:body.display_name},handoff:null,credential:{initial_password:'TEST-ONLY-NOT-A-REAL-PASSWORD',login_url:'http://test.invalid/login',delivery_method:'copy',email_sent:false}};status=201;
   }else{if(method==='PUT')Object.assign(lead,body);data=lead;}
  }else if(p==='/api/v1/liveops/customer-handoffs'){
   const s=u.searchParams.get('status');data=page(handoffs.filter(h=>!s||s==='all'||h.status===s),u);
  }else if(p==='/api/v1/liveops/customer-handoffs/1'){
   Object.assign(handoffs[0],{status:body.status,accepted_by_name:'测试运维'});events.push({id:events.length+1,status:body.status,content:body.note,operator_name:'测试运维',created_at:now});data=handoffs[0];
  }else if(p==='/api/v1/liveops/customer-handoffs/1/events')data=page(events,u);
  else if(p==='/api/v1/admin/sales/handover-people'){const s=u.searchParams.get('status');data=page(people.filter(v=>!s||s==='all'||v.status===s),u);}
  else if(p==='/api/v1/admin/sales/1/handover-preview')data={from_sales_staff_id:1,from_display_name:'离职销售',customer_count:2,open_lead_count:3};
  else if(p==='/api/v1/admin/sales/1/handover'){
   data={id:1,handover_no:'TEST-TRANSFER',from_display_name:'离职销售',to_display_name:'接手销售',customer_count:2,lead_count:3,reason:body.reason,status:'completed',created_by_user_id:101,created_at:now};history.push(data);people[0].customer_count=0;people[0].lead_count=0;status=201;
  }else if(p==='/api/v1/admin/sales/handovers')data=page(history,u);
  return new Response(JSON.stringify(data),{status,headers:{'Content-Type':'application/json'}});
 };
}
async function runPage(role,route,work){
 const {identifier}=await send('Page.addScriptToEvaluateOnNewDocument',{source:'('+installMock.toString()+')('+JSON.stringify(role)+')'});
 try{
  await send('Page.navigate',{url:'http://127.0.0.1:5173'+route});
  await until('!!document.querySelector(".sales-business tbody tr")');
  assert.equal(await evaluate('Boolean(document.querySelector(".forced-password-backdrop"))'),false,'fixture unexpectedly blocked by required account profile');
  assert.equal(await evaluate('getComputedStyle(document.querySelector(".sales-business")).fontSize'),'16px');
  const tooSmall=await evaluate('Array.from(document.querySelectorAll(".sales-business button,.sales-business input,.sales-business select,.sales-business td")).filter(e=>parseFloat(getComputedStyle(e).fontSize)<16).map(e=>({tag:e.tagName,classes:e.className,text:e.textContent.trim().slice(0,40),size:getComputedStyle(e).fontSize}))');
  assert.deepEqual(tooSmall,[],'main text and controls too small: '+JSON.stringify(tooSmall));
  await evaluate('document.querySelector(".data-view-toggle button").scrollIntoView({block:"center",behavior:"instant"})');await delay(300);
  const point=await evaluate('(()=>{const e=document.querySelector(".data-view-toggle button");const r=e.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()');
  await send('Input.dispatchMouseEvent',{type:'mouseMoved',...point});await delay(220);
  const hover=await evaluate('(()=>{const e=document.querySelector(".data-view-toggle button"),r=e.getBoundingClientRect();return {hover:e.matches(":hover"),shadow:getComputedStyle(e).boxShadow,hit:document.elementFromPoint(r.x+r.width/2,r.y+r.height/2)?.outerHTML.slice(0,300)}})()');
  assert.ok(hover.hover&&hover.shadow!=='none','hover glow missing: '+JSON.stringify(hover));
  await send('Input.dispatchMouseEvent',{type:'mouseMoved',x:1,y:1});
  await work();
  const requests=await evaluate('window.__salesTestRequests');
  assert.ok(!requests.some(r=>r.method!=='GET'&&/payments|recharge|refund|resources\/adjust/.test(r.path)));
  console.log('PASS '+route+' stateful UI workflow; mocked API only');
 }finally{await send('Page.removeScriptToEvaluateOnNewDocument',{identifier});}
}
async function seller(){
 await click('下一页','document.querySelector(".sales-business .pagination-bar")');
 await until('document.querySelectorAll(".sales-business tbody tr").length===2');
 assert.ok((await evaluate('window.__salesTestRequests')).some(r=>r.query.includes('page=2')));
 await click('卡片','document.querySelector(".data-view-toggle")');await until('document.querySelectorAll(".sb-record").length===2');
 await click('上一页','document.querySelector(".sales-business .pagination-bar")');await until('document.querySelectorAll(".sb-record").length===12');
 await click('表格','document.querySelector(".data-view-toggle")');
 await click('＋ 新增意向顾客');await fill('商家 / 顾客名称','回归新顾客');await fill('电话','13800139999');await submit();
 await until('!document.querySelector(".sb-dialog")&&document.body.innerText.includes("已保存：回归新顾客")');
 const create=await evaluate('window.__salesTestRequests.find(r=>r.method==="POST"&&r.path==="/api/v1/sales/leads")');
 for(const key of ['owner_sales_staff_id','status','converted_user_id'])assert.ok(!(key in create.body));
 await rowAction('测试商家01','编辑 / 改期');await fill('联系人','修改后的联系人');await submit();await until('!document.querySelector(".sb-dialog")');
 await rowAction('测试商家01','记拜访');await fill('拜访 / 沟通内容','客户需要三台设备，约定再次联系');await fill('最新阶段','interested','change');await fill('下一次跟进','2030-01-02T10:00');await submit();await until('!document.querySelector(".sb-dialog")');
 const visit=await evaluate('window.__salesTestRequests.find(r=>r.method==="POST"&&r.path.endsWith("/1/activities"))');assert.equal(visit.body.stage,'interested');assert.ok(visit.body.next_followup_at.endsWith('Z'));
 await rowAction('测试商家01','详情');await until('document.querySelector(".sb-dialog").innerText.includes("客户需要三台设备")');await click('关闭','document.querySelector(".sb-dialog")');
 await rowAction('测试商家02','未成交');await evaluate('document.querySelector(".sb-dialog form").requestSubmit()');assert.ok(!(await evaluate('window.__salesTestRequests')).some(r=>r.path.endsWith('/2/lost')));
 await fill('失败原因','客户预算不足，半年后再评估');await submit();await until('!document.querySelector(".sb-dialog")');await statusAll();await until('document.body.innerText.includes("客户预算不足")');
 await rowAction('测试商家01','预开户');assert.equal(await evaluate('document.querySelector(".sb-dialog button[type=submit]").disabled'),true);
 await fill('开户说明','已确认开户，收款尚未财务审核');await evaluate('document.querySelector(".sb-dialog input[type=checkbox]").click()');await submit();
 await until('!!document.querySelector(".credential-result-modal")');await click('完成','document.querySelector(".credential-result-modal")');
 assert.equal((await evaluate('window.__salesTestRequests')).filter(r=>r.path.endsWith('/1/convert')).length,1);
 await evaluate('window.__salesFailNextList=true');await click('刷新','document.querySelector(".sales-business")');await until('document.querySelector(".sb-error")?.innerText.includes("测试网络故障")');await click('重试','document.querySelector(".sb-error")');await until('!document.querySelector(".sb-error")');
 console.log('PASS server paging, table/cards, create/edit/visit/history, mandatory loss reason, confirmed conversion, error/retry');
}
async function customerList(){
 await until('document.querySelectorAll(".sc-table tbody tr").length===12');
 const layout=await evaluate(`(()=>{const r=document.querySelector('.sc-table tbody tr');const a=r.querySelector('.sc-avatar');return {columns:r.cells.length,identity:getComputedStyle(r.querySelector('.sc-identity')).display,avatar:Math.round(a.getBoundingClientRect().width),raw:document.querySelector('.sc-panel').innerText.includes('sales_lead')}})()`);
 assert.equal(layout.columns,9);assert.equal(layout.identity,'flex');assert.equal(layout.avatar,46);assert.equal(layout.raw,false);
 assert.ok(await evaluate('document.querySelector(".sc-panel").innerText.includes("意向顾客转入")'));
 assert.equal(await evaluate('document.querySelectorAll(".sc-table tbody tr")[0].querySelector("a[href*=followups]").textContent.trim()'),'售后回访记录');
 assert.equal(await evaluate('document.querySelectorAll(".sc-table tbody tr")[1].querySelector("a[href*=followups]").textContent.trim()'),'记录跟进');
 await click('下一页','document.querySelector(".sales-customers-page .pagination-bar")');await until('document.querySelectorAll(".sc-table tbody tr").length===2');
 await click('卡片','document.querySelector(".data-view-toggle")');await until('document.querySelectorAll(".sc-card").length===2');
 assert.equal(await evaluate('getComputedStyle(document.querySelector(".sc-card-grid")).display'),'grid');
 await click('上一页','document.querySelector(".sales-customers-page .pagination-bar")');await until('document.querySelectorAll(".sc-card").length===12');
 const card=await evaluate(`(()=>{const c=document.querySelector('.sc-card');const a=c.querySelector('.sc-avatar').getBoundingClientRect(),n=c.querySelector('h3').getBoundingClientRect();return {separated:n.x>=a.right+8,width:c.getBoundingClientRect().width,nameHeight:n.height}})()`);
 assert.ok(card.separated&&card.width>=290,'card layout regressed '+JSON.stringify(card));
 assert.equal(await evaluate('document.querySelector(".sc-card a[href*=followups]").textContent.trim()'),'售后回访记录');
 await click('表格','document.querySelector(".data-view-toggle")');await until('!!document.querySelector(".sc-table tbody tr")');
 await rowAction('测试客户01','查看资料');await until('document.querySelector(".sc-details")?.innerText.includes("用于检查完整资料")');
 assert.ok(await evaluate('Array.from(document.querySelectorAll(".sc-details a")).some(a=>a.getAttribute("href")==="/sales/followups?tenant=601")'));
  for(const path of ['/sales/customers/601/money','/sales/receipts?tenant=601','/sales/support?tenant=601'])assert.ok(await evaluate('Array.from(document.querySelectorAll(".sc-details a")).some(a=>a.getAttribute("href")==='+JSON.stringify(path)+')'));
 await send('Input.dispatchKeyEvent',{type:'keyDown',key:'Escape',code:'Escape',windowsVirtualKeyCode:27});await until('!document.querySelector(".sc-details")');
 await evaluate(`(()=>{const s=document.querySelector('.sc-extra-filters select');s.value='sales_lead';s.dispatchEvent(new Event('change',{bubbles:true}))})()`);
 await until('document.querySelectorAll(".sc-table tbody tr").length===7');
 await click('清空筛选');await until('document.querySelectorAll(".sc-table tbody tr").length===12');
 await evaluate(`(()=>{const s=document.querySelector('.data-list-controls .data-filter-select select');s.value='disabled';s.dispatchEvent(new Event('change',{bubbles:true}))})()`);
 await until('document.querySelectorAll(".sc-table tbody tr").length===1&&document.querySelector(".sc-table").innerText.includes("已停用")');
 await click('清空筛选');
 await evaluate(`(()=>{const s=document.querySelector('.data-list-controls input');s.value='没有这个测试客户';s.dispatchEvent(new Event('input',{bubbles:true}))})()`);
 await until('document.querySelector(".sb-empty")?.innerText.includes("没有符合条件")');
 await click('清空筛选');await until('document.querySelectorAll(".sc-table tbody tr").length===12');
 await evaluate('window.__salesFailCustomerPage=true');await click('刷新客户');await until('document.querySelector(".sb-error")?.innerText.includes("测试网络故障")');
 assert.equal(await evaluate('Boolean(document.querySelector(".sb-empty"))'),false,'request failure shown as empty portfolio');
 await click('重试','document.querySelector(".sb-error")');await until('document.querySelectorAll(".sc-table tbody tr").length===12');
 const requests=await evaluate('window.__salesTestRequests');
 assert.ok(requests.some(r=>r.path.endsWith('/customers/page')&&r.query.includes('page=2')));
 assert.ok(!requests.some(r=>r.path==='/api/v1/sales/customers'),'new list still calls unbounded API');
 assert.ok(!requests.some(r=>r.method!=='GET'),'customer list unexpectedly writes data');
 console.log('PASS customer DOM layout, readable text, card/table, server paging/filter, Chinese labels, readonly details, followup link, error/retry');
}
async function contactRecords(){
 const {identifier}=await send('Page.addScriptToEvaluateOnNewDocument',{source:'('+installMock.toString()+')("seller")'});
 try{
  await send('Page.navigate',{url:'http://127.0.0.1:5173/sales/followups?tenant=601'});
  await until('document.querySelector(".sales-followups-page h2")?.textContent==="售后回访记录"');
  assert.ok(await evaluate('document.querySelector(".sales-followup-form").innerText.includes("下次回访时间")'));
  assert.deepEqual(await evaluate('Array.from(document.querySelectorAll(".contact-record-kind")).map(e=>e.textContent)'),['售后回访记录','跟进记录']);
  await evaluate('(()=>{const f=document.querySelector(".sales-followup-form"),e=f.querySelector("textarea");e.value="测试回访：设备运行正常";e.dispatchEvent(new Event("input",{bubbles:true}));f.requestSubmit()})()');
  await until('document.querySelector(".inline-success")?.textContent.includes("售后回访记录已保存")');
  const saved=await evaluate('window.__salesTestRequests.find(r=>r.method==="POST"&&r.path==="/api/v1/sales/followups")');
  assert.equal(saved.body.tenant_id,601);assert.equal(saved.body.content,'测试回访：设备运行正常');assert.ok(!('qualified' in saved.body));
  await evaluate('(()=>{const e=document.querySelector(".sales-followup-form select");e.value="602";e.dispatchEvent(new Event("change",{bubbles:true}))})()');
  await until('document.querySelector(".sales-followups-page h2")?.textContent==="跟进记录"');
  assert.ok(await evaluate('document.querySelector(".sales-followup-form").innerText.includes("下次跟进时间")'));
  await evaluate('window.__failRecognition=true;document.querySelector(".sales-followups-page .feature-workspace-hero button").click()');
  await until('document.querySelector(".sales-followup-form .sb-error")?.textContent.includes("测试财务认定暂不可用")');
  assert.equal(await evaluate('document.querySelector(".sales-followups-page h2").textContent'),'客户联系记录');
  await click('重新核对','document.querySelector(".sales-followup-form")');
  await until('document.querySelector(".sales-followups-page h2")?.textContent==="跟进记录"');
  await evaluate('(()=>{const e=document.querySelector(".sales-followup-form select");e.value="601";e.dispatchEvent(new Event("change",{bubbles:true}))})()');
  await until('document.querySelector(".sales-followups-page h2")?.textContent==="售后回访记录"');
  assert.equal(await evaluate('document.querySelector(".sales-followup-form select").value'),'601');
  const small=await evaluate('Array.from(document.querySelectorAll(".sales-followup-form button,.sales-followup-form input,.sales-followup-form textarea,.sales-followup-form select")).filter(e=>parseFloat(getComputedStyle(e).fontSize)<16).length');assert.equal(small,0);
  console.log('PASS unpaid followup vs qualified after-sales labels, form/save, historical boundary, recognition error/retry, customer switching; mocked API only');
 }finally{await send('Page.removeScriptToEvaluateOnNewDocument',{identifier})}
}
async function ops(){
 await rowAction('测试客户','接单');await fill('处理说明','已联系客户');await submit();await until('!document.querySelector(".sb-dialog")');await statusAll();await until('document.body.innerText.includes("开始处理")');
 await rowAction('测试客户','开始处理');await fill('处理说明','设备正在联调');await submit();await until('!document.querySelector(".sb-dialog")');
 await rowAction('测试客户','完成交接');await evaluate('document.querySelector(".sb-dialog form").requestSubmit()');assert.equal((await evaluate('window.__salesTestRequests')).filter(r=>r.method==='PATCH').length,2);
 await fill('完成说明','三台设备联调通过，已向客户讲解使用');await submit();await until('!document.querySelector(".sb-dialog")');
 await rowAction('测试客户','处理历史');await until('document.querySelector(".sb-dialog").innerText.includes("三台设备联调通过")');assert.equal((await evaluate('window.__salesTestRequests')).filter(r=>r.method==='PATCH').length,3);
 console.log('PASS operations accept/start/complete with required completion note and full history');
}
async function manager(){
 await rowAction('离职销售','预览并交接');await until('document.querySelector(".sb-dialog").innerText.includes("接手销售 · @next")');await click('接手销售 · @next','document.querySelector(".sb-dialog")');
 assert.equal(await evaluate('document.querySelector(".sb-dialog button[type=submit]").disabled'),true);
 await fill('交接原因','销售离职，由接手销售继续跟进现有客户');await evaluate('document.querySelector(".sb-dialog input[type=checkbox]").click()');await submit();await until('!document.querySelector(".sb-dialog")&&document.body.innerText.includes("TEST-TRANSFER")');
 const req=await evaluate('window.__salesTestRequests.find(r=>r.method==="POST"&&r.path.endsWith("/handover"))');assert.equal(req.body.to_sales_staff_id,2);assert.equal(req.body.expected_customer_count,2);assert.equal(req.body.expected_lead_count,3);
 console.log('PASS disabled-source transfer, active recipient, count preview, explicit confirmation and history');
}
(async()=>{
 await fs.mkdir(profile,{recursive:true});
 child=spawn(process.env.CHROME_PATH||'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',['--headless=new','--disable-gpu','--no-first-run','--no-default-browser-check','--remote-debugging-port=0','--user-data-dir='+profile,'about:blank'],{stdio:'ignore'});
 child.on('error',e=>console.error(e.message));let port;
 for(let n=0;n<60;n++){try{port=(await fs.readFile(path.join(profile,'DevToolsActivePort'),'utf8')).split(/\r?\n/)[0];break}catch{}await delay(150);}
 if(!port)throw new Error('Isolated Chrome did not start');
 const tabs=await(await fetch('http://127.0.0.1:'+port+'/json/list')).json();socket=new WebSocket(tabs.find(t=>t.type==='page').webSocketDebuggerUrl);
 socket.addEventListener('message',e=>{const m=JSON.parse(e.data);if(m.method==='Runtime.exceptionThrown')exceptions.push(m.params.exceptionDetails.text);const p=waiting.get(m.id);if(!p)return;clearTimeout(p.timer);waiting.delete(m.id);m.error?p.reject(new Error(m.error.message)):p.resolve(m.result);});
 await new Promise((resolve,reject)=>{socket.addEventListener('open',resolve,{once:true});socket.addEventListener('error',reject,{once:true});});
 await send('Page.enable');await send('Runtime.enable');
 await send('Emulation.setDeviceMetricsOverride',{width:1440,height:1000,deviceScaleFactor:1,mobile:false});
 await runPage('seller','/sales/leads',seller);await runPage('ops','/operations/live/customer-handoffs',ops);await runPage('manager','/sales/handovers',manager);
 await runPage('seller','/sales/customers',customerList);
 await contactRecords();
 assert.deepEqual(exceptions,[],'uncaught browser errors');
 console.log('PASS ALL sales UI action contracts; no live API calls or real customer mutations.');
})().catch(e=>{console.error(e.stack||e);process.exitCode=1;}).finally(async()=>{
 for(const p of waiting.values())clearTimeout(p.timer);if(socket)socket.close();if(child?.pid)spawnSync('taskkill.exe',['/PID',String(child.pid),'/T','/F'],{stdio:'ignore'});
 await delay(400);await fs.rm(profile,{recursive:true,force:true,maxRetries:3,retryDelay:300}).catch(()=>{});
});
