// Real browser components, isolated profile, all API requests mocked. Never uses
// real employee credentials, live receipts, payment APIs or customer mutations.
const {spawn,spawnSync}=require('node:child_process');
const fs=require('node:fs/promises'),path=require('node:path'),assert=require('node:assert/strict');
const root=path.resolve(__dirname,'..'),profile=path.join(root,'data/tmp/customer-business-browser-'+process.pid);
const delay=ms=>new Promise(r=>setTimeout(r,ms));let chrome,ws,id=0;const pending=new Map(),exceptions=[];
function send(method,params={}){return new Promise((resolve,reject)=>{const n=++id,timer=setTimeout(()=>{pending.delete(n);reject(Error('CDP timeout '+method))},18000);pending.set(n,{resolve,reject,timer});ws.send(JSON.stringify({id:n,method,params}))})}
async function evaluate(expression){const r=await send('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true});if(r.exceptionDetails)throw Error(r.exceptionDetails.exception?.description||r.exceptionDetails.text);return r.result.value}
async function until(expression){for(let i=0;i<80;i++){try{if(await evaluate(expression))return}catch(e){if(!/Inspected target navigated or closed|Cannot find context|Execution context was destroyed/.test(e.message))throw e}await delay(120)}throw Error('UI condition '+expression+'\n'+await evaluate('document.body.innerText.slice(-2500)'))}
async function click(label,scope='document'){await evaluate(`(()=>{const b=Array.from(${scope}.querySelectorAll('button')).find(b=>b.textContent.trim()===${JSON.stringify(label)});if(!b||b.disabled)throw Error('button missing '+${JSON.stringify(label)});b.click()})()`)}
async function fill(label,value,event='input',scope='document.querySelector(".sb-dialog")'){await evaluate(`(()=>{const l=Array.from(${scope}.querySelectorAll('label')).find(l=>l.textContent.trim().startsWith(${JSON.stringify(label)}));const e=l?.querySelector('input,select,textarea');if(!e)throw Error('field missing '+${JSON.stringify(label)});e.value=${JSON.stringify(value)};e.dispatchEvent(new Event(${JSON.stringify(event)},{bubbles:true}))})()`)}
async function submit(scope='document.querySelector(".sb-dialog")'){await evaluate(`(()=>{const f=${scope}.querySelector('form');if(!f.checkValidity())throw Error('test form invalid');f.requestSubmit()})()`)}
async function rowAction(name,action){await evaluate(`(()=>{const row=Array.from(document.querySelectorAll('.sales-business tbody tr')).find(r=>r.innerText.includes(${JSON.stringify(name)}));const b=Array.from(row?.querySelectorAll('button')||[]).find(v=>v.textContent.trim()===${JSON.stringify(action)});if(!b||b.disabled)throw Error('row action missing');b.click()})()`);await until('!!document.querySelector(".sb-dialog")')}
function installMock(role){
 const original=window.fetch.bind(window),requests=window.__businessRequests=[];const now='2026-09-24T06:00:00Z';
 const customer=role==='customer',seller=role==='seller',admin=role==='admin',readonly=role==='finance-readonly',self=role==='finance-self',finance=['finance','admin','finance-readonly','finance-self'].includes(role),ops=role==='ops';
 const user=customer?501:(seller||self)?101:finance?900:103,group=finance?'finance':ops?'live_operations':'sales';
 const permissions=finance?(readonly?['finance.dashboard.view']:['finance.dashboard.view','finance.recharge.create','finance.recharge.approve']):ops?['liveops.configure']:['sales.customer.view_assigned'];
 const bootstrap={actor:{user_id:user,tenant_id:customer?601:null,username:'test_only',display_name:'测试账号',phone:'13800138000',province:'四川省',city:'南充市',district:'顺庆区',role:customer?'customer':seller?'sales_staff':admin?'platform_admin':'staff',must_change_password:false},staff_access:customer?null:{is_super_admin:admin,primary_group_id:10,primary_group_code:group,primary_group_name:finance?'财务部':ops?'营销运维部':'销售部',role_codes:[group+'_staff'],permissions,permission_scopes:Object.fromEntries(permissions.map(p=>[p,'all'])),permission_group_ids:{},group_ids:[10],managed_group_ids:[]},permissions,rooms:[],settings:{},capabilities:[]};
 const receipts=Array.from({length:14},(_,i)=>({id:i+1,receipt_no:'TEST-R-'+String(i+1).padStart(2,'0'),tenant_id:601,customer_name:'测试客户',channel:'bank_transfer',purpose:'recharge',amount_cents:12345,external_trade_no:'TEST-BANK-'+i,payer_name:'测试付款方',receiving_account:'测试收款账户',evidence:'测试凭据，仅模拟接口',occurred_at:now,status:i===1?'needs_info':'pending',requester_user_id:101,last_submitter_user_id:101,version:1,review_note:i===1?'补充回单':'',created_at:now}));
 const tickets=Array.from({length:14},(_,i)=>({id:i+1,ticket_no:'TEST-SUP-'+String(i+1).padStart(2,'0'),tenant_id:601,customer_name:'测试客户',requester_user_id:101,requester_role:'sales_staff',title:'测试问题'+String(i+1).padStart(2,'0'),description:'设备连接需要协助',category:'equipment',contact_name:'测试联系人',contact_phone:'13800138000',status:i===0&&(customer||seller)?'awaiting_confirmation':'pending',resolution:i===0&&(customer||seller)?'设备已联调':'',assigned_name:i===0&&(customer||seller)?'测试运维':'',version:1,created_at:now,updated_at:now}));
 const receiptEvents=[],ticketEvents=[];let qualified=false;
 const inboxGroups=[];
 const addInbox=(key,topic,department,category,title,to,count)=>inboxGroups.push({key,topic,department,category,title,to,count,due_count:0,shared_queue:true});
 if(finance&&!readonly)addInbox('receipt_review','finance','财务','review','客户收款确认','/staff/finance/receipts',13);
 if(admin){addInbox('repair_accept','inventory','仓储售后','accept','维修退换待受理','/staff/after-sales',14);addInbox('inventory_inbound','inventory','仓储售后','process','设备待入库','/resources/inventory',2);addInbox('logistics_dispatch','logistics','仓储物流','process','物流待发货','/resources/logistics',3)}
 if(customer||seller){addInbox('receipt_supplement','finance','客户服务','supplement','收款资料待补充',customer?'/finance/receipts':'/sales/receipts',1);addInbox('support_confirm','support','客户服务','confirm','处理结果待确认',customer?'/support':'/sales/support',1)}
 window.__inboxState={groups:inboxGroups,total:inboxGroups.reduce((n,g)=>n+g.count,0),version:'mock-inbox-1',as_of:now,stale:false,unavailable:[]};
 window.__inboxSources=[];
 window.EventSource=class extends EventTarget{constructor(url){super();this.url=url;this.closed=false;window.__inboxSources.push(this);setTimeout(()=>{if(!this.closed)this.emit('snapshot',window.__inboxState)},10)}close(){this.closed=true}emit(type,data){if(!this.closed)this.dispatchEvent(new MessageEvent(type,{data:JSON.stringify(data)}))}};
 window.__financeRequireDistinct=true;
 function page(items,u){const p=Number(u.searchParams.get('page')||1),n=Number(u.searchParams.get('page_size')||12);return{items:items.slice((p-1)*n,p*n),total:items.length,page:p,page_size:n,total_pages:Math.max(1,Math.ceil(items.length/n))}}
 window.fetch=async(input,init={})=>{const url=new URL(typeof input==='string'?input:input.url,location.href);if(!url.pathname.startsWith('/api/'))return original(input,init);const p=url.pathname,method=init.method||'GET',body=init.body?JSON.parse(init.body):null;requests.push({p,method,body,q:url.search});let result={items:[],permissions:[],capabilities:[],tools:[],commands:[],departments:[],navigation:[]},status=200,m;
 if(p==='/api/v1/bootstrap')result=bootstrap;
 else if(p==='/api/v1/account')result={profile:bootstrap.actor,quota:{total_seconds:0},membership:null};
 else if(p==='/api/v1/system/public-config')result={site_name:'测试系统',internal_agent_name:'小蓝工作搭子',client_agent_name:'小蓝直播搭子'};
 else if(p==='/api/v1/client-agent/chat')result={reply:'请在订单、售后和运维协助页面查看进度。',capabilities:[]};
 else if(p==='/api/v1/work/inbox')result=window.__inboxState;
 else if(p==='/api/v1/work/inbox/items'){const group=window.__inboxState.groups.find(g=>g.key===url.searchParams.get('group'));if(!group){status=404;result={error:'not allowed'}}else{const items=Array.from({length:group.count},(_,i)=>({id:i+1,key:group.key+':'+i,reference:'TEST-TASK-'+(i+1),title:group.title+' '+(i+1),status:'pending',created_at:now,to:group.to}));result={...page(items,url),group}}}
 else if(p==='/api/v1/staff/finance/review-policy'){if(window.__financePolicyError){status=503;result={error:'测试配置读取失败'}}else result={require_distinct_reviewer:window.__financeRequireDistinct}}
 else if(p==='/api/v1/system/settings'){if(method==='PUT'){if(!admin){status=403;result={error:'settings forbidden'}}else{const change=body.settings.find(v=>v.key==='finance_require_distinct_reviewer');if(change)window.__financeRequireDistinct=change.value!=='false'}}if(status===200)result={settings:[{key:'finance_require_distinct_reviewer',value:String(window.__financeRequireDistinct),group:'finance',label:'强制经办人与审核人不同',input_type:'boolean',sort_order:10}],dictionaries:{},warehouses:[],membership_room_limits:[]}}
 else if(p==='/api/v1/staff/finance')result={customers:[],tasks:[]};
 else if(p==='/api/v1/customer-business/todos')result={pending:receipts.filter(r=>r.status==='pending').length,needs_info:receipts.filter(r=>r.status==='needs_info').length,posting_failed:0};
 else if(p==='/api/v1/customer-business/accounts')result=page([{tenant_id:601,name:'测试客户',username:'testcustomer',qualified,created_at:now}],url);
 else if(p==='/api/v1/customer-business/receipts'){
  if(method==='POST'){result={...receipts[0],...body,id:15,receipt_no:'TEST-NEW',status:'pending',version:1};receipts.unshift(result);status=201}
  else if(window.__failBusinessList){window.__failBusinessList=false;status=503;result={error:'测试请求失败'}}
  else{const state=url.searchParams.get('status');result=page(receipts.filter(r=>!state||state==='all'||r.status===state),url)}
 }else if((m=p.match(/^\/api\/v1\/customer-business\/receipts\/(\d+)\/(review|resubmit|events)$/))){const r=receipts.find(r=>r.id===Number(m[1]));if(m[2]==='events')result=page(receiptEvents,url);else if(m[2]==='review'){if(!finance||readonly||!body.confirmed||(window.__financeRequireDistinct&&(r.requester_user_id===user||r.last_submitter_user_id===user))){status=403;result={error:'review denied'}}else{r.status=body.action==='post'?'posted':body.action==='reject'?'rejected':'needs_info';r.review_note=body.note;r.version++;if(r.status==='posted'){r.posted_at=now;qualified=true}receiptEvents.push({id:receiptEvents.length+1,action:r.status,note:body.note,actor_name:'测试财务',created_at:now});result=r}}else{r.status='pending';r.version++;result=r}}
 else if(p.endsWith('/recognition'))result={tenant_id:601,qualified,collection_status:qualified?'posted':'pending',confirmed_at:qualified?now:undefined,confirmed_amount_cents:qualified?12345:0,pending_count:1};
 else if(p.endsWith('/money'))result=page(Array.from({length:14},(_,i)=>({entry_key:'test:'+i,kind:i===0?'receipt_recharge':'order_payment',reference_no:'TEST-MONEY-'+i,amount_cents:12345,direction:'in',channel:i===1?'sandbox':'bank_transfer',status:i===1?'simulated':'posted',occurred_at:now,posted_at:now,note:'测试记录，不与真实客户相关'})),url);
 else if(p==='/api/v1/service/tickets'){if(method==='POST'){result={...tickets[1],...body,id:15,ticket_no:'TEST-NEW-SUP',status:'pending',requester_role:bootstrap.actor.role};tickets.unshift(result);status=201}else result=page(tickets.filter(t=>!url.searchParams.get('status')||url.searchParams.get('status')==='all'||t.status===url.searchParams.get('status')),url)}
 else if((m=p.match(/^\/api\/v1\/service\/tickets\/(\d+)\/(actions|events)$/))){const t=tickets.find(t=>t.id===Number(m[1]));if(m[2]==='events')result=page(ticketEvents,url);else{const state={accept:'accepted',start:'in_progress',wait_customer:'waiting_customer',resolve:'awaiting_confirmation',confirm:'completed',reopen:'pending',cancel:'cancelled'}[body.action];if(state)t.status=state;t.version++;if(body.action==='accept')t.assigned_name='测试运维';if(body.action==='resolve')t.resolution=body.note;ticketEvents.push({id:ticketEvents.length+1,action:body.action,note:body.note,actor_name:bootstrap.actor.display_name,created_at:now});result=t}}
 return new Response(JSON.stringify(result),{status,headers:{'Content-Type':'application/json'}})};
}
async function run(role,url,work){const {identifier}=await send('Page.addScriptToEvaluateOnNewDocument',{source:'('+installMock.toString()+')('+JSON.stringify(role)+')'});try{await send('Page.navigate',{url});await until('!!document.querySelector(".sales-business,.support-page")');await until('window.__businessRequests?.some(r=>r.p==="/api/v1/service/tickets"||r.p==="/api/v1/customer-business/receipts"||r.p.endsWith("/money"))');await delay(350);const bad=await evaluate('Array.from(document.querySelectorAll(".sales-business button,.sales-business td,.support-page button,.support-page input")).filter(e=>parseFloat(getComputedStyle(e).fontSize)<16).map(e=>e.outerHTML.slice(0,80))');assert.deepEqual(bad,[],'main text below 16px');await work();console.log('PASS '+role+' '+url+' mocked API browser actions')}finally{await send('Page.removeScriptToEvaluateOnNewDocument',{identifier})}}
async function receiptSeller(){await until('document.querySelectorAll(".sales-business tbody tr").length===12');assert.ok(!await evaluate('document.body.innerText.includes("核实 / 审核入账")'));await click('下一页','document.querySelector(".sales-business .pagination-bar")');await until('document.querySelectorAll(".sales-business tbody tr").length===2');await click('卡片','document.querySelector(".data-view-toggle")');await until('document.querySelectorAll(".sb-record").length===2');await click('表格','document.querySelector(".data-view-toggle")');await click('提交收款资料');await fill('实收金额','123.45');await fill('实际收款时间','2026-09-24T13:00');await fill('付款方','测试付款人');await fill('收款账户标识','测试对公账户');await fill('银行 / 渠道','TEST-REAL-REF');await fill('凭据编号','银行回单 TEST-001，待财务核实');await evaluate('document.querySelector(".sb-dialog input[type=checkbox]").click()');await submit();await until('!document.querySelector(".sb-dialog")');const req=await evaluate('window.__businessRequests.find(r=>r.method==="POST"&&r.p.endsWith("/receipts"))');assert.equal(req.body.amount_cents,12345);assert.equal(req.body.tenant_id,601);for(const key of ['status','reviewer_user_id','qualified'])assert.ok(!(key in req.body));await evaluate('window.__failBusinessList=true');await click('刷新');await until('!!document.querySelector(".sb-error")');await click('重试');await until('!document.querySelector(".sb-error")')}
async function finance(){await until('!!document.querySelector(".sales-business tbody tr")');await rowAction('TEST-R-01','核实 / 审核入账');assert.equal(await evaluate('document.querySelector(".sb-dialog button[type=submit]").disabled'),true);await fill('对外审核说明','已核实到账金额和真实银行流水');await evaluate('document.querySelector(".sb-dialog input[type=checkbox]").click()');await submit();await until('!document.querySelector(".sb-dialog")');await until('document.querySelector(".sales-business tbody tr").innerText.includes("财务已入账")');await rowAction('TEST-R-01','详情与历史');await until('document.querySelector(".sb-dialog").innerText.includes("已核实到账金额")');await click('关闭','document.querySelector(".sb-dialog")');await click('未确认客户（不是已收款）');await until('document.querySelector(".customer-receipts-page tbody")?.innerText.includes("待财务确认")');assert.ok((await evaluate('window.__businessRequests')).some(r=>r.p.endsWith('/review')&&r.body.confirmed===true))}
async function approvalDiscovery(){
 await until('!!document.querySelector(".customer-receipts-page tbody tr")');
 assert.ok(await evaluate('Array.from(document.querySelectorAll(".sidebar a")).some(a=>a.getAttribute("href")==="/staff/finance/receipts")'),'admin sidebar receipt entry missing');
 assert.ok(await evaluate('!!document.querySelector(".receipt-review-button")'));
 assert.equal(await evaluate('getComputedStyle(document.querySelector(".customer-receipts-page tbody tr td:last-child")).position'),'sticky');
 await rowAction('TEST-R-01','详情与历史');
 assert.equal(await evaluate('document.querySelector(".sb-dialog .receipt-review-button").disabled'),false);
 await click('核实 / 审核入账','document.querySelector(".sb-dialog")');
 await until('!!document.querySelector(".sb-dialog form")');
 await click('关闭','document.querySelector(".sb-dialog")');
 assert.ok(!(await evaluate('window.__businessRequests')).some(r=>r.p.endsWith('/review')),'opening review must not post money');
 await send('Page.navigate',{url:'http://127.0.0.1:5173/staff/finance/approvals'});
 await until('!!document.querySelector(".receipt-review-shortcut a")');
 assert.equal(await evaluate('document.querySelector(".receipt-review-shortcut a").getAttribute("href")'),'/staff/finance/receipts');
 await evaluate('document.querySelector(".receipt-review-shortcut a").click()');
 await until('!!document.querySelector(".customer-receipts-page tbody tr")');
 console.log('PASS admin direct navigation, old approval shortcut, sticky actions and detail review entry');
}
async function blockedApproval(){
 await until('!!document.querySelector(".customer-receipts-page tbody tr")');
 const expected=await evaluate('document.querySelector(".receipt-review-reason").getAttribute("title")');
 assert.ok(expected.includes('本人提交或补件')||expected.includes('没有收款审核权限'));
 assert.equal(await evaluate('document.querySelector(".receipt-review-button").disabled'),true);
 await rowAction('TEST-R-01','详情与历史');
 assert.equal(await evaluate('document.querySelector(".sb-dialog .receipt-review-button").disabled'),true);
 assert.ok(await evaluate('document.querySelector(".sb-dialog .receipt-review-reason").textContent.length>0'));
 await click('关闭','document.querySelector(".sb-dialog")');
 await click('卡片','document.querySelector(".data-view-toggle")');
 await until('document.querySelectorAll(".sb-record").length===12');
 assert.ok(await evaluate('Array.from(document.querySelectorAll(".sb-record .receipt-review-button")).every(b=>b.disabled)'));
 assert.ok(!(await evaluate('window.__businessRequests')).some(r=>r.p.endsWith('/review')));
 console.log('PASS blocked approval stays visible with reason; no review calls');
}
// Exercise actual pointer/keyboard states; DOM click alone cannot catch hover contrast regressions.
async function receiptButtonStates(){
 const tabs='.customer-receipts-page > .sb-panel > .sb-actions > button';
 const samples=[];
 function luminance(css){
  const numbers=css.match(/[\d.]+/g)?.map(Number);
  assert.ok(numbers&&numbers.length>=3,'unsupported browser color '+css);
  assert.ok(numbers.length<4||numbers[3]===1,'button background must be opaque '+css);
  const linear=n=>{const v=n/255;return v<=0.04045?v/12.92:((v+0.055)/1.055)**2.4};
  return linear(numbers[0])*0.2126+linear(numbers[1])*0.7152+linear(numbers[2])*0.0722;
 }
 async function state(selector,label,expected={}){
  const v=await evaluate(`(()=>{const e=document.querySelector(${JSON.stringify(selector)});if(!e)throw Error('test button missing');const s=getComputedStyle(e);return {color:s.color,background:s.backgroundColor,font:s.fontSize,shadow:s.boxShadow,hover:e.matches(':hover'),focus:e.matches(':focus-visible'),active:e.matches(':active'),disabled:e.disabled,opacity:s.opacity}})()`);
  for(const [key,value] of Object.entries(expected))assert.equal(v[key],value,label+' '+key);
  if(!v.disabled){
   const a=luminance(v.color),b=luminance(v.background),contrast=(Math.max(a,b)+0.05)/(Math.min(a,b)+0.05);
   assert.ok(contrast>=4.5,label+' loses text contrast: '+JSON.stringify({...v,contrast}));
   if(v.hover||v.focus)assert.notEqual(v.shadow,'none',label+' loses blue interaction glow');
   samples.push({label,contrast:Number(contrast.toFixed(2)),color:v.color,background:v.background});
  }
  // The receipt page is 18px; the teleported shared dialog keeps its existing font.
  if(selector.startsWith('.customer-receipts-page'))assert.equal(v.font,'18px',label+' changes requested font size');
  return v;
 }
 async function pointAt(selector){
  await evaluate(`document.querySelector(${JSON.stringify(selector)}).scrollIntoView({block:'center',behavior:'instant'})`);
  await delay(180);
  const point=await evaluate(`(()=>{const e=document.querySelector(${JSON.stringify(selector)}),r=e.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()`);
  await send('Input.dispatchMouseEvent',{type:'mouseMoved',...point});await delay(240);
  return point;
 }
 async function keyboardFocus(selector){
  await send('Input.dispatchMouseEvent',{type:'mouseMoved',x:1,y:1});
  await evaluate('document.activeElement?.blur()');
  await send('Input.dispatchKeyEvent',{type:'keyDown',key:'Tab',code:'Tab',windowsVirtualKeyCode:9});
  await send('Input.dispatchKeyEvent',{type:'keyUp',key:'Tab',code:'Tab',windowsVirtualKeyCode:9});
  await evaluate(`document.querySelector(${JSON.stringify(selector)}).focus({preventScroll:true})`);await delay(220);
 }
 await until('document.querySelectorAll("'+tabs+'").length===2');
 for(const [index,label] of [[1,'收款单与待办'],[2,'未确认客户（不是已收款）']]){
  const selected=tabs+':nth-child('+index+')',other=tabs+':nth-child('+(index===1?2:1)+')';
  await click(label);await until('document.querySelector('+JSON.stringify(selected)+').classList.contains("primary")');
  await send('Input.dispatchMouseEvent',{type:'mouseMoved',x:1,y:1});await evaluate('document.activeElement?.blur()');await delay(220);
  const rest=await state(selected,label+' selected',{disabled:false});
  const point=await pointAt(selected);await state(selected,label+' selected hover',{hover:true,color:rest.color});
  await send('Input.dispatchMouseEvent',{type:'mousePressed',button:'left',clickCount:1,...point});await delay(220);
  await state(selected,label+' selected pressed',{active:true,color:rest.color});
  await send('Input.dispatchMouseEvent',{type:'mouseReleased',button:'left',clickCount:1,...point});
  await keyboardFocus(selected);await state(selected,label+' selected keyboard',{focus:true,color:rest.color});
  await evaluate('document.activeElement?.blur()');await send('Input.dispatchMouseEvent',{type:'mouseMoved',x:1,y:1});await delay(220);
  await state(selected,label+' mouse leave',{color:rest.color,background:rest.background});
  await pointAt(other);await state(other,label+' unselected hover',{hover:true});
  await keyboardFocus(other);await state(other,label+' unselected keyboard',{focus:true});
 }
 await click('收款单与待办');await until('!!document.querySelector(".customer-receipts-page tbody tr")');
 const primary='.customer-receipts-page > .sb-toolbar button.primary';
 await pointAt(primary);await state(primary,'primary action hover',{hover:true});
 await keyboardFocus(primary);await state(primary,'primary action keyboard',{focus:true});
 await rowAction('TEST-R-01','核实 / 审核入账');
 const disabled='.sb-dialog button[type=submit]';
 await send('Input.dispatchMouseEvent',{type:'mouseMoved',x:1,y:1});await delay(220);
 const before=await state(disabled,'disabled primary',{disabled:true});await pointAt(disabled);
 await state(disabled,'disabled primary hover',{disabled:true,color:before.color,background:before.background,opacity:before.opacity,font:before.font});
 await click('关闭','document.querySelector(".sb-dialog")');
 assert.ok((await evaluate('window.__businessRequests')).every(r=>r.method==='GET'),'style test must not write business data');
 await send('Input.dispatchMouseEvent',{type:'mouseMoved',x:1,y:1});
 console.log('PASS selected/unselected tabs, pointer hover/press/leave, keyboard focus, primary action and disabled state; 18px; contrast samples '+JSON.stringify(samples));
}
// Two distinct finance businesses must not look like two reviews of one receipt.
async function financeEntryNavigation(){
 await until('document.querySelector(".customer-receipts-page h2")?.textContent==="客户收款确认"');
 await send('Page.navigate',{url:'http://127.0.0.1:5173/staff/finance'});
 await until('document.querySelectorAll(".hub-finance .finance-review-entry").length===2');
 const entries=await evaluate(`Array.from(document.querySelectorAll('.hub-finance .finance-review-entry')).map(e=>({to:e.getAttribute('href'),title:e.querySelector('strong').textContent,hint:e.querySelector('.module-entry-scope').textContent,english:e.querySelector('.module-quick-card-en').textContent,titleFont:getComputedStyle(e.querySelector('strong')).fontSize,hintFont:getComputedStyle(e.querySelector('.module-entry-scope')).fontSize}))`);
 assert.deepEqual(entries.map(e=>[e.to,e.title,e.hint]),[
  ['/staff/finance/receipts','客户收款确认','客户付款核实与入账'],
  ['/staff/finance/approvals','资金与权益审批','充值、退款、奖励、时长申请'],
 ]);
 for(const entry of entries){assert.notEqual(entry.english,'FUNCTION');assert.notEqual(entry.english,'PENDING APPROVALS');assert.equal(entry.titleFont,'18px');assert.equal(entry.hintFont,'16px')}
 assert.ok(!await evaluate(`Array.from(document.querySelectorAll('.hub-finance .module-quick-card strong')).some(e=>e.textContent==='待审核')`));
 assert.equal(await evaluate(`document.querySelector('.hub-finance .module-hub-metrics-v2')`),null,'overview statistics must be removed');
 assert.ok(!(await evaluate('window.__businessRequests')).some(r=>r.p==='/api/v1/staff/finance'),'function hub must not load business records solely for counts');
 await evaluate(`document.querySelector('.hub-finance a[href="/staff/finance/approvals"]').click()`);
 await until('document.querySelector(".staff-finance-page h2")?.textContent==="资金与权益审批"');
 assert.ok(await evaluate(`document.querySelector('.receipt-review-shortcut').textContent.includes('这里审批内部操作申请')&&!document.querySelector('.receipt-review-shortcut').textContent.includes('不核对客户付款凭据')`));
 await evaluate(`document.querySelector('.receipt-review-shortcut a').click()`);
 await until('document.querySelector(".customer-receipts-page h2")?.textContent==="客户收款确认"');
 assert.ok(await evaluate(`!!document.querySelector('.customer-receipts-page .receipt-review-button')&&!document.querySelector('.receipt-review-guide')`));
 assert.ok((await evaluate('window.__businessRequests')).every(r=>r.method==='GET'),'navigation must not mutate money or policy');
 console.log('PASS finance entry names, visible scopes, 18px/16px, matching page titles, existing links, removed overview counts; mocked GET only');
}
// All module landing pages share the retired statistics presentation.
async function overviewNavigation(){
 const hubs=[['/staff/finance','finance'],['/staff','staff'],['/customers','customers'],['/agents','agents'],['/sales','sales'],['/operations/live','live'],['/operations/live/marketing','activityMarketing'],['/resources','resources']];
 for(const [route,hub] of hubs){
  await send('Page.navigate',{url:'http://127.0.0.1:5173'+route});
  await until(`document.querySelectorAll('.hub-${hub} .module-quick-card').length>0`);
  const layout=await evaluate(`(()=>{const root=document.querySelector('.hub-${hub}'),hero=root.querySelector('.module-hub-hero-v2'),menu=root.querySelector('.module-quick-menu');return{cards:root.querySelectorAll('.module-quick-card').length,stats:root.querySelectorAll('.module-hub-metrics-v2,.sb-metrics,.admin-kpi-grid').length,gap:menu.getBoundingClientRect().top-hero.getBoundingClientRect().bottom}})()`);
  assert.equal(layout.stats,0,route+' still renders statistic blocks');assert.ok(layout.cards>0,route+' loses navigation');assert.ok(layout.gap<80,route+' leaves an empty statistics spacer '+layout.gap);
  const reqs=await evaluate('window.__businessRequests');assert.ok(reqs.every(r=>r.method==='GET'),'navigation writes data');
  assert.ok(!reqs.some(r=>/^\/api\/v1\/(admin\/(agents|customers|sales)|staff\/(finance|dashboard)$|rooms$)/.test(r.p)),route+' still loads lists just for stats');
 }
 console.log('PASS eight module hubs: no summary cards, no empty statistics gap, no count-only business requests; functional links retained');
}
async function mobileOverview(role,url){
 const {identifier}=await send('Page.addScriptToEvaluateOnNewDocument',{source:'('+installMock.toString()+')('+JSON.stringify(role)+')'});
 try{
  await send('Page.navigate',{url});await until('document.querySelectorAll(".quick-grid a").length===4');await delay(250);
  assert.equal(await evaluate('document.querySelectorAll(".metric-grid,.metric-card").length'),0,'mobile home statistics remain');
  const reqs=await evaluate('window.__businessRequests');assert.ok(reqs.every(r=>r.method==='GET'));assert.ok(!reqs.some(r=>/\/sales\/(customers|performance)$|\/rooms$|\/resources\/current$/.test(r.p)),'mobile home makes summary-only requests');
  if(role==='seller'){await evaluate(`document.querySelector('a[href="/performance"]').click()`);await until('document.body.innerText.includes("原统计展示已移除")');assert.equal(await evaluate('document.querySelectorAll(".metric-grid,.metric-card").length'),0);}
  console.log('PASS '+role+' mobile home: statistics removed, service navigation retained; no business writes');
 }finally{await send('Page.removeScriptToEvaluateOnNewDocument',{identifier})}
}
async function assertNoCustomerInbox(){
 await evaluate(`window.dispatchEvent(new Event('focus'));document.dispatchEvent(new Event('visibilitychange'))`);await delay(200);
 assert.equal(await evaluate('document.querySelectorAll(".work-inbox-entry,.todo-badge,.inbox-badge,.mobile-work-inbox,.agent-inbox-tabs,.agent-inbox-switch").length'),0,'terminal inbox UI survived');
 assert.equal(await evaluate('window.__inboxSources.filter(s=>s.url.includes("/work/inbox")).length'),0,'terminal connected inbox stream');
 assert.ok(!(await evaluate('window.__businessRequests')).some(r=>r.p.startsWith('/api/v1/work/inbox')),'terminal fetched inbox data');
}
async function terminalDesktopWithoutInbox(){
 await assertNoCustomerInbox();
 await evaluate(`document.querySelector('.system-agent-orb').click()`);
 await until('!!document.querySelector(".system-agent-dock textarea")');
 await evaluate(`(()=>{const t=document.querySelector('.system-agent-dock textarea');t.value='我的待办';t.dispatchEvent(new Event('input',{bubbles:true}))})()`);
 await click('发送','document.querySelector(".system-agent-dock")');
 await until('document.querySelector(".system-agent-drawer")?.innerText.includes("请在订单、售后和运维协助页面查看进度")');
 await assertNoCustomerInbox();
 assert.equal(await evaluate('document.querySelectorAll(".system-agent-drawer .work-inbox-panel").length'),0);
 assert.ok(await evaluate(`(()=>{const d=document.querySelector('.system-agent-drawer').getBoundingClientRect(),f=document.querySelector('.system-agent-drawer > footer').getBoundingClientRect(),c=document.querySelector('.system-agent-chat').getBoundingClientRect();return c.height>100&&f.bottom<=d.bottom+2})()`),'customer chat layout broken');
 await evaluate(`(async()=>{const m=await import('/src/workInbox.ts');m.startInbox('stale-customer-key');await m.refreshInbox();let rejected=false;try{await m.getInboxItems('receipt_supplement')}catch{rejected=true}if(!rejected)throw Error('direct customer helper must reject');})()`);
 await assertNoCustomerInbox();
 await send('Page.navigate',{url:'http://127.0.0.1:5173/work/inbox?category=review'});
 await until('location.pathname==="/personal"&&!!document.querySelector(".app-shell")');
 await assertNoCustomerInbox();
 console.log('PASS terminal desktop: no inbox menu, badges, tab, requests or stream; ordinary agent chat and legacy URL redirect retained');
}
async function terminalCustomerMobile(){
 const {identifier}=await send('Page.addScriptToEvaluateOnNewDocument',{source:'('+installMock.toString()+')("customer")'});
 try{
  await send('Page.navigate',{url:'http://customer.localhost:5174/'});await until('document.querySelectorAll(".quick-grid a").length===4');await assertNoCustomerInbox();
  await send('Page.navigate',{url:'http://customer.localhost:5174/agent'});await until('!!document.querySelector(".agent-composer input")');await assertNoCustomerInbox();
  await evaluate(`(()=>{const t=document.querySelector('.agent-composer input');t.value='我的待办';t.dispatchEvent(new Event('input',{bubbles:true}));document.querySelector('.agent-composer').requestSubmit()})()`);
  await until('document.querySelector(".agent-chat")?.innerText.includes("请在订单、售后和运维协助页面查看进度")');await assertNoCustomerInbox();
  console.log('PASS terminal mobile: no inbox code/bridge/tab/badge traffic; customer agent remains a normal conversation');
 }finally{await send('Page.removeScriptToEvaluateOnNewDocument',{identifier})}
}
async function mobileInbox(role,url){
 const {identifier}=await send('Page.addScriptToEvaluateOnNewDocument',{source:'('+installMock.toString()+')('+JSON.stringify(role)+')'});
 try{
  await send('Page.navigate',{url});await until('document.querySelector(".agent-inbox-switch .inbox-badge")?.textContent==="2"');
  await evaluate(`Array.from(document.querySelectorAll('.agent-inbox-switch button')).find(b=>b.textContent.includes('我的待办')).click()`);
  await until('document.querySelectorAll(".mobile-work-inbox .inbox-group-list button").length===2');
  const mobileGeometry=await evaluate(`(()=>{const n=document.querySelector('.agent-inbox-switch').getBoundingClientRect(),g=document.querySelector('.mobile-work-inbox .inbox-group-list').getBoundingClientRect(),c=document.querySelector('.agent-composer').getBoundingClientRect();return {tabs:n.height,firstTasks:g.top,composer:c.top}})()`);assert.ok(mobileGeometry.tabs<110&&mobileGeometry.firstTasks<mobileGeometry.composer,'mobile task panel displaced by grid rows');
  assert.ok(!await evaluate(`document.querySelector('.mobile-work-inbox').textContent.includes('维修退换待受理')||document.querySelector('.mobile-work-inbox').textContent.includes('客户收款确认')`),'mobile internal tasks leaked');
  await evaluate(`Array.from(document.querySelectorAll('.mobile-work-inbox nav button')).find(b=>b.textContent.startsWith('待确认')).click()`);
  await until('document.querySelectorAll(".mobile-work-inbox .inbox-group-list button").length===1');
  await evaluate(`document.querySelector('.mobile-work-inbox .inbox-group-list button').click()`);await until('!!document.querySelector(".mobile-work-inbox .inbox-detail article a")');
  assert.equal(await evaluate('document.querySelector(".mobile-work-inbox .inbox-detail article a").getAttribute("href")'),'/support');
  await evaluate(`(()=>{const e=document.querySelector('.agent-composer input');e.value='我的待补充事项';e.dispatchEvent(new Event('input',{bubbles:true}));document.querySelector('.agent-composer').requestSubmit()})()`);
  await until('document.querySelector(".mobile-work-inbox .inbox-group-list").textContent.includes("收款资料待补充")');
  assert.equal(await evaluate('window.__inboxSources.filter(s=>!s.closed).length'),1);
  assert.ok((await evaluate('window.__businessRequests')).every(r=>r.method==='GET'),'mobile reminder causes a business mutation');
  console.log('PASS '+role+' native mobile agent: shared badge, categories, own-scope tasks, native support link, natural input and one connection');
 }finally{await send('Page.removeScriptToEvaluateOnNewDocument',{identifier})}
}
async function unifiedInbox(){
 await until('document.querySelector(".work-inbox-entry .todo-badge")?.textContent==="32"');
 const badge=await evaluate(`(()=>{const e=document.querySelector('.work-inbox-entry .todo-badge'),s=getComputedStyle(e),r=e.getBoundingClientRect(),p=e.parentElement.getBoundingClientRect();return {color:s.color,background:s.backgroundColor,text:e.textContent,height:r.height,within:r.right<=p.right+3&&r.top>=p.top-4}})()`);
 assert.equal(badge.color,'rgb(255, 255, 255)');assert.equal(badge.background,'rgb(201, 45, 59)');assert.ok(badge.within,'badge not anchored to button');assert.equal(badge.height,22);
 await evaluate(`document.querySelector('.work-inbox-entry').click()`);await until('!!document.querySelector(".work-inbox-panel .inbox-groups button")');
 assert.equal(await evaluate('document.querySelectorAll(".inbox-categories button").length'),6);
 await evaluate(`Array.from(document.querySelectorAll('.inbox-groups button')).find(b=>b.innerText.includes('维修退换待受理')).click()`);
 await until('document.querySelectorAll(".inbox-details article").length===12');
 await click('下一页','document.querySelector(".inbox-details")');await until('document.querySelectorAll(".inbox-details article").length===2');
 assert.equal(await evaluate('document.querySelector(".work-inbox-entry .todo-badge").textContent'),'32','reading cleared todo');
 await evaluate(`document.querySelector('.system-agent-orb').click()`);await until('!!document.querySelector(".system-agent-dock textarea")');
 await evaluate(`(()=>{const t=document.querySelector('.system-agent-dock textarea');t.value='看看维修待接单';t.dispatchEvent(new Event('input',{bubbles:true}))})()`);
 await click('发送','document.querySelector(".system-agent-dock")');await until('!!document.querySelector(".system-agent-drawer .work-inbox-panel")');
 assert.ok(await evaluate(`document.querySelector('.system-agent-drawer .inbox-groups').innerText.includes('维修退换待受理')`));
 assert.ok(!await evaluate(`document.querySelector('.system-agent-drawer .inbox-groups').innerText.includes('客户收款确认')`),'agent category/department filter failed');
 const geometry=await evaluate(`(()=>{const d=document.querySelector('.system-agent-drawer'),p=d.querySelector('.work-inbox-panel').getBoundingClientRect(),f=d.querySelector(':scope > footer').getBoundingClientRect(),n=d.querySelector('.agent-inbox-tabs').getBoundingClientRect();return {panel:p.height,footer:f.bottom,height:innerHeight,tabs:n.height}})()`);assert.ok(geometry.panel>=120&&geometry.footer<=geometry.height+2&&geometry.tabs<90,'drawer panel/footer overflow '+JSON.stringify(geometry));
 assert.equal(await evaluate('window.__inboxSources.filter(s=>!s.closed&&s.url.includes("work/inbox")).length'),1,'each component created a connection');
 await evaluate(`(()=>{window.__inboxState.groups.find(g=>g.key==='repair_accept').count=0;window.__inboxState.total=18;window.__inboxState.version='mock-inbox-2';window.__inboxSources.filter(s=>!s.closed).forEach(s=>s.emit('snapshot',window.__inboxState))})()`);
 await until('document.querySelector(".work-inbox-entry .todo-badge")?.textContent==="18"');
 await until('!document.querySelector(".system-agent-drawer .inbox-groups button")');
 await evaluate(`(()=>{window.__inboxState.stale=true;window.__inboxState.unavailable=['finance'];window.__inboxState.version='mock-inbox-3';window.__inboxSources.filter(s=>!s.closed).forEach(s=>s.emit('snapshot',window.__inboxState))})()`);
 await until('!!document.querySelector(".system-agent-drawer .inbox-error")');
 assert.equal(await evaluate('document.querySelectorAll(".work-inbox-entry .todo-badge").length'),0,'unknown count shown as trusted badge');
 assert.ok((await evaluate('window.__businessRequests')).every(r=>r.method==='GET'),'viewing reminders caused a business mutation');
 console.log('PASS unified inbox: shared red/white corner badge, five categories, department filtering, server pagination, agent drawer intent, one shared stream, live count/zero/failure, read-only behavior');
}
async function compactReceiptLayout(){
 const layout=await evaluate(`(()=>{const row=document.querySelector('.customer-receipts-page tbody tr');return {cell:getComputedStyle(row.cells[2]).fontSize,button:getComputedStyle(row.querySelector('button')).fontSize,height:Math.round(row.getBoundingClientRect().height),reviewWidth:Math.round(row.querySelector('.receipt-review-button')?.getBoundingClientRect().width||0)}})()`);
 assert.equal(layout.cell,'18px');assert.equal(layout.button,'18px');assert.ok(layout.height<=120,'receipt row too tall '+JSON.stringify(layout));assert.ok(layout.reviewWidth<260,'review button stretches full row');console.log('PASS compact receipts 18px '+JSON.stringify(layout));
}
async function policySelf(){
 await until('!!document.querySelector(".receipt-review-reason")');
 assert.equal(await evaluate('document.querySelector(".receipt-review-button").disabled'),true);
 await compactReceiptLayout();
 await evaluate('window.__financeRequireDistinct=false;window.dispatchEvent(new Event("system-config-updated"))');
 await until('!document.querySelector(".receipt-review-button").disabled');
 await rowAction('TEST-R-01','核实 / 审核入账');await fill('对外审核说明','同一有权人员兼岗审核');await evaluate('document.querySelector(".sb-dialog input[type=checkbox]").click()');
 await evaluate('window.__financeRequireDistinct=true;window.dispatchEvent(new Event("system-config-updated"))');
 await until('document.querySelector(".sb-dialog button[type=submit]").disabled');
 assert.ok(!(await evaluate('window.__businessRequests')).some(r=>r.p.endsWith('/review')));
 await evaluate('window.__financeRequireDistinct=false;window.dispatchEvent(new Event("system-config-updated"))');await until('!document.querySelector(".sb-dialog button[type=submit]").disabled');await submit();await until('!document.querySelector(".sb-dialog")');
 await until('document.querySelector(".customer-receipts-page tbody tr").innerText.includes("财务已入账")');
 const req=await evaluate('window.__businessRequests.find(r=>r.p.endsWith("/review"))');assert.ok(!('require_distinct_reviewer' in req.body),'browser must not decide policy');
 await evaluate('window.__financePolicyError=true;window.dispatchEvent(new Event("system-config-updated"))');await until('document.querySelector(".receipt-policy-status").innerText.includes("配置读取失败")');assert.ok(await evaluate('Array.from(document.querySelectorAll(".receipt-review-button")).every(b=>b.disabled)'));
 console.log('PASS self review follows live setting, stale dialog blocked, invalid policy fail closed');
}
async function policyReadonly(){
 await evaluate('window.__financeRequireDistinct=false;window.dispatchEvent(new Event("system-config-updated"))');await delay(300);await blockedApproval();console.log('PASS permission-only mode does not grant review permission');
}
async function settingsSwitch(){
 await compactReceiptLayout();
 await evaluate('document.querySelector(".receipt-policy-link").click()');await until('!!document.querySelector("#finance-review-policy input")&&!document.querySelector("#finance-review-policy button").disabled');
 assert.equal(await evaluate('document.querySelector("#finance-review-policy input").checked'),true);
 await evaluate('document.querySelector("#finance-review-policy input").click()');await click('保存审核规则','document.querySelector("#finance-review-policy")');await until('window.__businessRequests.some(r=>r.p==="/api/v1/system/settings"&&r.method==="PUT")');
 const req=await evaluate('window.__businessRequests.find(r=>r.p==="/api/v1/system/settings"&&r.method==="PUT")');assert.deepEqual(req.body.settings,[{key:'finance_require_distinct_reviewer',value:'false'}]);assert.ok(!(await evaluate('window.__businessRequests')).some(r=>r.p.endsWith('/review')),'configuration cannot post receipts');console.log('PASS administrator settings checkbox, isolated setting save');
}
async function ops(){for(const [a,n] of [['接单','已接收申请'],['开始处理','正在联调'],['提交处理结果','设备已连接，待客户确认']]){await rowAction('测试问题01',a);await fill(a==='提交处理结果'?'处理结果':'补充 / 处理说明',n);await submit();await until('!document.querySelector(".sb-dialog")')}assert.ok(await evaluate('document.querySelector(".sales-business tbody tr").innerText.includes("待确认完成")'));assert.ok(!await evaluate('document.querySelector(".sales-business tbody tr").innerText.includes("确认已解决")'))}
async function requester(){await rowAction('测试问题01','确认已解决');await fill('确认说明','确认已经恢复使用');await submit();await until('!document.querySelector(".sb-dialog")');await rowAction('测试问题01','未解决 / 重新打开');await fill('补充 / 处理说明','问题再次出现');await submit();await until('!document.querySelector(".sb-dialog")');await click('新建协助申请');await fill('问题标题','新增安装咨询');await fill('问题描述','尚未建直播间，需要帮助安装');await fill('客户联系人','测试联系人');await fill('联系电话','13800138000');await submit();await until('!document.querySelector(".sb-dialog")');const r=await evaluate('window.__businessRequests.find(r=>r.method==="POST"&&r.p==="/api/v1/service/tickets")');assert.equal(r.body.tenant_id,601);assert.ok(!('capabilities' in r.body)&&!('assigned_user_id' in r.body))}
async function money(){await until('document.querySelectorAll(".sales-business tbody tr").length===12');assert.ok(await evaluate('document.querySelector(".sales-business tbody").innerText.includes("模拟支付")'));await click('下一页','document.querySelector(".sales-business .pagination-bar")');await until('document.querySelectorAll(".sales-business tbody tr").length===2');assert.ok(!(await evaluate('window.__businessRequests')).some(r=>r.method!=='GET'))}
async function mobile(){await until('document.querySelectorAll(".support-page article").length>=12');await click('下一页','document.querySelector(".support-page")');await until('document.querySelectorAll(".support-page > article").length===2');await click('表格','document.querySelector(".support-page")');await until('document.querySelectorAll(".support-page tbody tr").length===2');await click('新建协助申请','document.querySelector(".support-page")');await fill('问题标题','手机端安装咨询','input','document.querySelector(".support-page")');await fill('问题描述','外勤提交安装需求','input','document.querySelector(".support-page")');await fill('联系人','测试联系人','input','document.querySelector(".support-page")');await fill('联系电话','13800138000','input','document.querySelector(".support-page")');await submit('document.querySelector(".support-page")');await until('document.querySelector(".notice")?.innerText.includes("申请已提交")')}
(async()=>{await fs.mkdir(profile,{recursive:true});chrome=spawn('C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',['--headless=new','--disable-gpu','--no-first-run','--no-default-browser-check','--remote-debugging-port=0','--user-data-dir='+profile,'about:blank'],{stdio:'ignore'});let port;for(let i=0;i<70;i++){try{port=(await fs.readFile(path.join(profile,'DevToolsActivePort'),'utf8')).split(/\r?\n/)[0];break}catch{}await delay(150)}if(!port)throw Error('test Chrome failed');const tabs=await(await fetch('http://127.0.0.1:'+port+'/json/list')).json();ws=new WebSocket(tabs.find(t=>t.type==='page').webSocketDebuggerUrl);ws.addEventListener('message',e=>{const r=JSON.parse(e.data);if(r.method==='Runtime.exceptionThrown')exceptions.push({text:r.params.exceptionDetails.text,description:r.params.exceptionDetails.exception?.description,url:r.params.exceptionDetails.url,stack:r.params.exceptionDetails.stackTrace});const p=pending.get(r.id);if(p){clearTimeout(p.timer);pending.delete(r.id);r.error?p.reject(Error(r.error.message)):p.resolve(r.result)}});await new Promise((resolve,reject)=>{ws.addEventListener('open',resolve,{once:true});ws.addEventListener('error',reject,{once:true})});await send('Page.enable');await send('Runtime.enable');await send('Emulation.setDeviceMetricsOverride',{width:1440,height:1000,deviceScaleFactor:1,mobile:false});const base='http://127.0.0.1:5173';if(process.argv.includes('--customer-inbox-removal')){await run('customer',base+'/support',terminalDesktopWithoutInbox);await run('customer',base+'/support',requester);await run('admin',base+'/staff/finance/receipts',unifiedInbox);await send('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});await terminalCustomerMobile();await run('customer','http://customer.localhost:5174/support',mobile);await mobileInbox('seller','http://sales.localhost:5175/agent');assert.deepEqual(exceptions,[]);console.log('PASS customer inbox removal and preserved staff/sales/support business contracts; mocked APIs only');return}if(process.argv.includes('--work-inbox')){await run('admin',base+'/staff/finance/receipts',unifiedInbox);await send('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});await terminalCustomerMobile();await mobileInbox('seller','http://sales.localhost:5175/agent');assert.deepEqual(exceptions,[]);return}if(process.argv.includes('--receipt-button-states')){await run('finance',base+'/staff/finance/receipts',receiptButtonStates);assert.deepEqual(exceptions,[]);return}await run('finance',base+'/staff/finance/receipts',receiptButtonStates);await run('seller',base+'/sales/receipts?tenant=601',receiptSeller);await run('finance',base+'/staff/finance/receipts',finance);await run('admin',base+'/staff/finance/receipts',approvalDiscovery);await run('admin',base+'/staff/finance/receipts',financeEntryNavigation);await run('admin',base+'/staff/finance/receipts',overviewNavigation);await run('finance-self',base+'/staff/finance/receipts',blockedApproval);await run('finance-readonly',base+'/staff/finance/receipts',blockedApproval);await run('finance-self',base+'/staff/finance/receipts',policySelf);await run('finance-readonly',base+'/staff/finance/receipts',policyReadonly);await run('admin',base+'/staff/finance/receipts',settingsSwitch);await run('ops',base+'/operations/support',ops);await run('seller',base+'/sales/support?tenant=601',requester);await run('customer',base+'/support',requester);await run('seller',base+'/sales/customers/601/money',money);await send('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});await mobileOverview('customer','http://customer.localhost:5174/');await mobileOverview('seller','http://sales.localhost:5175/');await run('customer','http://customer.localhost:5174/support',mobile);await run('seller','http://sales.localhost:5175/support?tenant=601',mobile);assert.deepEqual(exceptions,[]);console.log('PASS ALL customer finance/support browser contracts. All APIs mocked, no live money or accounts changed.')})().catch(e=>{console.error(e.stack);process.exitCode=1}).finally(async()=>{for(const p of pending.values())clearTimeout(p.timer);if(ws)ws.close();if(chrome?.pid)spawnSync('taskkill.exe',['/PID',String(chrome.pid),'/T','/F'],{stdio:'ignore'});await delay(400);await fs.rm(profile,{recursive:true,force:true,maxRetries:3,retryDelay:300}).catch(()=>{})});
