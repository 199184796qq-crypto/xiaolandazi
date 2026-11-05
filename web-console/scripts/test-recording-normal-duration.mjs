import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { existsSync, mkdirSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'

const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const out = process.env.SCRIPT_DURATION_EVIDENCE || 'E:/直播伴播-local/host-status-duration-release-20261105/duration-browser'
mkdirSync(out, { recursive: true })
const root = fileURLToPath(new URL('..', import.meta.url))
const plugin = { name:'normal-duration-fixture', configureServer(server) {
  server.middlewares.use(async (req,res,next) => {
    if(req.url!=='/__normal-duration.html') return next()
    res.setHeader('Content-Type','text/html; charset=utf-8')
    res.end(await server.transformIndexHtml(req.url,`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><body style="margin:0;padding:16px;box-sizing:border-box"><div id="app"></div><script type="module">
      import {createApp,h,ref} from 'vue';import Entries from '/src/components/RecordingScriptEntries.vue';
      const entries=ref([{id:'s1',text:''},{id:'s2',text:'乙'.repeat(936)}]),disabled=ref(false);
      window.fixture={entries:()=>entries.value,setEntries:value=>entries.value=value,setDisabled:value=>disabled.value=value};
      createApp({render:()=>h(Entries,{entries:entries.value,disabled:disabled.value,onUpdate:value=>entries.value=value})}).mount('#app');
    </script></body></html>`))
  })
} }
const server=await createServer({root,configFile:false,plugins:[vue(),plugin],server:{host:'127.0.0.1',port:18629,strictPort:true},logLevel:'error'})
await server.listen()
const browser=await chromium.launch({executablePath:[process.env.CHROME_PATH,'C:/Program Files/Google/Chrome/Application/chrome.exe'].find(p=>p&&existsSync(p)),headless:true})
const errors=[],requests=[],checks=[]
try {
  const page=await browser.newPage({viewport:{width:1440,height:1000}})
  page.on('pageerror',e=>errors.push(e.message))
  await page.route('**/api/**',route=>{requests.push(route.request().url());return route.abort()})
  await page.goto('http://127.0.0.1:18629/__normal-duration.html')
  const first=page.getByRole('textbox',{name:'话术 1 正文',exact:true})
  const duration=()=>page.locator('.recording-script-entry-duration').first()
  for(const [text,count,label] of [
    ['',0,'0 秒'],['甲',1,'1 秒'],['甲'.repeat(60),60,'12 秒'],
    ['甲'.repeat(319),319,'1 分 0 秒'],['甲'.repeat(320),320,'1 分 0 秒'],
    ['甲'.repeat(321),321,'1 分 1 秒'],['甲'.repeat(470),470,'1 分 29 秒'],
    ['甲'.repeat(4000),4000,'12 分 30 秒'],[' 甲\n乙 😀 ',4,'1 秒'],
  ]) {
    await first.fill(text)
    assert.equal((await duration().innerText()).trim(),'· 正常语速约 '+label)
    assert.ok((await first.locator('xpath=ancestor::section[1]/header').innerText()).includes(count+' 字符'))
    assert.equal(await first.inputValue(),text)
  }
  checks.push('live estimate; empty text, minute boundaries, 470/4000 characters and Unicode; draft text preserved')
  assert.equal((await page.locator('.recording-script-entry-duration').nth(1).innerText()).trim(),'· 正常语速约 2 分 56 秒')
  assert.ok((await duration().getAttribute('title')).includes('320 字/分钟'))
  checks.push('each entry has an independent normal-speed estimate and an explanation of actual audio duration')
  await first.fill('甲'.repeat(470))
  for(const width of [1440,777,390,320]) {
    await page.setViewportSize({width,height:1000})
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true)
    assert.equal(await duration().evaluate(el=>getComputedStyle(el).fontSize),'20px')
    await page.screenshot({path:out+'/duration-'+width+'.png',fullPage:true})
  }
  checks.push('desktop and narrow layouts retain 20px text without horizontal overflow')
  await page.evaluate(()=>window.fixture.setDisabled(true))
  assert.equal(await first.isDisabled(),true)
  assert.ok((await duration().innerText()).includes('1 分 29 秒'))
  await page.evaluate(()=>{window.fixture.setDisabled(false);window.fixture.setEntries([{id:'new-group',text:'丙'.repeat(640)}])})
  assert.equal(await page.locator('.recording-script-entry-duration').count(),1)
  assert.ok((await duration().innerText()).includes('2 分 0 秒'))
  assert.equal(await first.inputValue(),'丙'.repeat(640))
  checks.push('disabled editor and switching groups preserve correct text and update estimates')
  assert.deepEqual(errors,[]);assert.deepEqual(requests,[])
  writeFileSync(out+'/results.json',JSON.stringify({passed:true,checks,errors,apiRequests:requests,realProviderCalls:0},null,2))
  console.log(JSON.stringify({passed:true,groups:checks.length,realProviderCalls:0}))
} finally {await browser.close();await server.close()}
