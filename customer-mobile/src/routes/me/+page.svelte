<script lang="ts">
  import { goto } from '$app/navigation';
  import { logout } from '$lib/api';
  import { clearSession, session } from '$lib/session';

  async function signOut() {
    await logout().catch(() => undefined);
    clearSession();
    await goto('/login');
  }
</script>

<section class="page-section top-space">
  <header class="mobile-header"><div><span class="eyebrow">我的</span><h1>{$session.bootstrap?.actor.display_name || $session.bootstrap?.actor.username}</h1></div></header>
  <div class="settings-list">
    <a href="/support"><span>申请运维协助 / 工单进度</span><b>›</b></a>
    <a href="/agent"><span>小蓝直播搭子</span><b>›</b></a>
    <a href="/me/addressing"><span>直播称呼策略</span><small>系统称呼 / 我的称呼</small><b>›</b></a>
    <a href="/shop"><span>会员与时长</span><b>›</b></a>
    <a href="/orders"><span>订单与售后</span><b>›</b></a>
    <a href="/invite"><span>邀请与奖励</span><b>›</b></a>
    <div><span>设备状态</span><small>仅查看基础在线状态</small></div>
  </div>
  <button class="danger-action" type="button" on:click={signOut}>退出登录</button>
</section>
