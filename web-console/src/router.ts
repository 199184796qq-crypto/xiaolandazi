import { createRouter, createWebHistory } from 'vue-router'
import LoginView from './views/LoginView.vue'
import RegisterView from './views/RegisterView.vue'
import RoomsView from './views/RoomsView.vue'
import RoomDetailView from './views/RoomDetailView.vue'
import CustomerManagementView from './views/CustomerManagementView.vue'
import SettingsView from './views/SettingsView.vue'
import { loadSession, session } from './session'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: LoginView,
      meta: { public: true },
    },
    {
      path: '/register',
      name: 'register',
      component: RegisterView,
      meta: { public: true },
    },
    {
      path: '/',
      name: 'rooms',
      component: RoomsView,
    },
    {
      path: '/rooms/:id',
      name: 'room-detail',
      component: RoomDetailView,
    },
    {
      path: '/customers',
      name: 'customers',
      component: CustomerManagementView,
      meta: { adminOnly: true },
    },
    {
      path: '/settings',
      name: 'settings',
      component: SettingsView,
    },
  ],
})

router.beforeEach(async (to) => {
  if (!session.initialized) {
    try {
      await loadSession()
    } catch {
      // An unauthenticated response is expected before login.
    }
  }

  const isPublic = to.meta.public === true

  if (isPublic) {
    if (session.bootstrap) {
      return { name: 'rooms' }
    }
    return true
  }

  if (!session.bootstrap) {
    return {
      name: 'login',
      query: {
        redirect: to.fullPath,
      },
    }
  }

  if (
    to.meta.adminOnly === true &&
    session.bootstrap.actor.role !== 'platform_admin'
  ) {
    return { name: 'rooms' }
  }

  return true
})