<script setup lang="ts">
import { RouterLink } from 'vue-router'
import ModulePageNav from '../components/ModulePageNav.vue'
import { session } from '../session'

const cards = [
  {
    title: '账户与安全',
    description: '管理头像、账号资料、联系方式、地区信息和登录密码。',
    icon: '安',
    to: '/account',
    badge: '账号资料',
  },
  {
    title: '个人设置',
    description: '调整控制台字号、公屏字号和其他个人显示偏好。',
    icon: '设',
    to: '/settings',
    badge: '个人偏好',
  },
]
</script>

<template>
  <div class="management-page personal-center-page">
    <ModulePageNav
      context="personal-auto"
      active-title="个人中心"
      variant="hub"
    />

    <section class="personal-center-hero">
      <div class="personal-center-hero-icon">我</div>
      <div>
        <p class="section-kicker">MY SPACE</p>
        <h2>个人中心</h2>
        <p>
          {{ session.bootstrap?.actor.display_name || '当前账户' }}的账号安全与个人偏好设置。
        </p>
      </div>
    </section>

    <section class="personal-center-grid">
      <RouterLink
        v-for="card in cards"
        :key="card.title"
        :to="card.to"
        class="personal-center-card"
      >
        <div class="personal-center-card-icon">{{ card.icon }}</div>
        <div class="personal-center-card-copy">
          <div>
            <strong>{{ card.title }}</strong>
            <span>{{ card.badge }}</span>
          </div>
          <p>{{ card.description }}</p>
        </div>
        <b>→</b>
      </RouterLink>
    </section>
  </div>
</template>
