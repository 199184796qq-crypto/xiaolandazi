import assert from 'node:assert/strict'
import {createRequire} from 'node:module'
import {startFixture} from './room-parity-fixture.mjs'
const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url)),{chromium}=require('playwright-core')
const server=await startFixture(),browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try {
 const page=await browser.newPage({viewport:{width:1680,height:1000}}),errors=[]
 page.on('pageerror',e=>errors.push(e.message))
 await page.goto('http://127.0.0.1:18601/__room-parity.html?role=customer&waiting=1&caption=1&pcm=1')
 await page.waitForFunction(()=>!!window.fixture.pcmController&&window.fixture.audioContext?.state==='running')
 await page.evaluate(()=>{const pcm=new Uint8Array(960);new DataView(pcm.buffer).setInt16(0,777,true);window.fixture.pcmController.enqueue(pcm)})
 await page.waitForFunction(()=>window.fixture.sources.length===1)
 const connections=await page.evaluate(()=>window.fixture.calls.filter(c=>c.path.endsWith('/composite.pcm')).length)
 await page.evaluate(()=>{
  window.fixture.delayUnlock=true;window.fixture.audioContext.state='suspended'
  window.fixture.audioEngine={room_id:15,phase:'interrupt',speech_feed:{current:{segment_id:'welcome',text:'欢迎小善',tone:'interrupt'}}}
  const pcm=new Uint8Array(960*2),view=new DataView(pcm.buffer);view.setInt16(0,5000,true);view.setInt16(960,9000,true);window.fixture.pcmController.enqueue(pcm)
 })
 await page.waitForFunction(()=>typeof window.fixture.releaseUnlock==='function')
 await page.waitForTimeout(4500)
 assert.equal(await page.evaluate(()=>window.fixture.sources.length),1,'suspended context consumed/dropped opening PCM')
 assert.equal(await page.evaluate(()=>window.fixture.calls.filter(c=>c.path.endsWith('/composite.pcm')).length),connections,'health polling discarded pending opening PCM while waiting for audio device')
 await page.evaluate(()=>window.fixture.releaseUnlock())
 await page.waitForFunction(()=>window.fixture.sources.length===3)
 assert.deepEqual(await page.evaluate(()=>window.fixture.sources.map(s=>s.sample)),[777/32768,5000/32768,9000/32768],'opening PCM was lost or reordered on resume')
 assert.deepEqual(errors,[])
 assert.equal(await page.evaluate(()=>window.fixture.calls.filter(c=>c.path.startsWith('/api/v1/')&&c.method!=='GET').length),0)
 console.log('PASS: webpage preserves first two voice frames during suspended AudioContext and resumes in order; health polling keeps pending stream; no live mutation')
} finally {await browser.close();await server.close()}
