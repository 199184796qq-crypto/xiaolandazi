import { reactive } from 'vue'
import { session } from './session'
import { canUseWorkInbox } from './workInboxRoutes'
export { canUseWorkInbox } from './workInboxRoutes'


export interface InboxGroup { key:string; topic:string; department:string; category:string; title:string; to:string; count:number; due_count:number; shared_queue:boolean }
export interface InboxSnapshot { groups:InboxGroup[]; total:number; version:string; as_of:string; stale:boolean; unavailable:string[] }
export interface InboxItem { key:string; id:number; reference:string; title:string; status:string; created_at:string; due_at?:string; to:string }
export interface InboxPage { group:InboxGroup; items:InboxItem[]; total:number; page:number; page_size:number; total_pages:number }
export const inbox = reactive<{snapshot:InboxSnapshot|null;loading:boolean;error:string}>({snapshot:null,loading:false,error:''})
export const inboxCategories = [
 {key:'review',label:'待审核'}, {key:'accept',label:'待接单'}, {key:'process',label:'待处理'}, {key:'supplement',label:'待补充'}, {key:'confirm',label:'待确认'},
]
let generation=0, snapshotEpoch=0, active=false, owner='', stream:EventSource|undefined, request:AbortController|undefined
let retry:ReturnType<typeof setTimeout>|undefined, retryDelay=10000, busy:Promise<void>|undefined
function validateSnapshot(value:unknown):value is InboxSnapshot {
 if(!value||typeof value!=='object')return false
 const v=value as InboxSnapshot
 return Array.isArray(v.groups)&&typeof v.version==='string'&&typeof v.stale==='boolean'&&Number.isSafeInteger(v.total)&&v.total>=0&&v.groups.every(g=>typeof g.key==='string'&&typeof g.to==='string'&&g.to.startsWith('/')&&!g.to.startsWith('//')&&Number.isSafeInteger(g.count)&&g.count>=0)
}
function accept(value:unknown){
 if(!validateSnapshot(value))throw new Error('待办返回格式异常')
 const previous=inbox.snapshot?.version
 inbox.snapshot=value;inbox.error=value.stale?'部分待办同步失败，数字暂不展示，请重试。':''
 if(value.stale)scheduleRetry();else clearTimeout(retry)
 if(previous!==value.version)window.dispatchEvent(new CustomEvent('work-inbox-updated'))
}
function scheduleRetry(){
 clearTimeout(retry);if(!active||document.hidden)return
 retry=setTimeout(()=>{void refreshInbox().then(connect)},retryDelay+Math.random()*2000)
 retryDelay=Math.min(retryDelay*2,60000)
}
function connect(){
 if(!canUseWorkInbox(session.bootstrap?.actor.role)){stopInbox();return}
 if(!active||document.hidden||stream||!inbox.snapshot||inbox.error)return
 const version=generation;const es=new EventSource('/api/v1/work/inbox/stream',{withCredentials:true});stream=es
 es.addEventListener('snapshot',event=>{if(version!==generation||stream!==es)return;try{snapshotEpoch++;accept(JSON.parse((event as MessageEvent).data));retryDelay=10000}catch{inbox.error='待办推送暂不可用';scheduleRetry()}})
 es.addEventListener('reset',()=>{if(stream!==es)return;inbox.snapshot=null;inbox.error='登录状态已变化，请刷新登录状态';es.close();stream=undefined})
 es.addEventListener('unavailable',()=>{if(stream!==es)return;inbox.snapshot=null;inbox.error='待办权限暂时无法核对';es.close();stream=undefined;scheduleRetry()})
 es.onerror=()=>{if(stream!==es)return;es.close();stream=undefined;inbox.error='实时提醒已断开，正在重连；请以业务单据为准';scheduleRetry()}
}
export async function refreshInbox():Promise<void>{
 if(!canUseWorkInbox(session.bootstrap?.actor.role)){stopInbox();return}
 if(!active||document.hidden)return
 if(busy)return busy
 const version=generation,epoch=snapshotEpoch,c=new AbortController();request=c;inbox.loading=true
 const task=(async()=>{try{const response=await fetch('/api/v1/work/inbox',{credentials:'include',signal:c.signal,headers:{Accept:'application/json'}});if(!response.ok)throw new Error(response.status===401?'登录已失效':'待办同步暂不可用');const value=await response.json();if(version===generation&&epoch===snapshotEpoch)accept(value)}catch(e){if(version===generation&&!c.signal.aborted){inbox.error=e instanceof Error?e.message:'待办同步失败';scheduleRetry()}}finally{if(version===generation){inbox.loading=false;busy=undefined}}})()
 busy=task;return task
}
function visibility(){if(document.hidden){stream?.close();stream=undefined;clearTimeout(retry)}else void refreshInbox().then(connect)}
function changed(){void refreshInbox().then(connect)}
export function startInbox(key:string){
 if(!canUseWorkInbox(session.bootstrap?.actor.role)){stopInbox();return}
 if(active&&owner===key)return
 stopInbox();if(!key||!session.bootstrap?.actor.user_id)return
 active=true;owner=key
 document.addEventListener('visibilitychange',visibility)
 window.addEventListener('focus',changed)
 window.addEventListener('system-config-updated',changed)
 void refreshInbox().then(connect)
}
export function stopInbox(){
 active=false;owner='';generation++;stream?.close();stream=undefined;request?.abort();request=undefined;clearTimeout(retry);busy=undefined;retryDelay=10000
 inbox.snapshot=null;inbox.loading=false;inbox.error=''
 document.removeEventListener('visibilitychange',visibility);window.removeEventListener('focus',changed);window.removeEventListener('system-config-updated',changed)
}
export function inboxCountFor(path?:string):number {
 if(!canUseWorkInbox(session.bootstrap?.actor.role)||!path||!inbox.snapshot||inbox.snapshot.stale||inbox.error)return 0
 const p=path.split('?')[0]?.split('#')[0]
 if(p==='/work/inbox')return inbox.snapshot.total
 const parentTopics:Record<string,string[]>={'/staff/finance':['finance'],'/resources':['inventory','logistics'],'/operations/live':['support']}
 const groups=inbox.snapshot.groups.filter(g=>g.to===p||parentTopics[p||'']?.includes(g.topic))
 return groups.reduce((sum,g)=>sum+g.count,0)
}
export function isInboxIntent(text:string):boolean{return /待办|待审核|待接单|待处理|待补充|待确认|要处理什么|该处理什么|到期回访|哪些.{0,8}(逾期|超时)|逾期待办/.test(text)&&!/设计|话术|规则怎么/.test(text)}
export async function getInboxItems(group:string,page=1,due=false,signal?:AbortSignal):Promise<InboxPage>{
 if(!canUseWorkInbox(session.bootstrap?.actor.role))throw new Error('当前账号不提供统一待办入口')
 const query=new URLSearchParams({group,page:String(page),page_size:'12',due:due?'1':'0'})
 const response=await fetch('/api/v1/work/inbox/items?'+query,{credentials:'include',signal})
 if(!response.ok)throw new Error(response.status===404?'这个分类已不在当前权限范围':'读取待办明细失败，请重试')
 return response.json()
}
