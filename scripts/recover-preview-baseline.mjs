// Extract only this task's historical style source patches into an isolated build overlay.
import fs from 'node:fs'
const log='C:/Users/19918/.codex/sessions/2026/10/03/rollout-2026-10-03T13-40-46-01a10047-794b-7630-8b04-a659b2f57f73.jsonl'
const target=p=>/live_agent_plan_style_(test|preview)(?:_test)?\.go$/.test(p)
const patches=[]
for(const line of fs.readFileSync(log,'utf8').split('\n')){
  if(!line)continue
  const item=JSON.parse(line)
  if(item.timestamp>'2026-10-04T12:00:00Z')continue
  if(item.type!=='response_item'||item.payload.type!=='custom_tool_call')continue
  for(const match of (item.payload.input||'').matchAll(/tools\.apply_patch\(("(?:\\.|[^"\\])*")\)/g)){
    const sections=JSON.parse(match[1]).split(/(?=\*\*\* (?:Add|Update|Delete) File: )/).slice(1)
    const kept=sections.filter(s=>target(s.split('\n')[0])).map(s=>s.replace(/\*\*\* End Patch\s*$/,'').replace(/(\*\*\* (?:Add File|Update File|Delete File|Move to): )([^\n]+)/g,(_,head,path)=>head+'E:/直播伴播/artifacts/ops-rooms-overlay/'+path.replaceAll('\\','/').split('/').at(-1)))
    if(kept.length)patches.push('*** Begin Patch\n'+kept.join('').trimEnd()+'\n*** End Patch')
  }
}
console.log(JSON.stringify({patches}))
