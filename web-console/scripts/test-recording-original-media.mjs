import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createServer } from 'vite'
import ts from 'typescript'

const require = createRequire(process.env.ORIGINAL_MEDIA_TEST_RUNTIME || new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const output = process.env.ORIGINAL_MEDIA_TEST_OUTPUT || 'E:/直播伴播-local/recording-tab-original-media-20261022'
const source = await readFile(new URL('../src/views/LiveStrategyView.vue', import.meta.url), 'utf8')
const marker = '<script setup lang="ts">'
const script = source.slice(source.indexOf(marker) + marker.length, source.indexOf('</script>'))
const ast = ts.createSourceFile('view.ts', script, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
const extractedFunctions = [], extractedVariables = []
function functionSource(name) {
  const node = ast.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === name)
  assert.ok(node, `Missing real parent function: ${name}`)
  extractedFunctions.push(name)
  return node.getText(ast)
}
function variableSource(name) {
  for (const statement of ast.statements) {
    if (!ts.isVariableStatement(statement)) continue
    const declaration = statement.declarationList.declarations.find(node => ts.isIdentifier(node.name) && node.name.text === name)
    if (declaration) {
      extractedVariables.push(name)
      return `const ${declaration.getText(ast)}`
    }
  }
  assert.fail(`Missing real parent variable: ${name}`)
}

// The fixture imports the actual display component and extracts the production
// association/archive/mutation functions above. No live API or speech provider is used.
function pcmFixture() {
  const samples = 1600, buffer = Buffer.alloc(44 + samples * 2)
  buffer.write('RIFF', 0);buffer.writeUInt32LE(buffer.length - 8, 4);buffer.write('WAVEfmt ', 8)
  buffer.writeUInt32LE(16, 16);buffer.writeUInt16LE(1, 20);buffer.writeUInt16LE(1, 22)
  buffer.writeUInt32LE(16000, 24);buffer.writeUInt32LE(32000, 28)
  buffer.writeUInt16LE(2, 32);buffer.writeUInt16LE(16, 34);buffer.write('data', 36)
  buffer.writeUInt32LE(samples * 2, 40)
  return buffer
}

async function startFixture(fixture) {
  const fixtureId = fileURLToPath(new URL('../src/__recording-original-media-fixture.vue', import.meta.url)).replaceAll('\\', '/')
  const server = await createServer({
    server: { host: '127.0.0.1', port: 0 },
    plugins: [{ name: 'recording-original-media-fixture',
      resolveId(id) { if (id === '/__recording-original-media-fixture.vue') return fixtureId },
      load(id) { if (id === fixtureId) return fixture },
      configureServer(dev) { dev.middlewares.use(async (req, res, next) => {
        if (req.url?.startsWith('/__fixture-audio/')) {
          res.setHeader('Content-Type', 'audio/wav');res.end(pcmFixture());return
        }
        if (req.url !== '/__recording-original-media-test') return next()
        const html = await dev.transformIndexHtml(req.url, '<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="icon" href="data:,"><div id="app"></div><script type="module">import {createApp} from "vue";import App from "/__recording-original-media-fixture.vue";createApp(App).mount("#app")</script></html>')
        res.setHeader('Content-Type', 'text/html;charset=utf-8');res.end(html)
      }) },
    }],
  })
  await server.listen()
  return { server, url: server.resolvedUrls.local[0] + '__recording-original-media-test' }
}

const mediaOne = {
  audio_asset_id: 101, audio_url: '/__fixture-audio/101.wav', duration_ms: 4500,
  transcript: '第一段录音对应正文',
  timeline: [{ segment_id: 'one-1', index: 1, start_ms: 0, end_ms: 4500, text: '第一段录音对应正文', safe_cut: true }],
  srt: '1\n00:00:00,000 --> 00:00:04,500\n第一段字幕\n',
  safe_points: [{ at_ms: 4500, grade: 'A', score: 96, sentence_id: 'one-1' }],
  asset_manifest: { tag: 'first' },
  voice_identity: { voice_id: 'voice-one', name: '保留的自定义音色', version: 1 },
}
const mediaTwo = {
  audio_asset_id: 202, audio_url: '/__fixture-audio/202.wav', duration_ms: 6200,
  transcript: '第二段录音对应正文',
  timeline: [{ segment_id: 'two-1', index: 1, start_ms: 0, end_ms: 6200, text: '第二段录音对应正文', safe_cut: true }],
  srt: '1\n00:00:00,000 --> 00:00:06,200\n第二段字幕\n',
  safe_points: [{ at_ms: 6200, grade: 'A', score: 96, sentence_id: 'two-1' }],
  asset_manifest: { tag: 'second' },
  voice_identity: { voice_id: 'voice-two', name: '第二个自定义音色', version: 1 },
}

async function localContext(browser, errors, externalRequests, options = {}) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, ...options })
  await context.route('**/*', async route => {
    if (new URL(route.request().url()).hostname === '127.0.0.1') return route.continue()
    externalRequests.push(route.request().url());await route.abort()
  })
  context.on('page', page => {
    page.setDefaultTimeout(10000)
    page.on('pageerror', error => errors.push(error.message))
  })
  return context
}

async function testDisplayComponent(browser, checks, layouts, errors, externalRequests) {
  const fixture = `<script setup lang="ts">
import {ref} from 'vue'
import RecordingOriginalMedia from '/src/components/RecordingOriginalMedia.vue'
import '/src/style.css'
const draft=ref(${JSON.stringify(mediaOne)}),busy=ref(false),cloneBusy=ref(false),events=ref([])
window.fixture={set:value=>{draft.value=value.draft||draft.value;busy.value=!!value.busy;cloneBusy.value=!!value.cloneBusy},events:()=>[...events.value]}
</script>
<template><main style="max-width:1050px;margin:20px auto;padding:16px;box-sizing:border-box"><RecordingOriginalMedia :draft="draft" :audio-src="draft.audio_url" duration-label="00:04" :busy="busy" :clone-busy="cloneBusy" @remove="events.push('remove')" @retry-clone="events.push('retryClone')"/></main></template>`
  const { server, url } = await startFixture(fixture)
  const context = await localContext(browser, errors, externalRequests)
  try {
    const page = await context.newPage();await page.goto(url)
    const media = page.locator('.recording-original-media')
    await media.waitFor()
    assert.equal(await media.locator('audio').getAttribute('src'), mediaOne.audio_url)
    assert.equal(await media.getByRole('button', { name: '移除原音和 SRT', exact: true }).count(), 1)
    await media.locator('summary').filter({ hasText: '查看 SRT' }).click()
    assert.equal(await media.locator('pre').textContent(), mediaOne.srt)
    await media.getByRole('button', { name: '移除原音和 SRT', exact: true }).click()
    assert.deepEqual(await page.evaluate(() => window.fixture.events()), ['remove'])
    checks.push('真实原音组件展示传入音频和对应SRT；移除入口仅发remove事件')
    await page.evaluate(value => window.fixture.set({ draft: { ...value, voice_identity: null, srt: '  ' } }), mediaOne)
    assert.equal(await media.locator('details').count(), 0)
    const retry = media.getByRole('button', { name: '重新生成自定义音色', exact: true })
    assert.ok(await retry.evaluate(element => parseFloat(getComputedStyle(element).fontSize) >= 16))
    await retry.click()
    assert.deepEqual(await page.evaluate(() => window.fixture.events()), ['remove', 'retryClone'])
    for (const flags of [{ busy: true }, { cloneBusy: true }]) {
      await page.evaluate(value => window.fixture.set(value), flags)
      assert.equal(await media.getByRole('button', { name: '移除原音和 SRT', exact: true }).isDisabled(), true)
      assert.equal(await media.locator('.recording-original-media__retry').isDisabled(), true)
    }
    checks.push('空SRT不显示空面板；重试音色事件正确，处理或克隆期间禁用移除及重试')
    await page.evaluate(value => window.fixture.set({ draft: value }), mediaOne)
    for (const width of [360, 390, 768, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      const layout = await media.evaluate(element => ({ width: innerWidth, scrollWidth: document.documentElement.scrollWidth,
        left: element.getBoundingClientRect().left, right: element.getBoundingClientRect().right,
        audioWidth: element.querySelector('audio').getBoundingClientRect().width,
        textFonts: [...element.querySelectorAll('strong,.recording-original-media__item>div>span,.recording-original-media__srt pre,.recording-original-media__srt summary,button')].map(item => ({ tag: item.tagName, size: parseFloat(getComputedStyle(item).fontSize) })),
      }))
      assert.ok(layout.scrollWidth <= width && layout.left >= 0 && layout.right <= width && layout.audioWidth > 0, JSON.stringify(layout))
      assert.ok(layout.textFonts.length > 0 && layout.textFonts.every(item => item.size >= 16), JSON.stringify(layout.textFonts))
      layouts.push(layout)
      if (width === 390) await page.screenshot({ path: output + '/component-mobile.png', fullPage: true })
    }
    checks.push('原音组件360/390/768/1440宽度音频控件与SRT无横向溢出，标题/辅助文字/摘要/SRT/按钮计算字号均至少16px')
  } finally {
    await context.close();await server.close()
  }
}

function productionWatch(containing) {
  const statement = ast.statements.find(node => ts.isExpressionStatement(node) && node.getText(ast).startsWith('watch(') && node.getText(ast).includes(containing))
  assert.ok(statement, `Missing actual watcher: ${containing}`)
  return statement.getText(ast)
}

function parentFixture() {
  const functions = [
    'recordingOriginalForTab', 'addRecordingTab', 'beginRecordingTabFromUpload', 'resetRecordingTabs', 'removeRecordingTab', 'clearUnreferencedRecordingFormalAudio',
    'recordingScriptForSave', 'restoreRecordingScript', 'recordingOriginalRequestIsCurrent', 'removeRecordingOriginal',
    'defaultFullShowVoiceState', 'formatFullShowTimelineMS', 'asRecord', 'originalSourceManifest',
    'bytesToBase64', 'base64ToBytes', 'compressVersionJSON', 'decompressVersionJSON',
    'compactOriginalSourceManifest', 'expandOriginalSourceManifest', 'recordingOriginalSourcesForSave', 'restoreRecordingOriginalSources',
    'buildOriginalTimeline', 'uploadCustomMainline', 'rebuildCustomMainline', 'retryCustomMainlineClone',
  ].map(functionSource).join('\n')
  const computed = ['activeRecordingTab', 'activeRecordingTabText', 'customMainlineTranscript', 'activeRecordingOriginal', 'showRecordingOriginalMedia', 'recordingOriginalBusy'].map(variableSource).join('\n')
  const component = source.match(/<RecordingOriginalMedia\b[\s\S]*?\/>/)?.[0]
  assert.ok(component?.includes('showRecordingOriginalMedia'), 'Use the real parent display gate')
  return `<script setup lang="ts">
import {computed,ref,shallowRef,watch,nextTick} from 'vue'
import RecordingOriginalMedia from '/src/components/RecordingOriginalMedia.vue'
import RecordingScriptTabs from '/src/components/RecordingScriptTabs.vue'
import '/src/style.css'
type RecordingScriptTab={id:string;title:string;text:string;audioAssetIds:number[]}
type LiveAgentCustomMainlineResponse=any
type LiveAgentPlanTimelineSegment=any
type LiveAgentPlanSafePoint=any
type LiveAgentPlanVersion=any
type LiveAgentVoiceIdentity=any
type FullShowVoiceState=any
const copy=value=>JSON.parse(JSON.stringify(value))
const recordingTabs=ref<RecordingScriptTab[]>([]),activeRecordingTabId=ref('t1'),recordingTabDrafts=ref<Record<string,LiveAgentCustomMainlineResponse>>({})
let recordingTabSeq=1,planVersionSaveEpoch=0,fullShowWorkspaceLoadToken=0,customMainlineCloneRequestNo=0,customMainlineAbort:AbortController|null=null
const recordingVoiceMode=ref<'original'|'stream_clone'>('original'),customMainlineDraft=ref(null),customMainlineTimeline=ref([])
const customMainlineStage=ref('idle'),customMainlineSaveNotice=ref(''),customMainlineError=ref(''),customMainlineCloneBusy=ref(false)
const fullShowVersionSaving=ref(false),fullShowSavedVersion=ref(null),fullShowSaveReceipt=ref(null),fullShowVoiceStates=ref({}),fullShowResult=ref(null)
const customMainlineFiles=shallowRef<File[]>([]),customMainlineFile=ref<File|null>(null),customMainlineDurationMS=ref(0),customMainlineVoiceSample=ref(null),customMainlineUploadProgress=ref(0)
const currentRoomPlanId=ref(5),activeRoom=ref({id:29,tenant_id:14}),activeRoomId=computed(()=>activeRoom.value?.id)
const contentModeAccess=ref({content_mode:'user_audio'}),contentModeReady=ref(true),isUserAudioContentMode=ref(true),fullShowOutputMode=ref('custom')
const session={bootstrap:{actor:{user_id:28,tenant_id:14,role:'customer'}}},props={supportSession:undefined,supportTenantId:undefined,supportRoomId:undefined}
const fullShowWorkspaceVoiceIdentity=ref({voice_id:'keep-plan-voice',name:'方案自选音色',version:'V1'})
const currentVoiceIdentity=computed(()=>fullShowWorkspaceVoiceIdentity.value),currentVoiceIdentityKey=ref('keep-plan-voice-key')
const fullShowFormalVariantKeys=ref(['CUSTOM']),selectedFullShowVariantKey=ref('CUSTOM'),fullShowVersionError=ref(''),fullShowNotice=ref('')
const fullShowPreviewReady=ref(true),originalAiCandidateKey=ref(''),originalAiSrtProgress=ref({status:'idle',percent:0,stage:'',error:''})
let contentModeDisposed=false
const apiCalls:any[]=[],pending:any[]=[]
function deferred(kind:string,args:any){return new Promise((resolve,reject)=>{apiCalls.push({kind,args:copy(args)});pending.push({kind,resolve,reject})})}
async function uploadLiveAgentCustomMainline(...args:any[]){return deferred('upload',{plan:args[0],room:args[1],count:args[2].length})}
async function rebuildLiveAgentCustomMainline(plan:any,body:any){return deferred('rebuild',{plan,body})}
async function cloneLiveVoiceProfile(body:any){return deferred('clone',body)}
async function chooseContentMode(value:any){contentModeAccess.value={content_mode:value}}
function selectFullShowOutputMode(value:any){fullShowOutputMode.value=value}
function updateFullShowVoiceState(key:string,value:any){fullShowVoiceStates.value[key]={...fullShowVoiceStates.value[key],...value}}
function liveSupportMediaPlaybackURL(value:string){return value}
${computed}
${functions}
${productionWatch('watch(activeRecordingOriginal')}
${productionWatch('planVersionSaveEpoch++')}
const mediaOne=${JSON.stringify(mediaOne)},mediaTwo=${JSON.stringify(mediaTwo)}
const initial=[{id:'t1',title:'开场',text:'标签一的编辑正文',audioAssetIds:[101]},{id:'t2',title:'塑品',text:'标签二的编辑正文',audioAssetIds:[202]},{id:'t3',title:'纯文字',text:'没有上传录音的独立正文',audioAssetIds:[]}]
const scope=ref(0)
function setState(value:any={}){
 planVersionSaveEpoch++;fullShowWorkspaceLoadToken++
 customMainlineAbort?.abort();customMainlineAbort=null;customMainlineCloneRequestNo++
 customMainlineCloneBusy.value=false;customMainlineStage.value='idle';fullShowVersionSaving.value=false
 recordingTabDrafts.value={};recordingTabs.value=copy(value.tabs||initial);activeRecordingTabId.value=value.activeId||recordingTabs.value[0].id
 recordingVoiceMode.value=value.mode||'original';recordingTabDrafts.value=copy(value.drafts||{t1:mediaOne,t2:mediaTwo})
 fullShowVoiceStates.value=copy(value.voiceStates||{CUSTOM:{audio_asset_id:101,audio_url:mediaOne.audio_url,srt:mediaOne.srt,timeline:mediaOne.timeline},OTHER:{audio_asset_id:202,audio_url:mediaTwo.audio_url,srt:mediaTwo.srt,timeline:mediaTwo.timeline}})
 fullShowWorkspaceVoiceIdentity.value={voice_id:'keep-plan-voice',name:'方案自选音色',version:'V1'}
 fullShowSavedVersion.value={id:999};fullShowSaveReceipt.value={id:999};fullShowResult.value={variants:[]}
 customMainlineFile.value=new File(['local'],'测试录音.wav',{type:'audio/wav'});customMainlineFiles.value=[customMainlineFile.value]
 customMainlineError.value='';customMainlineSaveNotice.value='';customMainlineUploadProgress.value=0
 recordingTabSeq=recordingTabs.value.length;scope.value++
}
setState()
window.fixture={set:setState,select:id=>activeRecordingTabId.value=id,mode:value=>recordingVoiceMode.value=value,
 state:()=>copy({tabs:recordingTabs.value,active:activeRecordingTabId.value,drafts:recordingTabDrafts.value,activeOriginal:activeRecordingOriginal.value,globalDraft:customMainlineDraft.value,globalTimeline:customMainlineTimeline.value,shown:showRecordingOriginalMedia.value,busy:recordingOriginalBusy.value,cloneBusy:customMainlineCloneBusy.value,stage:customMainlineStage.value,voiceStates:fullShowVoiceStates.value,identity:fullShowWorkspaceVoiceIdentity.value,notice:customMainlineSaveNotice.value,error:customMainlineError.value,epoch:planVersionSaveEpoch}),
 staleGlobal:value=>{customMainlineDraft.value=value;customMainlineTimeline.value=value.timeline},
 flags:value=>{fullShowVersionSaving.value=!!value.saving;customMainlineStage.value=value.stage||'idle';customMainlineCloneBusy.value=!!value.cloneBusy},
 remove:removeRecordingOriginal,
 removeTab:removeRecordingTab,
 save:async()=>({recording_script:recordingScriptForSave(),recording_voice_mode:recordingVoiceMode.value,recording_original_sources:await recordingOriginalSourcesForSave()}),
 load:async version=>{resetRecordingTabs();recordingVoiceMode.value=version.generation_context?.recording_voice_mode||'original';restoreRecordingScript(version.generation_context);await restoreRecordingOriginalSources(version)},
 restore:restoreRecordingOriginalSources,
 compact:compactOriginalSourceManifest,expand:expandOriginalSourceManifest,
 upload:uploadCustomMainline,rebuild:rebuildCustomMainline,clone:retryCustomMainlineClone,
 calls:()=>copy(apiCalls),pending:()=>pending.map(item=>item.kind),
 resolve:(kind,value)=>{const index=pending.findIndex(item=>item.kind===kind);if(index<0)throw Error('No pending '+kind);pending.splice(index,1)[0].resolve(value)},
 reject:(kind,message)=>{const index=pending.findIndex(item=>item.kind===kind);if(index<0)throw Error('No pending '+kind);pending.splice(index,1)[0].reject(Error(message))},
 switchScope:()=>{currentRoomPlanId.value++;activeRoom.value={id:activeRoom.value.id+1,tenant_id:14};setState()},
 bumpLoadToken:()=>fullShowWorkspaceLoadToken++,
}
</script>
<template><main class="recording-media-parent-fixture" style="max-width:1100px;margin:24px auto;padding:20px;background:#fff;border:1px solid #e0e7f3;border-radius:20px;box-sizing:border-box;min-width:0">
 <h1 style="font-size:26px;margin:0 0 18px">原始话术</h1>
 <div style="display:flex;gap:8px;margin-bottom:18px"><button type="button" @click="recordingVoiceMode='original'">原始声音</button><button type="button" @click="recordingVoiceMode='stream_clone'">AI 声音</button></div>
 <RecordingScriptTabs :key="scope" :tabs="recordingTabs" :active-id="activeRecordingTabId" @select="activeRecordingTabId=$event" @add="addRecordingTab"/>
 <label style="display:flex;gap:14px;align-items:center;margin-top:20px"><span>标题</span><input aria-label="标题" v-model="activeRecordingTab.title" style="width:0;min-width:0;flex:1"/></label>
 <textarea aria-label="话术正文" v-model="activeRecordingTab.text" style="width:100%;box-sizing:border-box;min-height:140px;margin:15px 0"/>
 ${component}
</main></template>`
}

async function testParentAssociation(browser, checks, layouts, errors, externalRequests) {
  const { server, url } = await startFixture(parentFixture())
  const context = await localContext(browser, errors, externalRequests)
  try {
    const page = await context.newPage();await page.goto(url)
    await page.getByRole('tab', { name: /开场/ }).waitFor()
    const media = page.locator('.recording-original-media')
    const state = () => page.evaluate(() => window.fixture.state())
    const set = async value => { await page.evaluate(value => window.fixture.set(value), value || {});await page.getByRole('tab').first().waitFor() }
    const select = async id => { await page.locator(`[data-recording-tab-id="${id}"] .recording-script-tab-select`).click() }
    const remove = () => media.getByRole('button', { name: '移除原音和 SRT', exact: true })
    const version = (generation_context, variants = []) => ({ id: 88, plan_id: 5, tenant_id: 14, room_id: 29, version_no: 8, generation_context, variants, voice_identity: { voice_id: 'fallback', name: '版本音色', version: 'V1' } })
    const start = async kind => {
      await page.evaluate(kind => { window.fixture.operations ||= [];window.fixture.operations.push(window.fixture[kind]()) }, kind)
      await page.waitForFunction(kind => window.fixture.pending().includes(kind === 'clone' ? 'clone' : kind === 'rebuild' ? 'rebuild' : 'upload'), kind)
    }
    const resolve = async (kind, value, operation = -1) => {
      await page.evaluate(({ kind, value }) => window.fixture.resolve(kind, value), { kind, value })
      await page.evaluate(index => window.fixture.operations.at(index), operation)
    }
    await set({ mode: 'stream_clone' })
    assert.equal(await media.count(), 0)
    assert.equal((await state()).activeOriginal.audio_asset_id, 101)
    await page.getByRole('button', { name: '原始声音', exact: true }).click()
    await media.waitFor()
    assert.equal(await media.locator('audio').getAttribute('src'), mediaOne.audio_url)
    checks.push('使用真实父展示节点：AI模式入页隐藏原音/SRT；切原声才显示当前标签的关联媒体')
    await select('t3')
    await page.evaluate(value => window.fixture.staleGlobal(value), mediaOne)
    assert.equal(await media.count(), 0)
    assert.equal((await state()).activeOriginal, null)
    await set({ drafts: { t1: mediaTwo, t2: mediaTwo } })
    assert.equal(await media.count(), 0)
    await set({ drafts: { t1: { ...mediaOne, audio_url: '' }, t2: mediaTwo } })
    assert.equal(await media.count(), 0)
    checks.push('无关联ID、ID不匹配或无音频地址均不显示；陈旧global draft不能让纯文字标签显示原音')
    await set()
    await media.locator('audio').evaluate(element => element.dataset.originalAudio = 'first')
    await media.locator('summary').filter({ hasText: '查看 SRT' }).click()
    assert.equal(await media.locator('pre').textContent(), mediaOne.srt)
    await select('t2')
    assert.equal(await media.locator('audio').getAttribute('src'), mediaTwo.audio_url)
    assert.equal(await media.locator('audio').getAttribute('data-original-audio'), null)
    const details = media.locator('details')
    if (!(await details.evaluate(element => element.open))) await details.locator('summary').click()
    assert.equal(await media.locator('pre').textContent(), mediaTwo.srt)
    assert.equal((await state()).globalTimeline[0].segment_id, 'two-1')
    await select('t1')
    assert.equal(await media.locator('audio').getAttribute('src'), mediaOne.audio_url)
    checks.push('多标签切换同步音频、SRT与时间轴，audio按asset ID重建，不沿用前一录音播放元素')

    for (const flags of [{ stage: 'uploading' }, { stage: 'analyzing' }, { stage: 'saving' }, { saving: true }, { cloneBusy: true }]) {
      await set();await page.evaluate(value => window.fixture.flags(value), flags)
      assert.equal(await remove().isDisabled(), true)
      const before = await state();await page.evaluate(() => window.fixture.remove())
      assert.deepEqual((await state()).tabs, before.tabs);assert.deepEqual((await state()).drafts, before.drafts)
      const callsBefore = await page.evaluate(() => window.fixture.calls().length)
      await page.evaluate(() => window.fixture.upload())
      assert.equal(await page.evaluate(() => window.fixture.calls().length), callsBefore)
    }
    checks.push('上传/分析/保存/版本保存/克隆期间移除入口禁用，真实父删除函数也拒绝修改')
    await set()
    const before = await state()
    await remove().click()
    const removed = await state()
    assert.deepEqual(removed.tabs.map(tab => ({ id: tab.id, title: tab.title, text: tab.text })), before.tabs.map(tab => ({ id: tab.id, title: tab.title, text: tab.text })))
    assert.deepEqual(removed.tabs[0].audioAssetIds, [])
    assert.equal(removed.drafts.t1, undefined);assert.deepEqual(removed.drafts.t2, before.drafts.t2)
    assert.deepEqual(removed.voiceStates.OTHER, before.voiceStates.OTHER)
    assert.equal(removed.voiceStates.CUSTOM.audio_asset_id, 0)
    assert.deepEqual(removed.identity, before.identity)
    assert.equal(removed.globalDraft, null);assert.deepEqual(removed.globalTimeline, [])
    assert.equal(await media.count(), 0)
    assert.match(removed.notice, /未保存|请保存/)
    checks.push('移除原音/SRT保留标签、标题、正文、其它录音及方案音色；仅清除直接引用该原音的正式音轨，并提示未保存')
    const savedAfterRemoval = await page.evaluate(() => window.fixture.save())
    assert.deepEqual(savedAfterRemoval.recording_script.tabs[0].audio_asset_ids, [])
    assert.deepEqual(savedAfterRemoval.recording_original_sources.map(item => item.tab_id), ['t2'])
    const staleVariant = { variant_key: 'CUSTOM', text: mediaOne.transcript, ...mediaOne, asset_manifest: { original_source: mediaOne } }
    await page.evaluate(value => window.fixture.load(value), version(savedAfterRemoval, [staleVariant]))
    assert.equal((await state()).activeOriginal, null);assert.equal(await media.count(), 0)
    assert.deepEqual((await state()).tabs[0].audioAssetIds, [])
    await select('t2');assert.equal(await media.locator('audio').getAttribute('src'), mediaTwo.audio_url)
    checks.push('移除后真实归档helper只保存剩余标签媒体；JSON保存/恢复后不被旧variant.original_source复活，其它录音照常回显')
    await set({ voiceStates: { CUSTOM: { audio_asset_id: 909, audio_url: '/__fixture-audio/909.wav', asset_manifest: { original_source: mediaOne } }, OTHER: before.voiceStates.OTHER } })
    const aiFormal = (await state()).voiceStates.CUSTOM
    await remove().click();assert.deepEqual((await state()).voiceStates.CUSTOM, aiFormal)
    await set({ tabs: [{ id: 't1', title: '甲', text: '甲', audioAssetIds: [101] }, { id: 't2', title: '乙', text: '乙', audioAssetIds: [101] }], drafts: { t1: mediaOne, t2: mediaOne } })
    await remove().click();assert.equal((await state()).voiceStates.CUSTOM.audio_asset_id, 101)
    await select('t2');assert.equal(await media.locator('audio').getAttribute('src'), mediaOne.audio_url)
    checks.push('移除原音不清仍有效的AI正式声音；其它标签共用该录音时保留有效正式音轨和关联')
    await set()
    await page.evaluate(() => window.fixture.removeTab('t1'))
    assert.equal((await state()).voiceStates.CUSTOM.audio_asset_id, 0)
    assert.equal((await state()).voiceStates.OTHER.audio_asset_id, 202)
    assert.deepEqual((await state()).tabs.map(tab => tab.id), ['t2', 't3'])
    assert.match((await state()).notice, /未保存/)
    await set({ tabs: [{ id: 't1', title: '甲', text: '甲', audioAssetIds: [101] }, { id: 't2', title: '乙', text: '乙', audioAssetIds: [101] }, { id: 't3', title: '丙', text: '丙', audioAssetIds: [] }], drafts: { t1: mediaOne, t2: mediaOne } })
    await page.evaluate(() => window.fixture.removeTab('t1'))
    assert.equal((await state()).voiceStates.CUSTOM.audio_asset_id, 101)
    await page.evaluate(() => window.fixture.removeTab('t2'))
    assert.equal((await state()).voiceStates.CUSTOM.audio_asset_id, 0)
    assert.equal((await state()).tabs[0].id, 't3')
    checks.push('删除完整标签同样清理末次直接引用的正式原音；仍被其它标签共用时保留，最后引用删除后才清理')

    await set()
    const saved = JSON.parse(JSON.stringify(await page.evaluate(() => window.fixture.save())))
    assert.deepEqual(saved.recording_original_sources.map(item => item.tab_id), ['t1', 't2'])
    const expanded = await page.evaluate(async sources => Promise.all(sources.map(async entry => ({ tab_id: entry.tab_id, source: await window.fixture.expand(entry.source) }))), saved.recording_original_sources)
    assert.equal(expanded[0].source.transcript, mediaOne.transcript)
    assert.equal(expanded[1].source.transcript, mediaTwo.transcript)
    assert.equal(expanded[0].source.srt, mediaOne.srt);assert.equal(expanded[1].source.srt, mediaTwo.srt)
    assert.deepEqual(expanded.map(item => item.source.audio_asset_id), [101, 202])
    await page.evaluate(value => window.fixture.load(value), version(saved))
    assert.equal((await state()).drafts.t1.srt, mediaOne.srt);assert.equal((await state()).drafts.t2.srt, mediaTwo.srt)
    assert.equal((await state()).tabs[0].text, '标签一的编辑正文')
    assert.equal((await state()).tabs[1].text, '标签二的编辑正文')
    checks.push('逐标签元数据经真实压缩/展开/恢复往返保持对应音频、SRT、原始转写；不把全标签正文混成某一份原音字幕')
    const explicitScript = { tabs: [{ tab_id: 't1', title: '纯文案', text: '明确无录音', audio_asset_ids: [] }], active_tab_id: 't1' }
    await page.evaluate(value => window.fixture.load(value), version({ recording_script: explicitScript }, [staleVariant]))
    assert.equal((await state()).activeOriginal, null);assert.equal((await state()).tabs[0].text, '明确无录音')
    await page.evaluate(value => window.fixture.load(value), version({ recording_script: explicitScript, recording_original_sources: [{ tab_id: 't1', source: mediaOne }] }, [staleVariant]))
    assert.equal((await state()).activeOriginal, null)
    await page.evaluate(value => window.fixture.load(value), version({}, [staleVariant]))
    assert.deepEqual((await state()).tabs[0].audioAssetIds, [101])
    assert.equal((await state()).activeOriginal.audio_asset_id, 101)
    checks.push('显式空audio IDs不从旧归档或错误新归档猜绑；真正无recording_script的单标签legacy才可迁移')
    const legacyCandidates = [
      { variant_key: 'CUSTOM', text: mediaTwo.transcript, ...mediaTwo },
      { variant_key: 'A', text: 'AI正式稿', asset_manifest: { original_source: mediaOne } },
    ]
    await page.evaluate(value => window.fixture.load(value), version({}, legacyCandidates))
    assert.deepEqual((await state()).tabs[0].audioAssetIds, [101])
    assert.equal((await state()).activeOriginal.audio_asset_id, 101)
    const explicitTwo = { tabs: [{ tab_id: 't1', title: '甲', text: '甲', audio_asset_ids: [101] }, { tab_id: 't2', title: '乙', text: '乙', audio_asset_ids: [202] }], active_tab_id: 't1' }
    await page.evaluate(value => window.fixture.load(value), version({ recording_script: explicitTwo }, legacyCandidates))
    assert.equal((await state()).drafts.t1.audio_asset_id, 101)
    assert.equal((await state()).drafts.t2.audio_asset_id, 202)
    checks.push('legacy双候选优先original_source且单标签仅迁移一次；显式多标签仍逐ID恢复全部匹配来源')

    await set()
    await start('rebuild')
    assert.equal((await state()).stage, 'saving')
    const rebuildBody = (await page.evaluate(() => window.fixture.calls())).at(-1).args.body
    assert.equal(rebuildBody.audio_asset_id, 101)
    assert.equal(rebuildBody.timeline.map(item => item.text).join(''), '标签一的编辑正文')
    await select('t2')
    await resolve('rebuild', { ...mediaOne, srt: '当前标签新SRT', timeline: [{ ...mediaOne.timeline[0], text: '标签一的编辑正文' }] })
    assert.equal((await state()).drafts.t1.srt, '当前标签新SRT')
    assert.equal((await state()).active, 't2');assert.equal((await state()).globalDraft.audio_asset_id, 202)
    assert.equal(await media.locator('audio').getAttribute('src'), mediaTwo.audio_url)
    checks.push('SRT重建只发送目标标签正文；切标签后回执更新原目标，不串到新当前标签')
    await set();await start('rebuild');await page.evaluate(() => window.fixture.switchScope())
    const afterSwitchRebuild = await state()
    await resolve('rebuild', { ...mediaOne, srt: '过期场景字幕' })
    assert.deepEqual(await state(), afterSwitchRebuild)
    await set({ drafts: { t1: { ...mediaOne, voice_identity: null }, t2: mediaTwo } })
    await start('clone');await page.evaluate(() => window.fixture.switchScope())
    const afterSwitchClone = await state()
    await resolve('clone', { id: 900, name: '过期克隆', provider: 'test', voice_id: 'stale-voice', config: {} })
    assert.deepEqual(await state(), afterSwitchClone)
    checks.push('真实重建/克隆函数的旧scope异步回执不修改新方案草稿、SRT、音色、busy或提示')
    await set({ drafts: { t1: { ...mediaOne, voice_identity: null }, t2: mediaTwo } });await start('clone')
    await page.evaluate(() => window.fixture.switchScope());await start('clone')
    await resolve('clone', { id: 901, name: '旧请求', provider: 'test', voice_id: 'old-voice', config: {} }, -2)
    assert.equal((await state()).cloneBusy, true)
    await resolve('clone', { id: 902, name: '新请求', provider: 'test', voice_id: 'new-voice', config: {} })
    assert.equal((await state()).cloneBusy, false)
    assert.equal((await state()).drafts.t1.voice_identity.voice_id, 'new-voice')
    assert.equal((await state()).identity.voice_id, 'keep-plan-voice')
    checks.push('旧克隆finally不解锁新scope正在处理的请求，新克隆回执只更新对应录音身份并保留方案音色')
    await set();await start('upload')
    await page.evaluate(() => window.fixture.switchScope());await start('upload')
    await resolve('upload', { ...mediaOne, audio_asset_id: 303, audio_url: '/__fixture-audio/303.wav' }, -2)
    assert.equal((await state()).stage, 'uploading')
    assert.equal((await state()).tabs.length, 3)
    await resolve('upload', { ...mediaTwo, audio_asset_id: 404, audio_url: '/__fixture-audio/404.wav', transcript: '新scope上传录音' })
    assert.equal((await state()).tabs.length, 4)
    assert.deepEqual((await state()).tabs.at(-1).audioAssetIds, [404])
    assert.equal((await state()).tabs.at(-1).text, '新scope上传录音')
    assert.equal((await state()).activeOriginal.audio_asset_id, 404)
    assert.equal((await state()).stage, 'ready')
    checks.push('旧上传成功及finally不影响新scope上传；当前上传才新建标签、绑定asset ID并回显自身音频')
    await set()
    const staleRestore = version(saved)
    await page.evaluate(value => {
      window.fixture.restoreOperation = window.fixture.restore(value)
      window.fixture.switchScope()
    }, staleRestore)
    const afterRestoreSwitch = await state()
    await page.evaluate(() => window.fixture.restoreOperation)
    assert.deepEqual(await state(), afterRestoreSwitch)
    await set()
    await page.evaluate(value => {
      window.fixture.restoreOperation = window.fixture.restore(value)
      window.fixture.bumpLoadToken()
    }, staleRestore)
    const afterLoadTokenBump = await state()
    await page.evaluate(() => window.fixture.restoreOperation)
    assert.deepEqual(await state(), afterLoadTokenBump)
    checks.push('原音归档异步恢复中切scope或加载令牌变化，旧恢复结果不覆盖新标签媒体')
    await set()
    await media.locator('summary').filter({ hasText: '查看 SRT' }).click()
    for (const width of [390, 768, 1440]) {
      await page.setViewportSize({ width, height: 1000 })
      const layout = await page.locator('.recording-media-parent-fixture').evaluate(element => ({ width: innerWidth, scrollWidth: document.documentElement.scrollWidth,
        right: element.getBoundingClientRect().right, mediaRight: element.querySelector('.recording-original-media').getBoundingClientRect().right,
      }))
      assert.ok(layout.scrollWidth <= width && layout.right <= width && layout.mediaRight <= width, JSON.stringify(layout))
      layouts.push(layout)
      if (width === 390) await page.screenshot({ path: output + '/parent-mobile.png', fullPage: true })
      if (width === 1440) await page.screenshot({ path: output + '/parent-desktop.png', fullPage: true })
    }
    checks.push('真实父展示门控+标签+音频/SRT在390/768/1440宽度无横向溢出')
    assert.match(source, /recording_original_sources:\s*recordingOriginalSources\b/)
  } finally {
    await context.close();await server.close()
  }
}

function testSavePayloadShape(checks) {
  const save = ast.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === 'saveFullShowVersion')
  assert.ok(save, 'Actual version save function must exist')
  let topLevelArchives = 0, legacyWrites = 0, legacyDeletes = 0, archiveCalls = 0
  const visit = node => {
    if (ts.isPropertyAssignment(node)) {
      const name = node.name.getText(ast).replace(/^['"]|['"]$/g, '')
      if (name === 'recording_original_sources') {
        topLevelArchives++
        assert.equal(node.initializer.getText(ast), 'recordingOriginalSources')
      }
      if (name === 'original_source') legacyWrites++
    }
    if (ts.isDeleteExpression(node) && ts.isPropertyAccessExpression(node.expression) && node.expression.name.text === 'original_source') legacyDeletes++
    if (ts.isCallExpression(node) && node.expression.getText(ast) === 'recordingOriginalSourcesForSave') archiveCalls++
    ts.forEachChild(node, visit)
  }
  visit(save)
  assert.equal(topLevelArchives, 1);assert.equal(archiveCalls, 1)
  assert.equal(legacyWrites, 0);assert.ok(legacyDeletes > 0)
  checks.push('实际saveFullShowVersion AST确认唯一顶层逐标签归档并剥离variant旧original_source，避免重复媒体包；未调用真实保存接口')
}

const checks = [], layouts = [], errors = [], externalRequests = []
await mkdir(output, { recursive: true })
const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH || 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
let failure
try {
  await testDisplayComponent(browser, checks, layouts, errors, externalRequests)
  await testParentAssociation(browser, checks, layouts, errors, externalRequests)
  testSavePayloadShape(checks)
  assert.deepEqual(errors, []);assert.deepEqual(externalRequests, [])
  console.log('PASS: ' + checks.join('；'))
} catch (error) {
  failure = error;console.error(error)
} finally {
  await writeFile(output + '/checks.json', JSON.stringify({ passed: !failure, checks, layouts, errors, externalRequests, extractedFunctions, extractedVariables, liveRequests: false, paidCalls: false, error: failure?.stack || null }, null, 2))
  await browser.close()
}
if (failure) throw failure
