<script lang="ts">
  import { onMount } from 'svelte';
  import { getInvitationDashboard, getReferralWallet, updateOwnInviteCodeStatus } from '$lib/api';
  import type {
    BeneficiaryWalletLedger,
    InvitationDashboard,
    InvitationRecord,
    ReferralWalletDashboard,
  } from '$lib/types';

  type DetailTab = 'records' | 'rewards';

  let dashboard: InvitationDashboard | null = null;
  let referralWallet: ReferralWalletDashboard | null = null;
  let loading = true;
  let refreshing = false;
  let updatingCode = false;
  let error = '';
  let rewardError = '';
  let notice = '';
  let copied = '';
  let origin = '';
  let detailTab: DetailTab = 'records';
  let recordPage = 1;
  const recordPageSize = 6;

  $: registrationUrl = (origin || '') + '/register?invite=' + encodeURIComponent(dashboard?.my_code.code || '');
  $: recordTotalPages = Math.max(1, Math.ceil((dashboard?.records_total || 0) / recordPageSize));

  onMount(() => {
    origin = window.location.origin;
    void load(true);
  });

  async function load(includeRewards = false) {
    if (dashboard) refreshing = true;
    else loading = true;
    error = '';
    if (includeRewards) rewardError = '';
    try {
      const invitationPromise = getInvitationDashboard({
        record_page: recordPage,
        record_page_size: recordPageSize,
      });
      if (!includeRewards) {
        dashboard = await invitationPromise;
        return;
      }
      const [invitationResult, rewardResult] = await Promise.allSettled([
        invitationPromise,
        getReferralWallet(50),
      ]);
      if (invitationResult.status === 'rejected') throw invitationResult.reason;
      dashboard = invitationResult.value;
      if (rewardResult.status === 'fulfilled') referralWallet = rewardResult.value;
      else rewardError = rewardResult.reason instanceof Error ? rewardResult.reason.message : '奖励明细读取失败';
    } catch (value) {
      error = value instanceof Error ? value.message : '读取邀请与推荐数据失败';
    } finally {
      loading = false;
      refreshing = false;
    }
  }

  async function changeRecordPage(page: number) {
    const target = Math.max(1, Math.min(recordTotalPages, page));
    if (target === recordPage || refreshing) return;
    recordPage = target;
    await load(false);
  }

  async function writeClipboard(value: string) {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(value);
      return;
    }
    const input = document.createElement('textarea');
    input.value = value;
    input.setAttribute('readonly', '');
    input.style.position = 'fixed';
    input.style.opacity = '0';
    document.body.appendChild(input);
    input.select();
    const ok = document.execCommand('copy');
    input.remove();
    if (!ok) throw new Error('copy failed');
  }

  function showCopied(label: string, message: string) {
    copied = label;
    notice = message;
    window.setTimeout(() => {
      if (copied === label) copied = '';
      if (notice === message) notice = '';
    }, 1800);
  }

  async function copyText(value: string, label: string) {
    try {
      await writeClipboard(value);
      showCopied(label, label === 'code' ? '邀请码已复制' : '邀请链接已复制');
    } catch {
      notice = '复制失败，请长按邀请码手动复制';
    }
  }

  async function shareInvitation() {
    if (!dashboard || dashboard.my_code.status !== 'active') return;
    const shareData = {
      title: '小蓝直播搭子',
      text: `邀请你使用小蓝直播搭子，我的邀请码是 ${dashboard.my_code.code}`,
      url: registrationUrl,
    };
    if (navigator.share) {
      try {
        await navigator.share(shareData);
        notice = '已打开系统分享';
        return;
      } catch (value) {
        if (value instanceof DOMException && value.name === 'AbortError') return;
      }
    }
    await copyText(registrationUrl, 'link');
  }

  async function toggleOwnCode() {
    if (!dashboard || updatingCode) return;
    updatingCode = true;
    error = '';
    notice = '';
    try {
      const next = dashboard.my_code.status === 'active' ? 'disabled' : 'active';
      await updateOwnInviteCodeStatus(next);
      notice = next === 'active' ? '我的邀请码已启用' : '我的邀请码已停用';
      await load(false);
    } catch (value) {
      error = value instanceof Error ? value.message : '更新邀请码状态失败';
    } finally {
      updatingCode = false;
    }
  }

  function formatDate(value?: string) {
    if (!value) return '长期有效';
    return new Date(value).toLocaleString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    });
  }

  function formatMoney(cents?: number) {
    return '¥' + (Number(cents || 0) / 100).toFixed(2);
  }

  function sourceLabel(source: string) {
    if (source === 'platform_invite') return '平台邀请';
    if (source === 'sales_invite') return '销售邀请';
    if (source === 'agent_invite') return '代理邀请';
    if (source === 'referral') return '我的推荐';
    return source || '邀请注册';
  }

  function rewardTypeLabel(item: BeneficiaryWalletLedger) {
    const labels: Record<string, string> = {
      referral_accrual: '推荐奖励入账',
      referral_release: '奖励转为可提现',
      withdrawal_hold: '提现申请冻结',
      withdrawal_rejected: '提现驳回退回',
      withdrawal_paid: '推荐奖励已打款',
      referral_refund_reversal: '退款奖励冲回',
    };
    return labels[item.business_type] || item.reason || '奖励变动';
  }

  function rewardDelta(item: BeneficiaryWalletLedger) {
    if (item.available_delta_cents !== 0) return item.available_delta_cents;
    return item.frozen_delta_cents;
  }

  function avatarLetter(item: InvitationRecord) {
    return (item.referred_display_name || item.referred_username || '新').slice(0, 1);
  }
</script>

<svelte:head><title>邀请与奖励</title></svelte:head>

<section class="invite-page top-space">
  <header class="invite-page-header">
    <div>
      <span>INVITE & REWARDS</span>
      <h1>邀请有礼</h1>
      <p>分享给身边商家，一起使用小蓝直播搭子</p>
    </div>
    <button type="button" aria-label="刷新邀请数据" disabled={loading || refreshing} on:click={() => load(true)}>
      <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 11a8 8 0 1 0-2.34 5.66M20 4v7h-7"></path></svg>
    </button>
  </header>

  {#if error}
    <button class="invite-error" type="button" on:click={() => load(true)}>{error}，点击重试</button>
  {/if}
  {#if notice}<div class="invite-notice" aria-live="polite">{notice}</div>{/if}

  {#if loading && !dashboard}
    <div class="invite-loading">
      <span></span><span></span><span></span>
      <p>正在读取你的邀请信息…</p>
    </div>
  {:else if dashboard}
    <article class="invite-showcase" class:is-disabled={dashboard.my_code.status !== 'active'}>
      <div class="invite-glow one"></div>
      <div class="invite-glow two"></div>
      <header>
        <div>
          <span>MY INVITE CODE</span>
          <h2>我的邀请码</h2>
        </div>
        <em class:inactive={dashboard.my_code.status !== 'active'}>
          <i></i>{dashboard.my_code.status === 'active' ? '可用' : '已停用'}
        </em>
      </header>

      <div class="invite-illustration" aria-hidden="true">
        <span class="invite-confetti confetti-one"></span>
        <span class="invite-confetti confetti-two"></span>
        <span class="invite-confetti confetti-three"></span>
        <div class="invite-envelope"><i></i><b>✦</b></div>
      </div>

      <div class="invite-code-ticket">
        <span>专属邀请码</span>
        <div>
          <strong>{dashboard.my_code.code}</strong>
          <button type="button" aria-label="复制邀请码" on:click={() => copyText(dashboard?.my_code.code || '', 'code')}>
            {copied === 'code' ? '已复制' : '复制'}
          </button>
        </div>
      </div>

      <div class="invite-stats">
        <div><strong>{dashboard.my_code.used_count}</strong><span>已注册账号</span></div>
        <div><strong>{dashboard.my_code.max_uses || '∞'}</strong><span>使用上限</span></div>
      </div>

      <div class="invite-validity">
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M7 2v3M17 2v3M3.5 9h17M5.5 4h13a2 2 0 0 1 2 2v13a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2Z"></path></svg>
        <span>有效期：{formatDate(dashboard.my_code.expires_at)}</span>
      </div>

      <div class="invite-actions">
        <button class="primary" type="button" disabled={dashboard.my_code.status !== 'active'} on:click={shareInvitation}>
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m21 3-7.6 18-3.1-7.3L3 10.6 21 3Zm-10.7 10.7L15 9"></path></svg>
          分享邀请
        </button>
        <button type="button" disabled={dashboard.my_code.status !== 'active'} on:click={() => copyText(registrationUrl, 'link')}>
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M10 13a5 5 0 0 0 7.1.1l2-2a5 5 0 0 0-7.1-7.1l-1.1 1.1M14 11a5 5 0 0 0-7.1-.1l-2 2A5 5 0 0 0 12 20l1.1-1.1"></path></svg>
          {copied === 'link' ? '已复制' : '复制链接'}
        </button>
      </div>

      <button class="invite-toggle" type="button" disabled={updatingCode} on:click={toggleOwnCode}>
        {updatingCode ? '处理中…' : dashboard.my_code.status === 'active' ? '暂时停用我的邀请码' : '重新启用我的邀请码'}
      </button>
    </article>

    <section class="reward-overview">
      <header>
        <div><span>REWARD WALLET</span><h2>推荐奖励</h2></div>
        <a href="/wallet">查看钱包 <b>›</b></a>
      </header>
      {#if referralWallet}
        <div class="reward-balances">
          <div><small>可提现奖励</small><strong>{formatMoney(referralWallet.wallet.available_balance_cents)}</strong></div>
          <div><small>冻结中</small><strong>{formatMoney(referralWallet.wallet.frozen_balance_cents)}</strong></div>
        </div>
      {:else}
        <p class="reward-unavailable">{rewardError || '奖励账户正在同步'}</p>
      {/if}
      <p>邀请成功后的奖励由后台规则自动计算，到账及冻结变化都记录在奖励明细中。</p>
    </section>

    <section class="invite-details">
      <div class="detail-tabs" role="tablist" aria-label="邀请详情">
        <button type="button" role="tab" aria-selected={detailTab === 'records'} class:active={detailTab === 'records'} on:click={() => (detailTab = 'records')}>
          邀请记录 <span>{dashboard.records_total}</span>
        </button>
        <button type="button" role="tab" aria-selected={detailTab === 'rewards'} class:active={detailTab === 'rewards'} on:click={() => (detailTab = 'rewards')}>
          奖励明细 <span>{referralWallet?.ledger?.length || 0}</span>
        </button>
      </div>

      {#if detailTab === 'records'}
        {#if refreshing}<div class="detail-refreshing">正在刷新…</div>{/if}
        {#if dashboard.records.length}
          <div class="invitation-list" class:is-refreshing={refreshing}>
            {#each dashboard.records as item}
              <article class="invitation-item">
                <span class="invitation-avatar">{avatarLetter(item)}</span>
                <div class="invitation-person">
                  <strong>{item.referred_display_name || item.referred_username}</strong>
                  <small>@{item.referred_username}</small>
                </div>
                <div class="invitation-meta">
                  <em>{sourceLabel(item.source_type)}</em>
                  <time datetime={item.bound_at}>{formatDate(item.bound_at)}</time>
                </div>
              </article>
            {/each}
          </div>
        {:else}
          <div class="detail-empty">
            <span>邀</span>
            <strong>还没有邀请记录</strong>
            <p>把邀请码分享给朋友，注册成功后会显示在这里。</p>
          </div>
        {/if}

        {#if dashboard.records_total > recordPageSize}
          <footer class="record-pagination">
            <button type="button" disabled={recordPage <= 1 || refreshing} on:click={() => changeRecordPage(recordPage - 1)}>←</button>
            <span>{recordPage} / {recordTotalPages}</span>
            <button type="button" disabled={recordPage >= recordTotalPages || refreshing} on:click={() => changeRecordPage(recordPage + 1)}>→</button>
          </footer>
        {/if}
      {:else}
        {#if rewardError}
          <button class="reward-error" type="button" on:click={() => load(true)}>{rewardError}，点击重试</button>
        {:else if referralWallet?.ledger?.length}
          <div class="reward-ledger">
            {#each referralWallet.ledger as item}
              <article>
                <span class:negative={rewardDelta(item) < 0}>{rewardDelta(item) < 0 ? '−' : '+'}</span>
                <div><strong>{rewardTypeLabel(item)}</strong><small>{item.reason}</small><time datetime={item.created_at}>{formatDate(item.created_at)}</time></div>
                <b class:negative={rewardDelta(item) < 0}>{rewardDelta(item) < 0 ? '−' : '+'}{formatMoney(Math.abs(rewardDelta(item)))}</b>
              </article>
            {/each}
          </div>
        {:else}
          <div class="detail-empty">
            <span>奖</span>
            <strong>暂无奖励明细</strong>
            <p>符合后台奖励规则后，奖励记录会自动显示在这里。</p>
          </div>
        {/if}
      {/if}
    </section>
  {/if}
</section>

<style>
  .invite-page{padding:5px 16px 128px;color:#23314b}
  .invite-page-header{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;padding:17px 4px 20px}
  .invite-page-header>div{display:grid;gap:4px}
  .invite-page-header span,.reward-overview header span{color:#8893ab;font-size:10px;font-weight:950;letter-spacing:.13em}
  .invite-page-header h1{margin:0;color:#172742;font-size:31px;letter-spacing:-1.2px}
  .invite-page-header p{margin:2px 0 0;color:#8690a5;font-size:12px}
  .invite-page-header>button{display:grid;width:42px;height:42px;flex:0 0 auto;place-items:center;border:1px solid #e2e6f0;border-radius:14px;background:#fff;color:#6471d7;box-shadow:0 8px 20px rgba(64,75,130,.08)}
  .invite-page-header>button:disabled{opacity:.5}
  .invite-page-header svg{width:20px;height:20px;fill:none;stroke:currentColor;stroke-width:1.9;stroke-linecap:round;stroke-linejoin:round}
  .invite-error,.reward-error{width:100%;margin-bottom:12px;padding:11px 13px;border:0;border-radius:13px;background:#fff0f1;color:#b74e58;font-size:12px;text-align:left}
  .invite-notice{position:fixed;left:50%;top:72px;z-index:90;min-width:150px;max-width:80vw;padding:10px 17px;transform:translateX(-50%);border-radius:999px;background:rgba(31,42,70,.92);box-shadow:0 10px 28px rgba(25,33,58,.22);color:#fff;font-size:12px;font-weight:850;text-align:center;backdrop-filter:blur(10px)}
  .invite-loading{display:grid;min-height:360px;place-items:center;align-content:center;grid-template-columns:repeat(3,10px);gap:7px;border:1px solid #e6e9f2;border-radius:26px;background:#fff;color:#8b94a8}
  .invite-loading span{width:10px;height:10px;border-radius:50%;background:#7a80e3;animation:invite-pulse 1s ease-in-out infinite}
  .invite-loading span:nth-child(2){animation-delay:.15s}.invite-loading span:nth-child(3){animation-delay:.3s}
  .invite-loading p{grid-column:1 / -1;margin:10px 0 0;font-size:12px}
  @keyframes invite-pulse{0%,100%{opacity:.35;transform:translateY(0)}50%{opacity:1;transform:translateY(-5px)}}
  .invite-showcase{position:relative;overflow:hidden;padding:22px;border:1px solid #dadbf8;border-radius:28px;background:linear-gradient(148deg,#f2efff 0%,#e7e8ff 48%,#f8f8ff 100%);box-shadow:0 18px 42px rgba(73,75,158,.13)}
  .invite-showcase.is-disabled{filter:saturate(.62)}
  .invite-glow{position:absolute;border-radius:50%;pointer-events:none}.invite-glow.one{right:-80px;top:-95px;width:230px;height:230px;background:rgba(126,113,232,.13)}.invite-glow.two{left:-80px;bottom:-105px;width:220px;height:220px;background:rgba(255,255,255,.58)}
  .invite-showcase>header{position:relative;z-index:2;display:flex;align-items:flex-start;justify-content:space-between;gap:12px}
  .invite-showcase>header>div{display:grid;gap:3px}.invite-showcase>header span{color:#7c82c6;font-size:9px;font-weight:950;letter-spacing:.12em}.invite-showcase h2{margin:0;color:#2a3159;font-size:21px}
  .invite-showcase header em{display:flex;align-items:center;gap:6px;padding:6px 10px;border-radius:999px;background:rgba(230,248,239,.9);color:#26815f;font-size:10px;font-style:normal;font-weight:900}.invite-showcase header em i{width:7px;height:7px;border-radius:50%;background:#27b77d}.invite-showcase header em.inactive{background:#eceef3;color:#7b8494}.invite-showcase header em.inactive i{background:#9ca4b2}
  .invite-illustration{position:absolute;right:18px;top:57px;width:108px;height:92px}.invite-envelope{position:absolute;right:5px;bottom:4px;width:80px;height:58px;overflow:hidden;transform:rotate(6deg);border:3px solid rgba(255,255,255,.82);border-radius:13px;background:linear-gradient(145deg,#777be4,#9fa4f5);box-shadow:0 14px 24px rgba(79,82,184,.23)}.invite-envelope::before,.invite-envelope::after{content:"";position:absolute;top:-34px;width:58px;height:70px;background:rgba(255,255,255,.22)}.invite-envelope::before{left:-21px;transform:rotate(45deg)}.invite-envelope::after{right:-21px;transform:rotate(-45deg)}.invite-envelope i{position:absolute;left:50%;top:50%;z-index:2;width:32px;height:32px;transform:translate(-50%,-38%);border-radius:50%;background:#fff}.invite-envelope b{position:absolute;left:50%;top:50%;z-index:3;transform:translate(-50%,-43%);color:#7077dc;font-size:18px}.invite-confetti{position:absolute;z-index:3;width:8px;height:16px;border-radius:5px;background:#ffb451}.confetti-one{right:4px;top:1px;transform:rotate(28deg)}.confetti-two{left:10px;top:9px;height:11px;background:#62ceb4;transform:rotate(-35deg)}.confetti-three{left:35px;top:0;width:7px;height:7px;border-radius:50%;background:#ff8ca5}
  .invite-code-ticket{position:relative;z-index:2;width:calc(100% - 94px);min-height:91px;margin-top:24px;padding:13px 14px;border:1px solid rgba(255,255,255,.92);border-radius:18px;background:rgba(255,255,255,.72);box-shadow:0 9px 22px rgba(80,82,160,.08);backdrop-filter:blur(8px)}.invite-code-ticket>span{color:#858ba5;font-size:10px;font-weight:800}.invite-code-ticket>div{display:flex;align-items:center;gap:8px;margin-top:7px}.invite-code-ticket strong{min-width:0;overflow:hidden;color:#4b51b5;font-size:22px;letter-spacing:.08em;text-overflow:ellipsis}.invite-code-ticket button{margin-left:auto;padding:6px 9px;border:0;border-radius:9px;background:#e8eaff;color:#5962cb;font-size:10px;font-weight:900}
  .invite-stats{position:relative;z-index:2;display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-top:17px}.invite-stats>div{display:grid;gap:2px;padding:12px 13px;border:1px solid rgba(255,255,255,.82);border-radius:15px;background:rgba(255,255,255,.47)}.invite-stats strong{color:#42497d;font-size:21px}.invite-stats span{color:#858ca4;font-size:10px}
  .invite-validity{position:relative;z-index:2;display:flex;align-items:center;gap:7px;margin-top:11px;color:#757d9b;font-size:10px}.invite-validity svg{width:15px;height:15px;fill:none;stroke:currentColor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}
  .invite-actions{position:relative;z-index:2;display:grid;grid-template-columns:1.1fr .9fr;gap:9px;margin-top:17px}.invite-actions button{display:flex;min-height:47px;align-items:center;justify-content:center;gap:7px;border:1px solid #d5d8ee;border-radius:14px;background:rgba(255,255,255,.82);color:#5861bd;font-size:12px;font-weight:900}.invite-actions button.primary{border-color:#656ddd;background:linear-gradient(135deg,#686cdc,#565ecb);box-shadow:0 10px 20px rgba(76,82,186,.22);color:#fff}.invite-actions button:disabled{opacity:.45}.invite-actions svg{width:17px;height:17px;fill:none;stroke:currentColor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}
  .invite-toggle{position:relative;z-index:2;width:100%;margin-top:10px;padding:7px;border:0;background:transparent;color:#8a90a9;font-size:10px;text-decoration:underline;text-underline-offset:3px}
  .reward-overview,.invite-details{margin-top:17px;border:1px solid #e2e6ef;border-radius:24px;background:#fff;box-shadow:0 12px 32px rgba(52,65,112,.07)}
  .reward-overview{padding:18px}.reward-overview header{display:flex;align-items:flex-end;justify-content:space-between}.reward-overview header>div{display:grid;gap:3px}.reward-overview h2{margin:0;font-size:18px}.reward-overview header a{color:#6570cf;font-size:11px;font-weight:850}.reward-overview header a b{font-size:18px}.reward-balances{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-top:15px}.reward-balances>div{display:grid;gap:5px;padding:14px;border-radius:16px;background:#f3f5ff}.reward-balances>div+div{background:#fff6ea}.reward-balances small{color:#858da1;font-size:10px}.reward-balances strong{color:#39446f;font-size:21px}.reward-overview>p{margin:13px 1px 0;color:#9299a9;font-size:10px;line-height:1.65}.reward-overview .reward-unavailable{padding:12px;border-radius:13px;background:#f7f8fb;color:#8a92a5;text-align:center}
  .invite-details{overflow:hidden}.detail-tabs{display:grid;grid-template-columns:1fr 1fr;padding:6px;border-bottom:1px solid #edf0f5;background:#fafbfe}.detail-tabs button{min-height:44px;border:0;border-radius:13px;background:transparent;color:#8a92a5;font-size:12px;font-weight:850}.detail-tabs button.active{background:#fff;box-shadow:0 5px 14px rgba(57,68,115,.09);color:#4d58bd}.detail-tabs span{display:inline-grid;min-width:20px;height:20px;margin-left:3px;place-items:center;border-radius:999px;background:#eef0fb;font-size:9px}
  .detail-refreshing{padding:8px 16px 0;color:#858ea5;font-size:10px}.invitation-list,.reward-ledger{display:grid;padding:4px 16px}.invitation-list.is-refreshing{opacity:.55}.invitation-item{display:grid;grid-template-columns:42px minmax(0,1fr) auto;align-items:center;gap:10px;padding:14px 0;border-bottom:1px solid #edf0f5}.invitation-item:last-child{border-bottom:0}.invitation-avatar{display:grid;width:42px;height:42px;place-items:center;border-radius:14px;background:linear-gradient(145deg,#e5e8ff,#d8dcff);color:#5964cd;font-size:16px;font-weight:950}.invitation-person,.invitation-meta{display:grid;min-width:0;gap:3px}.invitation-person strong{overflow:hidden;color:#303a55;font-size:13px;white-space:nowrap;text-overflow:ellipsis}.invitation-person small{color:#9299aa;font-size:9px}.invitation-meta{justify-items:end;text-align:right}.invitation-meta em{padding:3px 7px;border-radius:999px;background:#edf8f3;color:#27815f;font-size:9px;font-style:normal;font-weight:850}.invitation-meta time{color:#a0a6b5;font-size:8px}
  .record-pagination{display:flex;align-items:center;justify-content:center;gap:18px;padding:12px 16px 17px;border-top:1px solid #f0f2f6}.record-pagination button{width:38px;height:36px;border:1px solid #e0e4ee;border-radius:11px;background:#f8f9fc;color:#5e68cb;font-size:16px}.record-pagination button:disabled{opacity:.35}.record-pagination span{color:#7f879a;font-size:10px;font-weight:850}
  .reward-ledger article{display:grid;grid-template-columns:34px minmax(0,1fr) auto;align-items:center;gap:10px;padding:14px 0;border-bottom:1px solid #edf0f5}.reward-ledger article:last-child{border-bottom:0}.reward-ledger article>span{display:grid;width:32px;height:32px;place-items:center;border-radius:11px;background:#eaf8f1;color:#23835e;font-weight:950}.reward-ledger article>span.negative{background:#fff0f1;color:#b5545e}.reward-ledger article>div{display:grid;min-width:0;gap:2px}.reward-ledger strong{color:#384158;font-size:12px}.reward-ledger small{overflow:hidden;color:#9299aa;font-size:9px;white-space:nowrap;text-overflow:ellipsis}.reward-ledger time{color:#a4a9b6;font-size:8px}.reward-ledger article>b{color:#24815f;font-size:12px}.reward-ledger article>b.negative{color:#b5545e}
  .detail-empty{display:grid;min-height:205px;place-items:center;align-content:center;gap:7px;padding:24px;text-align:center}.detail-empty>span{display:grid;width:48px;height:48px;place-items:center;border-radius:16px;background:#eef0ff;color:#6770d2;font-size:17px;font-weight:950}.detail-empty strong{margin-top:3px;color:#50596f;font-size:13px}.detail-empty p{max-width:245px;margin:0;color:#969dad;font-size:10px;line-height:1.6}.reward-error{margin:15px;width:calc(100% - 30px)}
  @media(max-width:370px){.invite-code-ticket{width:calc(100% - 76px)}.invite-code-ticket strong{font-size:18px}.invite-illustration{right:8px;transform:scale(.86);transform-origin:right center}.invite-actions{grid-template-columns:1fr}.reward-balances{grid-template-columns:1fr}}
  @media(prefers-reduced-motion:reduce){.invite-loading span{animation:none}}
</style>
