// Entire real view; synthetic server state and intercepted writes only.
import assert from 'node:assert/strict'
import { readFile, mkdir } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { createServer } from 'vite'
const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url))
const {chromium}=require('playwright-core')
const source=await readFile(new URL('./test-interaction-full-view-location.mjs',import.meta.url),'utf8')
let fixture=source.match(/const fixture = `([\s\S]*?)`\nconst deferredWorkspace/)[1]
fixture=fixture.replace("name:'测试方案',room_ids", "name:'测试方案',status:'active',room_ids")
fixture=fixture.replace("window.fixture={calls:[],records,delayWorkspace:location.search.includes('delayed=1')};", `
const params=new URLSearchParams(location.search),options={buffer_seconds:120,body_min:2,body_max:4,max_chars:300};
window.fixture={calls:[],records,options,mode:params.get('mode')||'ai_dynamic',optionsError:params.has('options_error'),failSave:false,version:null,pool:{state:params.has('mode')||params.has('options_error')?'synced':'needs_update',changes:[],products:[],benefits:[],pools:[],set_id:9,saved_set_id:8,job:{}}};`)
fixture=fixture.replace("if(init.method&&init.method!=='GET')throw Error('Unexpected write '+p);", `
if(init.method==='POST'&&p.endsWith('/content-pools/update')){await new Promise(r=>window.fixture.releaseUpdate=r);window.fixture.pool.state='updating';return Response.json({state:'updating',job:{status:'running',stage:'正在更新内容'}});}
if(init.method==='POST'&&p.endsWith('/versions')){window.fixture.payload=JSON.parse(init.body);if(window.fixture.failSave){window.fixture.failSave=false;return Response.json({error:'模拟保存失败'},{status:500})}await new Promise(r=>window.fixture.releaseSave=r);window.fixture.pool.state='synced';window.fixture.pool.saved_set_id=9;window.fixture.version={id:80,plan_id:4,room_id:26,version_no:26,lifecycle_status:'published',duration_minutes:90,round_minutes:7,variants:[],voice_identity:{},generation_context:{content_mode:window.fixture.mode,anchor_generation:{buffer_seconds:120,config:{body_min:2,body_max:4,max_chars:300}}}};return Response.json(window.fixture.version);}
if(init.method&&init.method!=='GET')throw Error('Unexpected write '+p);`)
fixture=fixture.replace("if(p.endsWith('/content-mode'))return Response.json({content_mode:'static',allowed_modes:['static'],can_switch:false});", `
if(p.endsWith('/content-mode'))return Response.json({content_mode:window.fixture.mode,available_modes:['user_audio','ai_pregenerated','ai_dynamic'],dynamic_authorized:true});
if(p.endsWith('/content-pools'))return Response.json(window.fixture.pool);
if(p.endsWith('/realtime-options'))return window.fixture.optionsError?Response.json({error:'模拟参数读取失败'},{status:503}):Response.json(options);
if(p.endsWith('/versions'))return Response.json({items:window.fixture.version?[window.fixture.version]:[]});`)
const server=await createServer({server:{host:'127.0.0.1',port:0},appType:'custom'})
server.middlewares.use(async(req,res,next)=>{if(!req.url.startsWith('/__unified_save'))return next();res.setHeader('Content-Type','text/html;charset=utf-8');res.end(await server.transformIndexHtml('/__unified_save',fixture))})
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try {
 const page=await browser.newPage({viewport:{width:1800,height:1100},reducedMotion:'reduce'}),errors=[];page.on('pageerror',e=>errors.push(e.message))
 const base=server.resolvedUrls.local[0]+'__unified_save',main=page.locator('.pool-main-action .pool-primary')
 const writes=async suffix=>await page.evaluate(s=>window.fixture.calls.filter(c=>c.method==='POST'&&new URL(c.url,location.origin).pathname.endsWith(s)).length,suffix)
 await page.goto(base);await page.getByRole('button',{name:'更新受影响内容',exact:true}).waitFor()
 assert.equal(await writes('/versions'),0);assert.equal(await writes('/content-pools/update'),0,'mount does not perform paid updates')
 assert.equal(await page.locator('.strategy-plan-container-head .control-save').count(),0,'one primary action replaces the jump and second button')
 await main.click();await page.locator('#pool-primary-hint').filter({hasText:'内容正在认真准备中，完成后就能保存啦～'}).waitFor();assert.equal(await main.isDisabled(),true)
 await main.evaluate(el=>el.click());assert.equal(await writes('/content-pools/update'),1)
 await page.evaluate(()=>window.fixture.releaseUpdate());await page.getByText('正在更新受影响内容',{exact:true}).waitFor()
 await page.evaluate(()=>window.fixture.pool.state='ready');await page.getByRole('button',{name:'刷新',exact:true}).click()
 await page.waitForFunction(()=>document.querySelector('.pool-main-action button')?.textContent.trim()==='保存智能体'&&!document.querySelector('.pool-main-action button').disabled)
 await main.click();await page.waitForFunction(()=>!!window.fixture.releaseSave);await page.locator('#pool-primary-hint').filter({hasText:'正在把方案收好，稍等一下哦～'}).waitFor()
 await main.evaluate(el=>el.click());assert.equal(await writes('/versions'),1,'repeated save is blocked while preparing or saving')
 const payload=await page.evaluate(()=>window.fixture.payload);assert.equal(payload.room_id,26);assert.equal(payload.tenant_id,14);assert.equal(payload.generation_context.content_mode,'ai_dynamic');assert.deepEqual(payload.generation_context.anchor_generation_options,{buffer_seconds:120,body_min:2,body_max:4,max_chars:300})
 assert.equal(await page.locator('.workflow-step-products.active').count(),1,'save does not navigate away from the current module')
 await page.evaluate(()=>{window.fixture.releaseSave();window.fixture.releaseSave=null});await page.getByText('方案已保存好啦，下次开播就用它～',{exact:true}).waitFor()
 assert.equal(await page.getByRole('button',{name:'保存智能体',exact:true}).isEnabled(),true,'synced state still exposes direct save')
 await page.evaluate(()=>window.fixture.failSave=true);await main.click();await page.getByText('模拟保存失败',{exact:true}).waitFor();assert.equal(await main.isEnabled(),true,'save can be retried after failure')
 assert.equal(await writes('/content-pools/update'),1,'saving never triggers an extra pool update')
 await page.goto(base+'?mode=user_audio');await page.locator('#pool-primary-hint').filter({hasText:'还差一段主线录音哦～去“生成智能体”上传并完成处理，就能保存啦。'}).waitFor();assert.equal(await main.isDisabled(),true);await main.evaluate(el=>el.click());assert.equal(await writes('/versions'),0)
 await page.setViewportSize({width:390,height:1000});await page.locator('.pool-main-action').scrollIntoViewIfNeeded()
 const button=await main.boundingBox(),hint=await page.locator('#pool-primary-hint').boundingBox();assert.ok(hint.y>=button.y+button.height);assert.equal(button.width,224);assert.equal(button.height,58);assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
 const out=new URL('../../deploy/validation/unified-plan-save/',import.meta.url);await mkdir(out,{recursive:true});await page.screenshot({path:new URL('disabled-mobile.png',out).pathname.replace(/^\/([A-Z]:)/,'$1')})
 await page.setViewportSize({width:1800,height:1100});await page.locator('.pool-notice').screenshot({path:new URL('disabled-desktop.png',out).pathname.replace(/^\/([A-Z]:)/,'$1')})
 await page.goto(base+'?options_error=1');await page.locator('#pool-primary-hint').filter({hasText:'实时声音参数没读到，去“生成智能体”点重试吧～'}).waitFor();assert.equal(await main.isDisabled(),true)
 assert.deepEqual(errors,[]);console.log('PASS: real full view; explicit update→ready→direct save→synced; scoped payload; duplicate prevention; error/retry; missing recording/parameters show hints below disabled button; fixed dimensions; mobile layout; no live writes')
} finally {await browser.close();await server.close()}
