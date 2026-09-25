<script lang="ts">
import {onDestroy} from 'svelte';
import {workInbox,categories,refreshWorkInbox,inboxItems,type Group,type Page} from '$lib/workInbox';
import InboxBadge from '$lib/InboxBadge.svelte';
export let request='';
let category='',department='',selected='',page=1,due=false,detail:Page|null=null,error='',loading=false,lastRequest='';
let controller:AbortController|undefined,serial=0,snapshotVersion='';
$: if($workInbox.snapshot?.version && snapshotVersion!==$workInbox.snapshot.version){snapshotVersion=$workInbox.snapshot.version;if(selected)void openGroup(selected,page)}
$: valid=!!$workInbox.snapshot&&!$workInbox.error&&!$workInbox.snapshot.stale;
$: all=valid?$workInbox.snapshot!.groups:[];
$: groups=all.filter(g=>g.count>0&&(!category||g.category===category)&&(!department||g.department.includes(department))&&(!due||g.due_count>0));
$: departments=[...new Set(all.filter(g=>g.count>0).map(g=>g.department))];
$: if(request!==lastRequest){lastRequest=request;category=categories.find(c=>request.includes(c.label))?.key||'';department=/维修|库存|仓储/.test(request)?'仓储':/运维/.test(request)?'运维':/财务/.test(request)?'财务':/销售|回访/.test(request)?'销售':'';due=/超时|逾期/.test(request);selected='';detail=null}
function chooseCategory(key:string){category=key;selected='';detail=null}
async function openGroup(key:string,next=1){controller?.abort();const n=++serial;selected=key;page=next;detail=null;error='';loading=true;const c=new AbortController();controller=c;try{const v=await inboxItems(key,page,due,c.signal);if(n===serial){if(page>Math.max(1,v.total_pages)){void openGroup(key,Math.max(1,v.total_pages));return}detail=v}}catch(e){if(!c.signal.aborted&&n===serial)error=e instanceof Error?e.message:'读取失败'}finally{if(n===serial)loading=false}}
function amount(key:string){return all.filter(g=>g.category===key&&(!department||g.department.includes(department))).reduce((n,g)=>n+(due?g.due_count:g.count),0)}
function supportLink(g:Group){return g.topic==='support'?'/support':''}
onDestroy(()=>{serial++;controller?.abort()});
</script>
<section class="mobile-work-inbox">
<header><h2>我的待办</h2><button on:click={refreshWorkInbox} disabled={$workInbox.loading}>刷新</button></header>
{#if $workInbox.error}<p class="error" role="alert">{$workInbox.error}，未同步不代表没有待办。</p>{:else if !$workInbox.snapshot}<p>正在同步待办…</p>{:else}
<nav aria-label="待办分类"><button class:active={!category} on:click={()=>chooseCategory('')}>全部</button>{#each categories as c}<button class:active={category===c.key} on:click={()=>chooseCategory(c.key)}>{c.label}<InboxBadge count={amount(c.key)}/></button>{/each}</nav>
<label>业务 <select bind:value={department} on:change={()=>{selected='';detail=null}}><option value="">全部</option>{#each departments as d}<option value={d}>{d}</option>{/each}</select></label>
{#if due}<p>仅展示已到明确计划时间的事项；没有办理期限的业务不虚构超时。</p>{/if}
<div class="inbox-group-list">{#each groups as g}<button on:click={()=>openGroup(g.key)}><span>{g.title}</span><small>{g.department} · {g.shared_queue?'岗位共享待办':'当前可处理'}</small><InboxBadge count={due?g.due_count:g.count}/></button>{/each}</div>
{#if !groups.length}<p>当前已接入的业务中，没有符合条件的待办。</p>{/if}
{#if selected}<section class="inbox-detail"><h3>单据明细</h3>{#if loading}<p>读取中…</p>{:else if error}<p class="error">{error}</p>{:else if detail}{#each detail.items as item}<article><strong>{item.title}</strong><small>{item.reference}</small>{#if supportLink(detail.group)}<a href={supportLink(detail.group)}>打开运维协助</a>{:else}<p>请在电脑端“{detail.group.title}”核对办理。</p>{/if}</article>{/each}<footer><span>共{detail.total}条 · {page}/{detail.total_pages}页</span><button disabled={page<=1} on:click={()=>openGroup(selected,page-1)}>上一页</button><button disabled={page>=detail.total_pages} on:click={()=>openGroup(selected,page+1)}>下一页</button></footer>{/if}</section>{/if}
<p class="help">查看提醒不会结束任务，也不会自动审批、付款或更改库存。</p>
{/if}
</section>
<style>
.mobile-work-inbox{padding:16px;overflow:auto;font-size:18px;color:#253852}.mobile-work-inbox header{display:flex;align-items:center;justify-content:space-between}.mobile-work-inbox h2,.mobile-work-inbox h3{font-size:18px;margin:0}.mobile-work-inbox button{position:relative;min-height:42px;padding:8px 30px 8px 12px;border:1px solid #cad9ed;border-radius:9px;background:#fff;color:#28598e;font-size:16px}.mobile-work-inbox button.active{background:#edf5ff;border-color:#70a5ed}.mobile-work-inbox button:focus-visible,.mobile-work-inbox button:hover{outline:none;box-shadow:0 0 0 3px #4285ff20}.mobile-work-inbox nav{display:flex;flex-wrap:wrap;gap:8px;margin:14px 0}.inbox-group-list{display:grid;gap:9px;margin-top:14px}.inbox-group-list button{text-align:left;font-size:18px}.mobile-work-inbox small{display:block;font-size:14px;color:#667c94;margin-top:6px}.mobile-work-inbox select{font-size:16px;max-width:65%;padding:7px}.mobile-work-inbox p{font-size:16px;line-height:1.5}.mobile-work-inbox .error{color:#b33340}.inbox-detail{margin-top:16px}.inbox-detail article{padding:12px 0;border-bottom:1px solid #e2e9f4}.inbox-detail footer{display:flex;flex-wrap:wrap;gap:6px;font-size:14px;align-items:center;margin-top:10px}.help{color:#687e93}
</style>
