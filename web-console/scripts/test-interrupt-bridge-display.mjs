// Actual customer view follows Core's unified output timeline; no live mutation.
import assert from 'node:assert/strict'
import {createRequire} from 'node:module'
import {startFixture} from './room-parity-fixture.mjs'
const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url)),{chromium}=require('playwright-core')
const server=await startFixture(),browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try {
 const page=await browser.newPage({viewport:{width:1680,height:1000}}),errors=[]
 page.on('pageerror',e=>errors.push(e.message))
 await page.goto('http://127.0.0.1:18601/__room-parity.html?role=customer&caption=1')
 await page.locator('.speech-mainline-caption-row.role-current').first().waitFor()
 const body='星夜来了，也欢迎刚来的朋友们。',bridge='口味和吃法也一并说清楚。',resume='接着看看鸭子的保存方法。'
 async function feed(previous,current,next) {
  await page.evaluate(({previous,current,next})=>{window.fixture.audioEngine={room_id:15,phase:'interrupt_playing',speech_feed:{previous,current,next}}},{previous,current,next})
 }
 const row=(id,text,tone)=>({segment_id:id,text,tone})
 // Only the actual Core speech feed drives caption visibility, never a staged bridge.
 await page.evaluate(({body,bridge})=>{window.fixture.speechRuntime={room_id:15,interrupt:{status:'playing',reply_text:body,bridge_text:bridge,bridge_used:false}}},{body,bridge})
 await feed(row('cut','上一句完整主线','cut'),row('core',body,'interrupt'),row('resume',resume,'resume'))
 await page.waitForFunction(body=>document.querySelector('.speech-mainline-caption-row.role-current')?.textContent.includes(body),body)
 assert.equal((await page.locator('.speech-runtime-unified').innerText()).includes(bridge),false)
 await feed(row('core',body,'interrupt'),row('bridge',bridge,'interrupt'),row('resume',resume,'resume'))
 await page.waitForFunction(bridge=>document.querySelector('.speech-mainline-caption-row.role-current')?.textContent.includes(bridge),bridge)
 assert.equal((await page.locator('.speech-runtime-unified').innerText()).split(bridge).length-1,1)
 await feed(row('bridge',bridge,'interrupt'),row('resume',resume,'resume'),null)
 await page.waitForFunction(resume=>document.querySelector('.speech-mainline-caption-row.role-current')?.textContent.includes(resume),resume)
 assert.equal((await page.locator('.speech-mainline-caption-row.role-previous').first().innerText()).includes(bridge),true)
 await page.evaluate(()=>{
  window.fixture.review={summary:{active_seconds:60,entries:2,chats:1,likes:0,follows:0,order_signals:0,question_groups:[]},events:[],audit_review:{runtime_session_id:1,started_at:new Date().toISOString(),items:[{id:1,kind:'generation_skip',stage:'voice',decision:'skipped',task_key:'规格',text:'原段话术',reason:'本段TTS失败，继续下一段',created_at:new Date().toISOString()}]}}
 })
 await page.getByRole('button',{name:'查看复盘',exact:true}).click()
 await page.locator('.production-review-notes').waitFor()
 assert.equal((await page.locator('.production-review-notes').innerText()).includes('本段TTS失败，继续下一段'),true)
 assert.deepEqual(errors,[])
 assert.equal(await page.evaluate(()=>window.fixture.calls.filter(c=>c.method!=='GET' && c.path.startsWith('/api/v1/')).length),0)
 console.log('PASS: unified caption shows actual answer -> bridge -> mainline, no staged bridge/duplicate/mutation')
} finally {await browser.close();await server.close()}
