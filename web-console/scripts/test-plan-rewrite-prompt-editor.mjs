// Real parent, editor and API wrapper; every business endpoint is a local fixture.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createServer } from 'vite'
const { chromium } = createRequire(new URL('../../collector-worker/package.json', import.meta.url))('playwright-core')
const output = process.env.PLAN_PROMPT_TEST_OUTPUT || 'E:/直播伴播-local/plan-rewrite-prompt-editor-20261103/browser'
await mkdir(output, { recursive: true })
const source = (await readFile(new URL('./test-plan-prompt-editor.mjs', import.meta.url), 'utf8')).replace(/\r\n/g, '\n')
let fixture = source.split('const fixture=String.raw`')[1].split('`\nconst server=')[0]
const setup = String.raw`
import {emptyPlanPromptSections,compilePlanPrompts,compilePlanRewritePrompt} from '/src/planPrompt.ts';
const previousFetch=window.fetch;
const bindings=[101,102].map((id,i)=>({id,tenant_id:14,profile_id:201+i,sample_asset_id:1,provider:'aliyun_qwen',model:'test-model',voice_id:'test-voice-'+id,rate:1,status:'ready'}));
const voice=id=>{const b=bindings[id===4?0:1];return {name:'声音'+id,version:'V1',source:'clone',provider:b.provider,model:b.model,voice_id:b.voice_id,binding_id:b.id,profile_id:b.profile_id,rate:1,emotion_enabled:false,style:{}}};
const versions=Object.fromEntries([4,5].map(id=>[id,{id:id*10,tenant_id:14,plan_id:id,version_no:1,lifecycle_status:'published',duration_minutes:90,round_minutes:7,voice_identity:voice(id),variants:[],generation_context:{content_mode:'user_audio',recording_voice_mode:'stream_clone',recording_script:{version:2,tabs:[{tab_id:'g1',title:'测试话术',pinned:true,entries:[{entry_id:'e1',text:'这是本地测试话术。'.repeat(50)}],audio_asset_ids:[]}],active_tab_id:'g1'},anchor_generation_options:{buffer_seconds:120,body_min:2,body_max:4,max_chars:300}}}]));
const defaults=Object.fromEntries([4,5].map(id=>{const sections={...emptyPlanPromptSections(),persona_md:'方案'+id+'人设',facts_md:'方案'+id+'事实'};const planning='方案'+id+'现有规划正文\n{{已有开场}}';return [id,{sections,rewrite_planning_md:planning,rewrite_prompt_md:compilePlanRewritePrompt(sections,planning)}]}));
Object.assign(window.fixture,{documents:JSON.parse(sessionStorage.getItem('rewrite-documents')||'null')||defaults,versions});
window.fetch=async(url,init={})=>{
 const f=window.fixture,u=new URL(url,location.origin),p=u.pathname,method=init.method||'GET',body=init.body?JSON.parse(init.body):null,plan=Number(p.match(/live-agent-plans\/(\d+)/)?.[1]||0);
 if(!/\/(prompt(?:\/render)?|workspace|versions|official-voices|voice-model-bindings|voice-profiles|content-mode|save-check)$/.test(p))return previousFetch(url,init);
 f.calls.push({path:p,method,body});
 if(p.endsWith('/content-mode'))return Response.json({content_mode:'user_audio',available_modes:['user_audio'],dynamic_authorized:true});
 if(p.endsWith('/save-check'))return Response.json({editable:true});
 if(p.endsWith('/voice-profiles'))return Response.json([]);
 if(p.endsWith('/voice-model-bindings'))return Response.json(bindings);
 if(p.endsWith('/official-voices'))return Response.json({configured:true,items:[]});
 if(p.endsWith('/workspace'))return Response.json({version:versions[plan],source:'published',checks:[]});
 if(p.endsWith('/versions')){
  if(method==='POST'){
   if(f.holdVersion){f.holdVersion=false;await new Promise(r=>f.releaseVersion=r)}
   if(f.failVersion){f.failVersion=false;return Response.json({error:'正式版本保存失败'},{status:500})}
   versions[plan]={id:versions[plan].id+1,plan_id:plan,version_no:versions[plan].version_no+1,lifecycle_status:'published',...body,generation_context:body.generation_context};
   return Response.json(versions[plan]);
  }
  return Response.json({items:[versions[plan]]});
 }
 if(p.endsWith('/render'))return Response.json({sections:{persona:'重新生成的人设',facts:'重新生成的事实',speech:'新的话术要求',interaction:'新的回答要求'},facts_md:'重新生成的事实'});
 if(method==='PUT'){
  if(f.failPrompt){f.failPrompt=false;return Response.json({error:'提示词保存失败，请重试'},{status:500})}
  if(typeof body.rewrite_planning_md!=='string'||'rewrite_prompt_md' in body)throw Error('only original planning must be submitted');
  f.documents[plan]={...structuredClone(body),rewrite_prompt_md:compilePlanRewritePrompt(body.sections,body.rewrite_planning_md)};sessionStorage.setItem('rewrite-documents',JSON.stringify(f.documents));
 }
 const doc=structuredClone(f.documents[plan]);
 if(method==='GET'&&f.holdRead){f.holdRead=false;await new Promise(r=>f.releaseRead=r)}
 const compiled=compilePlanPrompts(doc.sections);
 return Response.json({...doc,...compiled,prompt_md:compiled.interaction_prompt_md});
};
`
fixture = fixture.replace('applySession(', setup + '\napplySession(')
const server = await createServer({ server: { host: '127.0.0.1', port: 55173 }, appType: 'custom', plugins: [{ name: 'rewrite-parent-inspect', enforce: 'pre', transform(code, id) {
  if (!id.endsWith('/src/views/LiveStrategyView.vue')) return
  return code.replace('</script>', 'window.fixture.view={plan:()=>currentRoomPlanId.value,setPlan:id=>handlePlanSwitch({target:{value:String(id)}}),loaded:()=>planPromptLoadedPlan.value,loading:()=>planPromptLoading.value,busy:()=>savePlanOneClickBusy.value,blocked:()=>fullShowSaveBlockedReason.value,hydrating:()=>fullShowWorkspaceHydrating.value,error:()=>fullShowError.value};</script>')
} }] })
server.middlewares.use(async (req, res, next) => {
  if (req.url !== '/__rewrite-editor') return next()
  res.setHeader('Content-Type', 'text/html;charset=utf-8'); res.end(await server.transformIndexHtml(req.url, fixture))
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const checks = [], errors = [], external = []
let page
try {
  const context = await browser.newContext({ viewport: { width: 1600, height: 1100 }, reducedMotion: 'reduce' })
  await context.route('**/*', route => { const u = new URL(route.request().url()); if (u.hostname === '127.0.0.1') return route.continue(); external.push(u.hostname); return route.abort() })
  page = await context.newPage(); page.setDefaultTimeout(15000); page.on('pageerror', e => errors.push(e.message))
  const tab = name => page.getByRole('tab', { name, exact: true })
  const editor = page.locator('#plan-prompt-content')
  const open = async () => { await page.getByRole('button', { name: '方案提示词', exact: true }).click(); await editor.locator('xpath=self::*[not(@readonly)]').waitFor() }
  const ready = () => page.waitForFunction(() => !window.fixture.view.busy() && !window.fixture.view.blocked() && !window.fixture.view.hydrating() && !window.fixture.view.loading())
  const switchTo = async id => { await page.evaluate(id => window.fixture.view.setPlan(id), id); await page.waitForFunction(id => window.fixture.view.loaded() === id && !window.fixture.view.loading() && !window.fixture.view.hydrating(), id) }
  const puts = () => page.evaluate(() => window.fixture.calls.filter(c => c.method === 'PUT' && c.path.endsWith('/prompt')))
  const showRewrite = async () => { await tab('话术规划').click(); return editor.inputValue() }
  const savePrompt = async () => { await page.getByRole('button', { name: '保存方案提示词', exact: true }).click(); await page.getByText('方案提示词已保存，已生成主播互动元提示词和主播话术生成提示词', { exact: true }).waitFor() }
  await page.goto(server.resolvedUrls.local[0] + '__rewrite-editor'); await page.waitForFunction(() => window.fixture?.view?.plan() === 4); await open(); await ready()
  assert.deepEqual(await page.getByRole('tab').allTextContents(), ['主播人设','事实依据','红线禁止','直播话术要求','互动回答要求','话术规划'])
  assert.equal(await showRewrite(), '方案4现有规划正文\n{{已有开场}}')
  assert.ok(!(await editor.inputValue()).includes('【事实依据】')); checks.push('话术规划紧跟互动回答要求，只回显规划原文，完整元提示词不混进编辑框')
  const draft = '  自定义规划\n\n{{已有开场}}\n{{开场方式}}\n〔提示〕原样保留😀\n  '
  await editor.fill(draft); await tab('互动回答要求').click(); await editor.fill('互动专用要求'); assert.equal(await showRewrite(), draft)
  await savePrompt(); assert.equal((await puts()).at(-1).body.rewrite_planning_md, draft)
  assert.equal((await puts()).at(-1).body.sections.interaction_requirements_md, '互动专用要求')
  await switchTo(5); assert.equal(await showRewrite(), '方案5现有规划正文\n{{已有开场}}'); await switchTo(4); assert.equal(await showRewrite(), draft)
  await page.reload(); await page.waitForFunction(() => window.fixture?.view?.plan() === 4); await open(); await ready(); assert.equal(await showRewrite(), draft)
  checks.push('单独保存、切换方案和刷新均逐字回显：空白、换行、占位符和 Unicode 保持，另外两份提示词独立')
  await tab('事实依据').click(); await editor.fill('更新后的方案事实：八十七元六角'); await savePrompt(); assert.equal(await showRewrite(), draft)
  const compiledDoc = await page.evaluate(() => window.fixture.documents[4])
  assert.ok(compiledDoc.rewrite_prompt_md.startsWith('你是直播话术改写器。'))
  assert.ok(compiledDoc.rewrite_prompt_md.includes('【事实依据】\n更新后的方案事实：八十七元六角'))
  assert.ok(compiledDoc.rewrite_prompt_md.includes('【样本话术】\n{{样本话术}}\n\n' + draft))
  assert.equal(compiledDoc.rewrite_prompt_md.split('{{样本话术}}').length - 1, 1)
  checks.push('更新事实后重新合成完整提示词；样本节与占位符保留，规划正文原样独立保存')

  const topDraft = draft + '顶部保存'
  await editor.fill(topDraft); await page.evaluate(() => window.fixture.holdVersion = true); await page.locator('#plan-save-button').click(); await page.waitForFunction(() => !!window.fixture.releaseVersion)
  assert.equal(await editor.getAttribute('readonly'), '')
  await page.evaluate(() => window.fixture.releaseVersion()); await ready(); assert.equal((await puts()).at(-1).body.rewrite_planning_md, topDraft)
  await switchTo(5); await switchTo(4); assert.equal(await showRewrite(), topDraft)
  checks.push('真正点击顶部保存方案：正式版本保存期间锁定编辑，提交点击时的规划快照并回显')
  const failedDraft = topDraft + '失败后保留'
  await editor.fill(failedDraft); await page.evaluate(() => window.fixture.failPrompt = true); await page.locator('#plan-save-button').click(); await ready()
  await page.getByText(/方案版本已保存，但方案提示词未保存/).first().waitFor(); assert.equal(await showRewrite(), failedDraft)
  await savePrompt(); assert.equal(await showRewrite(), failedDraft)
  const before = (await puts()).length
  await page.evaluate(() => window.fixture.failVersion = true); await page.locator('#plan-save-button').click(); await ready(); assert.equal((await puts()).length, before); assert.equal(await showRewrite(), failedDraft)
  checks.push('提示词保存失败保留草稿并可重试；正式版本失败不继续覆盖规划')
  await page.getByRole('button', { name: '重新生成', exact: true }).click()
  await page.getByRole('dialog', { name: '重新生成前五页文本框？', exact: true }).waitFor()
  await page.getByRole('button', { name: '确认重新生成', exact: true }).click(); await page.getByText(/已按模板重新生成/).waitFor(); assert.equal(await showRewrite(), failedDraft)
  checks.push('重新生成前五页保留手工话术规划，不用母版覆盖修改元提示词')
  await editor.fill(''); await savePrompt(); await switchTo(5); await switchTo(4); assert.equal(await showRewrite(), '')
  checks.push('显式清空规划后仍保存空值，不复活上一份文本')
  await page.evaluate(() => { window.fixture.documents[4].rewrite_planning_md = '隐藏页读取的规划'; sessionStorage.setItem('rewrite-documents', JSON.stringify(window.fixture.documents)) })
  await page.reload(); await page.waitForFunction(() => window.fixture?.view?.plan() === 4); await ready(); await page.locator('#plan-save-button').click(); await ready()
  assert.equal((await puts()).at(-1).body.rewrite_planning_md, '隐藏页读取的规划'); await open(); assert.equal(await showRewrite(), '隐藏页读取的规划')
  checks.push('未打开提示词页直接保存方案，会先加载已有规划，避免空稿覆盖')
  await switchTo(5); await page.evaluate(() => window.fixture.holdRead = true); await page.evaluate(() => window.fixture.view.setPlan(4)); await page.waitForFunction(() => !!window.fixture.releaseRead)
  await switchTo(5); await page.evaluate(() => window.fixture.releaseRead()); await page.waitForFunction(() => !window.fixture.view.loading()); assert.equal(await showRewrite(), '方案5现有规划正文\n{{已有开场}}')
  checks.push('旧方案迟到读取不串写新方案规划')
  await switchTo(4); await showRewrite()
  assert.equal(await editor.evaluate(el => getComputedStyle(el).fontSize), '20px')
  await page.locator('.plan-prompt-editor').screenshot({ path: output + '/desktop.png' })
  await page.setViewportSize({ width: 390, height: 900 }); await tab('话术规划').click()
  const size = await page.locator('.plan-prompt-editor').evaluate(el => ({ width: el.getBoundingClientRect().width, scroll: el.scrollWidth, client: el.clientWidth }))
  assert.ok(size.scroll <= size.client + 1, JSON.stringify(size)); await page.locator('.plan-prompt-editor').screenshot({ path: output + '/mobile.png' })
  checks.push('桌面与 390px 标签自动换行无横向溢出，规划文本保持 20px')
  assert.deepEqual(errors, []); assert.deepEqual(external, [])
  await writeFile(output + '/checks.json', JSON.stringify({ passed: true, checks, errors, external, scope: 'real parent/editor/API; local fixture endpoints only; no production/model/TTS calls' }, null, 2))
  console.log(JSON.stringify({ passed: true, checks }))
} catch (error) {
  if (page) await page.screenshot({ path: output + '/failure.png', fullPage: true })
  const state = page ? await page.evaluate(() => ({ calls: window.fixture?.calls, busy: window.fixture?.view?.busy(), blocked: window.fixture?.view?.blocked(), error: window.fixture?.view?.error() })) : null
  await writeFile(output + '/failure.json', JSON.stringify({ error: String(error), stack: error.stack, checks, errors, external, state }, null, 2)); throw error
} finally { await browser.close(); await server.close() }
