import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { readFile, mkdir, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { createServer } from 'vite'

const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const baselineDir = process.env.AGENT_LOGIN_BASELINE_DIR
const proofDir = process.env.AGENT_LOGIN_EVIDENCE_DIR
const baseline = baselineDir ? {
  name: 'agent-login-baseline', enforce: 'pre',
  async transform(code, id) {
    if (id.includes('?')) return
    const file = id.split('?')[0].replaceAll('\\', '/')
    if (file.endsWith('/src/App.vue')) return await readFile(path.join(baselineDir, 'App.vue'), 'utf8')
    if (file.endsWith('/src/components/SystemAgentLayer.vue')) return await readFile(path.join(baselineDir, 'SystemAgentLayer.vue'), 'utf8')
  },
} : null

const fixture = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><link rel="icon" href="data:,"><div id="app"></div><script type="module">
import {createApp,h,nextTick} from 'vue';import {createRouter,createMemoryHistory} from 'vue-router';
import App from '/src/App.vue';import Agent from '/src/components/SystemAgentLayer.vue';
import {session,loadSession,applySession,clearSession} from '/src/session.ts';
import {feedbackState} from '/src/uiFeedback.ts';import {beginLiveSupportScope} from '/src/liveSupportAccess.ts';
import {getLiveAgentPlans} from '/src/api.ts';import '/src/style.css';
const params=new URLSearchParams(location.search),mode=params.get('mode')||'app';
const bootstrap=(role='staff',user=8,tenant=0)=>({actor:{role,user_id:user,tenant_id:tenant||undefined,username:'fixture',display_name:'测试账号',must_change_password:false,phone:'13800000000',province:'四川省',city:'成都市',district:'武侯区'},staff_access:{permissions:[],roles:[],primary_group_code:'live_operations'},tenants:[],environment:'test'});
const state={calls:[],pending:[],server:bootstrap(),delayPlans:false};
window.EventSource=class {addEventListener(){} close(){}};
window.localStorage.setItem('system-agent-live-plan-id','4');
window.fetch=async(url,init={})=>{
 const p=new URL(url,location.origin).pathname,method=init.method||'GET';
 state.calls.push({path:p,method,room:new Headers(init.headers).get('X-Live-Support-Room-ID')});
 if(p.endsWith('/ui-preferences')&&method==='PUT')return Response.json(JSON.parse(init.body||'{}'));
 if(method!=='GET')throw Error('Unexpected mutation '+p);
 if(p==='/api/v1/bootstrap')return await new Promise(resolve=>state.releaseBootstrap=()=>resolve(Response.json(state.server)));
 if(p==='/api/v1/live-agent-plans'){
  const account=state.server.actor;
  if(account.role!=='customer'||!account.tenant_id)return Response.json({error:'请选择已获得客户授权的直播间'},{status:403});
  const result={items:[{id:4,tenant_id:account.tenant_id,name:'客户'+account.tenant_id+'方案',status:'active'},{id:5,tenant_id:account.tenant_id,name:'客户'+account.tenant_id+'备用',status:'active'}]};
  if(state.delayPlans)return await new Promise(resolve=>state.pending.push(()=>resolve(Response.json(result))));
  return Response.json(result);
 }
 if(p.endsWith('/ui-preferences'))return Response.json({agent_drawer_collapsed:false,sidebar_collapsed:true});
 if(p==='/api/v1/work/inbox')return Response.json({groups:[],version:'fixture',stale:false,total:0});
 return Response.json({items:[],capabilities:[],departments:[],groups:[],permissions:[],available:true});
};
if(params.get('role')){state.server=bootstrap(params.get('role'),14,params.get('tenant')==='none'?0:14);applySession(state.server)}
const router=createRouter({history:createMemoryHistory(),routes:[{path:'/',name:'fixture-home',component:{render:()=>h('div','登录后的页面')}},{path:'/:pathMatch(.*)*',component:{render:()=>h('div')}}]});
if(mode==='app')router.beforeEach(async()=>{await loadSession()});
const app=createApp(mode==='app'?App:Agent);app.use(router).mount('#app');
window.fixture={state,feedbackState,session,async login(role,user=14,tenant=14){state.server=bootstrap(role,user,tenant);applySession(state.server);await nextTick()},async logout(){clearSession();await nextTick()},async tick(){await nextTick()},async probeDenied(){const end=beginLiveSupportScope(29);try{await getLiveAgentPlans()}catch{}finally{end()}}};
</script></html>`

const server = await createServer({ plugins: baseline ? [baseline] : [], server: { host: '127.0.0.1', port: 0, proxy: {} }, appType: 'custom' })
server.middlewares.use(async (req, res, next) => {
  if (!req.url.startsWith('/__agent_login')) return next()
  res.setHeader('Content-Type', 'text/html;charset=utf-8')
  res.end(await server.transformIndexHtml('/__agent_login', fixture))
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const results = []
const pages = []
async function open(query) {
  const page = await browser.newPage({ viewport: { width: 1500, height: 1000 } })
  pages.push(page)
  page.fixtureErrors = []
  page.importRequests = []
  page.on('pageerror', error => page.fixtureErrors.push(error.message))
  await page.route('**/api/**', route => {
    // Imports can read public UI configuration before the fixture installs fetch.
    // Keep that traffic local too; business calls still run through the fixture.
    page.importRequests.push(new URL(route.request().url()).pathname)
    return route.fulfill({ json: {} })
  })
  await page.goto(server.resolvedUrls.local[0] + '__agent_login?' + query)
  await page.waitForFunction(() => Boolean(window.fixture))
  return page
}
async function snapshot(page) {
  return await page.evaluate(() => ({ calls: window.fixture.state.calls, errors: window.fixture.feedbackState.errors.map(e => e.message) }))
}
async function settle(page) { await page.evaluate(() => window.fixture.tick()); await page.waitForTimeout(100) }
try {
  const app = await open('mode=app')
  await app.waitForFunction(() => Boolean(window.fixture.state.releaseBootstrap))
  await settle(app)
  const beforeLogin = await snapshot(app)
  results.push({ case: 'bootstrap_pending', ...beforeLogin })
  if (baselineDir) {
    assert.ok(beforeLogin.calls.some(c => c.path === '/api/v1/live-agent-plans'))
    assert.ok(beforeLogin.errors.some(e => e.includes('请选择已获得客户授权的直播间')))
    console.log('REPRODUCED: App mounted the agent before bootstrap, issued a roomless plan request, and recorded the exact reported authorization error.')
  } else {
    assert.equal(beforeLogin.calls.filter(c => c.path === '/api/v1/live-agent-plans').length, 0)
    assert.deepEqual(beforeLogin.errors, [])
    await app.evaluate(() => window.fixture.state.releaseBootstrap())
    await app.locator('.app-shell').waitFor()
    await settle(app)
    const staff = await snapshot(app)
    results.push({ case: 'staff_after_bootstrap', ...staff })
    assert.equal(staff.calls.filter(c => c.path === '/api/v1/live-agent-plans').length, 0)
    assert.deepEqual(staff.errors, [])

    for (const query of ['', 'role=platform_admin', 'role=staff', 'role=sales_staff', 'role=agent_admin', 'role=customer&tenant=none']) {
      const page = await open('mode=agent&' + query)
      await settle(page)
      const state = await snapshot(page)
      assert.equal(state.calls.filter(c => c.path === '/api/v1/live-agent-plans').length, 0, query || 'anonymous')
      assert.deepEqual(state.errors, [])
      results.push({ case: query || 'anonymous_agent', ...state })
    }

    const customer = await open('mode=agent&role=customer')
    await customer.locator('.tenant-plan-picker').waitFor()
    assert.ok((await customer.locator('.tenant-plan-picker').innerText()).includes('客户14方案'))
    assert.equal((await snapshot(customer)).calls.filter(c => c.path === '/api/v1/live-agent-plans').length, 1, 'one initial customer request')
    await customer.locator('.tenant-plan-picker select').selectOption('5')
    await settle(customer)
    assert.equal(await customer.locator('.tenant-plan-picker select').inputValue(), '5')
    results.push({ case: 'customer_plan_loading_and_selection', ...(await snapshot(customer)) })

    await customer.evaluate(async () => {window.fixture.state.delayPlans=true;await window.fixture.login('customer',15,15)})
    await customer.waitForFunction(() => window.fixture.state.pending.length === 1)
    await customer.evaluate(async () => {window.fixture.state.delayPlans=false;await window.fixture.login('customer',16,16)})
    await customer.waitForFunction(() => document.querySelector('.tenant-plan-picker')?.textContent.includes('客户16方案'))
    await customer.evaluate(() => window.fixture.state.pending.shift()())
    await settle(customer)
    assert.ok((await customer.locator('.tenant-plan-picker').innerText()).includes('客户16方案'), 'late previous-account response must not overwrite the current account')

    await customer.evaluate(async () => {window.fixture.state.delayPlans=true;await window.fixture.login('customer',17,17)})
    await customer.waitForFunction(() => window.fixture.state.pending.length === 1)
    await customer.evaluate(() => window.fixture.logout())
    await customer.evaluate(() => window.fixture.state.pending.shift()())
    await settle(customer)
    await customer.evaluate(() => window.fixture.login('customer',18,0))
    await settle(customer)
    assert.equal(await customer.locator('.tenant-plan-picker').count(), 0, 'logout clears pending plan data')
    assert.deepEqual((await snapshot(customer)).errors, [])
    results.push({ case: 'account_switch_and_logout_discard_stale_responses', ...(await snapshot(customer)) })

    const denied = pages[2] // platform admin: the real support request still gets the explicit business failure.
    await denied.evaluate(() => window.fixture.probeDenied())
    const deniedState = await snapshot(denied)
    assert.ok(deniedState.errors.some(e => e.includes('请选择已获得客户授权的直播间')))
    assert.equal(deniedState.calls.filter(c => c.path === '/api/v1/live-agent-plans').at(-1).room, '29')
    results.push({ case: 'explicit_support_failure_remains_visible', ...deniedState })
    for (const page of pages) {
      assert.deepEqual(page.fixtureErrors, [])
      assert.ok((await snapshot(page)).calls.every(c => c.method === 'GET' || (c.path.endsWith('/ui-preferences') && c.method === 'PUT')), 'no business writes or generation requests')
      assert.ok(page.importRequests.every(p => p === '/api/v1/system/public-config'), 'only public branding can run before fetch is installed')
    }
    console.log('PASS: pending bootstrap, anonymous, four non-customer roles, missing tenant, customer selection, stale account responses, logout, and explicit support denial; no business writes or generation.')
  }
} finally {
  if (proofDir) {
    await mkdir(proofDir, { recursive: true })
    await writeFile(path.join(proofDir, baselineDir ? 'baseline-reproduction.json' : 'regression.json'), JSON.stringify(results, null, 2))
  }
  await browser.close()
  await server.close()
}
