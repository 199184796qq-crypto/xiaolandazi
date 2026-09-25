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
    <a href="/support"><span>代客户申请运维协助</span><b>›</b></a>
    <a href="/agent"><span>小蓝工作搭子</span><b>›</b></a>
    <a href="/customers"><span>我的客户</span><b>›</b></a>
    <a href="/performance"><span>我的业绩</span><b>›</b></a>
    <div><span>邀请二维码</span><small>后续接入我的销售邀请码</small></div>
    <div><span>通知</span><small>客户跟进与到期提醒</small></div>
  </div>
  <button class="danger-action" type="button" on:click={signOut}>退出登录</button>
</section>
