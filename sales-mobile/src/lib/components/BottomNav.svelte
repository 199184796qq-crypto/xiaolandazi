<script lang="ts">
  import { page } from '$app/stores';

  const items = [
    { href: '/', icon: '⌂', label: '工作台' },
    { href: '/customers', icon: '◎', label: '客户' },
    { href: '/business', icon: '◇', label: '业务' },
    { href: '/performance', icon: '▥', label: '业绩' },
    { href: '/me', icon: '○', label: '我的' },
  ];

  function active(href: string) {
    return href === '/' ? $page.url.pathname === '/' : $page.url.pathname.startsWith(href);
  }
</script>

<nav class="bottom-nav" aria-label="销售手机端主导航">
  {#each items as item}
    <a href={item.href} class:active={active(item.href)}>
      <span>{item.icon}</span>
      <small>{item.label}</small>
    </a>
  {/each}
</nav>
