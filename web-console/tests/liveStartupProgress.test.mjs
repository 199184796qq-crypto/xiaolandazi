import test from 'node:test'
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import ts from 'typescript'
const src=await readFile(new URL('../src/liveStartupProgress.ts',import.meta.url),'utf8')
const module={exports:{}}
new Function('module','exports',ts.transpile(src,{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}))(module,module.exports)
const {advanceStartupStage,startupMilestones}=module.exports
test('startup advances with actual same-session milestones, never rolls back or exposes internal names',()=>{
 const snapshot=(stage,percent= startupMilestones[stage]?.percent,session_id=8)=>({agent_state:'working',session:{id:8},startup:{session_id,stage,percent}})
 assert.equal(advanceStartupStage('reading_plan',snapshot('generating_voice')),'generating_voice')
 assert.equal(advanceStartupStage('generating_voice',snapshot('reading_plan')),'generating_voice')
 assert.equal(advanceStartupStage('reading_plan',snapshot('generating_voice',65,7)),'reading_plan')
 assert.equal(advanceStartupStage('reading_plan',snapshot('secret_provider_internal',90)),'reading_plan')
 assert.equal(advanceStartupStage('reading_plan',snapshot('voice_ready',100)),'reading_plan')
 assert.equal(advanceStartupStage('voice_ready',{...snapshot('waiting_playback'),agent_state:'stopped'}),'voice_ready')
 assert.ok(startupMilestones.waiting_playback.percent<100)
})
