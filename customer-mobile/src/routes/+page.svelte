<script lang="ts">
  import { onMount } from 'svelte';
  import { getPublicSystemConfig } from '$lib/api';
  import { session } from '$lib/session';
  let agentName = '小蓝直播搭子';

  onMount(async () => {
    try {
      const config = await getPublicSystemConfig();
      agentName = config.client_agent_name?.trim() || agentName;
    } catch {
      // Keep the lightweight shell usable while secondary APIs recover.
    }
  });

  $: displayName = $session.bootstrap?.actor.display_name || $session.bootstrap?.actor.username || '用户';
</script>

<svelte:head><title>{agentName}</title></svelte:head>

<section class="page-section top-space">
  <header class="mobile-header">
    <div>
      <span class="eyebrow">你好，{displayName}</span>
      <h1>{agentName}</h1>
    </div>
    <a class="mini-agent" href="/agent">✦</a>
  </header>

  <a class="agent-hero" href="/agent">
    <div>
      <span class="status-dot"></span><small>智能体在线</small>
      <h2>有事直接跟我说</h2>
      <p>查自己的会员、时长、订单和直播状态；不碰采集、公屏和复杂运维。</p>
    </div>
    <span class="hero-arrow">→</span>
  </a>


  <div class="section-title"><div><span>快捷服务</span><h2>手机上只放高频操作</h2></div></div>
  <div class="quick-grid">
    <a href="/shop"><b>◇</b><span>会员与时长</span><small>续费、买卡、设备</small></a>
    <a href="/orders"><b>▣</b><span>订单售后</span><small>订单、物流、售后</small></a>
    <a href="/invite"><b>↗</b><span>邀请推荐</span><small>邀请码与奖励</small></a>
    <a href="/me"><b>○</b><span>我的设备</span><small>只看基础在线状态</small></a>
  </div>

  <div class="notice-card">
    <span>手机端边界</span>
    <p>直播采集、公屏、弹幕、Chrome 采集、复杂策略和设备调试都留在电脑端。</p>
  </div>
</section>
