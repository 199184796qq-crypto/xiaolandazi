// Fixed shared header regression: real Vue routes in isolated headless Chrome.
// All API/EventSource traffic is mocked. No real session or business writes.
const fs = require('node:fs/promises');
const path = require('node:path');
const { spawn, spawnSync } = require('node:child_process');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const profile = path.join(root, 'data/tmp/global-topbar-' + process.pid);
const delay = ms => new Promise(r => setTimeout(r, ms));
let chrome, ws, sequence = 0;
const pending = new Map(), exceptions = [];
function cdp(method, params = {}) {
  return new Promise((resolve, reject) => {
    const id = ++sequence;
    const timer = setTimeout(() => { pending.delete(id); reject(Error('CDP timeout ' + method)); }, 18000);
    pending.set(id, { resolve, reject, timer });
    ws.send(JSON.stringify({ id, method, params }));
  });
}
async function evaluate(expression) {
  const r = await cdp('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
  if (r.exceptionDetails) throw Error(r.exceptionDetails.exception?.description || r.exceptionDetails.text);
  return r.result.value;
}
async function until(expression) {
  for (let n = 0; n < 100; n++) {
    try { if (await evaluate(expression)) return; }
    catch (e) { if (!/context|navigated/i.test(e.message)) throw e; }
    await delay(100);
  }
  throw Error('UI condition failed: ' + expression + '\n' + await evaluate('document.body.innerText.slice(-2000)'));
}
function installMock(config) {
  const original = window.fetch.bind(window);
  window.__headerRequests = [];
  const customer = config.role === 'customer';
  const actor = { user_id: customer ? 501 : 101, tenant_id: customer ? 601 : null,
    username: 'header-test', display_name: '顶部测试账号', role: config.role,
    phone: '13800138000', province: '四川省', city: '南充市', district: '顺庆区', must_change_password: false };
  const permissions = config.permissions || [];
  const access = { is_super_admin: config.role === 'platform_admin', primary_group_id: 10,
    primary_group_code: config.group || '', primary_group_name: config.label,
    role_codes: [], permissions, permission_scopes: Object.fromEntries(permissions.map(p => [p, 'all'])),
    permission_group_ids: {}, group_ids: [10], managed_group_ids: [] };
  const bootstrap = { actor, staff_access: customer ? null : access, permissions, rooms: [], settings: {}, capabilities: [] };
  const snapshot = { groups: [], total: 0, version: 'header-mock', as_of: '2026-09-24T12:00:00Z', stale: false, unavailable: [] };
  window.EventSource = class extends EventTarget {
    constructor(url) { super(); this.url = url; this.closed = false; setTimeout(() => { if (!this.closed) this.dispatchEvent(new MessageEvent('snapshot', { data: JSON.stringify(snapshot) })); }, 20); }
    close() { this.closed = true; }
  };
  window.fetch = async (input, init = {}) => {
    const url = new URL(typeof input === 'string' ? input : input.url, location.href);
    if (!url.pathname.startsWith('/api/')) return original(input, init);
    const method = init.method || (typeof input === 'object' && input.method) || 'GET';
    window.__headerRequests.push({ path: url.pathname, method });
    if (method !== 'GET') return new Response('{"error":"No writes in header tests"}', { status: 405 });
    let data = { items: [], total: 0, total_pages: 1, open_total: 0, page: 1, page_size: 12,
      capabilities: [], departments: [], commands: [], tools: [], permissions: [], customers: [], tasks: [] };
    const p = url.pathname;
    if (p === '/api/v1/bootstrap') data = bootstrap;
    else if (p === '/api/v1/account') data = { profile: actor, quota: { total_seconds: 36000 }, membership: null };
    else if (p === '/api/v1/system/public-config') data = { site_name: '测试系统', client_agent_name: '小蓝直播搭子', internal_agent_name: '小蓝工作搭子' };
    else if (p === '/api/v1/work/inbox') data = snapshot;
    else if (p === '/api/v1/staff/finance/review-policy') data = { require_distinct_reviewer: true };
    else if (p === '/api/v1/customer-business/receipts') data = { items: [{ id: 1, receipt_no: 'TOPBAR-001', tenant_id: 601,
      customer_name: '测试客户', amount_cents: 30000, channel: 'bank_transfer', purpose: 'recharge', status: 'posted',
      review_note: '模拟记录', version: 1, created_at: '2026-09-24T10:00:00Z', posted_at: '2026-09-24T11:00:00Z' }],
      page: 1, page_size: 12, total: 1, total_pages: 1 };
    return new Response(JSON.stringify(data), { status: 200, headers: { 'Content-Type': 'application/json' } });
  };
}
const cases = [
  { label: '终端商城', role: 'customer', path: '/shop' },
  { label: '终端收款', role: 'customer', path: '/finance/receipts' },
  { label: '系统管理', role: 'platform_admin', path: '/staff' },
  { label: '管理部', role: 'staff', group: 'management', permissions: ['system.architecture.view', 'staff.group.view'], path: '/staff' },
  { label: '财务部', role: 'staff', group: 'finance', permissions: ['finance.dashboard.view', 'finance.recharge.approve'], path: '/staff/finance/receipts' },
  { label: '财务首页', role: 'staff', group: 'finance', permissions: ['finance.dashboard.view'], path: '/staff/finance' },
  { label: '销售部', role: 'sales_staff', group: 'sales', permissions: ['sales.customer.view_assigned'], path: '/sales/support' },
  { label: '营销运维', role: 'staff', group: 'live_operations', permissions: ['liveops.configure', 'liveops.view_all'], path: '/operations/live' },
  { label: '运维工单', role: 'staff', group: 'live_operations', permissions: ['liveops.configure'], path: '/operations/support' },
  { label: '仓储', role: 'staff', group: 'warehouse_after_sales', permissions: ['inventory.view', 'inventory.manage'], path: '/resources/inventory' },
  { label: '维修售后', role: 'staff', group: 'warehouse_after_sales', permissions: ['inventory.view', 'inventory.after_sales.view', 'inventory.after_sales.manage'], path: '/staff/after-sales' },
  { label: '物流', role: 'staff', group: 'warehouse_after_sales', permissions: ['logistics.view', 'logistics.manage'], path: '/resources/logistics' },
];
async function measure() {
  return evaluate(`(()=>{const h=document.querySelector('.global-topbar'),r=h.getBoundingClientRect(),s=getComputedStyle(h),m=document.querySelector('.main-area').getBoundingClientRect(),c=document.querySelector('.page-content').getBoundingClientRect();return{y:r.y,x:r.x,width:r.width,height:r.height,right:r.right,bottom:r.bottom,position:s.position,background:s.backgroundColor,transform:s.transform,shadow:s.boxShadow,mainX:m.x,contentY:c.y,scrollY:window.scrollY,viewport:innerWidth,controls:Array.from(h.querySelectorAll('.account-chip,.logout-button,.customer-membership-trigger')).map(e=>{const a=e.getBoundingClientRect();return {x:a.x,y:a.y,right:a.right,bottom:a.bottom}}),onTop:!!document.elementFromPoint(r.left+Math.min(30,r.width/2),r.top+10)?.closest('.global-topbar')}})()`);
}
async function scrollTo(y) {
  await evaluate(`window.scrollTo({top:${y},left:0,behavior:'instant'})`);
  await delay(100);
}
async function verifyScroll(config, width = 1440) {
  await cdp('Emulation.setDeviceMetricsOverride', { width, height: 1000, deviceScaleFactor: 1, mobile: false });
  await scrollTo(0);
  const before = await measure();
  assert.equal(before.position, 'sticky', config.label);
  assert.ok(Math.abs(before.y) < 1 && Math.abs(before.x - before.mainX) < 1, JSON.stringify(before));
  assert.ok(before.contentY >= before.bottom - 1, 'Initial page content obscured: ' + JSON.stringify(before));
  assert.ok(before.right <= width + 1, 'Header exceeds viewport: ' + JSON.stringify(before));
  for (const c of before.controls) assert.ok(c.x >= before.x - 1 && c.right <= width + 1 && c.bottom <= before.bottom + 1, 'Header control clipped: ' + JSON.stringify(before));
  for (const y of [450, 1300, 99999, 0]) {
    await scrollTo(y);
    const after = await measure();
    assert.ok(Math.abs(after.y - before.y) < 1, config.label + ' moved at ' + y + ': ' + JSON.stringify(after));
    for (const key of ['height','width','background','transform','shadow']) assert.equal(after[key], before[key], config.label + ' scroll changed ' + key);
    assert.ok(after.onTop, config.label + ' header is covered by page content');
    if (y > 0) assert.ok(after.scrollY > 100, 'Fixture did not actually scroll');
  }
  console.log('PASS ' + config.label + ' ' + config.path + ' @' + width + ': pinned full bar, content scroll, stable size/style, controls visible');
}
async function openCase(config) {
  const { identifier } = await cdp('Page.addScriptToEvaluateOnNewDocument', { source: '(' + installMock.toString() + ')(' + JSON.stringify(config) + ')' });
  try {
    await cdp('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });
    await cdp('Page.navigate', { url: 'http://127.0.0.1:5173' + config.path });
    await until('!!document.querySelector(".global-topbar") && !!document.querySelector(".page-content > div") && location.pathname === ' + JSON.stringify(config.path));
    await evaluate('document.fonts.ready');
    await delay(200);
    // Ensure even an empty department hub has enough real document scroll range.
    // The shared shell and business page remain unchanged; fixture is browser-only.
    await evaluate(`(()=>{const f=document.createElement('div');f.dataset.headerScrollFixture='true';f.style.height='3200px';document.querySelector('.page-content').appendChild(f)})()`);
    await verifyScroll(config);
    if (['终端商城','财务部'].includes(config.label)) {
      for (const width of [1920, 1280, 820, 390]) await verifyScroll(config, width);
      await cdp('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });
    }
    if (config.label === '终端商城') {
      await scrollTo(900);
      await evaluate('document.querySelector(".customer-membership-trigger").click()');
      await until('!!document.querySelector(".membership-center-backdrop")');
      const overlay = await evaluate(`(()=>{const h=document.querySelector('.global-topbar').getBoundingClientRect(),e=document.elementFromPoint(h.x+30,10);return {onTop:!!e?.closest('.membership-center-backdrop'),hit:e?.className}})()`);
      assert.ok(overlay.onTop, 'Membership overlay behind header: ' + JSON.stringify(overlay));
      await evaluate('document.querySelector(".membership-center-backdrop").click()');
      await until('!document.querySelector(".membership-center-backdrop")');
      console.log('PASS customer membership dialog opens above pinned header while scrolled');
    }
    assert.ok((await evaluate('window.__headerRequests')).every(r => r.method === 'GET'), 'Unexpected write in header test');
    if (config.label === '财务部') {
      await scrollTo(900);
      await fs.mkdir(path.join(root,'data/tmp/header-proof'), { recursive: true });
      const image = await cdp('Page.captureScreenshot', { format: 'png' });
      await fs.writeFile(path.join(root,'data/tmp/header-proof/finance-scrolled.png'), Buffer.from(image.data,'base64'));
    }
  } finally { await cdp('Page.removeScriptToEvaluateOnNewDocument', { identifier }); }
}
(async () => {
  await fs.mkdir(profile, { recursive: true });
  chrome = spawn('C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe', ['--headless=new','--disable-gpu','--no-first-run','--no-default-browser-check','--remote-debugging-port=0','--user-data-dir='+profile,'about:blank'], { stdio:'ignore' });
  let port;
  for(let n=0;n<70;n++){try{port=(await fs.readFile(path.join(profile,'DevToolsActivePort'),'utf8')).split(/\r?\n/)[0];break}catch{}await delay(150)}
  if(!port)throw Error('Isolated Chrome failed to start');
  const tabs=await(await fetch('http://127.0.0.1:'+port+'/json/list')).json();
  ws=new WebSocket(tabs.find(t=>t.type==='page').webSocketDebuggerUrl);
  ws.addEventListener('message',e=>{const r=JSON.parse(e.data);if(r.method==='Runtime.exceptionThrown')exceptions.push(r.params.exceptionDetails.exception?.description||r.params.exceptionDetails.text);const p=pending.get(r.id);if(p){clearTimeout(p.timer);pending.delete(r.id);r.error?p.reject(Error(r.error.message)):p.resolve(r.result)}});
  await new Promise((resolve,reject)=>{ws.addEventListener('open',resolve,{once:true});ws.addEventListener('error',reject,{once:true})});
  await cdp('Page.enable');await cdp('Runtime.enable');
  for(const config of cases)await openCase(config);
  assert.deepEqual(exceptions, [], 'Browser runtime errors');
  console.log('PASS ALL shared topbar tests: terminal + all existing department shells, real scrolling, responsive viewports; APIs mocked, no business data changed.');
})().catch(e=>{console.error(e.stack);process.exitCode=1}).finally(async()=>{
  for(const p of pending.values())clearTimeout(p.timer);
  ws?.close();
  if(chrome?.pid)spawnSync('taskkill.exe',['/PID',String(chrome.pid),'/T','/F'],{stdio:'ignore'});
  await delay(400);await fs.rm(profile,{recursive:true,force:true,maxRetries:3,retryDelay:300}).catch(()=>{});
});
