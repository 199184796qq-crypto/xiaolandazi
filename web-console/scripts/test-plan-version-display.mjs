// Real RoomDetailView and LiveStrategyView, with synthetic read-only API fixtures.
import assert from 'node:assert/strict'
import { readFile, mkdir, writeFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { createServer } from 'vite'
const require = createRequire(process.env.PLAYWRIGHT_CORE_PACKAGE || new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const roomSource = await readFile(new URL('./room-parity-fixture.mjs', import.meta.url), 'utf8')
let roomHTML = roomSource.match(/server\.transformIndexHtml\('\/__room-parity.html', `([\s\S]*?)`\)\)/)[1]
roomHTML = roomHTML.replace('${JSON.stringify(speechIdentity)}', 'null')
roomHTML = roomHTML.replace('const bootstrap=', `
const baseFetch=window.fetch;
window.fixture.planId=4;window.fixture.savedNo=31;window.fixture.savedId=80;window.fixture.running=true;window.fixture.programSession=1;
window.fetch=async(url,options={})=>{
 const path=new URL(url,location.origin).pathname;
 if(path.endsWith('/published-version'))return Response.json({version:{id:window.fixture.savedId,plan_id:window.fixture.planId,version_no:window.fixture.savedNo,lifecycle_status:'published',variants:[],voice_identity:{rate:1}}});
 if(path.endsWith('/live-agent-plans'))return Response.json({items:[{id:4,name:'共享直播方案',room_ids:[15,26,27]},{id:5,name:'另一个方案',room_ids:[15]}]});
 if(path.endsWith('/runtime'))return Response.json({agent_plan_id:window.fixture.planId,agent_plan_name:'共享直播方案',agent_mode:'anchor',agent_state:window.fixture.running?'working':'stopped',quota_remaining_seconds:7200,session:{id:window.fixture.session,status:window.fixture.running?'running':'stopped'}});
 if(path.endsWith('/speech-runtime'))return Response.json({room_id:15,mainline:{},interrupt:{},program:{room_id:15,running:true,program_id:'program-test',version_id:80,version_no:31,task:{session_id:'live-runtime-'+window.fixture.programSession}}});
 return baseFetch(url,options);
};
const bootstrap=`)
const strategySource = await readFile(new URL('./test-interaction-full-view-location.mjs', import.meta.url), 'utf8')
let strategyHTML = strategySource.match(/const fixture = `([\s\S]*?)`\nconst deferredWorkspace/)[1]
strategyHTML = strategyHTML.replace("window.fixture={calls:[],records,delayWorkspace:location.search.includes('delayed=1')};", `
const version={id:80,tenant_id:14,room_id:26,plan_id:4,version_no:31,lifecycle_status:'published',variants:[],voice_identity:{},generation_context:{content_mode:'ai_dynamic',anchor_generation_options:{buffer_seconds:120,body_min:2,body_max:4,max_chars:300}}};
window.fixture={calls:[],records,version};`)
strategyHTML = strategyHTML.replace("if(p.endsWith('/config-versions'))", `
if(p.endsWith('/workspace'))return Response.json({version:window.fixture.version,source:'published',inherited:true,checks:[]});
if(p.endsWith('/versions'))return Response.json({items:[window.fixture.version]});
if(p.endsWith('/realtime-options'))return Response.json({buffer_seconds:120,body_min:2,body_max:4,max_chars:300});
if(p.endsWith('/config-versions'))`)
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom' })
server.middlewares.use(async (req, res, next) => {
  const isRoom = req.url?.startsWith('/__version_room'), isStrategy = req.url?.startsWith('/__version_strategy')
  if (!isRoom && !isStrategy) return next()
  res.setHeader('Content-Type', 'text/html;charset=utf-8')
  res.end(await server.transformIndexHtml(req.url, isRoom ? roomHTML : strategyHTML))
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const output = process.env.PLAN_VERSION_TEST_OUTPUT
if (output) await mkdir(output, { recursive: true })
const errors = []
try {
  const page = await browser.newPage({ viewport: { width: 1680, height: 1000 }, reducedMotion: 'reduce' })
  page.on('pageerror', error => errors.push(error.message))
  const base = server.resolvedUrls.local[0]
  await page.goto(base + '__version_room?role=customer')
  const badge = page.locator('.companion-plan-version')
  await badge.getByText('版本一致', { exact: true }).waitFor()
  assert.equal(await badge.getByText('V31', { exact: true }).count(), 2)
  await page.evaluate(() => { window.fixture.savedNo = 32; window.fixture.savedId = 81 })
  await badge.getByText('有新版本，下次开播加载', { exact: true }).waitFor({ timeout: 20000 })
  assert.equal(await badge.getByText('V31', { exact: true }).count(), 1)
  assert.equal(await badge.getByText('V32', { exact: true }).count(), 1)
  if (output) await badge.screenshot({ path: output + '/room-version-difference.png' })
  await page.evaluate(() => { window.fixture.session = 2 })
  await badge.getByText('待确认', { exact: true }).waitFor({ timeout: 10000 })
  assert.equal(await badge.getByText('V31', { exact: true }).count(), 0)
  await page.evaluate(() => { window.fixture.running = false })
  await badge.getByText('未开播', { exact: true }).waitFor({ timeout: 10000 })
  await page.setViewportSize({ width: 390, height: 844 })
  const bounds = await badge.boundingBox()
  assert.ok(bounds.x >= 0 && bounds.x + bounds.width <= 391, 'version status stays inside the mobile viewport')
  if (output) await badge.screenshot({ path: output + '/room-version-mobile.png' })
  await page.goto(base + '__version_strategy')
  await page.getByRole('button', { name: '生成智能体', exact: true }).click()
  const strategyBadge = page.locator('.strategy-content-mode-workflow .plan-version-badge')
  await strategyBadge.getByText('V31', { exact: true }).waitFor()
  await strategyBadge.getByText('方案 #4', { exact: true }).waitFor()
  if (output) await strategyBadge.screenshot({ path: output + '/strategy-version.png' })
  assert.deepEqual(errors, [])
  if (output) await writeFile(output + '/result.json', JSON.stringify({ passed: true, checks: ['Core actual version', 'new save preserves loaded version', 'reject previous scene', 'stopped scene', 'mobile layout', 'inherited shared strategy version'], errors }, null, 2))
  console.log('PASS real room and strategy version displays, including updates and old-session rejection.')
} finally {
  await browser.close()
  await server.close()
}
