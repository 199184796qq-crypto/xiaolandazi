<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, ref } from 'vue'
import { createAgentCustomer, getAgentCustomers } from '../api'
import CredentialResultModal from '../components/CredentialResultModal.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import RegionSelect from '../components/RegionSelect.vue'
import type { AdminCustomer, InitialCredential } from '../types'

const items = ref<AdminCustomer[]>([])
const loading = ref(false)
const error = useFeedbackErrorRef()
const showCreate = ref(false)
const search = ref('')
const page = ref(1)
const pageSize = 12
const filteredItems = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  if (!keyword) return items.value
  return items.value.filter((item) =>
    [item.display_name, item.username, item.phone, item.email, item.inviter_display_name, item.source_type]
      .some((value) => String(value || '').toLowerCase().includes(keyword)),
  )
})
const totalPages = computed(() => Math.max(1, Math.ceil(filteredItems.value.length / pageSize)))
const pagedItems = computed(() => {
  const current = Math.min(page.value, totalPages.value)
  const start = (current - 1) * pageSize
  return filteredItems.value.slice(start, start + pageSize)
})

const username = ref('')
const displayName = ref('')
const phone = ref('')
const email = ref('')
const province = ref('')
const city = ref('')
const district = ref('')
const deliveryMethod = ref<'copy' | 'email'>('copy')

const credentialOpen = ref(false)
const createdCredential = ref<InitialCredential | null>(null)
const createdDisplayName = ref('')
const createdUsername = ref('')

function sourceLabel(source: string) {
  if (source === 'agent') return '代理直接开户'
  if (source === 'agent_invite') return '代理邀请码注册'
  if (source === 'platform_invite') return '系统邀请码注册'
  if (source === 'referral') return '终端推荐'
  if (source === 'direct') return '历史直营'
  return source || '未知来源'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    items.value = (await getAgentCustomers()).items
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取终端列表失败'
  } finally {
    loading.value = false
  }
}

function openCreate() {
  error.value = ''
  username.value = ''
  displayName.value = ''
  phone.value = ''
  email.value = ''
  province.value = ''
  city.value = ''
  district.value = ''
  deliveryMethod.value = 'copy'
  showCreate.value = true
}

async function submitCreate() {
  error.value = ''

  if (!phone.value.trim()) {
    error.value = '联系电话不能为空'
    return
  }
  if (
    !province.value.trim() ||
    !city.value.trim() ||
    !district.value.trim()
  ) {
    error.value = '省、市、区/县必须选择'
    return
  }
  if (deliveryMethod.value === 'email' && !email.value.trim()) {
    error.value = '选择邮件发送时必须填写邮箱'
    return
  }

  try {
    const result = await createAgentCustomer({
      username: username.value.trim(),
      display_name: displayName.value.trim(),
      phone: phone.value.trim(),
      email: email.value.trim(),
      province: province.value.trim(),
      city: city.value.trim(),
      district: district.value.trim(),
      delivery_method: deliveryMethod.value,
    })

    createdDisplayName.value = result.display_name
    createdUsername.value = result.username
    createdCredential.value = result.credential
    showCreate.value = false
    credentialOpen.value = true
    await load()
  } catch (value) {
    error.value =
      value instanceof Error ? value.message : '开通终端账号失败'
  }
}

function closeCredential() {
  credentialOpen.value = false
  createdCredential.value = null
  createdDisplayName.value = ''
  createdUsername.value = ''
}

onMounted(load)
</script>

<template>
  <div class="management-page">
    <ModulePageNav context="workspace-agent" active-title="客户资源" />
    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">MY CUSTOMERS</p>
        <h2>客户资源</h2>
        <p>
          管理当前代理名下的客户资源。代理开通的终端账号可直接登录伴播搭子终端工作台。
        </p>
      </div>
      <button class="primary-button" type="button" @click="openCreate">
        开通终端账号
      </button>
    </section>

    <p v-if="error" class="auth-error">{{ error }}</p>

    <section class="settings-card">
      <div class="settings-card-header">
        <div>
          <span class="section-kicker">CUSTOMERS</span>
          <h3>我的终端</h3>
        </div>
        <span>{{ loading ? '加载中...' : filteredItems.length + ' / ' + items.length + ' 个终端' }}</span>
      </div>
      <div class="scalable-list-toolbar">
        <input v-model="search" type="search" placeholder="搜索终端名称 / 账号 / 手机号 / 邮箱" @input="page = 1" />
      </div>

      <div class="agent-list">
        <article
          v-for="item in pagedItems"
          :key="item.user_id"
          class="agent-row customer-row"
        >
          <div class="agent-main">
            <strong>{{ item.display_name }}</strong>
            <span>{{ item.username }}</span>
          </div>
          <div>
            <span class="muted-label">联系方式</span>
            <strong>{{ item.phone || '未完善' }}</strong>
            <span v-if="item.email">{{ item.email }}</span>
          </div>
          <div>
            <span class="muted-label">终端来源</span>
            <strong>{{ sourceLabel(item.source_type) }}</strong>
            <span v-if="item.inviter_display_name">
              推荐人：{{ item.inviter_display_name }}
            </span>
          </div>
          <div>
            <span class="status-pill">{{ item.status }}</span>
          </div>
        </article>

        <div v-if="!loading && filteredItems.length === 0" class="empty-state">
          暂无终端。点击“开通终端账号”即可为商家建立登录账号。
        </div>
      </div>
      <PaginationBar
        :page="Math.min(page, totalPages)"
        :total-pages="totalPages"
        :total="filteredItems.length"
        :page-size="pageSize"
        @update:page="page = $event"
      />
    </section>

    <div
      v-if="showCreate"
      class="modal-backdrop"
      @click.self="showCreate = false"
    >
      <form class="modal-card" @submit.prevent="submitCreate">
        <div class="modal-header">
          <div>
            <p class="section-kicker">OPEN CUSTOMER ACCOUNT</p>
            <h3>开通终端账号</h3>
          </div>
          <button
            class="icon-button"
            type="button"
            aria-label="关闭"
            @click="showCreate = false"
          >
            ×
          </button>
        </div>

        <p class="modal-helper">
          账号创建后，系统自动生成随机初始密码。您可以复制登录信息，或发送到终端邮箱。
        </p>

        <div class="form-grid">
          <label>
            <span>终端名称</span>
            <input
              v-model="displayName"
              required
              maxlength="64"
              placeholder="终端公司、门店或品牌名称"
            />
          </label>

          <label>
            <span>联系电话 <em class="required-mark">*</em></span>
            <input
              v-model="phone"
              type="text"
              required
              placeholder="联系电话"
            />
          </label>

          <label>
            <span>登录账号</span>
            <input
              v-model="username"
              required
              maxlength="32"
              autocomplete="off"
              placeholder="终端登录账号"
            />
          </label>

          <label>
            <span>邮箱</span>
            <input
              v-model="email"
              type="email"
              placeholder="邮件交付初始密码时必填"
            />
          </label>

          <RegionSelect
            class="form-span-2"
            v-model:province="province"
            v-model:city="city"
            v-model:district="district"
          />

          <label class="form-span-2">
            <span>初始凭证交付方式</span>
            <select v-model="deliveryMethod" class="text-input">
              <option value="copy">创建后复制登录信息</option>
              <option value="email">发送到邮箱</option>
            </select>
          </label>
        </div>

        <div class="account-opening-note">
          <strong>开户说明</strong>
          <span>
            初始密码由系统随机生成，代理人员不能手工设置。终端首次登录后必须立即修改密码。
          </span>
        </div>

        <div class="modal-actions">
          <button
            class="ghost-button"
            type="button"
            @click="showCreate = false"
          >
            取消
          </button>
          <button class="primary-button" type="submit">
            开通终端账号
          </button>
        </div>
      </form>
    </div>

    <CredentialResultModal
      :open="credentialOpen"
      title="终端账号已开通"
      :display-name="createdDisplayName"
      :username="createdUsername"
      :credential="createdCredential"
      @close="closeCredential"
    />
  </div>
</template>