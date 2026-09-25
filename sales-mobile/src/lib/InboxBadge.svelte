<script lang="ts">
import {workInbox} from '$lib/workInbox';
export let count:number|undefined=undefined;
$: total=count??($workInbox.error||$workInbox.snapshot?.stale?0:$workInbox.snapshot?.total||0);
</script>
{#if Number.isSafeInteger(total)&&total>0}<span class="inbox-badge" aria-label={total+'件待办'}>{total}</span>{/if}
<style>.inbox-badge{position:absolute;right:2px;top:2px;display:inline-flex;align-items:center;justify-content:center;min-width:22px;height:22px;box-sizing:border-box;padding:0 5px;border-radius:999px;background:#c92d3b;color:#fff;font-size:14px;line-height:1;font-weight:700;box-shadow:0 0 0 2px #fff;pointer-events:none;z-index:2}</style>
