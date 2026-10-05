// Reconstruct the deployed source baseline in a build overlay, never edit another task's checkout.
import fs from 'node:fs'
import crypto from 'node:crypto'
const log='C:/Users/19918/.codex/sessions/2026/10/04/rollout-2026-10-04T19-00-13-01a10692-4c6b-7193-b58a-134a5f16b179.jsonl'
const cutoff='2026-10-04T14:45:00Z', patches=[]
// The calls use both spaced assignments and compact function arguments.
for(const l of fs.readFileSync(log,'utf8').split('\n')){if(!l)continue;const x=JSON.parse(l);if(x.timestamp<=cutoff||x.type!=='response_item'||x.payload.type!=='custom_tool_call')continue;const s=x.payload.input||'';for(const m of s.matchAll(/(?:const patch\s*=\s*|tools\.apply_patch\()\s*("(?:\\.|[^"\\])*")/g)){const p=JSON.parse(m[1]);if(!patches.includes(p))patches.push(p)}}
const state=new Map(), failures=[], changed=new Set()
const relative=p=>p.replaceAll('\\','/').replace(/^E:\/直播伴播\//,'')
for(const patch of patches.reverse())for(const sec of patch.split(/(?=\*\*\* (?:Add|Update|Delete) File: )/).slice(1).reverse()){
 const lines=sec.replace(/\*\*\* End Patch\s*$/,'').trimEnd().split('\n'),m=/^\*\*\* (Add|Update|Delete) File: (.+)$/.exec(lines.shift());if(!m)continue
 const p=relative(m[2]);if(!p.startsWith('management-service/')&&!p.startsWith('web-console/src/'))continue
 let content=state.has(p)?state.get(p):fs.existsSync(p)?fs.readFileSync(p,'utf8').replaceAll('\r\n','\n'):null
 if(m[1]==='Add'){state.set(p,null);changed.add(p);continue}
 if(m[1]==='Delete'){failures.push('deleted source '+p);continue}
 if(content===null){failures.push('missing '+p);continue}
 const hunks=[];let h=null
 for(const line of lines){if(line.startsWith('@@')){h=[];hunks.push(h)}else if(h)h.push(line)}
 for(const hunk of hunks.reverse()){
   const before=hunk.filter(l=>l[0]!=='+'&&l[0]!=='\\').map(l=>l.slice(1)),after=hunk.filter(l=>l[0]!=='-'&&l[0]!=='\\').map(l=>l.slice(1)),now=content.split('\n')
   const norm=l=>l.replace(/\s+/g,'').trim(),hits=[]
   for(let i=0;i<=now.length-after.length;i++)if(after.every((l,k)=>norm(now[i+k])===norm(l)))hits.push(i)
   if(hits.length!==1){failures.push(p+' hunk '+after.slice(0,2).join(' | ')+' matches='+hits.length);if(process.argv[3]==='debug'&&p===process.argv[2])console.error(JSON.stringify({before,after}));continue}
   now.splice(hits[0],after.length,...before);content=now.join('\n')
 }
 state.set(p,content);changed.add(p)
}
const strategy='web-console/src/views/LiveStrategyView.vue'
if(state.has(strategy))state.set(strategy,state.get(strategy).replace(/\n\s*<section class="strategy-style-overlay-panel">[\s\S]*?\n\s*<div v-if="scriptNotice"/, '\n          <div v-if="scriptNotice"').replace(/\n\.strategy-style-overlay-panel\{[\s\S]*?\n(?=\.strategy-anchor-style-summary)/,''))
const out={patches:[],replace:{},failures,changed:[...changed]}
for(const [p,content] of state){const target='E:/直播伴播/artifacts/commerce-isolation/'+p;out.replace['E:/直播伴播/'+p]=content===null?'':target;if(content!==null)out.patches.push('*** Begin Patch\n*** Add File: '+target+'\n'+content.trimEnd().split('\n').map(l=>'+'+l).join('\n')+'\n*** End Patch')}
const chosen=process.argv[2]
if(process.argv[3]==='slice'){
 const content=state.get(chosen);console.log(JSON.stringify(content.split('\n').slice(Number(process.argv[4]),Number(process.argv[4])+500))); 
}else console.log(JSON.stringify(chosen?{patches:out.patches.filter(p=>p.includes('commerce-isolation/'+chosen+'\n')),replace:out.replace,failures:out.failures.filter(f=>f.startsWith(chosen))}: {...out,patches:out.patches.map(p=>p.split('\n')[1])}))
