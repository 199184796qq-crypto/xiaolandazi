import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'

const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const root = fileURLToPath(new URL('..', import.meta.url))
const server = await createServer({ root, configFile: false, plugins: [vue(), {
  name: 'host-voice-fixture', enforce: 'pre', load(id) {
    if (id.endsWith('/src/api.ts')) return `export const getLiveHostVoice=async room=>{window.fixture.reads++;return structuredClone(window.fixture.snapshot)};export const setLiveHostVoice=async(room,input)=>{window.fixture.commands.push({room,input});const s=window.fixture.snapshot;s.state.requested={voice_id:input.voice_id,model:input.model};s.state.revision++;const result=structuredClone(s);if(window.fixture.delayCommand)return await new Promise(resolve=>window.fixture.resolveCommand=()=>resolve(result));return result};`
  }, configureServer(s) { s.middlewares.use(async (req,res,next) => {
    if (req.url !== '/__voice.html') return next()
    res.setHeader('Content-Type','text/html; charset=utf-8')
    res.end(await s.transformIndexHtml('/__voice.html',`<html><body><div id="app"></div><script type="module">
      import {createApp,h,reactive} from 'vue';import View from '/src/components/LiveHostVoiceQueue.vue';
      window.fixture={reads:0,commands:[],props:reactive({roomId:29,sessionId:100,canChange:true}),snapshot:{state:{session_id:100,version_id:44,revision:1,elapsed_ms:0,batch:1,active:{voice_id:'A',model:'test-model'}},voices:[{voice_id:'A',model:'test-model',name:'主播小杨'},{voice_id:'B',model:'test-model',name:'主播小许'}]}};
      createApp({render:()=>h(View,window.fixture.props)}).mount('#app');
    </script></body></html>`))
  }) }
}], server:{host:'127.0.0.1',port:18613,strictPort:true}, logLevel:'error' })
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true,args:['--mute-audio']})
const page=await browser.newPage({viewport:{width:390,height:800}})
const errors=[];page.on('pageerror',e=>errors.push(e.message))
const results=[]
try {
  await page.clock.install();await page.goto('http://127.0.0.1:18613/__voice.html')
  await page.locator('strong').filter({hasText:'主播小杨'}).waitFor()
  assert.deepEqual(await page.getByRole('combobox').locator('option').allTextContents(),['主播小杨','主播小许'])
  await page.getByRole('combobox').selectOption(JSON.stringify(['test-model','B']))
  await page.getByText('正在准备所选主播声音，继续使用当前主播').waitFor()
  assert.ok((await page.locator('strong').innerText()).includes('主播小杨'))
  const commands=await page.evaluate(()=>window.fixture.commands)
  assert.equal(commands.length,1);assert.deepEqual(commands[0],{room:29,input:{session_id:100,version_id:44,voice_id:'B',model:'test-model'}})
  results.push('manual selection writes only this live session; current host stays A while B prepares')
  await page.evaluate(()=>{const s=window.fixture.snapshot.state;s.active=s.requested;delete s.requested;s.revision=3;s.batch++})
  await page.clock.runFor(3100);await page.locator('strong').filter({hasText:'主播小许'}).waitFor()
  await page.evaluate(()=>{const s=window.fixture.snapshot.state;s.active={voice_id:'A',model:'test-model'};s.revision=1})
  await page.clock.runFor(3100);assert.equal(await page.locator('strong').innerText(),'主播小许')
  results.push('newer active-host receipt wins over stale poll')
  await page.evaluate(()=>{window.fixture.delayCommand=true;const s=window.fixture.snapshot.state;s.active={voice_id:'B',model:'test-model'};s.revision=4})
  await page.getByRole('combobox').selectOption(JSON.stringify(['test-model','A']))
  await page.getByText('提交切换中…').waitFor()
  await page.evaluate(()=>{const s=window.fixture.snapshot.state;delete s.requested;s.revision=10})
  await page.clock.runFor(3100)
  await page.evaluate(()=>window.fixture.resolveCommand())
  await page.getByText('提交切换中…').waitFor({state:'hidden'})
  assert.equal(await page.getByRole('combobox').inputValue(),JSON.stringify(['test-model','B']))
  results.push('late command response cannot overwrite a newer playback state')
  await page.evaluate(()=>{window.fixture.props.sessionId=101;const s=window.fixture.snapshot.state;s.session_id=101;s.active={voice_id:'A',model:'test-model'};s.revision=1})
  await page.locator('strong').filter({hasText:'主播小杨'}).waitFor()
  await page.evaluate(()=>{const s=window.fixture.snapshot.state;s.session_id=100;s.active={voice_id:'B',model:'test-model'};s.revision=99})
  await page.clock.runFor(3100)
  assert.equal(await page.locator('strong').innerText(),'主播小杨')
  results.push('old live session cannot write into replacement session')
  await page.evaluate(()=>{window.fixture.props.canChange=false})
  assert.equal(await page.getByRole('combobox').isDisabled(),true)
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
  assert.deepEqual(errors,[])
  results.push('read-only permission and mobile width respected')
  console.log(JSON.stringify({passed:true,results}))
} finally {await browser.close();await server.close()}
