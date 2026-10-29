// Actual TTS settings view; all API traffic is intercepted in the test page.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdir, writeFile } from 'node:fs/promises'
import { createServer } from 'vite'
const { chromium } = createRequire(new URL('../../collector-worker/package.json', import.meta.url))('playwright-core')
const out = process.env.TTS_TEST_OUTPUT || 'E:/直播伴播-local/tts-pool-implementation-20261029/browser'
await mkdir(out, { recursive: true })
const fixture = `
import {createApp} from 'vue'
import View from '/src/views/TTSPoolSettingsView.vue'
window.fixture={calls:[],conflict:false,fail:false,pool:{accounts:[{id:'current-aliyun-tts',name:'阿里账号A',account_uid:'10001',credential_source:'current',configured:true,enabled:true,rpm:180,rps:3,revision:1,used_minute:180,used_second:3,in_flight:2,voices:{}},{id:'second',name:'阿里账号B',account_uid:'10002',credential_source:'encrypted',configured:true,enabled:true,rpm:180,rps:3,revision:2,used_minute:12,used_second:0,in_flight:0,voices:{'qwen-audio-3.1-tts-flash/original':'second-voice'}}],queued:1,requests:[{id:'q1',model:'qwen-audio-3.1-tts-flash',room_id:29,session_id:550,queued_at:new Date().toISOString()}],observed_at:new Date().toISOString()}}
window.fetch=async (url,init={})=>{
 if(!String(url).endsWith('/api/v1/system/tts-pool'))throw Error('Unexpected test request')
 const method=init.method||'GET',body=init.body?JSON.parse(init.body):null
 window.fixture.calls.push({method,body})
 if(window.fixture.fail)return Response.json({error:'读取TTS账户池失败'},{status:503})
 if(method==='GET')return Response.json(window.fixture.pool)
 if(window.fixture.conflict)return Response.json({error:'TTS账户配置已变化，请刷新后重试'},{status:409})
 return Response.json({saved:true})
}
createApp(View).mount('#app')
`
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom', plugins: [{
  name: 'tts-pool-fixture', enforce: 'pre',
  resolveId(id) { if (id === '/__tts-test.js') return '\0tts-test' },
  load(id) { if (id === '\0tts-test') return fixture },
  configureServer(dev) { dev.middlewares.use(async (req, res, next) => {
    if (req.url !== '/') return next()
    const html = await dev.transformIndexHtml('/', '<html><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body style="margin:0;background:#f4f6fd"><div id="app"></div><script type="module" src="/__tts-test.js"></script></body></html>')
    res.setHeader('Content-Type', 'text/html'); res.end(html)
  }) },
}] })
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const checks = [], errors = []
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } })
  page.on('pageerror', e => errors.push(e.message))
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/`)
  await page.getByText('阿里账号A', { exact: true }).waitFor()
  assert.match(await page.locator('main').innerText(), /180 \/ 180/)
  assert.match(await page.locator('main').innerText(), /12 \/ 180/)
  assert.match(await page.locator('main').innerText(), /550/)
  checks.push('真实页面显示两个账号分别用量、在途合成和跨房间排队数据')
  await page.getByRole('button', { name: '添加账号', exact: true }).click()
  const editor = page.getByRole('region', { name: '编辑TTS账号' })
  // section with an accessible name becomes a region in Chromium.
  await page.getByLabel('账号名称', { exact: true }).fill('阿里账号C')
  await page.getByLabel('阿里云主账号UID', { exact: true }).fill('10003')
  await page.getByLabel('API Key', { exact: true }).fill('local-fixture-key')
  assert.equal(await page.getByRole('button', { name: '保存账号', exact: true }).isDisabled(), true)
  await page.getByRole('button', { name: '添加音色对应', exact: true }).click()
  await page.getByLabel('方案原音色ID', { exact: true }).fill('original')
  await page.getByLabel('本账号对应音色ID', { exact: true }).fill('third-voice')
  await page.getByLabel('每分钟请求上限', { exact: true }).fill('181')
  assert.equal(await page.getByRole('button', { name: '保存账号', exact: true }).isDisabled(), true)
  await page.getByLabel('每分钟请求上限', { exact: true }).fill('180')
  await page.getByLabel('每秒请求上限', { exact: true }).fill('4')
  assert.equal(await page.getByRole('button', { name: '保存账号', exact: true }).isDisabled(), true)
  await page.getByLabel('每秒请求上限', { exact: true }).fill('3')
  await page.getByRole('button', { name: '保存账号', exact: true }).click()
  await page.getByRole('status').waitFor()
  const saved = await page.evaluate(() => window.fixture.calls.filter(c => c.method === 'PUT').at(-1).body)
  assert.equal(saved.rpm, 180); assert.equal(saved.rps, 3); assert.equal(saved.new_api_key, 'local-fixture-key')
  assert.equal(saved.voices['qwen-audio-3.1-tts-flash/original'], 'third-voice')
  assert.equal(await page.getByLabel('API Key', { exact: true }).count(), 0)
  checks.push('添加账号必须有凭据与音色对应；181 RPM/4 RPS不能保存；提交正确映射且保存后清除凭据')
  await page.getByRole('row').filter({ hasText: '阿里账号B' }).getByRole('button', { name: '编辑' }).click()
  await page.getByLabel('账号名称', { exact: true }).fill('保留中的编辑')
  await page.getByLabel('API Key', { exact: true }).fill('should-be-cleared-after-error')
  await page.evaluate(() => { window.fixture.conflict = true; window.fixture.pool.accounts[1].used_minute = 28 })
  await page.getByRole('button', { name: '保存账号', exact: true }).click()
  await page.getByRole('alert').waitFor()
  assert.equal(await page.getByLabel('账号名称', { exact: true }).inputValue(), '保留中的编辑')
  assert.equal(await page.getByLabel('API Key', { exact: true }).inputValue(), '')
  await page.getByRole('button', { name: '刷新用量', exact: true }).click()
  await page.getByRole('row').filter({ hasText: '阿里账号B' }).getByText('28 / 180').waitFor()
  assert.equal(await page.getByLabel('账号名称', { exact: true }).inputValue(), '保留中的编辑')
  checks.push('并发配置冲突保留编辑内容并清除新Key；刷新用量不覆盖正在编辑的草稿或版本')
  await page.evaluate(() => { window.fixture.conflict = false })
  await page.getByLabel('启用账号', { exact: true }).uncheck()
  await page.getByRole('button', { name: '保存账号', exact: true }).click()
  await page.getByRole('status').waitFor()
  assert.equal(await page.evaluate(() => window.fixture.calls.filter(c => c.method === 'PUT').at(-1).body.enabled), false)
  await page.screenshot({ path: `${out}/desktop.png`, fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('button', { name: '添加账号', exact: true }).click()
  await page.getByRole('button', { name: '添加音色对应', exact: true }).click()
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
  await page.screenshot({ path: `${out}/mobile.png`, fullPage: true })
  checks.push('停用账号使用保存版本提交；390px宽度表格内部滚动，编辑表单不溢出')
  assert.deepEqual(errors, [])
  await writeFile(`${out}/result.json`, JSON.stringify({ passed: true, checks, errors }, null, 2))
  console.log(JSON.stringify({ passed: true, checks }, null, 2))
} finally { await browser.close(); await server.close() }
