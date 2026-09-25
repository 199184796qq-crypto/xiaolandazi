// Browser-only smoke test. All /api/ requests are mocked before the app starts.
// It never logs into, writes to, or reads customer data from the live service.
const { spawn, spawnSync } = require('node:child_process');
const fs = require('node:fs/promises');
const path = require('node:path');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const profile = path.join(root, 'data', 'tmp', 'sales-ui-' + process.pid);
const chrome = process.env.CHROME_PATH || 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const delay = ms => new Promise(r => setTimeout(r, ms));
let child, socket;
let commandId = 0;
const waiting = new Map();
function send(method, params = {}) {
  return new Promise((resolve, reject) => {
    const id = ++commandId;
    const timer = setTimeout(() => { waiting.delete(id); reject(new Error('CDP timeout: ' + method)); }, 15000);
    waiting.set(id, { resolve, reject, timer });
    socket.send(JSON.stringify({ id, method, params }));
  });
}
async function evaluate(expression) {
  const r = await send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true });
  if (r.exceptionDetails) throw new Error(r.exceptionDetails.exception?.description || r.exceptionDetails.text);
  return r.result.value;
}
async function until(expression) {
  for (let n=0; n<60; n++) { const r=await evaluate(expression); if(r)return r; await delay(200); }
  throw new Error('UI condition not met: '+expression+'\n'+await evaluate('document.body.innerText.slice(0,1600)'));
}
function mockScript(role) {
 return `(() => {
 const real=window.fetch.bind(window); window.__salesTestRequests=[];
 const role=${JSON.stringify(role)};
 const lead={id:1,lead_no:'TEST-LEAD-001',owner_sales_staff_id:1,business_name:'测试商家',contact_name:'测试联系人',phone:'13800138000',wechat:'',email:'',industry_name:'餐饮',province:'',city:'',district:'',address:'测试地址',source_type:'self_developed',stage:'new',status:'open',lost_reason:'',created_at:'2026-09-24T04:00:00Z',updated_at:'2026-09-24T04:00:00Z'};
 const permissions=role==='ops'?['liveops.configure']:role==='manager'?['sales.view_all','sales.assignment.manage']:['sales.customer.view_assigned'];
 const bootstrap={actor:{user_id:101,username:'sales_ui_test',display_name:'测试账号',role:role==='seller'?'sales_staff':'staff',must_change_password:false},staff_access:{is_super_admin:false,primary_group_id:10,primary_group_code:role==='ops'?'live_operations':'sales',primary_group_name:role==='ops'?'营销运维部':'销售部',role_codes:[role==='ops'?'live_operations_staff':role==='manager'?'sales_manager':'sales_staff'],permissions,permission_scopes:Object.fromEntries(permissions.map(x=>[x,'group'])),permission_group_ids:{},group_ids:[10],managed_group_ids:role==='manager'?[10]:[]},permissions,rooms:[],capabilities:[],settings:{}};
 const page=(items)=>({items,total:items.length,page:1,page_size:12,total_pages:1});
 window.fetch=async (input,init={})=>{
  const url=new URL(typeof input==='string'?input:input.url,location.href);
  if(!url.pathname.startsWith('/api/'))return real(input,init);
  const method=init.method||'GET',body=init.body?JSON.parse(init.body):null;
  window.__salesTestRequests.push({path:url.pathname,method,body});
  let data={items:[],capabilities:[],commands:[],departments:[],tools:[],navigation:[],sections:[],permissions:[]};
  if(url.pathname==='/api/v1/bootstrap')data=bootstrap;
  else if(url.pathname==='/api/v1/system/public-config')data={site_name:'测试系统',internal_agent_name:'小蓝工作搭子',client_agent_name:'小蓝直播搭子'};
  else if(url.pathname==='/api/v1/sales/leads')data={...page([lead]),summary:{total_count:1,open_count:1,visit_due_count:0,followup_due_count:0,won_count:0,lost_count:0}};
  else if(url.pathname==='/api/v1/sales/leads/1'){if(method==='PUT')Object.assign(lead,body);data=lead;}
  else if(url.pathname.endsWith('/activities'))data=page([{id:1,lead_id:1,sales_staff_id:1,sales_display_name:'原销售',activity_type:'created',outcome:'',content:'建立测试顾客',occurred_at:lead.created_at,created_at:lead.created_at}]);
  else if(url.pathname==='/api/v1/liveops/customer-handoffs')data={...page([{id:1,handoff_no:'TEST-HO-001',customer_name:'测试客户',customer_phone:'13800138000',sales_display_name:'测试销售',status:'pending',summary:'请联调设备',accepted_by_name:'',created_at:lead.created_at}]),manager_view:false};
  else if(url.pathname==='/api/v1/admin/sales/handover-people')data=page([{staff_id:1,user_id:101,display_name:'离职销售',username:'former',status:'disabled',customer_count:2,lead_count:3},{staff_id:2,user_id:102,display_name:'接手销售',username:'next',status:'active',customer_count:0,lead_count:0}]);
  else if(url.pathname==='/api/v1/admin/sales/handovers')data=page([]);
  return new Response(JSON.stringify(data),{status:200,headers:{'Content-Type':'application/json'}});
 };
})();`;
}
async function runPage(role, route, title) {
 const {identifier}=await send('Page.addScriptToEvaluateOnNewDocument',{source:mockScript(role)});
 try {
  await send('Page.navigate',{url:'http://127.0.0.1:5173'+route});
  await until('Array.from(document.querySelectorAll("h2")).some(e=>e.textContent.includes('+JSON.stringify(title)+'))');
  await until('document.body.innerText.includes("测试商家") || document.body.innerText.includes("测试客户") || document.body.innerText.includes("离职销售")');
  assert.ok(await evaluate('getComputedStyle(document.querySelector(".sales-business")).fontSize === "16px"'));
  console.log('PASS browser render '+route+' with isolated mock identity '+role);
  if(role==='seller'){
   await evaluate('Array.from(document.querySelectorAll("button")).find(e=>e.textContent.trim()==="编辑 / 改期").click()');
   await until('!!document.querySelector(".sb-dialog form")');
   await evaluate('document.querySelector(".sb-dialog form").dispatchEvent(new Event("submit",{bubbles:true,cancelable:true}))');
   const request=await until('window.__salesTestRequests.find(r=>r.method==="PUT"&&r.path==="/api/v1/sales/leads/1")');
   assert.equal(request.body.business_name,'测试商家');
   for(const forbidden of ['id','status','owner_sales_staff_id','converted_user_id'])assert.ok(!(forbidden in request.body),'unexpected writable field '+forbidden);
   await until('!document.querySelector(".sb-dialog")');
   console.log('PASS browser edit form submits exact editable fields and shows saved state');
  }
 } finally {await send('Page.removeScriptToEvaluateOnNewDocument',{identifier});}
}
(async()=>{
 await fs.mkdir(profile,{recursive:true});
 child=spawn(chrome,['--headless=new','--disable-gpu','--no-first-run','--no-default-browser-check','--remote-debugging-port=0','--user-data-dir='+profile,'about:blank'],{stdio:'ignore'});
 child.on('error',e=>console.error('Chrome spawn failed: '+e.message));
 let port;
 for(let n=0;n<60;n++){try{port=(await fs.readFile(path.join(profile,'DevToolsActivePort'),'utf8')).split(/\r?\n/)[0];break}catch{}await delay(200)}
 if(!port)throw new Error('Isolated Chrome did not start');
 const tabs=await(await fetch('http://127.0.0.1:'+port+'/json/list')).json();const tab=tabs.find(t=>t.type==='page');
 socket=new WebSocket(tab.webSocketDebuggerUrl);
 socket.addEventListener('message',event=>{const m=JSON.parse(event.data);if(!m.id)return;const p=waiting.get(m.id);if(!p)return;clearTimeout(p.timer);waiting.delete(m.id);m.error?p.reject(new Error(m.error.message)):p.resolve(m.result)});
 await new Promise((resolve,reject)=>{socket.addEventListener('open',resolve,{once:true});socket.addEventListener('error',reject,{once:true})});
 await send('Page.enable');await send('Runtime.enable');
 await runPage('seller','/sales/leads','意向顾客');
 await runPage('ops','/operations/live/customer-handoffs','客户交接');
 await runPage('manager','/sales/handovers','销售离职');
 console.log('PASS all new sales business views; all API traffic was mocked, no live data touched.');
})().catch(e=>{console.error(e.stack||e.message);process.exitCode=1}).finally(async()=>{
 if(socket)socket.close();
 if(child?.pid)spawnSync('taskkill.exe',['/PID',String(child.pid),'/T','/F'],{stdio:'ignore'});
 await delay(500);await fs.rm(profile,{recursive:true,force:true,maxRetries:3,retryDelay:300}).catch(()=>{});
});
