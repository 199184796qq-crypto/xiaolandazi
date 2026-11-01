import assert from 'node:assert/strict'
import {createRequire} from 'node:module'
import {mkdir, writeFile} from 'node:fs/promises'
import {startFixture} from './room-parity-fixture.mjs'
const {chromium}=createRequire(new URL('../../collector-worker/package.json',import.meta.url))('playwright-core')
const output=process.env.REAL_SUBTITLES_OUTPUT||'E:/直播伴播-local/real-subtitles-only-20261101/browser'
await mkdir(output,{recursive:true})
const server=await startFixture(),browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
const checks=[]
try {
 for(const width of [1680,390]) {
  const page=await browser.newPage({viewport:{width,height:1000}}),errors=[]
  page.on('pageerror',e=>errors.push(e.message))
  await page.goto('http://127.0.0.1:18601/__room-parity.html?role=customer&waiting=0&caption=1')
  await page.getByText('正在回答保存问题哟',{exact:true}).waitFor()
  await page.evaluate(()=>{
   window.fixture.audioEngine={room_id:15,phase:'mainline',active_source:'mainline',current_segment_id:'reused',speech_feed:{}}
   window.fixture.speechRuntime={room_id:15,mainline:{status:'playing',text:'不应显示的整段原文'},interrupt:{status:'playing',text:'不应显示的互动原文'},program:{running:true,track_text:'不应显示的整段原文',current_ms:1000,task:{duration_ms:10000},current_segment:{segment_id:'old',text:'不应显示的旧字幕'},timeline:[{segment_id:'old',start_ms:0,end_ms:10000,text:'不应显示的旧字幕'}]}}
  })
  await page.locator('.speech-mainline-caption-row').first().waitFor({state:'hidden'})
  assert.equal(await page.locator('.speech-runtime-unified').getByText(/不应显示/).count(),0)
  assert.equal(await page.locator('.speech-runtime-unified').getByText(/等待主播文案/).count(),0)
  await page.screenshot({path:output+`/untimed-${width}.png`})
  checks.push(`${width}px: full source text/old timeline never become live captions`)
  await page.evaluate(()=>window.fixture.audioEngine={room_id:15,phase:'mainline',speech_feed:{current:{segment_id:'real',text:'恢复真实时间轴字幕',tone:'normal'}}})
  await page.getByText('恢复真实时间轴字幕',{exact:true}).waitFor()
  const font=await page.locator('.speech-mainline-caption-row span').evaluate(el=>parseFloat(getComputedStyle(el).fontSize))
  assert.ok(font>=16)
  await page.evaluate(()=>window.fixture.audioEngine={room_id:15,phase:'interrupt',speech_feed:{previous:{segment_id:'real',text:'恢复真实时间轴字幕'},next:{segment_id:'next',text:'下一段准备的字幕'}}})
  await page.locator('.speech-mainline-caption-row').first().waitFor({state:'hidden'})
  assert.equal(await page.locator('.speech-runtime-unified').getByText(/恢复真实时间轴字幕|下一段准备的字幕/).count(),0)
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
  assert.deepEqual(errors,[])
  checks.push(`${width}px: real captions recover, missing current cue clears all slots, font >=16px, no overflow/errors`)
  await page.close()
 }
 await writeFile(output+'/result.json',JSON.stringify({passed:true,checks},null,2))
 console.log(JSON.stringify({passed:true,checks}))
} finally { await browser.close(); await server.close() }
