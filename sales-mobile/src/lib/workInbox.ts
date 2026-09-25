import { writable } from 'svelte/store';
export interface Group {key:string;topic:string;department:string;category:string;title:string;to:string;count:number;due_count:number;shared_queue:boolean}
export interface Snapshot {groups:Group[];total:number;version:string;as_of:string;stale:boolean;unavailable:string[]}
export interface Item {key:string;id:number;reference:string;title:string;to:string;created_at:string;due_at?:string}
export interface Page {group:Group;items:Item[];total:number;total_pages:number;page:number;page_size:number}
interface State {snapshot:Snapshot|null;error:string;loading:boolean}
let value:State={snapshot:null,error:'',loading:false};
export const workInbox=writable(value);
export const categories=[{key:'review',label:'待审核'},{key:'accept',label:'待接单'},{key:'process',label:'待处理'},{key:'supplement',label:'待补充'},{key:'confirm',label:'待确认'}];
let owner='',generation=0,epoch=0,source:EventSource|undefined,controller:AbortController|undefined;
let retry:ReturnType<typeof setTimeout>|undefined,flight:Promise<void>|undefined;
function patch(next:Partial<State>){value={...value,...next};workInbox.set(value)}
function accept(raw:unknown){const v=raw as Snapshot;if(!v||!Array.isArray(v.groups)||!Number.isSafeInteger(v.total)||v.total<0||typeof v.stale!=='boolean'||typeof v.version!=='string'||v.groups.some(g=>!Number.isSafeInteger(g.count)||g.count<0))throw new Error('待办返回异常');patch({snapshot:v,error:v.stale?'部分待办暂时无法同步，不代表没有待办':''});if(v.stale)schedule();else clearTimeout(retry)}
function schedule(){clearTimeout(retry);if(owner&&!document.hidden)retry=setTimeout(()=>{void refreshWorkInbox().then(connect)},30000+Math.random()*3000)}
function connect(){if(!owner||document.hidden||source||value.error||!value.snapshot)return;const n=generation,s=new EventSource('/api/v1/work/inbox/stream',{withCredentials:true});source=s;
 s.addEventListener('snapshot',e=>{if(source!==s||n!==generation)return;try{epoch++;accept(JSON.parse((e as MessageEvent).data))}catch{patch({error:'待办同步暂不可用'});schedule()}});
 s.addEventListener('reset',()=>{if(source===s){s.close();source=undefined;patch({snapshot:null,error:'登录状态已变化，请重新登录'})}});
 s.addEventListener('unavailable',()=>{if(source===s){s.close();source=undefined;patch({snapshot:null,error:'待办权限暂时无法核对'});schedule()}});
 s.onerror=()=>{if(source===s){s.close();source=undefined;patch({error:'提醒连接中断，正在重连'});schedule()}};
}
export async function refreshWorkInbox(){if(!owner||document.hidden)return;if(flight)return flight;const n=generation,priorEpoch=epoch,c=new AbortController();controller=c;patch({loading:true});const timeout=setTimeout(()=>c.abort(),15000);
 const task=(async()=>{try{const r=await fetch('/api/v1/work/inbox',{credentials:'include',signal:c.signal});if(!r.ok)throw new Error('待办同步失败，请重试');const data=await r.json();if(n===generation&&epoch===priorEpoch)accept(data)}catch(e){if(n===generation){patch({error:e instanceof Error?e.message:'同步失败'});schedule()}}finally{clearTimeout(timeout);if(n===generation){flight=undefined;patch({loading:false})}}})();flight=task;return task;
}
function visible(){if(document.hidden){source?.close();source=undefined;clearTimeout(retry)}else void refreshWorkInbox().then(connect)}
export function startWorkInbox(key:string){if(owner===key)return;stopWorkInbox();if(!key)return;owner=key;document.addEventListener('visibilitychange',visible);void refreshWorkInbox().then(connect)}
export function stopWorkInbox(){owner='';generation++;source?.close();source=undefined;controller?.abort();clearTimeout(retry);flight=undefined;patch({snapshot:null,error:'',loading:false});if(typeof document!=='undefined')document.removeEventListener('visibilitychange',visible)}
export function inboxIntent(text:string){return /待办|待审核|待接单|待处理|待补充|待确认|要处理什么|该处理什么|哪些.{0,8}(超时|逾期)/.test(text)&&!/设计|话术/.test(text)}
export async function inboxItems(group:string,page:number,due:boolean,signal?:AbortSignal):Promise<Page>{const qs=new URLSearchParams({group,page:String(page),page_size:'12',due:due?'1':'0'});const r=await fetch('/api/v1/work/inbox/items?'+qs,{credentials:'include',signal});if(!r.ok)throw new Error('读取失败或已不在当前处理权限内');return r.json()}
