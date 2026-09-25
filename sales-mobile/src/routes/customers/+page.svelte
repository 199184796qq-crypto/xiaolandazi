<script lang="ts">
  import { onMount } from 'svelte';
  import { getSalesCustomers } from '$lib/api';
  import type { SalesCustomer } from '$lib/types';

  let customers: SalesCustomer[] = [];
  let search = '';
  let loading = true;

  onMount(async () => {
    try {
      customers = (await getSalesCustomers()).items || [];
    } finally {
      loading = false;
    }
  });

  $: keyword = search.trim().toLowerCase();
  $: filtered = keyword
    ? customers.filter((item) =>
        [item.display_name, item.username, item.phone, item.industry_name]
          .some((value) => String(value || '').toLowerCase().includes(keyword)),
      )
    : customers;
</script>

<section class="page-section top-space">
  <header class="mobile-header"><div><span class="eyebrow">我的客户</span><h1>客户列表</h1></div></header>
  <div class="field"><input bind:value={search} placeholder="搜索客户姓名、手机号、行业" /></div>

  {#if loading}
    <div class="empty-card">正在读取我的客户…</div>
  {:else}
    <div class="list-stack">
      {#each filtered as item}
        <article class="list-card">
          <div>
            <strong>{item.display_name || item.username}</strong>
            <p>{item.phone || '未留手机号'} · {item.industry_name || '未设置行业'}</p>
          </div>
          <b>{item.status}</b>
        </article>
      {:else}
        <div class="empty-card">当前范围内没有客户。</div>
      {/each}
    </div>
  {/if}
</section>
