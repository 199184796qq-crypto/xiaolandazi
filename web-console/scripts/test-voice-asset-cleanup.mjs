import assert from 'node:assert/strict'
import {createRequire} from 'node:module'
import {mkdir,writeFile} from 'node:fs/promises'
import {fileURLToPath} from 'node:url'
import {createServer} from 'vite'
const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url))
const {chromium}=require('playwright-core')
const out=process.env.VOICE_CLEANUP_TEST_OUTPUT||'E:/直播伴播-local/oss-voice-cleanup-20261104/browser'
const fixtureID=fileURLToPath(new URL('../src/__voice-cleanup-fixture.vue',import.meta.url)).replaceAll('\\','/')
const server=await createServer({server:{host:'127.0.0.1',port:0},plugins:[{name:'voice-cleanup-parent-fixture',resolveId(id){if(id==='/__voice-cleanup-fixture.vue')return fixtureID},load(id){if(id===fixtureID)return '<script setup>import View from "/src/views/UserVoiceCacheSettingsView.vue";import "/src/style.css";</script><template><View/></template>'},configureServer(dev){dev.middlewares.use(async(req,res,next)=>{if(req.url!=='/__voice-cleanup-test')return next();res.setHeader('Content-Type','text/html;charset=utf-8');res.end(await dev.transformIndexHtml(req.url,'<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="icon" href="data:,"><div id="app"></div><script type="module">import {createApp} from "vue";import View from "/__voice-cleanup-fixture.vue";createApp(View).mount("#app")</script>'))})}}]})
const checks=[],errors=[],external=[],clears=[];let failClear=true,failPreview=false
await mkdir(out,{recursive:true});await server.listen()
const browser=await chromium.launch({executablePath:process.env.CHROME_PATH||'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try{
 const context=await browser.newContext({viewport:{width:1440,height:1000}})
 await context.route('**/*',r=>{if(new URL(r.request().url()).hostname==='127.0.0.1')return r.continue();external.push(r.request().url());return r.abort()})
 await context.route('**/api/**',async route=>{
  const req=route.request(),path=new URL(req.url()).pathname;let value={};const send=(v,status=200)=>route.fulfill({status,contentType:'application/json',body:JSON.stringify(v)})
  if(path==='/api/v1/liveops/user-voice-cache-policies')value={items:[14,15].map(id=>({tenant_id:id,user_id:id,username:'u'+id,display_name:'用户'+id,retention_hours:5,mainline_cache_reuse_enabled:true,interaction_cache_reuse_enabled:true,using_default:true}))}
  else if(path==='/api/v1/system/anchor-voice-cache-policy')value={retention_hours:5,mainline_cache_reuse_enabled:true,interaction_cache_reuse_enabled:true}
  else if(path==='/api/v1/system/speech-library-policy')value={policy:{capacity:4,one_rate:30,two_rate:60,full_rate:70,cooldown_seconds:180},stats:{Texts:0,Audio:0,PlayedReuses:0},revision:'fixture'}
  else if(path.endsWith('/voice-assets/plans')){const tenant=new URL(req.url()).searchParams.get('tenant_id');if(tenant==='15')await new Promise(r=>setTimeout(r,150));value={items:[{id:tenant==='14'?4:5,name:tenant==='14'?'许牛府方案':'另一方案'}]}}
  else if(path.endsWith('/voice-assets/voices'))value={items:[{key:'a'.repeat(64),label:'鹅 · 完整身份A'},{key:'b'.repeat(64),label:'鸭 · 完整身份B'}]}
  else if(path.endsWith('/voice-assets/jobs'))value={items:[]}
  else if(path.endsWith('/voice-assets/preview')){if(failPreview)return send({error:'清单失败'},503);const scope=req.postDataJSON();delete scope.mode;value={scope,expected:{scope,plan_epoch:0,voice_epoch:0},plan_name:'许牛府方案',room_ids:[29,30],counts:{candidate_records:25639,review_required:3,object_count_unverified:7,expected_bytes_unverified:4096,confirmed_objects:5,confirmed_missing_objects:2,confirmed_current_bytes:2048},physical_inventory_complete:true,bucket_version_state:'enabled'}}
  else if(path.endsWith('/voice-assets/clear')){clears.push(req.postDataJSON());if(failClear){failClear=false;return send({error:'模拟连接失败，使用同一编号重试'},503)}value={job:{id:clears.at(-1).request_id}}}
  else if(path.endsWith('/voice-assets/scan'))value={dry_run:true,counts:{idle_candidate:2,protected_reference:4,usage_unknown_review:10}}
  else throw Error('Unmocked API '+path)
  return send(value)
 })
 const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));await page.goto(server.resolvedUrls.local[0]+'__voice-cleanup-test');page.setDefaultTimeout(10000)
 const panel=page.locator('.voice-cleanup'),selects=panel.locator('select')
 await selects.nth(0).selectOption('14');await selects.nth(1).selectOption('4');await selects.nth(3).selectOption('a'.repeat(64))
 await panel.getByRole('button',{name:'预览清理范围',exact:true}).click();await panel.locator('.preview').waitFor()
 assert.match(await panel.innerText(),/候选资产 25639 条/);assert.match(await panel.innerText(),/当前存在 5 个/);assert.match(await panel.innerText(),/确认缺失 2 个/);checks.push('真实父页：资产行/登记对象/完整盘点实体分开，显示涉及房间与完整音色')
 await panel.getByRole('button',{name:'清除这些历史声音',exact:true}).click();await panel.getByRole('button',{name:'取消',exact:true}).click();assert.equal(clears.length,0);checks.push('确认取消不提交删除')
 await panel.getByRole('button',{name:'清除这些历史声音',exact:true}).click();await panel.getByRole('button',{name:'确认清除',exact:true}).click();await panel.locator('.error').waitFor();await panel.getByRole('button',{name:'确认清除',exact:true}).click();await panel.locator('.notice').waitFor()
 assert.equal(clears.length,2);assert.equal(clears[0].request_id,clears[1].request_id);assert.equal(clears[1].scope.voice_key,'a'.repeat(64));assert.equal(clears[1].scope.tenant_id,14);assert.equal(clears[1].mode,'history');checks.push('失败后同操作编号幂等重试；固定租户/方案/完整音色')
 await selects.nth(4).selectOption('local_only');await panel.getByRole('button',{name:'预览清理范围',exact:true}).click();await panel.locator('.preview').waitFor();await panel.getByRole('button',{name:'清理本地副本',exact:true}).click();await panel.getByRole('button',{name:'确认清除',exact:true}).click();await panel.locator('.notice').waitFor();assert.equal(clears.at(-1).mode,'local_only');assert.match(await panel.locator('.notice').innerText(),/OSS归档仍可恢复/);checks.push('仅本地副本操作语义与请求独立')
 await panel.getByRole('button',{name:'生成自动回收清单',exact:true}).click();await page.waitForFunction(()=>document.querySelector('.voice-cleanup')?.textContent.includes('闲置候选 2'));assert.match(await panel.innerText(),/不自动删除/);checks.push('自动策略盘点显示保护、未知与候选，不执行删除')
 await selects.nth(0).selectOption('15');await selects.nth(0).selectOption('14');await selects.nth(1).selectOption('4');await page.waitForTimeout(250);assert.equal(await selects.nth(1).locator('option').last().textContent(),'许牛府方案');checks.push('迟到方案响应不覆盖已切回的用户范围')
 failPreview=true;await panel.getByRole('button',{name:'预览清理范围',exact:true}).click();await panel.locator('.error').waitFor();assert.equal(await panel.locator('.preview').count(),0);checks.push('盘点失败清除过期预览，禁止凭旧结果确认')
 await page.setViewportSize({width:390,height:844});assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1));await page.screenshot({path:out+'/mobile.png',fullPage:true});checks.push('390px窄屏无横向溢出')
 assert.deepEqual(errors,[]);assert.deepEqual(external,[])
 await writeFile(out+'/validation.json',JSON.stringify({passed:true,checks,browser_errors:errors,external_requests:external,real_supplier_calls:0,clear_requests:clears.length},null,2));console.log(JSON.stringify({passed:true,checks:checks.length,output:out}))
}finally{await browser.close();await server.close()}
