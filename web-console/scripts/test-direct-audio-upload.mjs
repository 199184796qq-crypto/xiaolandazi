import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { readFile, writeFile, mkdir } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'
import ts from 'typescript'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const out = process.env.DIRECT_AUDIO_TEST_OUTPUT || 'E:/直播伴播-local/custom-mainline-oss-upload-20261030/browser'
await mkdir(out, { recursive: true })
const source = await readFile(new URL('../src/views/LiveStrategyView.vue', import.meta.url), 'utf8')
const marker = '<script setup lang="ts">'
const script = source.slice(source.indexOf(marker) + marker.length, source.indexOf('</script>'))
const ast = ts.createSourceFile('view.ts', script, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
const fn = name => { const node = ast.statements.find(n => ts.isFunctionDeclaration(n) && n.name?.text === name); assert.ok(node, name); return node.getText(ast) }
assert.ok(!source.includes('createCustomMainlineVoiceSample('))
assert.ok(!source.includes('decodeAudioData(await file.arrayBuffer())'))
const start = source.indexOf('<label>', source.indexOf('class="strategy-custom-mainline-upload"'))
const label = source.slice(start, source.indexOf('</label>', start) + 8)
const css = source.slice(source.indexOf('<style scoped>') + 14, source.indexOf('</style>'))
const fixture = `<script setup lang="ts">
import {ref} from 'vue'
import {validateOriginalAudioFiles, uploadOriginalAudio} from '/src/directAudioUpload.ts'
import {uploadLiveAgentCustomMainline} from '/src/api.ts'
import {beginLiveSupportScope} from '/src/liveSupportAccess.ts'
beginLiveSupportScope(29)
const recordingOriginalBusy=ref(false),customMainlineCloneBusy=ref(false),customMainlineFile=ref(null),customMainlineFiles=ref([]),customMainlineDurationMS=ref(0),customMainlineVoiceSample=ref(null),customMainlineUploadProgress=ref(0),customMainlineStage=ref('idle'),customMainlineError=ref(''),customMainlineDraft=ref(null);let planVersionSaveEpoch=1;
${fn('readCustomMainlineDuration')}
${fn('handleCustomMainlineFile')}
window.fixture={validateOriginalAudioFiles,handleCustomMainlineFile,error:()=>customMainlineError.value,selected:()=>customMainlineFiles.value.length,upload:async(signal)=>uploadLiveAgentCustomMainline(21,0,[new File(['first-audio-bytes'],'一.wav'),new File(['second-audio-bytes'],'二.mp3')],1000,null,(loaded,total)=>{window.progress.push(loaded/total)},signal),uploadOriginalAudio};
</script><template><main class="strategy-fullshow-output-grid is-recording-mainline"><section class="strategy-fullshow-output"><div class="strategy-custom-mainline-inline"><div class="strategy-custom-mainline-upload">${label}</div><div class="strategy-custom-mainline-progress"><div><i style="width:50%"></i></div><span>正在直传OSS 50%</span></div></div></section></main></template><style>${css}</style>`
const id = fileURLToPath(new URL('../src/__direct-upload-fixture.vue', import.meta.url)).replaceAll('\\','/')
const server = await createServer({server:{host:'127.0.0.1',port:0},plugins:[{name:'direct-upload-fixture',resolveId(name){if(name==='/__direct-upload-fixture.vue')return id},load(name){if(name===id)return fixture},configureServer(dev){dev.middlewares.use(async(req,res,next)=>{if(req.url!=='/__direct-upload-test')return next();res.setHeader('Content-Type','text/html;charset=utf-8');res.end(await dev.transformIndexHtml(req.url,'<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><div id="app"></div><script type="module">import {createApp} from "vue";import App from "/__direct-upload-fixture.vue";window.progress=[];createApp(App).mount("#app")</script>'))})}}]})
await server.listen()
const origin = `http://127.0.0.1:${server.httpServer.address().port}`
const browser = await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
const context = await browser.newContext()
const page = await context.newPage()
const errors=[];page.on('pageerror',e=>errors.push(e.message))
const apiCalls=[],uploads=[];let mode='ready',polls=0
const headers={'Access-Control-Allow-Origin':origin,'Access-Control-Allow-Methods':'POST,OPTIONS','Access-Control-Allow-Headers':'*'}
await page.route('https://direct-oss.test/**',async route=>{
 const request=route.request();if(request.method()==='OPTIONS'){await route.fulfill({status:204,headers});return}
 const raw=request.postDataBuffer();uploads.push({method:request.method(),headers:request.headers(),body:raw.toString('utf8')})
 await route.fulfill({status:204,headers})
})
await page.route('**/api/v1/live-agent-plans/21/custom-mainline/uploads**',async route=>{
 const request=route.request();const url=new URL(request.url());apiCalls.push({path:url.pathname,method:request.method(),body:request.postData(),support:request.headers()['x-live-support-room-id']});
 if(request.method()==='DELETE'){await route.fulfill({json:{id:'job',status:'cancelled'}});return}
 if(url.pathname.endsWith('/uploads')){await route.fulfill({json:{id:'job',uploads:[0,1].map(i=>({url:'https://direct-oss.test/',fields:{key:'isolated/source-'+i,policy:'test-policy',OSSAccessKeyId:'test-id',Signature:'test-signature',success_action_status:'204'}}))}});return}
 const status=mode==='ready' && polls++>0?'ready':mode==='failure'?'failed':'processing'
 await route.fulfill({json:{id:'job',status,error:status==='failed'?'模拟识别失败':undefined,result:status==='ready'?{audio_asset_id:99,timeline:[],srt:'subtitle'}:undefined}})
})
const checks=[]
try{
 await page.goto(origin+'/__direct-upload-test');await page.waitForFunction(()=>window.fixture)
 const limits=await page.evaluate(async()=>{
  const f=(size,name='录音.wav')=>({size,name});const validate=window.fixture.validateOriginalAudioFiles
  validate([f(2*1024**3)]);validate(Array.from({length:20},()=>f(2*1024**3)))
  const reject=[];for(const input of [[f(2*1024**3+1)],[f(0)],Array.from({length:21},()=>f(1)),[f(1,'bad.exe')]]){try{validate(input);reject.push(false)}catch{reject.push(true)}}
  await window.fixture.handleCustomMainlineFile({currentTarget:{files:[f(2*1024**3+1)],value:'test'}})
  return {reject,error:window.fixture.error(),selected:window.fixture.selected()}
 });assert.deepEqual(limits.reject,[true,true,true,true]);assert.equal(limits.selected,0);assert.match(limits.error,/2GB/);assert.equal(apiCalls.length,0);checks.push('2GB exact boundary, 20 files, format and empty checks, real parent rejects oversized before network')
 const response=await page.evaluate(()=>window.fixture.upload());assert.equal(response.audio_asset_id,99);assert.equal(uploads.length,2)
 for(const [i,upload] of uploads.entries()){assert.equal(upload.method,'POST');assert.ok(upload.body.includes(i===0?'first-audio-bytes':'second-audio-bytes'));assert.ok(!upload.headers.cookie);assert.ok(!upload.headers.authorization);assert.ok(!upload.headers['x-live-support-room-id'])}
 assert.ok(apiCalls.every(call=>call.support==='29'));assert.equal(apiCalls[0].method,'POST');const init=JSON.parse(apiCalls[0].body);assert.equal(init.files.length,2);assert.ok(apiCalls.every(call=>!call.body?.includes('first-audio-bytes')))
 const progress=await page.evaluate(()=>window.progress);assert.equal(progress.at(-1),1);assert.ok(progress.every((p,i)=>i===0||p>=progress[i-1]));checks.push('actual API wrapper sends only JSON to management; native files POST directly to OSS without credentials; progress monotonic')
 mode='waiting';await page.evaluate(()=>{window.controller=new AbortController();window.pending=window.fixture.upload(window.controller.signal).then(()=>({ok:true}),e=>({name:e.name}))});await page.waitForTimeout(150);await page.evaluate(()=>window.controller.abort());const cancelled=await page.evaluate(()=>window.pending);assert.equal(cancelled.name,'AbortError');assert.ok(apiCalls.some(call=>call.method==='DELETE'));checks.push('abort cancels current XHR/poll and sends scoped cancellation')
 mode='failure';const failed=await page.evaluate(()=>window.fixture.upload().then(()=>'',e=>e.message));assert.equal(failed,'模拟识别失败');checks.push('background processing failure reported; no server-upload fallback')
 for(const width of [1440,768,390,360]){await page.setViewportSize({width,height:780});const metrics=await page.locator('.strategy-custom-mainline-upload label span').evaluateAll(nodes=>nodes.map(node=>({text:node.textContent,size:parseFloat(getComputedStyle(node).fontSize),right:node.getBoundingClientRect().right})));assert.ok(metrics.length>=3);assert.ok(metrics.every(item=>item.size>=16));assert.ok(metrics.every(item=>item.right<=width+1));await page.screenshot({path:`${out}/upload-${width}.png`,fullPage:true})};checks.push('real upload template/styles under actual layout classes: four widths, >=16px, no overflow')
 assert.deepEqual(errors,[])
 await writeFile(out+'/verification.json',JSON.stringify({passed:true,checks,limits,managementRequests:apiCalls.map(c=>({path:c.path,method:c.method})),ossUploadCount:uploads.length},null,2))
 console.log(JSON.stringify({passed:true,checks}))
}finally{await browser.close();await server.close()}
