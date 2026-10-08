import assert from 'node:assert/strict'
import { readFile, mkdir } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url))
const {chromium}=require('playwright-core')
const webRequire=createRequire(new URL('../package.json',import.meta.url))
const vueRequire=createRequire(webRequire.resolve('vue/package.json'))
const {parse}=webRequire('vue/compiler-sfc')
const {compile}=vueRequire('@vue/compiler-dom')
const source=await readFile(new URL('../src/views/LiveStrategyView.vue',import.meta.url),'utf8')
const start=source.indexOf('<nav class="strategy-mode-tabs strategy-mode-tabs-v2 strategy-workflow-nav"')
const nav=source.slice(start,source.indexOf('</nav>',start)+6)
const {code}=compile(nav,{mode:'function',prefixIdentifiers:true})
const css=parse(source).descriptor.styles.map(s=>s.content).join('\n')
const fixture=`<html><head><meta charset="utf-8"><style>*{box-sizing:border-box}body{margin:0;padding:16px;background:#f6f8fd;font-family:'Microsoft Yahei',sans-serif}.test-shell{max-width:1500px;--workflow-surface:#e9eeff}${css}</style></head><body><div class="test-shell" id="app"></div><script type="module">
import * as Vue from '/node_modules/.vite/deps/vue.js';import banner from '/src/assets/agent-generation-banner.png';import FactRiskTabNotice from '/src/components/FactRiskTabNotice.vue';
const state=Vue.reactive({activeMode:'voice',factRiskUnresolvedCount:0,wheelRiskUnresolvedCount:0,factRiskTabFocused:false,factRiskNoticeState:{loading:false,error:''}});window.fixture=state;
Vue.createApp({components:{FactRiskTabNotice},setup:()=>({...Vue.toRefs(state),agentGenerationBanner:banner,openFactRiskLocation:()=>state.activeMode='knowledge',openWheelRiskLocation:()=>state.activeMode='miner'}),render:new Function('Vue',${JSON.stringify(code)})(Vue)}).mount('#app');</script></body></html>`
const server=await createServer({server:{host:'127.0.0.1',port:0},appType:'custom'})
server.middlewares.use((req,res,next)=>{if(req.url==='/__banner'){res.setHeader('Content-Type','text/html; charset=utf-8');res.end(fixture)}else next()})
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
const page=await browser.newPage({viewport:{width:1540,height:280}}),errors=[]
page.on('pageerror',e=>{errors.push(e.message);console.error(e.message)})
await page.route('**/api/v1/**',()=>{throw new Error('UI test must not make API writes')})
try {
  await page.goto(server.resolvedUrls.local[0]+'__banner')
  const button=page.getByRole('button',{name:'生成智能体',exact:true})
  await button.waitFor()
  await page.waitForFunction(()=>document.querySelector('.strategy-agent-generation-banner')?.naturalWidth===2069)
  assert.equal(await page.locator('.strategy-workflow-output-mark').count(),0)
  assert.equal(await button.getAttribute('aria-current'),null)
  await button.click()
  assert.equal(await page.evaluate(()=>window.fixture.activeMode),'fullshow')
  assert.equal(await button.getAttribute('aria-current'),'step')
  await page.getByRole('button',{name:'声音',exact:true}).click()
  assert.equal(await page.evaluate(()=>window.fixture.activeMode),'voice')
  await button.focus();await page.keyboard.press('Enter')
  assert.equal(await page.evaluate(()=>window.fixture.activeMode),'fullshow')
  const out=new URL('../../deploy/validation/agent-generation-banner/',import.meta.url);await mkdir(out,{recursive:true})
  await page.screenshot({path:fileURLToPath(new URL('desktop.png',out))})
  await button.screenshot({path:fileURLToPath(new URL('button.png',out))})
  await page.setViewportSize({width:390,height:250})
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
  await button.click()
  await page.screenshot({path:fileURLToPath(new URL('mobile.png',out))})
  assert.deepEqual(errors,[])
  console.log('PASS: supplied original art; old icon removed; original action; active state; keyboard access; mobile no overflow')
}finally{await browser.close();await server.close()}
