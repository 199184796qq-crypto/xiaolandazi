// Mobile invitation browser regression. All API calls are mocked and no real account is changed.
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const { spawn, spawnSync } = require('node:child_process');

const root = path.resolve(__dirname, '..');
const profile = path.join(root, 'data/tmp/mobile-invite-ui-' + process.pid);
const origin = 'http://127.0.0.1:5861';
const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
let server;
let chrome;
let ws;
let sequence = 0;
const pending = new Map();
const exceptions = [];

function cdp(method, params = {}) {
  return new Promise((resolve, reject) => {
    const id = ++sequence;
    const timer = setTimeout(() => { pending.delete(id); reject(new Error('CDP timeout: ' + method)); }, 15000);
    pending.set(id, { resolve, reject, timer });
    ws.send(JSON.stringify({ id, method, params }));
  });
}

async function evaluate(expression) {
  const result = await cdp('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
  if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description || result.exceptionDetails.text);
  return result.result.value;
}

async function until(expression) {
  for (let attempt = 0; attempt < 120; attempt += 1) {
    try { if (await evaluate(expression)) return; }
    catch (error) { if (!/Execution context was destroyed|Cannot find context|Inspected target navigated/.test(error.message)) throw error; }
    await delay(100);
  }
  throw new Error('Condition failed: ' + expression + '\n' + await evaluate('document.body.innerText'));
}

function installMock() {
  const realFetch = window.fetch.bind(window);
  window.__inviteRequests = [];
  window.__sharedInvite = null;
  window.__copiedInvite = '';
  window.__inviteActive = true;
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async (value) => { window.__copiedInvite = value; } } });
  Object.defineProperty(navigator, 'share', { configurable: true, value: async (value) => { window.__sharedInvite = value; } });
  const actor = { user_id: 501, tenant_id: 601, username: 'merchant-a', display_name: '商家甲', role: 'customer' };
  const bootstrap = { actor, permissions: [], rooms: [], settings: {}, capabilities: [] };
  window.fetch = async (input, init = {}) => {
    const url = new URL(typeof input === 'string' ? input : input.url, location.href);
    if (!url.pathname.startsWith('/api/')) return realFetch(input, init);
    const method = init.method || 'GET';
    const body = init.body ? JSON.parse(init.body) : undefined;
    window.__inviteRequests.push({ path: url.pathname, method, search: url.search, body });
    let data = {};
    if (url.pathname === '/api/v1/bootstrap') data = bootstrap;
    else if (url.pathname === '/api/v1/system/public-config') data = { site_name: '测试系统', client_agent_name: '小蓝直播搭子' };
    else if (url.pathname === '/api/v1/invitations/dashboard') {
      const page = Number(url.searchParams.get('record_page') || 1);
      const records = Array.from({ length: page === 1 ? 6 : 1 }, (_, index) => {
        const number = page === 1 ? index + 1 : 7;
        return { id: number, invite_code_id: 31, invite_code: 'INVITE88', inviter_user_id: 501, inviter_tenant_id: 601,
          inviter_username: 'merchant-a', inviter_display_name: '商家甲', referred_user_id: 600 + number,
          referred_tenant_id: 700 + number, referred_username: 'friend-' + number, referred_display_name: '受邀商家' + number,
          source_type: 'referral', parent_org_id: 1, parent_org_name: '平台代理', bound_at: '2026-10-03T08:0' + index + ':00Z' };
      });
      data = { my_code: { id: 31, code: 'INVITE88', owner_user_id: 501, owner_tenant_id: 601, owner_role: 'customer',
        owner_username: 'merchant-a', owner_name: '商家甲', status: window.__inviteActive ? 'active' : 'disabled', max_uses: 20,
        used_count: 7, expires_at: '2027-10-03T08:00:00Z', created_at: '2026-01-01T00:00:00Z' },
        codes: [], codes_total: 1, records, records_total: 7, own_referral_count: 7 };
    } else if (url.pathname === '/api/v1/invitations/mine' && method === 'PATCH') {
      window.__inviteActive = body.status === 'active';
      return new Response(null, { status: 204 });
    } else if (url.pathname === '/api/v1/finance/referral-wallet') {
      data = { wallet: { available_balance_cents: 12800, frozen_balance_cents: 3200 }, ledger: [
        { id: 1, wallet_id: 9, external_id: 'R-1', business_type: 'referral_accrual', reference_type: 'earning',
          available_delta_cents: 0, frozen_delta_cents: 3200, available_before_cents: 12800, available_after_cents: 12800,
          frozen_before_cents: 0, frozen_after_cents: 3200, reason: '推荐客户订单返佣', created_at: '2026-10-03T09:00:00Z' },
      ], withdrawals: [] };
    } else if (url.pathname === '/api/v1/auth/invite/INVITE88') {
      data = { code: 'INVITE88', inviter_name: '商家甲', inviter_role: 'customer', source_type: 'referral' };
    } else if (url.pathname === '/api/v1/auth/register' && method === 'POST') data = bootstrap;
    else if (url.pathname === '/api/v1/rooms') data = { items: [], total: 0 };
    else if (url.pathname === '/api/v1/live/devices') data = [];
    else if (url.pathname === '/api/v1/live/quota-summary') data = { active_seconds: 0, active_time_card_seconds: 0, reserve_time_card_seconds: 0, reserve_time_card_count: 0, sources: [], time_cards: [], active_billing_rooms: [] };
    return new Response(JSON.stringify(data), { status: 200, headers: { 'Content-Type': 'application/json' } });
  };
}

(async () => {
  await fs.mkdir(profile, { recursive: true });
  server = spawn(process.execPath, ['node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', '5861', '--strictPort'], {
    cwd: path.join(root, 'customer-mobile'), stdio: 'ignore',
  });
  let ready = false;
  for (let attempt = 0; attempt < 100; attempt += 1) {
    try { if ((await fetch(origin)).ok) { ready = true; break; } } catch {}
    await delay(100);
  }
  if (!ready) throw new Error('Customer-mobile test server failed to start');
  chrome = spawn('C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe', [
    '--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check', '--remote-debugging-port=0',
    '--user-data-dir=' + profile, 'about:blank',
  ], { stdio: 'ignore' });
  let port;
  for (let attempt = 0; attempt < 70; attempt += 1) {
    try { port = (await fs.readFile(path.join(profile, 'DevToolsActivePort'), 'utf8')).split(/\r?\n/)[0]; break; } catch {}
    await delay(150);
  }
  if (!port) throw new Error('Test Chrome failed to start');
  const tabs = await (await fetch('http://127.0.0.1:' + port + '/json/list')).json();
  ws = new WebSocket(tabs.find((tab) => tab.type === 'page').webSocketDebuggerUrl);
  ws.addEventListener('message', (event) => {
    const result = JSON.parse(event.data);
    if (result.method === 'Runtime.exceptionThrown') exceptions.push(result.params.exceptionDetails.exception?.description || result.params.exceptionDetails.text);
    const task = pending.get(result.id);
    if (!task) return;
    clearTimeout(task.timer); pending.delete(result.id);
    result.error ? task.reject(new Error(result.error.message)) : task.resolve(result.result);
  });
  await new Promise((resolve, reject) => { ws.addEventListener('open', resolve, { once: true }); ws.addEventListener('error', reject, { once: true }); });
  await cdp('Page.enable'); await cdp('Runtime.enable');
  await cdp('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
  await cdp('Page.addScriptToEvaluateOnNewDocument', { source: '(' + installMock.toString() + ')()' });
  await cdp('Page.navigate', { url: origin + '/invite' });
  await until('document.querySelector(".invite-code-ticket")?.textContent.includes("INVITE88")');
  assert.match(await evaluate('document.querySelector(".reward-balances").textContent'), /¥128\.00/);
  assert.equal(await evaluate('document.documentElement.scrollWidth <= innerWidth + 1'), true, 'invite page overflows viewport');
  await evaluate('document.querySelector(".invite-actions .primary").click()');
  await until('window.__sharedInvite?.url.includes("invite=INVITE88")');
  await evaluate('Array.from(document.querySelectorAll(".detail-tabs button"))[1].click()');
  await until('document.querySelector(".reward-ledger")?.textContent.includes("推荐奖励入账")');
  await evaluate('Array.from(document.querySelectorAll(".detail-tabs button"))[0].click()');
  await evaluate('document.querySelector(".record-pagination button:last-child").click()');
  await until('document.querySelector(".invitation-list")?.textContent.includes("受邀商家7")');
  await evaluate('document.querySelector(".invite-toggle").click()');
  await until('document.querySelector(".invite-showcase header em")?.textContent.includes("已停用")');
  assert.ok((await evaluate('window.__inviteRequests')).some((item) => item.path === '/api/v1/invitations/mine' && item.method === 'PATCH'));

  await cdp('Page.navigate', { url: origin + '/register?invite=INVITE88' });
  await until('document.querySelector(".invite-valid")?.textContent.includes("商家甲")');
  assert.equal(await evaluate('document.querySelector("#invite-code").value'), 'INVITE88');
  assert.equal(await evaluate('document.querySelectorAll(".bottom-nav").length'), 0, 'public registration shows signed-in navigation');
  assert.equal(await evaluate('document.documentElement.scrollWidth <= innerWidth + 1'), true, 'register page overflows viewport');
  assert.deepEqual(exceptions, [], 'browser runtime errors');
  console.log('PASS mobile invite: code/share/status, records/pagination, reward wallet/ledger, and invite registration handoff');
})().catch((error) => { console.error(error.stack); process.exitCode = 1; }).finally(async () => {
  for (const task of pending.values()) clearTimeout(task.timer);
  ws?.close();
  if (chrome?.pid) spawnSync('taskkill.exe', ['/PID', String(chrome.pid), '/T', '/F'], { stdio: 'ignore' });
  if (server?.pid) spawnSync('taskkill.exe', ['/PID', String(server.pid), '/T', '/F'], { stdio: 'ignore' });
  await delay(400);
  await fs.rm(profile, { recursive: true, force: true, maxRetries: 3, retryDelay: 300 }).catch(() => {});
});
