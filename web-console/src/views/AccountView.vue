<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import PasswordInput from '../components/PasswordInput.vue'
import RegionSelect from '../components/RegionSelect.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import { computed, onMounted, ref } from 'vue'
import {
  changePassword,
  getAccountDashboard,
  getAuthSessions,
  logoutOtherAuthSessions,
  updateAccountProfile,
  uploadAccountAvatar,
} from '../api'
import { loadSession, session } from '../session'
import type { AccountDashboard, AuthSessionSummary } from '../types'

const dashboard = ref<AccountDashboard | null>(null)
const loading = ref(true)
const error = useFeedbackErrorRef()

const editingName = ref(false)
const displayName = ref('')
const phone = ref('')
const email = ref('')
const qq = ref('')
const wechat = ref('')
const province = ref('')
const city = ref('')
const district = ref('')
const address = ref('')
const savingProfile = ref(false)
const profileSuccess = ref('')

const avatarUploading = ref(false)
const avatarInput = ref<HTMLInputElement | null>(null)

const currentPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const passwordSubmitting = ref(false)
const passwordError = ref('')
const passwordSuccess = ref('')
const sessions = ref<AuthSessionSummary[]>([])
const sessionsLoading = ref(false)
const sessionError = ref('')
const logoutOthersBusy = ref(false)

const isCustomer = computed(() => dashboard.value?.profile.role === 'customer')
const isInternalStaff = computed(() => ['platform_admin', 'staff', 'sales_staff'].includes(dashboard.value?.profile.role || ''))

const roleLabel = computed(() => {
  const role = dashboard.value?.profile.role
  if (role === 'platform_admin') return '超级系统管理员'
  if (role === 'agent_admin') return '代理管理员'
  if (role === 'sales_staff' || role === 'staff') {
    return session.bootstrap?.staff_access?.primary_group_name || '内部员工'
  }
  if (role === 'customer') return '终端账号'
  return role || '未知角色'
})
const organizationLabel = computed(() => {
  const role = dashboard.value?.profile.role
  if (role === 'agent_admin') return '代理组织 ID'
  if (role === 'customer') return '终端组织 ID'
  return '组织 ID'
})

function syncProfileForm() {
  const profile = dashboard.value?.profile
  if (!profile) return
  displayName.value = profile.display_name || ''
  phone.value = profile.phone || ''
  email.value = profile.email || ''
  qq.value = profile.qq || ''
  wechat.value = profile.wechat || ''
  province.value = profile.province || ''
  city.value = profile.city || ''
  district.value = profile.district || ''
  address.value = profile.address || ''
}

async function loadAccount() {
  loading.value = true
  error.value = ''
  try {
    dashboard.value = await getAccountDashboard()
    syncProfileForm()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取账户信息失败'
  } finally {
    loading.value = false
  }
}

async function saveProfile() {
  const name = displayName.value.trim()
  const contactPhone = phone.value.trim()

  error.value = ''
  profileSuccess.value = ''

  if (name.length < 2) {
    error.value = '账户名称至少需要 2 个字符'
    return
  }
  if (!contactPhone) {
    error.value = '联系电话不能为空'
    return
  }
  if (!province.value.trim() || !city.value.trim() || !district.value.trim()) {
    error.value = '省、市、区/县不能为空'
    return
  }
  if (savingProfile.value) return

  savingProfile.value = true
  try {
    const profile = await updateAccountProfile({
      display_name: name,
      phone: contactPhone,
      email: email.value.trim(),
      qq: qq.value.trim(),
      wechat: wechat.value.trim(),
      province: province.value.trim(),
      city: city.value.trim(),
      district: district.value.trim(),
      address: address.value.trim(),
    })
    if (dashboard.value) dashboard.value.profile = profile
    syncProfileForm()
    editingName.value = false
    profileSuccess.value = '账户资料已保存'
    await loadSession()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '修改账户资料失败'
  } finally {
    savingProfile.value = false
  }
}

function cancelNameEdit() {
  editingName.value = false
  displayName.value = dashboard.value?.profile.display_name || ''
}

function chooseAvatar() {
  if (!avatarUploading.value) avatarInput.value?.click()
}

async function onAvatarSelected(event: Event) {
  const target = event.target as HTMLInputElement
  const file = target.files?.[0]
  target.value = ''
  if (!file || avatarUploading.value) return

  if (file.size > 2 * 1024 * 1024) {
    error.value = '头像大小需要小于 2MB'
    return
  }

  avatarUploading.value = true
  error.value = ''
  try {
    const profile = await uploadAccountAvatar(file)
    if (dashboard.value) dashboard.value.profile = profile
    syncProfileForm()
    await loadSession()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '上传头像失败'
  } finally {
    avatarUploading.value = false
  }
}

async function loadAuthSessions() {
  sessionsLoading.value = true
  sessionError.value = ''
  try {
    sessions.value = (await getAuthSessions()).items
  } catch (err) {
    sessionError.value = err instanceof Error ? err.message : '读取登录设备失败'
  } finally {
    sessionsLoading.value = false
  }
}

async function logoutOtherSessions() {
  if (logoutOthersBusy.value) return
  logoutOthersBusy.value = true
  sessionError.value = ''
  try {
    await logoutOtherAuthSessions()
    await loadAuthSessions()
  } catch (err) {
    sessionError.value = err instanceof Error ? err.message : '退出其他设备失败'
  } finally {
    logoutOthersBusy.value = false
  }
}

function deviceLabel(userAgent: string) {
  const ua = (userAgent || '').toLowerCase()
  const browser = ua.includes('edg/') ? 'Edge' : ua.includes('chrome/') ? 'Chrome' : ua.includes('firefox/') ? 'Firefox' : ua.includes('safari/') ? 'Safari' : '浏览器'
  const os = ua.includes('windows') ? 'Windows' : ua.includes('mac os') ? 'macOS' : ua.includes('android') ? 'Android' : ua.includes('iphone') || ua.includes('ipad') ? 'iOS/iPadOS' : '未知设备'
  return os + ' · ' + browser
}

function formatSessionTime(value?: string) {
  if (!value) return '—'
  return new Date(value).toLocaleString('zh-CN')
}

async function submitPassword() {
  passwordError.value = ''
  passwordSuccess.value = ''

  if (!currentPassword.value || !newPassword.value || !confirmPassword.value) {
    passwordError.value = '请完整填写当前密码和新密码。'
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    passwordError.value = '两次输入的新密码不一致。'
    return
  }
  if (newPassword.value.length < 8) {
    passwordError.value = '新密码至少需要 8 位。'
    return
  }

  passwordSubmitting.value = true
  try {
    await changePassword({
      current_password: currentPassword.value,
      new_password: newPassword.value,
      confirm_password: confirmPassword.value,
    })
    currentPassword.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
    passwordSuccess.value = '密码修改成功，其他登录会话已失效。'
    await loadAuthSessions()
  } catch (err) {
    passwordError.value = err instanceof Error ? err.message : '修改密码失败'
  } finally {
    passwordSubmitting.value = false
  }
}

function formatSeconds(value: number) {
  if (!value) return '0 小时'
  const hours = Math.floor(value / 3600)
  const minutes = Math.floor((value % 3600) / 60)
  if (hours > 0 && minutes > 0) return hours + ' 小时 ' + minutes + ' 分钟'
  if (hours > 0) return hours + ' 小时'
  return minutes + ' 分钟'
}

function formatDate(value?: string) {
  if (!value) return '—'
  return new Date(value).toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  })
}

function statusLabel(value: string) {
  if (value === 'active') return '正常'
  if (value === 'disabled') return '已停用'
  return value || '未知'
}

onMounted(async () => {
  await Promise.all([loadAccount(), loadAuthSessions()])
})
</script>

<template>
  <div class="account-page">
    <ModulePageNav context="personal-auto" active-title="账户与安全" />
    <section class="page-hero account-hero">
      <div>
        <p class="section-kicker">ACCOUNT CENTER</p>
        <h2>账户与安全</h2>
        <p>
          管理当前登录账号的资料、联系方式、地址信息、头像与登录安全。
        </p>
      </div>
    </section>

    <div v-if="loading" class="panel-loading">正在读取账户信息...</div>
    <div v-else-if="error && !dashboard" class="global-error">{{ error }}</div>

    <template v-if="dashboard">
      <p v-if="error" class="settings-error account-inline-error">{{ error }}</p>

      <section
        class="account-overview-grid"
        :class="{ 'account-overview-grid-basic': !isCustomer }"
      >
        <article class="account-card profile-card">
          <div class="account-card-heading">
            <div>
              <span class="section-kicker">PROFILE</span>
              <h3>账户资料</h3>
            </div>
            <span class="account-card-badge">可修改</span>
          </div>

          <div class="profile-main">
            <button
              class="profile-avatar-button"
              type="button"
              :disabled="avatarUploading"
              @click="chooseAvatar"
            >
              <img
                v-if="dashboard.profile.avatar_url"
                :src="dashboard.profile.avatar_url"
                alt="账户头像"
              />
              <span v-else>{{ dashboard.profile.display_name.slice(0, 1) || '账' }}</span>
              <em>{{ avatarUploading ? '上传中' : '更换' }}</em>
            </button>

            <input
              ref="avatarInput"
              class="visually-hidden"
              type="file"
              accept="image/jpeg,image/png,image/webp"
              @change="onAvatarSelected"
            />

            <div class="profile-copy">
              <template v-if="editingName">
                <input
                  v-model="displayName"
                  class="text-input profile-name-input"
                  maxlength="64"
                  @keyup.enter="saveProfile"
                />
                <div class="profile-edit-actions">
                  <button
                    class="primary-button compact-button"
                    type="button"
                    :disabled="savingProfile"
                    @click="saveProfile"
                  >
                    {{ savingProfile ? '保存中...' : '保存' }}
                  </button>
                  <button
                    class="ghost-button compact-button"
                    type="button"
                    :disabled="savingProfile"
                    @click="cancelNameEdit"
                  >
                    取消
                  </button>
                </div>
              </template>

              <template v-else>
                <strong class="profile-name">{{ dashboard.profile.display_name }}</strong>
                <button v-if="!isInternalStaff" class="text-action" type="button" @click="editingName = true">
                  修改名称
                </button>
                <small v-else class="formal-name-note">正式姓名由组织架构维护</small>
              </template>

              <span class="profile-login">@{{ dashboard.profile.username }}</span>
              <small>头像支持 JPG、PNG、WebP，最大 2MB。</small>
            </div>
          </div>
        </article>

        <article class="account-card status-card">
          <div class="account-card-heading">
            <div>
              <span class="section-kicker">ACCOUNT STATUS</span>
              <h3>账户身份</h3>
            </div>
            <span class="account-card-badge active">
              {{ statusLabel(dashboard.profile.status) }}
            </span>
          </div>

          <dl class="account-detail-list">
            <div>
              <dt>登录账号</dt>
              <dd>{{ dashboard.profile.username }}</dd>
            </div>
            <div>
              <dt>用户 ID</dt>
              <dd>#{{ dashboard.profile.user_id }}</dd>
            </div>
            <div v-if="dashboard.profile.tenant_id">
              <dt>{{ organizationLabel }}</dt>
              <dd>#{{ dashboard.profile.tenant_id }}</dd>
            </div>
            <div>
              <dt>账户类型</dt>
              <dd>{{ roleLabel }}</dd>
            </div>
            <div>
              <dt>联系电话</dt>
              <dd>{{ dashboard.profile.phone || '未完善' }}</dd>
            </div>
            <div>
              <dt>创建时间</dt>
              <dd>{{ formatDate(dashboard.profile.created_at) }}</dd>
            </div>
          </dl>
        </article>

        <article v-if="isCustomer" class="account-card membership-card">
          <div class="account-card-heading">
            <div>
              <span class="section-kicker">MEMBERSHIP</span>
              <h3>会员权益</h3>
            </div>
            <span
              class="account-card-badge"
              :class="{ active: Boolean(dashboard.membership) }"
            >
              {{ dashboard.membership ? '生效中' : '未开通' }}
            </span>
          </div>

          <template v-if="dashboard.membership">
            <strong class="membership-name">{{ dashboard.membership.plan_name }}</strong>
            <div class="membership-period">
              {{ formatDate(dashboard.membership.cycle_start_at) }}
              <span>—</span>
              {{ formatDate(dashboard.membership.cycle_end_at) }}
            </div>
            <div class="quota-mini-grid">
              <div>
                <span>本周期基础时长</span>
                <strong>{{ formatSeconds(dashboard.membership.included_seconds) }}</strong>
              </div>
              <div>
                <span>会员剩余</span>
                <strong>{{ formatSeconds(dashboard.quota.membership_seconds) }}</strong>
              </div>
            </div>
          </template>

          <div v-else class="account-empty-state">
            <strong>暂未开通会员</strong>
            <span>开通会员后，这里会显示当前会员方案和周期权益。</span>
          </div>
        </article>

        <article v-if="isCustomer" class="account-card quota-card">
          <div class="account-card-heading">
            <div>
              <span class="section-kicker">TIME ASSETS</span>
              <h3>时长资产</h3>
            </div>
            <span class="account-card-badge">实时汇总</span>
          </div>

          <strong class="quota-total">
            {{ formatSeconds(dashboard.quota.total_seconds) }}
          </strong>
          <span class="quota-total-label">当前可用总时长</span>

          <div class="quota-breakdown">
            <div>
              <span>会员时长</span>
              <strong>{{ formatSeconds(dashboard.quota.membership_seconds) }}</strong>
            </div>
            <div>
              <span>购买时长</span>
              <strong>{{ formatSeconds(dashboard.quota.purchased_seconds) }}</strong>
            </div>
            <div>
              <span>奖励时长</span>
              <strong>{{ formatSeconds(dashboard.quota.reward_seconds) }}</strong>
            </div>
          </div>
        </article>
      </section>

      <section class="settings-card account-contact-card">
        <div class="settings-card-header">
          <div>
            <span class="section-kicker">CONTACT PROFILE</span>
            <h3>联系方式与地址</h3>
          </div>
          <span class="account-role-badge">联系电话必填</span>
        </div>

        <form class="account-contact-form" @submit.prevent="saveProfile">
          <label>
            <span>联系电话 <em class="required-mark">*</em></span>
            <input
              v-model="phone"
              class="text-input"
              type="text"
              autocomplete="tel"
              required
              placeholder="联系电话，必填；不校验号码格式"
            />
          </label>

          <label>
            <span>邮箱地址</span>
            <input
              v-model="email"
              class="text-input"
              type="email"
              maxlength="254"
              autocomplete="email"
              placeholder="例如 name@example.com"
            />
          </label>

          <label>
            <span>QQ 号</span>
            <input
              v-model="qq"
              class="text-input"
              maxlength="32"
              placeholder="QQ 号"
            />
          </label>

          <label>
            <span>微信号</span>
            <input
              v-model="wechat"
              class="text-input"
              maxlength="64"
              placeholder="微信号"
            />
          </label>

          <RegionSelect class="contact-address-field" v-model:province="province" v-model:city="city" v-model:district="district" />

          <label class="contact-address-field">
            <span>详细地址</span>
            <input
              v-model="address"
              class="text-input"
              maxlength="255"
              placeholder="街道、门牌号、园区等详细地址"
            />
          </label>

          <div class="account-contact-actions">
            <p v-if="profileSuccess" class="settings-success">{{ profileSuccess }}</p>
            <span v-else>这些资料仅用于当前账户的联系与业务管理。</span>
            <button
              class="primary-button"
              type="submit"
              :disabled="savingProfile"
            >
              {{ savingProfile ? '保存中...' : '保存账户资料' }}
            </button>
          </div>
        </form>
      </section>

      <section class="settings-card account-security-card">
        <div class="settings-card-header">
          <div>
            <span class="section-kicker">ACCOUNT SECURITY</span>
            <h3>登录密码</h3>
          </div>
          <span class="account-role-badge">{{ roleLabel }}</span>
        </div>

        <div class="security-copy">
          <strong>修改登录密码</strong>
          <span>
            系统不会显示当前密码。验证当前密码后即可设置新密码，修改成功后其他登录会话会自动失效。
          </span>
        </div>

        <form class="password-form" @submit.prevent="submitPassword">
          <label>
            <span>当前密码</span>
            <PasswordInput
              v-model="currentPassword"
              class="text-input"
              autocomplete="current-password"
              placeholder="输入当前密码"
            />
          </label>

          <label>
            <span>新密码</span>
            <PasswordInput
              v-model="newPassword"
              class="text-input"
              autocomplete="new-password"
              placeholder="8-72 位"
            />
          </label>

          <label>
            <span>确认新密码</span>
            <PasswordInput
              v-model="confirmPassword"
              class="text-input"
              autocomplete="new-password"
              placeholder="再次输入新密码"
            />
          </label>

          <p v-if="passwordError" class="settings-error">{{ passwordError }}</p>
          <p v-if="passwordSuccess" class="settings-success">{{ passwordSuccess }}</p>

          <div class="security-actions">
            <span>密码修改成功后，仅保留当前会话。</span>
            <button
              class="primary-button"
              type="submit"
              :disabled="passwordSubmitting"
            >
              {{ passwordSubmitting ? '修改中...' : '修改密码' }}
            </button>
          </div>
        </form>
      </section>

      <section class="settings-card account-session-card">
        <div class="settings-card-header">
          <div>
            <span class="section-kicker">LOGIN SESSIONS</span>
            <h3>登录与会话安全</h3>
          </div>
          <button
            class="ghost-button"
            type="button"
            :disabled="sessionsLoading || logoutOthersBusy || sessions.filter((item) => !item.current).length === 0"
            @click="logoutOtherSessions"
          >
            {{ logoutOthersBusy ? '正在退出...' : '退出其他设备' }}
          </button>
        </div>
        <p class="session-security-copy">查看当前及其他有效登录设备。修改密码后其他会话会自动失效。</p>
        <p v-if="sessionError" class="settings-error">{{ sessionError }}</p>
        <div v-if="sessionsLoading" class="panel-loading">正在读取登录设备...</div>
        <div v-else-if="sessions.length === 0" class="empty-state">暂无有效登录会话。</div>
        <div v-else class="session-device-list">
          <article v-for="item in sessions" :key="item.id" class="session-device-card" :class="{ current: item.current }">
            <div class="session-device-icon">{{ item.current ? '本' : '设' }}</div>
            <div class="session-device-main">
              <div><strong>{{ deviceLabel(item.user_agent) }}</strong><span v-if="item.current" class="status-pill status-success">当前设备</span></div>
              <span>IP：{{ item.client_ip || '未知' }}</span>
              <small>最近活动：{{ formatSessionTime(item.last_seen_at) }} · 登录时间：{{ formatSessionTime(item.created_at) }}</small>
            </div>
          </article>
        </div>
      </section>
    </template>
  </div>
</template>
