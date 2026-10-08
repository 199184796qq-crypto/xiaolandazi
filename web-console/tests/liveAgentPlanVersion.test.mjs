import test from 'node:test'
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import ts from 'typescript'
const source=await readFile(new URL('../src/liveAgentPlanVersion.ts',import.meta.url),'utf8')
const js=ts.transpile(source,{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.ESNext})
const {normalizeLiveAgentPlanVersion:normalize}=await import('data:text/javascript;charset=utf-8,'+encodeURIComponent(js))
test('legacy null variants restore as an empty list without editing the original saved version',()=>{
 const original={id:37,variants:null,voice_identity:{profile_id:8},generation_context:{content_mode:'ai_dynamic'}}
 const restored=normalize(original)
 assert.deepEqual(restored.variants,[]);assert.equal(original.variants,null)
 assert.equal(restored.voice_identity,original.voice_identity);assert.equal(restored.generation_context,original.generation_context)
 assert.equal(restored.variants.length,0)
})
test('real manuscripts and voice assets are preserved; malformed data fails explicitly',()=>{
 const original={variants:[{variant_key:'A',text:'真实输入',audio_asset_id:123}]}
 assert.equal(normalize(original),original)
 assert.throws(()=>normalize({variants:'broken'}),/文稿列表格式异常/)
 assert.throws(()=>normalize({variants:[null]}),/文稿列表格式异常/)
})
