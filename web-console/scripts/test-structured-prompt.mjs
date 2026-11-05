import assert from 'node:assert/strict'
import test from 'node:test'
import { appendPromptField, appendPromptFieldsFromText, appendPromptNode, appendPromptNodesFromText, parseStructuredPrompt, promptNameError, removePromptNode, replacePromptValue, serializePromptMetadata, serializeStructuredPrompt } from '../src/structuredPrompt.ts'
import { compilePlanPrompts, compilePlanRewritePrompt, emptyPlanPromptSections, readPlanPromptSections } from '../src/planPrompt.ts'

test('Lossless parsing preserves headings, colon styles, ordering, whitespace, Unicode and all newline forms', () => {
  for (const text of ['', '  ', '\n\n', '普通正文😀\n第二行\n', '\ufeff正文\r\n', '【身份】\n主播名： 小林\n身份:店主\n\n【称呼】\n默认称呼：朋友\n', '# 主播人设\r\n## 身份\r\n- **主播名**：小林\r\n\r\n语速:中等\r\n', '【名称】\r键：值\r', '【空节点】', '## 标题 ##\n未带键的正文\n', '【重复】\n同名：一\n同名：二\n【重复】\n正文']) {
    assert.equal(serializeStructuredPrompt(parseStructuredPrompt(text)), text)
  }
})

test('Existing node names and key spellings stay unchanged when changing values or clearing them', () => {
  const text='【身份】\r\n- **主播名**: 小林\r\n角色：自己人\r\n\r\n【称呼】\r\n默认称呼：各位'
  const doc=parseStructuredPrompt(text), field=doc.nodes[0].fields[0]
  const changed=replacePromptValue(doc,field.id,'阿林😀')
  assert.equal(serializeStructuredPrompt(changed),text.replace('小林','阿林😀'))
  assert.deepEqual(changed.nodes.map(n=>[n.name,...n.fields.map(f=>f.key)]),doc.nodes.map(n=>[n.name,...n.fields.map(f=>f.key)]))
  assert.equal(serializeStructuredPrompt(replacePromptValue(doc,field.id,'')),text.replace('小林',''))
  assert.equal(serializeStructuredPrompt(doc),text)
})

test('Multiline pasted structural text stays inside its value after a round trip', () => {
  const doc=parseStructuredPrompt('【身份】\n姓名：小林\n角色：店主\n')
  const value='第一行\n【不能变成新节点】\n角色：只是引用\n最后一行'
  const changed=replacePromptValue(doc,doc.nodes[0].fields[0].id,value)
  const parsed=parseStructuredPrompt(serializeStructuredPrompt(changed))
  assert.equal(parsed.nodes.length,1);assert.equal(parsed.nodes[0].fields.length,2)
  assert.equal(parsed.nodes[0].fields[0].value,value)
  assert.equal(parsed.nodes[0].fields[1].key,'角色')
})

test('Free paragraphs, URLs and unknown forms are retained as editable text', () => {
  const raw='https://example.test/a:b\n12:30 是时间\n这是一个较长的说明，有冒号：仍保留。\n【身份】\n正文句子。'
  const doc=parseStructuredPrompt(raw)
  assert.equal(doc.nodes[0].fields.length,1);assert.equal(doc.nodes[0].fields[0].key,null)
  assert.equal(serializeStructuredPrompt(doc),raw)
  const changed=replacePromptValue(parseStructuredPrompt('原始正文'), 'f1', '【仅为引用】\n姓名：引用')
  const reread=parseStructuredPrompt(serializeStructuredPrompt(changed))
  assert.equal(reread.nodes[0].heading,'');assert.equal(reread.nodes[0].fields[0].value,'【仅为引用】\n姓名：引用')
})

test('Adding a node and key preserves previous source and accepts empty values', () => {
  const text='【身份】\r\n姓名：小林'
  let doc=appendPromptNode(parseStructuredPrompt(text),'表达习惯')
  const node=doc.nodes.at(-1)
  doc=appendPromptField(doc,node.id,'语速','中等\n略快')
  assert.equal(serializeStructuredPrompt(doc),text+'\r\n【表达习惯】\r\n语速：中等\r\n    略快\r\n')
  const reread=parseStructuredPrompt(serializeStructuredPrompt(doc))
  assert.equal(reread.nodes.at(-1).fields[0].value,'中等\n略快')
  doc=appendPromptField(doc,doc.nodes[0].id,'语速','')
  assert.equal(doc.nodes[0].fields.at(-1).value,'')
  assert.throws(()=>appendPromptField(doc,doc.nodes[0].id,'语速','重复'))
})

test('Names are checked for empty, separators, duplicates and Unicode-normalized duplicates', () => {
  for (const name of ['', ' ', '甲\n乙','【身份】','<script>','a'.repeat(41)]) assert.ok(promptNameError(name,'node',[]),name)
  for (const name of ['姓名：','语，速','12','https','- 隐藏前缀','+ 隐藏前缀']) assert.ok(promptNameError(name,'key',[]),name)
  assert.ok(promptNameError(' Ａ ','node',['A']))
  assert.equal(promptNameError('语速','key',[]),'')
  assert.equal(promptNameError('说话方式','node',[]),'')
})

test('Duplicate model labels are retained, array identities stay unique, unknown IDs do not rename anything', () => {
  let doc=parseStructuredPrompt('【身份】\n姓名：甲\n姓名：乙\n【身份】\n姓名：丙')
  const original=serializeStructuredPrompt(doc)
  assert.equal(serializeStructuredPrompt(replacePromptValue(doc,'missing','不能改')),original)
  assert.throws(()=>appendPromptNode(doc,'身份'))
  assert.throws(()=>appendPromptField(doc,'missing','键','值'))
  doc=appendPromptNode(doc,'新增')
  doc=appendPromptField(doc,doc.nodes.at(-1).id,'__proto__','文本')
  const ids=doc.nodes.flatMap(n=>[n.id,...n.fields.map(f=>f.id)])
  assert.equal(new Set(ids).size,ids.length)
  assert.equal(doc.nodes.at(-1).fields.at(-1).key,'__proto__')
})

test('An empty draft can add its first explicit node without inventing an empty preamble', () => {
  const doc=appendPromptNode(parseStructuredPrompt(''),'身份')
  assert.equal(doc.nodes.length,1)
  assert.equal(serializeStructuredPrompt(doc),'【身份】\n')
})

test('Inline additions accept both colons, multiple pairs, empty values and colons inside values', () => {
  const doc=parseStructuredPrompt('【身份】\r\n姓名：甲\r\n'),node=doc.nodes[0]
  const added=appendPromptFieldsFromText(doc,node.id,'语速：中等偏快😀\r\n停顿:句尾：自然停顿\r\n说明：\r\n')
  assert.deepEqual(added.nodes[0].fields.slice(1).map(f=>[f.key,f.value]),[['语速','中等偏快😀'],['停顿','句尾：自然停顿'],['说明','']])
  assert.equal(serializeStructuredPrompt(added),'【身份】\r\n姓名：甲\r\n语速：中等偏快😀\r\n停顿：句尾：自然停顿\r\n说明：\r\n')
  assert.equal(serializeStructuredPrompt(doc),'【身份】\r\n姓名：甲\r\n')
  assert.equal(appendPromptFieldsFromText(doc,node.id,' \n \t'),doc)
})

test('Inline continuations stay within their value; structural syntax is not promoted to labels', () => {
  const doc=parseStructuredPrompt('【身份】\n姓名：甲\n')
  const added=appendPromptFieldsFromText(doc,doc.nodes[0].id,'说明：第一句\n    第二句\n    【引用】\n\t键：也是值\n语速：中等')
  const reread=parseStructuredPrompt(serializeStructuredPrompt(added))
  assert.equal(reread.nodes.length,1)
  assert.deepEqual(reread.nodes[0].fields.map(f=>f.key),['姓名','说明','语速'])
  assert.equal(reread.nodes[0].fields[1].value,'第一句\n第二句\n【引用】\n键：也是值')
})

test('Malformed or duplicate inline additions are atomic and do not lose previous contents', () => {
  const doc=parseStructuredPrompt('【身份】\n姓名：甲\n'),raw=serializeStructuredPrompt(doc)
  for(const text of ['语速：中等\n缺少冒号','新键：值\n姓名：乙','新键：甲\n 新键：乙','Ａ：甲\na：乙','：空键','语，速：中等','- 隐藏前缀：值','    缺少首行','【新节点】\n键：值','a'.repeat(41)+'：值']){
    assert.throws(()=>appendPromptFieldsFromText(doc,doc.nodes[0].id,text),text)
    assert.equal(serializeStructuredPrompt(doc),raw)
  }
  assert.throws(()=>appendPromptFieldsFromText(doc,'missing','新键：值'),/当前节点已变化/)
})

test('Inline nodes accept bare names or headed blocks, validate atomically, and mark only additions as user-owned', () => {
  const doc=parseStructuredPrompt('【身份】\n姓名：甲\n')
  const added=appendPromptNodesFromText(doc,'表达习惯\n语速：中等\n停顿：自然\n【补充】\n备注:可留空')
  assert.deepEqual(added.nodes.map(n=>[n.name,n.origin]),[['身份','model'],['表达习惯','user'],['补充','user']])
  assert.equal(added.nodes[1].fields[0].value,'中等')
  assert.equal(appendPromptNodesFromText(doc,' \n '),doc)
  for(const draft of ['身份','【新建】\n缺少冒号','【甲】\n值：一\n【甲】\n值：二','a'.repeat(41),'【半个标题','【甲】\n键：一\n键：二']){
    assert.throws(()=>appendPromptNodesFromText(doc,draft),draft)
    assert.equal(serializeStructuredPrompt(doc),'【身份】\n姓名：甲\n')
  }
})

test('Only custom nodes may be removed, including all of their fields, without modifying model nodes', () => {
  const doc=parseStructuredPrompt('【身份】\n姓名：甲'),added=appendPromptNodesFromText(doc,'习惯\n语速：中等')
  assert.throws(()=>removePromptNode(added,doc.nodes[0].id),/只能删除自己添加/)
  assert.throws(()=>removePromptNode(added,'missing'),/只能删除自己添加/)
  const removed=removePromptNode(added,added.nodes.at(-1).id)
  assert.equal(serializeStructuredPrompt(removed),serializeStructuredPrompt(doc))
  assert.equal(serializePromptMetadata(removed),'')
  assert.equal(removePromptNode(appendPromptNode(parseStructuredPrompt(''),'唯一'), 'n1').nodes.length,0)
})

test('Custom origin survives save/readback and value edits through separate metadata, with no prompt markers', () => {
  let doc=appendPromptNodesFromText(parseStructuredPrompt('【身份】\n姓名：甲\n'),'表达习惯\n语速：中等')
  doc=replacePromptValue(doc,doc.nodes.at(-1).fields[0].id,'快一点😀')
  const text=serializeStructuredPrompt(doc),metadata=serializePromptMetadata(doc)
  assert.equal(JSON.parse(metadata).source,text)
  assert.ok(!text.includes('user_nodes'));assert.ok(!text.includes('persona_editor_json'))
  const reread=parseStructuredPrompt(text,metadata)
  assert.deepEqual(reread.nodes.map(n=>n.origin),['model','user'])
  assert.doesNotThrow(()=>removePromptNode(reread,reread.nodes[1].id))
  for(const meta of ['', 'broken', 'null', '{}', JSON.stringify({...JSON.parse(metadata),version:2}), JSON.stringify({...JSON.parse(metadata),source:'别的正文'}), JSON.stringify({...JSON.parse(metadata),user_nodes:[{index:88,name:'表达习惯'}]}), JSON.stringify({...JSON.parse(metadata),user_nodes:[{index:1,name:'错名'}]}), JSON.stringify({...JSON.parse(metadata),user_nodes:[{index:1,name:'表达习惯'},{index:1,name:'表达习惯'}]})]){
    assert.ok(parseStructuredPrompt(text,meta).nodes.every(n=>n.origin==='model'),meta)
  }
})

test('Plan originals retain provenance while all three model prompt compilations exclude it', () => {
  const baseline={...emptyPlanPromptSections(),persona_md:'【身份】\n姓名：甲'}
  const metadata='{"version":1,"source":"test","user_nodes":[]}'
  const sections=readPlanPromptSections({...baseline,persona_editor_json:metadata})
  assert.equal(sections.persona_editor_json,metadata)
  assert.deepEqual(compilePlanPrompts(sections),compilePlanPrompts(baseline))
  assert.equal(compilePlanRewritePrompt(sections,'规则'),compilePlanRewritePrompt(baseline,'规则'))
  assert.equal(readPlanPromptSections(baseline).persona_editor_json,undefined)
})
