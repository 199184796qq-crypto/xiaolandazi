// Actual parent save button, API wrapper and four-page editor. All endpoints are local fixtures.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createServer } from 'vite'
const { chromium } = createRequire(new URL('../../collector-worker/package.json', import.meta.url))('playwright-core')
const output = process.env.PLAN_PROMPT_TEST_OUTPUT || 'E:/直播伴播-local/plan-prompt-raw-roundtrip-20261031/browser'
await mkdir(output, { recursive: true })
const base = (await readFile(new URL('./test-plan-prompt-editor.mjs', import.meta.url), 'utf8')).replace(/\r\n/g, '\n')
let fixture = base.split('const fixture=String.raw`')[1].split('`\nconst server=')[0]
const setup = String.raw`
import {emptyPlanPromptSections,compilePlanPrompts} from '/src/planPrompt.ts';
const previousFetch=window.fetch;
const bindings=[101,102].map((id,i)=>({id,tenant_id:14,profile_id:201+i,sample_asset_id:1,provider:'aliyun_qwen',model:'test-model',voice_id:'test-voice-'+id,rate:1,status:'ready'}));
const voice=id=>{const b=bindings[id===4?0:1];return {name:'声音'+id,version:'V1',source:'clone',provider:b.provider,model:b.model,voice_id:b.voice_id,binding_id:b.id,profile_id:b.profile_id,rate:1,emotion_enabled:false,style:{}}};
const versions=Object.fromEntries([4,5].map(id=>[id,{id:id*10,tenant_id:14,plan_id:id,version_no:1,lifecycle_status:'published',duration_minutes:90,round_minutes:7,voice_identity:voice(id),variants:[],generation_context:{content_mode:'ai_dynamic',anchor_generation_options:{buffer_seconds:120,body_min:2,body_max:4,max_chars:300}}}]));
Object.assign(window.fixture,{raw:JSON.parse(sessionStorage.getItem('raw-pages')||'null')||{4:emptyPlanPromptSections(),5:{persona_md:'方案五人设',facts_md:'方案五事实',speech_requirements_md:'方案五话术',interaction_requirements_md:'方案五回答'}},legacy:{},versions});
window.fetch=async(url,init={})=>{
 const f=window.fixture,u=new URL(url,location.origin),p=u.pathname,method=init.method||'GET',body=init.body?JSON.parse(init.body):null,plan=Number(p.match(/live-agent-plans\/(\d+)/)?.[1]||0);
 const prompt=p.endsWith('/prompt');
 if(!prompt&&!p.endsWith('/workspace')&&!p.endsWith('/versions')&&!p.endsWith('/official-voices')&&!p.endsWith('/voice-model-bindings'))return previousFetch(url,init);
 f.calls.push({path:p,method,body});
 if(p.endsWith('/voice-model-bindings'))return Response.json(bindings);
 if(p.endsWith('/official-voices'))return Response.json({configured:true,items:[]});
 if(p.endsWith('/workspace'))return Response.json({version:versions[plan],source:'published',checks:[]});
 if(p.endsWith('/versions')){
  if(method==='POST'){
   if(f.holdVersion){f.holdVersion=false;await new Promise(r=>f.releaseVersion=r)}
   if(f.failVersion){f.failVersion=false;return Response.json({error:'正式版本保存失败'},{status:500})}
   versions[plan]={id:versions[plan].id+1,plan_id:plan,version_no:versions[plan].version_no+1,lifecycle_status:'published',...body,generation_context:{...body.generation_context,anchor_generation:{buffer_seconds:body.generation_context.anchor_generation_options.buffer_seconds,config:body.generation_context.anchor_generation_options}}};
   return Response.json(versions[plan]);
  }
  return Response.json({items:[versions[plan]]});
 }
 if(method==='PUT'){
  if(f.failPrompt){f.failPrompt=false;return Response.json({error:'原文保存失败，请重试'},{status:500})}
  if(!body.sections)throw Error('Original sections missing');
  f.raw[plan]=structuredClone(body.sections);delete f.legacy[plan];sessionStorage.setItem('raw-pages',JSON.stringify(f.raw));
 }
 if(f.legacy[plan])return Response.json({sections:emptyPlanPromptSections(),legacy_prompt_md:f.legacy[plan],prompt_md:f.legacy[plan],interaction_prompt_md:f.legacy[plan],speech_prompt_md:'',legacy_unstructured:true});
 const sections=f.raw[plan]||emptyPlanPromptSections(),compiled=compilePlanPrompts(sections);
 return Response.json({sections,...compiled,prompt_md:compiled.interaction_prompt_md});
};
`
fixture = fixture.replace('applySession(', setup + '\napplySession(')
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom', plugins: [{ name: 'raw-pages-inspect', enforce: 'pre', transform(code, id) {
  if (!id.endsWith('/src/views/LiveStrategyView.vue')) return
  return code.replace('</script>', 'window.fixture.view={plan:()=>currentRoomPlanId.value,setPlan:id=>handlePlanSwitch({target:{value:String(id)}}),loaded:()=>planPromptLoadedPlan.value,loading:()=>planPromptLoading.value,busy:()=>savePlanOneClickBusy.value,blocked:()=>fullShowSaveBlockedReason.value,hydrating:()=>fullShowWorkspaceHydrating.value};</script>')
} }] })
server.middlewares.use(async (req, res, next) => {
  if (req.url !== '/__raw-pages') return next()
  res.setHeader('Content-Type', 'text/html;charset=utf-8'); res.end(await server.transformIndexHtml(req.url, fixture))
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const checks = [], errors = [], external = []
let page
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1100 }, reducedMotion: 'reduce' })
  await context.route('**/*', route => { const u = new URL(route.request().url()); if (u.hostname === '127.0.0.1') return route.continue(); external.push(u.hostname); return route.abort() })
  page = await context.newPage(); page.setDefaultTimeout(20000); page.on('pageerror', e => errors.push(e.message))
  const labels = { persona_md: '主播人设', facts_md: '事实依据', speech_requirements_md: '直播话术要求', interaction_requirements_md: '互动回答要求' }
  const editor = page.locator('#plan-prompt-content')
  const tab = key => page.getByRole('tab', { name: labels[key], exact: true })
  const open = async () => { await page.getByRole('button', { name: '方案提示词', exact: true }).click(); await page.locator('#plan-prompt-content:not([readonly])').waitFor() }
  const fill = async data => { for (const [key, value] of Object.entries(data)) { await tab(key).click(); await editor.fill(value) } }
  const assertPages = async data => { for (const [key, value] of Object.entries(data)) { await tab(key).click(); assert.equal(await editor.inputValue(), value, key) } }
  const switchTo = async id => { await page.evaluate(id => window.fixture.view.setPlan(id), id); await page.waitForFunction(id => window.fixture.view.loaded() === id && !window.fixture.view.loading() && !window.fixture.view.hydrating(), id) }
  const puts = () => page.evaluate(() => window.fixture.calls.filter(c => c.method === 'PUT' && c.path.endsWith('/prompt')))
  const topSave = () => page.locator('#plan-save-button').click()
  const readySave = () => page.waitForFunction(() => !window.fixture.view.busy() && !window.fixture.view.blocked() && !window.fixture.view.hydrating())
  const pageSave = async () => { await page.getByRole('button', { name: '保存方案提示词', exact: true }).click(); await page.getByText('方案提示词已保存，已生成主播互动元提示词和主播话术生成提示词', { exact: true }).waitFor() }
  await page.goto(server.resolvedUrls.local[0] + '__raw-pages'); await page.waitForFunction(() => window.fixture?.view?.plan() === 4); await open(); await readySave()
  const original = { persona_md: '  人设原文\n\n【事实依据】\n这个标题仍属于人设页\n  ', facts_md: '\n〔提示〕此说明只保存在原文\n  本页事实  \n', speech_requirements_md: '\n\n', interaction_requirements_md: '  互动页原文\n【主播人设】\n这个标题不能移动\n' }
  await fill(original); await pageSave(); assert.deepEqual((await puts()).at(-1).body.sections, original)
  await switchTo(5); await assertPages({ persona_md: '方案五人设', facts_md: '方案五事实', speech_requirements_md: '方案五话术', interaction_requirements_md: '方案五回答' }); await switchTo(4); await assertPages(original)
  await page.reload(); await page.waitForFunction(() => window.fixture?.view?.plan() === 4); await open(); await readySave(); await assertPages(original)
  checks.push('单独保存、切换另一方案后返回、刷新：四页首尾空白/空页/同名标题/提示行逐字保持')
  await page.getByText('查看两份元提示词合成预览', { exact: true }).click()
  const compiled = await page.getByRole('textbox', { name: '主播互动元提示词', exact: true }).inputValue()
  assert.ok(!compiled.includes('〔提示〕') && compiled.includes('这个标题仍属于人设页'))
  await assertPages(original)
  checks.push('两份合成预览只处理副本，不反写或重新拆分四页')

  const clicked = { ...original, facts_md: '  顶部保存的事实原文\n\n', speech_requirements_md: '  只属主线\n〔提示〕不发送模型\n' }
  await fill(clicked); await page.evaluate(() => window.fixture.holdVersion = true); await topSave(); await page.waitForFunction(() => !!window.fixture.releaseVersion)
  assert.equal(await editor.getAttribute('readonly'), '')
  await page.evaluate(() => window.fixture.releaseVersion()); await page.waitForFunction(() => window.fixture.raw[4].facts_md.includes('顶部保存')); await readySave()
  assert.deepEqual((await puts()).at(-1).body.sections, clicked)
  await switchTo(5); await switchTo(4); await assertPages(clicked)
  checks.push('真正点击顶部保存方案：正式版本请求期间锁定编辑，刷新其它资产后仍提交点击时四页快照，返回回显一致')

  const failedDraft = { ...clicked, persona_md: '  失败时也保留的原始人设\n' }
  await fill(failedDraft); await readySave(); await page.evaluate(() => window.fixture.failPrompt = true); const before = (await puts()).length
  await topSave(); await page.waitForFunction(() => !window.fixture.view.busy()); assert.equal((await puts()).length, before + 1); await assertPages(failedDraft)
  await page.getByText(/方案版本已保存，但方案提示词未保存/).waitFor()
  await page.getByRole('button', { name: '商品资料', exact: true }).click(); await open(); await assertPages(failedDraft)
  await pageSave(); await switchTo(5); await switchTo(4); await assertPages(failedDraft)
  checks.push('正式版本成功而原文保存失败：明确提示部分成功且保留四页草稿，可重试原文保存')

  await readySave(); await page.evaluate(() => window.fixture.failVersion = true); const beforeFailure = (await puts()).length
  await topSave(); await page.waitForFunction(() => !window.fixture.view.busy()); assert.equal((await puts()).length, beforeFailure); await assertPages(failedDraft)
  checks.push('正式版本保存失败时不继续覆盖原文')

  const blank = { persona_md: '', facts_md: '', speech_requirements_md: '', interaction_requirements_md: '' }
  await fill(blank); await pageSave(); await switchTo(5); await switchTo(4); await assertPages(blank)
  assert.deepEqual((await puts()).at(-1).body.sections, blank)
  checks.push('显式清空四页也能保存，返回不会从旧合成内容复活原文')

  const legacy = '【主播人设】\n旧完整人设\n【事实依据】\n旧完整事实\n【互动回答要求】\n旧完整回答'
  await page.evaluate(value => window.fixture.legacy[5] = value, legacy); await switchTo(5); await assertPages(blank)
  await page.getByText('查看旧版完整提示词', { exact: true }).click(); assert.equal(await page.getByRole('textbox', { name: '旧版完整提示词', exact: true }).inputValue(), legacy)
  await readySave(); const legacyPuts = (await puts()).length; await topSave(); await page.waitForFunction(() => !window.fixture.view.busy()); assert.equal((await puts()).length, legacyPuts)
  assert.equal(await page.evaluate(() => window.fixture.legacy[5]), legacy)
  await fill({ ...blank, interaction_requirements_md: '  我自己填写的新原文\n【事实依据】\n仍在互动页  ' }); await pageSave(); await switchTo(4); await switchTo(5)
  await assertPages({ ...blank, interaction_requirements_md: '  我自己填写的新原文\n【事实依据】\n仍在互动页  ' })
  assert.equal(await page.getByText('查看旧版完整提示词', { exact: true }).count(), 0)
  checks.push('旧版整份提示词独立展示、不按标题自动分配；仅保存正式版本不擦除旧文，填写四页后才替换')

  const hidden = { persona_md: '  未打开提示词页也要保留\n', facts_md: '\n原始事实\n', speech_requirements_md: '', interaction_requirements_md: ' 回答要求 ' }
  await page.evaluate(value => { window.fixture.raw[4] = value; sessionStorage.setItem('raw-pages', JSON.stringify(window.fixture.raw)) }, hidden)
  await switchTo(4)
  await page.reload(); await page.waitForFunction(() => window.fixture?.view?.plan() === 4); await readySave(); await topSave(); await page.waitForFunction(() => !window.fixture.view.busy()); assert.deepEqual((await puts()).at(-1).body.sections, hidden)
  await open(); await assertPages(hidden)
  checks.push('未打开提示词页直接保存方案：先读取原始四字段，避免用未加载空稿覆盖')
  await page.locator('.plan-prompt-editor').screenshot({ path: output + '/raw-pages.png' })
  const mutations = await page.evaluate(() => window.fixture.calls.filter(c => c.method !== 'GET' && !c.path.endsWith('/prompt') && !c.path.endsWith('/versions') && !c.path.endsWith('/fact-gap-check')))
  assert.deepEqual(mutations, []); assert.deepEqual(errors, []); assert.deepEqual(external, [])
  await writeFile(output + '/checks.json', JSON.stringify({ passed: true, checks, errors, external, scope: 'real parent save handlers and API, local endpoints; no production/model/TTS calls' }, null, 2)); console.log(JSON.stringify({ passed: true, checks }))
} catch (error) {
  if (page) await page.screenshot({ path: output + '/failure.png', fullPage: true })
  const state = page ? await page.evaluate(() => ({ calls: window.fixture?.calls, versions: window.fixture?.versions, blocked: window.fixture?.view?.blocked(), busy: window.fixture?.view?.busy() })) : null
  await writeFile(output + '/failure.json', JSON.stringify({ error: String(error), stack: error.stack, checks, errors, external, state }, null, 2)); throw error
} finally { await browser.close(); await server.close() }
