<script lang="ts">
  import { onMount } from 'svelte';
  import { getDeviceOffers, getMembershipOffers, getTimeCardOffers, getMarketingCampaigns } from '$lib/api';
  import type { DeviceOffer, MembershipOffer, TimeCardOffer } from '$lib/types';

  type ShopSection = 'membership' | 'time' | 'device';
  type SelectedOffer = { kind: 'device'; item: DeviceOffer };

  let campaigns: import('$lib/types').MarketingCampaign[] = [];
  let memberships: MembershipOffer[] = [];
  let cards: TimeCardOffer[] = [];
  let devices: DeviceOffer[] = [];
  let activeSection: ShopSection = 'time';
  let selectedOffer: SelectedOffer | null = null;
  let loading = true;
  let error = '';

  onMount(async () => {
    try {
      const [membershipData, timeCardData, deviceData, campaignData] = await Promise.all([
        getMembershipOffers(),
        getTimeCardOffers(),
        getDeviceOffers(),
        getMarketingCampaigns(),
      ]);
      campaigns = campaignData.items || [];
      memberships = membershipData.items || [];
      cards = timeCardData.items || [];
      devices = deviceData.items || [];
    } catch (value) {
      error = value instanceof Error ? value.message : '商城数据加载失败，请稍后重试';
    } finally {
      loading = false;
    }
  });

  function campaignName(i:import('$lib/types').MarketingCampaignItem){return (i.target_type==='membership'?memberships:i.target_type==='time_card'?cards:devices).find(t=>t.id===i.target_id)?.name||'商品 #'+i.target_id}
  function campaignPrice(i:import('$lib/types').MarketingCampaignItem){if(i.pricing_mode==='free')return 0;if(i.pricing_mode==='fixed')return i.fixed_price_cents||0;const m=memberships.find(t=>t.id===i.target_id),c=cards.find(t=>t.id===i.target_id),d=devices.find(t=>t.id===i.target_id);const base=i.target_type==='membership'?(m?.monthly_price_cents||0)*(i.package_months||1):i.target_type==='time_card'?(c?.original_price_cents||0):(d?.original_price_cents||0);return Math.floor(Math.floor(base*(i.quantity||1)/100)*i.discount_bps/10000)*100}
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
    return Number.isInteger(value) ? value.toLocaleString('zh-CN') : value.toFixed(1);
  }

  function discount(bps: number) {
    if (!bps || bps >= 10000) return '原价';
    return (bps / 1000).toFixed(1).replace(/\.0$/, '') + ' 折';
  }

  function membershipBenefits(item: MembershipOffer) {
    const values = [`每月含 ${hours(item.included_seconds)} 小时 AI 时长`];
    values.push(item.time_card_discount_bps < 10000 ? `购买时长卡享 ${discount(item.time_card_discount_bps)}` : '时长卡按商城价购买');
    values.push(item.device_discount_bps < 10000 ? `购买设备享 ${discount(item.device_discount_bps)}` : '设备按商城价购买');
    if (item.allow_auto_renew) values.push('支持按月自动续费');
    return values;
  }

  function timeCardBenefits(item: TimeCardOffer) {
    const values = [`补充 ${hours(item.duration_seconds)} 小时 AI 时长`];
    values.push(item.validity_days > 0 ? `激活后有效 ${item.validity_days} 天` : '激活后长期有效');
    values.push(item.activation_mode === 'first_use' ? '首次实际使用时自动激活' : '购买后自动激活');
    values.push(item.activation_deadline_days > 0 ? `购买后 ${item.activation_deadline_days} 天内需激活` : '未激活前可长期放在卡包');
    return values;
  }

  function deviceBenefits(item: DeviceOffer) {
    const values: string[] = [];
    if (item.description) values.push(item.description);
    values.push(item.available_stock > 0 ? `现货 ${item.available_stock} ${item.unit_label || '台'}` : '暂时缺货');
    if (item.membership_discount_bps < 10000) values.push(`当前会员设备折扣 ${discount(item.membership_discount_bps)}`);
    values.push('购买后可绑定你的直播间');
    return values;
  }

  function offerPrice(offer: SelectedOffer) {
    return yuan(offer.item.sale_price_cents);
  }

  function offerOriginalPrice(offer: SelectedOffer) {
    return offer.item.original_price_cents > offer.item.sale_price_cents ? offer.item.original_price_cents : 0;
  }

  function jumpTo(section: ShopSection) {
    activeSection = section;
    document.getElementById('shop-' + section)?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }
</script>

<svelte:head><title>小蓝商城</title></svelte:head>

<section class="shop-page top-space">
  <header class="shop-hero">
    <div class="shop-hero-copy">
      <span class="shop-kicker">小蓝商城</span>
      <h1>时长 · 会员 · 设备</h1>
      <p>选择适合你的方案，解锁更多直播能力</p>
    </div>
    <div class="shop-bag-art" aria-hidden="true">
      <i></i>
      <svg viewBox="0 0 48 48"><path d="M13 19h22l-2.5 23h-17L13 19Zm6-2v-3a5 5 0 0 1 10 0v3h4v-3a9 9 0 0 0-18 0v3h4Zm5 7 2.3 5.2 5.7.6-4.3 3.8 1.2 5.6-4.9-2.9-4.9 2.9 1.2-5.6-4.3-3.8 5.7-.6L24 24Z" /></svg>
    </div>
  </header>

  <nav class="shop-tabs" aria-label="商品分类">
    <button class:active={activeSection === 'time'} on:click={() => jumpTo('time')}>
      <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20Zm1 5v4.6l3.2 3.2-1.4 1.4L11 12.4V7h2Z" /></svg>
      时长卡 {cards.length}
    </button>
    <button class:active={activeSection === 'membership'} on:click={() => jumpTo('membership')}>
      <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m3 7 4 3 5-7 5 7 4-3-2 12H5L3 7Zm3 14h12v-2H6v2Z" /></svg>
      会员 {memberships.length}
    </button>
    <button class:active={activeSection === 'device'} on:click={() => jumpTo('device')}>
      <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m12 2 9 5v10l-9 5-9-5V7l9-5Zm0 2.3L6 7.6l6 3.3 6-3.3-6-3.3ZM5 9.3v6.5l6 3.3v-6.5L5 9.3Zm8 9.8 6-3.3V9.3l-6 3.3v6.5Z" /></svg>
      设备 {devices.length}
    </button>
  </nav>

  {#if loading}
    <div class="shop-loading"><span></span><p>正在读取商城商品…</p></div>
  {:else if error}
    <div class="shop-error">{error}</div>
  {:else}
    {#if campaigns.length}<section class="shop-category"><header class="category-head"><span>营销活动</span><h2>当前优惠与新人礼包</h2></header>{#each campaigns as p}<article class="product-card" style="margin-bottom:16px"><h3>{p.name}</h3><p>{p.description}</p>{#if p.controls?.audience&&p.controls.audience!=='all'}<p>新开户专享 · 每人限领一次</p>{/if}{#each p.items as i}<div style="display:flex;align-items:center;justify-content:space-between;gap:12px;margin:14px 0"><div><b>{campaignName(i)} × {i.quantity}</b><p>{campaignPrice(i)===0?'免费领取':yuan(campaignPrice(i))}</p></div>{#if p.eligible===false}<small>{p.ineligible_reason}</small>{:else if i.target_type!=='device_product'}<a class="card-action" href={`/shop/checkout?type=${i.target_type}&id=${i.target_id}&campaign=${p.id}`}>{campaignPrice(i)===0?'领取':'购买'}</a>{:else}<small>设备活动请在网页商城办理</small>{/if}</div>{/each}</article>{/each}</section>{/if}
    <section id="shop-time" class="shop-category">
      <header class="category-head">
        <span>时长卡</span>
        <h2>按量补充 AI 时长</h2>
        <p>购买后进入卡包，需要使用时再启用</p>
      </header>
      <div class="product-rail" aria-label="AI 时长卡，可左右滑动">
        {#each cards as item, index}
          <article class="product-card time-card" class:blue={index % 2 === 1}>
            <header class="product-card-head">
              <span class="product-icon clock-icon">
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20Zm1 5v4.6l3.2 3.2-1.4 1.4L11 12.4V7h2Z" /></svg>
              </span>
              <div><strong>{item.name}</strong><p>{item.description || `补充 ${hours(item.duration_seconds)} 小时 AI 时长`}</p></div>
            </header>
            <div class="product-price"><b>{yuan(item.sale_price_cents)}</b>{#if item.original_price_cents > item.sale_price_cents}<del>{yuan(item.original_price_cents)}</del>{/if}</div>
            <ul class="benefit-list">
              {#each timeCardBenefits(item).slice(0, 3) as benefit}<li><i>✓</i>{benefit}</li>{/each}
            </ul>
            <footer><span>{discount(item.discount_bps)}</span><a class="card-action" href={`/shop/checkout?type=time_card&id=${item.id}`}>购买</a></footer>
          </article>
        {:else}
          <div class="rail-empty">暂无可购买时长卡</div>
        {/each}
      </div>
    </section>

    <section id="shop-membership" class="shop-category">
      <header class="category-head">
        <span>会员方案</span>
        <h2>订阅专属权益</h2>
      </header>
      <div class="product-rail" aria-label="会员方案，可左右滑动">
        {#each memberships as item, index}
          <article class="product-card membership-card" class:gold={index % 2 === 1}>
            <header class="product-card-head">
              <span class="product-icon crown-icon">
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m3 7 4 3 5-7 5 7 4-3-2 12H5L3 7Zm3 14h12v-2H6v2Z" /></svg>
              </span>
              <div><strong>{item.name}</strong><p>{item.description || '适合持续直播运营使用'}</p></div>
            </header>
            <div class="product-price"><b>{yuan(item.monthly_price_cents)}</b><span>/ 月</span></div>
            <ul class="benefit-list">
              {#each membershipBenefits(item).slice(0, 4) as benefit}<li><i>✓</i>{benefit}</li>{/each}
            </ul>
            <footer><span>每月 {hours(item.included_seconds)} 小时</span><a class="card-action" href={`/shop/checkout?type=membership&id=${item.id}`}>订阅</a></footer>
          </article>
        {:else}
          <div class="rail-empty">暂无可购买会员方案</div>
        {/each}
      </div>
    </section>

    <section id="shop-device" class="shop-category">
      <header class="category-head">
        <span>设备</span>
        <h2>直播搭子设备</h2>
        <p>专业设备助力，获得更稳定的直播体验</p>
      </header>
      <div class="product-rail" aria-label="直播设备，可左右滑动">
        {#each devices as item}
          <article class="product-card device-card">
            <header class="product-card-head">
              <span class="product-icon device-icon"><i></i></span>
              <div><strong>{item.name}</strong><p>{item.description || '小巧稳定，开箱即用'}</p></div>
            </header>
            <div class="device-body">
              <div class="product-price"><b>{yuan(item.sale_price_cents)}</b>{#if item.original_price_cents > item.sale_price_cents}<del>{yuan(item.original_price_cents)}</del>{/if}</div>
              {#if item.image_url}
                <img src={item.image_url} alt={item.name} />
              {:else}
                <div class="device-box-art" aria-hidden="true"><span>LIVE</span><i></i></div>
              {/if}
            </div>
            <ul class="benefit-list device-benefits">
              {#each deviceBenefits(item).slice(0, 3) as benefit}<li><i>✓</i>{benefit}</li>{/each}
            </ul>
            <footer><span>{item.available_stock > 0 ? '现货 ' + item.available_stock + (item.unit_label || '台') : '暂时缺货'}</span><button on:click={() => selectedOffer = { kind: 'device', item }}>查看详情</button></footer>
          </article>
        {:else}
          <div class="rail-empty">暂无可购买设备</div>
        {/each}
      </div>
    </section>
  {/if}
</section>

{#if selectedOffer}
  <div class="detail-layer">
    <button class="detail-backdrop" aria-label="关闭商品详情" on:click={() => selectedOffer = null}></button>
    <div class="detail-sheet" role="dialog" aria-modal="true" aria-label="商品详情">
      <header><div><span>直播设备</span><h2>{selectedOffer.item.name}</h2></div><button class="detail-close" aria-label="关闭" on:click={() => selectedOffer = null}>×</button></header>
      <p class="detail-description">{selectedOffer.item.description || '具体权益以当前商品版本为准。'}</p>
      <div class="detail-price"><strong>{offerPrice(selectedOffer)}</strong>{#if offerOriginalPrice(selectedOffer)}<del>{yuan(offerOriginalPrice(selectedOffer))}</del>{/if}</div>
      <ul class="detail-benefits">{#each deviceBenefits(selectedOffer.item) as benefit}<li><i>✓</i><span>{benefit}</span></li>{/each}</ul>
      <button class="detail-done" on:click={() => selectedOffer = null}>我知道了</button>
    </div>
  </div>
{/if}

<style>
  .shop-page{min-height:100vh;padding:0 0 34px;overflow:hidden;background:radial-gradient(circle at 92% 5%,rgba(129,154,255,.18),transparent 24%),linear-gradient(180deg,#f7f9ff 0,#fbfcff 35%,#f5f8ff 100%)}
  .shop-hero{position:relative;min-height:176px;padding:24px 20px 18px;overflow:hidden}
  .shop-hero::before{content:"";position:absolute;right:-38px;top:-42px;width:210px;height:210px;border-radius:50%;background:radial-gradient(circle at 35% 30%,rgba(163,183,255,.5),rgba(223,233,255,.22) 47%,transparent 70%)}
  .shop-hero-copy{position:relative;z-index:2;max-width:72%}.shop-kicker{color:#74829e;font-size:12px;font-weight:850;letter-spacing:.04em}.shop-hero h1{margin:10px 0 7px;color:#101a36;font-size:28px;line-height:1.12;letter-spacing:-1px}.shop-hero p{margin:0;color:#7887a7;font-size:12px;line-height:1.55}
  .shop-bag-art{position:absolute;z-index:1;right:17px;top:24px;width:112px;height:120px;transform:rotate(7deg);border-radius:31px 31px 25px 25px;background:linear-gradient(145deg,#c9d7ff 0%,#8196ff 47%,#655be8 100%);box-shadow:0 22px 42px rgba(74,83,205,.27),inset 0 2px rgba(255,255,255,.68)}.shop-bag-art::before{content:"";position:absolute;left:29px;top:-20px;width:48px;height:38px;border:9px solid #7488f4;border-bottom:0;border-radius:28px 28px 0 0}.shop-bag-art::after{content:"";position:absolute;right:-18px;bottom:-10px;width:62px;height:62px;border-radius:50%;background:rgba(178,222,255,.26);filter:blur(1px)}.shop-bag-art svg{position:absolute;left:30px;top:36px;width:53px;height:53px;fill:#fff;filter:drop-shadow(0 7px 9px rgba(76,79,188,.25))}.shop-bag-art>i{position:absolute;left:-39px;top:31px;width:20px;height:20px;border-radius:50%;background:#a9bdff;box-shadow:90px 87px 0 -5px #9e95f9}
  .shop-tabs{position:relative;z-index:3;display:flex;gap:8px;margin:-17px 18px 5px;padding-bottom:3px;overflow-x:auto;scrollbar-width:none}.shop-tabs::-webkit-scrollbar{display:none}.shop-tabs button{display:flex;align-items:center;gap:6px;flex:0 0 auto;min-height:43px;padding:0 14px;border:1px solid #e2e8f5;border-radius:999px;color:#73809a;background:rgba(255,255,255,.94);font-size:12px;font-weight:850;box-shadow:0 7px 20px rgba(65,83,139,.08)}.shop-tabs button svg{width:17px;height:17px;fill:currentColor}.shop-tabs button.active{border-color:transparent;color:#fff;background:linear-gradient(135deg,#6f7bff,#5a63e6);box-shadow:0 12px 24px rgba(79,87,218,.28)}
  .shop-loading,.shop-error{margin:28px 18px;padding:28px;border:1px solid #e4e9f3;border-radius:22px;color:#7d879c;text-align:center;background:#fff}.shop-loading span{display:block;width:28px;height:28px;margin:0 auto 10px;border:3px solid #e4e8f7;border-top-color:#6675e6;border-radius:50%;animation:shop-spin .8s linear infinite}.shop-loading p{margin:0;font-size:12px}.shop-error{color:#c94c59;background:#fff7f8}@keyframes shop-spin{to{transform:rotate(360deg)}}
  .shop-category{padding-top:17px;scroll-margin-top:10px}.category-head{padding:0 20px 11px}.category-head>span{color:#8490a7;font-size:11px;font-weight:850;letter-spacing:.05em}.category-head h2{margin:4px 0 3px;color:#17213b;font-size:21px;letter-spacing:-.35px}.category-head p{margin:0;color:#8a96ad;font-size:11px;line-height:1.5}
  .product-rail{display:flex;gap:12px;padding:0 20px 10px;overflow-x:auto;overscroll-behavior-inline:contain;scroll-snap-type:x mandatory;scroll-padding-inline:20px;scrollbar-width:none;touch-action:pan-x}.product-rail::-webkit-scrollbar{display:none}.product-card{position:relative;display:flex;flex:0 0 84%;min-height:284px;flex-direction:column;padding:17px;overflow:hidden;border:1px solid rgba(205,216,244,.9);border-radius:21px;scroll-snap-align:start;background:linear-gradient(145deg,#fff 0%,#f3f6ff 62%,#e9efff 100%);box-shadow:0 13px 30px rgba(64,82,145,.09)}.product-card::after{content:"";position:absolute;right:-46px;bottom:-55px;width:190px;height:120px;border-radius:50%;background:rgba(153,175,255,.14);transform:rotate(-17deg)}.product-card.gold{border-color:#f1dfc4;background:linear-gradient(145deg,#fff 0%,#fff9f0 58%,#fff0d8 100%)}.product-card.gold::after{background:rgba(255,187,83,.13)}.product-card.time-card{background:linear-gradient(145deg,#fff 0%,#f8f5ff 58%,#ebe5ff 100%)}.product-card.time-card.blue{background:linear-gradient(145deg,#fff 0%,#f5f9ff 58%,#e3efff 100%)}.product-card.device-card{flex-basis:92%;min-height:315px;background:linear-gradient(145deg,#fff 0%,#f3fbfc 58%,#dff8f2 100%)}
  .product-card-head{position:relative;z-index:2;display:grid;grid-template-columns:48px minmax(0,1fr);align-items:center;gap:11px}.product-icon{display:grid;width:48px;height:48px;place-items:center;border-radius:14px;color:#fff;background:linear-gradient(145deg,#78c8ff,#3b72f3);box-shadow:0 10px 20px rgba(49,105,225,.2)}.product-icon svg{width:27px;height:27px;fill:currentColor}.gold .product-icon{background:linear-gradient(145deg,#ffc969,#ff9f2e);box-shadow:0 10px 20px rgba(237,145,37,.2)}.time-card .product-icon{background:linear-gradient(145deg,#9788ff,#6250f2)}.time-card.blue .product-icon{background:linear-gradient(145deg,#5fb7ff,#246fe9)}.product-card-head strong{display:block;overflow:hidden;color:#14203a;font-size:17px;line-height:1.25;text-overflow:ellipsis;white-space:nowrap}.product-card-head p{display:-webkit-box;margin:4px 0 0;overflow:hidden;color:#7e8aa4;font-size:11px;line-height:1.45;line-clamp:2;-webkit-box-orient:vertical;-webkit-line-clamp:2}
  .product-price{position:relative;z-index:2;display:flex;align-items:baseline;gap:6px;margin:15px 0 10px}.product-price b{color:#245bda;font-size:27px;line-height:1;letter-spacing:-1px}.gold .product-price b{color:#ce7612}.time-card .product-price b{color:#6244ea}.time-card.blue .product-price b,.device-card .product-price b{color:#1f70d9}.product-price span{color:#8994a8;font-size:11px}.product-price del{color:#9ba4b5;font-size:10px}
  .benefit-list{position:relative;z-index:2;display:grid;gap:8px;margin:2px 0 15px;padding:0;list-style:none}.benefit-list li{display:flex;align-items:flex-start;gap:7px;color:#68758f;font-size:11px;line-height:1.4}.benefit-list i{display:grid;width:16px;height:16px;flex:0 0 16px;place-items:center;border-radius:50%;color:#fff;background:#4d91f2;font-size:10px;font-style:normal}.gold .benefit-list i{background:#ffac3f}.time-card .benefit-list i{background:#7159f2}.time-card.blue .benefit-list i{background:#3687ed}.device-card .benefit-list i{background:#18ae83}
  .product-card footer{position:relative;z-index:3;display:flex;align-items:center;justify-content:space-between;gap:10px;margin-top:auto}.product-card footer>span{overflow:hidden;color:#8792a7;font-size:10px;text-overflow:ellipsis;white-space:nowrap}.product-card footer button,.product-card footer .card-action{display:grid;flex:0 0 auto;min-height:38px;padding:0 18px;place-items:center;border:1px solid rgba(255,255,255,.92);border-radius:999px;color:#315ed6;background:rgba(255,255,255,.79);font-size:11px;font-weight:900;box-shadow:0 7px 18px rgba(77,94,153,.09)}.gold footer .card-action{color:#fff;background:linear-gradient(135deg,#ffb94d,#f29a22);box-shadow:0 9px 20px rgba(232,145,29,.21)}.time-card footer .card-action{color:#fff;background:linear-gradient(135deg,#7768f5,#5c49e6);box-shadow:0 9px 20px rgba(94,71,220,.2)}.device-card footer button{color:#118e6e}
  .device-icon{position:relative;background:linear-gradient(145deg,#b9f3e7,#66d9c5);box-shadow:0 10px 20px rgba(38,169,142,.17)}.device-icon::before{content:"";width:25px;height:22px;border-radius:50% 50% 42% 42%;background:#087f73;box-shadow:inset 0 0 0 7px #66e1d1}.device-icon::after{content:"";position:absolute;left:20px;bottom:8px;width:9px;height:3px;border-radius:3px;background:#087f73}.device-body{position:relative;z-index:2;min-height:104px}.device-body img{position:absolute;right:2px;top:-7px;width:140px;height:104px;object-fit:contain}.device-box-art{position:absolute;right:8px;top:5px;width:130px;height:79px;border-radius:17px;transform:perspective(240px) rotateX(7deg) rotateY(-12deg);background:linear-gradient(150deg,#53627f,#26344c 54%,#18243a);box-shadow:0 18px 30px rgba(26,47,77,.28)}.device-box-art span{position:absolute;left:47px;top:20px;padding:5px 10px;border-radius:999px;color:#c7d4e7;background:#33415a;font-size:9px;font-weight:850}.device-box-art i{position:absolute;left:17px;bottom:8px;width:35px;height:4px;border-radius:4px;background:#19a9ff;box-shadow:0 0 10px #0ea5ff}.device-benefits{grid-template-columns:1fr}
  .rail-empty{flex:0 0 100%;padding:28px;border:1px dashed #dce3f1;border-radius:20px;color:#8a95a8;text-align:center;background:rgba(255,255,255,.7);font-size:12px}
  .detail-layer{position:fixed;z-index:80;inset:0;display:flex;align-items:flex-end;justify-content:center}.detail-backdrop{position:absolute;inset:0;width:100%;border:0;background:rgba(16,24,48,.32);backdrop-filter:blur(4px)}.detail-sheet{position:relative;width:min(100%,540px);padding:22px 20px calc(20px + env(safe-area-inset-bottom));border-radius:26px 26px 0 0;background:#fff;box-shadow:0 -24px 60px rgba(28,41,78,.22)}.detail-sheet header{display:flex;align-items:flex-start;justify-content:space-between;gap:14px}.detail-sheet header span{color:#7382a2;font-size:10px;font-weight:850;letter-spacing:.08em}.detail-sheet h2{margin:4px 0 0;color:#16213b;font-size:22px}.detail-close{display:grid;width:36px;height:36px;place-items:center;border:0;border-radius:50%;color:#6f7b92;background:#f1f4f9;font-size:24px}.detail-description{margin:13px 0;color:#7e899f;font-size:12px;line-height:1.65}.detail-price{display:flex;align-items:baseline;gap:7px;padding:13px 0;border-block:1px solid #edf0f6}.detail-price strong{color:#405cda;font-size:28px}.detail-price del{color:#929bad;font-size:11px}.detail-benefits{display:grid;gap:10px;margin:16px 0;padding:0;list-style:none}.detail-benefits li{display:flex;gap:9px;align-items:center;color:#5f6c83;font-size:12px}.detail-benefits i{display:grid;width:18px;height:18px;place-items:center;border-radius:50%;color:#fff;background:#6477e9;font-size:11px;font-style:normal}.detail-done{width:100%;min-height:48px;border:0;border-radius:15px;color:#fff;background:linear-gradient(135deg,#6678ed,#505fda);font-weight:900;box-shadow:0 12px 25px rgba(76,91,208,.25)}
  @media(max-width:370px){.shop-hero h1{font-size:25px}.shop-bag-art{right:8px;transform:scale(.86) rotate(7deg)}.product-card{flex-basis:88%}.product-card-head{grid-template-columns:43px minmax(0,1fr)}.product-icon{width:43px;height:43px}.product-card-head strong{font-size:15px}}
</style>
