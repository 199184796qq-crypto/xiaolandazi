import assert from 'node:assert/strict'
import {createRequire} from 'node:module'
import {mkdir,writeFile} from 'node:fs/promises'
import {startFixture} from './room-parity-fixture.mjs'
const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url)),{chromium}=require('playwright-core')
const output=process.env.ROOM_FIRST_VOICE_TEST_OUTPUT||'E:/直播伴播/work/room-first-voice'
await mkdir(output,{recursive:true})
const server=await startFixture(),browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try{
  const page=await browser.newPage({viewport:{width:1680,height:1000}}),errors=[]
  page.on('pageerror',e=>errors.push(e.message))
  await page.goto('http://127.0.0.1:18601/__room-parity.html?role=customer&waiting=1&caption=1&pcm=1')
  await page.locator('.speech-runtime-initializing').waitFor()
  await page.waitForFunction(()=>!!window.fixture.pcmController)
  assert.equal(await page.getByText('正在回答保存问题哟',{exact:true}).count(),0,'prepared captions must not end first-voice loading')
  assert.match(await page.locator('.startup-wave-flow').first().evaluate(e=>getComputedStyle(e).animationName),/^startup-wave-layer-scan(?:-|$)/)
  await page.evaluate(()=>window.fixture.pcmController.enqueue(new Uint8Array(960)))
  await page.waitForFunction(()=>window.fixture.sources.length>0)
  assert.equal(await page.locator('.speech-runtime-initializing').count(),1,'silent idle PCM must not end loading')
  await page.screenshot({path:output+'/preparing.png',fullPage:true})
  await page.evaluate(()=>{const pcm=new Uint8Array(960);new DataView(pcm.buffer).setInt16(0,2000,true);window.fixture.pcmController.enqueue(pcm)})
  await page.getByText('正在回答保存问题哟',{exact:true}).waitFor()
  assert.equal(await page.locator('.speech-runtime-initializing').count(),0,'played sound must end loading')
  await page.screenshot({path:output+'/playing.png',fullPage:true})
  await page.evaluate(()=>{window.fixture.session=2})
  await page.locator('.speech-runtime-initializing').waitFor()
  assert.equal(await page.getByText('正在回答保存问题哟',{exact:true}).count(),0,'new session must not borrow prior audio readiness')
  await page.evaluate(()=>{window.fixture.waiting=false})
  await page.locator('.speech-runtime-initializing').waitFor({state:'hidden'})
  assert.deepEqual(errors,[])
  await writeFile(output+'/result.json',JSON.stringify({passed:true,checks:['caption arrives before sound','loading animation active','silent PCM keeps loading','first played sound shows captions','new session resets readiness','stop clears loading'],errors},null,2))
  console.log('PASS first voice loading: captions and silent PCM do not hide animation, played audio does, new session resets, stopped session does not spin.')
}finally{await browser.close();await server.close()}
