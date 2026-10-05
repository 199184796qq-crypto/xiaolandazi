// Browser regression for the customer-mobile AI time balance and time-card activation flow.
// Every API call is mocked; the test never signs in or mutates real business data.
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const { spawn, spawnSync } = require('node:child_process');

const root = path.resolve(__dirname, '..');
const profile = path.join(root, 'data/tmp/mobile-ai-time-ui-' + process.pid);
const origin = 'http://127.0.0.1:5859';
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
    const timer = setTimeout(() => {
      pending.delete(id);
      reject(new Error('CDP timeout: ' + method));
    }, 15000);
    pending.set(id, { resolve, reject, timer });
    ws.send(JSON.stringify({ id, method, params }));
  });
}

async function evaluate(expression) {
  const result = await cdp('Runtime.evaluate', {
    expression,
    awaitPromise: true,
    returnByValue: true,
  });
  if (result.exceptionDetails) {
    throw new Error(result.exceptionDetails.exception?.description || result.exceptionDetails.text);
  }
  return result.result.value;
}

async function until(expression) {
  for (let attempt = 0; attempt < 120; attempt += 1) {
    try {
      if (await evaluate(expression)) return;
    } catch (error) {
      if (!/Execution context was destroyed|Cannot find context|Inspected target navigated/.test(error.message)) throw error;
    }
    await delay(100);
  }
  throw new Error('Condition failed: ' + expression + '\n' + await evaluate('document.body.innerText'));
}

function installMock() {
  const realFetch = window.fetch.bind(window);
  window.__aiTimeRequests = [];
  window.__timeCardActivated = false;
  window.fetch = async (input, init = {}) => {
    const url = new URL(typeof input === 'string' ? input : input.url, location.href);
    if (!url.pathname.startsWith('/api/')) return realFetch(input, init);
    const method = init.method || 'GET';
    window.__aiTimeRequests.push({ path: url.pathname, method, search: url.search });
    let data = {};
    if (url.pathname === '/api/v1/bootstrap') {
      data = {
        actor: {
          user_id: 501,
          tenant_id: 601,
          username: 'mobile-ui-test',
          display_name: '测试账号',
          role: 'customer',
          must_change_password: false,
        },
        permissions: [],
        rooms: [],
        settings: {},
        capabilities: [],
      };
    } else if (url.pathname === '/api/v1/system/public-config') {
      data = { site_name: '测试系统', client_agent_name: '小蓝直播搭子' };
    } else if (url.pathname === '/api/v1/rooms') {
      data = { items: [], page: 1, page_size: 20, total: 0 };
    } else if (url.pathname === '/api/v1/live/devices') {
      data = [];
    } else if (url.pathname === '/api/v1/live/quota-summary') {
      data = {
        active_seconds: window.__timeCardActivated ? 18300 : 7500,
        active_time_card_seconds: window.__timeCardActivated ? 18300 : 7500,
        reserve_time_card_seconds: window.__timeCardActivated ? 0 : 10800,
        reserve_time_card_count: window.__timeCardActivated ? 0 : 1,
        sources: [],
        time_cards: [],
        active_billing_rooms: [],
      };
    } else if (url.pathname === '/api/v1/live/time-cards' && method === 'GET') {
      data = {
        items: window.__timeCardActivated ? [] : [{
          id: 31,
          asset_no: 'TC-UI-00031',
          product_name: '3 小时时长卡',
          status: 'unactivated',
          original_seconds: 10800,
          remaining_seconds: 10800,
          activation_mode: 'manual',
          validity_days: 30,
          activation_deadline_at: '2026-12-31T12:00:00Z',
          purchased_at: '2026-10-03T08:00:00Z',
        }],
        page: 1,
        page_size: 4,
        total: window.__timeCardActivated ? 0 : 1,
      };
    } else if (url.pathname === '/api/v1/live/time-cards/31/activate' && method === 'POST') {
      window.__timeCardActivated = true;
      data = {
        asset_id: 31,
        quota: {
          active_seconds: 18300,
          active_time_card_seconds: 18300,
          reserve_time_card_seconds: 0,
          reserve_time_card_count: 0,
          sources: [],
          time_cards: [],
          active_billing_rooms: [],
        },
      };
    }
    return new Response(JSON.stringify(data), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    });
  };
}

(async () => {
  await fs.mkdir(profile, { recursive: true });
  server = spawn(process.execPath, ['node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', '5859', '--strictPort'], {
    cwd: path.join(root, 'customer-mobile'),
    stdio: 'ignore',
  });
  let serverReady = false;
  for (let attempt = 0; attempt < 100; attempt += 1) {
    try {
      const response = await fetch(origin);
      if (response.ok) {
        serverReady = true;
        break;
      }
    } catch {}
    await delay(100);
  }
  if (!serverReady) throw new Error('Customer-mobile test server failed to start');

  chrome = spawn('C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe', [
    '--headless=new',
    '--disable-gpu',
    '--no-first-run',
    '--no-default-browser-check',
    '--remote-debugging-port=0',
    '--user-data-dir=' + profile,
    'about:blank',
  ], { stdio: 'ignore' });
  let port;
  for (let attempt = 0; attempt < 70; attempt += 1) {
    try {
      port = (await fs.readFile(path.join(profile, 'DevToolsActivePort'), 'utf8')).split(/\r?\n/)[0];
      break;
    } catch {}
    await delay(150);
  }
  if (!port) throw new Error('Test Chrome failed to start');
  const tabs = await (await fetch('http://127.0.0.1:' + port + '/json/list')).json();
  ws = new WebSocket(tabs.find((tab) => tab.type === 'page').webSocketDebuggerUrl);
  ws.addEventListener('message', (event) => {
    const result = JSON.parse(event.data);
    if (result.method === 'Runtime.exceptionThrown') {
      exceptions.push(result.params.exceptionDetails.exception?.description || result.params.exceptionDetails.text);
    }
    const task = pending.get(result.id);
    if (!task) return;
    clearTimeout(task.timer);
    pending.delete(result.id);
    result.error ? task.reject(new Error(result.error.message)) : task.resolve(result.result);
  });
  await new Promise((resolve, reject) => {
    ws.addEventListener('open', resolve, { once: true });
    ws.addEventListener('error', reject, { once: true });
  });
  await cdp('Page.enable');
  await cdp('Runtime.enable');
  await cdp('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
  await cdp('Page.addScriptToEvaluateOnNewDocument', { source: '(' + installMock.toString() + ')()' });
  await cdp('Page.navigate', { url: origin + '/' });

  await until('document.querySelector(".home-ai-balance")?.textContent.includes("2小时5分")');
  assert.match(await evaluate('document.querySelector(".home-time-card-entry").textContent'), /1 张待启用/);
  await evaluate('document.querySelector(".home-time-card-entry").click()');
  await until('document.querySelector(".time-card-item")?.textContent.includes("3")');
  assert.match(await evaluate('document.querySelector(".time-card-item").textContent'), /启用后 30 天/);
  await evaluate('document.querySelector(".time-card-detail button").click()');
  await until('document.querySelector(".time-card-notice")?.textContent.includes("启用成功")');
  await until('document.querySelector(".home-ai-balance")?.textContent.includes("5小时5分")');
  assert.ok((await evaluate('window.__aiTimeRequests')).some((item) => item.path === '/api/v1/live/time-cards/31/activate' && item.method === 'POST'));
  assert.deepEqual(exceptions, [], 'browser runtime errors');
  console.log('PASS mobile AI time: balance, card pack, activation, and immediate balance refresh');
})().catch((error) => {
  console.error(error.stack);
  process.exitCode = 1;
}).finally(async () => {
  for (const task of pending.values()) clearTimeout(task.timer);
  ws?.close();
  if (chrome?.pid) spawnSync('taskkill.exe', ['/PID', String(chrome.pid), '/T', '/F'], { stdio: 'ignore' });
  if (server?.pid) spawnSync('taskkill.exe', ['/PID', String(server.pid), '/T', '/F'], { stdio: 'ignore' });
  await delay(400);
  await fs.rm(profile, { recursive: true, force: true, maxRetries: 3, retryDelay: 300 }).catch(() => {});
});
