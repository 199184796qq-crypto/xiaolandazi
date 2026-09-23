<script setup lang="ts">
import PasswordInput from './components/PasswordInput.vue'
import GlobalFeedback from './components/GlobalFeedback.vue'
import RegionSelect from './components/RegionSelect.vue'
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  changePassword,
  getAccountDashboard,
  logout,
  updateAccountProfile,
} from './api'
import { clearSession, loadSession, session } from './session'

interface NavItem {
  label: string
  to: string
  icon: string
  routeNames: string[]
}

interface NavSection {
  label: string
  items: NavItem[]
}

const route = useRoute()
const router = useRouter()

const loggingOut = ref(false)

const isAuthPage = computed(() => route.meta.public === true)
const actor = computed(() => session.bootstrap?.actor)
const staffAccess = computed(() => session.bootstrap?.staff_access ?? null)

const isAdmin = computed(() => actor.value?.role === 'platform_admin')
const isAgent = computed(() => actor.value?.role === 'agent_admin')
const isSales = computed(() => actor.value?.role === 'sales_staff')
const isStaff = computed(() => actor.value?.role === 'staff')
const isInternalStaff = computed(
  () => isAdmin.value || isSales.value || isStaff.value,
)

function hasStaffPermission(code: string) {
  const access = staffAccess.value
  return Boolean(
    access &&
      (access.is_super_admin || access.permissions.includes(code)),
  )
}

function navItem(
  label: string,
  to: string,
  icon: string,
  routeNames: string[],
): NavItem {
  return { label, to, icon, routeNames }
}

const navSections = computed<NavSection[]>(() => {
  if (isAdmin.value) {
    return [
      {
        label: '系统',
        items: [
          navItem('系统总览', '/overview', '⌂', ['platform-overview']),
          navItem('组织架构', '/staff', '♜', ['staff-hub', 'staff-groups', 'staff-employees', 'staff-roles', 'staff-approvals', 'staff-audit']),
        ],
      },
      {
        label: '运营管理',
        items: [
          navItem('直播运维', '/operations/live', '▣', ['live-hub', 'live-monitor', 'live-events', 'rooms', 'rooms-list', 'room-detail']),
          navItem('终端资源', '/customers', '◎', ['customers-hub', 'customers-list']),
          navItem('代理体系', '/agents', '◇', ['agents-hub', 'agents-list', 'agent-levels', 'agent-contracts', 'agent-exit']),
          navItem('销售体系', '/sales', '◈', ['sales-hub', 'sales-team', 'sales-performance']),
          navItem('邀请与推荐', '/invitations', '↗', ['invitations']),
        ],
      },
      {
        label: '商业管理',
        items: [
          navItem(
            '商品与会员',
            '/commercial/memberships',
            '◆',
            ['commercial-hub', 'commercial-membership-plans', 'commercial-membership-simulator', 'commercial-time-cards', 'commercial-referrals', 'commercial-settlement'],
          ),
          navItem('财务结算', '/staff/finance', '¥', ['staff-finance-hub', 'staff-finance-accounts', 'staff-finance-approvals', 'staff-finance-ledger', 'staff-finance-history', 'staff-finance-trace', 'staff-finance-settlements', 'staff-finance-ai-time']),
          navItem('设备库存', '/resources', '◌', ['resources-hub', 'resource-devices', 'resource-inventory', 'resource-logistics']),
          navItem('售后维修', '/staff/after-sales', '修', ['staff-after-sales']),
        ],
      },
    ]
  }

  if (isAgent.value) {
    return [
      {
        label: '代理工作台',
        items: [
          navItem('代理总览', '/agent/overview', '⌂', ['agent-overview']),
          navItem('终端管理', '/agent/customers', '◎', ['agent-customers']),
          navItem('AI 时长', '/resources/workspace', '时', ['resources-workspace']),
          navItem('售后维修', '/after-sales', '修', ['after-sales-portal']),
          navItem('邀请与推荐', '/invitations', '↗', ['invitations']),
        ],
      },
    ]
  }

  if (isInternalStaff.value) {
    const workItems: NavItem[] = []

    if (hasStaffPermission('system.architecture.view')) {
      workItems.push(
        navItem('系统总览', '/overview', '⌂', ['platform-overview']),
        navItem('直播运维', '/operations/live', '▣', ['live-hub', 'live-monitor', 'live-events', 'rooms', 'rooms-list', 'room-detail']),
      )
    }

    if (
      hasStaffPermission('system.architecture.view') ||
      hasStaffPermission('staff.group.view') ||
      hasStaffPermission('staff.employee.view') ||
      hasStaffPermission('staff.role.view')
    ) {
      workItems.push(
        navItem(
          '组织架构',
          '/staff',
          '♜',
          ['staff-hub', 'staff-groups', 'staff-employees', 'staff-roles', 'staff-approvals', 'staff-audit'],
        ),
      )
    }

    if (isSales.value) {
      workItems.push(
        navItem('我的终端', '/sales/customers', '◎', ['sales-customers']),
      )
    }
    if (hasStaffPermission('customer.view_all')) {
      workItems.push(navItem('终端资源', '/customers', '◎', ['customers-hub', 'customers-list']))
    }
    if (hasStaffPermission('agent.view_all')) {
      workItems.push(navItem('代理体系', '/agents', '◇', ['agents-hub', 'agents-list', 'agent-levels', 'agent-contracts', 'agent-exit']))
    }
    if (hasStaffPermission('sales.view_all')) {
      workItems.push(navItem('销售体系', '/sales', '◈', ['sales-hub', 'sales-team', 'sales-performance']))
    }
    if (hasStaffPermission('finance.dashboard.view')) {
      workItems.push(
        navItem('财务结算', '/staff/finance', '¥', ['staff-finance-hub', 'staff-finance-accounts', 'staff-finance-approvals', 'staff-finance-ledger', 'staff-finance-history', 'staff-finance-trace', 'staff-finance-settlements', 'staff-finance-ai-time']),
      )
    }

    if (
      hasStaffPermission('commercial.membership.view')
    ) {
      workItems.push(
        navItem(
          '会员方案',
          '/commercial/memberships',
          '◆',
          ['commercial-hub', 'commercial-membership-plans', 'commercial-membership-simulator', 'commercial-time-cards', 'commercial-referrals', 'commercial-settlement'],
        ),
      )
    }
    if (
      hasStaffPermission('inventory.view') ||
      hasStaffPermission('logistics.view')
    ) {
      workItems.push(navItem('设备库存', '/resources', '◌', ['resources-hub', 'resource-devices', 'resource-inventory', 'resource-logistics']))
    }
    if (hasStaffPermission('inventory.after_sales.view')) {
      workItems.push(navItem('售后维修', '/staff/after-sales', '修', ['staff-after-sales']))
    }
    if (isSales.value || hasStaffPermission('invitations.view_all')) {
      workItems.push(
        navItem('邀请与推荐', '/invitations', '↗', ['invitations']),
      )
    }

    return [
      {
        label: staffAccess.value?.primary_group_name || '内部员工',
        items: workItems,
      },
    ]
  }

  return [
    {
      label: '终端工作台',
      items: [
        navItem('直播运维', '/', '▣', ['rooms', 'room-detail']),
        navItem('商城', '/shop', '▤', ['shop']),
        navItem('财务管理', '/finance', '¥', ['finance']),
        navItem('售后维修', '/after-sales', '修', ['after-sales-portal']),
        navItem('邀请与推荐', '/invitations', '↗', ['invitations']),
      ],
    },
  ]
})

function navActive(item: NavItem) {
  return item.routeNames.includes(String(route.name || ''))
}

const collapsedNavSections = ref<Record<string, boolean>>({})

function sectionHasActive(section: NavSection) {
  return section.items.some((item) => navActive(item))
}

function sectionCollapsed(section: NavSection) {
  return collapsedNavSections.value[section.label] === true
}

function toggleNavSection(section: NavSection) {
  collapsedNavSections.value = {
    ...collapsedNavSections.value,
    [section.label]: !sectionCollapsed(section),
  }
}


watch(
  () => String(route.name || ''),
  () => {
    const activeSection = navSections.value.find((section) => sectionHasActive(section))
    if (activeSection) {
      collapsedNavSections.value = {
        ...collapsedNavSections.value,
        [activeSection.label]: false,
      }
    }

  },
  { immediate: true },
)

const mobileNavItems = computed<NavItem[]>(() => {
  if (isAdmin.value) {
    return [
      navItem('系统总览', '/overview', '⌂', ['platform-overview']),
      navItem('组织', '/staff', '♜', ['staff-hub', 'staff-groups', 'staff-employees', 'staff-roles', 'staff-approvals', 'staff-audit']),
      navItem('终端', '/customers', '◎', ['customers-hub', 'customers-list']),
      navItem('商业', '/commercial/memberships', '◆', [
        'commercial-hub',
        'commercial-membership-plans',
        'commercial-membership-simulator',
        'resources-hub',
        'resources-workspace',
      ]),
      navItem('我的', '/personal', '♙', ['personal-center', 'account', 'settings']),
    ]
  }

  if (isAgent.value) {
    return [
      navItem('总览', '/agent/overview', '⌂', ['agent-overview']),
      navItem('终端', '/agent/customers', '◎', ['agent-customers']),
      navItem('资源', '/resources/workspace', '时', ['resources-workspace']),
      navItem('售后', '/after-sales', '修', ['after-sales-portal']),
      navItem('我的', '/personal', '♙', ['personal-center', 'account', 'settings']),
    ]
  }

  if (isSales.value) {
    return [
      navItem('终端', '/sales/customers', '◎', ['sales-customers']),
      navItem('组织', '/staff', '♜', ['staff-hub', 'staff-groups', 'staff-employees', 'staff-roles', 'staff-approvals', 'staff-audit']),
      navItem('邀请', '/invitations', '↗', ['invitations']),
      navItem('我的', '/personal', '♙', ['personal-center', 'account', 'settings']),
    ]
  }

  if (isStaff.value) {
    const items: NavItem[] = [
      navItem('组织', '/staff', '♜', ['staff-hub', 'staff-groups', 'staff-employees', 'staff-roles', 'staff-approvals', 'staff-audit']),
    ]
    if (hasStaffPermission('system.architecture.view')) {
      items.unshift(navItem('系统', '/overview', '⌂', ['platform-overview']))
    }
    if (hasStaffPermission('finance.dashboard.view')) {
      items.push(
        navItem('财务', '/staff/finance', '¥', ['staff-finance-hub', 'staff-finance-accounts', 'staff-finance-approvals', 'staff-finance-ledger', 'staff-finance-history', 'staff-finance-trace', 'staff-finance-settlements']),
      )
    }
    if (hasStaffPermission('customer.view_all')) {
      items.push(navItem('终端', '/customers', '◎', ['customers-hub', 'customers-list']))
    }
    if (
      hasStaffPermission('resources.view') ||
      hasStaffPermission('finance.resource.adjust')
    ) {
      items.push(navItem('资源', '/resources', '◌', ['resources-hub', 'resources-workspace', 'resource-devices', 'resource-inventory', 'resource-logistics']))
    } else if (hasStaffPermission('commercial.membership.view')) {
      items.push(
        navItem('商业', '/commercial/memberships', '◆', [
          'commercial-hub',
          'commercial-membership-plans',
          'commercial-membership-simulator',
        ]),
      )
    }
    if (hasStaffPermission('inventory.after_sales.view')) {
      items.push(navItem('售后', '/staff/after-sales', '修', ['staff-after-sales']))
    }
    items.push(navItem('我的', '/personal', '♙', ['personal-center', 'account', 'settings']))
    return items.slice(0, 5)
  }

  return [
    navItem('直播运维', '/', '▣', ['rooms', 'room-detail']),
    navItem('商城', '/shop', '▤', ['shop']),
    navItem('财务', '/finance', '¥', ['finance']),
    navItem('售后', '/after-sales', '修', ['after-sales-portal']),
    navItem('我的', '/personal', '♙', ['personal-center', 'account', 'settings']),
  ]
})

const topbarEyebrow = computed(() => {
  if (isAdmin.value) return 'SYSTEM'
  if (isInternalStaff.value) return 'STAFF CONSOLE'
  if (isAgent.value) return 'AGENT CONSOLE'
  return 'BANBO AI'
})

const topbarTitle = computed(() => {
  if (isAdmin.value) return '系统'
  if (isAgent.value) return '代理工作台'
  if (isSales.value) return '销售工作台'
  if (isStaff.value) {
    return (staffAccess.value?.primary_group_name || '内部员工') + '工作台'
  }
  return '终端工作台'
})

const accountRoleLabel = computed(() => {
  if (isAdmin.value) return '超级系统管理员'
  if (isAgent.value) return '代理账号'
  if (isSales.value) {
    return staffAccess.value?.primary_group_name || '销售部'
  }
  if (isStaff.value) {
    return staffAccess.value?.primary_group_name || '内部员工'
  }
  return '终端账号'
})

const mustChangePassword = computed(
  () => actor.value?.must_change_password === true,
)

const mustCompleteContact = computed(() => {
  if (!actor.value) return false
  return !(
    actor.value.phone?.trim() &&
    actor.value.province?.trim() &&
    actor.value.city?.trim() &&
    actor.value.district?.trim()
  )
})

const forcedCurrentPassword = ref('')
const forcedNewPassword = ref('')
const forcedConfirmPassword = ref('')
const forcedPasswordSubmitting = ref(false)
const forcedPasswordError = ref('')

const requiredPhone = ref('')
const requiredProvince = ref('')
const requiredCity = ref('')
const requiredDistrict = ref('')
const requiredPhoneSubmitting = ref(false)
const requiredPhoneError = ref('')

watch(
  mustCompleteContact,
  (required) => {
    if (!required || !actor.value) return
    requiredPhone.value = actor.value.phone?.trim() || ''
    requiredProvince.value = actor.value.province?.trim() || ''
    requiredCity.value = actor.value.city?.trim() || ''
    requiredDistrict.value = actor.value.district?.trim() || ''
  },
  { immediate: true },
)

async function submitRequiredPhone() {
  requiredPhoneError.value = ''
  const phoneValue = requiredPhone.value.trim()
  const provinceValue = requiredProvince.value.trim()
  const cityValue = requiredCity.value.trim()
  const districtValue = requiredDistrict.value.trim()

  if (!phoneValue) {
    requiredPhoneError.value = '联系电话不能为空'
    return
  }
  if (!provinceValue || !cityValue || !districtValue) {
    requiredPhoneError.value = '省、市、区/县不能为空'
    return
  }

  requiredPhoneSubmitting.value = true
  try {
    const dashboard = await getAccountDashboard()
    const profile = dashboard.profile

    await updateAccountProfile({
      display_name: profile.display_name,
      phone: phoneValue,
      email: profile.email || '',
      qq: profile.qq || '',
      wechat: profile.wechat || '',
      province: provinceValue,
      city: cityValue,
      district: districtValue,
      address: profile.address || '',
    })

    requiredPhone.value = ''
    requiredProvince.value = ''
    requiredCity.value = ''
    requiredDistrict.value = ''
    requiredPhoneError.value = ''
    await loadSession()
  } catch (error) {
    requiredPhoneError.value =
      error instanceof Error ? error.message : '保存基础资料失败'
  } finally {
    requiredPhoneSubmitting.value = false
  }
}

async function submitForcedPassword() {
  forcedPasswordError.value = ''

  if (
    !forcedCurrentPassword.value ||
    !forcedNewPassword.value ||
    !forcedConfirmPassword.value
  ) {
    forcedPasswordError.value = '请完整填写当前密码和新密码。'
    return
  }

  if (forcedNewPassword.value !== forcedConfirmPassword.value) {
    forcedPasswordError.value = '两次输入的新密码不一致。'
    return
  }

  if (forcedNewPassword.value.length < 8) {
    forcedPasswordError.value = '新密码至少需要 8 位。'
    return
  }

  forcedPasswordSubmitting.value = true
  try {
    await changePassword({
      current_password: forcedCurrentPassword.value,
      new_password: forcedNewPassword.value,
      confirm_password: forcedConfirmPassword.value,
    })

    await loadSession()

    forcedCurrentPassword.value = ''
    forcedNewPassword.value = ''
    forcedConfirmPassword.value = ''
    forcedPasswordError.value = ''
  } catch (error) {
    forcedPasswordError.value =
      error instanceof Error ? error.message : '修改密码失败'
  } finally {
    forcedPasswordSubmitting.value = false
  }
}

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
        <div class="brand-mark">{{ isInternalStaff ? '蓝' : '伴' }}</div>
        <div>
          <strong>{{ isInternalStaff ? '小蓝搭子管理系统' : '伴播搭子' }}</strong>
          <span>{{ isInternalStaff ? 'BANBO AI SYSTEM' : 'BANBO AI' }}</span>
        </div>
      </div>

      <nav class="nav-list">
        <section
          v-for="section in navSections"
          :key="section.label"
          class="nav-section-card"
          :class="{ collapsed: sectionCollapsed(section), 'has-active': sectionHasActive(section) }"
        >
          <button
            class="nav-section-heading"
            type="button"
            :aria-expanded="!sectionCollapsed(section)"
            @click="toggleNavSection(section)"
          >
            <span class="nav-section-title-wrap">
              <span class="nav-section-label">{{ section.label }}</span>
            </span>
            <span class="nav-section-heading-actions">
              <span class="nav-section-count">{{ section.items.length }}</span>
              <span class="nav-section-chevron">⌄</span>
            </span>
          </button>

          <div v-show="!sectionCollapsed(section)" class="nav-section-items">
            <RouterLink
              v-for="item in section.items"
              :key="item.to + item.label"
              class="nav-item"
              :class="{ active: navActive(item) }"
              :to="item.to"
            >
              <span class="nav-icon-shell">
                <span class="nav-icon">{{ item.icon }}</span>
              </span>
              <span class="nav-item-label">{{ item.label }}</span>
              <span class="nav-item-arrow">›</span>
            </RouterLink>
          </div>
        </section>
      </nav>

      <section class="sidebar-personal-section">
        <RouterLink
          class="sidebar-personal-button"
          :class="{
            active:
              route.name === 'personal-center' ||
              route.name === 'account' ||
              route.name === 'settings',
          }"
          to="/personal"
        >
          <span class="sidebar-personal-button-icon">我</span>
          <span class="sidebar-personal-button-copy">
            <strong>个人中心</strong>
            <small>账户、安全与个人偏好</small>
          </span>
          <span class="sidebar-personal-button-arrow">›</span>
        </RouterLink>
      </section>

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
          <p class="eyebrow">{{ topbarEyebrow }}</p>
          <h1>{{ topbarTitle }}</h1>
        </div>

        <div class="topbar-actions">
          <RouterLink class="account-chip account-chip-link" to="/personal">
            <div class="avatar">
              <img
                v-if="actor?.avatar_url"
                :src="actor.avatar_url"
                alt="账户头像"
              />
              <span v-else>{{ actor?.display_name?.slice(0, 1) || '账' }}</span>
            </div>
            <div>
              <strong>{{ actor?.display_name || '账户' }}</strong>
              <span>{{ accountRoleLabel }} · {{ actor?.username || '' }}</span>
            </div>
          </RouterLink>

          <button
            class="logout-button"
            type="button"
            :disabled="loggingOut"
            @click="signOut"
          >
            {{ loggingOut ? '退出中...' : '退出' }}
          </button>
        </div>
      </header>

      <section class="page-content">
        <RouterView />
      </section>
    </main>

    <nav class="mobile-nav" aria-label="移动端导航">
      <RouterLink
        v-for="item in mobileNavItems"
        :key="'mobile-' + item.to + item.label"
        class="mobile-nav-item"
        :class="{ active: navActive(item) }"
        :to="item.to"
      >
        <span class="mobile-nav-icon">{{ item.icon }}</span>
        <span>{{ item.label }}</span>
      </RouterLink>
    </nav>
  </div>

  <GlobalFeedback />

  <Teleport to="body">
    <div
      v-if="mustChangePassword"
      class="forced-password-backdrop"
      role="dialog"
      aria-modal="true"
      aria-labelledby="forced-password-title"
    >
      <form
        class="forced-password-card"
        @submit.prevent="submitForcedPassword"
      >
        <div class="forced-password-brand">
          <div class="brand-mark">{{ isInternalStaff ? '蓝' : '伴' }}</div>
          <div>
            <strong>{{ isInternalStaff ? '小蓝搭子管理系统' : '伴播搭子' }}</strong>
            <span>ACCOUNT SECURITY</span>
          </div>
        </div>

        <div class="forced-password-heading">
          <span class="section-kicker">PASSWORD REQUIRED</span>
          <h2 id="forced-password-title">首次登录请修改密码</h2>
          <p>
            当前账号使用的是系统生成的初始密码。修改完成前不能继续使用其他功能。
          </p>
        </div>

        <div class="forced-password-fields">
          <label>
            <span>当前初始密码</span>
            <PasswordInput
              v-model="forcedCurrentPassword"
              autocomplete="current-password"
              placeholder="输入当前初始密码"
              required
            />
          </label>

          <label>
            <span>新密码</span>
            <PasswordInput
              v-model="forcedNewPassword"
              autocomplete="new-password"
              placeholder="至少 8 位"
              required
            />
          </label>

          <label>
            <span>确认新密码</span>
            <PasswordInput
              v-model="forcedConfirmPassword"
              autocomplete="new-password"
              placeholder="再次输入新密码"
              required
            />
          </label>
        </div>

        <p v-if="forcedPasswordError" class="forced-password-error">
          {{ forcedPasswordError }}
        </p>

        <button
          class="forced-password-submit"
          type="submit"
          :disabled="forcedPasswordSubmitting"
        >
          {{ forcedPasswordSubmitting ? '正在修改...' : '修改密码并继续' }}
        </button>

        <p class="forced-password-note">
          修改成功后，其他登录会话会自动失效。
        </p>
      </form>
    </div>
  </Teleport>

  <Teleport to="body">
    <div
      v-if="mustCompleteContact && !mustChangePassword"
      class="forced-password-backdrop required-contact-backdrop"
      role="dialog"
      aria-modal="true"
      aria-labelledby="required-contact-title"
    >
      <form
        class="forced-password-card required-contact-card"
        @submit.prevent="submitRequiredPhone"
      >
        <div class="forced-password-brand">
          <div class="brand-mark">{{ isInternalStaff ? '蓝' : '伴' }}</div>
          <div>
            <strong>{{ isInternalStaff ? '小蓝搭子管理系统' : '伴播搭子' }}</strong>
            <span>ACCOUNT PROFILE</span>
          </div>
        </div>

        <div class="forced-password-heading">
          <span class="section-kicker">PROFILE REQUIRED</span>
          <h2 id="required-contact-title">首次进入，请补全地区资料</h2>
          <p>
            注册已经完成。首次进入系统请补全省、市、区/县，并确认联系电话，保存后即可继续使用。
          </p>
        </div>

        <div class="forced-password-fields">
          <label>
            <span>联系电话 <em class="required-mark">*</em></span>
            <input
              v-model="requiredPhone"
              type="text"
              autocomplete="tel"
              placeholder="请输入联系电话"
              required
            />
          </label>

          <RegionSelect
            v-model:province="requiredProvince"
            v-model:city="requiredCity"
            v-model:district="requiredDistrict"
          />
        </div>

        <p v-if="requiredPhoneError" class="forced-password-error">
          {{ requiredPhoneError }}
        </p>

        <button
          class="forced-password-submit"
          type="submit"
          :disabled="requiredPhoneSubmitting"
        >
          {{ requiredPhoneSubmitting ? '正在保存...' : '保存资料并继续' }}
        </button>

        <p class="forced-password-note">
          邮箱、QQ、微信和详细地址可在“账户管理”中继续完善。
        </p>
      </form>
    </div>
  </Teleport>
</template>
