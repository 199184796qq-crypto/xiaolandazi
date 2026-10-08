import test from 'node:test'
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
const view=await readFile(new URL('../src/views/LiveStrategyView.vue',import.meta.url),'utf8')
test('initialization reads real server milestones instead of ticking to a fake 92 percent',()=>{
 const start=view.indexOf('function startAnchorInitializationProgress()'),end=view.indexOf('async function refreshSpeechMinerWorkspace()',start),code=view.slice(start,end)
 assert.doesNotMatch(code,/setInterval|Math\.min\(92/)
 assert.match(code,/Accept: 'text\/event-stream'/)
 assert.match(code,/anchorInitializationStage\.value = event\.message/)
 assert.match(code,/Math\.max\(anchorInitializationProgress\.value, event\.percent/)
})
