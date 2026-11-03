import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createServer } from '../node_modules/vite/dist/node/index.js'
const { chromium } = createRequire(new URL('../../collector-worker/package.json', import.meta.url))('playwright-core')
const out = process.env.PLAYBACK_TEST_OUTPUT || '../deploy/validation/script-playback-settings'
await mkdir(out, { recursive: true })
const fixture = await readFile(new URL('./fixtures/script-playback-settings.html', import.meta.url), 'utf8')
const server = await createServer({ root: fileURLToPath(new URL('..', import.meta.url)), server: { host: '127.0.0.1', port: 0 }, appType: 'custom',
  plugins: [{ name: 'playback-fixture', enforce: 'pre', transform(code, id) {
    if (!id.endsWith('/src/views/LiveStrategyView.vue')) return
    return code.replace('</script>', `window.fixture.view={state:()=>({plan:currentRoomPlanId.value,loading:fullShowWorkspaceLoading.value,hydrating:fullShowWorkspaceHydrating.value,busy:savePlanOneClickBusy.value,error:fullShowVersionError.value}),setPlan:id=>handlePlanSwitch({target:{value:String(id)}})};</script>`)
  } }],
})
server.middlewares.use(async (req, res, next) => {
  if (!req.url.startsWith('/__playback')) return next()
  res.setHeader('Content-Type', 'text/html;charset=utf-8')
  res.end(await server.transformIndexHtml(req.url, fixture))
})
await server.listen()
const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH || 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const checks = [], errors = [], external = []
let page
try {
  const context = await browser.newContext({ viewport: { width: 1500, height: 1000 } })
  await context.route('**/*', route => {
    if (new URL(route.request().url()).hostname === '127.0.0.1') return route.continue()
    external.push(route.request().url()); return route.abort()
  })
  page = await context.newPage(); page.setDefaultTimeout(20000)
  page.on('pageerror', error => errors.push(error.message))
  const ready = plan => page.waitForFunction(id => window.fixture?.view?.state().plan === id && !window.fixture.view.state().loading && !window.fixture.view.state().hydrating, plan)
  const speech = () => page.getByRole('button', { name: '话术生成', exact: true }).click()
  const voices = async () => {
    await page.getByRole('button', { name: '声音', exact: true }).click()
    await page.waitForFunction(() => !document.querySelector('.voice-pool-picker select')?.disabled)
  }
  const save = async () => {
    await page.locator('#plan-save-button:not([disabled])').click()
    await page.waitForFunction(() => !window.fixture.view.state().busy)
  }
  const payload = () => page.evaluate(() => window.fixture.calls.filter(call => call.method === 'POST' && call.path.endsWith('/versions')).at(-1)?.body)
  await page.goto(server.resolvedUrls.local[0] + '__playback'); await ready(7); await speech()
  const mode = page.getByRole('combobox', { name: '话术播放方式' })
  assert.equal(await mode.inputValue(), 'full')
  assert.equal(await mode.evaluate(el => getComputedStyle(el).fontSize), '20px')
  const positions = await page.locator('.recording-script-tabs').evaluate(el => ({
    buttons: el.querySelector('.recording-script-tab-actions').getBoundingClientRect().right,
    picker: el.querySelector('select').getBoundingClientRect().left,
  }))
  assert.ok(positions.picker >= positions.buttons)
  await mode.selectOption('single')
  checks.push('默认整轮，单次下拉位于标签管理按钮右侧，字号20px')
  await voices()
  const row = name => page.locator('article.voice-row').filter({ has: page.locator('strong', { hasText: name }) })
  await row('测试声音2').getByRole('button', { name: '选择声音', exact: true }).click()
  assert.equal(await page.locator('.voice-selection-item').count(), 2)
  await save()
  let saved = await payload()
  assert.equal(saved.generation_context.recording_script.play_mode, 'single')
  assert.equal(saved.generation_context.recording_script.version_cap, 7)
  assert.equal(saved.generation_context.recording_script.min_voice_block, 4)
  assert.deepEqual(saved.generation_context.recording_script.voices.map(voice => voice.voice_id), ['longanlingxin', 'voice2'])
  assert.equal(saved.voice_identity.voice_id, 'longanlingxin')
  for (const voice of saved.generation_context.recording_script.voices) {
    assert.equal(voice.identity.provider, 'aliyun_qwen'); assert.equal(voice.identity.rate, 1.2)
    assert.equal(voice.identity.voice_id, voice.voice_id); assert.equal(voice.identity.model, voice.model)
  }
  checks.push('真实父页保存两个声音的顺序与完整身份，主声音保持第一个，单次模式随方案保存')
  const versionCount = () => page.evaluate(() => window.fixture.calls.filter(call => call.method === 'POST' && call.path.endsWith('/versions')).length)
  const beforeDisabled = await versionCount()
  await page.evaluate(() => { window.fixture.disabledVoices = ['voice2'] })
  await speech(); await voices()
  assert.equal(await page.locator('.voice-selection-item.unavailable').count(), 1)
  await save(); assert.equal(await versionCount(), beforeDisabled)
  await page.getByRole('alertdialog').getByRole('button', { name: '知道了', exact: true }).click()
  assert.equal(await page.locator('.voice-selection-item').count(), 2)
  await page.evaluate(() => { window.fixture.disabledVoices = [] })
  checks.push('第二个声音停用时标红并禁止保存，主声音有效也不能绕过，选择与模式保持')
  for (const id of [8, 7]) { await page.evaluate(id => window.fixture.view.setPlan(id), id); await ready(id) }
  assert.equal(await page.locator('.voice-selection-item').count(), 2)
  await speech(); assert.equal(await mode.inputValue(), 'single')
  await page.reload(); await ready(7); await speech(); assert.equal(await mode.inputValue(), 'single')
  await voices(); assert.equal(await page.locator('.voice-selection-item').count(), 2)
  checks.push('切换其它方案再返回、整页刷新均回显两声音与单次模式')
  await page.getByRole('button', { name: '取消选择龙安灵心', exact: true }).click()
  await save(); saved = await payload()
  assert.equal(saved.voice_identity.voice_id, 'voice2')
  assert.deepEqual(saved.generation_context.recording_script.voices.map(voice => voice.voice_id), ['voice2'])
  assert.equal(await page.locator('.voice-selection-item button').first().isDisabled(), true)
  checks.push('取消第一个声音后下一声音成为主声音，至少保留一个声音')
  for (let i = 3; i <= 9; i++) await row('测试声音' + i).getByRole('button', { name: '选择声音', exact: true }).click()
  assert.equal(await page.locator('.voice-selection-item').count(), 8)
  await row('龙安灵心').getByRole('button', { name: '选择声音', exact: true }).click()
  assert.equal(await page.locator('.voice-selection-item').count(), 8)
  await page.getByRole('alertdialog').getByRole('button', { name: '知道了', exact: true }).click()
  checks.push('最多8个声音，第9个拒绝添加')
  await page.locator('.voice-pool-picker select').selectOption('high_fidelity')
  await page.waitForFunction(() => !document.querySelector('.voice-pool-picker select')?.disabled)
  assert.equal(await page.locator('.voice-selection-item').count(), 0)
  await page.getByRole('button', { name: '选择声音', exact: true }).click()
  await save(); saved = await payload()
  assert.equal(saved.voice_identity.provider, 'volcengine')
  assert.equal(saved.generation_context.recording_script.voices.length, 1)
  assert.equal(saved.generation_context.recording_script.voices[0].identity.tts_pool, 'high_fidelity')
  assert.equal(saved.generation_context.recording_script.play_mode, 'single')
  checks.push('切换语音池清除旧池多选，重新选择高仿真后保存完整身份，播放方式保留')
  await speech(); await mode.selectOption('full')
  await page.evaluate(() => { window.fixture.blocked = true; window.fixture.calls = [] })
  await save()
  await page.getByRole('alertdialog').waitFor()
  assert.equal(await mode.inputValue(), 'full')
  assert.equal(await page.evaluate(() => window.fixture.saved.generation_context.recording_script.play_mode), 'single')
  await page.getByRole('alertdialog').getByRole('button', { name: '知道了', exact: true }).click()
  await page.evaluate(() => { window.fixture.blocked = false })
  await save(); assert.equal((await payload()).generation_context.recording_script.play_mode, 'full')
  checks.push('活动直播阻止保存并保留本地选择；停播后可重新保存整轮')
  await page.screenshot({ path: out + '/desktop.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
  assert.equal(await mode.evaluate(el => getComputedStyle(el).fontSize), '20px')
  await page.screenshot({ path: out + '/mobile.png', fullPage: true })
  checks.push('390px下无横向溢出，控件仍为20px')
  const forbidden = await page.evaluate(() => window.fixture.calls.filter(call => /preview|clone|synthesize|tts|generate/.test(call.path)))
  assert.deepEqual(forbidden, []); assert.deepEqual(errors, []); assert.deepEqual(external, [])
  await writeFile(out + '/result.json', JSON.stringify({ passed: true, checks, errors, external }, null, 2))
  console.log(JSON.stringify({ passed: true, checks }))
} catch (error) {
  if (page) { await page.screenshot({ path: out + '/failure.png', fullPage: true }); await writeFile(out + '/failure.txt', String(error) + '\n' + await page.locator('body').innerText() + '\n' + JSON.stringify(errors)) }
  throw error
} finally { await browser.close(); await server.close() }
