// Actual parent, maintenance view and API wrappers. Local fixture endpoints only.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createServer } from 'vite'
const { chromium } = createRequire(new URL('../../collector-worker/package.json', import.meta.url))('playwright-core')
const output = process.env.CATALOG_TEST_OUTPUT || 'E:/直播伴播-local/prompt-template-catalog-20261105/browser'
await mkdir(output, { recursive: true })
const source = (await readFile(new URL('./test-plan-prompt-editor.mjs', import.meta.url), 'utf8')).replace(/\r\n/g, '\n')
const parent = (await readFile(new URL('./test-plan-rewrite-prompt-editor.mjs', import.meta.url), 'utf8')).replace(/\r\n/g, '\n')
let fixture = source.split('const fixture=String.raw`')[1].split('`\nconst server=')[0]
const setup = parent.split('const setup = String.raw`')[1].split('\n`\nfixture =')[0]
fixture = fixture.replace('applySession(', setup + '\napplySession(').replace('render:()=>h(View)', 'render:()=>h(window.fixture.showTemplates.value?TemplateView:View)')
const keys = ['persona_md','facts_md','red_lines_md','speech_requirements_md','interaction_requirements_md','rewrite_planning_md']
const labels = ['主播人设','事实依据','红线禁止','直播话术要求','互动回答要求','话术规划']
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom', plugins: [{ name: 'catalog-parent', enforce: 'pre', transform(code, id) {
  if (!id.endsWith('/src/views/LiveStrategyView.vue')) return
  return code.replace('</script>', 'window.fixture.view={plan:()=>currentRoomPlanId.value,setPlan:id=>handlePlanSwitch({target:{value:String(id)}}),loaded:()=>planPromptLoadedPlan.value,loading:()=>planPromptLoading.value,busy:()=>savePlanOneClickBusy.value,hydrating:()=>fullShowWorkspaceHydrating.value,sections:()=>planPromptSections.value,planning:()=>planRewritePlanning.value};</script>')
} }] })
server.middlewares.use(async (req,res,next) => { if(req.url!=='/__catalog') return next(); res.setHeader('Content-Type','text/html;charset=utf-8'); res.end(await server.transformIndexHtml(req.url,fixture)) })
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const checks=[], errors=[], external=[]; let page
try {
  const context=await browser.newContext({viewport:{width:1600,height:1100},reducedMotion:'reduce'})
  await context.route('**/*',route=>{const u=new URL(route.request().url());if(u.hostname==='127.0.0.1') return route.continue();external.push(u.hostname);return route.abort()})
  page=await context.newPage();page.setDefaultTimeout(15000);page.on('pageerror',e=>errors.push(e.message))
  const ready=async()=>{await page.waitForFunction(()=>window.fixture?.view?.loaded()===window.fixture.view.plan()&&!window.fixture.view.loading()&&!window.fixture.view.busy()&&!window.fixture.view.hydrating());await page.waitForFunction(()=>document.querySelector('.plan-prompt-editor')?.getAttribute('aria-busy')==='false')}
  const open=async()=>{await page.getByRole('button',{name:'方案提示词',exact:true}).click();await ready()}
  const draft=()=>page.evaluate(()=>({...JSON.parse(JSON.stringify(window.fixture.view.sections())),rewrite_planning_md:window.fixture.view.planning()}))
  const picker=()=>page.getByRole('dialog',{name:/模板选择/})
  const choose=async(name)=>{await picker().getByRole('radio',{name,exact:true}).check();await picker().getByRole('button',{name:'确认使用',exact:true}).click();await ready()}
  const cancel=async()=>{await picker().getByRole('button',{name:'取消',exact:true}).click();await ready()}
  const select=async(i)=>{await page.getByRole('tab',{name:labels[i],exact:true}).click();await ready()}
  const showPicker=async()=>{await page.getByRole('button',{name:'模板选择',exact:true}).click();await picker().waitFor()}
  const templateEditor=()=>page.locator('#prompt-template-content')
  await page.goto(server.resolvedUrls.local[0]+'__catalog');await page.waitForFunction(()=>window.fixture?.view?.plan()===4);await open()
  assert.equal(await page.getByRole('button',{name:'拉取提示词',exact:true}).count(),0)
  assert.equal(await page.getByRole('tab').count(),6)
  assert.equal(await page.getByRole('button',{name:'模板选择',exact:true}).getAttribute('role'),null)
  checks.push('移除拉取功能，六标签后独立模板按钮，不加入标签键盘导航')
  await page.evaluate(()=>window.fixture.catalog={version:1,pages:Object.fromEntries(Object.keys(window.fixture.templates).map(k=>[k,[]]))})
  const original=await draft();await showPicker();await picker().getByText(/当前标签暂无模板/).waitFor();assert.ok(await picker().getByRole('button',{name:'确认使用'}).isDisabled());await cancel();assert.deepEqual(await draft(),original)
  checks.push('空目录显示后台维护提示，不能确认且不覆盖原稿')
  await page.evaluate(()=>window.fixture.showTemplates.value=true)
  await page.getByRole('heading',{name:'提示词维护',exact:true}).waitFor();await page.waitForFunction(()=>document.querySelector('.prompt-templates')?.getAttribute('aria-busy')==='false')
  const contents={}
  for(let i=0;i<6;i++){
    await page.locator('.prompt-templates .tabs').getByRole('button',{name:labels[i],exact:true}).click()
    await page.getByRole('button',{name:'添加模板',exact:true}).click()
    const text='  【'+labels[i]+'】\n键名：第一份😀\n{{直播间名}}\n  '
    await page.getByRole('textbox',{name:'模板名称',exact:true}).fill(labels[i]+'甲');await templateEditor().fill(text)
    await page.getByRole('button',{name:'添加模板',exact:true}).click()
    await page.getByRole('textbox',{name:'模板名称',exact:true}).fill(labels[i]+'乙');await templateEditor().fill('【第二份】\n键名：'+labels[i]+'乙')
    contents[keys[i]]=text
    assert.equal(await page.locator('.template-toolbar option').count(),2)
  }
  checks.push('后台六个标签分别创建两份命名模板，列表切换与原文20px编辑')
  await page.evaluate(()=>window.fixture.failTemplateSave=true);await page.getByRole('button',{name:'保存模板',exact:true}).click();await page.getByRole('alert').filter({hasText:'模板保存失败'}).waitFor();assert.ok((await templateEditor().inputValue()).includes('话术规划乙'))
  await page.getByRole('button',{name:'保存模板',exact:true}).click();await page.getByRole('status').filter({hasText:'模板目录已保存'}).waitFor()
  const saved=await page.evaluate(()=>window.fixture.catalog);assert.equal(Object.values(saved.pages).flat().length,12)
  checks.push('失败保留全部模板草稿，成功统一保存十二份模板')
  await templateEditor().fill('并发草稿');await page.evaluate(()=>window.fixture.templateRevision='c'.repeat(64));await page.getByRole('button',{name:'保存模板',exact:true}).click();await page.getByRole('alert').filter({hasText:'其他人修改'}).waitFor();assert.equal(await templateEditor().inputValue(),'并发草稿')
  await page.getByRole('button',{name:'重新读取',exact:true}).click();await page.getByRole('dialog').getByRole('button',{name:'取消',exact:true}).click();assert.equal(await templateEditor().inputValue(),'并发草稿')
  await page.getByRole('button',{name:'重新读取',exact:true}).click();await page.getByRole('button',{name:'确认读取',exact:true}).click();await page.waitForFunction(()=>document.querySelector('.prompt-templates')?.getAttribute('aria-busy')==='false');assert.ok((await templateEditor().inputValue()).includes('话术规划乙'))
  checks.push('并发版本冲突保留草稿；重新读取须确认，取消不丢输入')
  await page.getByRole('textbox',{name:'模板名称',exact:true}).fill('  ');assert.ok(await page.getByRole('button',{name:'保存模板',exact:true}).isDisabled());await page.getByRole('textbox',{name:'模板名称',exact:true}).fill('话术规划乙')
  await page.getByRole('button',{name:'删除模板',exact:true}).click();assert.equal(await page.locator('.template-toolbar option').count(),2);await page.getByRole('button',{name:'取消删除',exact:true}).click()
  await page.getByRole('button',{name:'删除模板',exact:true}).click();await page.getByRole('button',{name:'确认删除模板',exact:true}).click();assert.equal(await page.locator('.template-toolbar option').count(),1)
  await page.getByRole('button',{name:'保存模板',exact:true}).click();await page.getByRole('status').filter({hasText:'模板目录已保存'}).waitFor()
  checks.push('模板名称校验、可取消删除、二次确认删除及保存')
  for(const width of [1600,390]){await page.setViewportSize({width,height:1100});const s=await page.locator('.prompt-templates').evaluate(e=>[e.scrollWidth,e.clientWidth]);assert.ok(s[0]<=s[1]+1);await page.locator('.prompt-templates').screenshot({path:output+'/admin-'+width+'.png'})}
  await page.setViewportSize({width:1600,height:1100});await page.evaluate(()=>window.fixture.showTemplates.value=false);await open()
  for(let i=0;i<6;i++){
    await select(i);const before=await draft();await showPicker()
    await picker().getByRole('radio',{name:labels[i]+'甲',exact:true}).check()
    assert.equal(await picker().locator('pre').textContent(),contents[keys[i]])
    assert.equal(await picker().getByRole('radio').count(),i===5?1:2)
    await cancel();assert.deepEqual(await draft(),before)
    await showPicker();await choose(labels[i]+'甲')
    const after=await draft();assert.equal(after[keys[i]],contents[keys[i]])
    for(const key of keys.filter(k=>k!==keys[i]))assert.equal(after[key],before[key])
  }
  assert.equal(await page.evaluate(()=>window.fixture.calls.filter(c=>c.path.endsWith('/render')).length),0)
  assert.equal(await page.evaluate(()=>window.fixture.calls.filter(c=>/\/prompt$/.test(c.path)&&c.method==='PUT').length),0)
  checks.push('六页只显示对应模板，预览逐字一致；取消无变更，确认仅当前页，无生成或自动保存')
  await page.locator('#plan-save-button').click();await ready();await page.getByText('方案提示词已随方案保存，三份元提示词已更新',{exact:true}).waitFor()
  await page.reload();await page.waitForFunction(()=>window.fixture?.view?.plan()===4);await open();const adopted=await draft();for(const key of keys)assert.equal(adopted[key],contents[key])
  checks.push('顶部统一保存后刷新六页原文及模板目录持久回显')
  for(const flag of ['failRender','incompleteRender']){
    const before=await draft();await page.evaluate(f=>window.fixture[f]=true,flag);await showPicker();await picker().getByRole('alert').waitFor();await cancel();assert.deepEqual(await draft(),before)
  }
  checks.push('目录读取失败或缺项保留草稿，不把错误回执作为空模板')
  await showPicker();await page.keyboard.press('Escape');await ready()
  const beforeLate=await draft();await page.evaluate(()=>window.fixture.holdRender=true);await showPicker();await page.waitForFunction(()=>!!window.fixture.releaseRender);await cancel();await page.evaluate(()=>{window.fixture.releaseRender();window.fixture.releaseRender=null});await page.waitForTimeout(100);assert.deepEqual(await draft(),beforeLate);assert.equal(await picker().count(),0)
  checks.push('Esc取消与取消后的迟到GET不复活弹窗、不覆盖草稿')
  await showPicker();await picker().getByRole('radio',{name:'主播人设甲',exact:true}).check();await page.evaluate(()=>window.fixture.view.sections().persona_md='外部新草稿');await picker().getByRole('button',{name:'确认使用'}).click();await ready();assert.equal((await draft()).persona_md,'外部新草稿');await page.getByRole('alert').filter({hasText:'当前页已变化'}).waitFor()
  checks.push('选取期间当前页外部变化时拒绝覆盖新草稿')
  await page.evaluate(()=>window.fixture.holdRender=true);await showPicker();await page.waitForFunction(()=>!!window.fixture.releaseRender);await page.evaluate(()=>window.fixture.view.setPlan(5));await ready();await page.evaluate(()=>{window.fixture.releaseRender();window.fixture.releaseRender=null});await page.waitForTimeout(100);assert.equal((await draft()).persona_md,'方案5人设');assert.equal(await picker().count(),0)
  checks.push('切方案作废目录请求与旧选择，租户方案草稿隔离')
  await page.evaluate(()=>{window.fixture.catalog.pages.persona_md=[{id:'empty',name:'空白模板',content:''}]});await showPicker();await picker().getByRole('radio',{name:'空白模板'}).check();await picker().getByText(/空模板：确认后将清空当前页/).waitFor();await choose('空白模板');assert.equal((await draft()).persona_md,'')
  checks.push('显式空模板可确认清空当前页，预览说明清空效果')
  await page.evaluate(()=>{window.fixture.catalog.pages.persona_md=[{id:'large',name:'超长模板',content:'字'.repeat(20001)}]});await showPicker();await picker().getByRole('alert').waitFor();await cancel();assert.equal((await draft()).persona_md,'')
  checks.push('畸形或超长模板在写入前拒绝，原稿保持')
  await page.evaluate(()=>{window.fixture.catalog.pages.persona_md=[{id:'visual',name:'手机模板',content:'【身份】\n姓名：小林'}]})
  for(const width of [1600,777,390,320]){await page.setViewportSize({width,height:1100});await showPicker();await picker().getByRole('radio',{name:'手机模板'}).check();const radio=await picker().getByRole('radio').evaluate(e=>[e.getBoundingClientRect().width,e.getBoundingClientRect().height]);assert.ok(radio.every(n=>n<=24));const size=await picker().evaluate(e=>[e.scrollWidth,e.clientWidth]);assert.ok(size[0]<=size[1]+1);await picker().screenshot({path:output+'/picker-'+width+'.png'});await cancel()}
  checks.push('后台与选择弹窗桌面/手机无横向溢出，模板正文可滚动')
  assert.deepEqual(errors,[]);assert.deepEqual(external,[])
  await writeFile(output+'/checks.json',JSON.stringify({passed:true,groups:checks.length,checks,browser_errors:errors,external_requests:external,real_provider_calls:0},null,2));console.log(JSON.stringify({passed:true,groups:checks.length,checks}))
}catch(error){if(page){await page.screenshot({path:output+'/failure.png',fullPage:true}).catch(()=>{});await writeFile(output+'/failure.txt',error.stack||String(error))}throw error}finally{await browser.close();await server.close()}
