<script lang="ts">
  import { onMount } from 'svelte';
  import {
    createWechatRecharge,
    getWechatRefundWallet,
    createWechatCashRefund,
    queryWechatCashRefund,
    getWechatRecharge,
    getWechatPaymentStatus,
    prepayWechatRecharge,
    queryWechatRecharge,
    createCustomerWalletWithdrawal,
    createReferralWithdrawal,
    getCustomerBeanWallet,
    getCustomerWithdrawals,
    getFinanceDashboard,
    getReferralWallet,
    purchaseCustomerBeans,
  } from '$lib/api';
  import type {
    BeanWalletDashboard,
    FinanceDashboard,
    ReferralWalletDashboard,
    WithdrawalRequest,
    RechargeRecord,
    WechatRefundWallet,
  } from '$lib/types';
  import { invokeWechatPayment, rechargeAmountCents, rechargeIsPaid } from '$lib/wechatPay';
  import { refundAmountCents, refundDraft, clearRefundDraft, savedRefundAmount } from '$lib/wechatRefund';
  import { readRefundAwareWallet, refundNeedsSync, refundProgressMessage, type RefundProgress } from '../../../../shared/wechatRefundSync';

  type WalletAction = 'recharge' | 'cash' | 'reward' | 'commission' | 'beans';
  type RecordTab = 'funds' | 'beans' | 'recharge' | 'payment' | 'purchase' | 'refund' | 'withdrawal';

  let dashboard: FinanceDashboard | null = null;
  let beanWallet: BeanWalletDashboard | null = null;
  let referralWallet: ReferralWalletDashboard | null = null;
  let customerWithdrawals: WithdrawalRequest[] = [];
  let refundWallet: WechatRefundWallet | null = null;
  let walletSyncWarning = '';
  let cashRefundWarning = '';
  let acceptedCashRefund: RefundProgress | null = null;
  let walletReadSequence = 0;
  let loading = true;
  let error = '';
  let activeTab: RecordTab = 'funds';
  let action: WalletAction | null = null;
  let amount = '';
  let submitting = false;
  let actionError = '';
  let actionMessage = '';
  let recharge: RechargeRecord | null = null;
  let rechargeKey = '';
  let draftCents = 0;
  let wechatEnabled = false;
  let inWechat = false;
  let mounted = true;

  onMount(() => {
    inWechat = /micromessenger/i.test(navigator.userAgent);
    void initializeWallet();
    const refundTimer = setInterval(() => {
      if (refundNeedsSync(acceptedCashRefund) || refundWallet?.records.some(refundNeedsSync) || walletSyncWarning) refreshWalletOnReturn();
    }, 5000);
    document.addEventListener('visibilitychange', refreshWalletOnReturn);
    window.addEventListener('focus', refreshWalletOnReturn);
    return () => {
      mounted = false; clearInterval(refundTimer);
      document.removeEventListener('visibilitychange', refreshWalletOnReturn);
      window.removeEventListener('focus', refreshWalletOnReturn);
    };
  });

  function refreshWalletOnReturn() { if (!document.hidden && !loading && !submitting) void loadWallet(); }

  async function initializeWallet() {
    await loadWallet();
    try {
      wechatEnabled = (await getWechatPaymentStatus()).enabled;
      const params = new URLSearchParams(window.location.search);
      const id = Number(params.get('recharge') || 0);
      if (Number.isSafeInteger(id) && id > 0) {
        await resumeRecharge(id);
        if (params.get('wechat_auth') === 'failed') actionError = '微信授权未完成，请重新点击充值。';
        if (params.get('wechat_auth') === 'ready' && recharge && !rechargeIsPaid(recharge)) await payRecharge();
      }
    } catch (value) { actionError = value instanceof Error ? value.message : '微信充值准备失败'; }
  }

  function saveRechargeDraft(cents: number) {
    if (draftCents !== cents || !rechargeKey) {
      try {
        const cached = JSON.parse(sessionStorage.getItem('wechat-recharge-draft') || 'null');
        rechargeKey = cached?.cents === cents && typeof cached?.key === 'string' ? cached.key : idempotencyKey();
      } catch { rechargeKey = idempotencyKey(); }
      draftCents = cents;
      try { sessionStorage.setItem('wechat-recharge-draft', JSON.stringify({ cents, key: rechargeKey })); } catch { /* storage is optional */ }
    }
  }

  async function resumeRecharge(id: number) {
    if (submitting) return;
    submitting = true;
    action = 'recharge'; actionError = ''; actionMessage = '';
    try {
      recharge = await getWechatRecharge(id);
      amount = (recharge.requested_amount_cents / 100).toFixed(2);
      history.replaceState(history.state, '', `/wallet?recharge=${id}`);
      if (wechatEnabled) await checkRecharge(false);
    } catch (value) { recharge = null; actionError = value instanceof Error ? value.message : '读取充值单失败'; }
    finally { submitting = false; }
  }

  async function checkRecharge(manual = true) {
    if (!recharge || (manual && submitting)) return;
    if (manual) { submitting = true; actionError = ''; }
    try {
      const result = await queryWechatRecharge(recharge.id);
      recharge = result.recharge;
      if (rechargeIsPaid(recharge)) {
        actionMessage = `充值成功，${money(recharge.credited_amount_cents)} 已到账。`;
        rechargeKey = ''; draftCents = 0;
        try { sessionStorage.removeItem('wechat-recharge-draft'); } catch { /* storage is optional */ }
        activeTab = 'recharge';
        await loadWallet();
      } else {
        actionMessage = result.trade_state === 'CLOSED' ? '上次支付已关闭，可继续支付此充值单。' : '尚未确认微信到账，请稍后查询；不要重复付款。';
      }
    } finally { if (manual) submitting = false; }
  }

  async function queryRechargeResult() {
    try { await checkRecharge(); } catch (value) { actionError = value instanceof Error ? value.message : '到账查询失败，请稍后重试'; }
  }

  async function payRecharge() {
    if (submitting) return;
    submitting = true; actionError = ''; actionMessage = '';
    try {
      if (!wechatEnabled) throw new Error('微信支付暂不可用，请稍后再试。');
      if (!inWechat) throw new Error('请在微信中打开此钱包页面进行充值。');
      if (!recharge || rechargeIsPaid(recharge)) {
        const cents = rechargeAmountCents(amount);
        if (cents === null) throw new Error('请输入正确的充值金额，最多两位小数，最低0.01元。');
        saveRechargeDraft(cents);
        recharge = await createWechatRecharge(cents, rechargeKey);
        history.replaceState(history.state, '', `/wallet?recharge=${recharge.id}`);
      }
      if (rechargeIsPaid(recharge)) { await checkRecharge(false); return; }
      const response = await prepayWechatRecharge(recharge.id);
      recharge = response.recharge;
      if (rechargeIsPaid(recharge)) { await checkRecharge(false); return; }
      if (response.authorization_required && response.authorization_url) { window.location.assign(response.authorization_url); return; }
      if (!response.payment_params) throw new Error('当前充值单不可支付，请先查询到账结果。');
      const bridgeResult = await invokeWechatPayment(response.payment_params);
      await checkRecharge(false);
      // Brief bounded polling handles notification delivery lag. Callback and
      // query both verify server-side; bridge success alone never credits cash.
      for (let attempt = 0; bridgeResult === 'ok' && recharge && !rechargeIsPaid(recharge) && attempt < 4 && mounted; attempt++) {
        await new Promise((resolve) => setTimeout(resolve, 2000));
        if (mounted) await checkRecharge(false);
      }
      if (recharge && !rechargeIsPaid(recharge) && bridgeResult === 'cancel') actionMessage = '已取消微信收银台，未确认到账；充值单保留，可稍后继续支付。';
    } catch (value) { actionError = value instanceof Error ? value.message : '充值失败，请查询到账结果后重试'; }
    finally { submitting = false; }
  }

  async function loadWallet() {
    const sequence = ++walletReadSequence;
    loading = true;
    const [wallet, extras] = await Promise.all([
      readRefundAwareWallet(() => getFinanceDashboard(100), getWechatRefundWallet, dashboard),
      Promise.allSettled([getCustomerBeanWallet(), getReferralWallet(100), getCustomerWithdrawals('all')]),
    ]);
    if (!mounted || sequence !== walletReadSequence) return;
    dashboard = wallet.dashboard;
    refundWallet = wallet.refunds;
    if (extras[0].status === 'fulfilled') beanWallet = extras[0].value;
    if (extras[1].status === 'fulfilled') referralWallet = extras[1].value;
    if (extras[2].status === 'fulfilled') customerWithdrawals = extras[2].value.items || [];
    walletSyncWarning = wallet.warning || (extras.some(r => r.status === 'rejected') ? '部分账户记录正在同步，页面会自动重试。' : '');
    const current = acceptedCashRefund
      ? wallet.refunds?.records.find(r => r.id === acceptedCashRefund?.id)
      : wallet.refunds?.records.find(refundNeedsSync);
    if (current) {
      acceptedCashRefund = current;
      if (action === 'cash') actionMessage = refundProgressMessage(current);
      cashRefundWarning = '';
    }
    loading = false;
  }

  function money(cents: number) {
    return new Intl.NumberFormat('zh-CN', {
      style: 'currency',
      currency: 'CNY',
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format((cents || 0) / 100).replace('CN¥', '¥');
  }

  function date(value?: string) {
    if (!value) return '—';
    return new Date(value).toLocaleString('zh-CN', {
      month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
    });
  }

  function businessLabel(value: string) {
    const labels: Record<string, string> = {
      recharge: '账户充值', membership_purchase: '购买会员', membership_renewal: '会员续费',
      time_card_purchase: '购买时长卡', bean_purchase: '购买小蓝豆', refund: '退款',
      wechat_refund_hold: '原路退回冻结', wechat_refund_release: '退回关闭解冻',
      manual_adjustment: '人工调账', referral_commission: '推荐返佣',
      referral_commission_reversal: '返佣冲正', reward: '活动奖励',
    };
    return labels[value] || value || '资金变动';
  }

  function statusLabel(value: string) {
    const labels: Record<string, string> = {
      pending: '待处理', pending_approval: '待财务审核', reviewing: '审核中', approved: '已审核',
      rejected: '已驳回', paid: '已支付', success: '成功', completed: '已完成',
      refunded: '已退款', partially_refunded: '部分退款', failed: '失败', cancelled: '已取消',
      queued: '待微信受理', processing: '原路退回中', abnormal: '退款异常，请联系客服', closed: '已关闭，金额已解冻',
    };
    return labels[value] || value || '—';
  }

  function orderLabel(value: string) {
    if (value === 'membership') return '会员订阅';
    if (value === 'time_card') return 'AI 时长卡';
    if (value === 'device') return '直播设备';
    return value || '商城消费';
  }

  function openAction(next: WalletAction) {
    action = next;
    amount = next === 'cash' ? savedRefundAmount() : '';
    if (next === 'recharge' && recharge && !rechargeIsPaid(recharge)) amount = (recharge.requested_amount_cents / 100).toFixed(2);
    else if (next === 'recharge') { recharge = null; history.replaceState(history.state, '', '/wallet'); }
    actionError = '';
    actionMessage = '';
    cashRefundWarning = '';
    if (next === 'cash') {
      if (acceptedCashRefund && refundNeedsSync(acceptedCashRefund)) {
        amount = (acceptedCashRefund.amount_cents / 100).toFixed(2);
        actionMessage = refundProgressMessage(acceptedCashRefund);
      } else acceptedCashRefund = null;
    }
  }

  function closeAction() {
    if (submitting) return;
    action = null;
  }

  function availableBalance(kind: WalletAction) {
    if (!dashboard) return 0;
    if (kind === 'cash') return refundWallet?.refundable_cents || 0;
    if (kind === 'reward') return dashboard.reward_balance_cents;
    if (kind === 'commission') return dashboard.commission_balance_cents;
    return 0;
  }

  function actionTitle() {
    if (action === 'recharge') return '微信充值';
    if (action === 'beans') return '购买小蓝豆';
    if (action === 'cash') return '充值余额原路退回';
    if (action === 'reward') return '奖励余额提现';
    return '返佣余额提现';
  }

  function idempotencyKey() {
    const random = typeof crypto !== 'undefined' && 'randomUUID' in crypto
      ? crypto.randomUUID()
      : `${Date.now()}-${Math.random().toString(36).slice(2)}`;
    return `mobile-bean-${random}`;
  }

  async function submitAction() {
    if (!action || !dashboard || submitting) return;
    if (action === 'recharge') { await payRecharge(); return; }
    if (action === 'cash') { await submitCashRefund(); return; }
    const yuan = Number(amount);
    if (!Number.isFinite(yuan) || yuan <= 0) {
      actionError = '请输入正确的金额';
      return;
    }
    const cents = Math.round(yuan * 100);
    if (action === 'beans' && beanWallet && cents < beanWallet.settings.minimum_purchase_cents) {
      actionError = `最低购买金额为 ${money(beanWallet.settings.minimum_purchase_cents)}`;
      return;
    }
    if (action !== 'beans' && cents > availableBalance(action)) {
      actionError = '提现金额不能超过当前可提现余额';
      return;
    }

    submitting = true;
    actionError = '';
    actionMessage = '';
    try {
      if (action === 'beans') {
        const result = await purchaseCustomerBeans(cents, idempotencyKey());
        actionMessage = `购买成功，${result.credited_beans.toLocaleString('zh-CN')} 小蓝豆已到账。`;
        activeTab = 'beans';
      } else if (action === 'commission') {
        await createReferralWithdrawal(cents);
        actionMessage = '返佣提现申请已提交，等待财务审核。';
        activeTab = 'withdrawal';
      } else {
        await createCustomerWalletWithdrawal(action, cents);
        actionMessage = '提现申请已提交，等待财务审核。';
        activeTab = 'withdrawal';
      }
      amount = '';
      await loadWallet();
    } catch (value) {
      actionError = value instanceof Error ? value.message : '提交失败，请稍后重试';
    } finally {
      submitting = false;
    }
  }

  async function submitCashRefund() {
    if (acceptedCashRefund) { await checkCashRefund(acceptedCashRefund.id); return; }
    let draft: ReturnType<typeof refundDraft>;
    try { draft = refundDraft(refundAmountCents(String(amount)), refundWallet?.refundable_cents || 0); }
    catch (value) { cashRefundWarning = value instanceof Error ? value.message : '请输入正确的退回金额'; return; }
    submitting = true; actionError = ''; actionMessage = ''; cashRefundWarning = '';
    try {
      // Always retry the persisted key: the server may have frozen a request
      // even if its HTTP response was lost (current eligible balance is lower).
      const result = await createWechatCashRefund(draft.cents, draft.key);
      acceptedCashRefund = result;
      try { clearRefundDraft(); } catch { /* Storage cannot invalidate an accepted refund. */ }
      activeTab = 'refund';
      actionMessage = refundProgressMessage(result);
      await loadWallet();
    } catch { cashRefundWarning = '退回结果暂未获取，请先刷新退款记录核验。原请求标识已保留，勿另发一笔。'; walletSyncWarning = '退回状态待核验，页面会自动同步记录。'; }
    finally { submitting = false; }
  }

  async function checkCashRefund(id: number) {
    if (submitting) return;
    submitting = true;
    try {
      const result = await queryWechatCashRefund(id);
      acceptedCashRefund = result;
      if (action === 'cash') actionMessage = refundProgressMessage(result);
      cashRefundWarning = '';
      await loadWallet();
    }
    catch { cashRefundWarning = '暂时无法核验退回进度，不代表退款失败，请勿重复提交。'; walletSyncWarning = '退回状态正在同步，页面会自动重试。'; }
    finally { submitting = false; }
  }

  $: withdrawals = [
    ...customerWithdrawals,
    ...(referralWallet?.withdrawals || []),
  ].sort((a, b) => new Date(b.requested_at).getTime() - new Date(a.requested_at).getTime());
  $: beanPreview = beanWallet && Number(amount) > 0
    ? Math.floor(Number(amount) * beanWallet.settings.purchase_beans_per_yuan)
    : 0;
</script>

<svelte:head><title>我的钱包</title></svelte:head>

<section class="wallet-page top-space">
  <header class="wallet-hero">
    <div><span>MY WALLET</span><h1>我的钱包</h1><p>资金、奖励与小蓝豆统一管理</p></div>
    <button aria-label="刷新钱包" disabled={loading} on:click={loadWallet}>↻</button>
  </header>

  {#if loading && !dashboard}
    <div class="wallet-state">正在读取钱包数据…</div>
  {:else if error && !dashboard}
    <div class="wallet-state error-state">{error}<button on:click={loadWallet}>重新加载</button></div>
  {:else if dashboard}
    {#if error}<p class="page-error">{error}</p>{/if}

    <div class="balance-rail" aria-label="钱包余额，可左右滑动">
      <article class="balance-card cash-card">
        <header><i>¥</i><div><b>现金余额</b><small>充值、购买等支付场景</small></div><span>•••</span></header>
        <strong>{money(dashboard.cash_balance_cents)}</strong>
        <div class="card-art wallet-art"><i></i></div>
        <p>可退充值本金 {refundWallet ? money(refundWallet.refundable_cents) : '核验中'} · 冻结 {refundWallet ? money(refundWallet.frozen_cents) : '核验中'}</p>
        <footer><button class="primary" on:click={() => openAction('recharge')}>充值 ›</button><button on:click={() => openAction('cash')}>余额退回</button></footer>
      </article>

      <article class="balance-card bean-card">
        <header><i>豆</i><div><b>小蓝豆</b><small>非直播 AI 与付费服务</small></div><span>•••</span></header>
        <strong>{(beanWallet?.wallet.available_beans ?? dashboard.bean_balance).toLocaleString('zh-CN')}<em> 豆</em></strong>
        {#if (beanWallet?.wallet.frozen_beans ?? dashboard.bean_frozen) > 0}<p>冻结 {(beanWallet?.wallet.frozen_beans ?? dashboard.bean_frozen).toLocaleString('zh-CN')} 豆</p>{/if}
        <div class="card-art bean-art"><i></i><i></i><i></i></div>
        <footer><button class="primary" disabled={!beanWallet?.settings.enabled} on:click={() => openAction('beans')}>购买小蓝豆 ›</button></footer>
      </article>

      <article class="balance-card reward-card">
        <header><i>礼</i><div><b>奖励余额</b><small>完成任务、活动获得</small></div><span>•••</span></header>
        <strong>{money(dashboard.reward_balance_cents)}</strong>
        <div class="card-art gift-art">✦</div>
        <footer><button class="primary" on:click={() => openAction('reward')}>提现 ›</button></footer>
      </article>

      <article class="balance-card commission-card">
        <header><i>佣</i><div><b>返佣余额</b><small>订单返佣、推广收益</small></div><span>•••</span></header>
        <strong>{money(dashboard.commission_balance_cents)}</strong>
        {#if dashboard.commission_frozen_cents > 0}<p>冻结 {money(dashboard.commission_frozen_cents)}</p>{/if}
        <div class="card-art coin-art"><i></i><i></i><i></i></div>
        <footer><button class="primary" on:click={() => openAction('commission')}>提现 ›</button></footer>
      </article>
    </div>

    <section class="wallet-summary">
      <div><span>总资金余额</span><b>{money(dashboard.total_balance_cents)}</b></div>
      <div><span>本月消费</span><b>{money(dashboard.month_spent_cents)}</b></div>
      <div><span>可用 AI 时长</span><b>{(dashboard.available_seconds / 3600).toFixed(2)} 小时</b></div>
    </section>

    <section class="record-panel">
      <header><div><span>ACCOUNT HISTORY</span><h2>账户记录</h2></div><a href="/orders">商城订单 ›</a></header>
      <nav class="record-tabs" aria-label="账户记录分类">
        <button class:active={activeTab === 'funds'} on:click={() => activeTab = 'funds'}>资金流水</button>
        <button class:active={activeTab === 'beans'} on:click={() => activeTab = 'beans'}>小蓝豆</button>
        <button class:active={activeTab === 'recharge'} on:click={() => activeTab = 'recharge'}>充值</button>
        <button class:active={activeTab === 'payment'} on:click={() => activeTab = 'payment'}>支付</button>
        <button class:active={activeTab === 'purchase'} on:click={() => activeTab = 'purchase'}>消费</button>
        <button class:active={activeTab === 'refund'} on:click={() => activeTab = 'refund'}>退款</button>
        <button class:active={activeTab === 'withdrawal'} on:click={() => activeTab = 'withdrawal'}>提现</button>
      </nav>

      <div class="record-list">
        {#if activeTab === 'funds'}
          {#each dashboard.ledger as item}
            <article><i class:out={item.direction !== 'credit'}>{item.direction === 'credit' ? '入' : '出'}</i><div><b>{businessLabel(item.business_type)}</b><small>{item.reason || item.order_no || '钱包余额变动'} · {date(item.occurred_at)}</small></div><strong class:minus={item.direction !== 'credit'}>{item.direction === 'credit' ? '+' : '-'}{money(item.amount_cents)}</strong></article>
          {:else}<p class="record-empty">暂无资金流水</p>{/each}
        {:else if activeTab === 'beans'}
          {#each beanWallet?.ledger || [] as item}
            <article><i class:out={item.available_delta < 0}>豆</i><div><b>{businessLabel(item.business_type)}</b><small>{item.reason || '小蓝豆余额变动'} · {date(item.created_at)}</small></div><strong class:minus={item.available_delta < 0}>{item.available_delta > 0 ? '+' : ''}{item.available_delta.toLocaleString('zh-CN')}</strong></article>
          {:else}<p class="record-empty">暂无小蓝豆流水</p>{/each}
        {:else if activeTab === 'recharge'}
          {#each dashboard.recharges as item}
            <article><i>充</i><div><b>{item.recharge_no}</b><small>{item.payment_method === 'wechat_jsapi' && item.status === 'pending' ? '待微信支付' : statusLabel(item.status)} · {date(item.paid_at || item.created_at)}</small>{#if item.payment_method === 'wechat_jsapi' && item.status === 'pending'}<button type="button" disabled={submitting} on:click={() => resumeRecharge(item.id)}>继续支付 / 查询到账</button>{/if}</div><strong>{money(item.credited_amount_cents || item.requested_amount_cents)}</strong></article>
          {:else}<p class="record-empty">暂无充值记录</p>{/each}
        {:else if activeTab === 'payment'}
          {#each dashboard.payments as item}
            <article><i class="out">付</i><div><b>{item.order_no || item.payment_no}</b><small>{statusLabel(item.status)} · {date(item.paid_at || item.created_at)}</small></div><strong class="minus">-{money(item.paid_amount_cents)}</strong></article>
          {:else}<p class="record-empty">暂无支付记录</p>{/each}
        {:else if activeTab === 'purchase'}
          {#each dashboard.purchases as item}
            <article><i class="out">购</i><div><b>{orderLabel(item.order_type)}</b><small>{item.order_no} · {statusLabel(item.status)} · {date(item.paid_at || item.created_at)}</small></div><strong class="minus">-{money(item.paid_amount_cents)}</strong></article>
          {:else}<p class="record-empty">暂无消费记录</p>{/each}
        {:else if activeTab === 'refund'}
          {#each refundWallet?.records || [] as item}
            <article><i class="out">退</i><div><b>充值本金退回 · {statusLabel(item.status)}</b><small>{item.refund_no} · {date(item.created_at)}</small><small>已退 {money(item.refunded_cents)} · 冻结 {money(item.frozen_cents)} · 解冻 {money(item.released_cents)}</small>
              {#each item.items as part}<small>{part.recharge_no}：{statusLabel(part.status)} {part.received_account} {part.message}</small>{/each}
              {#if item.frozen_cents > 0}<button disabled={submitting} on:click={() => checkCashRefund(item.id)}>查询退回进度</button>{/if}
            </div><strong class="minus">-{money(item.amount_cents)}</strong></article>
          {/each}
          {#each dashboard.refunds as item}
            <article><i>退</i><div><b>{item.refund_no}</b><small>{item.reason || statusLabel(item.status)} · {date(item.processed_at || item.created_at)}</small></div><strong>+{money(item.refund_amount_cents)}</strong></article>
          {/each}
          {#if !refundWallet?.records.length && !dashboard.refunds.length}<p class="record-empty">暂无退款记录</p>{/if}
        {:else}
          {#each withdrawals as item}
            <article><i class="out">提</i><div><b>{item.withdrawal_no}</b><small>{statusLabel(item.status)} · {date(item.requested_at)}</small></div><strong class="minus">-{money(item.amount_cents)}</strong></article>
          {:else}<p class="record-empty">暂无提现记录</p>{/each}
        {/if}
      </div>
    </section>
  {:else}
    <div class="wallet-state">钱包数据暂未读取，请稍后重试。<button on:click={loadWallet}>重新加载</button></div>
  {/if}
  {#if walletSyncWarning}<p class="wallet-sync-warning" role="status">{walletSyncWarning}</p>{/if}
</section>

{#if action && dashboard}
  <div class="sheet-layer">
    <button class="sheet-backdrop" aria-label="关闭" on:click={closeAction}></button>
    <div class="action-sheet" role="dialog" aria-modal="true" aria-label={actionTitle()}>
      <header><div><span>钱包服务</span><h2>{actionTitle()}</h2></div><button on:click={closeAction}>×</button></header>
      {#if action === 'recharge'}
        <p>输入充值金额，点击充值后使用微信支付。服务器确认到账后自动增加现金余额，无需财务审核。</p>
        {#if !inWechat}<p>请将此页面在微信中打开后充值。</p>{/if}
        {#if recharge}<div class="sheet-info"><span>充值单 {recharge.recharge_no}</span><b>{rechargeIsPaid(recharge) ? '已到账' : '待微信支付'}</b></div>{/if}
      {:else if action === 'beans' && beanWallet}
        <p>小蓝豆用于录音转文字、非直播 AI 和付费运维协助等服务。</p>
        <div class="sheet-info"><span>当前兑换比例</span><b>1 元 = {beanWallet.settings.purchase_beans_per_yuan} 豆</b></div>
      {:else if action === 'cash'}
        <p>仅退回未消费的微信充值本金，消费优先扣充值本金。赠送、奖励与返佣不包含在内；超出微信退款期限或来源不明的款项请联系客服。</p>
        <p>资金退回原付款人的微信支付账户或银行卡，不能指定新账户；他人代付将退给代付人。处理中金额冻结，到账以微信结果为准。</p>
        <div class="sheet-info"><span>当前可退本金</span><b>{money(refundWallet?.refundable_cents || 0)}</b></div>
        {#if !refundWallet}<p class="sheet-error">可退本金暂无法核验，请刷新或联系客服。</p>{/if}
      {:else}
        <div class="sheet-info"><span>当前可提现</span><b>{money(availableBalance(action))}</b></div>
      {/if}

      <form on:submit|preventDefault={submitAction}>
        <label><span>{action === 'beans' ? '购买金额（元）' : action === 'recharge' ? '充值金额（元）' : action === 'cash' ? '退回金额（元）' : '提现金额（元）'}</span><input bind:value={amount} type="text" inputmode="decimal" placeholder="0.00" disabled={submitting || (action === 'recharge' && recharge !== null) || (action === 'cash' && acceptedCashRefund !== null)} required /></label>
        {#if action === 'beans' && beanWallet}<div class="bean-preview"><span>预计到账</span><b>{beanPreview.toLocaleString('zh-CN')} 小蓝豆</b></div>{/if}
        {#if actionError}<p class="sheet-error">{actionError}</p>{/if}
        {#if actionMessage}<p class="sheet-success">{actionMessage}</p>{/if}
        {#if action === 'cash' && cashRefundWarning}<p class="wallet-sync-warning" role="status">{cashRefundWarning}</p>{/if}
        {#if action === 'cash' && acceptedCashRefund}
          <button type="button" class="submit-action" disabled={submitting} on:click={() => acceptedCashRefund && (refundNeedsSync(acceptedCashRefund) ? checkCashRefund(acceptedCashRefund.id) : closeAction())}>{submitting ? '查询中…' : refundNeedsSync(acceptedCashRefund) ? '查询退回进度' : '完成'}</button>
        {:else if action === 'recharge' && recharge && rechargeIsPaid(recharge)}
          <button type="button" class="submit-action" on:click={() => openAction('recharge')}>再充一笔</button>
        {:else}
          <button class="submit-action" disabled={submitting || (action === 'cash' && !refundWallet) || (action === 'recharge' && (!wechatEnabled || !inWechat))}>{submitting ? '处理中…' : action === 'beans' ? '确认购买' : action === 'recharge' ? recharge ? '继续微信支付' : '充值' : action === 'cash' ? '确认原路退回' : '提交提现申请'}</button>
        {/if}
        {#if action === 'recharge' && recharge && !rechargeIsPaid(recharge)}<button type="button" disabled={submitting} on:click={queryRechargeResult}>查询到账结果</button>{/if}
      </form>
    </div>
  </div>
{/if}

<style>
  .wallet-sync-warning{margin:12px 20px;padding:10px 12px;border-radius:12px;color:#5d709b;background:#eef3ff;font-size:11px;line-height:1.6}.action-sheet .wallet-sync-warning{margin:0}
  .wallet-page{min-height:100vh;padding:22px 0 38px;overflow:hidden;background:radial-gradient(circle at 86% 3%,rgba(126,151,255,.2),transparent 25%),linear-gradient(180deg,#f7f9ff,#f4f7fd)}
  .wallet-hero{display:flex;align-items:flex-start;justify-content:space-between;padding:0 20px 18px}.wallet-hero span,.record-panel header span{color:#7e8ba7;font-size:9px;font-weight:900;letter-spacing:.13em}.wallet-hero h1{margin:4px 0 3px;color:#142039;font-size:27px}.wallet-hero p{margin:0;color:#8994a8;font-size:11px}.wallet-hero>button{display:grid;width:39px;height:39px;place-items:center;border:1px solid #dfe5f1;border-radius:13px;color:#5366d9;background:rgba(255,255,255,.9);font-size:20px}
  .wallet-state{margin:20px;padding:34px 20px;border:1px solid #e3e8f3;border-radius:22px;color:#7c879c;text-align:center;background:#fff}.error-state{color:#c34d59}.error-state button{display:block;margin:15px auto 0;border:0;color:#5365d7;background:none;font-weight:850}.page-error{margin:0 20px 12px;padding:10px 12px;border-radius:12px;color:#bd4b57;background:#fff0f2;font-size:11px}
  .balance-rail{display:flex;gap:12px;padding:0 20px 12px;overflow-x:auto;scroll-snap-type:x mandatory;scroll-padding-inline:20px;scrollbar-width:none}.balance-rail::-webkit-scrollbar{display:none}.balance-card{position:relative;display:flex;flex:0 0 84%;min-height:224px;flex-direction:column;padding:17px;overflow:hidden;border:1px solid #d9e3f7;border-radius:23px;scroll-snap-align:start;background:linear-gradient(145deg,#fff,#edf4ff);box-shadow:0 13px 30px rgba(57,77,139,.1)}.balance-card::after{content:"";position:absolute;right:-54px;bottom:-73px;width:230px;height:150px;border-radius:50%;background:rgba(114,157,255,.13);transform:rotate(-15deg)}.bean-card{background:linear-gradient(145deg,#fff,#edf4ff 58%,#e4eeff)}.reward-card{border-color:#f2dec9;background:linear-gradient(145deg,#fff,#fff8f0 58%,#ffedda)}.commission-card{border-color:#d4eee8;background:linear-gradient(145deg,#fff,#effbf8 58%,#ddf7f0)}
  .balance-card header{position:relative;z-index:2;display:grid;grid-template-columns:44px minmax(0,1fr) auto;align-items:center;gap:10px}.balance-card header>i{display:grid;width:44px;height:44px;place-items:center;border-radius:13px;color:#fff;background:linear-gradient(145deg,#64aaff,#456be9);font-size:16px;font-style:normal;font-weight:900;box-shadow:0 8px 18px rgba(58,102,218,.2)}.reward-card header>i{background:linear-gradient(145deg,#ffbb5d,#ff8f28)}.commission-card header>i{background:linear-gradient(145deg,#48d6b5,#0cb38c)}.balance-card header b{display:block;color:#17223c;font-size:15px}.balance-card header small{display:block;margin-top:3px;color:#8591a7;font-size:9px}.balance-card header>span{align-self:start;color:#90a0bf;font-weight:900;letter-spacing:2px}.balance-card>strong{position:relative;z-index:2;margin-top:28px;color:#12305d;font-size:29px;letter-spacing:-1px}.balance-card>strong em{color:#64738e;font-size:11px;font-style:normal}.balance-card>p{position:relative;z-index:2;margin:5px 0 0;color:#8792a6;font-size:9px}.balance-card footer{position:relative;z-index:3;display:flex;gap:8px;margin-top:auto}.balance-card footer button{min-width:94px;min-height:38px;padding:0 15px;border:1px solid rgba(223,230,243,.9);border-radius:999px;color:#5365d5;background:rgba(255,255,255,.82);font-size:11px;font-weight:900}.balance-card footer .primary{border:0;color:#fff;background:linear-gradient(135deg,#5e8cf7,#4764e3);box-shadow:0 8px 18px rgba(65,93,208,.2)}.reward-card footer .primary{background:linear-gradient(135deg,#ffad4d,#f28a21)}.commission-card footer .primary{background:linear-gradient(135deg,#36cba7,#10aa82)}.balance-card footer button:disabled{opacity:.5}
  .card-art{position:absolute;z-index:1;right:21px;bottom:50px}.wallet-art{width:76px;height:57px;border-radius:13px;background:linear-gradient(145deg,#c5dcff,#79a7fa);box-shadow:0 14px 23px rgba(75,118,209,.22);transform:rotate(-8deg)}.wallet-art::before{content:"";position:absolute;left:11px;top:-13px;width:52px;height:29px;border-radius:8px;background:linear-gradient(145deg,#e7f0ff,#91b6fb)}.wallet-art i{position:absolute;right:-7px;top:18px;width:30px;height:22px;border-radius:8px;background:#5e88ec}.bean-art{display:flex;align-items:flex-end}.bean-art i{display:block;width:35px;height:35px;margin-left:-9px;border-radius:54% 46% 55% 45%;background:radial-gradient(circle at 32% 28%,#e9f6ff,#77a8ff 42%,#466ae4);box-shadow:0 10px 16px rgba(68,100,212,.18);transform:rotate(18deg)}.bean-art i:nth-child(2){width:51px;height:51px}.bean-art i:nth-child(3){width:27px;height:27px}.gift-art{display:grid;width:67px;height:58px;place-items:center;border-radius:12px;color:#fff;background:linear-gradient(145deg,#ffd6b0,#ff8e4f);font-size:25px;box-shadow:0 13px 22px rgba(237,125,59,.2)}.coin-art{display:flex;align-items:flex-end}.coin-art i{display:block;width:46px;height:18px;margin-left:-28px;border-radius:50%;background:linear-gradient(#72e3c4,#1fbe93);box-shadow:0 5px 0 #0fac83,0 10px 15px rgba(19,165,125,.18)}.coin-art i:nth-child(2){margin-bottom:13px}.coin-art i:nth-child(3){margin-bottom:26px}
  .wallet-summary{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin:6px 20px 0}.wallet-summary div{min-width:0;padding:13px 10px;border:1px solid #e5e9f2;border-radius:15px;background:rgba(255,255,255,.8)}.wallet-summary span{display:block;color:#8a94a7;font-size:9px}.wallet-summary b{display:block;margin-top:6px;overflow:hidden;color:#26334f;font-size:11px;text-overflow:ellipsis;white-space:nowrap}
  .record-panel{margin:18px 20px 0;padding:17px 0 3px;border:1px solid #e3e8f2;border-radius:23px;background:#fff;box-shadow:0 10px 30px rgba(52,68,120,.05)}.record-panel>header{display:flex;align-items:center;justify-content:space-between;padding:0 16px}.record-panel h2{margin:4px 0 0;color:#18233d;font-size:18px}.record-panel header a{color:#5e70dd;font-size:10px;font-weight:850}.record-tabs{display:flex;gap:7px;margin-top:14px;padding:0 15px 10px;overflow-x:auto;scrollbar-width:none}.record-tabs::-webkit-scrollbar{display:none}.record-tabs button{flex:0 0 auto;min-height:31px;padding:0 11px;border:1px solid #e3e7f0;border-radius:999px;color:#7b869b;background:#fafbfe;font-size:10px;font-weight:800}.record-tabs button.active{border-color:#6373e1;color:#fff;background:#6373e1}.record-list{border-top:1px solid #edf0f5}.record-list article{display:grid;grid-template-columns:34px minmax(0,1fr) auto;align-items:center;gap:10px;padding:13px 15px;border-bottom:1px solid #f0f2f6}.record-list article>i{display:grid;width:34px;height:34px;place-items:center;border-radius:11px;color:#237f67;background:#e4f8f1;font-size:10px;font-style:normal;font-weight:900}.record-list article>i.out{color:#b67818;background:#fff4df}.record-list article div{min-width:0}.record-list article b{display:block;color:#28344d;font-size:11px}.record-list article small{display:block;margin-top:4px;overflow:hidden;color:#9099aa;font-size:8px;text-overflow:ellipsis;white-space:nowrap}.record-list article>strong{color:#168b69;font-size:11px}.record-list article>strong.minus{color:#cf7049}.record-empty{margin:0;padding:28px 15px;color:#929bab;text-align:center;font-size:11px}
  .sheet-layer{position:fixed;z-index:80;inset:0;display:flex;align-items:flex-end;justify-content:center}.sheet-backdrop{position:absolute;inset:0;border:0;background:rgba(17,26,51,.36);backdrop-filter:blur(4px)}.action-sheet{position:relative;width:min(100%,540px);padding:21px 20px calc(20px + env(safe-area-inset-bottom));border-radius:26px 26px 0 0;background:#fff;box-shadow:0 -24px 60px rgba(28,41,78,.22)}.action-sheet header{display:flex;align-items:flex-start;justify-content:space-between}.action-sheet header span{color:#7e8ba4;font-size:9px;font-weight:850}.action-sheet h2{margin:4px 0 0;color:#19243e;font-size:21px}.action-sheet header button{display:grid;width:35px;height:35px;place-items:center;border:0;border-radius:50%;color:#6f7a90;background:#f1f4f8;font-size:22px}.action-sheet>p{margin:13px 0;color:#7d889e;font-size:11px;line-height:1.6}.sheet-info,.bean-preview{display:flex;align-items:center;justify-content:space-between;margin-top:14px;padding:12px 13px;border-radius:13px;color:#7b8699;background:#f5f7fb;font-size:10px}.sheet-info b,.bean-preview b{color:#3548b6}.action-sheet form{display:grid;gap:13px;margin-top:14px}.action-sheet label{display:grid;gap:6px;color:#66728a;font-size:10px;font-weight:800}.action-sheet input{width:100%;border:1px solid #dfe4ee;border-radius:13px;outline:none;background:#fbfcff;padding:12px 13px;color:#25304a}.action-sheet input:focus{border-color:#8794e9;box-shadow:0 0 0 3px rgba(100,115,220,.1)}.sheet-error,.sheet-success{margin:0;padding:10px 12px;border-radius:12px;font-size:10px}.sheet-error{color:#bf4855;background:#fff0f2}.sheet-success{color:#158461;background:#eaf9f3}.submit-action{min-height:48px;border:0;border-radius:14px;color:#fff;background:linear-gradient(135deg,#6878ed,#4f5ed4);font-weight:900;box-shadow:0 12px 24px rgba(77,91,204,.22)}.submit-action:disabled{opacity:.6}
  .record-list article div>button{margin-top:8px;padding:6px 10px;border:1px solid #dce4f8;border-radius:10px;color:#4f63c9;background:#f4f7ff;font-size:10px}.action-sheet form>button:not(.submit-action){min-height:42px;border:1px solid #dfe4ee;border-radius:13px;color:#5366d9;background:#f8faff;font-weight:800}
  @media(max-width:360px){.balance-card{flex-basis:89%}.wallet-summary{grid-template-columns:1fr}.record-panel{margin-inline:14px}.wallet-page{padding-top:18px}}
</style>
