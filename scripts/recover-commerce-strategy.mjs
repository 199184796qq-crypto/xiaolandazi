import fs from 'node:fs'
import crypto from 'node:crypto'
const file='web-console/src/views/LiveStrategyView.vue'
let source=fs.readFileSync(file,'utf8').split('\n')
const changes=[],errors=[],cutoff=process.env.COMMERCE_BASE_CUTOFF||'2026-10-04T14:45:00Z'
for(const l of fs.readFileSync('C:/Users/19918/.codex/sessions/2026/10/04/rollout-2026-10-04T19-00-13-01a10692-4c6b-7193-b58a-134a5f16b179.jsonl','utf8').split('\n')){if(!l)continue;const x=JSON.parse(l);if(x.timestamp<=cutoff||x.type!=='event_msg'||x.payload.item?.type!=='FileChange')continue;for(const[p,c]of Object.entries(x.payload.item.changes||{}))if(p.replaceAll('\\','/').endsWith(file))changes.push({time:x.timestamp,...c})}
for(const c of changes.reverse()){
 const hunks=[];let h
 for(const l of c.unified_diff.split('\n'))if(l.startsWith('@@')){h={rows:[],index:Number(/\+(\d+)/.exec(l)[1])-1};hunks.push(h)}else if(h&&/^[ +\-]/.test(l))h.rows.push(l)
 for(const h of hunks.reverse()){
  const before=h.rows.filter(l=>l[0]!=='+').map(l=>l.slice(1)),after=h.rows.filter(l=>l[0]!=='-').map(l=>l.slice(1)),hits=[]
  for(let i=0;i<=source.length-after.length;i++)if(after.every((l,k)=>source[i+k].replace(/\r$/,'')===l.replace(/\r$/,'')))hits.push(i)
  if(!hits.length){errors.push({time:c.time,after:after.slice(0,3)});continue}
  hits.sort((a,b)=>Math.abs(a-h.index)-Math.abs(b-h.index));source.splice(hits[0],after.length,...before)
 }
}
const text=source.join('\n'),pinned=text.replace(/^.*<small v-if="anchorStyleTestResult\.(?:runtime_evaluation|style_purity|style_vector_evaluation\?\.available)">.*\r?\n/gm,'')
if(process.argv[2]==='slice')console.log(JSON.stringify(source.slice(Number(process.argv[3]),Number(process.argv[3])+500)))
else console.log(JSON.stringify({errors,hash:crypto.createHash('sha256').update(pinned).digest('hex'),crlf_hash:crypto.createHash('sha256').update(pinned.replaceAll('\n','\r\n')).digest('hex'),lines:source.length,changes:changes.length}))
