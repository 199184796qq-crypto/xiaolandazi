<script setup lang="ts">
import TodoBadge from './TodoBadge.vue'
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import {
  resolveNavigationContext,
  type NavigationContextKey,
} from '../navigationUi'
import type { HubKey } from '../moduleUi'
import { session } from '../session'

const props = withDefaults(
  defineProps<{
    context?: NavigationContextKey
    hub?: HubKey
    activeTitle: string
    activeNavTitle?: string
    sectionTitle?: string
    sectionTo?: string
    sectionIcon?: string
    variant?: 'detail' | 'hub'
  }>(),
  {
    variant: 'detail',
    activeNavTitle: '',
    sectionTitle: '',
    sectionTo: '',
    sectionIcon: '',
  },
)

const route = useRoute()

const contextKey = computed<NavigationContextKey>(
  () => props.context || props.hub || 'workspace-auto',
)

const config = computed(() =>
  resolveNavigationContext(contextKey.value, session.bootstrap),
)

const resolvedSectionTitle = computed(
  () => props.sectionTitle || config.value.sectionTitle || '',
)
const resolvedSectionTo = computed(
  () => props.sectionTo || config.value.sectionTo || '',
)
const resolvedSectionIcon = computed(
  () => props.sectionIcon || config.value.sectionIcon || '',
)
const activeNavTitle = computed(
  () => props.activeNavTitle || props.activeTitle,
)

const isHubVariant = computed(() => props.variant === 'hub')
const atRoot = computed(
  () => route.path === config.value.rootTo.split('?')[0],
)


const siblingEntries = computed(() =>
  config.value.entries.filter((entry) => entry.title !== activeNavTitle.value),
)
</script>

<template>
  <Teleport to="#app-global-breadcrumbs">
    <nav class="module-breadcrumbs breadcrumb-nav" aria-label="页面位置">
      <RouterLink :to="config.rootTo" class="breadcrumb-back-chip">
        <span class="breadcrumb-back-icon">{{ atRoot ? '⌂' : '←' }}</span>
        <span>{{ config.rootTitle }}</span>
      </RouterLink>

      <template v-if="isHubVariant && resolvedSectionTitle">
        <span class="breadcrumb-chevron">›</span>
        <strong class="breadcrumb-current-chip">
          <i v-if="resolvedSectionIcon">{{ resolvedSectionIcon }}</i>
          <span>{{ resolvedSectionTitle }}</span>
        </strong>
      </template>

      <template v-else>
        <template v-if="resolvedSectionTitle">
          <span class="breadcrumb-chevron">›</span>
          <RouterLink
            v-if="props.activeTitle !== resolvedSectionTitle && resolvedSectionTo"
            :to="resolvedSectionTo"
            class="breadcrumb-section-chip"
          >
            <i v-if="resolvedSectionIcon">{{ resolvedSectionIcon }}</i>
            <span>{{ resolvedSectionTitle }}</span>
          </RouterLink>
          <strong v-else class="breadcrumb-current-chip">
            <i v-if="resolvedSectionIcon">{{ resolvedSectionIcon }}</i>
            <span>{{ resolvedSectionTitle }}</span>
          </strong>
        </template>

        <template v-if="!resolvedSectionTitle || props.activeTitle !== resolvedSectionTitle">
          <span class="breadcrumb-chevron">›</span>
          <strong class="breadcrumb-current-chip breadcrumb-current-page">
            <span>{{ props.activeTitle }}</span>
          </strong>
        </template>
      </template>
    </nav>
  </Teleport>

  <Teleport
    v-if="!isHubVariant && siblingEntries.length"
    to="#app-global-switches"
  >
    <div
      class="module-page-nav-main module-page-nav-switches"
    >
      <span class="module-sibling-label">切换功能</span>
      <nav class="module-sibling-nav" aria-label="同级功能">
        <RouterLink
          v-for="entry in siblingEntries"
          :key="entry.title + entry.to"
          :to="entry.to"
          class="module-sibling-link"
          active-class="module-sibling-route-parent"
          exact-active-class="active"
        >
          <i>{{ entry.icon }}</i>
          <span>{{ entry.title }}</span>
          <TodoBadge :to="entry.to" />
        </RouterLink>
      </nav>
    </div>
  </Teleport>
</template>
<style scoped>.module-sibling-link{position:relative;padding-right:30px}</style>
