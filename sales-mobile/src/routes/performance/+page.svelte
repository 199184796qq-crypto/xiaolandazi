<script lang="ts">
  import { onMount } from 'svelte';
  import { getSalesPerformance, getSalesCommissionWallet, createSalesCommissionWithdrawal } from '$lib/api';
  import type { SalesCommissionDashboard, SalesPerformanceResponse } from '$lib/types';
  let period = new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit' }).format(new Date());
  let wallet: SalesCommissionDashboard | null = null;
  let performance: SalesPerformanceResponse | null = null;
  let loading = false, busy = false, confirming = false, error = '', notice = '', amount: number | undefined;
  let sequence = 0;
  const money = (cents: number) => '¥' + ((cents || 0) / 100).toFixed(2);
  const date = (v?: string) => v ? new Date(v).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' }) : '—';
  const status = (v: string) => ({ pending: '待解冻', available: '可提现', reviewing: '待审核', approved: '待打款', paid: '已打款', rejected: '已驳回', reversed: '已冲回' }[v] || v);
  async function load() {
    const token = ++sequence; loading = true; error = '';
    const [w, p] = await Promise.allSettled([getSalesCommissionWallet(period), getSalesPerformance(period)]);
    if (token !== sequence) return;
    if (w.status === 'fulfilled') wallet = w.value;
    else error = w.reason instanceof Error ? w.reason.message : '提成加载失败';
    if (p.status === 'fulfilled') performance = p.value;
    else performance = null;
    loading = false;
  }
  function prepare() {
    if (busy || !wallet) return;
    const cents = Math.round((amount || 0) * 100);
    if (!Number.isSafeInteger(cents) || cents <= 0 || cents > wallet.wallet.available_balance_cents) { error = '请输入不超过可提现余额的金额'; return; }
    error = ''; confirming = true;
  }
  async function withdraw() {
    if (busy || !confirming) return;
    busy = true; error = '';
    try { await createSalesCommissionWithdrawal(Math.round((amount || 0) * 100)); amount = undefined; confirming = false; notice = '申请已提交，等待财务审核和实际打款。'; await load(); }
    catch (e) { error = e instanceof Error ? e.message : '提交失败，请先刷新提现记录，勿重复提交'; }
    finally { busy = false; }
  }
  onMount(load);
</script>
<svelte:head><title>我的业绩与提成</title></svelte:head>
<section class="page-section top-space">
  <header class="mobile-header"><div><span class="eyebrow">我的业绩</span><h1>业绩与成交提成</h1></div><button class="refresh" disabled={loading || busy} on:click={load}>刷新</button></header>
  <label class="period">查看账期 <input type="month" bind:value={period} on:change={load} disabled={busy} /></label>
  {#if error}<p class="message error" role="alert">{error}</p>{/if}
  {#if notice}<p class="message success" role="status">{notice}</p>{/if}
  {#if loading}<p class="hint">正在同步业绩与提成…</p>{/if}
  {#if performance}<div class="metric-grid"><article class="metric-card"><span>本期净销售额</span><strong>{money(performance.totals.net_revenue_cents)}</strong></article><article class="metric-card"><span>本期成交订单</span><strong>{performance.totals.paid_order_count}</strong></article></div>{/if}
  {#if wallet}
    <div class="metric-grid"><article class="metric-card"><span>累计可提现余额</span><strong class:negative={wallet.wallet.available_balance_cents < 0}>{money(wallet.wallet.available_balance_cents)}</strong></article><article class="metric-card"><span>待解冻 / 审核中</span><strong>{money(wallet.wallet.frozen_balance_cents)}</strong></article></div>
    <p class="hint">成交后自动计提，提现日期按订单锁定的财务规则。账期筛选仅影响明细，钱包余额是累计值。退款应扣欠额需先抵扣。</p>
    <section class="panel"><h2>申请提现</h2><form on:submit|preventDefault={prepare}><label>金额（元）<input aria-label="提现金额" type="number" bind:value={amount} min="0.01" step="0.01" max={Math.max(0, wallet.wallet.available_balance_cents / 100)} disabled={busy || confirming} required /></label><button class="primary-action" disabled={busy || confirming || wallet.wallet.available_balance_cents <= 0}>申请提现</button></form>
      {#if confirming}<div class="confirm" role="group" aria-label="确认提现"><p>确认申请 {money(Math.round((amount || 0) * 100))}？提交后冻结该金额，财务审核并实际打款后到账。</p><div><button disabled={busy} on:click={() => confirming = false}>取消</button><button class="confirm-primary" disabled={busy} on:click={withdraw}>{busy ? '提交中…' : '确认提交'}</button></div></div>{/if}
    </section>
    <section class="panel"><h2>{period} 提成明细</h2><p class="hint">最近 500 条，包含退款冲回。</p>{#each wallet.earnings as e (e.id)}<article class="entry"><header><b>{e.product_name || '订单收益'}</b><strong class:negative={e.amount_cents < 0}>{money(e.amount_cents)}</strong></header><small>{e.order_no}</small><p>{e.rule_name} · 版本 #{e.rule_version_id}</p><footer><span>{status(e.status)}</span><time>可提现：{date(e.available_at)}</time></footer></article>{:else}<p class="empty">本期暂无新规则产生的提成。</p>{/each}</section>
    <section class="panel"><h2>提现记录</h2>{#each wallet.withdrawals as w (w.id)}<article class="entry"><header><b>{status(w.status)}</b><strong>{money(w.amount_cents)}</strong></header><small>{w.withdrawal_no}</small><p>{date(w.requested_at)}</p>{#if w.reject_reason}<p class="negative">{w.reject_reason}</p>{/if}</article>{:else}<p class="empty">暂无提现申请。</p>{/each}</section>
  {/if}
</section>
<style>
  .refresh{border:1px solid #e1e6f3;background:#fff;padding:9px 14px;border-radius:12px;color:#5365d4}.period{display:flex;align-items:center;justify-content:space-between;color:#718096;font-size:14px}.period input,.panel input{border:1px solid #dfe5f0;border-radius:12px;padding:12px;min-width:0;background:#fff;color:#27324d}.metric-card strong{font-size:23px;overflow-wrap:anywhere;color:#535fd5}.hint{font-size:12px;line-height:1.7;color:#8992a4}.panel{margin-top:16px;padding:18px;background:#fff;border:1px solid #e4e9f5;border-radius:20px}.panel h2{font-size:18px;margin:0 0 12px}.panel form{display:grid;gap:12px}.panel label{display:grid;gap:8px;font-size:14px;color:#6e7a91}.entry{padding:14px 0;border-top:1px solid #edf0f6}.entry header{display:flex;justify-content:space-between;gap:10px;font-size:14px}.entry small,.entry p{display:block;font-size:11px;color:#939caf;margin:7px 0}.entry strong{color:#5365d4;white-space:nowrap}.entry footer{display:grid;gap:6px;font-size:11px;color:#8690a3}.entry footer span{color:#5477b8}.empty{font-size:13px;color:#9aa3b5;text-align:center;padding:20px 0}.negative,.entry .negative,.metric-card .negative{color:#cc5366}.message{padding:12px;border-radius:12px;font-size:13px;line-height:1.6}.error{background:#fff0f2;color:#c74d61}.success{background:#edf9f2;color:#258357}.confirm{margin-top:14px;padding:14px;border-radius:14px;background:#f0f3ff;font-size:13px;line-height:1.6}.confirm>div{display:flex;justify-content:flex-end;gap:10px}.confirm button{padding:9px 14px;border:0;border-radius:10px;background:#fff}.confirm .confirm-primary{background:#5967df;color:#fff}button:disabled{opacity:.5}
</style>
