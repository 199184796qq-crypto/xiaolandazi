<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { logout } from './api'
import { clearSession, session } from './session'

const route = useRoute()
const router = useRouter()

const loggingOut = ref(false)

const isAuthPage = computed(() => route.meta.public === true)
const isRoomsPage = computed(
  () => route.name === 'rooms' || route.name === 'room-detail',
)
const isCustomersPage = computed(() => route.name === 'customers')
const isSettingsPage = computed(() => route.name === 'settings')
const actor = computed(() => session.bootstrap?.actor)
const isAdmin = computed(() => actor.value?.role === 'platform_admin')

async function signOut() {
  if (loggingOut.value) return

  loggingOut.value = true
  try {
    await logout()
  } catch {
    // Clear local state even if the server session has already expired.
  } finally {
    clearSession()
    loggingOut.value = false
    await router.replace('/login')
  }
}
</script>

<template>
  <RouterView v-if="isAuthPage" />

  <div v-else class="app-shell">
    <aside class="sidebar">
      <div class="brand">
        <div class="brand-mark">蓝</div>
        <div>
          <strong>小蓝搭子</strong>
          <span>BANBO AI</span>
        </div>
      </div>

      <nav class="nav-list">
        <RouterLink class="nav-item" :class="{ active: isRoomsPage }" to="/">
          <span class="nav-icon">▣</span>
          <span>直播间</span>
        </RouterLink>

        <RouterLink
          v-if="isAdmin"
          class="nav-item"
          :class="{ active: isCustomersPage }"
          to="/customers"
        >
          <span class="nav-icon">◎</span>
          <span>客户管理</span>
        </RouterLink>

        <div class="nav-item disabled">
          <span class="nav-icon">◉</span>
          <span>设备</span>
          <span class="soon">稍后</span>
        </div>

        <div class="nav-item disabled">
          <span class="nav-icon">✦</span>
          <span>AI 伴播</span>
          <span class="soon">稍后</span>
        </div>

        <div class="nav-item disabled">
          <span class="nav-icon">◌</span>
          <span>用量中心</span>
          <span class="soon">稍后</span>
        </div>

        <RouterLink
          class="nav-item"
          :class="{ active: isSettingsPage }"
          to="/settings"
        >
          <span class="nav-icon">⚙</span>
          <span>设置</span>
        </RouterLink>
      </nav>

      <div class="sidebar-footer">
        <div class="service-dot-row">
          <span class="service-dot"></span>
          <span>管理服务已连接</span>
        </div>
        <span class="version">V1 本地开发版</span>
      </div>
    </aside>

    <main class="main-area">
      <header class="topbar">
        <div>
          <p class="eyebrow">控制台</p>
          <h1>{{ isAdmin ? '平台管理' : '客户工作台' }}</h1>
        </div>

        <div class="topbar-actions">
          <div class="account-chip">
            <div class="avatar">
              {{ actor?.display_name?.slice(0, 1) || '用' }}
            </div>
            <div>
              <strong>{{ actor?.display_name || '正在加载' }}</strong>
              <span>
                {{
                  isAdmin
                    ? '平台管理员 · ' + (actor?.username || '')
                    : '客户账号 · ' + (actor?.username || '')
                }}
              </span>
            </div>
          </div>

          <button
            class="logout-button"
            type="button"
            :disabled="loggingOut"
            @click="signOut"
          >
            {{ loggingOut ? '退出中...' : '退出登录' }}
          </button>
        </div>
      </header>

      <div v-if="session.error && session.bootstrap" class="global-error">
        {{ session.error }}
      </div>

      <section class="page-content">
        <RouterView />
      </section>
    </main>

    <nav
      class="mobile-nav"
      :class="{ 'has-admin-item': isAdmin }"
      aria-label="移动端导航"
    >
      <RouterLink
        class="mobile-nav-item"
        :class="{ active: isRoomsPage }"
        to="/"
      >
        <span class="mobile-nav-icon">▣</span>
        <span>直播间</span>
      </RouterLink>

      <RouterLink
        v-if="isAdmin"
        class="mobile-nav-item"
        :class="{ active: isCustomersPage }"
        to="/customers"
      >
        <span class="mobile-nav-icon">◎</span>
        <span>客户</span>
      </RouterLink>

      <RouterLink
        class="mobile-nav-item"
        :class="{ active: isSettingsPage }"
        to="/settings"
      >
        <span class="mobile-nav-icon">⚙</span>
        <span>设置</span>
      </RouterLink>
    </nav>
  </div>
</template>