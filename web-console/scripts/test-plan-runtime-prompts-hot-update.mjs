// Actual parent and API wrappers; all business responses are local fixtures.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createServer } from 'vite'
const { chromium } = createRequire(new URL('../../collector-worker/package.json', import.meta.url))('playwright-core')
const output = process.env.HOT_PROMPT_TEST_OUTPUT || 'E:/直播伴播-local/plan-runtime-prompts-hot-update-20261106/browser'
await mkdir(output, { recursive: true })
const source = (await readFile(new URL('./test-plan-prompt-editor.mjs', import.meta.url), 'utf8')).replace(/\r\n/g, '\n')
const parent = (await readFile(new URL('./test-plan-rewrite-prompt-editor.mjs', import.meta.url), 'utf8')).replace(/\r\n/g, '\n')
let fixture = source.split('const fixture=String.raw`')[1].split('`\nconst server=')[0]
const setup = parent.split('const setup = String.raw`')[1].split('\n`\nfixture =')[0]
const hot = String.raw`
const delegatedHotFetch=window.fetch;
Object.assign(window.fixture,{live:true,hotRevision:sessionStorage.getItem('hot-revision')||'r0'});
window.fetch=async(url,init={})=>{
 const f=window.fixture,u=new URL(url,location.origin),p=u.pathname,method=init.method||'GET',plan=Number(p.match(/live-agent-plans\/(\d+)/)?.[1]||0),body=init.body?JSON.parse(init.body):null;
 if(p.endsWith('/save-check')&&f.live){
  f.calls.push({path:p,method,body,query:u.search});
  return u.searchParams.get('allow_runtime_prompt_update')==='1'?Response.json({editable:false,runtime_prompts_editable:true}):Response.json({error:'直播中完整方案不能保存'},{status:409});
 }
 if(p.endsWith('/runtime-prompts')){
  f.calls.push({path:p,method,body});
  if(f.holdHot){f.holdHot=false;await new Promise(resolve=>f.releaseHot=resolve)}
  if(f.failHot||body.expected_revision!==f.hotRevision){f.failHot=false;return Response.json({error:'提示词版本冲突，当前草稿请先保留'},{status:409})}
  const doc=f.documents[plan];doc.sections.speech_requirements_md=body.speech_requirements_md;doc.sections.interaction_requirements_md=body.interaction_requirements_md;doc.rewrite_planning_md=body.rewrite_planning_md;
  f.hotRevision='r'+(Number(f.hotRevision.slice(1))+1);sessionStorage.setItem('hot-revision',f.hotRevision);sessionStorage.setItem('rewrite-documents',JSON.stringify(f.documents));
  return Response.json({...structuredClone(doc),...compilePlanPrompts(doc.sections),revision:f.hotRevision,runtime_hot_update:true});
 }
 const result=await delegatedHotFetch(url,init);
 if(p.endsWith('/prompt')&&result.ok){const doc=await result.json();return Response.json({...doc,revision:f.hotRevision})}
 return result;
};
`
fixture = fixture.replace('applySession(', setup + '\n' + hot + '\napplySession(')
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom', plugins: [{ name: 'hot-prompt-parent', enforce: 'pre', transform(code, id) {
  if (!id.endsWith('/src/views/LiveStrategyView.vue')) return
  return code.replace('</script>', `window.fixture.view={plan:()=>currentRoomPlanId.value,setPlan:id=>handlePlanSwitch({target:{value:String(id)}}),loaded:()=>planPromptLoadedPlan.value,busy:()=>savePlanOneClickBusy.value,hydrating:()=>fullShowWorkspaceHydrating.value,sections:()=>planPromptSections.value,planning:()=>planRewritePlanning.value,setDraft:(s,p)=>{planPromptSections.value={...planPromptSections.value,...s};planRewritePlanning.value=p}};</script>`)
} }] })
server.middlewares.use(async (req,res,next) => { if(req.url!=='/__hot') return next(); res.setHeader('Content-Type','text/html;charset=utf-8'); res.end(await server.transformIndexHtml(req.url,fixture)) })
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const checks=[], errors=[], external=[]
let page
try {
  const context=await browser.newContext({viewport:{width:1600,height:1100},reducedMotion:'reduce'})
  await context.route('**/*',route=>{const u=new URL(route.request().url());if(u.hostname==='127.0.0.1') return route.continue();external.push(u.hostname);return route.abort()})
  page=await context.newPage();page.setDefaultTimeout(15000);page.on('pageerror',e=>errors.push(e.message))
  const ready=()=>page.waitForFunction(()=>window.fixture?.view?.loaded()===window.fixture.view.plan()&&!window.fixture.view.busy()&&!window.fixture.view.hydrating())
  const open=async()=>{await page.getByRole('button',{name:'方案提示词',exact:true}).click();await ready()}
  const calls=()=>page.evaluate(()=>window.fixture.calls)
  const draft=()=>page.evaluate(()=>({...JSON.parse(JSON.stringify(window.fixture.view.sections())),planning:window.fixture.view.planning()}))
  await page.goto(server.resolvedUrls.local[0]+'__hot');await page.waitForFunction(()=>window.fixture?.view?.plan()===4);await open()
  const original=await page.evaluate(()=>structuredClone(window.fixture.documents[4]))
  await page.evaluate(()=>window.fixture.view.setDraft({persona_md:'尚未保存的人设',speech_requirements_md:'新的主线要求',interaction_requirements_md:'新的互动要求'},'新的话术规划'))
  await page.locator('#plan-save-button').click();await ready()
  await page.getByText('直播话术要求、互动回答要求、话术规划已热更新，后续生成使用新内容。其他修改需结束直播后保存。',{exact:true}).first().waitFor()
  const first=(await calls()).filter(c=>c.path.endsWith('/runtime-prompts')).at(-1)
  assert.deepEqual(Object.keys(first.body).sort(),['expected_revision','interaction_requirements_md','rewrite_planning_md','speech_requirements_md'])
  assert.equal(first.body.speech_requirements_md,'新的主线要求');assert.equal(first.body.rewrite_planning_md,'新的话术规划')
  assert.equal((await calls()).filter(c=>c.method==='POST'&&c.path.endsWith('/versions')||c.method==='PUT'&&c.path.endsWith('/prompt')).length,0)
  assert.equal((await draft()).persona_md,'尚未保存的人设')
  assert.equal(await page.evaluate(()=>window.fixture.documents[4].sections.persona_md),original.sections.persona_md)
  checks.push('直播中顶部保存只提交三项，三份新内容采用，未保存人设草稿保留，无版本发布或全提示词保存')
  await page.reload();await page.waitForFunction(()=>window.fixture?.view?.plan()===4);await open()
  assert.equal((await draft()).speech_requirements_md,'新的主线要求');assert.equal((await draft()).planning,'新的话术规划')
  checks.push('保存后刷新读取三项新原文')
  await page.evaluate(()=>{window.fixture.view.setDraft({interaction_requirements_md:'冲突时保留的草稿'},'冲突时保留的规划');window.fixture.failHot=true})
  await page.locator('#plan-save-button').click();await ready()
  assert.equal((await draft()).interaction_requirements_md,'冲突时保留的草稿');assert.equal((await draft()).planning,'冲突时保留的规划')
  checks.push('冲突/失败保留原草稿，不伪报保存成功')
  await page.evaluate(()=>window.fixture.holdHot=true);await page.locator('#plan-save-button').click();await page.waitForFunction(()=>typeof window.fixture.releaseHot==='function')
  assert.equal(await page.locator('#plan-save-button').isDisabled(),true)
  await page.evaluate(()=>window.fixture.releaseHot());await ready()
  checks.push('保存期间锁定重复提交')
  await page.evaluate(()=>{window.fixture.live=false;window.fixture.view.setDraft({speech_requirements_md:'停播后完整保存'},'停播后规划')})
  await page.locator('#plan-save-button').click();await ready()
  assert.ok((await calls()).some(c=>c.method==='POST'&&c.path.endsWith('/versions')))
  assert.ok((await calls()).some(c=>c.method==='PUT'&&c.path.endsWith('/prompt')))
  checks.push('停播后沿原完整版本和六页保存流程')
  assert.deepEqual(errors,[]);assert.deepEqual(external,[])
  await writeFile(output+'/result.json',JSON.stringify({checks,errors,external},null,2))
  console.log(JSON.stringify({passed:checks.length,errors,external}))
} catch(error) {
  await writeFile(output+'/failure.json',JSON.stringify({error:String(error),stack:error.stack,checks,errors,external,calls:page?await callsForFailure(page):[]},null,2))
  throw error
} finally { await browser.close(); await server.close() }
async function callsForFailure(page){return page.evaluate(()=>window.fixture?.calls||[])}
