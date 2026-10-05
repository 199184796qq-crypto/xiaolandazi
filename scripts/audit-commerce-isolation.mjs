import fs from 'node:fs'
import crypto from 'node:crypto'
const hash=s=>crypto.createHash('sha256').update(s).digest('hex')
const baseline=JSON.parse(fs.readFileSync('E:/直播伴播-local/releases/20261004-speech-model-discovery-v1-dirty-manifest.json','utf8'))
const path='web-console/src/views/LiveStrategyView.vue'
let s=fs.readFileSync('artifacts/commerce-isolation/'+path,'utf8').replace(/^.*<small v-if="anchorStyleTestResult\.(?:runtime_evaluation|style_purity|style_vector_evaluation\?\.available)">.*\r?\n/gm,'')
console.log('Strategy pinned hash',hash(s.replaceAll('\r\n','\n')),hash(s.replaceAll('\r\n','\n').replaceAll('\n','\r\n')))
const expected='22d1bc28ae47ed25023376ac537ef8b6657bc040f2631e3a09e2c5fbcf08654a',lines=s.replaceAll('\r\n','\n').split('\n')
for(let n=0;n<5;n++){const t=s.trimEnd()+'\n'.repeat(n);if([t,t.replaceAll('\n','\r\n')].some(x=>hash(x)===expected))console.log('MATCH EOF',n)}
for(let i=0;i<lines.length;i++)if(!lines[i].trim()){const t=[...lines.slice(0,i),...lines.slice(i+1)].join('\n');if([t,t.replaceAll('\n','\r\n')].some(x=>hash(x)===expected))console.log('MATCH removed blank',i+1)}
const schema='management-service/internal/db/live_runtime_schema.sql',target=baseline.files.find(f=>f.path===schema).sha256
const current=fs.readFileSync('artifacts/commerce-isolation/'+schema,'utf8'),bin=fs.readFileSync('artifacts/20261004-ops-rooms-v1/bin/management-service')
const prefix=Buffer.from(current.split('\n')[0]);let at=bin.indexOf(prefix),found
while(at>=0&&!found){for(let len=Buffer.byteLength(current)-5000;len<Buffer.byteLength(current)*2+5000;len++){if(hash(bin.subarray(at,at+len))===target){found=bin.subarray(at,at+len).toString();break}}at=bin.indexOf(prefix,at+1)}
console.log('Embedded deployed schema found',Boolean(found))
if(process.argv[2]==='proof'){if(!found)throw Error('Deployed schema not found');if(current.replaceAll('\r\n','\n').trimEnd()!==found.replaceAll('\r\n','\n').trimEnd())throw Error('Schema differs');console.log('Frozen schema content exactly matches embedded deployed schema (line endings/EOF normalized)')}
if(process.argv[2]==='schema'&&found)console.log(JSON.stringify({patch:'*** Begin Patch\n*** Update File: E:/直播伴播/artifacts/commerce-isolation/'+schema+'\n@@\n'+current.replaceAll('\r\n','\n').trimEnd().split('\n').map(l=>'-'+l).join('\n')+'\n'+found.replaceAll('\r\n','\n').trimEnd().split('\n').map(l=>'+'+l).join('\n')+'\n*** End Patch'}))
