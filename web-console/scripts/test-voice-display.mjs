import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { mkdir, writeFile } from 'node:fs/promises'
import { createServer } from '../node_modules/vite/dist/node/index.js'
import vue from '../node_modules/@vitejs/plugin-vue/dist/index.mjs'
const { chromium } = createRequire(new URL('../../collector-worker/package.json', import.meta.url))('playwright-core')
const out = process.env.VOICE_DISPLAY_OUTPUT || 'E:/直播伴播-local/voice-display-20261101/admin-browser'
await mkdir(out, { recursive: true })
const fixture = `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><div id="app"></div><script type="module">
import{createApp}from'vue';import View from'/src/views/TTSPoolSettingsView.vue';import'/src/style.css';
window.fixture={calls:[],items:[{voice_id:'S_one',name:'',gender:'',revision:0},{voice_id:'S_two',name:'男声甲',gender:'男声',revision:1}],conflict:false,fail:false};
window.fetch=async(url,init={})=>{const u=new URL(url,location.origin),p=u.pathname,method=init.method||'GET',body=init.body?JSON.parse(init.body):null,f=window.fixture;f.calls.push({p,method,body});
if(p.endsWith('/tts-voice-display')){if(method==='GET')return Response.json({items:f.items});if(f.conflict)return Response.json({error:'声音展示资料已被修改，请重新读取后保存'},{status:409});if(f.fail)return Response.json({error:'保存声音展示资料失败'},{status:500});const saved={...body,revision:body.revision+1};f.items=f.items.map(v=>v.voice_id===body.voice_id?saved:v);return Response.json(saved)}
if(p.endsWith('/tts-pool'))return Response.json({accounts:[],queued:0,requests:[],observed_at:new Date().toISOString()});
if(p.endsWith('/rooms'))return Response.json({items:[]});
throw Error('Unexpected local request '+p)};
createApp(View).mount('#app');</script>`
const server = await createServer({ root: fileURLToPath(new URL('..', import.meta.url)), configFile: false, cacheDir: out + '/vite-cache', plugins: [vue(), { name: 'display-fixture', configureServer(dev) { dev.middlewares.use(async (req, res, next) => { if (req.url !== '/__display') return next(); res.setHeader('Content-Type', 'text/html;charset=utf-8'); res.end(await dev.transformIndexHtml(req.url, fixture)) }) } }], server: { host: '127.0.0.1', port: 0 }, logLevel: 'error' })
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const checks = [], errors = [], external = []
let page
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, reducedMotion: 'reduce' })
  await context.route('**/*', route => { if (new URL(route.request().url()).hostname === '127.0.0.1') return route.continue(); external.push(route.request().url()); return route.abort() })
  page = await context.newPage(); page.on('pageerror', e => errors.push(e.message)); page.setDefaultTimeout(15000)
  const url = `http://127.0.0.1:${server.httpServer.address().port}/__display`
  await page.goto(url)
  const panel = page.getByRole('region', { name: '高仿真声音展示资料' }), first = panel.locator('form').nth(0), second = panel.locator('form').nth(1)
  await first.getByLabel('声音名称').waitFor()
  assert.equal(await first.getByLabel('声音头像').inputValue(), '')
  assert.equal(await page.evaluate(() => window.fixture.calls.some(c => c.method !== 'GET')), false)
  await first.getByLabel('声音名称').fill('晚晴'); await first.getByLabel('声音头像').selectOption('女声')
  await second.getByLabel('声音名称').fill('另一个未保存草稿')
  // The parent refreshes account usage every five seconds. Force that same
  // parent action to prove it never replaces display drafts or account keys.
  await page.getByRole('button', { name: '刷新用量', exact: true }).click()
  assert.equal(await first.getByLabel('声音名称').inputValue(), '晚晴')
  await first.getByRole('button', { name: '保存展示资料', exact: true }).click()
  await panel.getByRole('status').filter({ hasText: '展示资料已保存' }).waitFor()
  const writes = await page.evaluate(() => window.fixture.calls.filter(c => c.method !== 'GET'))
  assert.deepEqual(writes, [{ p: '/api/v1/system/tts-voice-display', method: 'PUT', body: { voice_id: 'S_one', name: '晚晴', gender: '女声', revision: 0 } }])
  assert.equal(await second.getByLabel('声音名称').inputValue(), '另一个未保存草稿')
  checks.push('实际TTS后台父页：独立保存名称/女声头像，不写账户凭据或合成队列；刷新用量保留展示草稿')
  await page.reload(); await first.getByLabel('声音名称').waitFor()
  // A fresh page fixture starts with server data replaced below, followed by
  // explicit reload of the component from that server response.
  await page.evaluate(() => { window.fixture.items[0] = { voice_id: 'S_one', name: '晚晴', gender: '女声', revision: 1 } })
  await panel.getByRole('button', { name: '重新读取音色' }).click()
  await page.waitForFunction(() => document.querySelector('.voice-display-panel input')?.value === '晚晴')
  assert.equal(await first.getByLabel('声音头像').inputValue(), '女声')
  checks.push('名称及女声资料重新读取回显，不依赖账户编辑表单')
  await page.evaluate(() => window.fixture.conflict = true)
  await first.getByLabel('声音名称').fill('冲突草稿'); await first.getByRole('button', { name: '保存展示资料', exact: true }).click()
  await panel.getByRole('alert').filter({ hasText: '重新读取后保存' }).waitFor()
  assert.equal(await first.getByLabel('声音名称').inputValue(), '冲突草稿')
  await page.evaluate(() => { window.fixture.conflict = false; window.fixture.fail = true })
  await first.getByRole('button', { name: '保存展示资料', exact: true }).click()
  await panel.getByRole('alert').filter({ hasText: '保存声音展示资料失败' }).waitFor()
  assert.equal(await first.getByLabel('声音名称').inputValue(), '冲突草稿')
  checks.push('并发冲突/服务器失败明确显示且保留原草稿，不伪装保存成功')
  await page.evaluate(() => { window.fixture.fail = false; window.fixture.items = [] })
  await panel.getByRole('button', { name: '重新读取音色' }).click()
  await panel.getByText('暂无可用音色，请先保存账号及音色映射，再重新读取。').waitFor()
  assert.equal(await panel.locator('form').count(), 0)
  checks.push('空目录不保留过期编辑行')
  await page.evaluate(() => window.fixture.items = [{ voice_id: 'S_one', name: '晚晴', gender: '女声', revision: 1 }])
  await panel.getByRole('button', { name: '重新读取音色' }).click(); await first.getByLabel('声音名称').waitFor()
  await panel.screenshot({ path: out + '/desktop.png' }); await page.setViewportSize({ width: 390, height: 844 })
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
  await panel.screenshot({ path: out + '/mobile.png' })
  checks.push('后台完整父页1440/390px布局无横向溢出')
  assert.deepEqual(errors, []); assert.deepEqual(external, [])
  await writeFile(out + '/result.json', JSON.stringify({ passed: true, checks, errors, external }, null, 2))
  console.log(JSON.stringify({ passed: true, checks }))
} catch (e) {
  if (page) { await page.screenshot({ path: out + '/failure.png', fullPage: true }); await writeFile(out + '/failure.txt', String(e) + '\n' + await page.locator('body').innerText()) }
  throw e
} finally { await browser.close(); await server.close() }
