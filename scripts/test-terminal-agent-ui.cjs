// Isolated Chrome profile. Every /api/ request and EventSource is mocked.
// Checks the real Vue/Svelte UI; never logs in, posts funds or changes business records.
const fs = require('node:fs/promises');
const path = require('node:path');
const { spawn, spawnSync } = require('node:child_process');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const profile = path.join(root, 'data/tmp/terminal-agent-ui-' + process.pid);
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
let chrome, ws, sequence = 0;
const pending = new Map(), exceptions = [];
function cdp(method, params = {}) {
  return new Promise((resolve, reject) => {
    const id = ++sequence;
    const timer = setTimeout(() => { pending.delete(id); reject(Error('CDP timeout: ' + method)); }, 15000);
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
    catch (e) { if (!/Execution context was destroyed|Cannot find context|Inspected target navigated/.test(e.message)) throw e; }
    await delay(100);
  }
  throw Error('Condition failed: ' + expression + '\n' + await evaluate('document.body.innerText.slice(-2500)'));
}
function installMock(role) {
  const realFetch = window.fetch.bind(window);
  window.__uiRequests = [];
  window.__uiStreams = [];
  const customer = role === 'customer', seller = role === 'sales_staff', dual = role === 'dual';
  const perms = seller ? ['sales.customer.view_assigned'] : dual ? ['finance.dashboard.view'] : ['finance.dashboard.view', 'finance.recharge.approve'];
  const actor = { user_id: customer ? 501 : seller ? 101 : 900, tenant_id: customer ? 601 : null,
    username: 'ui-test-only', display_name: '测试账号', role: dual ? 'staff' : role, phone: '13800138000',
    province: '四川省', city: '南充市', district: '顺庆区', must_change_password: false };
  const bootstrap = { actor, staff_access: customer ? null : { is_super_admin: role === 'platform_admin',
    primary_group_id: 10, primary_group_code: seller ? 'sales' : dual ? 'warehouse_after_sales' : 'finance', primary_group_name: seller ? '销售部' : '财务部',
    role_codes: [], permissions: perms, permission_scopes: Object.fromEntries(perms.map(p => [p, 'all'])),
    permission_group_ids: {}, group_ids: [10], managed_group_ids: [] }, permissions: perms, rooms: [], settings: {}, capabilities: [] };
  const groups = [{ key: 'receipt_review', topic: 'finance', department: '财务', category: 'review',
    title: '客户收款确认', to: '/staff/finance/receipts', count: 3, due_count: 0, shared_queue: true }];
  window.__uiInbox = { groups, total: 3, version: 'ui-test-1', as_of: '2026-09-24T11:00:00Z', stale: false, unavailable: [] };
  window.EventSource = class extends EventTarget {
    constructor(url) { super(); this.url = url; this.closed = false; window.__uiStreams.push(this);
      setTimeout(() => { if (!this.closed) this.dispatchEvent(new MessageEvent('snapshot', { data: JSON.stringify(window.__uiInbox) })); }, 30); }
    close() { this.closed = true; }
  };
  window.fetch = async (input, init = {}) => {
    const url = new URL(typeof input === 'string' ? input : input.url, location.href);
    if (!url.pathname.startsWith('/api/')) return realFetch(input, init);
    const p = url.pathname, method = init.method || 'GET', body = init.body ? JSON.parse(init.body) : undefined;
    window.__uiRequests.push({ p, method, body, q:url.search });
    let data = { items: [], total: 0, total_pages: 1, page: 1, page_size: 12, capabilities: [], departments: [], commands: [], tools: [], permissions: [] };
    if (p === '/api/v1/bootstrap') data = bootstrap;
    else if (p === '/api/v1/account') data = { profile: actor, quota: { total_seconds: 0 }, membership: null };
    else if (p === '/api/v1/system/public-config') data = { site_name: '测试系统', client_agent_name: '小蓝直播搭子', internal_agent_name: '小蓝工作搭子' };
    else if (p === '/api/v1/work/inbox') data = window.__uiInbox;
    else if (p === '/api/v1/staff/finance') data = { customers: [], tasks: [] };
    else if (p === '/api/v1/staff/finance/review-policy') data = { require_distinct_reviewer: true };
    else if(p==='/api/v1/staff/finance/invitations'){
      const all=Array.from({length:14},(_,i)=>({id:i+1,invite_code:'REF-'+i,inviter_user_id:101,inviter_display_name:'推荐人甲',inviter_username:'ref-a',referred_user_id:501+i,referred_tenant_id:601+i,referred_display_name:'受邀客户'+(i+1),referred_username:'customer'+i,source_type:'referral',bound_at:'2026-09-24T10:00:00Z',confirmed_at:i%2===0?'2026-09-24T11:00:00Z':undefined,confirmed_amount_cents:i%2===0?30000:0}));
      const filtered=all.filter(v=>(!url.searchParams.get('search')||v.referred_display_name.includes(url.searchParams.get('search')))&&(url.searchParams.get('status')!=='confirmed'||v.confirmed_at)&&(url.searchParams.get('status')!=='unconfirmed'||!v.confirmed_at));
      const page=Number(url.searchParams.get('page')||1),size=Number(url.searchParams.get('page_size')||12);data={items:filtered.slice((page-1)*size,page*size),page,page_size:size,total:filtered.length,total_pages:Math.max(1,Math.ceil(filtered.length/size))};
    }
    else if(/^\/api\/v1\/staff\/finance\/invitations\/\d+\/earnings$/.test(p)){
      const page=Number(url.searchParams.get('page')||1),all=Array.from({length:14},(_,i)=>({id:i+1,external_id:'E-'+i,order_id:801,order_no:'ORDER-001',beneficiary_type:'user',beneficiary_id:101,earning_type:'referral_reward',amount_cents:i===0?-1200:1200,currency:'CNY',quota_seconds:0,status:i===0?'reversed':'paid',program_version_id:3,rule_id:4,reversal_of_earning_id:i===0?2:undefined,source_refund_id:i===0?999:undefined,settlement_batch_no:'BATCH-1',settlement_status:'paid',created_at:'2026-09-24T11:00:00Z'}));data={items:all.slice((page-1)*12,page*12),page,page_size:12,total:14,total_pages:2};
    }
    else if(p==='/api/v1/customer-business/receipts')data={items:[{id:1,receipt_no:'RCPT-UI-001',tenant_id:601,customer_name:'客户甲',amount_cents:30000,channel:'bank_transfer',purpose:'recharge',status:'posted',review_note:'收到款项',version:1,created_at:'2026-09-24T10:00:00Z',posted_at:'2026-09-24T11:00:00Z',external_trade_no:'BANK-1',evidence:'测试凭据',payer_name:'客户甲',receiving_account:'测试账户'}],page:1,page_size:12,total:1,total_pages:1};
    else if(p==='/api/v1/customer-business/receipts/1/events')data={items:[{id:3,action:'posted',note:'收到款项',actor_name:'财务甲',created_at:'2026-09-24T11:00:00Z'},{id:2,action:'review_policy',note:'审核规则：不强制分人，按审核权限办理；本次经办/补件与审核为同一人',actor_name:'财务甲',created_at:'2026-09-24T10:59:00Z'},{id:1,action:'submitted',note:'收款资料已提交，等待财务核实',actor_name:'财务甲',created_at:'2026-09-24T10:00:00Z'}],page:1,page_size:12,total:3,total_pages:1};
    else if (p.endsWith('/chat') || p.endsWith('/policy/agent')) {
      await new Promise(resolve => setTimeout(resolve, 450));
      data = { reply: '测试回复：已收到你的问题。', capabilities: ['测试能力'], model: 'mock-only' };
    }
    return new Response(JSON.stringify(data), { status: 200, headers: { 'Content-Type': 'application/json' } });
  };
}
async function withMock(role, url, task) {
  const { identifier } = await cdp('Page.addScriptToEvaluateOnNewDocument', { source: '(' + installMock.toString() + ')(' + JSON.stringify(role) + ')' });
  try { await cdp('Page.navigate', { url }); await until('window.__uiRequests?.some(r=>r.p==="/api/v1/bootstrap")'); await task(); }
  finally { await cdp('Page.removeScriptToEvaluateOnNewDocument', { identifier }); }
}
async function noTerminalInbox() {
  assert.equal(await evaluate('document.querySelectorAll(".work-inbox-entry,.todo-badge,.inbox-badge,.agent-inbox-tabs,.agent-inbox-switch,.work-inbox-panel,.mobile-work-inbox").length'), 0);
  assert.ok(!(await evaluate('window.__uiRequests')).some(r => r.p.startsWith('/api/v1/work/inbox') || r.p.endsWith('/todos')), 'terminal still queries counts');
  assert.equal(await evaluate('window.__uiStreams.filter(s=>s.url.includes("work/inbox")).length'), 0, 'terminal opens inbox stream');
}
async function checkTerminalBrandingAndSend(mobile = false) {
  const root = mobile ? '.agent-page' : '.terminal-agent-drawer';
  const editor = mobile ? '.agent-composer input' : 'footer textarea';
  const button = mobile ? '.agent-composer > button' : 'footer > button';
  async function geometry() {
    return evaluate(`(()=>{const root=document.querySelector(${JSON.stringify(root)}),title=root.querySelector('header h1,header h3'),subtitle=root.querySelector('.terminal-agent-subtitle'),input=root.querySelector(${JSON.stringify(editor)}),button=root.querySelector(${JSON.stringify(button)}),t=title.getBoundingClientRect(),s=subtitle.getBoundingClientRect(),i=input.getBoundingClientRect(),b=button.getBoundingClientRect(),css=getComputedStyle(button);return {english:subtitle.textContent,lang:subtitle.lang,subtitleBelow:s.top-t.bottom,leftDelta:Math.abs(s.left-t.left),centerDelta:Math.abs((i.top+i.bottom)/2-(b.top+b.bottom)/2),radius:[css.borderTopLeftRadius,css.borderTopRightRadius,css.borderBottomRightRadius,css.borderBottomLeftRadius],font:css.fontSize,height:b.height,visible:b.right<=innerWidth+1&&b.bottom<=innerHeight+1,disabled:button.disabled,x:b.left+b.width/2,y:b.top+b.height/2}})()`);
  }
  function check(g, label) {
    assert.equal(g.english, 'XIAOLAN LIVE COMPANION', label);
    assert.equal(g.lang, 'en', label);
    assert.ok(g.subtitleBelow >= 0 && g.subtitleBelow <= 8 && g.leftDelta <= 1, label + ' subtitle position: ' + JSON.stringify(g));
    assert.ok(g.centerDelta <= 1, label + ' send not centered: ' + JSON.stringify(g));
    assert.deepEqual(g.radius, ['12px', '12px', '12px', '12px'], label);
    assert.equal(g.height, 48, label);
    assert.equal(g.font, '18px', label);
    assert.ok(g.visible, label + ' button clipped');
  }
  const initial = await geometry();check(initial, 'empty');assert.equal(initial.disabled, true);
  await evaluate(`(()=>{const e=document.querySelector(${JSON.stringify(root + ' ' + editor)});e.value=${JSON.stringify(mobile ? '测试输入' : '第一行测试\n第二行测试')};e.dispatchEvent(new Event('input',{bubbles:true}))})()`);
  await until(`!document.querySelector(${JSON.stringify(root + ' ' + button)}).disabled`);
  check(await geometry(), 'enabled');
  await evaluate(`document.querySelector(${JSON.stringify(root + ' ' + button)}).focus()`);
  await delay(200);check(await geometry(), 'keyboard focus');
  const beforeHover = await geometry();
  await cdp('Input.dispatchMouseEvent', { type: 'mouseMoved', x: beforeHover.x, y: beforeHover.y });
  await delay(200);check(await geometry(), 'hover');
  if (!mobile) {
    await cdp('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: false });
    await delay(250);check(await geometry(), 'narrow desktop');
    await cdp('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });
    await delay(250);
  }
  await evaluate(`(()=>{const e=document.querySelector(${JSON.stringify(root + ' ' + editor)});e.value='';e.dispatchEvent(new Event('input',{bubbles:true}))})()`);
  await until(`document.querySelector(${JSON.stringify(root + ' ' + button)}).disabled`);
  console.log('PASS ' + (mobile ? 'mobile' : 'desktop') + ' English subtitle below name; 12px rounded send, 48px height, vertical center delta=' + initial.centerDelta + 'px; empty/enabled/hover/keyboard' + (mobile ? '' : '/narrow viewport'));
}
async function desktopCustomer() {
  await until('!!document.querySelector(".system-agent-orb")');
  await evaluate('document.querySelector(".system-agent-orb").click()');
  await until('!!document.querySelector(".system-agent-open")');
  await evaluate('document.querySelector(".system-agent-open").click()');
  await until('!!document.querySelector(".terminal-agent-drawer")');
  await noTerminalInbox();
  assert.equal(await evaluate('document.querySelectorAll(".terminal-agent-drawer > header .section-kicker,.terminal-agent-drawer > header p,.terminal-agent-drawer .system-agent-capabilities,.terminal-agent-drawer .system-agent-context-empty,.terminal-agent-drawer .system-agent-message").length'), 0, 'customer boilerplate remains');
  const geometry = await evaluate(`(()=>{const d=document.querySelector('.terminal-agent-drawer'),h=d.querySelector('header').getBoundingClientRect(),c=d.querySelector('.system-agent-chat').getBoundingClientRect(),f=d.querySelector('footer').getBoundingClientRect();return {headerHeight:h.height,contentHeight:c.height,gap:c.top-h.bottom,footerBottom:f.bottom,viewport:innerHeight,font:getComputedStyle(d.querySelector('textarea')).fontSize}})()`);
  assert.ok(geometry.headerHeight < 100 && geometry.contentHeight > 300 && geometry.gap < 30 && geometry.footerBottom <= geometry.viewport + 2, JSON.stringify(geometry));
  assert.equal(geometry.font, '18px');
  await checkTerminalBrandingAndSend();
  await evaluate(`(async()=>{const t=document.querySelector('.terminal-agent-drawer textarea');t.value='我的待办';t.dispatchEvent(new Event('input',{bubbles:true}));await new Promise(r=>setTimeout(r,20));document.querySelector('.terminal-agent-drawer footer button').click()})()`);
  await until('!!document.querySelector(".terminal-agent-drawer .system-agent-thinking")');
  await until('document.querySelector(".terminal-agent-drawer .system-agent-chat").textContent.includes("测试回复")');
  await noTerminalInbox();
  assert.ok((await evaluate('window.__uiRequests')).some(r => r.p === '/api/v1/client-agent/chat' && r.body.message === '我的待办'));
  // Use a tiny host view for the live-strategy route so this test isolates the real
  // persistent agent from unrelated business-form API fixtures. Routing and agent are real.
  await evaluate(`(async()=>{const router=document.querySelector('#app').__vue_app__.config.globalProperties.$router;router.addRoute({path:'/test-terminal-strategy',name:'live-strategy',component:{render:()=>null}});await router.push('/test-terminal-strategy');window.dispatchEvent(new CustomEvent('system-agent-live-strategy-context',{detail:{room_id:701,mode:'strategy'}}))})()`);
  await until('location.pathname==="/test-terminal-strategy"');
  assert.equal(await evaluate('document.querySelectorAll(".terminal-agent-drawer .system-agent-context-empty,.terminal-agent-drawer .system-agent-capabilities").length'), 0);
  await evaluate(`(async()=>{const t=document.querySelector('.terminal-agent-drawer textarea');t.value='欢迎新来的朋友，语气自然些';t.dispatchEvent(new Event('input',{bubbles:true}));await new Promise(r=>setTimeout(r,20));document.querySelector('.terminal-agent-drawer footer button').click()})()`);
  await until('window.__uiRequests.some(r=>r.method==="POST"&&r.p.includes("/live/rooms/701/"))');
  await until('!document.querySelector(".terminal-agent-drawer .system-agent-thinking")');
  await noTerminalInbox();
  await evaluate(`(async()=>{const router=document.querySelector('#app').__vue_app__.config.globalProperties.$router;await router.push('/work/inbox')})()`);
  await until('location.pathname!=="/work/inbox"');
  await noTerminalInbox();
  console.log('PASS desktop terminal: clean 3-row drawer, 18px, no boilerplate/tabs/counts/stream, chat + thinking, live strategy room context retained, old inbox route blocked');
}
async function internalRetained() {
  await until('document.querySelector(".work-inbox-entry .todo-badge")?.textContent==="3"');
  await evaluate('document.querySelector(".system-agent-orb").click()');
  await until('!!document.querySelector(".system-agent-open")');
  await evaluate('document.querySelector(".system-agent-open").click()');
  await until('!!document.querySelector(".agent-inbox-tabs")');
  assert.equal(await evaluate('document.querySelectorAll(".system-agent-capabilities,.system-agent-drawer > header p").length'),0,'removed internal chrome returned');
  assert.equal(await evaluate('document.querySelectorAll(".system-agent-drawer .terminal-agent-subtitle").length'), 0, 'terminal branding leaked to employee UI');
  await evaluate(`Array.from(document.querySelectorAll('.agent-inbox-tabs button')).find(e=>e.textContent.includes('我的待办')).click()`);
  await until('!!document.querySelector(".work-inbox-panel")');
  assert.equal(await evaluate('window.__uiStreams.filter(s=>!s.closed).length'), 1);
  console.log('PASS internal/sales inbox retained: red/white badge, categories, one stream, original work context');
}
async function mobileCustomer() {
  await until('!!document.querySelector(".agent-composer input")');
  await noTerminalInbox();
  assert.equal(await evaluate('document.querySelectorAll(".agent-header div > span,.chat-bubble").length'), 0);
  assert.equal(await evaluate('getComputedStyle(document.querySelector(".agent-composer input")).fontSize'), '18px');
  await checkTerminalBrandingAndSend(true);
  await evaluate(`(()=>{const t=document.querySelector('.agent-composer input');t.value='我有哪些待办';t.dispatchEvent(new Event('input',{bubbles:true}));document.querySelector('.agent-composer').requestSubmit()})()`);
  await until('document.querySelector(".agent-chat").textContent.includes("测试回复")');
  await noTerminalInbox();
  assert.ok((await evaluate('window.__uiRequests')).some(r => r.p === '/api/v1/client-agent/chat'));
  console.log('PASS customer mobile: direct chat, no inbox connection/entry/badge, 18px, replies preserved');
}
(async () => {
  await fs.mkdir(profile, { recursive: true });
  chrome = spawn('C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe', ['--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check', '--remote-debugging-port=0', '--user-data-dir=' + profile, 'about:blank'], { stdio: 'ignore' });
  let port;
  for (let n = 0; n < 70; n++) { try { port = (await fs.readFile(path.join(profile, 'DevToolsActivePort'), 'utf8')).split(/\r?\n/)[0]; break; } catch {} await delay(150); }
  if (!port) throw Error('Test Chrome failed to start');
  const tabs = await (await fetch('http://127.0.0.1:' + port + '/json/list')).json();
  ws = new WebSocket(tabs.find(t => t.type === 'page').webSocketDebuggerUrl);
  ws.addEventListener('message', e => { const r = JSON.parse(e.data); if (r.method === 'Runtime.exceptionThrown') exceptions.push(r.params.exceptionDetails.exception?.description || r.params.exceptionDetails.text); const p = pending.get(r.id); if (p) { clearTimeout(p.timer); pending.delete(r.id); r.error ? p.reject(Error(r.error.message)) : p.resolve(r.result); } });
  await new Promise((resolve, reject) => { ws.addEventListener('open', resolve, { once: true }); ws.addEventListener('error', reject, { once: true }); });
  await cdp('Page.enable'); await cdp('Runtime.enable');
  await cdp('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });
  await withMock('customer', 'http://127.0.0.1:5173/support', desktopCustomer);
  await withMock('platform_admin', 'http://127.0.0.1:5173/staff/finance/receipts', internalRetained);
  await withMock('sales_staff', 'http://127.0.0.1:5173/sales/support', internalRetained);
  await require('./ui-cleanup-browser-cases.cjs')({cdp,evaluate,until,withMock,assert,delay});
  await cdp('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
  await withMock('customer', 'http://customer.localhost:5174/agent', mobileCustomer);
  assert.deepEqual(exceptions, [], 'browser runtime errors');
  console.log('PASS terminal chat simplification: isolated mocked browser only; no real account or business mutation');
})().catch(e => { console.error(e.stack); process.exitCode = 1; }).finally(async () => {
  for (const p of pending.values()) clearTimeout(p.timer);
  ws?.close();
  if (chrome?.pid) spawnSync('taskkill.exe', ['/PID', String(chrome.pid), '/T', '/F'], { stdio: 'ignore' });
  await delay(400);
  await fs.rm(profile, { recursive: true, force: true, maxRetries: 3, retryDelay: 300 }).catch(() => {});
});
