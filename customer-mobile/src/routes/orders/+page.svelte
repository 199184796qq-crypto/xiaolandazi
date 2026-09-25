<script lang="ts">
  import { onMount } from 'svelte';
  import { getOrders } from '$lib/api';
  import type { ShopOrder } from '$lib/types';

  let orders: ShopOrder[] = [];
  let loading = true;

  onMount(async () => {
    try {
      orders = (await getOrders()).items || [];
    } finally {
      loading = false;
    }
  });

  function amount(item: ShopOrder) {
    const cents = item.paid_amount_cents ?? item.total_amount_cents ?? 0;
    return '¥' + (cents / 100).toFixed(2);
  }
</script>

<section class="page-section top-space">
  <header class="mobile-header"><div><span class="eyebrow">订单</span><h1>订单与售后</h1></div></header>
  {#if loading}
    <div class="empty-card">正在读取订单…</div>
  {:else}
    <div class="list-stack">
      {#each orders as item}
        <article class="list-card">
          <div><strong>{item.order_no || '订单 #' + item.id}</strong><p>{item.status} · {item.created_at ? new Date(item.created_at).toLocaleDateString() : ''}</p></div>
          <b>{amount(item)}</b>
        </article>
      {:else}
        <div class="empty-card">还没有订单。需要会员、时长或设备时可以去商城看看。</div>
      {/each}
    </div>
  {/if}
</section>
