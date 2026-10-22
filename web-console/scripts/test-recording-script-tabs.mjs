import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createServer } from 'vite'
import ts from 'typescript'

const require = createRequire(process.env.TAB_TEST_RUNTIME || new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const output = process.env.TAB_TEST_OUTPUT || '../deploy/validation/recording-script-tabs'
const fixtureId = fileURLToPath(new URL('../src/__recording-tabs-fixture.vue', import.meta.url)).replaceAll('\\', '/')
const source = await readFile(new URL('../src/views/LiveStrategyView.vue', import.meta.url), 'utf8')
const marker = '<script setup lang="ts">'
const script = source.slice(source.indexOf(marker) + marker.length, source.indexOf('</script>'))
const ast = ts.createSourceFile('view.ts', script, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
const functionNames = ['addRecordingTab', 'removeRecordingTab', 'reorderRecordingTabs', 'recordingScriptForSave', 'restoreRecordingScript']
const parentFunctions = functionNames.map(name => {
  const node = ast.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === name)
  assert.ok(node, `Missing real parent function: ${name}`)
  return node.getText(ast)
}).join('\n')
const fixture = `<script setup lang="ts">
import {computed,ref} from 'vue'
import RecordingScriptTabs from '/src/components/RecordingScriptTabs.vue'
import '/src/style.css'
type RecordingScriptTab={id:string;title:string;text:string;audioAssetIds:number[]}
type LiveAgentCustomMainlineResponse=Record<string,unknown>
const recordingTabs=ref<RecordingScriptTab[]>([]),activeRecordingTabId=ref('t1')
const recordingTabDrafts=ref<Record<string,LiveAgentCustomMainlineResponse>>({})
let recordingTabSeq=1
const activeRecordingTab=computed(()=>recordingTabs.value.find(tab=>tab.id===activeRecordingTabId.value)||recordingTabs.value[0]||null)
const scope=ref(1),disabled=ref(false),customMainlineSaveNotice=ref(''),customMainlineDraft=ref(null),customMainlineTimeline=ref([])
const customMainlineStage=ref('idle')
${parentFunctions}
const initial=[
 {id:'t1',title:'开场',text:'开场正文，仅属于第一项',audioAssetIds:[101]},
 {id:'t2',title:'塑品',text:'塑品正文，仅属于第二项',audioAssetIds:[202]},
 {id:'t3',title:'痛点',text:'痛点正文，仅属于第三项',audioAssetIds:[303]},
 {id:'t4',title:'功能',text:'功能正文，仅属于第四项',audioAssetIds:[404]},
]
const copy=value=>JSON.parse(JSON.stringify(value))
function setState(value={}){
 recordingTabs.value=copy(value.tabs||initial)
 activeRecordingTabId.value=value.activeId||recordingTabs.value[0].id
 recordingTabDrafts.value=value.drafts||Object.fromEntries(recordingTabs.value.map(tab=>[tab.id,{marker:tab.id}]))
 recordingTabSeq=value.sequence||1
 disabled.value=!!value.disabled;scope.value++
}
setState()
window.fixture={set:setState,state:()=>copy({tabs:recordingTabs.value,active:activeRecordingTabId.value,drafts:recordingTabDrafts.value,disabled:disabled.value}),
 save:recordingScriptForSave,restore:restoreRecordingScript,reorder:reorderRecordingTabs,remove:removeRecordingTab,add:addRecordingTab,
 disable:value=>disabled.value=value,scope:()=>scope.value++,events:[]}
function reorder(ids:string[]){window.fixture.events.push({type:'reorder',ids:[...ids]});reorderRecordingTabs(ids)}
</script>
<template><main class="recording-tabs-fixture" style="max-width:1100px;margin:24px auto;padding:24px;box-sizing:border-box;background:white;border:1px solid #e1e7fa;border-radius:22px;min-width:0">
 <h1 style="font-size:26px;margin:0 0 24px">原始话术</h1>
 <RecordingScriptTabs :key="scope" :tabs="recordingTabs" :active-id="activeRecordingTabId" :disabled="disabled"
   @select="activeRecordingTabId=$event" @add="addRecordingTab" @remove="removeRecordingTab" @reorder="reorder"/>
 <label v-if="activeRecordingTab" style="display:flex;align-items:center;gap:12px;margin-top:24px"><span>标题</span>
   <input aria-label="标题" v-model="activeRecordingTab.title" maxlength="30" style="width:0;flex:1;min-width:0"/>
 </label>
 <textarea v-if="activeRecordingTab" aria-label="话术正文" v-model="activeRecordingTab.text" style="margin-top:20px;width:100%;box-sizing:border-box;min-height:170px"/>
 </main></template>`
const server = await createServer({
  server: { host: '127.0.0.1', port: 0 },
  plugins: [{ name: 'recording-script-tabs-fixture',
    resolveId(id) { if (id === '/__recording-tabs-fixture.vue') return fixtureId },
    load(id) { if (id === fixtureId) return fixture },
    configureServer(dev) { dev.middlewares.use(async (req, res, next) => {
      if (req.url !== '/__recording-tabs-test') return next()
      const html = await dev.transformIndexHtml(req.url, '<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="icon" href="data:,"><div id="app"></div><script type="module">import {createApp} from "vue";import App from "/__recording-tabs-fixture.vue";createApp(App).mount("#app")</script></html>')
      res.setHeader('Content-Type', 'text/html;charset=utf-8');res.end(html)
    }) },
  }],
})
const checks = [], layouts = [], errors = [], externalRequests = []
await mkdir(output, { recursive: true })
await server.listen()
const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH || 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
let failure
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, reducedMotion: 'no-preference' })
  await context.route('**/*', async route => {
    const url = new URL(route.request().url())
    if (url.hostname === '127.0.0.1') return route.continue()
    externalRequests.push(route.request().url());await route.abort()
  })
  const page = await context.newPage()
  page.setDefaultTimeout(10000)
  page.on('pageerror', error => errors.push(error.message))
  const url = server.resolvedUrls.local[0] + '__recording-tabs-test'
  await page.goto(url)
  await page.getByRole('tab', { name: /开场/ }).waitFor()
  const state = () => page.evaluate(() => window.fixture.state())
  const ids = async () => (await state()).tabs.map(tab => tab.id)
  const reset = async value => { await page.evaluate(value => window.fixture.set(value), value || {});await page.getByRole('tab').first().waitFor() }
  const select = id => page.locator(`[data-recording-tab-id="${id}"] .recording-script-tab-select`)
  const remove = id => page.locator(`[data-recording-tab-id="${id}"] .recording-script-tab-remove`)
  const manage = page.getByRole('button', { name: '管理标签删除', exact: true })
  const add = page.getByRole('button', { name: '新增话术标签', exact: true })
  assert.equal(await page.locator('.recording-script-tab-remove:visible').count(), 0)
  assert.equal(await manage.getAttribute('aria-pressed'), 'false')
  await manage.click()
  assert.equal(await manage.getAttribute('aria-pressed'), 'true')
  assert.equal(await page.locator('.recording-script-tab-remove:visible').count(), 4)
  await remove('t3').click()
  assert.deepEqual(await ids(), ['t1', 't2', 't4'])
  assert.equal((await state()).active, 't1')
  assert.equal(Object.hasOwn((await state()).drafts, 't3'), false)
  assert.equal(await page.getByRole('textbox', { name: '话术正文', exact: true }).inputValue(), '开场正文，仅属于第一项')
  checks.push('删除模式隐藏/显示圆形角标；任意删除中间非当前标签，当前正文不跳，删除对应草稿')
  await select('t2').click();await remove('t2').click()
  assert.equal((await state()).active, 't1');assert.deepEqual(await ids(), ['t1', 't4'])
  await remove('t1').click()
  assert.equal((await state()).active, 't4');assert.deepEqual(await ids(), ['t4'])
  assert.equal(await page.getByRole('textbox', { name: '话术正文', exact: true }).inputValue(), '功能正文，仅属于第四项')
  const lastRemove = remove('t4')
  assert.ok((await lastRemove.count()) === 0 || await lastRemove.isDisabled())
  await page.evaluate(() => window.fixture.remove('t4'))
  assert.deepEqual(await ids(), ['t4'])
  checks.push('删除当前项选左邻，删除首项后选新首项，至少保留一个标签且父级有兜底')
  await add.click()
  const added = (await state()).tabs.at(-1)
  assert.equal((await state()).active, added.id)
  assert.deepEqual(added.audioAssetIds, []);assert.equal(added.text, '')
  assert.equal(await page.getByRole('textbox', { name: '标题', exact: true }).inputValue(), added.title)
  checks.push('新增按钮创建并激活独立空标签，标题输入回显，未继承他项正文或声音')

  await reset()
  await select('t3').click()
  const before = await state()
  await page.evaluate(() => window.fixture.reorder(['t4', 't2', 't1', 't3']))
  const reordered = await state()
  assert.deepEqual(reordered.tabs.map(tab => tab.id), ['t4', 't2', 't1', 't3'])
  assert.equal(reordered.active, 't3')
  for (const tab of reordered.tabs) assert.deepEqual(tab, before.tabs.find(item => item.id === tab.id))
  assert.deepEqual(reordered.drafts, before.drafts)
  assert.equal(await page.getByRole('textbox', { name: '话术正文', exact: true }).inputValue(), '痛点正文，仅属于第三项')
  for (const invalid of [['t1'], ['t1', 't1', 't3', 't4'], ['t1', 't2', 't3', 'missing']]) {
    await page.evaluate(order => window.fixture.reorder(order), invalid)
    assert.deepEqual((await state()).tabs, reordered.tabs)
  }
  const saved = await page.evaluate(() => window.fixture.save())
  assert.deepEqual(saved.tabs.map(tab => tab.tab_id), ['t4', 't2', 't1', 't3'])
  assert.deepEqual(saved.tabs.map(tab => tab.order), [0, 1, 2, 3])
  assert.equal(saved.active_tab_id, 't3')
  await reset({ tabs: [{ id: 'new', title: '另一方案', text: '', audioAssetIds: [] }] })
  await page.evaluate(script => window.fixture.restore({ recording_script: script }), saved)
  assert.deepEqual((await state()).tabs, reordered.tabs)
  assert.equal((await state()).active, 't3')
  checks.push('稳定ID重排不串正文/声音/草稿或当前项；拒绝缺项/重复/外来ID；保存order并恢复原顺序及激活项')
  await page.evaluate(() => {
    window.fixture.restore({ recording_script: { tabs: [{ tab_id: 't1', title: '甲', text: '甲正文' }, { tab_id: 't3', title: '丙', text: '丙正文' }], active_tab_id: 't3' } })
  })
  await add.click()
  const sparseIDs = await ids()
  assert.equal(sparseIDs.length, 3);assert.equal(new Set(sparseIDs).size, 3)
  assert.deepEqual(sparseIDs.slice(0, 2), ['t1', 't3'])
  checks.push('删除造成非连续ID后恢复再新增不会撞已有t3')

  await reset()
  await select('t2').focus();await page.keyboard.press('ArrowRight')
  assert.equal((await state()).active, 't3')
  assert.deepEqual(await ids(), ['t1', 't2', 't3', 't4'])
  await select('t3').focus();await page.keyboard.press('Alt+ArrowLeft')
  await page.waitForFunction(() => window.fixture.state().tabs.map(tab => tab.id).join() === 't1,t3,t2,t4')
  assert.equal((await state()).active, 't3')
  checks.push('普通方向键切换标签；Alt+左右改变顺序，正文/当前ID保持')

  await reset()
  const dragFrom = await select('t1').boundingBox(), dragTo = await select('t3').boundingBox()
  assert.ok(dragFrom && dragTo)
  await page.mouse.move(dragFrom.x + dragFrom.width / 2, dragFrom.y + dragFrom.height / 2)
  await page.mouse.down()
  await page.mouse.move(dragTo.x + dragTo.width * .75, dragTo.y + dragTo.height / 2, { steps: 12 })
  await page.locator('.recording-script-tab-ghost').waitFor()
  const animation = await page.locator('.recording-script-tab-item').evaluateAll(items => ({
    moving: items.some(item => item.classList.contains('recording-tab-move') || item.getAnimations().length > 0),
    transition: items.map(item => getComputedStyle(item).transitionDuration),
  }))
  assert.ok(animation.moving || animation.transition.some(value => value.split(',').some(part => Number.parseFloat(part) > 0)), JSON.stringify(animation))
  await page.mouse.up()
  await page.locator('.recording-script-tab-ghost').waitFor({ state: 'detached' })
  assert.notDeepEqual(await ids(), ['t1', 't2', 't3', 't4'])
  const pointerState = await state()
  assert.equal(pointerState.tabs.find(tab => tab.id === 't1').text, '开场正文，仅属于第一项')
  assert.deepEqual(pointerState.tabs.find(tab => tab.id === 't1').audioAssetIds, [101])
  await page.screenshot({ path: output + '/desktop.png', fullPage: true })
  checks.push('真实鼠标Pointer拖拽产生浮层/过渡动画并提交顺序，正文和声音关联保持')
  await reset()
  const escapeFrom = await select('t1').boundingBox(), escapeTo = await select('t3').boundingBox()
  await page.mouse.move(escapeFrom.x + 30, escapeFrom.y + escapeFrom.height / 2);await page.mouse.down()
  await page.mouse.move(escapeTo.x + escapeTo.width * .75, escapeTo.y + escapeTo.height / 2, { steps: 10 })
  await page.locator('.recording-script-tab-ghost').waitFor()
  await page.keyboard.press('Escape');await page.mouse.up()
  assert.deepEqual(await ids(), ['t1', 't2', 't3', 't4'])
  await reset()
  const outsideFrom = await select('t1').boundingBox(), outsideTo = await select('t3').boundingBox()
  await page.mouse.move(outsideFrom.x + 30, outsideFrom.y + outsideFrom.height / 2);await page.mouse.down()
  await page.mouse.move(outsideTo.x + 30, outsideTo.y + outsideTo.height / 2, { steps: 8 })
  await page.locator('.recording-script-tab-ghost').waitFor()
  await page.mouse.move(2, 850, { steps: 6 });await page.mouse.up()
  assert.deepEqual(await ids(), ['t1', 't2', 't3', 't4'])
  checks.push('Escape或组件外释放取消拖动，未提交临时预览顺序')

  await reset();await manage.click();await page.evaluate(() => window.fixture.scope())
  assert.equal(await manage.getAttribute('aria-pressed'), 'false')
  await page.evaluate(() => window.fixture.disable(true))
  assert.equal(await add.isDisabled(), true);assert.equal(await manage.isDisabled(), true)
  const disabledBefore = await state()
  await select('t2').press('Alt+ArrowRight')
  assert.deepEqual((await state()).tabs, disabledBefore.tabs)
  assert.equal((await state()).active, disabledBefore.active)
  checks.push('切换方案作用域重置删除模式；disabled阻止新增、删除管理及键盘排序')
  await reset()
  const manyTabs = Array.from({ length: 12 }, (_, index) => ({ id: `t${index + 1}`, title: `第${index + 1}段话术`, text: `正文${index + 1}`, audioAssetIds: [] }))
  for (const width of [390, 768, 1440]) {
    await page.setViewportSize({ width, height: 900 });await reset({ tabs: manyTabs });await manage.click()
    const layout = await page.locator('.recording-tabs-fixture').evaluate(element => {
      const controls = [...element.querySelectorAll('.recording-script-tab-select,.recording-script-tab-remove')].filter(item => item.getBoundingClientRect().width)
      return { width: innerWidth, scrollWidth: document.documentElement.scrollWidth, right: element.getBoundingClientRect().right,
        overflow: controls.some(item => { const box = item.getBoundingClientRect();return box.left < 0 || box.right > innerWidth }),
        selectFonts: [...element.querySelectorAll('.recording-script-tab-select')].map(item => parseFloat(getComputedStyle(item).fontSize)),
      }
    })
    assert.ok(layout.scrollWidth <= width && layout.right <= width && !layout.overflow, JSON.stringify(layout))
    layouts.push(layout)
    if (width === 390) await page.screenshot({ path: output + '/mobile.png', fullPage: true })
  }
  checks.push('12个标签在390/768/1440屏宽自适应换行，删除角标和内容无横向溢出')

  const touch = await browser.newContext({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true })
  await touch.route('**/*', route => new URL(route.request().url()).hostname === '127.0.0.1' ? route.continue() : route.abort())
  const mobile = await touch.newPage();mobile.on('pageerror', error => errors.push(error.message))
  await mobile.goto(url);await mobile.getByRole('tab').first().waitFor()
  const grip = mobile.locator('[data-recording-tab-id="t1"] .recording-script-tab-grip')
  const touchFrom = await grip.boundingBox(), touchTo = await mobile.locator('[data-recording-tab-id="t3"] .recording-script-tab-select').boundingBox()
  assert.ok(touchFrom && touchTo)
  const session = await touch.newCDPSession(mobile)
  const start = { x: touchFrom.x + touchFrom.width / 2, y: touchFrom.y + touchFrom.height / 2 }
  const end = { x: touchTo.x + touchTo.width * .75, y: touchTo.y + touchTo.height / 2 }
  await session.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ ...start, id: 1 }] })
  for (let step = 1; step <= 12; step++) await session.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: start.x + (end.x - start.x) * step / 12, y: start.y + (end.y - start.y) * step / 12, id: 1 }] })
  await mobile.locator('.recording-script-tab-ghost').waitFor()
  await session.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
  assert.notDeepEqual(await mobile.evaluate(() => window.fixture.state().tabs.map(tab => tab.id)), ['t1', 't2', 't3', 't4'])
  checks.push('390px触屏通过真实Touch输入在六点手柄上拖拽并提交')
  await touch.close()
  assert.deepEqual(errors, []);assert.deepEqual(externalRequests, [])
  console.log('PASS: ' + checks.join('；'))
} catch (error) {
  failure = error
  console.error(error)
} finally {
  await writeFile(output + '/checks.json', JSON.stringify({ passed: !failure, checks, layouts, errors, externalRequests, actualParentFunctions: functionNames, liveRequests: false, error: failure?.stack || null }, null, 2))
  await browser.close();await server.close()
}
if (failure) throw failure
