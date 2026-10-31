// Actual parent page, API wrappers and four-tab component against local-only fixtures.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createServer } from 'vite'
const { chromium } = createRequire(new URL('../../collector-worker/package.json', import.meta.url))('playwright-core')
const output = process.env.PLAN_PROMPT_TEST_OUTPUT || 'E:/直播伴播-local/plan-prompt-sections-20261031/browser'
await mkdir(output, { recursive: true })
const previous = (await readFile(new URL('./test-plan-prompt-editor.mjs', import.meta.url), 'utf8')).replace(/\r\n/g, '\n')
let fixture = previous.split('const fixture=String.raw`')[1].split('`\nconst server=')[0]
if (fixture === undefined || !fixture.includes('applySession(')) throw Error('Parent fixture not found')
const routes = String.raw`
import {emptyPlanPromptSections,compilePlanPrompts} from '/src/planPrompt.ts';
const oldFetch=window.fetch;
window.fixture.data=JSON.parse(sessionStorage.getItem('sections-data')||'null')||{4:emptyPlanPromptSections(),5:{persona_md:'方案五人设',facts_md:'方案五事实',speech_requirements_md:'方案五主线',interaction_requirements_md:'方案五回答'}};
window.fetch=async(url,init={})=>{
 const f=window.fixture,u=new URL(url,location.origin),p=u.pathname,method=init.method||'GET',body=init.body?JSON.parse(init.body):null,plan=Number(p.match(/live-agent-plans\/(\d+)/)?.[1]||0);
 if(!/\/prompt(?:\/render)?$/.test(p))return oldFetch(url,init);
 f.calls.push({path:p,method,body});
 if(p.endsWith('/render')){
  if(f.holdRender){f.holdRender=false;await new Promise(r=>f.releaseRender=r)}
  if(f.failRender){f.failRender=false;return Response.json({error:'读取事实失败'},{status:500})}
  if(f.emptyRender){f.emptyRender=false;return Response.json({prompt_md:'不应误用整份旧母版'})}
  const text=f.largeRender?'字'.repeat(20001):'【商品卡】\n新商品900克87.6元\n【已确认事实】\n新的事实第'+(++f.renderCount)+'版';f.largeRender=false;
  return Response.json({facts_md:text,prompt_md:'整份母版不能覆盖其余三个框'});
 }
 if(method==='PUT'){
  if(f.holdSave){f.holdSave=false;await new Promise(r=>f.releaseSave=r)}
  if(f.failSave){f.failSave=false;return Response.json({error:'保存失败，保留原文'},{status:500})}
  if(!body.sections)throw Error('Missing sections in save request');
  f.data[plan]=body.sections;sessionStorage.setItem('sections-data',JSON.stringify(f.data));
 }
 if(method==='GET'){
  if(f.holdRead){f.holdRead=false;await new Promise(r=>f.releaseRead=r)}
  if(f.failRead){f.failRead=false;return Response.json({error:'读取提示词失败'},{status:500})}
 }
 const sections=f.data[plan]||emptyPlanPromptSections(),compiled=compilePlanPrompts(sections);
 return Response.json({sections,...compiled,prompt_md:compiled.interaction_prompt_md});
};
`
fixture = fixture.replace('applySession(', routes + '\napplySession(')
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom', plugins: [{ name: 'prompt-section-inspect', enforce: 'pre', transform(code, id) {
  if (!id.endsWith('/src/views/LiveStrategyView.vue')) return
  return code.replace('</script>', 'window.fixture.view={plan:()=>currentRoomPlanId.value,setPlan:id=>handlePlanSwitch({target:{value:String(id)}}),persist:()=>persistPlanPromptIfAny(currentRoomPlanId.value)};</script>')
} }] })
server.middlewares.use(async (req, res, next) => {
  if (req.url !== '/__prompt-sections') return next()
  res.setHeader('Content-Type', 'text/html;charset=utf-8'); res.end(await server.transformIndexHtml(req.url, fixture))
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const checks = [], errors = [], external = []
let page
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1100 }, reducedMotion: 'reduce' })
  await context.route('**/*', route => {
    const u = new URL(route.request().url()); if (u.hostname === '127.0.0.1') return route.continue()
    external.push(u.hostname); return route.abort()
  })
  page = await context.newPage(); page.setDefaultTimeout(20000); page.on('pageerror', e => errors.push(e.message))
  const open = async () => { await page.getByRole('button', { name: '方案提示词', exact: true }).click(); await page.locator('#plan-prompt-content:not([readonly])').waitFor() }
  const tab = name => page.getByRole('tab', { name, exact: true })
  const editor = page.locator('#plan-prompt-content')
  const save = () => page.getByRole('button', { name: '保存方案提示词', exact: true }).click()
  const getCalls = method => page.evaluate(method => window.fixture.calls.filter(c => c.path.endsWith('/prompt') && c.method === method), method)
  const regenerate = async () => { await page.getByRole('button', { name: '重新生成', exact: true }).click(); const dialog = page.getByRole('dialog'); if (await dialog.isVisible()) await dialog.getByRole('button', { name: '确认重新生成', exact: true }).click() }
  await page.goto(server.resolvedUrls.local[0] + '__prompt-sections'); await page.waitForFunction(() => window.fixture?.view?.plan() === 4); await open()
  assert.equal(await page.getByRole('tab').count(), 4)
  const data = { 主播人设: '热情人设测试', 事实依据: '原始事实测试', 直播话术要求: '仅用于主线的要求', 互动回答要求: '仅用于互动的要求' }
  for (const [label, text] of Object.entries(data)) { await tab(label).click(); await editor.fill(text) }
  await tab('主播人设').click(); assert.equal(await editor.inputValue(), data.主播人设)
  await tab('主播人设').press('ArrowRight'); assert.equal(await tab('事实依据').getAttribute('aria-selected'), 'true')
  await save(); await page.getByText('方案提示词已保存，已生成主播互动元提示词和主播话术生成提示词', { exact: true }).waitFor()
  const put = (await getCalls('PUT')).at(-1).body
  assert.deepEqual(put.sections, { persona_md: data.主播人设, facts_md: data.事实依据, speech_requirements_md: data.直播话术要求, interaction_requirements_md: data.互动回答要求 })
  await page.getByText('查看两份元提示词合成预览', { exact: true }).click()
  assert.ok(!(await page.getByRole('textbox', { name: '主播互动元提示词', exact: true }).inputValue()).includes(data.直播话术要求))
  assert.ok(!(await page.getByRole('textbox', { name: '主播话术生成提示词', exact: true }).inputValue()).includes(data.互动回答要求))
  checks.push('四标签切换/键盘导航保留草稿；真实API提交四字段；两份预览不串用专属要求')

  await page.reload(); await page.waitForFunction(() => window.fixture?.view?.plan() === 4); await open()
  for (const [label, text] of Object.entries(data)) { await tab(label).click(); assert.equal(await editor.inputValue(), text) }
  await page.getByRole('button', { name: '重新生成', exact: true }).click(); await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click()
  assert.equal((await getCalls('PUT')).length, 0)
  for (const failure of ['failRender', 'emptyRender', 'largeRender']) {
    await page.evaluate(key => window.fixture[key] = true, failure); await regenerate(); await page.getByText('生成未完成', { exact: true }).waitFor()
    await tab('事实依据').click(); assert.equal(await editor.inputValue(), data.事实依据)
  }
  await regenerate(); await page.getByText('已更新事实依据，其余三个文本框保持不变，检查后保存', { exact: true }).waitFor()
  assert.ok((await editor.inputValue()).includes('新商品900克87.6元'))
  for (const [label, text] of Object.entries(data).filter(([label]) => label !== '事实依据')) { await tab(label).click(); assert.equal(await editor.inputValue(), text) }
  assert.equal((await getCalls('PUT')).length, 0)
  checks.push('保存后重读四框一致；重新生成的取消/错误/缺字段/超限不破坏草稿；成功只改事实且不自动保存')

  await page.evaluate(() => window.fixture.failSave = true); await save(); await page.getByText('保存未完成', { exact: true }).waitFor()
  assert.equal(await editor.inputValue(), data.互动回答要求)
  await save(); await page.getByText('方案提示词已保存，已生成主播互动元提示词和主播话术生成提示词', { exact: true }).waitFor()
  await page.evaluate(() => window.fixture.view.persist())
  assert.ok((await getCalls('PUT')).at(-1).body.sections.speech_requirements_md === data.直播话术要求)
  checks.push('保存失败保留四框；一键保存方案复用四分区持久化入口')

  await page.evaluate(() => window.fixture.holdRender = true); await regenerate(); await page.waitForFunction(() => !!window.fixture.releaseRender)
  await page.evaluate(() => window.fixture.view.setPlan(5)); await page.waitForFunction(() => document.querySelector('#plan-prompt-content')?.value === '方案五人设')
  await page.evaluate(() => window.fixture.releaseRender()); await tab('事实依据').click(); assert.equal(await editor.inputValue(), '方案五事实')
  await page.evaluate(() => { window.fixture.holdRead = true; window.fixture.view.setPlan(4) }); await page.waitForFunction(() => !!window.fixture.releaseRead)
  await page.evaluate(() => window.fixture.view.setPlan(5)); await page.waitForFunction(() => document.querySelector('#plan-prompt-content')?.value === '方案五人设')
  await page.evaluate(() => window.fixture.releaseRead()); await tab('事实依据').click(); assert.equal(await editor.inputValue(), '方案五事实')
  checks.push('生成/读取期间切换方案，迟到响应不覆盖新方案或向新方案写入旧内容')

  await page.evaluate(() => window.fixture.holdSave = true); await save(); await page.waitForFunction(() => !!window.fixture.releaseSave)
  await page.evaluate(() => window.fixture.view.setPlan(4)); await page.waitForFunction(() => document.querySelector('#plan-prompt-content')?.value === '热情人设测试')
  await page.evaluate(() => window.fixture.view.setPlan(5)); await page.waitForFunction(() => document.querySelector('#plan-prompt-content')?.value === '方案五人设')
  await tab('事实依据').click(); await editor.fill('返回同方案后的新草稿')
  await page.evaluate(() => window.fixture.releaseSave()); await page.waitForFunction(() => window.fixture.data[5].facts_md === '方案五事实')
  assert.equal(await editor.inputValue(), '返回同方案后的新草稿')
  checks.push('保存未返回时离开再回到同一方案，旧保存回执不会覆盖新页面草稿')

  await tab('主播人设').click(); await editor.fill('字'.repeat(11000)); await tab('事实依据').click(); await editor.fill('字'.repeat(10000))
  assert.equal(await page.getByRole('button', { name: '保存方案提示词', exact: true }).isDisabled(), true)
  await tab('主播人设').click(); await editor.fill('方案五人设'); await tab('事实依据').click(); await editor.fill('方案五事实')
  checks.push('按合成后的两份正文限制长度，单框未超限但组合超限时禁止保存')
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 1100 }); await page.locator('.plan-prompt-editor').scrollIntoViewIfNeeded()
    const layout = await page.locator('.plan-prompt-editor').evaluate(root => {
      const box = root.getBoundingClientRect()
      return { left: box.left, right: box.right, overflow: root.scrollWidth > root.clientWidth + 1, fonts: [...root.querySelectorAll('button,label,p,span,textarea,summary')].filter(e => e.getClientRects().length).map(e => Number.parseFloat(getComputedStyle(e).fontSize)) }
    })
    assert.ok(layout.left >= 0 && layout.right <= width + 1 && !layout.overflow && layout.fonts.every(font => font >= 16), JSON.stringify(layout))
    await page.locator('.plan-prompt-editor').screenshot({ path: output + '/sections-' + width + '.png' })
    checks.push(width + 'px真实父页无横向溢出，标签/正文/按钮/提示字号至少16px')
  }
  const mutations = await page.evaluate(() => window.fixture.calls.filter(c => c.method !== 'GET' && !c.path.endsWith('/prompt') && !c.path.endsWith('/prompt/render') && !c.path.endsWith('/fact-gap-check')))
  assert.deepEqual(mutations, []); assert.deepEqual(errors, []); assert.deepEqual(external, [])
  await writeFile(output + '/checks.json', JSON.stringify({ passed: true, checks, errors, external, scope: 'actual parent view and API; local fixtures, no production or paid requests' }, null, 2))
  console.log(JSON.stringify({ passed: true, checks }))
} catch (error) {
  if (page) await page.screenshot({ path: output + '/failure.png', fullPage: true })
  await writeFile(output + '/failure.json', JSON.stringify({ error: String(error), stack: error.stack, checks, errors, external }, null, 2)); throw error
} finally { await browser.close(); await server.close() }
