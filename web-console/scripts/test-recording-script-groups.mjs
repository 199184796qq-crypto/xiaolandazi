import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createServer } from 'vite'
import ts from 'typescript'

const require = createRequire(process.env.TAB_TEST_RUNTIME || new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const output = process.env.TAB_TEST_OUTPUT || '../deploy/validation/recording-script-groups'
const fixtureId = fileURLToPath(new URL('../src/__script-groups-fixture.vue', import.meta.url)).replaceAll('\\', '/')
const source = await readFile(new URL('../src/views/LiveStrategyView.vue', import.meta.url), 'utf8')
const marker = '<script setup lang="ts">'
const ast = ts.createSourceFile('view.ts', source.slice(source.indexOf(marker) + marker.length, source.indexOf('</script>')), ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
const functions = ['recordingScriptForSave', 'restoreRecordingScript'].map(name => {
  const node = ast.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === name)
  assert.ok(node); return node.getText(ast)
}).join('\n')
const fixture = `<script setup lang="ts">
import { computed, ref } from 'vue'
import RecordingScriptTabs from '/src/components/RecordingScriptTabs.vue'
import RecordingScriptEntries from '/src/components/RecordingScriptEntries.vue'
import { groupedScriptForSave, scriptEntries } from '/src/recordingScriptGroups'
import { readScriptPlaybackSettings } from '/src/scriptPlaybackSettings'
const recordingPlayback=ref(readScriptPlaybackSettings(undefined))
import '/src/style.css'
type RecordingScriptTab=import('/src/recordingScriptGroups').ScriptGroup
const recordingTabs=ref<RecordingScriptTab[]>([]), activeRecordingTabId=ref('a'), saving=ref(false), notice=ref('')
let recordingTabSeq=1
const active=computed(()=>recordingTabs.value.find(t=>t.id===activeRecordingTabId.value))
const entries=computed(()=>active.value?scriptEntries(active.value):[])
${functions}
const copy=x=>JSON.parse(JSON.stringify(x))
function reset(tabs){recordingTabs.value=copy(tabs);activeRecordingTabId.value=tabs[0].id;notice.value=''}
reset([{id:'a',title:'开场',text:'',audioAssetIds:[],pinned:true},{id:'b',title:'商品',text:'',audioAssetIds:[],pinned:false}])
window.fixture={reset, save:()=>recordingScriptForSave(true), restore:restoreRecordingScript, state:()=>copy(recordingTabs.value), disable:value=>saving.value=value}
</script>
<template><main style="max-width:1100px;margin:20px auto;padding:20px;box-sizing:border-box" class="strategy-fullshow-output-grid is-recording-mainline">
<section class="strategy-custom-mainline-segments">
<header><strong>原始话术</strong></header>
<RecordingScriptTabs :tabs="recordingTabs" :active-id="activeRecordingTabId" :disabled="saving" @select="activeRecordingTabId=$event" />
<label class="strategy-script-tab-title"><span>组名</span><input v-model="active.title" :disabled="saving" aria-label="组名" /></label>
<RecordingScriptEntries :key="active.id" :entries="entries" :disabled="saving" :plan-id="1" @update="active.entries=$event" />
</section></main></template>
<style scoped src="/src/styles/recordingScriptGroups.css"></style>`
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, plugins: [{ name: 'script-group-fixture',
  resolveId(id) { if (id === '/__script-groups-fixture.vue') return fixtureId },
  load(id) { if (id === fixtureId) return fixture },
  configureServer(dev) { dev.middlewares.use(async (req, res, next) => {
    if (req.url !== '/__script-groups-test') return next()
    res.setHeader('Content-Type', 'text/html;charset=utf-8')
    res.end(await dev.transformIndexHtml(req.url, '<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="icon" href="data:,"><div id="app"></div><script type="module">import {createApp} from "vue";import App from "/__script-groups-fixture.vue";createApp(App).mount("#app")</script>'))
  }) },
}] })
const checks = [], errors = [], externalRequests = [], layouts = [], riskRequests = []
await mkdir(output, { recursive: true }); await server.listen()
const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH || 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
let failure
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
  await context.route('**/*', route => {
    if (new URL(route.request().url()).hostname === '127.0.0.1') return route.continue()
    externalRequests.push(route.request().url());return route.abort()
  })
  await context.route('**/api/v1/live-agent-plans/1/anchor-style-workbench/risk-annotations', async route => {
    const body = route.request().postDataJSON()
    const text = String(body?.text || '')
    riskRequests.push(text)
    const p0 = 'P0命中词'
    const p1 = 'P1候选词'
    const p0Start = Array.from(text).join('').indexOf(p0)
    const p1Start = Array.from(text).join('').indexOf(p1)
    const hits = []
    if (p0Start >= 0) hits.push({ rule_id: 'test-p0', severity: 'P0', category: '硬拦词', matched_text: p0, original_start: p0Start, original_end: p0Start + Array.from(p0).length })
    if (p1Start >= 0) hits.push({ rule_id: 'test-p1', severity: 'P1', category: '候选词', matched_text: p1, original_start: p1Start, original_end: p1Start + Array.from(p1).length })
    await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ hits, mode: 'lexical_review_only' }) })
  })
  const page = await context.newPage();page.on('pageerror', e => errors.push(e.message));page.setDefaultTimeout(10000)
  await page.goto(server.resolvedUrls.local[0] + '__script-groups-test')
  const editors = page.locator('.recording-script-entry textarea')
  const add = page.getByRole('button', { name: '＋ 添加话术', exact: true })
  const first = () => page.getByRole('textbox', { name: '话术 1 正文', exact: true })
  await first().fill('甲'.repeat(399));await add.click()
  assert.equal(await editors.count(), 1)
  assert.ok((await page.locator('.recording-script-entry-error').first().innerText()).includes('399'))
  const blocked = await page.evaluate(() => {try {window.fixture.save();return false}catch{return true}})
  assert.equal(blocked, true)
  checks.push('399字符红字位于框上方；添加被阻止；实际父级序列化拒绝保存')
  await first().fill('甲'.repeat(399) + '。');await add.click()
  assert.equal(await editors.count(), 2)
  const second = page.getByRole('textbox', { name: '话术 2 正文', exact: true })
  await second.fill('乙'.repeat(3599) + '😀')
  assert.equal(await page.locator('.recording-script-entry-error').count(), 0)
  await add.click();assert.equal(await editors.count(), 2)
  assert.ok((await page.locator('.recording-script-entry-error').last().innerText()).includes('不足新增'))
  checks.push('400可新增独立空框；汉字/标点/补充平面字符各计1；合计4000合法且不能再增一框')
  await second.fill('乙'.repeat(3600) + '😀')
  const overflow = await page.locator('.recording-script-entry-error').allInnerTexts()
  assert.equal(overflow.length, 2);assert.ok(overflow.every(text => text.includes('4001')))
  await add.click();assert.equal(await editors.count(), 2)
  checks.push('两框合计4001时每框上方均红字提示；不截断输入，不能添加')
  await second.fill('乙'.repeat(4001))
  assert.ok((await page.locator('.recording-script-entry-error').nth(1).innerText()).includes('单条不能超过'))
  const removeSecond = page.getByRole('button', { name: '删除话术 2', exact: true })
  await removeSecond.click()
  assert.equal(await editors.count(), 2)
  const confirmSecond = page.getByRole('button', { name: '确认删除话术 2', exact: true })
  assert.equal(await confirmSecond.innerText(), '确认删除')
  assert.equal(await confirmSecond.evaluate(button => button.classList.contains('is-confirming')), true)
  const confirmationStyle = await confirmSecond.evaluate(button => {
    const style = getComputedStyle(button)
    return { background: style.backgroundColor, color: style.color }
  })
  assert.ok(['rgb(217, 56, 66)', 'rgb(199, 47, 57)'].includes(confirmationStyle.background))
  assert.equal(confirmationStyle.color, 'rgb(255, 255, 255)')
  await first().click()
  assert.equal(await page.getByRole('button', { name: '删除话术 2', exact: true }).count(), 1)
  await removeSecond.click()
  await confirmSecond.click()
  assert.equal(await editors.count(), 1)
  await first().fill('甲'.repeat(3601));await add.click()
  assert.equal(await editors.count(), 1)
  checks.push('删除首次原位红色确认、点击其它位置取消、再次确认后删除；单框4001有独立越界提示；删除恢复额度；仅剩399额度时拒绝新增')
  await first().fill('甲'.repeat(400));await add.click();await second.fill('乙'.repeat(400))
  await page.getByRole('tab', { name: '商品', exact: true }).click()
  await first().fill('丙'.repeat(399) + '。')
  const saved = await page.evaluate(() => window.fixture.save())
  assert.equal(saved.version, 2);assert.equal(saved.tabs.length, 2)
  assert.equal(saved.tabs[0].entries.length, 2);assert.equal(saved.tabs[1].entries.length, 1)
  assert.equal(saved.tabs[0].pinned, true)
  assert.equal(saved.tabs.some(tab => Object.hasOwn(tab, 'text')), false)
  await page.evaluate(() => window.fixture.reset([{id:'other',title:'别的方案',text:'',audioAssetIds:[],pinned:false}]))
  await page.evaluate(script => window.fixture.restore({recording_script:script}), saved)
  assert.equal(await first().inputValue(), saved.tabs[1].entries[0].text)
  await page.getByRole('tab', { name: '开场', exact: true }).click()
  assert.equal(await editors.count(), 2);assert.equal(await second.inputValue(), saved.tabs[0].entries[1].text)
  checks.push('实际父级保存/恢复两组3条独立正文，无分隔符拼接；切换后正文/固定标记/激活组完整回显')
  await page.evaluate(() => window.fixture.disable(true))
  assert.equal(await first().isDisabled(), true);assert.equal(await add.isDisabled(), true)
  assert.equal(await page.getByRole('textbox', {name:'组名',exact:true}).isDisabled(), true)
  await page.evaluate(() => window.fixture.disable(false))
  checks.push('保存中编辑器、添加、删除与组名禁用，避免回执覆盖在途修改')
  await page.evaluate(() => window.fixture.reset([{id:'a',title:'旧方案',text:'甲'.repeat(400)+'\n====\n'+'乙'.repeat(400),audioAssetIds:[],pinned:false}]))
  assert.equal(await editors.count(), 2)
  const migrated = await page.evaluate(() => window.fixture.save())
  assert.equal(migrated.tabs[0].entries.length, 2)
  checks.push('历史====内容仅在编辑兼容入口拆框；新保存为独立条目')
  await first().fill('甲'.repeat(396)+'====')
  const literal = await page.evaluate(() => window.fixture.save())
  assert.equal(literal.tabs[0].entries.length, 2);assert.ok(literal.tabs[0].entries[0].text.endsWith('===='))
  checks.push('新条目内字面====按原文保存，不再充当分段符')
  await page.evaluate(() => window.fixture.reset([{id:'a',title:'P0测试',text:'',audioAssetIds:[],pinned:false}]))
  const blankHeight = await first().evaluate(element => element.getBoundingClientRect().height)
  await first().fill('甲'.repeat(1200) + 'P1候选词' + '乙'.repeat(600) + 'P0命中词')
  await page.waitForFunction(() => document.querySelector('.recording-script-entry-violations')?.textContent?.includes('P0命中词'))
  const p0Notice = await page.locator('.recording-script-entry-violations').innerText()
  assert.ok(p0Notice.includes('P0 命中「P0命中词」'))
  assert.equal(p0Notice.includes('P1候选词'), false)
  const highlighted = page.locator('.recording-script-entry-mirror mark')
  assert.equal(await highlighted.count(), 1)
  assert.equal(await highlighted.innerText(), 'P0命中词')
  const markedStyle = await highlighted.evaluate(element => ({ color: getComputedStyle(element).color, background: getComputedStyle(element).backgroundColor }))
  assert.equal(markedStyle.color, 'rgb(196, 44, 59)')
  assert.notEqual(markedStyle.background, 'rgba(0, 0, 0, 0)')
  assert.ok((await first().evaluate(element => element.getBoundingClientRect().height)) > blankHeight)
  assert.ok(riskRequests.some(text => text.includes('P0命中词')))
  checks.push('话术片按内容自动撑高；输入后防抖检查，只有 P0 在框上方提示并在可编辑正文中红色标记')
  for (const width of [1440, 390]) {
    await page.setViewportSize({width,height:1000})
    const info = await page.evaluate(() => ({
      overflow: document.documentElement.scrollWidth > innerWidth,
      wrongFonts: [...document.querySelectorAll('main input,main textarea,main button,main p,main span,main label,main strong')].filter(el=>el.getClientRects().length && getComputedStyle(el).fontSize!=='20px').map(el=>({tag:el.tagName,size:getComputedStyle(el).fontSize})),
    }))
    layouts.push({width,...info});assert.equal(info.overflow,false);assert.deepEqual(info.wrongFonts,[])
    await page.screenshot({path:`${output}/${width}.png`,fullPage:true})
  }
  checks.push('1440/390px实际组件文字、标签、按钮、提示、文本框均20px，无页面横向溢出')
  assert.deepEqual(errors, []);assert.deepEqual(externalRequests, [])
  await context.close()
} catch (error) {failure=error;console.error(error)} finally {
  await writeFile(output+'/checks.json',JSON.stringify({passed:!failure,checks,layouts,errors,externalRequests,liveRequests:false,error:failure?.stack||null},null,2))
  await browser.close();await server.close()
}
console.log(JSON.stringify({passed:!failure,checks:checks.length,output}))
if (failure) throw failure
