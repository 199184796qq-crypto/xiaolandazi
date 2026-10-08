// Real WebAudio and streaming HTTP; silent synthetic PCM, no paid model calls.
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { createRequire } from 'node:module'
import { mkdir } from 'node:fs/promises'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const output = process.env.PCM_TEST_OUTPUT || 'E:/直播伴播/work/preview-pcm-test'
await mkdir(output, { recursive: true })
const run = { run_id: 'pcm-fixture', revision: 1, phase: 'body', status: 'ready', busy: false, uncertain: false, stream_controlled: true, preview_limit_ms: 120000, cursor: 2, attempts: [], queue: [], config: { max_retries: 1, tts: { rate: 1, buffer_seconds: 120 } }, accepted: [{selected:{id:'first',text:'第一份完整声音：111.367秒。'},task:{}},{selected:{id:'second',text:'第二份完整声音可以超过试听终点。'},task:{}}] }
let ack = 0, sent = 0, limit = false, paused = true, receiver, maximumLead = 0, streamConnections = 0
const calls = [], controls = []
const fixture = `<html><body><div id="app"></div><script type="module">import{createApp,h}from'/node_modules/.vite/deps/vue.js';import Panel from'/src/components/RealtimeAnchorPreviewPanel.vue';createApp({render:()=>h(Panel,{planId:2,tenantId:14,generationOptions:{buffer_seconds:120,body_min:2,body_max:6,max_chars:300}})}).mount('#app')</script></body></html>`
const server = await createServer({ configFile: false, plugins: [vue()], appType: 'custom', server: {host:'127.0.0.1',port:5382,strictPort:true} })
server.middlewares.use((req,res,next) => {
  const path = new URL(req.url, 'http://localhost').pathname
  if (path === '/__pcm_fixture') { res.setHeader('Content-Type','text/html'); res.end(fixture); return }
  if (!path.startsWith('/api/')) return next()
  calls.push(req.method + ' ' + path)
  const json = value => { res.setHeader('Content-Type','application/json'); res.end(JSON.stringify(value)) }
  if (path.endsWith('/risk-annotations')) return json({ hits: [], unresolved_count: 0 })
  if (path.endsWith('/realtime-preview') && req.method === 'POST') {
    let data=''; req.on('data',chunk=>data+=chunk); req.on('end',()=>{ assert.equal(JSON.parse(data).stream_version,1); setTimeout(()=>json({run}),300) }); return
  }
  if (path.endsWith('/playback')) {
    let data=''; req.on('data',chunk=>data+=chunk); req.on('end',()=>{
      const body=JSON.parse(data); controls.push(body); ack=Math.max(ack,Math.min(body.played_ms,sent))
      if(body.action==='pause'||body.action==='limit'||body.action==='stop'){paused=true;receiver?.end()}
      if(body.action==='resume')paused=false
      if(body.action==='limit')limit=true
      json({paused,played_ms:ack})
    }); return
  }
  if(path.endsWith('/stream')) {
    if(limit||receiver){res.statusCode=409;return json({error:'fixture already has receiver'})}
    receiver=res;paused=false;sent=ack;streamConnections++
    res.setHeader('Content-Type','application/x-ndjson');res.setHeader('Cache-Control','no-store');res.flushHeaders()
    const send=value=>res.write(JSON.stringify(value)+'\n')
    send({type:'state',run,ready_ms:146369-ack,part:'voice',progress:100})
    let start=Date.now(),lastFrame=0
    const timer=setInterval(()=>{
      if(paused||res.destroyed)return
      if(Date.now()-start<500||Date.now()-lastFrame<195||sent-ack>=800)return
      let samples=4800
      if(sent<111367)samples=Math.min(samples,Math.round((111367-sent)*24))
      const second=sent>=111367
      send({type:'audio',candidate_id:second?'second':'first',sample_rate:24000,channels:1,pcm:Buffer.alloc(samples*2).toString('base64'),clip_ms:second?sent-111367:sent,stream_ms:sent,caption:second?'第二份完整声音正在继续':'第一份声音正在播放'})
      sent+=samples/24;maximumLead=Math.max(maximumLead,sent-ack);lastFrame=Date.now()
    },10)
    res.on('close',()=>{clearInterval(timer);if(receiver===res)receiver=undefined})
    return
  }
  if(req.method==='DELETE')return json({run:null})
  res.statusCode=404;json({error:'unexpected fixture call'})
})
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true,args:['--mute-audio','--autoplay-policy=no-user-gesture-required']})
const page=await browser.newPage(),errors=[]
page.on('pageerror',e=>errors.push(e.message))
try {
  await page.goto('http://127.0.0.1:5382/__pcm_fixture')
  const play=page.getByRole('button',{name:'开始或继续试听'})
  await play.click()
  await page.getByText('正在准备…',{exact:true}).waitFor()
  await page.getByText('第一份声音正在播放',{exact:true}).waitFor()
  await page.waitForTimeout(2500)
  await page.getByRole('button',{name:'暂停试听',exact:true}).click()
  await page.waitForTimeout(400)
  const pausedClock=await page.locator('.elapsed').textContent().catch(()=>page.locator('.audio-player').innerText())
  const pauseAck=ack
  await page.waitForTimeout(1200)
  assert.equal(ack,pauseAck,'paused playback advanced server cursor')
  assert.equal(await page.locator('.elapsed').textContent().catch(()=>page.locator('.audio-player').innerText()),pausedClock)
  await play.click()
  await page.getByText('第一份声音正在播放',{exact:true}).waitFor()
  console.log('PASS real HTTP streaming, first-sound loading, pause and resume; running actual 120-second playback')
  await page.getByText('第二份完整声音正在继续',{exact:true}).waitFor({timeout:145000})
  await page.screenshot({path:output+'/after-1m51.png',fullPage:true})
  await page.getByText('本场试听已播放完成',{exact:true}).waitFor({timeout:25000})
  await page.waitForTimeout(600)
  assert.equal(limit,true,'browser did not stop backend at actual playback deadline')
  const last=controls.find(c=>c.action==='limit')
  assert.equal(last.played_ms,120000)
  assert.ok(sent<=121000,'backend PCM lead unbounded: '+sent)
  assert.ok(maximumLead<=1000.01,'jitter queue exceeded one second')
  assert.equal(streamConnections,2)
  assert.equal(calls.some(call=>/\/advance|\/voices\/|\.wav/.test(call)),false,'browser fetched/generated complete voice files')
  const sentAtLimit=sent;await page.waitForTimeout(1000);assert.equal(sent,sentAtLimit,'output continued after limit')
  assert.deepEqual(errors,[])
  await page.screenshot({path:output+'/at-2m00.png',fullPage:true})
  console.log(JSON.stringify({passed:true,actual_played_ms:last.played_ms,sent_ms:sent,maximum_lead_ms:maximumLead,pause_resume:true,continued_after_111367ms:true,complete_file_calls:0,page_errors:0}))
} finally { receiver?.end();await browser.close();await server.close() }
