import type { PlanMaterialClaim, PlanMaterialClaimInput, MaterialCondition, MaterialObjectTarget, MaterialLinkObject, MaterialStoreObject } from './api'
export type ImportRow = PlanMaterialClaimInput & { uid: string; id?: number; revision?: string; embedding_model?: string; rowState?: 'new' | 'conflict' | 'in_library' | 'lib_conflict'; conflictWith?: string; oldContent?: string }
export function row(input: PlanMaterialClaimInput | PlanMaterialClaim): ImportRow {
 const clone = structuredClone(input)
 return { ...clone, uid: 'id' in clone ? `lib-${clone.id}` : clone.client_id || crypto.randomUUID(), destination: clone.destination || 'fact', conditions:clone.conditions||[], exceptions: clone.exceptions || [] }
}
export function toInput(it: ImportRow): PlanMaterialClaimInput {
 const allowed=['object_targets','is_benefit','claim_key','topic','attribute','value','content','polarity','fact_type','conditions','source_quote','source_sentence','scope_kind','store_name','product_link_id','replaces_id','destination','kind','scope_name','exceptions','question','answer','aliases','risk_flags','risks','p0_hits','needs_confirmation','needs_split','atomicity_reason','polarity_issue','condition_issue']
 const input=Object.fromEntries(allowed.filter(k=>k in it).map(k=>[k,(it as unknown as Record<string,unknown>)[k]]))
 return {...input,client_id:it.uid} as PlanMaterialClaimInput
}
export function genericStore(name:string){return ['本直播间','直播间','本店','门店','线下门店','全部门店','商家','全场'].includes(name.trim())}
export function targetsFor(it:ImportRow):MaterialObjectTarget[]{
 if(it.object_targets)return it.object_targets
 if(it.scope_kind==='unresolved')return []
 if(it.scope_kind==='product')return it.product_link_id>0?[{kind:'product',link_no:it.product_link_id}]:[]
 if(it.scope_kind==='store'){if(!it.store_name?.trim()||it.store_name==='未说明')return [];return genericStore(it.store_name)?[{kind:'plan'}]:[{kind:'store',store_name:it.store_name.trim()}]}
 if(['item','variant','voucher','item_group'].includes(it.scope_kind))return []
 if(it.scope_kind==='service'&&(!it.scope_name?.trim()||it.scope_name==='未说明'))return []
 return [{kind:'plan'}]
}
export function resolveRowObjects(it:ImportRow,links:MaterialLinkObject[]):ImportRow{
 const targets:MaterialObjectTarget[]=[]
 for(const target of targetsFor(it)){
  if(target.kind==='plan'){targets.push({kind:'plan'});continue}
  if(target.kind==='store'){if(target.store_name?.trim())targets.push({kind:'store',store_name:target.store_name.trim()});continue}
  const matches=links.filter(l=>target.link_id?l.id===target.link_id:Number(l.link_key.match(/^第?(\d+)\s*(?:号(?:链接)?)?$/)?.[1])===target.link_no)
  if(matches.length!==1)return assignedTargets(it,[])
  targets.push({kind:'product',link_id:matches[0]!.id,link_key:matches[0]!.link_key})
 }
 return assignedTargets(it,targets)
}
export function assignedTargets(it:ImportRow,targets:MaterialObjectTarget[]):ImportRow{
 const t=targets[0];return {...it,object_targets:targets,scope_kind:t?.kind||'unresolved',store_name:t?.store_name||'',product_link_id:t?.link_id||t?.link_no||0,scope_name:'',needs_confirmation:targets.length===0?true:it.needs_confirmation}
}
export function targetKey(t:MaterialObjectTarget){return t.kind==='plan'?'plan':t.kind==='store'?`store:${t.store_name}`:t.link_id?`link:${t.link_id}`:`product:${t.link_no}`}
export function objectKeys(it:ImportRow){const targets=targetsFor(it);return targets.length?targets.map(targetKey):['unresolved']}
export function objectKey(it:ImportRow){return objectKeys(it)[0]||'unresolved'}
export function objectLabel(key:string,links:MaterialLinkObject[]=[]):string{
 if(key==='all')return '全部属性';if(key==='plan')return '全局';if(key==='unresolved')return '待归属'
 const [kind,...parts]=key.split(':');const name=parts.join(':')
 if(kind==='link'){const link=links.find(l=>l.id===Number(name));if(!link)return '链接已移除';return link.display_name?.trim()||(/^\d+$/.test(link.link_key)?`${link.link_key}号链接`:link.link_key)}
 if(kind==='product')return `${name}号链接`
 return kind==='store'?name:'全局'
}
export function matchesObject(it:ImportRow,key:string,stores:MaterialStoreObject[]=[],includeGlobal=true){
 const keys=objectKeys(it);if(key==='all')return true;if(keys.includes(key))return true
 if(!key.startsWith('link:'))return false
 if(keys.includes('plan'))return includeGlobal
 const id=Number(key.slice(5));return targetsFor(it).some(t=>t.kind==='store'&&stores.some(s=>s.name===t.store_name&&s.link_ids.includes(id)))
}
export function scopeIdentity(it:ImportRow){return [...objectKeys(it)].sort()}
export function conditionLabel(c: MaterialCondition) {
 if(typeof c === 'string') return c
 return c.raw || `${c.field || ''} ${c.op || ''} ${(c.values || []).join('、')}${c.universe ? '（'+c.universe+'）' : ''}`.trim()
}
const canonical = (value: unknown): unknown => Array.isArray(value) ? value.map(canonical) : value && typeof value === 'object' ? Object.fromEntries(Object.entries(value).sort(([a],[b])=>a.localeCompare(b)).map(([k,v])=>[k,canonical(v)])) : value
export function logicalKey(it: ImportRow) {
 return JSON.stringify([scopeIdentity(it), it.destination || 'fact', it.claim_key || it.topic, it.destination==='qa'?it.question:undefined, canonical(it.conditions || []),canonical(it.exceptions || [])])
}
export function sameContent(a: ImportRow,b: ImportRow) {
 return logicalKey(a) === logicalKey(b) && JSON.stringify([a.content,a.value,a.polarity,a.question,a.answer,!!a.is_benefit])===JSON.stringify([b.content,b.value,b.polarity,b.question,b.answer,!!b.is_benefit])
}
export function hasCondition(it: ImportRow) { return !!((it.conditions || []).length || (it.exceptions || []).length) }
export function isRisk(it: ImportRow) {return !!(it.risks?.length || it.risk_flags?.length || it.p0_hits?.length)}
export function isTodo(it:ImportRow){return !targetsFor(it).length}
export function canConfirm(it:ImportRow){return !it.id&&!['word','gap'].includes(it.destination||'')&&!['conflict','in_library'].includes(it.rowState||'')&&targetsFor(it).length>0&&!it.polarity_issue&&!it.condition_issue}
export function compareLibrary(items: ImportRow[], library: ImportRow[]) {
 return items.map(it=>{
  const old=library.find(x=>logicalKey(x)===logicalKey(it))
  if(!old) return {...it,rowState:'new',oldContent:undefined} as ImportRow
  const next={...it,is_benefit:it.is_benefit===undefined?old.is_benefit:it.is_benefit}
  return {...next,rowState:sameContent(next,old)?'in_library':'lib_conflict',replaces_id:sameContent(next,old)?undefined:old.id,oldContent:old.content} as ImportRow
 })
}
export function mergeDraft(draft: ImportRow[], incoming: ImportRow[], mode: 'append' | 'overwrite') {
 if(mode==='overwrite') return incoming
 const next=[...draft]
 for(const item of incoming) {
  const old=next.find(x=>logicalKey(x)===logicalKey(item))
  if(!old) next.push(item)
  else next.push({...item,uid:item.uid===old.uid?crypto.randomUUID():item.uid,rowState:'conflict',conflictWith:old.uid,oldContent:old.content})
 }
 return next
}
export function suggestMerge(text:string,draft:ImportRow[],incoming:ImportRow[]) {
 if(!draft.length) return 'append'
 if(/覆盖|替换|重新导入|全部更新|整批/.test(text))return 'overwrite'
 const overlap=incoming.filter(x=>draft.some(d=>logicalKey(x)===logicalKey(d))).length/Math.max(1,incoming.length)
 return incoming.length>=5&&overlap>=.4 ? 'overwrite' : 'append'
}

export type DuplicateGroup={key:string;objectKey:string;rows:ImportRow[];different:boolean}
const normalizedText=(value:unknown)=>String(value??'').normalize('NFKC').trim().replace(/\s+/g,' ').toLowerCase()
function duplicateRules(it:ImportRow){return JSON.stringify([it.destination||'fact',canonical(it.conditions||[]),canonical(it.exceptions||[])])}
function duplicateAttribute(a:ImportRow,b:ImportRow){
 const key=normalizedText(a.claim_key),other=normalizedText(b.claim_key)
 if(a.destination==='qa'||b.destination==='qa')return !!normalizedText(a.question)&&normalizedText(a.question)===normalizedText(b.question)
 return (!!key&&key!=='claim'&&key===other)||!!normalizedText(a.topic||a.attribute)&&normalizedText(a.topic||a.attribute)===normalizedText(b.topic||b.attribute)
}
// Existing-attribute review is broader than an exact duplicate alert.
function attributeMatch(a:ImportRow,b:ImportRow){return duplicateRules(a)===duplicateRules(b)&&duplicateAttribute(a,b)&&objectKeys(a).some(k=>k!=='unresolved'&&objectKeys(b).includes(k))}
export function duplicateOriginal(it:ImportRow){return it.source_sentence||it.source_quote||''}
export function exactDuplicateData(a:ImportRow,b:ImportRow){
 const value=String(a.value??''),original=duplicateOriginal(a)
 return !!value.trim()&&!!original.trim()&&value===String(b.value??'')&&original===duplicateOriginal(b)
}
function duplicateBody(it:ImportRow){return JSON.stringify([normalizedText(it.content),normalizedText(it.value),it.polarity||'positive',normalizedText(it.question),normalizedText(it.answer)])}
// Duplicate identity uses an explicit owner only. Inheritance and equal text
// never turn separate objects into the same owner. Unknown ownership stays pending.
export function duplicateGroups(items:ImportRow[],_stores:MaterialStoreObject[]=[]):DuplicateGroup[]{
 const owners=[...new Set(items.flatMap(objectKeys))].filter(k=>k!=='unresolved'),out:DuplicateGroup[]=[]
 for(const owner of owners){
  const local=items.filter(it=>objectKeys(it).includes(owner)),parents=local.map((_,i)=>i)
  const root=(i:number):number=>parents[i]===i?i:(parents[i]=root(parents[i]!))
  for(let i=0;i<local.length;i++)for(let j=i+1;j<local.length;j++)if(attributeMatch(local[i]!,local[j]!)&&exactDuplicateData(local[i]!,local[j]!))parents[root(j)]=root(i)
  const groups=new Map<number,ImportRow[]>()
  local.forEach((it,i)=>{const key=root(i);groups.set(key,[...(groups.get(key)||[]),it])})
  for(const rows of groups.values())if(rows.length>1)out.push({key:JSON.stringify([owner,rows.map(it=>it.uid).sort()]),objectKey:owner,rows,different:new Set(rows.map(duplicateBody)).size>1})
 }
 return out
}
export type ImportReviewGroup={key:string;draft:ImportRow[];library:ImportRow[]}
export function importReviewGroups(drafts:ImportRow[],library:ImportRow[],_stores:MaterialStoreObject[]=[]):ImportReviewGroup[]{
 // Build components across draft/library edges only. Separate new cards can
 // share one old multi-target record, so they must get one consistent choice.
 const groups:ImportReviewGroup[]=[]
 for(const it of drafts){
  const matches=library.filter(old=>attributeMatch(it,old))
  if(!matches.length)continue
  const ids=new Set(matches.map(old=>old.uid)),linked=groups.filter(g=>g.library.some(old=>ids.has(old.uid)))
  const merged={key:'',draft:[it,...linked.flatMap(g=>g.draft)],library:[...new Map([...matches,...linked.flatMap(g=>g.library)].map(old=>[old.uid,old])).values()]}
  merged.key=merged.draft.map(x=>x.uid).sort().join('|');for(const g of linked)groups.splice(groups.indexOf(g),1);groups.push(merged)
 }
 return groups
}
export function resolvedDuplicate(group:DuplicateGroup,keep:ImportRow):ImportRow{
 const next=assignedTargets(JSON.parse(JSON.stringify(keep)) as ImportRow,targetsFor(keep).filter(t=>targetKey(t)===group.objectKey))
 next.rowState='new';delete next.conflictWith;delete next.oldContent;delete next.replaces_id
 return next
}
// Removing a duplicate from A must not remove its separately assigned B owner.
export function duplicateRemainder(it:ImportRow,owner:string):ImportRow|null{
 const targets=targetsFor(it).filter(t=>targetKey(t)!==owner)
 return targets.length?assignedTargets(it,targets):null
}
export type Truth = 'true' | 'false' | 'unknown'
const provinces='北京 天津 河北 山西 内蒙古 辽宁 吉林 黑龙江 上海 江苏 浙江 安徽 福建 江西 山东 河南 湖北 湖南 广东 广西 海南 重庆 四川 贵州 云南 西藏 陕西 甘肃 青海 宁夏 新疆 香港 澳门 台湾'.split(' ')
const aliases: Record<string,string[]>={川渝:['四川','重庆'],江浙沪:['江苏','浙江','上海'],东三省:['辽宁','吉林','黑龙江'],北上广:['北京','上海','广东']}
function regions(v: unknown) { const s=String(v || '').trim();return aliases[s] || provinces.filter(p=>s===p||s===p+'省'||s===p+'市'||s===p+'自治区'||s===p+'特别行政区') }
export function evalCondition(c: MaterialCondition,context:Record<string,string>):Truth {
 if(typeof c==='string'||c.op==='raw'||c.status==='unresolved')return 'unknown'
 if(c.op==='and'||c.op==='or') {
  const results=(c.values || []).map(x=>evalCondition(x as MaterialCondition,context))
  if(c.op==='and')return results.includes('false')?'false':results.includes('unknown')?'unknown':'true'
  return results.includes('true')?'true':results.includes('unknown')?'unknown':'false'
 }
 const actual=context[c.field || ''];if(actual==null||actual==='')return 'unknown'
 const values=(c.values || []).flatMap(v=>c.field==='region'?regions(v):[String(v)])
 const actualValues=c.field==='region'?regions(actual):[actual]
 if(!actualValues.length||!values.length)return 'unknown'
 const match=actualValues.some(v=>values.includes(v))
 if(c.op==='in'||c.op==='eq')return match?'true':'false'
 if(c.op==='not_in'||c.op==='neq')return c.op==='not_in'&&!c.universe?'unknown':match?'false':'true'
 const n=Number(actual),v=Number(values[0]);if(!Number.isFinite(n)||!Number.isFinite(v))return 'unknown'
 const ops:Record<string,boolean>={gt:n>v,gte:n>=v,lt:n<v,lte:n<=v,between:n>=v&&n<=Number(values[1])}
 return c.op && c.op in ops ? ops[c.op]?'true':'false' : 'unknown'
}
export function applicability(it:ImportRow,context:Record<string,string>):Truth {
 let out:Truth='true'
 for(const c of it.conditions || []){const v=evalCondition(c,context);if(v==='false')return 'false';if(v==='unknown')out='unknown'}
 for(const c of it.exceptions || []){const v=evalCondition(c,context);if(v==='true')return 'false';if(v==='unknown')out='unknown'}
 return out
}
