<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { getStaffPermissionCenter, updateStaffPermissionCenterRole } from '../api'
import ModulePageNav from '../components/ModulePageNav.vue'
import { useFeedbackErrorRef } from '../uiFeedback'
import type {
  StaffEmployeeSummary,
  StaffPermissionCenterDashboard,
  StaffPermissionSummary,
  StaffRoleSummary,
} from '../types'

type CenterTab = 'role' | 'permission' | 'employee'

const dashboard = ref<StaffPermissionCenterDashboard | null>(null)
const loading = ref(false)
const saving = ref(false)
const error = useFeedbackErrorRef()
const notice = ref('')
const activeTab = ref<CenterTab>('permission')
const search = ref('')
const employeePage = ref(1)
const employeePageSize = ref(20)
const employeeListEl = ref<HTMLElement | null>(null)
const selectedRoleId = ref<number | null>(null)
const selectedPermissionId = ref<number | null>(null)
const selectedEmployeeId = ref<number | null>(null)
const selectedBusinessGroup = ref('AI 时长')
const rolePermissionDrafts = ref<Record<number, number[]>>({})
const dirtyRoleIds = ref<number[]>([])

const roles = computed(() => dashboard.value?.roles ?? [])
const permissions = computed(() => dashboard.value?.permissions ?? [])
const employees = computed(() => dashboard.value?.employees ?? [])

function businessGroupForPermission(permission: StaffPermissionSummary) {
  const code = permission.code
  if (code.includes('.ai_time.')) return 'AI 时长'
  if (code.startsWith('liveops.')) return '直播运维'
  if (code.startsWith('livepolicy.')) return '直播策略'
  if (code.startsWith('livecoach.')) return '主播训练'
  if (code.startsWith('livevoice.')) return '声音能力'
  if (code.startsWith('liveanalysis.')) return '直播分析'
  if (code.startsWith('commercial.membership.')) return '会员方案'
  if (code.startsWith('commercial.time_card.')) return '时长卡运营'
  if (code.startsWith('commercial.device.')) return '设备商城'
  if (code.startsWith('commercial.marketing.')) return '活动营销'
  if (code.startsWith('commercial.referral.') || code.startsWith('invitations.')) return '邀请与推荐'
  if (code.startsWith('inventory.after_sales.') || code.startsWith('after_sales.')) return '售后管理'
  if (code.startsWith('inventory.')) return '仓储库存'
  if (code.startsWith('logistics.')) return '物流管理'
  if (code.startsWith('finance.')) return '财务'
  if (code.startsWith('staff.') || code.startsWith('system.architecture.')) return '组织架构'
  if (code.startsWith('customer.')) return '客户资源'
  if (code.startsWith('agent.')) return '代理'
  if (code.startsWith('sales.')) return '客资销售'
  if (code.startsWith('audit.')) return '审计'
  return permission.module || '其他'
}

const businessGroups = computed(() => {
  const counts = new Map<string, number>()
  for (const item of permissions.value) {
    const name = businessGroupForPermission(item)
    counts.set(name, (counts.get(name) || 0) + 1)
  }
  return Array.from(counts.entries())
    .map(([name, count]) => ({ name, count }))
    .sort((a, b) => {
      if (a.name === 'AI 时长') return -1
      if (b.name === 'AI 时长') return 1
      return a.name.localeCompare(b.name, 'zh-CN')
    })
})

const visibleBusinessPermissions = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  return permissions.value.filter((item) => {
    if (businessGroupForPermission(item) !== selectedBusinessGroup.value) return false
    if (!keyword) return true
    return [item.description, item.code, item.action].some((value) =>
      String(value || '').toLowerCase().includes(keyword),
    )
  })
})

const selectedPermission = computed(() =>
  permissions.value.find((item) => item.id === selectedPermissionId.value) || null,
)

const selectedRole = computed(() =>
  roles.value.find((item) => item.id === selectedRoleId.value) || null,
)

const selectedEmployee = computed(() =>
  employees.value.find((item) => item.id === selectedEmployeeId.value) || null,
)

function scopeLabel(value: string) {
  const labels: Record<string, string> = {
    self: '本人',
    assigned: '已分配业务',
    group: '本部门',
    managed_groups: '负责的部门',
    all_internal: '全部内部员工',
    all: '全系统',
  }
  return labels[value] || value
}

function roleDraft(roleId: number) {
  return rolePermissionDrafts.value[roleId] || []
}

function roleHasPermission(roleId: number, permissionId: number) {
  return roleDraft(roleId).includes(permissionId)
}

function markDirty(roleId: number) {
  if (!dirtyRoleIds.value.includes(roleId)) {
    dirtyRoleIds.value = [...dirtyRoleIds.value, roleId]
  }
}

function toggleRolePermission(role: StaffRoleSummary, permissionId: number) {
  const current = roleDraft(role.id)
  rolePermissionDrafts.value = {
    ...rolePermissionDrafts.value,
    [role.id]: current.includes(permissionId)
      ? current.filter((id) => id !== permissionId)
      : [...current, permissionId],
  }
  markDirty(role.id)
}

function employeeHasRole(employee: StaffEmployeeSummary, roleId: number) {
  return employee.roles.some((item) => item.role_id === roleId)
}

const selectedPermissionRoles = computed(() => {
  if (!selectedPermission.value) return []
  const permissionId = selectedPermission.value.id
  return roles.value
    .filter((role) => role.status === 'active')
    .map((role) => ({
      role,
      enabled: roleHasPermission(role.id, permissionId),
      employeeCount: employees.value.filter(
        (employee) => employee.employment_status === 'active' && employeeHasRole(employee, role.id),
      ).length,
    }))
})

const selectedPermissionEffectiveEmployees = computed(() => {
  if (!selectedPermission.value) return []
  const permissionId = selectedPermission.value.id
  const allowedRoleIds = new Set(
    roles.value
      .filter((role) => role.status === 'active' && roleHasPermission(role.id, permissionId))
      .map((role) => role.id),
  )
  return employees.value.filter(
    (employee) =>
      employee.employment_status === 'active' &&
      employee.roles.some((role) => allowedRoleIds.has(role.role_id)),
  )
})

const filteredRoles = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  if (!keyword) return roles.value
  return roles.value.filter((role) =>
    [role.name, role.code, role.group_name, role.description].some((value) =>
      String(value || '').toLowerCase().includes(keyword),
    ),
  )
})

const selectedRolePermissions = computed(() => {
  if (!selectedRole.value) return []
  const ids = new Set(roleDraft(selectedRole.value.id))
  return permissions.value.filter((item) => ids.has(item.id))
})

const filteredEmployees = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  if (!keyword) return employees.value
  return employees.value.filter((employee) =>
    [
      employee.display_name,
      employee.username,
      employee.employee_no,
      employee.primary_group_name,
      ...employee.roles.map((role) => role.name),
    ].some((value) => String(value || '').toLowerCase().includes(keyword)),
  )
})

const employeeTotalPages = computed(() =>
  Math.max(1, Math.ceil(filteredEmployees.value.length / employeePageSize.value)),
)

const pagedEmployees = computed(() => {
  const start = (employeePage.value - 1) * employeePageSize.value
  return filteredEmployees.value.slice(start, start + employeePageSize.value)
})

const employeePageNumbers = computed(() => {
  const total = employeeTotalPages.value
  if (total <= 5) return Array.from({ length: total }, (_, index) => index + 1)
  const start = Math.min(Math.max(1, employeePage.value - 2), total - 4)
  return Array.from({ length: 5 }, (_, index) => start + index)
})

const employeeRangeStart = computed(() =>
  filteredEmployees.value.length ? (employeePage.value - 1) * employeePageSize.value + 1 : 0,
)
const employeeRangeEnd = computed(() =>
  Math.min(employeePage.value * employeePageSize.value, filteredEmployees.value.length),
)

function scrollEmployeeListToSelection() {
  void nextTick(() => {
    const list = employeeListEl.value
    if (!list) return
    const active = list.querySelector<HTMLElement>('.active')
    if (active) {
      active.scrollIntoView({ block: 'nearest' })
    } else {
      list.scrollTop = 0
    }
  })
}

function setEmployeePage(page: number) {
  employeePage.value = Math.min(Math.max(1, page), employeeTotalPages.value)
  scrollEmployeeListToSelection()
}

function changeEmployeePageSize() {
  employeePage.value = 1
  scrollEmployeeListToSelection()
}

function selectEmployee(employeeId: number) {
  selectedEmployeeId.value = employeeId
  scrollEmployeeListToSelection()
}

function revealSelectedEmployeePage() {
  if (!selectedEmployeeId.value) return
  const index = filteredEmployees.value.findIndex((item) => item.id === selectedEmployeeId.value)
  if (index < 0) return
  employeePage.value = Math.floor(index / employeePageSize.value) + 1
  scrollEmployeeListToSelection()
}

const employeeEffectivePermissions = computed(() => {
  const employee = selectedEmployee.value
  if (!employee) return []
  const employeeRoleIds = new Set(employee.roles.map((role) => role.role_id))
  return permissions.value
    .map((permission) => {
      const sourceRoles = roles.value.filter(
        (role) =>
          role.status === 'active' &&
          employeeRoleIds.has(role.id) &&
          roleHasPermission(role.id, permission.id),
      )
      return { permission, sourceRoles }
    })
    .filter((item) => item.sourceRoles.length > 0)
    .sort((a, b) =>
      businessGroupForPermission(a.permission).localeCompare(
        businessGroupForPermission(b.permission),
        'zh-CN',
      ),
    )
})

const aiTimeDiagnostics = computed(() => {
  const view = permissions.value.find((item) => item.code === 'commercial.ai_time.view')
  const request = permissions.value.find((item) => item.code === 'commercial.ai_time.request')
  if (!view || !request) return []
  return roles.value
    .filter(
      (role) =>
        role.status === 'active' &&
        roleHasPermission(role.id, request.id) &&
        !roleHasPermission(role.id, view.id),
    )
    .map((role) => ({
      role,
      message: '拥有“申请增加 AI 时长”，但缺少“查看 AI 时长”，业务权限组合不完整。',
    }))
})

const pendingImpactEmployees = computed(() => {
  if (!selectedPermission.value) return []
  return selectedPermissionEffectiveEmployees.value
})

function initializeSelections() {
  const draft: Record<number, number[]> = {}
  for (const role of roles.value) {
    draft[role.id] = role.permissions.map((item) => item.id)
  }
  rolePermissionDrafts.value = draft
  dirtyRoleIds.value = []

  if (!businessGroups.value.some((item) => item.name === selectedBusinessGroup.value)) {
    selectedBusinessGroup.value = businessGroups.value[0]?.name || ''
  }
  const groupPermissions = permissions.value.filter(
    (item) => businessGroupForPermission(item) === selectedBusinessGroup.value,
  )
  if (!selectedPermissionId.value || !permissions.value.some((item) => item.id === selectedPermissionId.value)) {
    selectedPermissionId.value = groupPermissions[0]?.id || permissions.value[0]?.id || null
  }
  if (!selectedRoleId.value || !roles.value.some((item) => item.id === selectedRoleId.value)) {
    selectedRoleId.value = roles.value[0]?.id || null
  }
  if (!selectedEmployeeId.value || !employees.value.some((item) => item.id === selectedEmployeeId.value)) {
    selectedEmployeeId.value = employees.value[0]?.id || null
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    dashboard.value = await getStaffPermissionCenter()
    initializeSelections()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取权限中心失败'
  } finally {
    loading.value = false
  }
}

function selectBusinessGroup(name: string) {
  selectedBusinessGroup.value = name
  const first = permissions.value.find((item) => businessGroupForPermission(item) === name)
  selectedPermissionId.value = first?.id || null
}

async function saveChanges() {
  if (!dirtyRoleIds.value.length || saving.value) return
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    await Promise.all(
      dirtyRoleIds.value.map((roleId) =>
        updateStaffPermissionCenterRole(roleId, roleDraft(roleId)),
      ),
    )
    notice.value = '角色权限已更新，新的权限关系立即生效。'
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存权限失败'
  } finally {
    saving.value = false
  }
}

function switchTab(tab: CenterTab) {
  activeTab.value = tab
  search.value = ''
  if (tab === 'employee') {
    void nextTick(revealSelectedEmployeePage)
  }
}

watch(search, () => {
  if (activeTab.value !== 'employee') return
  employeePage.value = 1
  void nextTick(() => {
    if (employeeListEl.value) employeeListEl.value.scrollTop = 0
  })
})

watch([() => filteredEmployees.value.length, employeePageSize], () => {
  if (employeePage.value > employeeTotalPages.value) {
    employeePage.value = employeeTotalPages.value
  }
})

onMounted(load)
</script>

<template>
  <div class="management-page permission-center-page">
    <ModulePageNav hub="staff" active-title="权限中心" />

    <section class="permission-center-hero">
      <div>
        <p class="section-kicker">BUSINESS PERMISSION CENTER</p>
        <h2>权限中心</h2>
        <p>从业务能力反查角色和员工，查看最终权限来源，并在授权前预览实际影响范围。</p>
      </div>
      <div class="permission-center-hero-actions">
        <span v-if="dirtyRoleIds.length" class="pending-badge">{{ dirtyRoleIds.length }} 个角色待保存</span>
        <button class="ghost-button" type="button" :disabled="loading" @click="load">
          {{ loading ? '刷新中...' : '刷新数据' }}
        </button>
        <button class="primary-button" type="button" :disabled="!dirtyRoleIds.length || saving" @click="saveChanges">
          {{ saving ? '保存中...' : '保存权限变更' }}
        </button>
      </div>
    </section>

    <p v-if="error" class="auth-error">{{ error }}</p>
    <p v-if="notice" class="settings-success">{{ notice }}</p>

    <section v-if="dashboard" class="permission-center-shell">
      <div class="permission-center-tabs">
        <button :class="{ active: activeTab === 'role' }" type="button" @click="switchTab('role')">角色看权限</button>
        <button :class="{ active: activeTab === 'permission' }" type="button" @click="switchTab('permission')">权限看角色</button>
        <button :class="{ active: activeTab === 'employee' }" type="button" @click="switchTab('employee')">员工权限追踪</button>
      </div>

      <div v-if="aiTimeDiagnostics.length" class="permission-diagnostic">
        <div>
          <strong>发现 {{ aiTimeDiagnostics.length }} 个 AI 时长权限组合问题</strong>
          <span>“申请增加时长”必须同时具备“查看 AI 时长”，否则页面会出现能申请但找不到业务对象的问题。</span>
        </div>
        <ul>
          <li v-for="item in aiTimeDiagnostics" :key="item.role.id">
            <b>{{ item.role.group_name }} · {{ item.role.name }}</b>
            <span>{{ item.message }}</span>
          </li>
        </ul>
      </div>

      <div class="permission-center-search">
        <span>⌕</span>
        <input v-model="search" type="search" :placeholder="activeTab === 'employee' ? '搜索员工、账号、工号或角色' : activeTab === 'role' ? '搜索角色、部门或角色编码' : '搜索业务能力或权限代码'" />
      </div>

      <div v-if="activeTab === 'permission'" class="permission-center-grid permission-reverse-grid">
        <aside class="permission-business-nav">
          <header>
            <strong>业务功能</strong>
            <small>{{ permissions.length }} 项权限</small>
          </header>
          <button
            v-for="group in businessGroups"
            :key="group.name"
            type="button"
            :class="{ active: selectedBusinessGroup === group.name }"
            @click="selectBusinessGroup(group.name)"
          >
            <span>{{ group.name }}</span>
            <b>{{ group.count }}</b>
          </button>
        </aside>

        <section class="permission-capability-panel">
          <header>
            <div>
              <strong>{{ selectedBusinessGroup }}</strong>
              <small>选择一个业务能力，右侧立即反查已授权角色和实际生效员工。</small>
            </div>
          </header>
          <div class="permission-capability-list">
            <button
              v-for="permission in visibleBusinessPermissions"
              :key="permission.id"
              type="button"
              :class="{ active: selectedPermissionId === permission.id }"
              @click="selectedPermissionId = permission.id"
            >
              <span>
                <strong>{{ permission.description }}</strong>
                <small>{{ permission.code }}</small>
              </span>
              <i>›</i>
            </button>
          </div>
        </section>

        <section class="permission-role-panel">
          <template v-if="selectedPermission">
            <header class="permission-role-panel-head">
              <div>
                <span class="panel-kicker">当前业务能力</span>
                <h3>{{ selectedPermission.description }}</h3>
                <code>{{ selectedPermission.code }}</code>
              </div>
              <span class="role-count-chip">
                {{ selectedPermissionRoles.filter((item) => item.enabled).length }} 个角色已授权
              </span>
            </header>

            <div class="permission-role-list">
              <label
                v-for="item in selectedPermissionRoles"
                :key="item.role.id"
                class="permission-role-row"
                :class="{ enabled: item.enabled }"
              >
                <input
                  type="checkbox"
                  :checked="item.enabled"
                  @change="toggleRolePermission(item.role, selectedPermission.id)"
                />
                <div class="permission-role-copy">
                  <strong>{{ item.role.name }}</strong>
                  <span>{{ item.role.group_name }} · {{ scopeLabel(item.role.default_scope_type) }}</span>
                  <small>{{ item.role.code }}</small>
                </div>
                <div class="permission-role-impact">
                  <b>{{ item.employeeCount }}</b>
                  <span>名员工</span>
                </div>
              </label>
            </div>

            <div class="permission-impact-preview">
              <div>
                <strong>当前生效员工</strong>
                <span>按勾选后的角色关系实时预览</span>
              </div>
              <b>{{ pendingImpactEmployees.length }} 人</b>
              <div class="impact-employee-chips">
                <span v-for="employee in pendingImpactEmployees.slice(0, 12)" :key="employee.id">
                  {{ employee.display_name }}
                </span>
                <span v-if="pendingImpactEmployees.length > 12">+{{ pendingImpactEmployees.length - 12 }}</span>
              </div>
            </div>
          </template>
          <div v-else class="permission-empty">请选择一个业务能力</div>
        </section>
      </div>

      <div v-else-if="activeTab === 'role'" class="permission-center-grid role-permission-grid">
        <aside class="permission-role-browser">
          <header>
            <strong>角色</strong>
            <small>{{ filteredRoles.length }} 个</small>
          </header>
          <button
            v-for="role in filteredRoles"
            :key="role.id"
            type="button"
            :class="{ active: selectedRoleId === role.id }"
            @click="selectedRoleId = role.id"
          >
            <span>
              <strong>{{ role.name }}</strong>
              <small>{{ role.group_name }} · {{ role.code }}</small>
            </span>
            <b>{{ roleDraft(role.id).length }}</b>
          </button>
        </aside>

        <section v-if="selectedRole" class="role-permission-detail">
          <header>
            <div>
              <span class="panel-kicker">角色权限画像</span>
              <h3>{{ selectedRole.name }}</h3>
              <p>{{ selectedRole.group_name }} · 默认范围：{{ scopeLabel(selectedRole.default_scope_type) }}</p>
            </div>
            <strong>{{ selectedRolePermissions.length }} 项权限</strong>
          </header>

          <div class="role-business-groups">
            <article v-for="group in businessGroups" :key="group.name">
              <header>
                <strong>{{ group.name }}</strong>
                <span>
                  {{ selectedRolePermissions.filter((item) => businessGroupForPermission(item) === group.name).length }}
                </span>
              </header>
              <div>
                <label
                  v-for="permission in permissions.filter((item) => businessGroupForPermission(item) === group.name)"
                  :key="permission.id"
                  :class="{ enabled: roleHasPermission(selectedRole.id, permission.id) }"
                >
                  <input
                    type="checkbox"
                    :checked="roleHasPermission(selectedRole.id, permission.id)"
                    @change="toggleRolePermission(selectedRole, permission.id)"
                  />
                  <span>
                    <b>{{ permission.description }}</b>
                    <small>{{ permission.code }}</small>
                  </span>
                </label>
              </div>
            </article>
          </div>
        </section>
      </div>

      <div v-else class="permission-center-grid employee-trace-grid">
        <aside class="permission-employee-browser">
          <header>
            <strong>员工</strong>
            <small>{{ filteredEmployees.length }} 人</small>
          </header>

          <div ref="employeeListEl" class="permission-employee-list">
            <button
              v-for="employee in pagedEmployees"
              :key="employee.id"
              type="button"
              :class="{ active: selectedEmployeeId === employee.id }"
              @click="selectEmployee(employee.id)"
            >
              <span>
                <strong>{{ employee.display_name }}</strong>
                <small>{{ employee.primary_group_name }} · {{ employee.employee_no }}</small>
              </span>
              <b>{{ employee.roles.length }}</b>
            </button>
            <div v-if="!pagedEmployees.length" class="permission-employee-empty">没有匹配的员工</div>
          </div>

          <footer class="permission-employee-pager">
            <div class="employee-page-size">
              <span>每页</span>
              <select v-model.number="employeePageSize" @change="changeEmployeePageSize">
                <option :value="10">10</option>
                <option :value="20">20</option>
                <option :value="50">50</option>
              </select>
            </div>
            <div class="employee-page-summary">{{ employeeRangeStart }}-{{ employeeRangeEnd }} / 共 {{ filteredEmployees.length }} 人</div>
            <div v-if="employeeTotalPages > 1" class="employee-page-controls">
              <button type="button" :disabled="employeePage <= 1" @click="setEmployeePage(employeePage - 1)">‹</button>
              <button
                v-for="page in employeePageNumbers"
                :key="page"
                type="button"
                :class="{ active: employeePage === page }"
                @click="setEmployeePage(page)"
              >{{ page }}</button>
              <button type="button" :disabled="employeePage >= employeeTotalPages" @click="setEmployeePage(employeePage + 1)">›</button>
            </div>
          </footer>
        </aside>

        <section v-if="selectedEmployee" class="employee-permission-detail">
          <header>
            <div>
              <span class="panel-kicker">最终有效权限</span>
              <h3>{{ selectedEmployee.display_name }}</h3>
              <p>{{ selectedEmployee.primary_group_name }} · {{ selectedEmployee.username }}</p>
            </div>
            <strong>{{ employeeEffectivePermissions.length }} 项</strong>
          </header>

          <div class="employee-role-strip">
            <article v-for="role in selectedEmployee.roles" :key="role.role_id">
              <span>角色</span>
              <strong>{{ role.name }}</strong>
              <small>{{ role.group_name }} · {{ scopeLabel(role.scope_type) }}</small>
            </article>
          </div>

          <div class="employee-permission-trace">
            <article v-for="item in employeeEffectivePermissions" :key="item.permission.id">
              <div class="trace-permission">
                <span>{{ businessGroupForPermission(item.permission) }}</span>
                <strong>{{ item.permission.description }}</strong>
                <code>{{ item.permission.code }}</code>
              </div>
              <div class="trace-sources">
                <span>权限来源</span>
                <div v-for="role in item.sourceRoles" :key="role.id">
                  <b>{{ role.name }}</b>
                  <small>{{ role.group_name }} · {{ scopeLabel(role.default_scope_type) }}</small>
                </div>
              </div>
            </article>
          </div>
        </section>
      </div>
    </section>
  </div>
</template>

<style scoped>
.permission-center-page{width:100%}.permission-center-hero{display:flex;align-items:flex-start;justify-content:space-between;gap:24px;margin-bottom:16px;padding:22px 24px;border:1px solid rgba(96,112,190,.14);border-radius:18px;background:linear-gradient(135deg,#fff 0%,#f8faff 100%);box-shadow:0 12px 32px rgba(45,60,110,.06)}.permission-center-hero h2{margin:4px 0 7px;color:#20283a;font-size:28px}.permission-center-hero p:last-child{max-width:760px;margin:0;color:#7b8499;line-height:1.65}.permission-center-hero-actions{display:flex;align-items:center;justify-content:flex-end;gap:10px;flex-wrap:wrap}.pending-badge{padding:7px 10px;border-radius:999px;color:#8d6721;font-size:12px;font-weight:800;background:#fff4d8}.permission-center-shell{overflow:hidden;border:1px solid rgba(96,112,190,.14);border-radius:18px;background:#fff;box-shadow:0 15px 40px rgba(43,55,98,.07)}.permission-center-tabs{display:flex;gap:8px;padding:16px 18px 0}.permission-center-tabs button{min-height:40px;padding:0 18px;border:1px solid #e4e8f1;border-radius:11px;color:#6c7589;font:inherit;font-weight:800;background:#f7f8fb;cursor:pointer}.permission-center-tabs button.active{border-color:rgba(88,103,224,.35);color:#5060d3;background:#f1f3ff;box-shadow:0 4px 14px rgba(78,94,207,.08)}.permission-diagnostic{margin:16px 18px 0;padding:14px 16px;border:1px solid rgba(205,75,75,.22);border-radius:13px;background:#fff7f7}.permission-diagnostic>div{display:flex;align-items:center;justify-content:space-between;gap:16px}.permission-diagnostic strong{color:#b94747}.permission-diagnostic span{color:#8f6670;font-size:12px}.permission-diagnostic ul{display:grid;gap:6px;margin:10px 0 0;padding:0;list-style:none}.permission-diagnostic li{display:flex;gap:10px;align-items:center;padding:8px 10px;border-radius:9px;background:#fff}.permission-diagnostic li b{color:#4c5569}.permission-center-search{display:flex;align-items:center;gap:9px;margin:16px 18px;padding:0 13px;border:1px solid #e1e5ef;border-radius:11px;background:#fbfcfe}.permission-center-search span{color:#9aa3b5;font-size:18px}.permission-center-search input{width:100%;height:42px;border:0;outline:0;color:#344056;font:inherit;background:transparent}.permission-center-grid{display:grid;min-height:600px;border-top:1px solid #eef1f6}.permission-reverse-grid{grid-template-columns:210px minmax(280px,.78fr) minmax(420px,1.42fr)}.permission-business-nav,.permission-capability-panel,.permission-role-browser,.permission-employee-browser{min-width:0;border-right:1px solid #edf0f5;background:#fbfcfe}.permission-business-nav header,.permission-capability-panel header,.permission-role-browser header,.permission-employee-browser header{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:17px 16px;border-bottom:1px solid #edf0f5}.permission-business-nav header small,.permission-role-browser header small,.permission-employee-browser header small{color:#9ba4b7;font-size:11px}.permission-capability-panel header small{color:#9ba4b7;font-size:13px}.permission-capability-panel header strong{font-size:15px}.permission-business-nav>button,.permission-role-browser>button,.permission-employee-browser>button{display:flex;width:calc(100% - 16px);align-items:center;justify-content:space-between;gap:10px;margin:6px 8px;padding:11px 12px;border:1px solid transparent;border-radius:10px;color:#586278;text-align:left;background:transparent;cursor:pointer}.permission-business-nav>button:hover,.permission-role-browser>button:hover,.permission-employee-browser>button:hover{background:#f2f5fb}.permission-business-nav>button.active,.permission-role-browser>button.active,.permission-employee-browser>button.active{border-color:rgba(91,105,220,.2);color:#4756c4;background:#eef1ff}.permission-business-nav>button b,.permission-role-browser>button b,.permission-employee-browser>button b{flex:0 0 auto;padding:3px 7px;border-radius:999px;color:#7f899d;font-size:11px;background:#eef1f5}.permission-role-browser>button>span,.permission-employee-browser>button>span{display:grid;min-width:0;gap:3px}.permission-role-browser>button strong,.permission-employee-browser>button strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.permission-role-browser>button small,.permission-employee-browser>button small{overflow:hidden;color:#9aa3b4;font-size:10px;text-overflow:ellipsis;white-space:nowrap}.permission-capability-list{display:grid;padding:8px}.permission-capability-list button{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:12px;border:1px solid transparent;border-radius:11px;text-align:left;background:transparent;cursor:pointer}.permission-capability-list button:hover{background:#f5f7fb}.permission-capability-list button.active{border-color:rgba(92,107,221,.22);background:#f0f2ff}.permission-capability-list button>span{display:grid;min-width:0;gap:4px}.permission-capability-list strong{color:#414b60;font-size:15px}.permission-capability-list small{overflow:hidden;color:#9aa3b5;font:12px/1.4 ui-monospace,SFMono-Regular,Menlo,monospace;text-overflow:ellipsis;white-space:nowrap}.permission-capability-list i{color:#8993aa;font-style:normal;font-size:22px}.permission-role-panel,.role-permission-detail,.employee-permission-detail{min-width:0;padding:20px 22px}.permission-role-panel-head,.role-permission-detail>header,.employee-permission-detail>header{display:flex;align-items:flex-start;justify-content:space-between;gap:18px;padding-bottom:16px;border-bottom:1px solid #edf0f5}.panel-kicker{color:#8f99ad;font-size:10px;font-weight:800;letter-spacing:.11em}.permission-role-panel h3,.role-permission-detail h3,.employee-permission-detail h3{margin:5px 0;color:#273247}.permission-role-panel code,.trace-permission code{color:#8791a7;font-size:10px}.role-count-chip{padding:6px 9px;border-radius:999px;color:#5261cc;font-size:11px;font-weight:800;background:#eff2ff}.permission-role-list{display:grid;gap:9px;margin-top:14px}.permission-role-row{display:grid;grid-template-columns:22px minmax(0,1fr) auto;align-items:center;gap:12px;min-height:70px;padding:10px 13px;border:1px solid #edf0f5;border-radius:12px;background:#fafbfd;cursor:pointer}.permission-role-row.enabled{border-color:rgba(84,101,218,.25);background:#f3f5ff}.permission-role-row input{width:19px;height:19px;margin:0;accent-color:#596ae1}.permission-role-copy{display:grid;gap:2px}.permission-role-copy strong{color:#3f495f}.permission-role-copy span{color:#7e879b;font-size:11px}.permission-role-copy small{color:#a0a7b6;font:10px ui-monospace,SFMono-Regular,Menlo,monospace}.permission-role-impact{display:grid;justify-items:end}.permission-role-impact b{color:#4f5c76;font-size:18px}.permission-role-impact span{color:#9aa3b5;font-size:10px}.permission-impact-preview{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:12px;margin-top:16px;padding:14px;border:1px solid #e6eaf3;border-radius:12px;background:#fbfcff}.permission-impact-preview>div:first-child{display:grid;gap:3px}.permission-impact-preview>div:first-child span{color:#919aac;font-size:10px}.permission-impact-preview>b{color:#5362d0;font-size:20px}.impact-employee-chips{grid-column:1/-1;display:flex;gap:6px;flex-wrap:wrap}.impact-employee-chips span{padding:5px 8px;border-radius:999px;color:#667087;font-size:10px;background:#eef1f7}.role-permission-grid{grid-template-columns:280px minmax(0,1fr)}.employee-trace-grid{grid-template-columns:320px minmax(0,1fr)}.role-permission-detail>header p,.employee-permission-detail>header p{margin:4px 0 0;color:#8b94a8;font-size:12px}.role-permission-detail>header>strong,.employee-permission-detail>header>strong{padding:7px 10px;border-radius:999px;color:#5665d1;background:#eff2ff}.role-business-groups{display:grid;gap:12px;margin-top:15px}.role-business-groups>article{padding:14px;border:1px solid #e9ecf3;border-radius:13px;background:#fcfdff}.role-business-groups>article>header{display:flex;align-items:center;justify-content:space-between;margin-bottom:10px}.role-business-groups>article>header span{padding:3px 7px;border-radius:999px;color:#7f899d;font-size:10px;background:#eef1f5}.role-business-groups>article>div{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px}.role-business-groups label{display:grid;grid-template-columns:20px minmax(0,1fr);align-items:center;gap:9px;padding:10px;border:1px solid #eef1f5;border-radius:10px;background:#fafbfd;cursor:pointer}.role-business-groups label.enabled{border-color:rgba(82,100,216,.23);background:#f2f4ff}.role-business-groups input{width:18px;height:18px;margin:0;accent-color:#5a6be0}.role-business-groups label>span{display:grid;gap:3px}.role-business-groups b{color:#475166;font-size:11px}.role-business-groups small{overflow:hidden;color:#9aa3b5;font:9px ui-monospace,SFMono-Regular,Menlo,monospace;text-overflow:ellipsis;white-space:nowrap}.employee-role-strip{display:grid;grid-template-columns:repeat(auto-fit,minmax(170px,1fr));gap:9px;margin-top:14px}.employee-role-strip article{display:grid;gap:3px;padding:11px;border:1px solid #e8ebf3;border-radius:11px;background:#fafbfd}.employee-role-strip span{color:#9aa3b5;font-size:9px}.employee-role-strip strong{color:#465168}.employee-role-strip small{color:#8993a7;font-size:10px}.employee-permission-trace{display:grid;gap:8px;margin-top:15px}.employee-permission-trace>article{display:grid;grid-template-columns:minmax(0,.9fr) minmax(240px,1.1fr);gap:14px;padding:12px 14px;border:1px solid #e7ebf3;border-radius:12px;background:#fbfcff;transition:background .16s ease,border-color .16s ease,box-shadow .16s ease}.employee-permission-trace>article:nth-child(even){background:#f5f7fb;border-color:#e3e7f0}.employee-permission-trace>article:hover{border-color:#d8deeb;background:#fff;box-shadow:0 5px 16px rgba(47,58,94,.05)}.trace-permission{display:grid;gap:4px}.trace-sources{display:grid;gap:7px;justify-items:start}.trace-permission>span,.trace-sources>span{color:#7f8ba5;font-size:11px;font-weight:850}.trace-permission strong{color:#404a60}.trace-sources>div{display:inline-flex;max-width:100%;align-items:center;justify-content:flex-start;gap:9px;padding:7px 11px;border:1px solid rgba(86,103,219,.20);border-radius:999px;background:linear-gradient(135deg,#eef2ff 0%,#f5f2ff 100%);box-shadow:0 3px 10px rgba(70,84,170,.05)}.trace-sources>div:nth-of-type(even){border-color:rgba(53,151,143,.20);background:linear-gradient(135deg,#edf9f6 0%,#f3fbf8 100%)}.trace-sources b{color:#4656bd;font-size:13px;font-weight:850}.trace-sources>div:nth-of-type(even) b{color:#2f8178}.trace-sources small{color:#77839b;font-size:11px}.permission-empty{display:grid;min-height:420px;place-items:center;color:#9aa3b5}.permission-center-page :deep(.primary-button),.permission-center-page :deep(.ghost-button){min-height:40px}


.employee-trace-grid{align-items:start}
.employee-trace-grid .permission-employee-browser{display:flex;max-height:min(680px,calc(100dvh - 260px));align-self:start;flex-direction:column;overflow:hidden;border-right:1px solid #edf0f5;border-bottom:1px solid #edf0f5;background:#fbfcfe}
.employee-trace-grid .permission-employee-browser>header{flex:0 0 auto;padding:15px 16px}
.permission-employee-list{flex:0 1 auto;min-height:0;max-height:520px;overflow-y:auto;padding:6px 0;scrollbar-width:thin;scrollbar-color:#c8cfdd transparent;overscroll-behavior:contain}
.permission-employee-list::-webkit-scrollbar{width:7px}
.permission-employee-list::-webkit-scrollbar-thumb{border:2px solid transparent;border-radius:999px;background:#c8cfdd;background-clip:padding-box}
.permission-employee-list>button{display:flex;width:calc(100% - 16px);min-height:62px;align-items:center;justify-content:space-between;gap:10px;margin:4px 8px;padding:10px 12px;border:1px solid transparent;border-radius:10px;color:#586278;text-align:left;background:transparent;cursor:pointer;transition:background .16s ease,border-color .16s ease,box-shadow .16s ease}
.permission-employee-list>button:hover{background:#f2f5fb}
.permission-employee-list>button.active{border-color:rgba(91,105,220,.24);color:#4756c4;background:linear-gradient(135deg,#eef1ff 0%,#f5f6ff 100%);box-shadow:0 5px 14px rgba(78,94,207,.06)}
.permission-employee-list>button>span{display:grid;min-width:0;gap:3px}
.permission-employee-list>button strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.permission-employee-list>button small{overflow:hidden;color:#9aa3b4;font-size:10px;text-overflow:ellipsis;white-space:nowrap}
.permission-employee-list>button>b{flex:0 0 auto;padding:3px 7px;border-radius:999px;color:#7f899d;font-size:11px;background:#eef1f5}
.permission-employee-empty{display:grid;min-height:120px;place-items:center;color:#9aa3b5;font-size:11px}
.permission-employee-pager{display:flex;flex:0 0 auto;align-items:center;justify-content:space-between;gap:8px;padding:9px 10px;border-top:1px solid #e8ecf3;background:rgba(255,255,255,.98);box-shadow:0 -5px 16px rgba(42,53,90,.03)}
.employee-page-size{display:flex;flex:0 0 auto;align-items:center;gap:6px;color:#8993a8;font-size:10px;white-space:nowrap}
.employee-page-size select{width:58px;height:30px;padding:0 20px 0 8px;border:1px solid #dfe4ee;border-radius:8px;outline:none;color:#59647b;font:inherit;background:#fff}
.employee-page-summary{flex:1 1 auto;color:#98a1b3;font-size:10px;text-align:center;white-space:nowrap}
.employee-page-controls{display:flex;flex:0 0 auto;align-items:center;justify-content:flex-end;gap:4px}
.employee-page-controls button{display:grid;width:28px;height:28px;padding:0;place-items:center;border:1px solid #e1e5ee;border-radius:8px;color:#69748a;font:inherit;font-size:10px;font-weight:800;background:#fff;cursor:pointer}
.employee-page-controls button:hover:not(:disabled){border-color:#cfd5e8;background:#f4f6fb}
.employee-page-controls button.active{border-color:rgba(84,101,216,.30);color:#4e5dcc;background:#edf0ff}
.employee-page-controls button:disabled{opacity:.4;cursor:not-allowed}
.employee-page-controls button:first-child,.employee-page-controls button:last-child{font-size:15px}
@media(max-width:1100px){.permission-reverse-grid{grid-template-columns:190px minmax(240px,.8fr) minmax(340px,1.2fr)}.role-business-groups>article>div{grid-template-columns:1fr}}
@media(max-width:820px){.permission-center-hero{flex-direction:column}.permission-center-hero-actions{justify-content:flex-start}.permission-reverse-grid,.role-permission-grid,.employee-trace-grid{grid-template-columns:1fr}.permission-business-nav,.permission-capability-panel,.permission-role-browser,.permission-employee-browser{border-right:0;border-bottom:1px solid #edf0f5}.permission-business-nav{display:flex;overflow-x:auto;padding:8px}.permission-business-nav header{display:none}.permission-business-nav>button{flex:0 0 auto;width:auto;margin:0}.employee-permission-trace>article{grid-template-columns:1fr}.permission-center-tabs{overflow-x:auto}.permission-center-tabs button{flex:0 0 auto}}
</style>
