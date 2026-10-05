<script lang="ts">
  import { page } from '$app/stores';

  const items = [
    { id: 'home', href: '/', label: '首页', tone: 'blue' },
    { id: 'shop', href: '/shop', label: '商城', tone: 'purple' },
    { id: 'wallet', href: '/wallet', label: '钱包', tone: 'wallet', featured: true },
    { id: 'invite', href: '/invite', label: '邀请', tone: 'orange' },
    { id: 'me', href: '/me', label: '我的', tone: 'green' },
  ];

  function active(href: string) {
    return href === '/'
      ? $page.url.pathname === '/' || $page.url.pathname.startsWith('/rooms/')
      : $page.url.pathname.startsWith(href);
  }
</script>

<nav class="bottom-nav" aria-label="主导航">
  {#each items as item}
    <a
      href={item.href}
      class:active={active(item.href)}
      class:featured={item.featured}
      data-tone={item.tone}
      aria-current={active(item.href) ? 'page' : undefined}
    >
      <span class="nav-icon" aria-hidden="true">
        {#if item.id === 'home'}
          <svg viewBox="0 0 24 24"><path d="M3.5 10.6 12 3.7l8.5 6.9v8.7a1.7 1.7 0 0 1-1.7 1.7h-4.2v-6.3H9.4V21H5.2a1.7 1.7 0 0 1-1.7-1.7z"/><path d="m2.2 11.5 9.1-7.4a1.1 1.1 0 0 1 1.4 0l9.1 7.4"/></svg>
        {:else if item.id === 'shop'}
          <svg viewBox="0 0 24 24"><path d="M5.2 8.4h13.6l-.8 12H6z"/><path d="M8.7 9V6.8a3.3 3.3 0 0 1 6.6 0V9"/></svg>
        {:else if item.id === 'wallet'}
          <svg viewBox="0 0 24 24"><path d="m7.2 5.1 4.8 6.1 4.8-6.1M12 11.2v8.1M7.8 12h8.4M7.8 15.5h8.4"/></svg>
        {:else if item.id === 'invite'}
          <svg viewBox="0 0 24 24"><path d="m3 10.6 17.6-7-5.9 16.8-3.3-6.7z"/><path d="m11.4 13.7 9.2-10.1"/><circle cx="18.8" cy="18.6" r="2.1"/></svg>
        {:else}
          <svg viewBox="0 0 24 24"><circle cx="12" cy="7.4" r="4.2"/><path d="M4.7 20.2c.4-4.2 3-6.4 7.3-6.4s6.9 2.2 7.3 6.4z"/><path d="M9.2 17.4h5.6"/></svg>
        {/if}
      </span>
      <small>{item.label}</small>
      <i class="active-mark" aria-hidden="true"></i>
    </a>
  {/each}
</nav>

<style>
  .bottom-nav{position:fixed;left:50%;bottom:0;z-index:50;width:min(calc(100% - 20px),520px);height:calc(78px + env(safe-area-inset-bottom));transform:translateX(-50%);display:grid;grid-template-columns:repeat(5,1fr);align-items:end;padding:8px 7px calc(7px + env(safe-area-inset-bottom));border:1px solid rgba(255,255,255,.9);border-bottom:0;border-radius:27px 27px 0 0;background:linear-gradient(180deg,rgba(255,255,255,.97),rgba(249,251,255,.97));box-shadow:0 15px 42px rgba(63,79,142,.2),inset 0 1px 0 #fff;backdrop-filter:blur(22px);-webkit-backdrop-filter:blur(22px)}
  .bottom-nav a{position:relative;display:grid;justify-items:center;align-content:end;gap:3px;min-width:0;height:62px;padding:2px 1px 3px;color:#8a96ad;-webkit-tap-highlight-color:transparent}
  .bottom-nav a:not(.featured){transform:translateY(-17px)}
  .nav-icon{display:grid;width:36px;height:36px;place-items:center;border-radius:13px;color:#fff;transition:transform .2s ease,filter .2s ease,box-shadow .2s ease}
  .nav-icon svg{width:25px;height:25px;overflow:visible;fill:currentColor;stroke:currentColor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}
  [data-tone='blue'] .nav-icon{background:linear-gradient(145deg,#79c9ff,#3970f4);box-shadow:0 7px 16px rgba(55,116,235,.24)}
  [data-tone='purple'] .nav-icon{background:linear-gradient(145deg,#9d8bff,#6554e8);box-shadow:0 7px 16px rgba(100,81,223,.24)}
  [data-tone='orange'] .nav-icon{background:linear-gradient(145deg,#ffc465,#ff8d31);box-shadow:0 7px 16px rgba(247,148,49,.23)}
  [data-tone='green'] .nav-icon{background:linear-gradient(145deg,#61e2bd,#13b896);box-shadow:0 7px 16px rgba(20,181,143,.23)}
  .bottom-nav small{font-size:10px;font-weight:850;line-height:1.1;transition:color .2s ease}
  .active-mark{display:block;width:19px;height:3px;border-radius:99px;background:transparent;transition:background .2s ease,box-shadow .2s ease}
  .bottom-nav a.active{background:transparent}
  .bottom-nav a.active:not(.featured){color:#5369dc}
  .bottom-nav a.active:not(.featured) .nav-icon{transform:translateY(-2px) scale(1.06);filter:saturate(1.08) brightness(1.03)}
  .bottom-nav a.active .active-mark{background:linear-gradient(90deg,#3f7bff,#6655ed);box-shadow:0 3px 8px rgba(78,96,227,.28)}
  .bottom-nav a.featured{height:85px;align-content:start;transform:translateY(-18px);color:#64718b}
  .bottom-nav a.featured::before{content:'';position:absolute;top:-5px;width:66px;height:66px;border-radius:50%;background:rgba(255,255,255,.95);box-shadow:0 8px 25px rgba(84,101,209,.14),inset 0 0 0 1px rgba(224,231,255,.95)}
  .featured .nav-icon{position:relative;width:56px;height:56px;border-radius:50%;background:linear-gradient(145deg,#3ebcf5 0%,#4b7af7 45%,#7352ef 100%);box-shadow:0 10px 27px rgba(80,91,224,.42),0 0 0 5px rgba(238,242,255,.96);z-index:1}
  .featured .nav-icon svg{width:33px;height:33px;fill:none;stroke:#fff;stroke-width:2.3}
  .featured small{position:relative;margin-top:5px;font-size:11px;z-index:1}
  .featured .active-mark{position:relative;z-index:1;width:24px}
  .bottom-nav a.featured.active{color:#4368e8}
  .bottom-nav a:active .nav-icon{transform:scale(.94)}
  .bottom-nav a.featured:active .nav-icon{transform:scale(.95)}
  @media(max-width:360px){.bottom-nav{width:calc(100% - 12px);border-radius:23px 23px 0 0}.bottom-nav a{height:59px}.nav-icon{width:33px;height:33px}.bottom-nav small{font-size:9px}}
  @media(prefers-reduced-motion:reduce){.nav-icon,.active-mark,.bottom-nav small{transition:none}}
</style>
