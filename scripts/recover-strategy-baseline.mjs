import fs from 'node:fs'
import {execFileSync} from 'node:child_process'
const log='C:/Users/19918/.codex/sessions/2026/10/03/rollout-2026-10-03T13-40-46-01a10047-794b-7630-8b04-a659b2f57f73.jsonl'
let source=execFileSync('git',['show','HEAD:web-console/src/views/LiveStrategyView.vue'],{encoding:'utf8'}).replaceAll('\r\n','\n').split('\n')
for(const line of fs.readFileSync(log,'utf8').split('\n')){
 if(!line)continue;const event=JSON.parse(line)
 if(event.timestamp>'2026-10-04T12:00:00Z'||event.type!=='event_msg'||event.payload.item?.type!=='FileChange')continue
 for(const[path,change]of Object.entries(event.payload.item.changes||{})){
  if(!path.endsWith('LiveStrategyView.vue')||!change.unified_diff)continue
  const hunks=[];let hunk
  for(const l of change.unified_diff.replaceAll('\r\n','\n').split('\n')){if(l.startsWith('@@')){hunk=[];hunks.push(hunk)}else if(hunk&&/^[ +\-]/.test(l))hunk.push(l)}
  for(const h of hunks){
   const old=h.filter(l=>!l.startsWith('+')).map(l=>l.slice(1));const next=h.filter(l=>!l.startsWith('-')).map(l=>l.slice(1))
   const index=source.findIndex((_,i)=>old.every((l,j)=>source[i+j]===l))
   if(index<0){const nearby=source.findIndex(l=>l.includes('fullShowNotice.value = `直播智能体'));throw Error('Missing history at '+event.timestamp+' '+JSON.stringify(old).slice(0,400)+' ACTUAL '+JSON.stringify(source.slice(nearby-3,nearby+4)))}
   source.splice(index,old.length,...next)
  }
 }
}
console.log(JSON.stringify({patch:'*** Begin Patch\n*** Add File: E:/直播伴播/artifacts/ops-rooms-overlay/LiveStrategyView.vue\n'+source.map(l=>'+'+l).join('\n')+'\n*** End Patch'}))
