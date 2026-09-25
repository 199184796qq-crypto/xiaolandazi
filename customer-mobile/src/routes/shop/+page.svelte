<script lang="ts">
  import { onMount } from 'svelte';
  import { getDeviceOffers, getMembershipOffers, getTimeCardOffers } from '$lib/api';
  import type { CatalogItem } from '$lib/types';

  let memberships: CatalogItem[] = [];
  let cards: CatalogItem[] = [];
  let devices: CatalogItem[] = [];
  let loading = true;

  onMount(async () => {
    try {
      const [m, t, d] = await Promise.all([
        getMembershipOffers(),
        getTimeCardOffers(),
        getDeviceOffers(),
      ]);
      memberships = m.items || [];
      cards = t.items || [];
      devices = d.items || [];
    } finally {
      loading = false;
    }
  });

  function money(item: CatalogItem) {
    const cents = item.current_price_cents ?? item.price_cents;
    return typeof cents === 'number' ? '¥' + (cents / 100).toFixed(0) : '查看详情';
  }
</script>

<section class="page-section top-space">
  <header class="mobile-header"><div><span class="eyebrow">商城</span><h1>会员 · 时长 · 设备</h1></div></header>
  <div class="segmented-summary">
    <span>会员 {memberships.length}</span><span>时长卡 {cards.length}</span><span>设备 {devices.length}</span>
  </div>

  {#if loading}
    <div class="empty-card">正在读取商城…</div>
  {:else}
    <div class="section-title"><div><span>会员方案</span><h2>按需要续费</h2></div></div>
    <div class="list-stack">
      {#each memberships.slice(0, 4) as item}
        <article class="list-card"><div><strong>{item.name}</strong><p>{item.description || '会员权益方案'}</p></div><b>{money(item)}</b></article>
      {:else}<div class="empty-card">暂无可购买会员方案</div>{/each}
    </div>

    <div class="section-title"><div><span>时长卡</span><h2>按量补充 AI 时长</h2></div></div>
    <div class="list-stack">
      {#each cards.slice(0, 6) as item}
        <article class="list-card"><div><strong>{item.name}</strong><p>{item.description || 'AI 时长卡'}</p></div><b>{money(item)}</b></article>
      {:else}<div class="empty-card">暂无可购买时长卡</div>{/each}
    </div>

    <div class="section-title"><div><span>设备</span><h2>直播搭子设备</h2></div></div>
    <div class="list-stack">
      {#each devices.slice(0, 4) as item}
        <article class="list-card"><div><strong>{item.name}</strong><p>{item.description || '直播辅助设备'}</p></div><b>{money(item)}</b></article>
      {:else}<div class="empty-card">暂无可购买设备</div>{/each}
    </div>
  {/if}
</section>
