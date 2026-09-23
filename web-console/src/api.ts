import { showPermissionToast } from './uiFeedback'
import type {
  AccountDashboard,
  AccountProfile,
  AuthSessionSummary,
  AdminAuditLog,
  AdminCustomer,
  AgentSummary,
  Bootstrap,
  CommercialMembershipInput,
  CommercialMembershipPlan,
  CommercialTimeCardProduct,
  CommercialTimeCardInput,
  CustomerTimeCardOffer,
  CommercialDeviceProduct,
  CommercialDeviceInput,
  CustomerDeviceOffer,
  CustomerShopOrder,
  CreateCustomerShopOrderInput,
  SandboxPayOrderInput,
  SandboxRefundOrderInput,
  RefundRecord,
  FeatureRecord,
  FeatureRecordInput,
  InventoryWarehouse,
  InventoryDeviceProduct,
  InventoryBatchInboundInput,
  InventoryBatchInboundResult,
  InventoryDevice,
  InventoryDeviceLedgerEntry,
  InventoryStockDocument,
  InventorySummary,
  InventoryRMA,
  InventoryRMAPage,
  InventoryCreateDeviceInput,
  InventoryDeviceTransitionInput,
  InventoryCreateRMAInput,
  InventoryCompleteRMAInput,
  RMAEvent,
  RMACost,
  CreateRMACostInput,
  AfterSalesRequestPage,
  AfterSalesRequestInput,
  LogisticsShipment,
  CreateLogisticsShipmentInput,
  UpdateLogisticsShipmentStatusInput,
  ScrapDisposalInput,
  ScrapDisposal,
  OperatingFinanceOverview,
  TokenPurchase,
  TokenPurchaseInput,
  SalesPerformanceResponse,
  IncentiveProgram,
  IncentiveProgramInput,
  SettlementDashboard,
  SettlementBatch,
  CreateSettlementBatchInput,
  AgentExitCheck,
  AgentExitRecord,
  AgentLevel,
  AgentLevelHistory,
  AgentContract,
  CreateRoomPayload,
  FinanceDashboard,
  Room,
  RoomEvent,
  InvitationDashboard,
  InvitePreview,
  InitialCredential,
  ResourceDashboard,
  SalesStaffSummary,
  StaffRoleSummary,
  StaffGroupSummary,
  StaffEmployeeSummary,
  StaffDashboard,
  StaffFinanceOverview,
  StaffFinanceOperationResult,
} from './types'

interface ListResponse<T> {
  items: T[]
}

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers)
  if (init?.body !== undefined && !(init.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  const response = await fetch(url, {
    credentials: 'include',
    ...init,
    headers,
  })

  if (!response.ok) {
    let message = '请求失败'
    try {
      const body = await response.json() as { error?: string }
      if (body.error) message = body.error
    } catch {
      // Keep the fallback message.
    }
    if (response.status === 403) {
      showPermissionToast(message)
    }
    throw new Error(message)
  }

  if (response.status === 204) {
    return undefined as T
  }

  return response.json() as Promise<T>
}

export function getBootstrap() {
  return request<Bootstrap>('/api/v1/bootstrap')
}

export function getRooms(tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<ListResponse<Room>>('/api/v1/rooms' + query)
}

export function createRoom(payload: CreateRoomPayload) {
  return request<Room>('/api/v1/rooms', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function deleteRoom(roomId: number) {
  return request<void>('/api/v1/rooms/' + roomId, {
    method: 'DELETE',
  })
}

export function getRoom(roomId: number) {
  return request<Room>('/api/v1/rooms/' + roomId)
}

export function getRoomEvents(roomId: number, limit = 200) {
  return request<ListResponse<RoomEvent>>(
    '/api/v1/rooms/' + roomId + '/events?limit=' + limit,
  )
}

export function login(payload: {
  username: string
  password: string
  captcha: string
}) {
  return request<{ actor: import('./types').Actor }>('/api/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function register(payload: {
  username: string
  display_name: string
  phone: string
  password: string
  confirm_password: string
  invite_code: string
  captcha: string
}) {
  return request<{ actor: import('./types').Actor }>('/api/v1/auth/register', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function logout() {
  return request<void>('/api/v1/auth/logout', {
    method: 'POST',
  })
}

export function changePassword(payload: {
  current_password: string
  new_password: string
  confirm_password: string
}) {
  return request<{ ok: boolean }>('/api/v1/auth/change-password', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getAuthSessions() {
  return request<ListResponse<AuthSessionSummary>>('/api/v1/auth/sessions')
}

export function logoutOtherAuthSessions() {
  return request<void>('/api/v1/auth/sessions/logout-others', {
    method: 'POST',
  })
}
export function getAccountDashboard() {
  return request<AccountDashboard>('/api/v1/account')
}

export function updateAccountProfile(payload: {
  display_name: string
  phone: string
  email: string
  qq: string
  wechat: string
  province: string
  city: string
  district: string
  address: string
}) {
  return request<AccountProfile>('/api/v1/account/profile', {
    method: 'PATCH',
    body: JSON.stringify(payload),
  })
}

export function uploadAccountAvatar(file: File) {
  const body = new FormData()
  body.append('avatar', file)
  return request<AccountProfile>('/api/v1/account/avatar', {
    method: 'POST',
    body,
  })
}

export function getFinanceDashboard(limit = 50) {
  return request<FinanceDashboard>(
    '/api/v1/finance/dashboard?limit=' + encodeURIComponent(String(limit)),
  )
}

export function createCustomerRechargeRequest(payload: {
  amount_cents: number
  reason: string
}) {
  return request<StaffFinanceOperationResult>('/api/v1/finance/recharge-request', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getAdminCustomers() {
  return request<ListResponse<AdminCustomer>>('/api/v1/admin/customers')
}

export function adminResetCustomerPassword(
  userId: number,
  payload: {
    delivery_method: 'copy' | 'email'
    email?: string
  },
) {
  return request<{
    ok: boolean
    customer: AdminCustomer
    credential: InitialCredential
  }>(
    '/api/v1/admin/customers/' + userId + '/reset-password',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}
export function adminDeleteCustomer(userId: number) {
  return request<void>('/api/v1/admin/customers/' + userId, {
    method: 'DELETE',
  })
}

export function getAdminAuditLogs(limit = 50) {
  return request<ListResponse<AdminAuditLog>>(
    '/api/v1/admin/audit-logs?limit=' + encodeURIComponent(String(limit)),
  )
}
export function getCommercialMemberships() {
  return request<ListResponse<CommercialMembershipPlan>>(
    '/api/v1/commercial/memberships',
  )
}

export function createCommercialMembership(payload: CommercialMembershipInput) {
  return request<CommercialMembershipPlan>('/api/v1/commercial/memberships', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function saveCommercialMembershipDraft(
  planId: number,
  payload: CommercialMembershipInput,
) {
  return request<CommercialMembershipPlan>(
    '/api/v1/commercial/memberships/' + planId + '/draft',
    {
      method: 'PUT',
      body: JSON.stringify(payload),
    },
  )
}

export function publishCommercialMembership(planId: number) {
  return request<CommercialMembershipPlan>(
    '/api/v1/commercial/memberships/' + planId + '/publish',
    {
      method: 'POST',
    },
  )
}
export function getAdminAgents() {
  return request<ListResponse<AgentSummary>>('/api/v1/admin/agents')
}

export function createAdminAgent(payload: {
  code: string
  name: string
  username: string
  display_name: string
  phone: string
  email: string
  province: string
  city: string
  district: string
  delivery_method: 'copy' | 'email'
}) {
  return request<{
    item: AgentSummary
    credential: InitialCredential
  }>('/api/v1/admin/agents', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}
export function getAgentCustomers() {
  return request<ListResponse<AdminCustomer>>('/api/v1/agent/customers')
}

export function createAgentCustomer(payload: {
  username: string
  display_name: string
  phone: string
  email: string
  province: string
  city: string
  district: string
  delivery_method: 'copy' | 'email'
}) {
  return request<{
    user_id: number
    tenant_id?: number
    username: string
    display_name: string
    email: string
    status: string
    credential: InitialCredential
  }>('/api/v1/agent/customers', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}
export function getInvitePreview(code: string) {
  return request<InvitePreview>(
    '/api/v1/auth/invite/' + encodeURIComponent(code.trim()),
  )
}

export function getInvitationDashboard() {
  return request<InvitationDashboard>('/api/v1/invitations/dashboard')
}

export function updateOwnInviteCodeStatus(status: 'active' | 'disabled') {
  return request<void>('/api/v1/invitations/mine', {
    method: 'PATCH',
    body: JSON.stringify({ status }),
  })
}

export function updateAdminInviteCodePolicy(
  codeId: number,
  payload: {
    status: 'active' | 'disabled'
    max_uses?: number
    expires_at?: string
  },
) {
  return request<void>('/api/v1/admin/invitations/' + codeId, {
    method: 'PATCH',
    body: JSON.stringify(payload),
  })
}
export function updateAdminInviteCodeStatus(
  codeId: number,
  status: 'active' | 'disabled',
) {
  return request<void>('/api/v1/admin/invitations/' + codeId, {
    method: 'PATCH',
    body: JSON.stringify({ status }),
  })
}
export function getCurrentResources() {
  return request<ResourceDashboard>('/api/v1/resources')
}

export function getAdminAgentResources(orgId: number) {
  return request<ResourceDashboard>(
    '/api/v1/admin/agents/' + orgId + '/resources',
  )
}

export function adjustAdminAgentResource(
  orgId: number,
  payload: {
    resource_type: string
    delta: number
    reason: string
  },
) {
  return request<void>(
    '/api/v1/admin/agents/' + orgId + '/resources/adjust',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function getAgentCustomerResources(tenantId: number) {
  return request<ResourceDashboard>(
    '/api/v1/agent/customers/' + tenantId + '/resources',
  )
}

export function allocateAgentCustomerResource(
  tenantId: number,
  payload: {
    resource_type: string
    quantity: number
    reason: string
  },
) {
  return request<void>(
    '/api/v1/agent/customers/' + tenantId + '/resources/allocate',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}
export function getAdminCustomerResources(tenantId: number) {
  return request<ResourceDashboard>(
    '/api/v1/admin/customers/' + tenantId + '/resources',
  )
}

export function adjustAdminCustomerResource(
  tenantId: number,
  payload: {
    resource_type: string
    delta: number
    reason: string
  },
) {
  return request<void>(
    '/api/v1/admin/customers/' + tenantId + '/resources/adjust',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}
export function getAdminSalesStaff() {
  return request<ListResponse<SalesStaffSummary>>('/api/v1/admin/sales')
}

export function createAdminSalesStaff(payload: {
  employee_code: string
  username: string
  display_name: string
  phone: string
  email: string
  province: string
  city: string
  district: string
  delivery_method: 'copy' | 'email'
}) {
  return request<{
    item: SalesStaffSummary
    credential: InitialCredential
  }>('/api/v1/admin/sales', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}
export function assignAdminCustomerSales(
  tenantId: number,
  payload: {
    sales_staff_id: number
    reason: string
  },
) {
  return request<void>(
    '/api/v1/admin/customers/' + tenantId + '/sales-assignment',
    {
      method: 'PUT',
      body: JSON.stringify(payload),
    },
  )
}

export function getSalesCustomers() {
  return request<ListResponse<AdminCustomer>>('/api/v1/sales/customers')
}

export function getStaffDashboard() {
  return request<StaffDashboard>('/api/v1/staff/dashboard')
}

export function createStaffGroup(payload: {
  code: string
  name: string
  description: string
}) {
  return request<StaffGroupSummary>('/api/v1/staff/groups', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateStaffGroup(
  groupId: number,
  payload: {
    name: string
    description: string
    status: 'active' | 'disabled'
  },
) {
  return request<void>('/api/v1/staff/groups/' + groupId, {
    method: 'PATCH',
    body: JSON.stringify(payload),
  })
}

export function createStaffRole(payload: {
  group_id: number
  code: string
  name: string
  description: string
  is_group_manager: boolean
  default_scope_type: string
  permission_ids: number[]
}) {
  return request<StaffRoleSummary>('/api/v1/staff/roles', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateStaffRole(
  roleId: number,
  payload: {
    name: string
    description: string
    is_group_manager: boolean
    default_scope_type: string
    status: 'active' | 'disabled'
    permission_ids: number[]
  },
) {
  return request<void>('/api/v1/staff/roles/' + roleId, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function createStaffEmployee(payload: {
  employee_no: string
  primary_group_id: number
  role_ids: number[]
  username: string
  display_name: string
  phone: string
  email: string
  province: string
  city: string
  district: string
  delivery_method: 'copy' | 'email'
}) {
  return request<{
    item: StaffEmployeeSummary
    credential: InitialCredential
  }>('/api/v1/staff/employees', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function disableStaffEmployee(employeeId: number) {
  return request<void>('/api/v1/staff/employees/' + employeeId + '/disable', {
    method: 'POST',
  })
}

export function replaceStaffEmployeeRoles(
  employeeId: number,
  roleIds: number[],
) {
  return request<void>('/api/v1/staff/employees/' + employeeId + '/roles', {
    method: 'PUT',
    body: JSON.stringify({ role_ids: roleIds }),
  })
}

export function updateStaffApprovalPolicy(
  policyId: number,
  payload: {
    mode: string
    threshold_amount: number
    approver_role_code: string
    status: 'active' | 'disabled'
  },
) {
  return request<void>('/api/v1/staff/approval-policies/' + policyId, {
    method: 'PATCH',
    body: JSON.stringify(payload),
  })
}
export function getStaffFinanceOverview() {
  return request<StaffFinanceOverview>('/api/v1/staff/finance')
}

export function getStaffFinanceCustomer(tenantId: number) {
  return request<FinanceDashboard>(
    '/api/v1/staff/finance/customers/' + tenantId,
  )
}

export function createStaffFinanceRecharge(payload: {
  tenant_id: number
  amount_cents: number
  payment_method: string
  reason: string
}) {
  return request<StaffFinanceOperationResult>(
    '/api/v1/staff/finance/recharge',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function createStaffFinanceRefund(payload: {
  tenant_id: number
  amount_cents: number
  reason: string
}) {
  return request<StaffFinanceOperationResult>(
    '/api/v1/staff/finance/refund',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function createStaffFinanceReward(payload: {
  tenant_id: number
  amount_cents: number
  reason: string
}) {
  return request<StaffFinanceOperationResult>(
    '/api/v1/staff/finance/reward',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function approveStaffFinanceTask(taskId: number) {
  return request<void>(
    '/api/v1/staff/finance/approvals/' + taskId + '/approve',
    { method: 'POST' },
  )
}

export function rejectStaffFinanceTask(taskId: number) {
  return request<void>(
    '/api/v1/staff/finance/approvals/' + taskId + '/reject',
    { method: 'POST' },
  )
}


export function getCommercialTimeCards() {
  return request<ListResponse<CommercialTimeCardProduct>>('/api/v1/commercial/time-cards')
}

export function createCommercialTimeCard(payload: CommercialTimeCardInput) {
  return request<CommercialTimeCardProduct>('/api/v1/commercial/time-cards', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function saveCommercialTimeCardDraft(productId: number, payload: CommercialTimeCardInput) {
  return request<CommercialTimeCardProduct>(
    '/api/v1/commercial/time-cards/' + productId + '/draft',
    {
      method: 'PUT',
      body: JSON.stringify(payload),
    },
  )
}

export function publishCommercialTimeCard(productId: number) {
  return request<CommercialTimeCardProduct>(
    '/api/v1/commercial/time-cards/' + productId + '/publish',
    {
      method: 'POST',
    },
  )
}

export function getCommercialDeviceProducts() {
  return request<ListResponse<CommercialDeviceProduct>>('/api/v1/commercial/device-products')
}

export function createCommercialDeviceProduct(payload: CommercialDeviceInput) {
  return request<CommercialDeviceProduct>('/api/v1/commercial/device-products', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function saveCommercialDeviceProductDraft(
  productId: number,
  payload: CommercialDeviceInput,
) {
  return request<CommercialDeviceProduct>(
    '/api/v1/commercial/device-products/' + productId + '/draft',
    {
      method: 'PUT',
      body: JSON.stringify(payload),
    },
  )
}

export function publishCommercialDeviceProduct(productId: number) {
  return request<CommercialDeviceProduct>(
    '/api/v1/commercial/device-products/' + productId + '/publish',
    { method: 'POST' },
  )
}

export function getCustomerDeviceOffers() {
  return request<ListResponse<CustomerDeviceOffer>>('/api/v1/shop/devices')
}

export function getCustomerTimeCardOffers() {
  return request<ListResponse<CustomerTimeCardOffer>>('/api/v1/shop/time-cards')
}

export function getCustomerShopOrders() {
  return request<ListResponse<CustomerShopOrder>>('/api/v1/shop/orders')
}

export function getCustomerShopOrder(orderId: number) {
  return request<CustomerShopOrder>('/api/v1/shop/orders/' + orderId)
}

export function createCustomerShopOrder(payload: CreateCustomerShopOrderInput) {
  return request<CustomerShopOrder>('/api/v1/shop/orders', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function sandboxPayCustomerShopOrder(orderId: number, payload: SandboxPayOrderInput) {
  return request<{ order: CustomerShopOrder; sandbox: boolean }>(
    '/api/v1/shop/orders/' + orderId + '/sandbox-pay',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function sandboxRefundCustomerShopOrder(orderId: number, payload: SandboxRefundOrderInput) {
  return request<{ order: CustomerShopOrder; refund: RefundRecord; sandbox: boolean }>(
    '/api/v1/shop/orders/' + orderId + '/sandbox-refund',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function cancelCustomerShopOrder(orderId: number) {
  return request<CustomerShopOrder>('/api/v1/shop/orders/' + orderId + '/cancel', {
    method: 'POST',
  })
}

export function getFeatureRecords(featureKey: string) {
  return request<ListResponse<FeatureRecord>>(
    '/api/v1/admin/features/' + encodeURIComponent(featureKey),
  )
}

export function createFeatureRecord(featureKey: string, payload: FeatureRecordInput) {
  return request<FeatureRecord>(
    '/api/v1/admin/features/' + encodeURIComponent(featureKey),
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function updateFeatureRecord(
  featureKey: string,
  recordId: number,
  payload: FeatureRecordInput,
) {
  return request<FeatureRecord>(
    '/api/v1/admin/features/' +
      encodeURIComponent(featureKey) +
      '/' +
      encodeURIComponent(String(recordId)),
    {
      method: 'PUT',
      body: JSON.stringify(payload),
    },
  )
}

export function deleteFeatureRecord(featureKey: string, recordId: number) {
  return request<void>(
    '/api/v1/admin/features/' +
      encodeURIComponent(featureKey) +
      '/' +
      encodeURIComponent(String(recordId)),
    {
      method: 'DELETE',
    },
  )
}


export function getInventoryWarehouses() {
  return request<ListResponse<InventoryWarehouse>>('/api/v1/inventory/warehouses')
}

export function getInventoryDeviceProducts() {
  return request<ListResponse<InventoryDeviceProduct>>('/api/v1/inventory/device-products')
}

export function getInventoryAgents() {
  return request<ListResponse<AgentSummary>>('/api/v1/inventory/agents')
}

export function createInventoryBatchInbound(payload: InventoryBatchInboundInput) {
  return request<InventoryBatchInboundResult>('/api/v1/inventory/inbounds', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getInventoryDevices() {
  return request<ListResponse<InventoryDevice>>('/api/v1/inventory/devices')
}

export function createInventoryDevice(payload: InventoryCreateDeviceInput) {
  return request<InventoryDevice>('/api/v1/inventory/devices', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function transitionInventoryDevice(
  deviceId: number,
  payload: InventoryDeviceTransitionInput,
) {
  return request<InventoryDevice>(
    '/api/v1/inventory/devices/' + deviceId + '/transition',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function disposeInventoryScrapDevice(
  deviceId: number,
  payload: ScrapDisposalInput,
) {
  return request<ScrapDisposal>(
    '/api/v1/inventory/devices/' + deviceId + '/scrap-dispose',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function getInventoryDeviceLedger(deviceId: number) {
  return request<ListResponse<InventoryDeviceLedgerEntry>>(
    '/api/v1/inventory/devices/' + deviceId + '/ledger',
  )
}

export function getInventoryLedger() {
  return request<ListResponse<InventoryDeviceLedgerEntry>>('/api/v1/inventory/ledger')
}

export function getInventorySummary() {
  return request<ListResponse<InventorySummary>>('/api/v1/inventory/summary')
}

export function getInventoryDocuments() {
  return request<ListResponse<InventoryStockDocument>>('/api/v1/inventory/documents')
}

export function getInventoryRMAs(page = 1, pageSize = 20) {
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(pageSize),
  })
  return request<InventoryRMAPage>(
    '/api/v1/inventory/rmas?' + params.toString(),
  )
}

export function createInventoryRMA(payload: InventoryCreateRMAInput) {
  return request<InventoryRMA>('/api/v1/inventory/rmas', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function acceptInventoryRMA(rmaId: number) {
  return request<InventoryRMA>('/api/v1/inventory/rmas/' + rmaId + '/accept', {
    method: 'POST',
  })
}

export function startInventoryRMARepair(rmaId: number) {
  return request<InventoryRMA>('/api/v1/inventory/rmas/' + rmaId + '/start-repair', {
    method: 'POST',
  })
}

export function getInventoryRMAEvents(rmaId: number) {
  return request<ListResponse<RMAEvent>>('/api/v1/inventory/rmas/' + rmaId + '/events')
}

export function getInventoryRMACosts(rmaId: number) {
  return request<ListResponse<RMACost>>('/api/v1/inventory/rmas/' + rmaId + '/costs')
}

export function createInventoryRMACost(rmaId: number, payload: CreateRMACostInput) {
  return request<RMACost>('/api/v1/inventory/rmas/' + rmaId + '/costs', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getAfterSalesRequests(page = 1, pageSize = 20) {
  const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
  return request<AfterSalesRequestPage>('/api/v1/after-sales/requests?' + params.toString())
}

export function createAfterSalesRequest(payload: AfterSalesRequestInput) {
  return request<InventoryRMA>('/api/v1/after-sales/requests', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getAfterSalesRequestEvents(rmaId: number) {
  return request<ListResponse<RMAEvent>>('/api/v1/after-sales/requests/' + rmaId + '/events')
}

export function completeInventoryRMA(
  rmaId: number,
  payload: InventoryCompleteRMAInput,
) {
  return request<InventoryRMA>(
    '/api/v1/inventory/rmas/' + rmaId + '/complete',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function getLogisticsShipments() {
  return request<ListResponse<LogisticsShipment>>('/api/v1/logistics/shipments')
}

export function createLogisticsShipment(payload: CreateLogisticsShipmentInput) {
  return request<LogisticsShipment>('/api/v1/logistics/shipments', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateLogisticsShipmentStatus(
  shipmentId: number,
  payload: UpdateLogisticsShipmentStatusInput,
) {
  return request<LogisticsShipment>(
    '/api/v1/logistics/shipments/' + shipmentId + '/status',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}


export function getAdminSalesPerformance(period: string) {
  return request<SalesPerformanceResponse>(
    '/api/v1/admin/sales/performance?period=' + encodeURIComponent(period),
  )
}


export function getIncentivePrograms(programType?: string) {
  const suffix = programType ? '?type=' + encodeURIComponent(programType) : ''
  return request<ListResponse<IncentiveProgram>>('/api/v1/commercial/incentives' + suffix)
}

export function createIncentiveProgram(payload: IncentiveProgramInput) {
  return request<IncentiveProgram>('/api/v1/commercial/incentives', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function saveIncentiveProgramDraft(
  programId: number,
  payload: IncentiveProgramInput,
) {
  return request<IncentiveProgram>(
    '/api/v1/commercial/incentives/' + programId + '/draft',
    {
      method: 'PUT',
      body: JSON.stringify(payload),
    },
  )
}

export function publishIncentiveProgram(programId: number) {
  return request<IncentiveProgram>(
    '/api/v1/commercial/incentives/' + programId + '/publish',
    { method: 'POST' },
  )
}

export function getOperatingFinance() {
  return request<OperatingFinanceOverview>('/api/v1/finance/operating')
}

export function createTokenPurchase(payload: TokenPurchaseInput) {
  return request<TokenPurchase>('/api/v1/finance/token-purchases', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getFinanceSettlementDashboard() {
  return request<SettlementDashboard>('/api/v1/finance/settlements')
}

export function createSettlementBatch(payload: CreateSettlementBatchInput) {
  return request<SettlementBatch>('/api/v1/finance/settlements/batches', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function approveSettlementBatch(batchId: number) {
  return request<SettlementBatch>(
    '/api/v1/finance/settlements/batches/' + batchId + '/approve',
    { method: 'POST' },
  )
}

export function rejectSettlementBatch(batchId: number) {
  return request<SettlementBatch>(
    '/api/v1/finance/settlements/batches/' + batchId + '/reject',
    { method: 'POST' },
  )
}

export function paySettlementBatch(batchId: number) {
  return request<SettlementBatch>(
    '/api/v1/finance/settlements/batches/' + batchId + '/pay',
    { method: 'POST' },
  )
}


export function getAdminAgentExitCheck(organizationId: number) {
  return request<AgentExitCheck>(
    '/api/v1/admin/agents/' + organizationId + '/exit-check',
  )
}

export function getAdminAgentExitHistory(organizationId: number) {
  return request<ListResponse<AgentExitRecord>>(
    '/api/v1/admin/agents/' + organizationId + '/exit-history',
  )
}

export function finalizeAdminAgentExit(organizationId: number) {
  return request<{ ok: boolean; check: AgentExitCheck }>(
    '/api/v1/admin/agents/' + organizationId + '/finalize-exit',
    { method: 'POST' },
  )
}


export function getAdminAgentLevels() {
  return request<{ levels: AgentLevel[]; history: AgentLevelHistory[] }>(
    '/api/v1/admin/agent-levels',
  )
}

export function createAdminAgentLevel(payload: {
  code: string
  name: string
  status: string
  entry_fee_cents: number
  included_devices: number
  device_discount_bps: number
  consumer_share_bps: number
  reserve_bps: number
  settlement_cycle: string
  hold_days: number
  oem_enabled: boolean
  note: string
}) {
  return request<AgentLevel>('/api/v1/admin/agent-levels', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateAdminAgentLevel(
  levelId: number,
  payload: {
    code: string
    name: string
    status: string
    entry_fee_cents: number
    included_devices: number
    device_discount_bps: number
    consumer_share_bps: number
    reserve_bps: number
    settlement_cycle: string
    hold_days: number
    oem_enabled: boolean
    note: string
  },
) {
  return request<AgentLevel>('/api/v1/admin/agent-levels/' + levelId, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function assignAdminAgentLevel(
  organizationId: number,
  payload: {
    level_id: number
    effective_at: string
    reason: string
  },
) {
  return request<AgentLevelHistory>(
    '/api/v1/admin/agents/' + organizationId + '/level-assignments',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function getAdminAgentContracts() {
  return request<{ items: AgentContract[] }>('/api/v1/admin/agent-contracts')
}

export function createAdminAgentContract(payload: {
  external_contract_no: string
  agent_tenant_id: number
  parent_contract_id?: number
  contract_type: string
  level_id?: number
  starts_on: string
  ends_on?: string
  contract_amount_cents: number
  note: string
}) {
  return request<AgentContract>('/api/v1/admin/agent-contracts', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateAdminAgentContract(
  contractId: number,
  payload: {
    external_contract_no: string
    agent_tenant_id: number
    parent_contract_id?: number
    contract_type: string
    level_id?: number
    starts_on: string
    ends_on?: string
    contract_amount_cents: number
    note: string
  },
) {
  return request<AgentContract>('/api/v1/admin/agent-contracts/' + contractId, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function transitionAdminAgentContract(
  contractId: number,
  status: string,
) {
  return request<AgentContract>(
    '/api/v1/admin/agent-contracts/' + contractId + '/transition',
    {
      method: 'POST',
      body: JSON.stringify({ status }),
    },
  )
}

export function uploadAdminAgentContractAttachments(contractId: number, files: File[]) {
  const body = new FormData()
  files.forEach((file) => body.append('files', file))
  return request<{ items: import('./types').AgentContractAttachment[] }>(
    '/api/v1/admin/agent-contracts/' + contractId + '/attachments',
    { method: 'POST', body },
  )
}

export function deleteAdminAgentContractAttachment(contractId: number, attachmentId: number) {
  return request<void>(
    '/api/v1/admin/agent-contracts/' + contractId + '/attachments/' + attachmentId,
    { method: 'DELETE' },
  )
}