<script lang="ts">
  import { onMount } from 'svelte';
  import {
    createShopOrder,
    getMarketingCampaigns,
    claimFreeMarketingOrder,
    getFinanceDashboard,
    getMembershipOffers,
    getTimeCardOffers,
    getShopOrder,
    getWechatPaymentStatus,
    prepayWechatShopOrder,
    queryWechatShopOrder,
    walletPayShopOrder,
  } from '$lib/api';
  import type { FinanceDashboard, MembershipOffer, ShopOrder, TimeCardOffer } from '$lib/types';
  import { invokeWechatPayment, shopOrderIsPaid } from '$lib/wechatPay';

  type ProductKind = 'membership' | 'time_card';
  type MembershipCycle = 'single_month' | 'recurring_month' | 'quarter' | 'half_year' | 'annual';
  type CycleOption = { key: MembershipCycle; label: string; note: string; months: number; discountBps: number };

  let campaignId=0;
  let campaignItem: import('$lib/types').MarketingCampaignItem | null=null;
  let kind: ProductKind = 'time_card';
  let membership: MembershipOffer | null = null;
  let timeCard: TimeCardOffer | null = null;
  let finance: FinanceDashboard | null = null;
  let cycle: MembershipCycle = 'single_month';
  let quantity = 1;
  let order: ShopOrder | null = null;
  let loading = true;
  let processing = false;
  let error = '';
  let paid = false;
  let paymentMethod: 'wechat' | 'wallet' = 'wechat';
  let wechatEnabled = false;
  let inWechat = false;
  let paymentNote = '';

  onMount(async () => {
    const params = new URLSearchParams(window.location.search);
    kind = params.get('type') === 'membership' ? 'membership' : 'time_card';
    const productId = Number(params.get('id') || 0);
    campaignId=Number(params.get('campaign')||0);
    inWechat = /micromessenger/i.test(navigator.userAgent);
    if (params.get('wechat_auth') === 'failed') error = '微信授权未完成，请重新点击支付。';
    try {
      wechatEnabled = (await getWechatPaymentStatus().catch(() => ({ enabled: false }))).enabled;
      const orderId = Number(params.get('order') || 0);
      if (Number.isSafeInteger(orderId) && orderId > 0) {
        order = await getShopOrder(orderId);
        if (order.order_type !== 'membership' && order.order_type !== 'time_card') {
          order = null;
          throw new Error('此订单暂不支持手机端支付');
        }
        kind = order.order_type;
        paid = shopOrderIsPaid(order);
        finance = await getFinanceDashboard().catch(() => null);
        if (!paid && wechatEnabled) await checkPayment(false);
        history.replaceState(history.state, '', `/shop/checkout?order=${orderId}`);
        return;
      }
      const [catalog, currentFinance] = await Promise.all([
        kind === 'membership' ? getMembershipOffers() : getTimeCardOffers(),
        getFinanceDashboard().catch(() => null),
      ]);
      finance = currentFinance;
      if(campaignId){const p=(await getMarketingCampaigns()).items.find(p=>p.id===campaignId);if(!p||p.eligible===false)throw new Error(p?.ineligible_reason||'活动不可参与');campaignItem=p.items.find(i=>i.target_type===kind&&i.target_id===productId)||null;if(!campaignItem)throw new Error('活动商品不存在');quantity=campaignItem.quantity;}
      if (kind === 'membership') {
        membership = (catalog.items as MembershipOffer[]).find((item) => item.id === productId) || null;
      } else {
        timeCard = (catalog.items as TimeCardOffer[]).find((item) => item.id === productId) || null;
      }
      if (!membership && !timeCard) error = '没有找到这个商品，可能已经下架。';
    } catch (value) {
      error = value instanceof Error ? value.message : '商品信息加载失败';
    } finally {
      loading = false;
    }
  });

  function yuan(cents: number) {
    return new Intl.NumberFormat('zh-CN', {
      style: 'currency',
      currency: 'CNY',
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format((cents || 0) / 100).replace('CN¥', '¥');
  }

  function hours(seconds: number) {
    const value = (seconds || 0) / 3600;
    return Number.isInteger(value) ? String(value) : value.toFixed(1);
  }

  function idempotencyKey(prefix: string) {
    const random = typeof crypto !== 'undefined' && 'randomUUID' in crypto
      ? crypto.randomUUID()
      : `${Date.now()}-${Math.random().toString(36).slice(2)}`;
    return `mobile-${prefix}-${random}`;
  }

  function cycleOptions(item: MembershipOffer): CycleOption[] {
    const options: CycleOption[] = [
      { key: 'single_month', label: '月付', note: '购买 1 个月', months: 1, discountBps: 10000 },
      { key: 'quarter', label: '季付', note: '购买 3 个月', months: 3, discountBps: item.recurring_quarter_discount_bps || 10000 },
      { key: 'half_year', label: '半年', note: '购买 6 个月', months: 6, discountBps: 10000 },
      { key: 'annual', label: '年付', note: '购买 12 个月', months: 12, discountBps: item.annual_discount_bps || 10000 },
    ];
    // JSAPI has no recurring debit agreement; only one-off periods are offered.
    return options;
  }

  function selectedCycle(item: MembershipOffer) {
    return cycleOptions(item).find((option) => option.key === cycle) || cycleOptions(item)[0];
  }

  function estimatedMembershipPrice(item: MembershipOffer) {
    const selected = selectedCycle(item);
    return Math.round(item.monthly_price_cents * selected.months * selected.discountBps / 10000 / 100) * 100;
  }

  function totalPrice() {
    if (order) return order.payable_amount_cents;
    if(campaignItem){if(campaignItem.pricing_mode==='free')return 0;if(campaignItem.pricing_mode==='fixed')return campaignItem.fixed_price_cents||0;const base=kind==='membership'?(membership?.monthly_price_cents||0)*(campaignItem.package_months||1)*quantity:(timeCard?.original_price_cents||0)*quantity;return Math.floor(Math.floor(base/100)*campaignItem.discount_bps/10000)*100;}
    if (membership) return estimatedMembershipPrice(membership);
    return (timeCard?.sale_price_cents || 0) * quantity;
  }

  async function createOrder() {
    if ((!membership && !timeCard) || processing) return;
    processing = true;
    error = '';
    try {
      order = await createShopOrder({
        product_type: kind,
        product_id: membership?.id || timeCard?.id || 0,
        quantity: kind === 'time_card' ? quantity : 1,
        membership_cycle: kind === 'membership' ? cycle : undefined,
        marketing_placement: 'shop',
        marketing_campaign_id: campaignId||undefined,
        idempotency_key: idempotencyKey('order'),
      });
      history.replaceState(history.state, '', `/shop/checkout?order=${order.id}`);
    } catch (value) {
      error = value instanceof Error ? value.message : '订单创建失败';
    } finally {
      processing = false;
    }
  }

  async function payOrder() {
    if (!order || processing) return;
    processing = true;
    error = '';
    try {
      if(order.payable_amount_cents===0){order=(await claimFreeMarketingOrder(order.id)).order;paid=shopOrderIsPaid(order);return}
      if (paymentMethod === 'wallet' && kind === 'time_card') {
        const response = await walletPayShopOrder(order.id, idempotencyKey('wallet-pay'));
        order = response.order;
      } else {
        if (!wechatEnabled) throw new Error('微信支付尚未开通，请等待管理员完成商户配置。');
        if (!inWechat) throw new Error('请在微信中打开手机商城使用微信支付。');
        const response = await prepayWechatShopOrder(order.id);
        order = response.order;
        if (shopOrderIsPaid(order)) { paid = true; return; }
        if (response.authorization_required && response.authorization_url) {
          window.location.assign(response.authorization_url);
          return;
        }
        if (!response.payment_params) throw new Error('订单当前不可支付，请查询支付结果。');
        const result = await invokeWechatPayment(response.payment_params);
        await checkPayment(false);
        if (!paid) {
          paymentNote = result === 'cancel' ? '你已取消收银台；订单保留，可稍后继续支付。' : '正在等待微信到账确认，请点击“查询支付结果”；不要重复付款。';
          return;
        }
      }
      paid = shopOrderIsPaid(order);
      finance = await getFinanceDashboard().catch(() => finance);
    } catch (value) {
      error = value instanceof Error ? value.message : '支付失败，请稍后重试';
    } finally {
      processing = false;
    }
  }

  async function checkPayment(manual = true) {
    if (!order || (manual && processing)) return;
    if (manual) { processing = true; error = ''; }
    try {
      const result = await queryWechatShopOrder(order.id);
      order = result.order;
      paid = shopOrderIsPaid(order);
      if (paid) {
        paymentNote = '';
        finance = await getFinanceDashboard().catch(() => finance);
      } else {
        paymentNote = result.trade_state === 'CLOSED' ? '上次微信支付已关闭，可重新发起支付。' : '尚未确认付款，请稍后查询支付结果。';
      }
    } catch (value) {
      error = value instanceof Error ? value.message : '支付状态查询失败，请稍后重试';
    } finally { if (manual) processing = false; }
  }
</script>

<svelte:head><title>{kind === 'membership' ? '会员订阅' : '购买 AI 时长'}</title></svelte:head>

<section class="checkout-page top-space">
  <header class="checkout-header">
    <a href="/shop" aria-label="返回商城">‹</a>
    <div><span>{kind === 'membership' ? 'MEMBERSHIP' : 'AI TIME'}</span><h1>{kind === 'membership' ? '会员订阅' : '购买 AI 时长'}</h1></div>
  </header>

  {#if loading}
    <div class="state-card">正在准备订单…</div>
  {:else if error && !membership && !timeCard && !order}
    <div class="state-card error-card">{error}<a href="/shop">返回小蓝商城</a></div>
  {:else if membership || timeCard || order}
    <article class="checkout-product" class:time-product={kind === 'time_card'}>
      <div class="product-badge">{kind === 'membership' ? '♛' : 'AI'}</div>
      <div class="product-copy">
        <span>{kind === 'membership' ? '会员方案' : 'AI 时长卡'}</span>
        <h2>{membership?.name || timeCard?.name || order?.items?.[0]?.product_name || '商城订单'}</h2>
        <p>{membership?.description || timeCard?.description || '选择后进入订单支付流程'}</p>
      </div>
      <strong>{order ? yuan(order.payable_amount_cents) : campaignItem ? yuan(totalPrice()) : kind === 'membership' ? `${yuan(membership?.monthly_price_cents || 0)} / 月` : yuan(timeCard?.sale_price_cents || 0)}</strong>
    </article>

    {#if !order}
      {#if campaignItem}<section class="checkout-block"><h3>活动商品包</h3><p>数量 {campaignItem.quantity}；应付 {yuan(totalPrice())}。活动规格固定，不可更改。</p></section>
      {:else if membership}
        <section class="checkout-block">
          <header><div><span>订阅周期</span><h3>选择适合你的付费周期</h3></div></header>
          <div class="cycle-grid">
            {#each cycleOptions(membership) as option}
              <button class:active={cycle === option.key} on:click={() => cycle = option.key}>
                <b>{option.label}</b><small>{option.note}</small>
                {#if option.discountBps < 10000}<i>{(option.discountBps / 1000).toFixed(1).replace('.0', '')} 折</i>{/if}
              </button>
            {/each}
          </div>
          <ul class="order-benefits">
            <li><i>✓</i>每月包含 {hours(membership.included_seconds)} 小时 AI 时长</li>
            <li><i>✓</i>时长卡折扣 {(membership.time_card_discount_bps / 1000).toFixed(1).replace('.0', '')} 折</li>
            <li><i>✓</i>设备折扣 {(membership.device_discount_bps / 1000).toFixed(1).replace('.0', '')} 折</li>
          </ul>
        </section>
      {:else if timeCard}
        <section class="checkout-block">
          <header><div><span>购买数量</span><h3>确认本次购买数量</h3></div></header>
          <div class="quantity-row">
            <div><b>{hours(timeCard.duration_seconds)} 小时 / 张</b><small>{timeCard.activation_mode === 'first_use' ? '首次实际使用时自动激活' : '购买后自动激活'}</small></div>
            <div class="stepper"><button disabled={quantity <= 1} on:click={() => quantity = Math.max(1, quantity - 1)}>−</button><b>{quantity}</b><button disabled={quantity >= 100} on:click={() => quantity = Math.min(100, quantity + 1)}>＋</button></div>
          </div>
          <div class="validity-note">购买成功后放入时长卡包，{timeCard.validity_days > 0 ? `激活后有效 ${timeCard.validity_days} 天` : '未激活前可长期保存'}。</div>
        </section>
      {/if}

      <section class="summary-card">
        <div><span>商品金额</span><b>{yuan(totalPrice())}</b></div>
        <div><span>支付方式</span><b>{paymentMethod === 'wallet' ? '钱包余额' : '微信支付'}</b></div>
        {#if kind === 'time_card' && finance}<div><span>钱包余额</span><b class:warn={finance.cash_balance_cents < totalPrice()}>{yuan(finance.cash_balance_cents)}</b></div>{/if}
        <div class="summary-total"><span>应付金额</span><strong>{yuan(totalPrice())}</strong></div>
      </section>

      {#if error}<p class="checkout-error">{error}</p>{/if}
      <button class="checkout-primary" disabled={processing} on:click={createOrder}>{processing ? '正在生成订单…' : (kind === 'membership' ? '确认订阅' : '确认购买')}</button>
      <p class="agreement">点击即表示同意服务规则，最终金额以订单为准。</p>
    {:else if order.status !== 'pending' && !paid}
      <div class="state-card">订单已关闭或不可支付，请返回商城重新下单。<a href="/orders">查看订单</a></div>
    {:else if !paid}
      <section class="payment-card">
        <span>订单已创建</span>
        <h2>{order.order_no}</h2>
        <div class="pay-amount"><small>待支付</small><strong>{yuan(order.payable_amount_cents)}</strong></div>
        <button class="pay-method" class:selected={paymentMethod === 'wechat'} disabled={processing} on:click={() => paymentMethod = 'wechat'}><i>微</i><div><b>微信支付</b><small>{!wechatEnabled ? '商户配置中，暂未开通' : !inWechat ? '请在微信中打开此页面支付' : '微信确认到账后自动发放权益'}</small></div><span>{paymentMethod === 'wechat' ? '✓' : ''}</span></button>
        {#if kind === 'time_card'}
          <button class="pay-method" class:selected={paymentMethod === 'wallet'} disabled={processing} on:click={() => paymentMethod = 'wallet'}><i>¥</i><div><b>钱包余额支付</b><small>可用余额 {yuan(finance?.cash_balance_cents || 0)}</small></div><span>{paymentMethod === 'wallet' ? '✓' : ''}</span></button>
        {/if}
      </section>
      {#if paymentNote}<p class="agreement">{paymentNote}</p>{/if}
      {#if error}<p class="checkout-error">{error}</p>{/if}
      <button class="checkout-primary" disabled={processing || (order.payable_amount_cents > 0 && paymentMethod === 'wechat' && (!wechatEnabled || !inWechat))} on:click={payOrder}>{processing ? '正在处理…' : (order.payable_amount_cents===0?'确认免费领取':`支付 ${yuan(order.payable_amount_cents)}`)}</button>
      {#if wechatEnabled}<button class="query-payment" disabled={processing} on:click={() => checkPayment()}>查询支付结果</button>{/if}
      <a class="order-link" href="/orders">稍后支付，在订单中查看</a>
    {:else}
      <section class="success-card">
        <div>✓</div><span>支付成功</span>
        <h2>{kind === 'membership' ? '会员订阅已生效' : '时长卡已放入卡包'}</h2>
        <p>{kind === 'membership' ? '会员权益与本周期 AI 时长已经到账。' : '需要使用时可在 AI 时长页面启用，启用前不会消耗有效期。'}</p>
        <strong>{yuan(order.paid_amount_cents)}</strong>
      </section>
      <a class="checkout-primary action-link" href="/me">查看我的权益</a>
      <a class="order-link" href="/shop">继续逛小蓝商城</a>
    {/if}
  {/if}
</section>

<style>
  .pay-method{width:100%;text-align:left;cursor:pointer}.pay-method.selected{border-color:#7784ec}.query-payment{display:block;margin:14px auto 0;padding:9px 18px;border:1px solid #dfe5f2;border-radius:12px;background:#fff;color:#5566d2;font-weight:800}
  .checkout-page{min-height:100vh;padding:22px 18px 38px;background:radial-gradient(circle at 88% 2%,rgba(125,142,255,.2),transparent 27%),linear-gradient(180deg,#f7f9ff,#f4f7fd)}
  .checkout-header{display:flex;align-items:center;gap:13px;margin-bottom:20px}.checkout-header>a{display:grid;width:40px;height:40px;place-items:center;border:1px solid #e2e7f3;border-radius:13px;color:#364568;background:rgba(255,255,255,.88);font-size:30px;line-height:1}.checkout-header span{color:#7c89a7;font-size:9px;font-weight:900;letter-spacing:.13em}.checkout-header h1{margin:2px 0 0;color:#14203b;font-size:22px}
  .state-card{padding:34px 20px;border:1px solid #e3e8f3;border-radius:22px;color:#7c879c;text-align:center;background:#fff}.error-card{color:#c3505b}.error-card a{display:block;margin-top:16px;color:#5365d7;font-weight:850}
  .checkout-product{display:grid;grid-template-columns:54px minmax(0,1fr);gap:13px;padding:18px;border:1px solid #dfe6f6;border-radius:23px;background:linear-gradient(145deg,#fff,#eef4ff);box-shadow:0 14px 34px rgba(58,77,139,.09)}.checkout-product.time-product{background:linear-gradient(145deg,#fff,#f1edff)}.product-badge{display:grid;width:54px;height:54px;place-items:center;border-radius:16px;color:#fff;background:linear-gradient(145deg,#5caeff,#5366e8);font-size:21px;font-weight:900;box-shadow:0 10px 20px rgba(61,102,214,.21)}.time-product .product-badge{background:linear-gradient(145deg,#9485ff,#5d4be7)}.product-copy{min-width:0}.product-copy>span{color:#7484a4;font-size:10px;font-weight:850}.product-copy h2{margin:4px 0 3px;overflow:hidden;color:#15203a;font-size:19px;text-overflow:ellipsis;white-space:nowrap}.product-copy p{display:-webkit-box;margin:0;overflow:hidden;color:#7c88a1;font-size:11px;line-height:1.45;line-clamp:2;-webkit-box-orient:vertical;-webkit-line-clamp:2}.checkout-product>strong{grid-column:2;color:#355ed5;font-size:17px}
  .checkout-block,.summary-card,.payment-card,.success-card{margin-top:14px;padding:18px;border:1px solid #e3e8f3;border-radius:22px;background:#fff;box-shadow:0 10px 30px rgba(50,66,120,.05)}.checkout-block header span{color:#8490a5;font-size:10px;font-weight:850}.checkout-block h3{margin:4px 0 14px;color:#1b263f;font-size:16px}.cycle-grid{display:grid;grid-template-columns:repeat(2,1fr);gap:8px}.cycle-grid button{position:relative;display:grid;gap:2px;min-height:61px;padding:10px 12px;text-align:left;border:1px solid #e1e6f1;border-radius:14px;color:#26324d;background:#fafbfe}.cycle-grid button.active{border-color:#6b76e7;background:#f0f1ff;box-shadow:0 0 0 2px rgba(99,111,226,.09)}.cycle-grid b{font-size:13px}.cycle-grid small{color:#8b95a8;font-size:9px}.cycle-grid i{position:absolute;right:7px;top:7px;padding:2px 5px;border-radius:999px;color:#6550df;background:#e9e5ff;font-size:8px;font-style:normal}.order-benefits{display:grid;gap:8px;margin:15px 0 0;padding:0;list-style:none}.order-benefits li{display:flex;align-items:center;gap:7px;color:#6f7b91;font-size:11px}.order-benefits i{display:grid;width:16px;height:16px;place-items:center;border-radius:50%;color:#fff;background:#6476e8;font-size:9px;font-style:normal}
  .quantity-row{display:flex;align-items:center;justify-content:space-between;gap:13px}.quantity-row>div:first-child{display:grid;gap:4px}.quantity-row b{color:#263149;font-size:13px}.quantity-row small{color:#8a94a8;font-size:10px}.stepper{display:grid;grid-template-columns:34px 36px 34px;align-items:center;text-align:center}.stepper button{height:34px;border:1px solid #dfe5f2;color:#5366da;background:#f7f9ff;font-size:19px}.stepper button:first-child{border-radius:11px 0 0 11px}.stepper button:last-child{border-radius:0 11px 11px 0}.stepper button:disabled{color:#c1c7d3}.validity-note{margin-top:14px;padding:10px 12px;border-radius:12px;color:#7c879d;background:#f5f7fb;font-size:10px;line-height:1.5}
  .summary-card{display:grid;gap:11px}.summary-card>div{display:flex;align-items:center;justify-content:space-between;color:#7b869b;font-size:11px}.summary-card b{color:#303c56;font-size:11px}.summary-card b.warn{color:#ce4c5d}.summary-card .summary-total{margin-top:2px;padding-top:13px;border-top:1px solid #edf0f5}.summary-total strong{color:#e94f50;font-size:23px}.checkout-error{margin:13px 2px 0;padding:11px 13px;border-radius:12px;color:#c54d59;background:#fff1f3;font-size:11px;line-height:1.5}.checkout-primary{display:grid;width:100%;min-height:50px;margin-top:16px;place-items:center;border:0;border-radius:15px;color:#fff;background:linear-gradient(135deg,#6878ef,#4f5dd5);font-weight:900;box-shadow:0 13px 28px rgba(72,87,205,.25)}.checkout-primary:disabled{opacity:.62}.agreement{margin:10px 0 0;color:#9aa2b1;text-align:center;font-size:9px}
  .payment-card>span{color:#7885a1;font-size:10px;font-weight:850}.payment-card>h2{margin:4px 0 17px;color:#1d2942;font-size:15px}.pay-amount{display:grid;gap:3px;padding:17px 0;text-align:center;border-block:1px solid #edf0f5}.pay-amount small{color:#8b95a8;font-size:10px}.pay-amount strong{color:#e94f50;font-size:30px}.pay-method{display:grid;grid-template-columns:42px 1fr auto;align-items:center;gap:11px;margin-top:16px;padding:12px;border:1px solid #dfe5f2;border-radius:15px;background:#fafbff}.pay-method>i{display:grid;width:42px;height:42px;place-items:center;border-radius:12px;color:#fff;background:linear-gradient(145deg,#49bdf0,#5269e6);font-size:16px;font-style:normal;font-weight:900}.pay-method div{display:grid;gap:3px}.pay-method b{color:#28334c;font-size:12px}.pay-method small{color:#8b95a7;font-size:9px}.pay-method>span{display:grid;width:20px;height:20px;place-items:center;border-radius:50%;color:#fff;background:#596cdf;font-size:11px}.order-link{display:block;margin-top:14px;color:#697696;text-align:center;font-size:11px;font-weight:800}
  .success-card{text-align:center}.success-card>div{display:grid;width:60px;height:60px;margin:2px auto 13px;place-items:center;border-radius:50%;color:#fff;background:linear-gradient(145deg,#44d3a1,#17ad78);font-size:28px;box-shadow:0 14px 28px rgba(25,175,122,.22)}.success-card>span{color:#18a675;font-size:11px;font-weight:900}.success-card h2{margin:6px 0;color:#17223b;font-size:21px}.success-card p{margin:0;color:#7e899e;font-size:11px;line-height:1.6}.success-card strong{display:block;margin-top:15px;color:#354ec2;font-size:24px}.action-link{text-decoration:none}
  @media(max-width:360px){.cycle-grid{grid-template-columns:1fr}.checkout-product{grid-template-columns:48px minmax(0,1fr)}.product-badge{width:48px;height:48px}.checkout-page{padding-inline:14px}}
</style>
