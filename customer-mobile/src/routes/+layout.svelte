<script lang="ts">
  import '../app.css';
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import BottomNav from '$lib/components/BottomNav.svelte';
  import FloatingRoomPlayer from '$lib/components/FloatingRoomPlayer.svelte';
  import { unlockCustomerAudio } from '$lib/audioRuntime';
  import { loadSession, session } from '$lib/session';

  let ready = false;
  $: isLogin = $page.url.pathname === '/login';

  onMount(() => {
    const unlock = () => {
      void unlockCustomerAudio().catch(() => undefined);
    };
    window.addEventListener('pointerdown', unlock, { capture: true, passive: true });

    void (async () => {
      const current = window.location;
      if (current.hostname === '127.0.0.1' || current.hostname === 'localhost') {
        const target =
          current.protocol +
          '//customer.localhost:' +
          current.port +
          current.pathname +
          current.search +
          current.hash;
        window.location.replace(target);
        return;
      }

      if (isLogin) {
        ready = true;
        return;
      }

      const fallbackTimer = window.setTimeout(() => {
        if (!ready && window.location.pathname !== '/login') {
          window.location.replace('/login');
        }
      }, 8000);

      try {
        const bootstrap = await loadSession();
        if (bootstrap.actor.role !== 'customer') throw new Error('role');
        ready = true;
      } catch {
        window.location.replace('/login');
      } finally {
        window.clearTimeout(fallbackTimer);
      }
    })();

    return () => {
      window.removeEventListener('pointerdown', unlock, true);
    };
  });
</script>

{#if isLogin}
  <slot />
{:else if ready && $session.bootstrap}
  <div class="mobile-shell"><slot /></div>
  <FloatingRoomPlayer />
  <BottomNav />
{:else}
  <div class="boot-screen">
    <div class="brand-orb">✦</div>
    <p>正在进入小蓝直播搭子…</p>
  </div>
{/if}
