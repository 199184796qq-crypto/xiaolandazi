import assert from 'node:assert/strict'
import {createServer} from 'vite'
import {createRequire} from 'node:module'
import {mkdir} from 'node:fs/promises'
const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url)),{chromium}=require('playwright-core')
const out='E:/直播伴播-local/validation/20261008-compact-miner-buttons';await mkdir(out,{recursive:true})
const fixture=`<html><head><meta charset="utf-8"><style>*{box-sizing:border-box}body{margin:0;padding:22px;background:#f0f3ff;font-family:Arial,'Microsoft Yahei',sans-serif}#app{max-width:900px;margin:auto}.speech-miner-head,.speech-miner-results,.speech-miner-resizer,.speech-miner-chat>header,.speech-miner-messages{display:none!important}.speech-miner-grid{display:block!important;min-height:0!important}.speech-miner-chat{min-height:0!important}</style></head><body><div id="app"></div><script type="module">import{createApp,h}from'/node_modules/.vite/deps/vue.js';import Miner from'/src/components/SpeechMinerWorkbench.vue';createApp({render:()=>h(Miner,{planId:4})}).mount('#app')</script></body></html>`
const server=await createServer({server:{host:'127.0.0.1',port:5389},appType:'custom'})
server.middlewares.use((req,res,next)=>{if(req.url==='/__buttons'){res.setHeader('Content-Type','text/html; charset=utf-8');res.end(fixture)}else next()});await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true}),page=await browser.newPage({viewport:{width:1000,height:620}}),errors=[]
page.on('pageerror',e=>errors.push(e.message))
try{
 await page.goto('http://127.0.0.1:5389/__buttons')
 const upper=page.locator('.speech-miner-chat-start>button');await upper.first().waitFor()
 assert.equal(await upper.count(),2)
 assert.ok(await upper.first().getByText('从内容中提炼亮点',{exact:true}).isVisible())
 assert.ok(await upper.last().getByText('补充真实信息',{exact:true}).isVisible())
 for(let i=0;i<2;i++){const box=await upper.nth(i).boundingBox();assert.ok(box.width<=190&&box.height<=70,JSON.stringify(box));assert.ok(await upper.nth(i).locator('svg').isVisible())}
 const backgrounds=await upper.evaluateAll(els=>els.map(el=>getComputedStyle(el).backgroundImage));assert.notEqual(backgrounds[0],backgrounds[1]);assert.match(backgrounds[0],/242, 237, 255/)
 const lower=page.locator('.speech-miner-composer button');for(let i=0;i<2;i++){const box=await lower.nth(i).boundingBox();assert.ok(box.width<=140&&box.height<=44,JSON.stringify(box));assert.ok(await lower.nth(i).isDisabled())}
 await page.locator('textarea').fill('本品常温保存');assert.ok(await lower.last().isEnabled());assert.ok(await lower.first().isDisabled())
 await page.screenshot({path:out+'/desktop.png',fullPage:true})
 await page.setViewportSize({width:390,height:700})
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
 for(let i=0;i<2;i++){const box=await upper.nth(i).boundingBox();assert.ok(box.width<=180&&box.height<=70)}
 await page.screenshot({path:out+'/mobile.png',fullPage:true})
 assert.deepEqual(errors,[])
 console.log('PASS: compact pastel-purple/blue gradient entry cards with SVG and subtitles, small footer buttons, original disabled guards, mobile wrapping; no API calls')
}finally{await browser.close();await server.close()}
