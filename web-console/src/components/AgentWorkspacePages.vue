<template>
  <div class="agent-workspace-pages">
    <section class="workspace-page workspace-preview-page" aria-label="生成与预览">
      <div class="workspace-page-content"><slot name="preview" /></div>
    </section>
  </div>
</template>

<style scoped>
.agent-workspace-pages{box-sizing:border-box;display:grid;gap:18px;width:100%;min-width:0;padding:22px;border-radius:18px;background:#f5f7fe;color:#253751;font-size:16px}
.workspace-page{min-width:0}.workspace-page-content{display:grid;grid-template-columns:minmax(0,1fr);gap:20px;min-width:0}.workspace-page-content :deep(>*){min-width:0;max-width:100%;box-sizing:border-box}
@media(max-width:700px){.agent-workspace-pages{padding:14px}}
</style>
