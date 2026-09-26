<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { confirmAction } from '../uiFeedback'
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  createStaffEmployee,
  createStaffGroup,
  createStaffRole,
  disableStaffEmployee,
  getStaffDashboard,
  replaceStaffEmployeeRoles,
  resetStaffEmployeePassword,
  updateStaffApprovalPolicy,
  updateStaffGroup,
  updateStaffRole,
} from '../api'
import CredentialResultModal from '../components/CredentialResultModal.vue'
import RegionSelect from '../components/RegionSelect.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import type {
  InitialCredential,
  StaffApprovalPolicySummary,
  StaffDashboard,
  StaffEmployeeSummary,
  StaffGroupSummary,
  StaffPermissionSummary,
  StaffRoleSummary,
} from '../types'

type StaffTab = 'employees' | 'roles' | 'approvals'

const props = defineProps<{ initialTab?: StaffTab; groupsOnly?: boolean }>()

const route = useRoute()

const dashboard = ref<StaffDashboard | null>(null)
const loading = ref(false)
const error = useFeedbackErrorRef()
const notice = ref('')
const selectedGroupId = ref<number | null>(null)
const activeTab = ref<StaffTab>(props.initialTab ?? 'employees')

watch(
  () => props.initialTab,
  (value) => {
    if (value) activeTab.value = value
  },
)

const showEmployeeModal = ref(false)
const showGroupModal = ref(false)
const showRoleModal = ref(false)
const showEmployeeRolesModal = ref(false)

const editingGroup = ref<StaffGroupSummary | null>(null)
const editingRole = ref<StaffRoleSummary | null>(null)
const editingEmployee = ref<StaffEmployeeSummary | null>(null)
const editingPolicy = ref<StaffApprovalPolicySummary | null>(null)
const policyMode = ref('manual')
const policyThreshold = ref(0)
const policyApproverRoleCode = ref('finance_reviewer')
const policyStatus = ref<'active' | 'disabled'>('active')

const employeeNo = ref('')
const employeeFullNo = computed(() => {
  const digits = employeeNo.value.replace(/\\D/g, '').slice(0, 6)
  return digits ? 'EMP-' + digits.padStart(6, '0') : ''
})
const employeeNoDuplicate = computed(() =>
  Boolean(
    employeeFullNo.value &&
      dashboard.value?.employees.some((item) => item.employee_no === employeeFullNo.value),
  ),
)
const employeeUsername = ref('')
const employeeName = ref('')
const employeePhone = ref('')
const employeeEmail = ref('')
const employeeProvince = ref('')
const employeeCity = ref('')
const employeeDistrict = ref('')
const employeeDelivery = ref<'copy' | 'email'>('copy')
const employeeRoleIds = ref<number[]>([])
const employeeRoleSearch = ref('')
const employeeRoleExpandedGroupIds = ref<number[]>([])

const groupCode = ref('')
const groupName = ref('')
const groupDescription = ref('')
const groupStatus = ref<'active' | 'disabled'>('active')

const roleCode = ref('')
const roleName = ref('')
const roleDescription = ref('')
const roleIsManager = ref(false)
const roleScope = ref('self')
const roleStatus = ref<'active' | 'disabled'>('active')
const rolePermissionIds = ref<number[]>([])
const roleEditorRoleId = ref<number | null>(null)

const employeeEditRoleIds = ref<number[]>([])
const saving = ref(false)

const credentialOpen = ref(false)
const credential = ref<InitialCredential | null>(null)
const credentialName = ref('')
const credentialUsername = ref('')
const credentialTitle = ref('员工账号已创建')

const access = computed(() => dashboard.value?.access ?? null)
const groups = computed(() => dashboard.value?.groups ?? [])
const roles = computed(() => dashboard.value?.roles ?? [])
const employees = computed(() => dashboard.value?.employees ?? [])
const permissions = computed(() => dashboard.value?.permissions ?? [])
const policies = computed(() => dashboard.value?.approval_policies ?? [])

const selectedGroup = computed(
  () =>
    groups.value.find((item) => item.id === selectedGroupId.value) ??
    groups.value[0] ??
    null,
)

const selectedRoles = computed(() =>
  roles.value.filter((item) => item.group_id === selectedGroup.value?.id),
)

const roleEditorOptions = computed(() =>
  selectedRoles.value.filter((item) => item.status === 'active' || item.id === editingRole.value?.id),
)

const selectedEmployees = computed(() =>
  employees.value.filter((item) => {
    const groupId = selectedGroup.value?.id
    if (!groupId) return false
    if (item.primary_group_id === groupId) return true
    return item.groups?.some((group) => group.group_id === groupId) === true
  }),
)

const isSuperAdmin = computed(() => access.value?.is_super_admin === true)

const financeApprovalRoles = computed(() =>
  roles.value.filter(
    (role) =>
      role.group_code === 'finance' &&
      role.status === 'active' &&
      role.permission_codes.some((code) => code.endsWith('.approve')),
  ),
)

const permissionModules = computed(() => {
  const grouped = new Map<string, StaffPermissionSummary[]>()
  for (const item of permissions.value) {
    const list = grouped.get(item.module) ?? []
    list.push(item)
    grouped.set(item.module, list)
  }
  return Array.from(grouped.entries()).map(([module, items]) => ({
    module,
    items,
  }))
})

function hasPermission(code: string) {
  if (isSuperAdmin.value) return true
  return access.value?.permissions.includes(code) === true
}

function permissionScope(code: string) {
  if (isSuperAdmin.value) return 'all'
  return access.value?.permission_scopes?.[code] || ''
}

function canManageGroup(code: string, groupId: number) {
  if (isSuperAdmin.value) return true
  const scope = permissionScope(code)
  if (scope === 'all' || scope === 'all_internal') return true
  if (scope === 'group') {
    const groupIds = access.value?.permission_group_ids?.[code] ?? []
    if (groupIds.length) return groupIds.includes(groupId)
    return access.value?.primary_group_id === groupId
  }
  if (scope === 'managed_groups') {
    return access.value?.managed_group_ids.includes(groupId) === true
  }
  return false
}

const canCreateEmployee = computed(
  () =>
    Boolean(selectedGroup.value) &&
    hasPermission('staff.employee.create') &&
    canManageGroup('staff.employee.create', selectedGroup.value!.id),
)

const canAssignEmployeeRoles = computed(
  () =>
    Boolean(selectedGroup.value) &&
    hasPermission('staff.employee.role_assign') &&
    canManageGroup('staff.employee.role_assign', selectedGroup.value!.id),
)

function canDisableEmployeeAccount(item: StaffEmployeeSummary) {
  return (
    hasPermission('staff.employee.disable') &&
    canManageGroup('staff.employee.disable', item.primary_group_id)
  )
}

const canAssignManagerRole = computed(
  () =>
    isSuperAdmin.value ||
    permissionScope('staff.employee.role_assign') === 'all_internal',
)

function canAssignRole(role: StaffRoleSummary, creating = false) {
  if (role.status !== 'active') return false
  if (role.is_group_manager && !canAssignManagerRole.value) return false
  if (isSuperAdmin.value) return true
  if (
    creating &&
    role.group_id === selectedGroup.value?.id &&
    canCreateEmployee.value
  ) {
    return true
  }
  return (
    hasPermission('staff.employee.role_assign') &&
    canManageGroup('staff.employee.role_assign', role.group_id)
  )
}

const assignableEmployeeRoles = computed(() =>
  roles.value.filter((role) => canAssignRole(role, true)),
)

const selectedEmployeeCreateRoles = computed(() =>
  employeeRoleIds.value
    .map((roleId) => assignableEmployeeRoles.value.find((role) => role.id === roleId))
    .filter((role): role is StaffRoleSummary => Boolean(role)),
)

const employeeRoleTree = computed(() => {
  const keyword = employeeRoleSearch.value.trim().toLowerCase()
  const grouped = groups.value
    .map((group) => {
      const items = assignableEmployeeRoles.value.filter((role) => role.group_id === group.id)
      const visibleItems = keyword
        ? items.filter((role) =>
            [role.name, role.code, role.group_name, role.description]
              .some((value) => String(value || '').toLowerCase().includes(keyword)),
          )
        : items
      return { group, items: visibleItems, total: items.length }
    })
    .filter((entry) => entry.items.length > 0)

  return grouped.sort((a, b) => {
    if (a.group.id === selectedGroup.value?.id) return -1
    if (b.group.id === selectedGroup.value?.id) return 1
    return a.group.sort_order - b.group.sort_order || a.group.name.localeCompare(b.group.name, 'zh-CN')
  })
})

function employeeRoleGroupExpanded(groupId: number) {
  return employeeRoleSearch.value.trim() !== '' || employeeRoleExpandedGroupIds.value.includes(groupId)
}

function toggleEmployeeRoleGroup(groupId: number) {
  if (employeeRoleExpandedGroupIds.value.includes(groupId)) {
    employeeRoleExpandedGroupIds.value = employeeRoleExpandedGroupIds.value.filter((id) => id !== groupId)
  } else {
    employeeRoleExpandedGroupIds.value = [...employeeRoleExpandedGroupIds.value, groupId]
  }
}

function employeeRoleGroupSelectedCount(groupId: number) {
  return assignableEmployeeRoles.value.filter(
    (role) => role.group_id === groupId && employeeRoleIds.value.includes(role.id),
  ).length
}

const editableEmployeeRoles = computed(() => {
  const assigned = new Set(employeeEditRoleIds.value)
  return roles.value.filter(
    (role) => assigned.has(role.id) || canAssignRole(role, false),
  )
})

function canEditEmployeeRole(role: StaffRoleSummary) {
  return canAssignRole(role, false)
}

const mutuallyExclusiveLiveOpsRoleCodes = new Set([
  'live_operations_manager',
  'live_operations_staff',
])

function toggleEmployeeRole(role: StaffRoleSummary, editing = false) {
  if (editing && !canEditEmployeeRole(role)) return
  if (!editing && !canAssignRole(role, true)) return
  const selected = editing ? employeeEditRoleIds : employeeRoleIds
  const isSelected = selected.value.includes(role.id)

  if (isSelected) {
    selected.value = selected.value.filter((id) => id !== role.id)
    return
  }

  let next = selected.value
  if (mutuallyExclusiveLiveOpsRoleCodes.has(role.code)) {
    const conflictingRoleIds = new Set(
      roles.value
        .filter(
          (item) =>
            item.id !== role.id &&
            mutuallyExclusiveLiveOpsRoleCodes.has(item.code),
        )
        .map((item) => item.id),
    )
    next = next.filter((id) => !conflictingRoleIds.has(id))
  }

  selected.value = [...next, role.id]
}

function scopeLabel(value: string) {
  if (value === 'self') return '本人'
  if (value === 'assigned') return '已分配业务'
  if (value === 'group') return '本部门'
  if (value === 'managed_groups') return '负责的部门'
  if (value === 'all_internal') return '全部内部员工'
  if (value === 'all') return '全系统'
  return value
}

function statusLabel(value: string) {
  if (value === 'active') return '正常'
  if (value === 'disabled') return '已停用'
  return value
}

function moduleLabel(value: string) {
  const labels: Record<string, string> = {
    system: '系统',
    staff: '员工体系',
    customer: '终端',
    agent: '代理',
    sales: '销售',
    finance: '财务',
    commercial: '商业运营',
    resources: '资源账户',
    invitations: '邀请与推荐',
    audit: '审计',
    after_sales: '售后管理',
    inventory: '仓储库存',
    logistics: '物流管理',
    liveops: '直播运维',
    livepolicy: '直播策略',
    livecoach: '主播训练',
    livevoice: '声音能力',
    liveanalysis: '直播分析',
  }
  return labels[value] || value
}

function permissionModuleSelectedCount(items: StaffPermissionSummary[]) {
  return items.reduce((count, item) => count + (rolePermissionIds.value.includes(item.id) ? 1 : 0), 0)
}

function setSelectedGroup(groupId: number) {
  selectedGroupId.value = groupId
  activeTab.value = 'employees'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    dashboard.value = await getStaffDashboard()

    const requestedGroup = String(route.query.group || '').trim()
    const requested = dashboard.value.groups.find(
      (item) => item.code === requestedGroup,
    )
    const current = dashboard.value.access.primary_group_id

    if (requested) {
      selectedGroupId.value = requested.id
    } else if (
      !selectedGroupId.value ||
      !dashboard.value.groups.some((item) => item.id === selectedGroupId.value)
    ) {
      selectedGroupId.value =
        current && dashboard.value.groups.some((item) => item.id === current)
          ? current
          : dashboard.value.groups[0]?.id ?? null
    }
  } catch (value) {
    error.value =
      value instanceof Error ? value.message : '读取组织架构失败'
  } finally {
    loading.value = false
  }
}

function resetEmployeeForm() {
  employeeNo.value = ''
  employeeUsername.value = ''
  employeeName.value = ''
  employeePhone.value = ''
  employeeEmail.value = ''
  employeeProvince.value = ''
  employeeCity.value = ''
  employeeDistrict.value = ''
  employeeDelivery.value = 'copy'
  employeeRoleIds.value = []
  employeeRoleSearch.value = ''
  employeeRoleExpandedGroupIds.value = []
}

function openEmployeeCreate() {
  if (!selectedGroup.value) return
  resetEmployeeForm()
  const firstRole = assignableEmployeeRoles.value.find(
    (item) =>
      item.status === 'active' && item.group_id === selectedGroup.value?.id,
  )
  if (firstRole) employeeRoleIds.value = [firstRole.id]
  employeeRoleExpandedGroupIds.value = [selectedGroup.value.id]
  showEmployeeModal.value = true
  error.value = ''
}

async function submitEmployee() {
  if (!selectedGroup.value || saving.value) return
  if (employeeNoDuplicate.value) {
    error.value = '员工编号已存在，请更换'
    return
  }
  if (!employeeFullNo.value) {
    error.value = '请填写员工编号数字'
    return
  }
  if (!employeeRoleIds.value.length) {
    error.value = '至少选择一个员工岗位'
    return
  }
  if (
    !employeeRoleIds.value.some((roleId) =>
      roles.value.some(
        (role) => role.id === roleId && role.group_id === selectedGroup.value?.id,
      ),
    )
  ) {
    error.value = '主部门必须至少选择一个岗位'
    return
  }
  if (
    !employeeProvince.value ||
    !employeeCity.value ||
    !employeeDistrict.value
  ) {
    error.value = '省、市、区/县必须选择'
    return
  }
  if (employeeDelivery.value === 'email' && !employeeEmail.value.trim()) {
    error.value = '选择邮件发送时必须填写邮箱'
    return
  }

  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await createStaffEmployee({
      employee_no: employeeFullNo.value,
      primary_group_id: selectedGroup.value.id,
      role_ids: employeeRoleIds.value,
      username: employeeUsername.value.trim(),
      display_name: employeeName.value.trim(),
      phone: employeePhone.value.trim(),
      email: employeeEmail.value.trim(),
      province: employeeProvince.value,
      city: employeeCity.value,
      district: employeeDistrict.value,
      delivery_method: employeeDelivery.value,
    })

    credentialName.value = result.item.display_name
    credentialUsername.value = result.item.username
    credentialTitle.value = '员工账号已创建'
    credential.value = result.credential
    showEmployeeModal.value = false
    credentialOpen.value = true
    notice.value = '员工账号已创建。'
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '创建员工失败'
  } finally {
    saving.value = false
  }
}

function openGroupCreate() {
  editingGroup.value = null
  groupCode.value = ''
  groupName.value = ''
  groupDescription.value = ''
  groupStatus.value = 'active'
  showGroupModal.value = true
}

function openGroupEdit(item: StaffGroupSummary) {
  editingGroup.value = item
  groupCode.value = item.code
  groupName.value = item.name
  groupDescription.value = item.description
  groupStatus.value = item.status === 'disabled' ? 'disabled' : 'active'
  showGroupModal.value = true
}

async function submitGroup() {
  if (saving.value) return
  saving.value = true
  error.value = ''
  try {
    if (editingGroup.value) {
      await updateStaffGroup(editingGroup.value.id, {
        name: groupName.value.trim(),
        description: groupDescription.value.trim(),
        status: groupStatus.value,
      })
      notice.value = '部门已更新。'
    } else {
      const created = await createStaffGroup({
        code: groupCode.value.trim(),
        name: groupName.value.trim(),
        description: groupDescription.value.trim(),
      })
      selectedGroupId.value = created.id
      notice.value = '部门已创建。'
    }
    showGroupModal.value = false
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存部门失败'
  } finally {
    saving.value = false
  }
}

function resetRoleForm() {
  editingRole.value = null
  roleEditorRoleId.value = null
  roleCode.value = ''
  roleName.value = ''
  roleDescription.value = ''
  roleIsManager.value = false
  roleScope.value = 'self'
  roleStatus.value = 'active'
  rolePermissionIds.value = []
}

function loadRoleIntoEditor(item: StaffRoleSummary) {
  editingRole.value = item
  roleEditorRoleId.value = item.id
  roleCode.value = item.code
  roleName.value = item.name
  roleDescription.value = item.description
  roleIsManager.value = item.is_group_manager
  roleScope.value = item.default_scope_type
  roleStatus.value = item.status === 'disabled' ? 'disabled' : 'active'
  rolePermissionIds.value = item.permissions.map((permission) => permission.id)
}

function changeRoleEditorSelection() {
  if (!roleEditorRoleId.value) return
  const nextRole = selectedRoles.value.find((item) => item.id === roleEditorRoleId.value)
  if (nextRole) loadRoleIntoEditor(nextRole)
}

function openRoleCreate() {
  resetRoleForm()
  showRoleModal.value = true
}

function openRoleEdit(item: StaffRoleSummary) {
  loadRoleIntoEditor(item)
  showRoleModal.value = true
}

function togglePermission(permissionId: number) {
  if (rolePermissionIds.value.includes(permissionId)) {
    rolePermissionIds.value = rolePermissionIds.value.filter(
      (item) => item !== permissionId,
    )
  } else {
    rolePermissionIds.value = [...rolePermissionIds.value, permissionId]
  }
}

async function submitRole() {
  if (!selectedGroup.value || saving.value) return
  saving.value = true
  error.value = ''
  try {
    if (editingRole.value) {
      await updateStaffRole(editingRole.value.id, {
        name: roleName.value.trim(),
        description: roleDescription.value.trim(),
        is_group_manager: roleIsManager.value,
        default_scope_type: roleScope.value,
        status: roleStatus.value,
        permission_ids: rolePermissionIds.value,
      })
      notice.value = '角色权限已更新。'
    } else {
      await createStaffRole({
        group_id: selectedGroup.value.id,
        code: roleCode.value.trim(),
        name: roleName.value.trim(),
        description: roleDescription.value.trim(),
        is_group_manager: roleIsManager.value,
        default_scope_type: roleScope.value,
        permission_ids: rolePermissionIds.value,
      })
      notice.value = '角色已创建。'
    }
    showRoleModal.value = false
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存角色失败'
  } finally {
    saving.value = false
  }
}

function openEmployeeRoleEdit(item: StaffEmployeeSummary) {
  editingEmployee.value = item
  employeeEditRoleIds.value = item.roles.map((role) => role.role_id)
  showEmployeeRolesModal.value = true
}

async function submitEmployeeRoles() {
  if (!editingEmployee.value || saving.value) return
  if (!employeeEditRoleIds.value.length) {
    error.value = '至少保留一个角色'
    return
  }
  saving.value = true
  error.value = ''
  try {
    await replaceStaffEmployeeRoles(
      editingEmployee.value.id,
      employeeEditRoleIds.value,
    )
    showEmployeeRolesModal.value = false
    notice.value = '员工部门职责已更新。'
    await load()
  } catch (value) {
    error.value =
      value instanceof Error ? value.message : '更新员工部门职责失败'
  } finally {
    saving.value = false
  }
}

async function resetEmployeePassword(item: StaffEmployeeSummary) {
  if (!isSuperAdmin.value || item.employment_status !== 'active') return
  if (!(await confirmAction({
    title: '重置员工密码',
    message: '确认重置员工“' + item.display_name + '”的密码吗？重置后该员工现有登录会话会立即失效，并在下次登录时强制修改密码。',
    confirmText: '确认重置',
    danger: true,
  }))) return

  error.value = ''
  notice.value = ''
  try {
    const result = await resetStaffEmployeePassword(item.id)
    credentialName.value = result.item.display_name
    credentialUsername.value = result.item.username
    credentialTitle.value = '员工密码已重置'
    credential.value = result.credential
    credentialOpen.value = true
    notice.value = '员工密码已重置，旧会话已注销。'
  } catch (value) {
    error.value = value instanceof Error ? value.message : '重置员工密码失败'
  }
}

async function disableEmployee(item: StaffEmployeeSummary) {
  if (!(await confirmAction({
    title: '停用员工',
    message: '确认停用员工“' + item.display_name + '”吗？停用后现有登录会话会立即失效。',
    confirmText: '确认停用',
    danger: true,
  }))) return
  error.value = ''
  try {
    await disableStaffEmployee(item.id)
    notice.value = '员工已停用。'
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '停用员工失败'
  }
}

function openPolicyEdit(item: StaffApprovalPolicySummary) {
  editingPolicy.value = item
  policyMode.value = item.mode
  policyThreshold.value = Number(item.threshold_amount || 0)
  policyApproverRoleCode.value =
    item.approver_role_code || 'finance_reviewer'
  policyStatus.value =
    item.status === 'disabled' ? 'disabled' : 'active'
  error.value = ''
  notice.value = ''
}

async function savePolicy() {
  if (!isSuperAdmin.value || !editingPolicy.value || saving.value) return

  saving.value = true
  error.value = ''
  try {
    await updateStaffApprovalPolicy(editingPolicy.value.id, {
      mode: policyMode.value,
      threshold_amount: Math.max(0, Number(policyThreshold.value || 0)),
      approver_role_code: policyApproverRoleCode.value,
      status: policyStatus.value,
    })
    notice.value = '审批策略已更新。'
    editingPolicy.value = null
    await load()
  } catch (value) {
    error.value =
      value instanceof Error ? value.message : '修改审批策略失败'
  } finally {
    saving.value = false
  }
}
function closeCredential() {
  credentialOpen.value = false
  credential.value = null
  credentialName.value = ''
  credentialUsername.value = ''
}

onMounted(load)
</script>

<template>
  <div class="management-page staff-page">
    <ModulePageNav
      hub="staff"
      :active-title="props.groupsOnly ? '部门' : props.initialTab === 'roles' ? '角色权限' : props.initialTab === 'approvals' ? '审批策略' : '员工账号'"
    />
    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">INTERNAL ORGANIZATION</p>
        <h2>组织架构</h2>
        <p>
          主部门只负责人事归属；同一员工可跨多个部门兼任不同岗位。权限按全部有效岗位合并，并保留各岗位所属部门的数据范围。
        </p>
      </div>
      <button class="ghost-button" type="button" :disabled="loading" @click="load">
        {{ loading ? '刷新中...' : '刷新数据' }}
      </button>
    </section>

    <p v-if="error" class="auth-error">{{ error }}</p>
    <p v-if="notice" class="settings-success">{{ notice }}</p>

    <template v-if="dashboard">

      <section class="settings-card staff-groups-card">
        <div class="settings-card-header">
          <div>
            <span class="section-kicker">STAFF DEPARTMENTS</span>
            <h3>部门</h3>
          </div>
          <button
            v-if="isSuperAdmin"
            class="primary-button compact-button"
            type="button"
            @click="openGroupCreate"
          >
            新增部门
          </button>
        </div>

        <div class="staff-group-list">
          <button
            v-for="group in groups"
            :key="group.id"
            type="button"
            class="staff-group-card"
            :class="{ active: selectedGroup?.id === group.id }"
            @click="setSelectedGroup(group.id)"
          >
            <div>
              <div class="staff-group-code-row">
                <span class="staff-group-code">{{ group.code }}</span>
                <span v-if="group.system_managed" class="staff-group-system-badge">系统内置</span>
              </div>
              <strong>{{ group.name }}</strong>
              <small>{{ group.description }}</small>
            </div>
            <div class="staff-group-counts">
              <span>{{ group.member_count }} 员工</span>
              <span>{{ group.manager_count }} 负责人</span>
            </div>
          </button>
        </div>
      </section>

      <section v-if="selectedGroup" class="settings-card staff-detail-card">
        <div class="staff-detail-header">
          <div>
            <div class="staff-detail-kicker-row">
              <span class="section-kicker">{{ selectedGroup.code }}</span>
              <span v-if="selectedGroup.system_managed" class="staff-group-system-badge">系统内置 · 锁定</span>
            </div>
            <h3>{{ selectedGroup.name }}</h3>
            <p>{{ selectedGroup.description }}</p>
          </div>
          <div class="staff-detail-actions">
            <button
              v-if="isSuperAdmin && !selectedGroup.system_managed"
              class="ghost-button compact-button"
              type="button"
              @click="openGroupEdit(selectedGroup)"
            >
              编辑组
            </button>
            <button
              v-if="canCreateEmployee"
              class="primary-button compact-button"
              type="button"
              @click="openEmployeeCreate"
            >
              添加员工
            </button>
          </div>
        </div>

        <div v-if="!props.groupsOnly" class="staff-tabs">
          <button
            type="button"
            :class="{ active: activeTab === 'employees' }"
            @click="activeTab = 'employees'"
          >
            员工
          </button>
          <button
            type="button"
            :class="{ active: activeTab === 'roles' }"
            @click="activeTab = 'roles'"
          >
            角色与权限
          </button>
          <button
            v-if="selectedGroup.code === 'finance' && policies.length"
            type="button"
            :class="{ active: activeTab === 'approvals' }"
            @click="activeTab = 'approvals'"
          >
            审批规则
          </button>
        </div>

        <div v-if="!props.groupsOnly && activeTab === 'employees'" class="staff-tab-body">
          <div v-if="selectedEmployees.length === 0" class="empty-state">
            当前部门还没有可见员工。
          </div>

          <div v-else class="staff-employee-list">
            <article
              v-for="item in selectedEmployees"
              :key="item.id"
              class="staff-employee-row"
            >
              <div class="staff-employee-main">
                <span class="staff-employee-avatar">
                  {{ item.display_name.slice(0, 1) }}
                </span>
                <div>
                  <strong>{{ item.display_name }}</strong>
                  <span>{{ item.employee_no }} · @{{ item.username }}</span>
                  <small>主部门：{{ item.primary_group_name }}<template v-if="item.groups?.length > 1"> · 兼任 {{ item.groups.length - 1 }} 个部门</template></small>
                </div>
              </div>

              <div class="staff-employee-contact">
                <span class="muted-label">联系方式</span>
                <strong>{{ item.phone }}</strong>
                <span>{{ item.email || '未填写邮箱' }}</span>
              </div>

              <div class="staff-role-badges">
                <span
                  v-for="role in item.roles"
                  :key="role.role_id"
                  class="staff-role-badge"
                  :class="{ manager: role.is_group_manager }"
                >
                  {{ role.group_name }} · {{ role.name }}<template v-if="role.group_id !== item.primary_group_id"> · 兼任</template>
                </span>
              </div>

              <div class="staff-row-actions">
                <span
                  class="status-pill"
                  :class="{ inactive: item.employment_status !== 'active' }"
                >
                  {{ statusLabel(item.employment_status) }}
                </span>
                <button
                  v-if="canAssignEmployeeRoles && item.employment_status === 'active'"
                  class="text-action"
                  type="button"
                  @click="openEmployeeRoleEdit(item)"
                >
                  职责
                </button>
                <button
                  v-if="isSuperAdmin && item.employment_status === 'active'"
                  class="text-action"
                  type="button"
                  @click="resetEmployeePassword(item)"
                >
                  重置密码
                </button>
                <button
                  v-if="canDisableEmployeeAccount(item) && item.employment_status === 'active'"
                  class="text-action danger"
                  type="button"
                  @click="disableEmployee(item)"
                >
                  停用
                </button>
              </div>
            </article>
          </div>
        </div>

        <div v-if="!props.groupsOnly && activeTab === 'roles'" class="staff-tab-body">
          <div class="staff-role-header">
            <div>
              <strong>当前组角色</strong>
              <span>角色定义权限，员工只分配已有角色。</span>
            </div>
            <button
              v-if="isSuperAdmin"
              class="primary-button compact-button"
              type="button"
              @click="openRoleCreate"
            >
              新建角色
            </button>
          </div>

          <div class="staff-role-grid">
            <article
              v-for="role in selectedRoles"
              :key="role.id"
              class="staff-role-card"
            >
              <div class="staff-role-card-head">
                <div>
                  <span>{{ role.code }}</span>
                  <strong>{{ role.name }}</strong>
                </div>
                <span v-if="role.is_group_manager" class="staff-manager-tag">
                  部门负责人
                </span>
              </div>
              <p>{{ role.description }}</p>
              <div class="staff-role-meta">
                <span>数据范围：{{ scopeLabel(role.default_scope_type) }}</span>
                <span>{{ role.permission_codes.length }} 项权限</span>
              </div>
              <div class="staff-permission-chip-list">
                <span
                  v-for="permission in role.permissions.slice(0, 8)"
                  :key="permission.id"
                >
                  {{ permission.description }}
                </span>
                <span v-if="role.permissions.length > 8">
                  +{{ role.permissions.length - 8 }}
                </span>
              </div>
              <button
                v-if="isSuperAdmin"
                class="text-action"
                type="button"
                @click="openRoleEdit(role)"
              >
                编辑角色权限
              </button>
            </article>
          </div>
        </div>

        <div v-if="!props.groupsOnly && activeTab === 'approvals'" class="staff-tab-body">
          <div class="staff-role-header">
            <div>
              <strong>财务审批策略</strong>
              <span>充值、退款、奖励发放支持始终审核、金额阈值审核和直接执行。操作人与审核人必须分离。</span>
            </div>
          </div>

          <div class="approval-policy-list">
            <article
              v-for="policy in policies"
              :key="policy.id"
              class="approval-policy-row"
            >
              <div>
                <strong>{{ policy.name }}</strong>
                <span>{{ policy.operation_code }}</span>
              </div>

              <div>
                <span class="muted-label">执行模式</span>
                <strong>
                  {{
                    policy.mode === 'manual'
                      ? '始终审核'
                      : policy.mode === 'threshold'
                        ? '超额审核'
                        : '直接执行'
                  }}
                </strong>
                <span v-if="policy.mode === 'threshold'">
                  超过 ¥{{ Number(policy.threshold_amount || 0).toFixed(2) }}
                </span>
              </div>

              <div>
                <span class="muted-label">审核角色</span>
                <strong>{{ policy.approver_role_code || '未指定' }}</strong>
              </div>

              <div class="staff-row-actions">
                <span
                  class="status-pill"
                  :class="{ inactive: policy.status !== 'active' }"
                >
                  {{ statusLabel(policy.status) }}
                </span>
                <button
                  v-if="isSuperAdmin"
                  class="text-action"
                  type="button"
                  @click="openPolicyEdit(policy)"
                >
                  配置
                </button>
              </div>
            </article>
          </div>
        </div>
      </section>
    </template>

    <div
      v-if="showEmployeeModal && selectedGroup"
      class="modal-backdrop"
      @click.self="showEmployeeModal = false"
    >
      <form class="modal-card staff-employee-modal" @submit.prevent="submitEmployee">
        <div class="modal-header">
          <div>
            <p class="section-kicker">NEW EMPLOYEE</p>
            <h3>添加员工 · 主部门：{{ selectedGroup.name }}</h3>
          </div>
          <button class="icon-button" type="button" @click="showEmployeeModal = false">
            ×
          </button>
        </div>

        <div class="staff-employee-modal-body">
          <section class="staff-employee-basic-panel">
            <div class="staff-employee-section-head">
              <div>
                <strong>员工资料</strong>
                <small>账号与基础信息</small>
              </div>
            </div>

            <div class="form-grid staff-employee-form-grid">
              <label :class="{ 'field-error': employeeNoDuplicate }">
                <span>员工编号</span>
                <div class="employee-number-input">
                  <b>EMP-</b>
                  <input
                    :value="employeeNo"
                    inputmode="numeric"
                    maxlength="6"
                    required
                    placeholder="例如：32"
                    @input="employeeNo = ($event.target as HTMLInputElement).value.replace(/\D/g, '').slice(0, 6)"
                  />
                </div>
                <small v-if="employeeFullNo && !employeeNoDuplicate">系统编号：{{ employeeFullNo }}</small>
                <small v-if="employeeNoDuplicate" class="field-error-text">{{ employeeFullNo }} 已存在，请更换</small>
              </label>
              <label>
                <span>员工姓名</span>
                <input v-model="employeeName" required placeholder="员工姓名" />
              </label>
              <label>
                <span>登录账号</span>
                <input v-model="employeeUsername" required placeholder="登录账号" />
              </label>
              <label>
                <span>联系电话</span>
                <input v-model="employeePhone" required placeholder="联系电话" />
              </label>
              <label class="form-span-2">
                <span>邮箱</span>
                <input
                  v-model="employeeEmail"
                  type="email"
                  placeholder="选择邮件交付初始密码时必填"
                />
              </label>

              <RegionSelect
                class="form-span-2 staff-employee-region-select"
                v-model:province="employeeProvince"
                v-model:city="employeeCity"
                v-model:district="employeeDistrict"
              />

              <label class="form-span-2">
                <span>初始凭证交付</span>
                <select v-model="employeeDelivery" class="text-input">
                  <option value="copy">创建后复制登录信息</option>
                  <option value="email">发送到邮箱</option>
                </select>
              </label>
            </div>

            <div class="account-opening-note staff-employee-security-note">
              <strong>账号安全</strong>
              <span>初始密码由系统随机生成，数据库只保存哈希。员工首次登录后必须修改密码。</span>
            </div>
          </section>

          <section class="staff-employee-role-panel">
            <div class="staff-employee-section-head">
              <div>
                <strong>部门岗位</strong>
                <small>树型多选 · 支持跨部门兼任</small>
              </div>
              <span>{{ employeeRoleIds.length }} 个已选</span>
            </div>

            <div v-if="selectedEmployeeCreateRoles.length" class="staff-role-selected-chips">
              <button
                v-for="role in selectedEmployeeCreateRoles"
                :key="role.id"
                type="button"
                :class="{ primary: role.group_id === selectedGroup.id, manager: role.is_group_manager }"
                @click="toggleEmployeeRole(role)"
              >
                <span>{{ role.group_name }} · {{ role.name }}</span>
                <b>×</b>
              </button>
            </div>

            <div class="staff-role-tree-search">
              <span>⌕</span>
              <input v-model="employeeRoleSearch" type="search" placeholder="搜索部门或岗位" />
            </div>

            <div class="staff-role-tree" role="tree" aria-label="部门岗位选择">
              <article v-for="entry in employeeRoleTree" :key="entry.group.id" class="staff-role-tree-group">
                <button
                  class="staff-role-tree-group-button"
                  type="button"
                  :class="{ primary: entry.group.id === selectedGroup.id }"
                  @click="toggleEmployeeRoleGroup(entry.group.id)"
                >
                  <i>{{ employeeRoleGroupExpanded(entry.group.id) ? '▾' : '▸' }}</i>
                  <span>
                    <strong>{{ entry.group.name }}</strong>
                    <small v-if="entry.group.id === selectedGroup.id">主部门</small>
                  </span>
                  <b>{{ employeeRoleGroupSelectedCount(entry.group.id) }}/{{ entry.total }}</b>
                </button>

                <div v-if="employeeRoleGroupExpanded(entry.group.id)" class="staff-role-tree-children">
                  <label
                    v-for="role in entry.items"
                    :key="role.id"
                    class="staff-role-tree-role"
                    :class="{
                      selected: employeeRoleIds.includes(role.id),
                      manager: role.is_group_manager,
                    }"
                  >
                    <input
                      type="checkbox"
                      :checked="employeeRoleIds.includes(role.id)"
                      @change="toggleEmployeeRole(role)"
                    />
                    <span class="staff-role-tree-role-copy">
                      <strong>{{ role.name }}</strong>
                      <small>{{ scopeLabel(role.default_scope_type) }}<template v-if="role.is_group_manager"> · 高权限负责人</template></small>
                    </span>
                    <em v-if="role.group_id === selectedGroup.id">主</em>
                    <em v-else>兼</em>
                  </label>
                </div>
              </article>
              <div v-if="!employeeRoleTree.length" class="staff-role-tree-empty">没有匹配的部门岗位</div>
            </div>

            <small class="staff-multi-role-note">
              主部门至少保留一个岗位；其他部门岗位表示兼任，仍使用同一个登录账号。
            </small>
          </section>
        </div>

        <div class="modal-actions staff-employee-modal-actions">
          <button class="ghost-button" type="button" @click="showEmployeeModal = false">取消</button>
          <button class="primary-button" type="submit" :disabled="saving">
            {{ saving ? '创建中...' : '创建员工' }}
          </button>
        </div>
      </form>
    </div>

    <div
      v-if="showGroupModal"
      class="modal-backdrop"
      @click.self="showGroupModal = false"
    >
      <form class="modal-card" @submit.prevent="submitGroup">
        <div class="modal-header">
          <div>
            <p class="section-kicker">STAFF DEPARTMENT</p>
            <h3>{{ editingGroup ? '编辑自定义部门' : '新增部门' }}</h3>
          </div>
          <button class="icon-button" type="button" @click="showGroupModal = false">
            ×
          </button>
        </div>

        <div class="form-grid">
          <label>
            <span>部门编码</span>
            <input
              v-model="groupCode"
              :disabled="Boolean(editingGroup)"
              required
              placeholder="例如：operations"
            />
          </label>
          <label>
            <span>显示名称</span>
            <input v-model="groupName" required placeholder="例如：运营组" />
          </label>
          <label class="form-span-2">
            <span>说明</span>
            <textarea v-model="groupDescription" rows="3"></textarea>
          </label>
          <label v-if="editingGroup" class="form-span-2">
            <span>状态</span>
            <select v-model="groupStatus" class="text-input">
              <option value="active">正常</option>
              <option value="disabled">停用</option>
            </select>
          </label>
        </div>

        <div class="account-opening-note">
          <strong>自定义部门编码创建后不可修改</strong>
          <span>
            管理部、财务部、销售部、仓储售后部由系统自动建立并锁定；这里仅用于新增特殊部门。
          </span>
        </div>

        <div class="modal-actions">
          <button class="ghost-button" type="button" @click="showGroupModal = false">
            取消
          </button>
          <button class="primary-button" type="submit" :disabled="saving">
            保存
          </button>
        </div>
      </form>
    </div>

    <div
      v-if="showRoleModal && selectedGroup"
      class="modal-backdrop"
      @click.self="showRoleModal = false"
    >
      <form class="modal-card staff-role-modal" @submit.prevent="submitRole">
        <div class="modal-header">
          <div>
            <p class="section-kicker">ROLE & PERMISSIONS</p>
            <h3>{{ editingRole ? '编辑角色' : '新建角色' }}</h3>
          </div>
          <button class="icon-button" type="button" @click="showRoleModal = false">
            ×
          </button>
        </div>

        <div class="staff-role-modal-body">
          <div class="form-grid staff-role-form-grid">
          <label>
            <span>角色编码</span>
            <input
              v-model="roleCode"
              :disabled="Boolean(editingRole)"
              required
              placeholder="例如：operations_manager"
            />
          </label>
          <label>
            <span>角色名称</span>
            <select
              v-if="editingRole"
              v-model.number="roleEditorRoleId"
              class="text-input role-name-select"
              required
              @change="changeRoleEditorSelection"
            >
              <option
                v-for="role in roleEditorOptions"
                :key="role.id"
                :value="role.id"
              >
                {{ role.name }}
              </option>
            </select>
            <input v-else v-model="roleName" required placeholder="请输入角色名称" />
          </label>
          <label>
            <span>默认数据范围</span>
            <select v-model="roleScope" class="text-input">
              <option value="self">本人</option>
              <option value="assigned">已分配业务</option>
              <option value="group">本部门</option>
              <option value="managed_groups">负责的部门</option>
              <option value="all_internal">全部内部员工</option>
              <option value="all">全系统</option>
            </select>
          </label>
          <label>
            <span>角色类型</span>
            <select v-model="roleIsManager" class="text-input">
              <option :value="false">普通角色</option>
              <option :value="true">部门负责人角色</option>
            </select>
          </label>
          <label class="form-span-2">
            <span>角色说明</span>
            <textarea v-model="roleDescription" rows="3" placeholder="说明该角色负责的业务范围与职责边界"></textarea>
          </label>
          </div>

          <div class="staff-permission-editor-head">
            <div>
              <span>权限配置</span>
              <small>按业务模块分组，可精确控制该角色可访问和可操作的范围。</small>
            </div>
            <strong>{{ rolePermissionIds.length }} 项已选</strong>
          </div>

          <div class="staff-permission-editor">
          <div
            v-for="section in permissionModules"
            :key="section.module"
            class="staff-permission-module"
          >
            <div class="staff-permission-module-head">
              <div>
                <strong>{{ moduleLabel(section.module) }}</strong>
                <small>{{ section.module }}</small>
              </div>
              <span>{{ permissionModuleSelectedCount(section.items) }}/{{ section.items.length }}</span>
            </div>
            <div class="staff-permission-grid">
              <label
                v-for="permission in section.items"
                :key="permission.id"
                class="staff-permission-option"
                :class="{ selected: rolePermissionIds.includes(permission.id) }"
              >
                <input
                  type="checkbox"
                  :checked="rolePermissionIds.includes(permission.id)"
                  @change="togglePermission(permission.id)"
                />
                <span>
                  <b>{{ permission.description }}</b>
                  <small>{{ permission.code }}</small>
                </span>
              </label>
            </div>
          </div>
        </div>
        </div>

        <div class="modal-actions staff-role-modal-actions">
          <button class="ghost-button staff-role-action-button" type="button" @click="showRoleModal = false">
            取消
          </button>
          <button class="primary-button staff-role-action-button" type="submit" :disabled="saving">
            {{ saving ? '保存中...' : '保存角色' }}
          </button>
        </div>
      </form>
    </div>

    <div
      v-if="showEmployeeRolesModal && editingEmployee"
      class="modal-backdrop"
      @click.self="showEmployeeRolesModal = false"
    >
      <form class="modal-card" @submit.prevent="submitEmployeeRoles">
        <div class="modal-header">
          <div>
            <p class="section-kicker">EMPLOYEE RESPONSIBILITIES</p>
            <h3>调整部门职责 · {{ editingEmployee.display_name }}</h3>
          </div>
          <button
            class="icon-button"
            type="button"
            @click="showEmployeeRolesModal = false"
          >
            ×
          </button>
        </div>

        <div class="staff-role-selector">
          <label
            v-for="role in editableEmployeeRoles"
            :key="role.id"
            class="staff-role-option"
            :class="{ selected: employeeEditRoleIds.includes(role.id), manager: role.is_group_manager }"
          >
            <input
              type="checkbox"
              :checked="employeeEditRoleIds.includes(role.id)"
              :disabled="!canEditEmployeeRole(role)"
              @change="toggleEmployeeRole(role, true)"
            />
            <div>
              <strong>{{ role.group_name }} · {{ role.name }}</strong>
              <span>{{ role.group_id === editingEmployee.primary_group_id ? '主部门岗位' : '兼任岗位' }} · {{ scopeLabel(role.default_scope_type) }}<template v-if="role.is_group_manager"> · 高权限负责人</template><template v-if="!canEditEmployeeRole(role)"> · 其他部门锁定</template></span>
            </div>
          </label>
        </div>

        <div class="modal-actions">
          <button
            class="ghost-button"
            type="button"
            @click="showEmployeeRolesModal = false"
          >
            取消
          </button>
          <button class="primary-button" type="submit" :disabled="saving">
            保存部门职责
          </button>
        </div>
      </form>
    </div>

    <div
      v-if="editingPolicy"
      class="modal-backdrop"
      @click.self="editingPolicy = null"
    >
      <form class="modal-card staff-policy-modal" @submit.prevent="savePolicy">
        <div class="modal-header">
          <div>
            <p class="section-kicker">APPROVAL POLICY</p>
            <h3>配置审批规则 · {{ editingPolicy.name }}</h3>
          </div>
          <button
            class="icon-button"
            type="button"
            @click="editingPolicy = null"
          >
            ×
          </button>
        </div>

        <div class="form-grid">
          <label>
            <span>执行模式</span>
            <select v-model="policyMode" class="text-input">
              <option value="manual">始终进入审核</option>
              <option value="threshold">超过金额阈值后审核</option>
              <option value="direct">直接执行</option>
            </select>
          </label>

          <label>
            <span>状态</span>
            <select v-model="policyStatus" class="text-input">
              <option value="active">启用</option>
              <option value="disabled">停用</option>
            </select>
          </label>

          <label v-if="policyMode === 'threshold'">
            <span>审核阈值（元）</span>
            <input
              v-model.number="policyThreshold"
              type="number"
              min="0"
              step="0.01"
              required
              placeholder="0.00"
            />
          </label>

          <label>
            <span>审核角色</span>
            <select v-model="policyApproverRoleCode" class="text-input">
              <option
                v-for="role in financeApprovalRoles"
                :key="role.id"
                :value="role.code"
              >
                {{ role.name }}
              </option>
            </select>
          </label>
        </div>

        <div class="account-opening-note">
          <strong>职责分离</strong>
          <span>
            进入审核的财务单据不允许由发起人本人审核。停用审批策略后，对应业务按无审核策略执行。
          </span>
        </div>

        <div class="modal-actions">
          <button
            class="ghost-button"
            type="button"
            @click="editingPolicy = null"
          >
            取消
          </button>
          <button class="primary-button" type="submit" :disabled="saving">
            {{ saving ? '保存中...' : '保存审批规则' }}
          </button>
        </div>
      </form>
    </div>
    <CredentialResultModal
      :open="credentialOpen"
      :title="credentialTitle"
      :display-name="credentialName"
      :username="credentialUsername"
      :credential="credential"
      @close="closeCredential"
    />
  </div>
</template>