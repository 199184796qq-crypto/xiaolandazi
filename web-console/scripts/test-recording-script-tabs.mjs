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
const showTabs=ref(true),outsideClicks=ref(0)
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
 disabled.value=!!value.disabled;outsideClicks.value=0;showTabs.value=true;scope.value++
}
setState()
window.fixture={set:setState,state:()=>copy({tabs:recordingTabs.value,active:activeRecordingTabId.value,drafts:recordingTabDrafts.value,disabled:disabled.value,outsideClicks:outsideClicks.value}),
 save:recordingScriptForSave,restore:restoreRecordingScript,reorder:reorderRecordingTabs,remove:removeRecordingTab,add:addRecordingTab,
 disable:value=>disabled.value=value,scope:()=>scope.value++,hide:()=>showTabs.value=false,events:[]}
function reorder(ids:string[]){window.fixture.events.push({type:'reorder',ids:[...ids]});reorderRecordingTabs(ids)}
</script>
<template><main class="recording-tabs-fixture" style="max-width:1100px;margin:24px auto;padding:24px;box-sizing:border-box;background:white;border:1px solid #e1e7fa;border-radius:22px;min-width:0">
 <h1 style="font-size:26px;margin:0 0 24px">原始话术</h1>
 <RecordingScriptTabs v-if="showTabs" :key="scope" :tabs="recordingTabs" :active-id="activeRecordingTabId" :disabled="disabled"
   @select="activeRecordingTabId=$event" @add="addRecordingTab" @remove="removeRecordingTab" @reorder="reorder"/>
 <label v-if="activeRecordingTab" style="display:flex;align-items:center;gap:12px;margin-top:24px"><span>标题</span>
   <input aria-label="标题" v-model="activeRecordingTab.title" maxlength="30" style="width:0;flex:1;min-width:0"/>
 </label>
 <textarea v-if="activeRecordingTab" aria-label="话术正文" v-model="activeRecordingTab.text" style="margin-top:20px;width:100%;box-sizing:border-box;min-height:170px"/>
 <button type="button" aria-label="独立外部操作" style="margin-top:16px" @click="outsideClicks++">外部操作 {{ outsideClicks }}</button>
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
const checks = [], layouts = [], interactionStyles = [], boundaryNotes = [], errors = [], externalRequests = []
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
  const mouseGrip = id => page.locator(`[data-recording-tab-id="${id}"] .recording-script-tab-grip`)
  const remove = id => page.locator(`[data-recording-tab-id="${id}"] .recording-script-tab-remove`)
  const manage = page.getByRole('button', { name: '管理标签删除', exact: true })
  const add = page.getByRole('button', { name: '新增话术标签', exact: true })
  const outside = page.getByRole('button', { name: '独立外部操作', exact: true })
  assert.equal(await page.locator('.recording-script-tab-remove:visible').count(), 0)
  assert.equal(await manage.getAttribute('aria-pressed'), 'false')
  await manage.click()
  assert.equal(await manage.getAttribute('aria-pressed'), 'true')
  assert.equal(await page.locator('.recording-script-tab-remove:visible').count(), 4)
  await remove('t3').locator('svg path').click()
  assert.deepEqual(await ids(), ['t1', 't2', 't4'])
  assert.equal((await state()).active, 't1')
  assert.equal(Object.hasOwn((await state()).drafts, 't3'), false)
  assert.equal(await page.getByRole('textbox', { name: '话术正文', exact: true }).inputValue(), '开场正文，仅属于第一项')
  assert.equal(await manage.getAttribute('aria-pressed'), 'true')
  await remove('t3').waitFor({ state: 'detached' })
  assert.equal(await page.locator('.recording-script-tab-remove:visible').count(), 3)
  await remove('t2').click()
  assert.deepEqual(await ids(), ['t1', 't4'])
  assert.equal(await manage.getAttribute('aria-pressed'), 'true')
  await remove('t2').waitFor({ state: 'detached' })
  assert.equal(await page.locator('.recording-script-tab-remove:visible').count(), 2)
  checks.push('点击删除角标及其SVG都正常删除；删除后保留模式，可连续删除任意项且清理对应草稿')
  await reset()
  await select('t2').click();await manage.click();await remove('t2').click()
  assert.equal((await state()).active, 't1');assert.deepEqual(await ids(), ['t1', 't3', 't4'])
  await remove('t3').click()
  assert.equal((await state()).active, 't1');assert.deepEqual(await ids(), ['t1', 't4'])
  await remove('t1').click()
  assert.equal((await state()).active, 't4');assert.deepEqual(await ids(), ['t4'])
  assert.equal(await page.getByRole('textbox', { name: '话术正文', exact: true }).inputValue(), '功能正文，仅属于第四项')
  const lastRemove = remove('t4')
  assert.ok((await lastRemove.count()) === 0 || await lastRemove.isDisabled())
  if (await lastRemove.count()) {
    const bounds = await lastRemove.boundingBox()
    await page.mouse.click(bounds.x + bounds.width / 2, bounds.y + bounds.height / 2)
    assert.equal(await manage.getAttribute('aria-pressed'), 'true')
    assert.deepEqual(await ids(), ['t4'])
  }
  await page.evaluate(() => window.fixture.remove('t4'))
  assert.deepEqual(await ids(), ['t4'])
  checks.push('删除当前项选左邻，删除首项后选新首项，至少保留一个标签且父级有兜底')
  const beforeExitAdd = await state()
  await add.click()
  assert.equal(await manage.getAttribute('aria-pressed'), 'false')
  assert.deepEqual(await state(), beforeExitAdd)
  await add.click()
  const added = (await state()).tabs.at(-1)
  assert.equal((await state()).active, added.id)
  assert.deepEqual(added.audioAssetIds, []);assert.equal(added.text, '')
  assert.equal(await page.getByRole('textbox', { name: '标题', exact: true }).inputValue(), added.title)
  checks.push('新增按钮创建并激活独立空标签，标题输入回显，未继承他项正文或声音')

  for (const action of ['文字', '组件内空白', '新增', '把手', '管理', '组件外']) {
    await reset();await manage.click()
    const beforeDismiss = await state()
    const clickTarget = async () => {
      if (action === '文字') await select('t2').locator('.recording-script-tab-name').click()
      else if (action === '组件内空白') {
        const first = await select('t1').boundingBox(), second = await select('t2').boundingBox()
        await page.mouse.click((first.x + first.width + second.x) / 2, first.y + first.height / 2)
      } else if (action === '新增') await add.click()
      else if (action === '把手') await mouseGrip('t2').click()
      else if (action === '管理') await manage.click()
      else await outside.click()
    }
    await clickTarget()
    assert.equal(await manage.getAttribute('aria-pressed'), 'false', `${action} first click must dismiss deletion`)
    assert.equal(await page.locator('.recording-script-tab-remove:visible').count(), 0)
    assert.deepEqual(await state(), beforeDismiss, `${action} first click must be consumed without business changes`)
    await clickTarget()
    if (action === '文字' || action === '把手') assert.equal((await state()).active, 't2')
    else if (action === '新增') assert.equal((await state()).tabs.length, beforeDismiss.tabs.length + 1)
    else if (action === '管理') assert.equal(await manage.getAttribute('aria-pressed'), 'true')
    else if (action === '组件外') assert.equal((await state()).outsideClicks, 1)
    else assert.deepEqual(await state(), beforeDismiss)
  }
  checks.push('删除模式点击文字/空白/新增/把手/管理/组件外：首次只关闭且消费点击，第二次才执行正常操作')
  await reset();await manage.click()
  const deletionGrip = await mouseGrip('t1').boundingBox(), deletionTarget = await select('t3').boundingBox()
  await page.mouse.move(deletionGrip.x + deletionGrip.width / 2, deletionGrip.y + deletionGrip.height / 2);await page.mouse.down()
  await page.mouse.move(deletionTarget.x + deletionTarget.width * .75, deletionTarget.y + deletionTarget.height / 2, { steps: 12 })
  assert.equal(await page.locator('.recording-script-tab-ghost').count(), 0)
  assert.deepEqual(await ids(), ['t1', 't2', 't3', 't4'])
  await page.mouse.up()
  assert.deepEqual(await ids(), ['t1', 't2', 't3', 't4'])
  assert.equal((await state()).active, 't1')
  checks.push('删除模式下从六点把手移动不启动拖动、不重排或切换当前项')
  await reset();await manage.click();await outside.click();await outside.click()
  assert.equal((await state()).outsideClicks, 1)
  await manage.click();await page.evaluate(() => window.fixture.scope())
  await outside.click()
  assert.equal((await state()).outsideClicks, 2)
  assert.equal(await manage.getAttribute('aria-pressed'), 'false')
  await manage.click();await page.evaluate(() => window.fixture.hide())
  await page.locator('.recording-script-tabs').waitFor({ state: 'detached' })
  await outside.click()
  assert.equal((await state()).outsideClicks, 3)
  checks.push('关闭删除模式、切换作用域及卸载组件后，独立外部按钮不被残留监听拦截')

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
  await page.mouse.move(2, 850)
  const normalColor = await select('t2').evaluate(element => getComputedStyle(element).color)
  const selectedColor = await select('t1').evaluate(element => getComputedStyle(element).color)
  assert.equal(selectedColor, 'rgb(255, 255, 255)')
  for (const [id, color] of [['t1', selectedColor], ['t2', normalColor]]) {
    await select(id).hover()
    await select(id).evaluate(async element => { await Promise.all(element.getAnimations().map(animation => animation.finished)) })
    const style = await select(id).evaluate(element => ({ color: getComputedStyle(element).color, cursor: getComputedStyle(element).cursor }))
    assert.equal(style.color, color, `${id} hover must preserve text color`)
    assert.equal(style.cursor, 'pointer')
    const gripCursor = await mouseGrip(id).evaluate(element => getComputedStyle(element).cursor)
    assert.equal(gripCursor, 'grab')
    interactionStyles.push({ id, beforeColor: color, hoverColor: style.color, labelCursor: style.cursor, gripCursor })
  }
  await select('t1').hover()
  await page.screenshot({ path: output + '/selected-hover.png', fullPage: true })
  await select('t2').locator('.recording-script-tab-name').click()
  assert.equal((await state()).active, 't2')
  assert.equal(await page.getByRole('textbox', { name: '话术正文', exact: true }).inputValue(), '塑品正文，仅属于第二项')
  checks.push('选中标签悬停保持白字，普通标签悬停颜色不变；文字区域pointer/六点把手grab，文字点击正常切换')
  await reset()
  const labelFrom = await select('t1').locator('.recording-script-tab-name').boundingBox(), labelTo = await select('t3').boundingBox()
  assert.ok(labelFrom && labelTo)
  await page.mouse.move(labelFrom.x + labelFrom.width / 2, labelFrom.y + labelFrom.height / 2)
  await page.mouse.down()
  await page.mouse.move(labelTo.x + labelTo.width * .75, labelTo.y + labelTo.height / 2, { steps: 12 })
  assert.equal(await page.locator('.recording-script-tab-ghost').count(), 0)
  await page.mouse.up()
  assert.deepEqual(await ids(), ['t1', 't2', 't3', 't4'])
  checks.push('鼠标从标签文字区域移动超过阈值不产生拖拽浮层、不重排')
  await reset()
  const dragFrom = await mouseGrip('t1').boundingBox(), dragTo = await select('t3').boundingBox()
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
  checks.push('真实鼠标仅从六点把手Pointer拖拽产生浮层/过渡动画并提交顺序，正文和声音关联保持')
  await reset()
  const escapeFrom = await mouseGrip('t1').boundingBox(), escapeTo = await select('t3').boundingBox()
  await page.mouse.move(escapeFrom.x + escapeFrom.width / 2, escapeFrom.y + escapeFrom.height / 2);await page.mouse.down()
  await page.mouse.move(escapeTo.x + escapeTo.width * .75, escapeTo.y + escapeTo.height / 2, { steps: 10 })
  await page.locator('.recording-script-tab-ghost').waitFor()
  await page.keyboard.press('Escape');await page.mouse.up()
  assert.deepEqual(await ids(), ['t1', 't2', 't3', 't4'])
  await reset()
  const outsideFrom = await mouseGrip('t1').boundingBox(), outsideTo = await select('t3').boundingBox()
  await page.mouse.move(outsideFrom.x + outsideFrom.width / 2, outsideFrom.y + outsideFrom.height / 2);await page.mouse.down()
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
  await reset({ tabs: Array.from({ length: 20 }, (_, index) => ({ id: `t${index + 1}`, title: `话术${index + 1}`, text: `正文${index + 1}`, audioAssetIds: [] })) })
  await manage.click()
  assert.equal(await add.isDisabled(), true)
  const disabledAdd = await add.boundingBox()
  await page.mouse.click(disabledAdd.x + disabledAdd.width / 2, disabledAdd.y + disabledAdd.height / 2)
  assert.equal(await manage.getAttribute('aria-pressed'), 'false', 'Even a disabled non-delete control must dismiss deletion mode')
  assert.equal((await state()).tabs.length, 20)
  boundaryNotes.push({ case: '20标签时点击原生disabled新增按钮', deletedModeAfterClick: await manage.getAttribute('aria-pressed') === 'true', tabsAfterClick: (await state()).tabs.length })
  checks.push('20项时点击禁用的新增按钮也只退出删除模式，最后一项禁用×仍不删、不退出')

  const touch = await browser.newContext({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true })
  await touch.route('**/*', route => new URL(route.request().url()).hostname === '127.0.0.1' ? route.continue() : route.abort())
  const mobile = await touch.newPage();mobile.on('pageerror', error => errors.push(error.message))
  await mobile.goto(url);await mobile.getByRole('tab').first().waitFor()
  const session = await touch.newCDPSession(mobile)
  const touchLabel = await mobile.locator('[data-recording-tab-id="t1"] .recording-script-tab-name').boundingBox()
  const touchTarget = await mobile.locator('[data-recording-tab-id="t3"] .recording-script-tab-select').boundingBox()
  assert.ok(touchLabel && touchTarget)
  const labelStart = { x: touchLabel.x + touchLabel.width / 2, y: touchLabel.y + touchLabel.height / 2 }
  const labelEnd = { x: touchTarget.x + touchTarget.width * .75, y: touchTarget.y + touchTarget.height / 2 }
  await session.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ ...labelStart, id: 1 }] })
  for (let step = 1; step <= 12; step++) await session.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: labelStart.x + (labelEnd.x - labelStart.x) * step / 12, y: labelStart.y + (labelEnd.y - labelStart.y) * step / 12, id: 1 }] })
  assert.equal(await mobile.locator('.recording-script-tab-ghost').count(), 0)
  await session.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
  assert.deepEqual(await mobile.evaluate(() => window.fixture.state().tabs.map(tab => tab.id)), ['t1', 't2', 't3', 't4'])
  checks.push('390px触屏从标签文字区域移动不进入拖动、不重排')
  const grip = mobile.locator('[data-recording-tab-id="t1"] .recording-script-tab-grip')
  const touchFrom = await grip.boundingBox(), touchTo = await mobile.locator('[data-recording-tab-id="t3"] .recording-script-tab-select').boundingBox()
  assert.ok(touchFrom && touchTo)
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
  await writeFile(output + '/checks.json', JSON.stringify({ passed: !failure, checks, layouts, interactionStyles, boundaryNotes, errors, externalRequests, actualParentFunctions: functionNames, liveRequests: false, error: failure?.stack || null }, null, 2))
  await browser.close();await server.close()
}
if (failure) throw failure
