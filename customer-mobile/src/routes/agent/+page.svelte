<script lang="ts">
  import { goto } from '$app/navigation';
  import { onMount } from 'svelte';

  onMount(() => {
    void goto('/', { replaceState: true });
  });
</script>

<svelte:head><title>小蓝直播搭子</title></svelte:head>

<div class="agent-redirect">正在打开智能体…</div>

<style>
  .agent-redirect{display:grid;min-height:70vh;place-items:center;color:#7f8aa0;font-size:13px}
</style>
