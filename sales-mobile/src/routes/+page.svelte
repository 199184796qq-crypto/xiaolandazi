<script lang="ts">
  import InboxBadge from '$lib/InboxBadge.svelte';
  import { onMount } from 'svelte';
  import { getPublicSystemConfig } from '$lib/api';
  import { session } from '$lib/session';
  let agentName = '小蓝工作搭子';

  onMount(async () => {
    try {
      const config = await getPublicSystemConfig();
      agentName = config.internal_agent_name?.trim() || agentName;
    } catch {}
  });

  $: displayName = $session.bootstrap?.actor.display_name || $session.bootstrap?.actor.username || '销售';
</script>

<section class="page-section top-space">
  <header class="mobile-header">
    <div><span class="eyebrow">外勤工作台 · {displayName}</span><h1>销售工作台</h1></div>
    <a class="mini-agent" href="/agent" style="position:relative">✦<InboxBadge/></a>
  </header>

  <a class="agent-hero" href="/agent">
    <div>
      <span class="status-dot"></span><small>{agentName}</small>
      <h2>查客户、做跟进、看业绩</h2>
      <p>只在你的销售权限范围内工作，不开放充值、退款、奖励、财务审批和系统设定。</p>
    </div>
    <span class="hero-arrow">→</span>
  </a>


  <div class="section-title"><div><span>今日常用</span><h2>拿出手机就能办</h2></div></div>
  <div class="quick-grid">
    <a href="/customers"><b>◎</b><span>我的客户</span><small>查资料与跟进</small></a>
    <a href="/business"><b>◇</b><span>产品方案</span><small>会员、时长、设备</small></a>
    <a href="/performance"><b>▥</b><span>我的业绩</span><small>销售额与提成</small></a>
    <a href="/agent"><b>✦</b><span>{agentName}</span><small>直接问当前工作</small></a>
  </div>

  <div class="notice-card">
    <span>手机端原则</span>
    <p>只做销售每天在外面真正会用的事，不做完整 CRM，也不把财务和后台管理能力搬进来。</p>
  </div>
</section>
