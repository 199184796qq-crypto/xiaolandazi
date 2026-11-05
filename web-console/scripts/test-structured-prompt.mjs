import assert from 'node:assert/strict'
import test from 'node:test'
import { appendPromptField, appendPromptFieldsFromText, appendPromptNode, appendPromptNodesFromText, parseStructuredPrompt, promptNameError, removePromptField, removePromptNode, replacePromptValue, serializePromptMetadata, serializeStructuredPrompt } from '../src/structuredPrompt.ts'
import { compilePlanPrompts, compilePlanRewritePrompt, emptyPlanPromptSections, readPlanPromptSections } from '../src/planPrompt.ts'

test('Numbered lists including colon sentences stay whole keyless content without altering source', () => {
  for (const newline of ['\n','\r\n','\r']) {
    const body=['1. 清晰讲解产品卖点、使用方法；','2. 承接公屏互动，引导停留；','3. 自然引导下单；','4. 维持直播间氛围稳定；','5. 每一轮讲解循环：痛点引入→展示产品→卖点拆解。'].join(newline)
    const raw='【直播目标】'+newline+body+newline
    const doc=parseStructuredPrompt(raw)
    assert.equal(doc.nodes[0].fields.length,1)
    assert.equal(doc.nodes[0].fields[0].key,null)
    assert.equal(doc.nodes[0].fields[0].value,body.replace(/\r\n|\r/g,'\n'))
    assert.equal(serializeStructuredPrompt(doc),raw)
    assert.equal(serializeStructuredPrompt(removePromptField(doc,doc.nodes[0].fields[0].id)),'【直播目标】'+newline)
  }
})

test('List markers are not keys; standard KV and indented list continuations retain their meanings', () => {
  for (const marker of ['1.','2．','3、','4)','5）','(6)','（7）','一、','十）','**8.']) {
    const raw='【规则】\n语速：正常\n'+marker+' 注意：不要夸大。\n称呼：各位\n'
    const doc=parseStructuredPrompt(raw)
    assert.deepEqual(doc.nodes[0].fields.map(f=>f.key),['语速',null,'称呼'])
    assert.equal(doc.nodes[0].fields[1].value,marker+' 注意：不要夸大。')
    assert.equal(serializeStructuredPrompt(doc),raw)
  }
  const doc=parseStructuredPrompt('【规则】\n说明：第一句\n    1. 注意：这是说明内容\n1.5米规格：有货\n- **主播名**：小林\n')
  assert.deepEqual(doc.nodes[0].fields.map(f=>f.key),['说明','1.5米规格','主播名'])
  assert.ok(doc.nodes[0].fields[0].value.includes('1. 注意：这是说明内容'))
})

test('Editing and re-reading a keyless list preserves its numbered text and does not promote its colons to keys', () => {
  const doc=parseStructuredPrompt('【目标】\n1. 第一条：原文\n2. 第二条：原文\n')
  const value='1. 第一条：修改😀\n2. 第二条：原文\n3. 补充：内容'
  const changed=replacePromptValue(doc,doc.nodes[0].fields[0].id,value)
  const reread=parseStructuredPrompt(serializeStructuredPrompt(changed),serializePromptMetadata(changed))
  assert.equal(reread.nodes[0].fields.length,1)
  assert.equal(reread.nodes[0].fields[0].key,null)
  assert.equal(reread.nodes[0].fields[0].value,value)
})

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

test('Nodes from either origin may be removed with their own fields, without modifying other nodes', () => {
  const doc=parseStructuredPrompt('【身份】\n姓名：甲'),added=appendPromptNodesFromText(doc,'习惯\n语速：中等')
  assert.equal(serializeStructuredPrompt(removePromptNode(added,doc.nodes[0].id)),'\n【习惯】\n语速：中等\n')
  assert.throws(()=>removePromptNode(added,'missing'),/当前节点已变化/)
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

test('All fields delete key and entire multiline value together without deleting other fields', () => {
  for (const newline of ['\n','\r\n','\r']) {
    const raw='【身份】'+newline+'姓名：甲'
    const base=parseStructuredPrompt(raw)
    let doc=appendPromptField(base,base.nodes[0].id,'备注','第一句\n【引用节点】\n键：仍为内容')
    assert.equal(doc.nodes[0].fields[1].origin,'user')
    assert.equal(removePromptField(doc,base.nodes[0].fields[0].id).nodes[0].fields.length,1)
    assert.throws(()=>removePromptField(doc,'missing'),/当前键值或正文已变化/)
    doc=removePromptField(doc,doc.nodes[0].fields[1].id)
    assert.equal(serializeStructuredPrompt(doc),raw)
    assert.equal(serializePromptMetadata(doc),'')
  }
})

test('Field provenance survives edit/save/refresh and updates positions after deleting other nodes or keys', () => {
  let doc=appendPromptNodesFromText(parseStructuredPrompt('【模型】\n键：原值\n'),'自建一\n键：一\n【自建二】\n键：二')
  doc=appendPromptFieldsFromText(doc,doc.nodes[0].id,'自建键：\n第二键：值')
  doc=replacePromptValue(doc,doc.nodes[0].fields.at(-1).id,'修改后的值😀')
  doc=removePromptNode(doc,doc.nodes[1].id)
  doc=parseStructuredPrompt(serializeStructuredPrompt(doc),serializePromptMetadata(doc))
  assert.equal(doc.nodes[0].origin,'model')
  assert.deepEqual(doc.nodes[0].fields.map(f=>f.origin),['model','user','user'])
  doc=removePromptField(doc,doc.nodes[0].fields[1].id)
  doc=parseStructuredPrompt(serializeStructuredPrompt(doc),serializePromptMetadata(doc))
  assert.deepEqual(doc.nodes.map(n=>n.fields.map(f=>[f.key,f.origin])),[[['键','model'],['第二键','user']],[['键','user']]])
  assert.equal(doc.nodes[0].fields[1].value,'修改后的值😀')
  assert.doesNotThrow(()=>removePromptField(doc,doc.nodes[0].fields[0].id))
  doc=removePromptField(doc,doc.nodes[1].fields[0].id)
  assert.equal(doc.nodes[1].fields.length,0)
  assert.equal(doc.nodes[1].origin,'user')
  assert.equal(parseStructuredPrompt(serializeStructuredPrompt(doc),serializePromptMetadata(doc)).nodes[1].origin,'user')
})

test('Old node-only metadata retains custom-node keys but never guesses field ownership in a model node', () => {
  const source='【模型】\n模型键：值\n旧新增键：未知\n【自建】\n自建键：值\n'
  const metadata=JSON.stringify({version:1,source,user_nodes:[{index:1,name:'自建'}]})
  const doc=parseStructuredPrompt(source,metadata)
  assert.deepEqual(doc.nodes.map(n=>n.fields.map(f=>f.origin)),[['model','model'],['user']])
  assert.doesNotThrow(()=>removePromptField(doc,doc.nodes[0].fields[1].id))
  assert.doesNotThrow(()=>removePromptField(doc,doc.nodes[1].fields[0].id))
})

test('Malformed, stale, mismatched and duplicate field metadata locks all origins atomically', () => {
  const doc=appendPromptNodesFromText(parseStructuredPrompt('【模型】\n键：原值\n'),'自建\n键：新值')
  const source=serializeStructuredPrompt(doc),base=JSON.parse(serializePromptMetadata(doc))
  const entry={node_index:1,index:0,key:'键'}
  for(const user_fields of [null,{},[null],[{...entry,node_index:-1}],[{...entry,index:9}],[{...entry,index:0.5}],[{...entry,key:'错名'}],[entry,entry]]){
    const reread=parseStructuredPrompt(source,JSON.stringify({...base,user_fields}))
    assert.ok(reread.nodes.every(n=>n.origin==='model'&&n.fields.every(f=>f.origin==='model')))
  }
  const stale=parseStructuredPrompt(source+'新正文',JSON.stringify(base))
  assert.ok(stale.nodes.every(n=>n.origin==='model'&&n.fields.every(f=>f.origin==='model')))
})

test('Duplicate model labels and ungrouped text use field positions without enabling deletion of the wrong key', () => {
  let doc=parseStructuredPrompt('前言\n同名：一\n同名：二\n【模型】\n同名：三\n')
  doc=appendPromptField(doc,doc.nodes[0].id,'新键','前言里的新值')
  doc=appendPromptField(doc,doc.nodes[1].id,'新键','模型里的新值')
  doc=parseStructuredPrompt(serializeStructuredPrompt(doc),serializePromptMetadata(doc))
  assert.deepEqual(doc.nodes[0].fields.map(f=>f.origin),['model','model','model','user'])
  doc=removePromptField(doc,doc.nodes[0].fields[3].id)
  assert.equal(doc.nodes[1].fields[1].origin,'user')
  assert.equal(doc.nodes[1].fields[1].value,'模型里的新值')
  assert.doesNotThrow(()=>removePromptField(doc,doc.nodes[0].fields[0].id))
})

test('Duplicate model names use exact node and field identities when deleting', () => {
  let doc=parseStructuredPrompt('【同名】\r\n键：一\r\n键：二\r\n【同名】\r\n键：三\r\n')
  doc=removePromptField(doc,doc.nodes[0].fields[1].id)
  assert.equal(serializeStructuredPrompt(doc),'【同名】\r\n键：一\r\n【同名】\r\n键：三\r\n')
  doc=removePromptNode(doc,doc.nodes[0].id)
  assert.equal(serializeStructuredPrompt(doc),'【同名】\r\n键：三\r\n')
})

test('Untagged, stale-provenance and ordinary body contents may all be removed', () => {
  const raw='无键正文\n第二句\n【模型节点】\n键：多行\n    【引用不是节点】\n    键：仍是内容\n'
  for(const metadata of ['', 'broken', '{"version":1,"source":"旧原文","user_nodes":[]}']){
    let doc=parseStructuredPrompt(raw,metadata)
    doc=removePromptField(doc,doc.nodes[0].fields[0].id)
    assert.equal(serializeStructuredPrompt(doc),'【模型节点】\n键：多行\n    【引用不是节点】\n    键：仍是内容\n')
    doc=removePromptField(doc,doc.nodes[1].fields[0].id)
    assert.equal(serializeStructuredPrompt(doc),'【模型节点】\n')
  }
})

test('Deleting every model and custom node saves an empty source and allows a fresh first node', () => {
  let doc=appendPromptNodesFromText(parseStructuredPrompt('【模型】\n键：值\n'),'自建\n键：自建值')
  for(const id of doc.nodes.map(n=>n.id))doc=removePromptNode(doc,id)
  assert.equal(serializeStructuredPrompt(doc),'');assert.equal(serializePromptMetadata(doc),'')
  doc=parseStructuredPrompt(serializeStructuredPrompt(doc),serializePromptMetadata(doc))
  assert.equal(serializeStructuredPrompt(doc),'')
  doc=appendPromptNodesFromText(doc,'新节点\n新键：新值')
  assert.equal(serializeStructuredPrompt(doc),'【新节点】\n新键：新值\n')
})
