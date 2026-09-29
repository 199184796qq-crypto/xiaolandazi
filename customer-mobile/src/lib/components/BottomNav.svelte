<script lang="ts">
  import { page } from '$app/stores';

  const items = [
    { href: '/', icon: '⌂', label: '首页' },
    { href: '/shop', icon: '◇', label: '商城' },
    { href: '/orders', icon: '▣', label: '订单' },
    { href: '/invite', icon: '↗', label: '邀请' },
    { href: '/me', icon: '○', label: '我的' },
  ];

  function active(href: string) {
    return href === '/'
      ? $page.url.pathname === '/' || $page.url.pathname.startsWith('/rooms/')
      : $page.url.pathname.startsWith(href);
  }
</script>

<nav class="bottom-nav" aria-label="主导航">
  {#each items as item}
    <a href={item.href} class:active={active(item.href)}>
      <span>{item.icon}</span>
      <small>{item.label}</small>
    </a>
  {/each}
</nav>
