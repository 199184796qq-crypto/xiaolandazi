import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createServer } from 'vite'
import ts from 'typescript'
const { chromium } = createRequire(process.env.PLAN_TEST_RUNTIME || new URL('../../collector-worker/package.json', import.meta.url))('playwright-core')
const output = process.env.PLAN_TEST_OUTPUT || '../deploy/validation/plan-voice-rate'
const room = await readFile(new URL('../src/views/RoomDetailView.vue', import.meta.url), 'utf8')
const strategy = await readFile(new URL('../src/views/LiveStrategyView.vue', import.meta.url), 'utf8')
const ast = ts.createSourceFile('room.ts', room.slice(room.indexOf('<script setup lang="ts">') + 24, room.indexOf('</script>')), ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
const handler = ast.statements.find(n => ts.isFunctionDeclaration(n) && n.name?.text === 'changeLiveSpeechRate')?.getText(ast)
assert.ok(handler)
const roomControl = room.slice(room.indexOf('<label v-if="canConfigureRoomStrategy" class="speech-rate-control">'), room.indexOf('</label>', room.indexOf('<label v-if="canConfigureRoomStrategy" class="speech-rate-control">')) + 8)
const strategyControl = strategy.match(/<PlanVoiceRateControl[\s\S]*?\/>/)[0]
const roomRetry = room.match(/<button v-if="canConfigureRoomStrategy && liveSpeechRateError[\s\S]*?<\/button>/)[0]
const roomStyles = room.match(/^\.speech-rate-.+$/gm).join('\n')
assert.ok(strategy.includes('rate: planVoiceRate.saved.value'))
const roomFixture = `<script setup lang="ts">
import {usePlanVoiceRate} from '/src/usePlanVoiceRate.ts'
import {planVoiceRateOptions as liveSpeechRateOptions} from '/src/planVoiceRate.ts'
const canConfigureRoomStrategy=true
const planVoiceRate=usePlanVoiceRate(()=>window.fixture.scope)
const liveSpeechRate=planVoiceRate.rate,liveSpeechRateBusy=planVoiceRate.busy,liveSpeechRateError=planVoiceRate.error
${handler}
window.fixture.room=()=>({rate:liveSpeechRate.value,ready:planVoiceRate.ready.value,error:planVoiceRate.error.value})
</script><template><article aria-label="直播页"><h2>直播页</h2>${roomControl}<p role="alert" v-if="planVoiceRate.error.value">{{planVoiceRate.error.value}}</p>${roomRetry}</article></template><style scoped>${roomStyles}\narticle{background:#121e30;color:#aebbd0}</style>`
const mainFixture = `<script setup lang="ts">
import {reactive} from 'vue'
import {applySession} from '/src/session.ts'
import {usePlanVoiceRate} from '/src/usePlanVoiceRate.ts'
import PlanVoiceRateControl from '/src/components/PlanVoiceRateControl.vue'
import Room from '/__rate-room.vue'
const scope=reactive({tenantId:14,planId:5,key:'user14:room29',allowed:true})
window.fixture={scope,calls:[],data:window.__rates||{'14:6':1.1,'24:5':1.2},failRead:false,failSave:false,holdRead:false,holdSave:false,wrongReply:false}
window.fetch=async(url,init={})=>{
 const u=new URL(url,location.origin),plan=Number(u.pathname.split('/').at(-2)),tenant=Number(u.searchParams.get('tenant_id')),key=tenant+':'+plan,method=init.method||'GET',body=init.body?JSON.parse(init.body):null
 if(!u.pathname.endsWith('/voice-rate'))throw Error('Unexpected API '+u.pathname)
 window.fixture.calls.push({plan,tenant,method,body})
 if(method==='GET'){
  const rate=window.fixture.data[key]??1
  if(window.fixture.holdRead){window.fixture.holdRead=false;await new Promise(resolve=>window.fixture.releaseRead=resolve)}
  if(window.fixture.failRead)return Response.json({error:'读取失败，请重试'},{status:503})
  return Response.json({tenant_id:tenant,plan_id:plan,rate})
 }
 if(window.fixture.holdSave){window.fixture.holdSave=false;await new Promise(resolve=>window.fixture.releaseSave=resolve)}
 if(window.fixture.failSave)return Response.json({error:'保存失败，请重试'},{status:409})
 window.fixture.data[key]=body.rate
 return Response.json({tenant_id:tenant,plan_id:window.fixture.wrongReply?999:plan,rate:body.rate})
}
applySession({actor:{user_id:14,tenant_id:14,role:'customer',username:'test'},tenants:[{id:14,name:'本地测试'}],environment:'test'})
const planVoiceRate=usePlanVoiceRate(()=>scope)
window.fixture.strategy=()=>({rate:planVoiceRate.rate.value,saved:planVoiceRate.saved.value,ready:planVoiceRate.ready.value,error:planVoiceRate.error.value,busy:planVoiceRate.busy.value})
window.fixture.reload=planVoiceRate.load
window.fixture.save=planVoiceRate.save
</script><template><main><h1>方案语速本地验证</h1><article aria-label="方案页"><h2>方案页</h2>${strategyControl}</article><Room/></main></template><style>body{font:16px Microsoft YaHei,sans-serif;background:#f4f6ff;color:#344568;margin:20px}main{max-width:1100px;margin:auto}article{padding:16px;margin:16px 0;background:white;border:1px solid #e0e5ff;border-radius:14px}select{font-size:16px;padding:12px;border:1px solid #ccd6ee;border-radius:10px}.speech-rate-control{display:flex;align-items:center;gap:12px;font-size:16px}h1{font-size:24px}h2{font-size:20px}</style>`
const ids = Object.fromEntries(['main', 'room'].map(name => [name, fileURLToPath(new URL('../src/__rate-' + name + '.vue', import.meta.url)).replaceAll('\\', '/')]))
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, plugins: [{ name: 'rate-local-fixture',
  resolveId(id) { if (id === '/__rate-main.vue') return ids.main; if (id === '/__rate-room.vue') return ids.room },
  load(id) { if (id === ids.main) return mainFixture; if (id === ids.room) return roomFixture },
  configureServer(dev) { dev.middlewares.use(async (req, res, next) => {
    if (req.url !== '/__rate-test') return next()
    res.setHeader('Content-Type', 'text/html;charset=utf-8'); res.end(await dev.transformIndexHtml(req.url, `<!doctype html><html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="icon" href="data:,"><div id="app"></div><script type="module">import {createApp} from 'vue';import App from '/__rate-main.vue';createApp(App).mount('#app')</script></html>`))
  }) },
}] })
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const checks = [], errors = []
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } }); page.setDefaultTimeout(10000); page.on('pageerror', e => errors.push(e.message))
  const planSelect = page.getByRole('article', { name: '方案页' }).getByRole('combobox'), liveSelect = page.getByRole('article', { name: '直播页' }).getByRole('combobox')
  const waitRate = async (rate) => page.waitForFunction(rate => window.fixture.strategy().ready && window.fixture.room().ready && window.fixture.strategy().rate === rate && window.fixture.room().rate === rate, rate)
  await page.goto(server.resolvedUrls.local[0] + '__rate-test'); await waitRate(1)
  assert.equal(await page.evaluate(() => window.fixture.calls.some(c => c.method === 'PUT')), false)
  await liveSelect.selectOption('1.1'); await waitRate(1.1)
  assert.deepEqual(await page.evaluate(() => window.fixture.calls.find(c => c.method === 'PUT')), { plan: 5, tenant: 14, method: 'PUT', body: { rate: 1.1 } })
  checks.push('无版本新方案默认1，无默认写入；直播页真实处理函数自动保存到当前方案，方案页同步回显')
  const data = await page.evaluate(() => window.fixture.data); await page.addInitScript(data => window.__rates = data, data)
  await page.reload(); await waitRate(1.1)
  await planSelect.selectOption('0.9'); await waitRate(0.9)
  checks.push('真实页面reload恢复服务器保存值；方案页修改同样自动保存，直播页同步')
  await page.evaluate(() => window.fixture.scope.planId = 6); await waitRate(1.1)
  await page.evaluate(() => window.fixture.scope.planId = 5); await waitRate(0.9)
  await page.evaluate(() => window.fixture.scope.tenantId = 24); await waitRate(1.2)
  await page.evaluate(() => window.fixture.scope.tenantId = 14); await waitRate(0.9)
  await page.evaluate(() => { window.fixture.scope.planId = 99 }); await waitRate(1)
  checks.push('方案/租户隔离，后续任意新方案默认1，切回恢复原方案值')
  await page.evaluate(() => { window.fixture.failSave = true }); await liveSelect.selectOption('1.2')
  await page.waitForFunction(() => window.fixture.room().error.includes('保存失败')); assert.equal(await liveSelect.inputValue(), '1'); assert.equal(await planSelect.inputValue(), '1')
  await page.evaluate(() => { window.fixture.failSave = false; window.fixture.failRead = true; window.fixture.scope.planId = 5 })
  await page.waitForFunction(() => !!window.fixture.strategy().error); assert.equal(await planSelect.isDisabled(), true)
  await page.evaluate(() => window.fixture.failRead = false); await page.getByRole('button', { name: '重新读取', exact: true }).click(); await page.getByRole('button', { name: '重新读取语速', exact: true }).click(); await waitRate(0.9)
  checks.push('保存失败回退实际已保存值并报错；读取失败禁用选择，重新读取可恢复')
  await page.evaluate(() => { window.fixture.holdRead = true; window.fixture.reload() }); await page.waitForFunction(() => !!window.fixture.releaseRead)
  await page.evaluate(() => window.fixture.scope.planId = 6); await waitRate(1.1); await page.evaluate(() => window.fixture.releaseRead()); await waitRate(1.1)
  checks.push('前一个方案的迟到读取不能覆盖新方案')
  await page.evaluate(() => { window.fixture.holdSave = true }); await planSelect.selectOption('1.2'); await page.waitForFunction(() => !!window.fixture.releaseSave)
  assert.equal(await planSelect.isDisabled(), true)
  const puts = await page.evaluate(() => window.fixture.calls.filter(c => c.method === 'PUT').length); await page.evaluate(() => window.fixture.save(0.8)); assert.equal(await page.evaluate(() => window.fixture.calls.filter(c => c.method === 'PUT').length), puts)
  await page.evaluate(() => window.fixture.scope.planId = 5); await waitRate(0.9); await page.evaluate(() => window.fixture.releaseSave()); await waitRate(0.9)
  await page.evaluate(() => window.fixture.scope.planId = 6); await waitRate(1.2)
  checks.push('处理中防重复提交；切换时保存仍只写原方案，迟到写回不覆盖新方案，切回可读真实结果')
  await page.evaluate(() => { window.fixture.holdSave = true; window.fixture.releaseSave = null }); await planSelect.selectOption('0.8'); await page.waitForFunction(() => !!window.fixture.releaseSave)
  await page.evaluate(() => window.fixture.scope.planId = 5); await waitRate(0.9); await page.evaluate(() => window.fixture.scope.planId = 6); await waitRate(1.2)
  await page.evaluate(() => window.fixture.releaseSave()); await waitRate(0.8)
  checks.push('A→B→A后原请求完成时重新读取服务器真值，不恢复旧页面状态')
  await page.evaluate(() => window.fixture.scope.allowed = false); await page.waitForFunction(() => !window.fixture.strategy().ready)
  assert.equal(await planSelect.isDisabled(), true); await page.evaluate(() => window.fixture.scope.allowed = true); await waitRate(0.8)
  checks.push('授权变化清理旧值并重新读取，不显示前一身份的保存成功状态')
  await mkdir(output, { recursive: true }); await page.screenshot({ path: output + '/desktop.png', fullPage: true })
  for (const width of [1440, 1024, 700, 390, 320]) { await page.setViewportSize({ width, height: 900 }); assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true); for (const select of [planSelect, liveSelect]) assert.ok(await select.evaluate(e => parseFloat(getComputedStyle(e).fontSize) >= 16)) }
  await page.screenshot({ path: output + '/mobile.png', fullPage: true }); checks.push('五种宽度无横向溢出，方案页控件和直播页控件均可用')
  assert.deepEqual(errors, []); await writeFile(output + '/checks.json', JSON.stringify({ checks, errors, liveRequests: false }, null, 2)); console.log('PASS: ' + checks.join('；'))
} finally { await browser.close(); await server.close() }
