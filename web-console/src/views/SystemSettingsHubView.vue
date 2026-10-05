<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import ModulePageNav from '../components/ModulePageNav.vue'
import { session } from '../session'
import {
  visibleSystemSettingsCategories,
  visibleSystemSettingsGroups,
} from '../systemSettingsCatalog'

const isPlatformAdmin = computed(() => session.bootstrap?.actor.role === 'platform_admin')
const categories = computed(() => visibleSystemSettingsCategories(session.bootstrap))

function visibleEntryCount(category: (typeof categories.value)[number]) {
  return visibleSystemSettingsGroups(category, session.bootstrap)
    .reduce((total, group) => total + group.entries.length, 0)
}
</script>

<template>
  <div class="management-page settings-hub-page">
    <ModulePageNav
      :context="isPlatformAdmin ? 'workspace-admin' : 'workspace-staff'"
      active-title="系统设置"
    />

    <section class="settings-hub-hero">
      <div>
        <p class="section-kicker">SYSTEM CONFIGURATION</p>
        <h2>系统设置</h2>
        <p>配置按业务域分层管理。你只会看到当前岗位有权查看的入口，编辑能力仍由具体权限单独控制。</p>
      </div>
      <div class="settings-hub-summary">
        <strong>{{ categories.length }}</strong>
        <span>个可见配置域</span>
      </div>
    </section>

    <section v-if="categories.length" class="settings-category-grid" aria-label="系统设置分类">
      <RouterLink
        v-for="category in categories"
        :key="category.key"
        :to="category.to"
        class="settings-category-card"
      >
        <div class="settings-category-icon">{{ category.icon }}</div>
        <div class="settings-category-copy">
          <span>{{ category.kicker }}</span>
          <h3>{{ category.title }}</h3>
          <p>{{ category.description }}</p>
          <div class="settings-category-meta">
            <b>{{ visibleSystemSettingsGroups(category, session.bootstrap).length }} 个分组</b>
            <b>{{ visibleEntryCount(category) }} 项配置</b>
          </div>
        </div>
        <i class="settings-category-arrow">›</i>
      </RouterLink>
    </section>

    <section v-else class="settings-empty-state">
      <strong>当前岗位没有可查看的系统配置</strong>
      <p>如需查看或维护配置，请由管理员为你的岗位分配相应权限。</p>
    </section>
  </div>
</template>

<style scoped>
.settings-hub-page{display:grid;gap:18px}.settings-hub-hero{display:flex;align-items:center;justify-content:space-between;gap:24px;padding:25px 28px;border:1px solid #e3e8f2;border-radius:22px;background:linear-gradient(120deg,#fff 0%,#f5f8ff 58%,#eef2ff 100%);box-shadow:0 16px 42px rgba(41,55,94,.07)}.settings-hub-hero h2,.settings-hub-hero p{margin:0}.settings-hub-hero h2{margin-top:4px;color:#202a3f;font-size:30px}.settings-hub-hero>div>p:last-child{max-width:760px;margin-top:8px;color:#748097;font-size:14px;line-height:1.7}.settings-hub-summary{display:grid;min-width:120px;padding:14px 18px;border:1px solid rgba(82,103,212,.13);border-radius:16px;background:rgba(255,255,255,.72);text-align:center}.settings-hub-summary strong{color:#5065d9;font-size:30px}.settings-hub-summary span{color:#8490a5;font-size:12px}.settings-category-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:15px}.settings-category-card{position:relative;display:grid;grid-template-columns:50px minmax(0,1fr) 22px;gap:14px;min-height:168px;box-sizing:border-box;padding:20px;border:1px solid #e2e7f0;border-radius:19px;color:inherit;background:#fff;box-shadow:0 12px 30px rgba(42,55,87,.055);text-decoration:none;transition:transform .18s ease,border-color .18s ease,box-shadow .18s ease}.settings-category-card:hover{transform:translateY(-2px);border-color:#aab8f4;box-shadow:0 18px 38px rgba(66,83,165,.11)}.settings-category-icon{display:grid;place-items:center;width:48px;height:48px;border-radius:15px;color:#fff;background:linear-gradient(145deg,#6f83ee,#5367d4);box-shadow:0 9px 20px rgba(82,103,212,.22);font-weight:900}.settings-category-copy>span{color:#919bb0;font-size:10px;font-weight:900;letter-spacing:.13em}.settings-category-copy h3{margin:4px 0 7px;color:#273149;font-size:19px}.settings-category-copy p{min-height:43px;margin:0;color:#7d889e;font-size:12px;line-height:1.65}.settings-category-meta{display:flex;flex-wrap:wrap;gap:7px;margin-top:13px}.settings-category-meta b{padding:5px 8px;border-radius:999px;color:#5f6f9f;background:#f0f3fb;font-size:10px}.settings-category-arrow{align-self:center;color:#8795bf;font-size:28px;font-style:normal}.settings-empty-state{padding:48px;border:1px dashed #d8deea;border-radius:20px;background:#fff;text-align:center}.settings-empty-state strong{color:#344058}.settings-empty-state p{margin:7px 0 0;color:#8994a7}@media(max-width:1080px){.settings-category-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:680px){.settings-hub-hero{align-items:flex-start;flex-direction:column;padding:20px}.settings-hub-summary{box-sizing:border-box;width:100%}.settings-category-grid{grid-template-columns:1fr}.settings-category-card{min-height:0}}
</style>
