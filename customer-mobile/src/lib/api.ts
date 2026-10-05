import type {
  AgentChatResponse,
  AgentDecisionEnqueueResult,
  AgentDecisionSnapshot,
  BeanPurchaseOrder,
  BeanWalletDashboard,
  Bootstrap,
  CreateShopOrderInput,
  DeviceOffer,
  FinanceDashboard,
  InvitationDashboard,
  InvitePreview,
  LiveAgentPlan,
  LiveAddressingStrategy,
  LiveDevice,
  LiveQuotaSummary,
  LiveRuntimeSnapshot,
  LiveTimeCardPage,
  ListResponse,
  MembershipOffer,
  PublicSystemConfig,
  ResourceDashboard,
  Room,
  RoomEvent,
  RoomSessionStats,
  RechargeRequestResult,
  ReferralWalletDashboard,
  ShopOrder,
  TimeCardOffer,
  WithdrawalRequest,
	WechatPrepayResponse,
	WechatRechargePrepayResponse,
	RechargeRecord,
	WechatCashRefund,
	WechatRefundWallet,
} from './types';

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  const isFormDataBody = typeof FormData !== 'undefined' && init?.body instanceof FormData;
  if (init?.body !== undefined && !isFormDataBody && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }

  let response: Response;
  try {
    response = await fetch(url, {
      credentials: 'include',
      ...init,
      headers,
    });
  } catch {
    throw new Error('服务暂时不可用，请稍后重试');
  }

  if (!response.ok) {
    let message = response.status === 401 ? '登录已失效，请重新登录' : '请求失败';
    try {
      const body = (await response.json()) as { error?: string };
      if (body.error) message = body.error;
    } catch {
      // Keep fallback message.
    }
    throw new Error(message);
  }

  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

export const getBootstrap = () => request<Bootstrap>('/api/v1/bootstrap');

export function login(payload: { username: string; password: string; captcha: string }) {
  return request<Bootstrap>('/api/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify({
      method: 'password',
      identifier: payload.username,
      credential: payload.password,
      captcha: payload.captcha,
    }),
  });
}

export function getInvitePreview(code: string) {
  return request<InvitePreview>('/api/v1/auth/invite/' + encodeURIComponent(code.trim()));
}

export function register(payload: {
  username: string;
  phone: string;
  password: string;
  confirm_password: string;
  invite_code: string;
  captcha: string;
}) {
  return request<Bootstrap>('/api/v1/auth/register', {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}

export const logout = () => request<void>('/api/v1/auth/logout', { method: 'POST' });
export const getPublicSystemConfig = () => request<PublicSystemConfig>('/api/v1/system/public-config');
export const getCurrentResources = () => request<ResourceDashboard>('/api/v1/resources');
export const getRooms = () => request<ListResponse<Room>>('/api/v1/rooms');
export const getRoom = (roomId: number) => request<Room>('/api/v1/rooms/' + roomId);
export const getRoomSessionStats = (roomId: number) =>
  request<RoomSessionStats>('/api/v1/rooms/' + roomId + '/session-stats');
export function resolveRoomSessionDecision(roomId: number, action: 'merge' | 'fresh') {
  return request<RoomSessionStats>('/api/v1/rooms/' + roomId + '/session-decision', {
    method: 'POST',
    body: JSON.stringify({ action }),
  });
}
export const getRoomEvents = (
  roomId: number,
  options: {
    limit?: number;
    channel?: 'important';
    eventType?: string;
    beforeId?: number;
  } = {},
) => {
  const params = new URLSearchParams();
  params.set('limit', String(options.limit || 120));
  if (options.channel) params.set('channel', options.channel);
  if (options.eventType) params.set('type', options.eventType);
  if (options.beforeId && options.beforeId > 0) params.set('before_id', String(options.beforeId));
  return request<ListResponse<RoomEvent>>('/api/v1/rooms/' + roomId + '/events?' + params.toString());
};
export const getLiveDevices = () => request<LiveDevice[]>('/api/v1/live/devices');
export const getLiveQuotaSummary = () => request<LiveQuotaSummary>('/api/v1/live/quota-summary');
export const getLiveTimeCards = (page = 1, pageSize = 4) =>
  request<LiveTimeCardPage>(`/api/v1/live/time-cards?page=${page}&page_size=${pageSize}`);
export const activateLiveTimeCard = (assetId: number) =>
  request<{ asset_id: number; quota: LiveQuotaSummary }>(`/api/v1/live/time-cards/${assetId}/activate`, {
    method: 'POST',
  });
export const getDefaultDeviceName = () => request<{ device_name: string }>('/api/v1/live/devices/default-name');
export const claimDevice = (bindingCode: string, deviceName: string) => request<LiveDevice>('/api/v1/live/devices/claim', {
  method: 'POST', body: JSON.stringify({ binding_code: bindingCode, device_name: deviceName }),
});
export const renameDevice = (id: number, deviceName: string) => request<LiveDevice>('/api/v1/live/devices/' + id, {
  method: 'PATCH', body: JSON.stringify({ device_name: deviceName }),
});
export const bindDeviceRoom = (id: number, roomId: number, bindingRole: 'primary' | 'listener' = 'primary') => request<LiveDevice>('/api/v1/live/devices/' + id + '/bind', {
	method: 'POST', body: JSON.stringify({ room_id: roomId, binding_role: bindingRole }),
});
export const unbindDeviceRoom = (id: number) => request<LiveDevice>('/api/v1/live/devices/' + id + '/bind', { method: 'DELETE' });
export type DeviceAddressingMode = 'auto' | 'female' | 'male' | 'child' | 'neutral';
export const getDeviceAddressing = (id: number) => request<{ mode: DeviceAddressingMode }>('/api/v1/live/devices/' + id + '/addressing');
export const saveDeviceAddressing = (id: number, mode: DeviceAddressingMode) => request<{ mode: DeviceAddressingMode }>('/api/v1/live/devices/' + id + '/addressing', { method: 'PUT', body: JSON.stringify({ mode }) });
export const getAddressingStrategy = () => request<LiveAddressingStrategy>('/api/v1/live/addressing-strategy');
export function updateAddressingStrategy(payload: LiveAddressingStrategy) {
  return request<LiveAddressingStrategy>('/api/v1/live/addressing-strategy', {
    method: 'PUT',
    body: JSON.stringify(payload),
  });
}
export const getLiveRuntime = (roomId: number) =>
  request<LiveRuntimeSnapshot>('/api/v1/rooms/' + roomId + '/runtime');
export function setLiveRuntimeMode(roomId: number, mode: 'control' | 'anchor') {
  return request<LiveRuntimeSnapshot>('/api/v1/rooms/' + roomId + '/runtime/mode', {
    method: 'POST',
    body: JSON.stringify({ mode }),
  });
}
export const getRoomLiveAgentPlans = (roomId: number, publishedOnly = false) =>
  request<{ items: LiveAgentPlan[] }>(
    '/api/v1/rooms/' + roomId + '/live-agent-plans' + (publishedOnly ? '?published_only=1' : ''),
  );
export function setLiveRuntimePlan(roomId: number, planId: number) {
  return request<LiveRuntimeSnapshot>('/api/v1/rooms/' + roomId + '/runtime/plan', {
    method: 'POST',
    body: JSON.stringify({ plan_id: planId }),
  });
}
export function startLiveRuntime(roomId: number) {
  return request<unknown>('/api/v1/rooms/' + roomId + '/runtime/start', {
    method: 'POST',
    body: JSON.stringify({}),
  });
}
export function pauseLiveRuntime(roomId: number) {
  return request<unknown>('/api/v1/rooms/' + roomId + '/runtime/pause', { method: 'POST' });
}
export function resumeLiveRuntime(roomId: number) {
  return request<unknown>('/api/v1/rooms/' + roomId + '/runtime/resume', { method: 'POST' });
}
export function stopLiveRuntime(roomId: number) {
  return request<unknown>('/api/v1/rooms/' + roomId + '/runtime/stop', {
    method: 'POST',
    body: JSON.stringify({ reason: 'customer_mobile_stop' }),
  });
}
export function createRoom(payload: {
  platform: string;
  external_room_id: string;
  name: string;
  collector_mode: string;
}) {
  return request<Room>('/api/v1/rooms', {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}
export const getMembershipOffers = () => request<ListResponse<MembershipOffer>>('/api/v1/shop/memberships');
export const getTimeCardOffers = () => request<ListResponse<TimeCardOffer>>('/api/v1/shop/time-cards');
export const getDeviceOffers = () => request<ListResponse<DeviceOffer>>('/api/v1/shop/devices');
export const getOrders = () => request<ListResponse<ShopOrder>>('/api/v1/shop/orders');
export const getFinanceDashboard = (limit = 100) => request<FinanceDashboard>(`/api/v1/finance/dashboard?limit=${limit}`);
export const getCustomerBeanWallet = () => request<BeanWalletDashboard>('/api/v1/finance/beans');
export const getReferralWallet = (limit = 100) => request<ReferralWalletDashboard>(`/api/v1/finance/referral-wallet?limit=${limit}`);
export function getInvitationDashboard(params: {
  record_page?: number;
  record_page_size?: number;
} = {}) {
  const search = new URLSearchParams();
  if (params.record_page) search.set('record_page', String(params.record_page));
  if (params.record_page_size) search.set('record_page_size', String(params.record_page_size));
  const query = search.toString();
  return request<InvitationDashboard>('/api/v1/invitations/dashboard' + (query ? '?' + query : ''));
}
export function updateOwnInviteCodeStatus(status: 'active' | 'disabled') {
  return request<void>('/api/v1/invitations/mine', {
    method: 'PATCH',
    body: JSON.stringify({ status }),
  });
}
export const getCustomerWithdrawals = (accountType = 'all') =>
  request<{ items: WithdrawalRequest[] }>(`/api/v1/finance/withdrawals?account_type=${encodeURIComponent(accountType)}`);
export function createCustomerRechargeRequest(amountCents: number, reason: string) {
  return request<RechargeRequestResult>('/api/v1/finance/recharge-request', {
    method: 'POST',
    body: JSON.stringify({ amount_cents: amountCents, reason }),
  });
}
export function createCustomerWalletWithdrawal(accountType: 'cash' | 'reward', amountCents: number) {
  return request<WithdrawalRequest>('/api/v1/finance/withdrawals', {
    method: 'POST',
    body: JSON.stringify({ account_type: accountType, amount_cents: amountCents }),
  });
}
export function createReferralWithdrawal(amountCents: number) {
  return request<WithdrawalRequest>('/api/v1/finance/referral-withdrawals', {
    method: 'POST',
    body: JSON.stringify({ amount_cents: amountCents }),
  });
}
export function purchaseCustomerBeans(amountCents: number, idempotencyKey: string) {
  return request<BeanPurchaseOrder>('/api/v1/finance/beans/purchases', {
    method: 'POST',
    body: JSON.stringify({ amount_cents: amountCents, idempotency_key: idempotencyKey }),
  });
}
export function createShopOrder(payload: CreateShopOrderInput) {
  return request<ShopOrder>('/api/v1/shop/orders', {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}
export function walletPayShopOrder(orderId: number, idempotencyKey: string) {
  return request<{ order: ShopOrder; payment_method: string }>(`/api/v1/shop/orders/${orderId}/wallet-pay`, {
    method: 'POST',
    body: JSON.stringify({ idempotency_key: idempotencyKey }),
  });
}
export const getShopOrder = (orderId: number) => request<ShopOrder>(`/api/v1/shop/orders/${orderId}`);
export const getWechatRefundWallet = () => request<WechatRefundWallet>('/api/v1/wallet/wechat-refunds');
export const createWechatCashRefund = (amountCents: number, idempotencyKey: string) => request<WechatCashRefund>('/api/v1/wallet/wechat-refunds', { method: 'POST', body: JSON.stringify({ amount_cents: amountCents, idempotency_key: idempotencyKey }) });
export const queryWechatCashRefund = (id: number) => request<WechatCashRefund>(`/api/v1/wallet/wechat-refunds/${id}/query`, { method: 'POST', body: '{}' });
export const getWechatPaymentStatus = () => request<{ enabled: boolean; mode: string; auto_renew_supported: boolean }>('/api/v1/payments/wechat/status');

export function createWechatRecharge(amountCents: number, idempotencyKey: string) {
  return request<RechargeRecord>('/api/v1/wallet/recharges', { method: 'POST', body: JSON.stringify({ amount_cents: amountCents, idempotency_key: idempotencyKey }) });
}
export const getWechatRecharge = (id: number) => request<RechargeRecord>(`/api/v1/wallet/recharges/${id}`);
export const prepayWechatRecharge = (id: number) => request<WechatRechargePrepayResponse>(`/api/v1/wallet/recharges/${id}/wechat-prepay`, { method: 'POST', body: '{}' });
export const queryWechatRecharge = (id: number) => request<{ recharge: RechargeRecord; trade_state: string }>(`/api/v1/wallet/recharges/${id}/wechat-query`, { method: 'POST', body: '{}' });

export function prepayWechatShopOrder(orderId: number) {
  return request<WechatPrepayResponse>(`/api/v1/shop/orders/${orderId}/wechat-prepay`, {
    method: 'POST',
    body: '{}',
  });
}

export function queryWechatShopOrder(orderId: number) {
  return request<{ order: ShopOrder; trade_state: string }>(`/api/v1/shop/orders/${orderId}/wechat-query`, { method: 'POST', body: '{}' });
}

export function chatClientAgent(
  message: string,
  history: Array<{ role: 'user' | 'agent'; text: string }>,
) {
  return request<AgentChatResponse>('/api/v1/client-agent/chat', {
    method: 'POST',
    body: JSON.stringify({
      message,
      history,
      current_path: location.pathname,
      navigation: [
        { title: '首页', to: '/', section: '终端' },
        { title: '商城', to: '/shop', section: '终端' },
        { title: '我的钱包', to: '/wallet', section: '终端' },
        { title: '邀请', to: '/invite', section: '终端' },
        { title: '我的', to: '/me', section: '终端' },
      ],
    }),
  });
}

export function classifyLiveRoomAgentMessage(
  roomId: number,
  payload: {
    message: string;
    current_mode?: 'chat' | 'learning' | 'test' | 'execution';
    learning_active?: boolean;
    test_active?: boolean;
    execution_active?: boolean;
    history?: Array<{ role: 'user' | 'agent'; text: string }>;
  },
) {
  return request<{ intent: 'chat' | 'learning' | 'test' | 'execution' | 'adopt'; confidence?: string; reason?: string }>(
    '/api/v1/live/rooms/' + roomId + '/agent-learning/intent',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  );
}

export function enqueueRoomManualAgentDecision(
  roomId: number,
  payload: {
    question?: string;
    title?: string;
    summary?: string;
    reply_hint?: string;
    event_id?: number;
    user_id?: string;
    force_reopen?: boolean;
    manual_action?: 'answer' | 'quick';
    manual_origin?: 'agent_input' | 'agent_input_preview';
    execution_mode?: 'intent' | 'verbatim';
    fixed_text?: string;
    ttl_seconds?: number;
  },
) {
  return request<AgentDecisionEnqueueResult>('/api/v1/rooms/' + roomId + '/agent-decisions/manual', {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}

export const getRoomAgentDecisions = (roomId: number) =>
  request<AgentDecisionSnapshot>('/api/v1/rooms/' + roomId + '/agent-decisions');

export function createAgentLearningSession(
  roomId: number,
  payload: {
    source_type?: string;
    source_ref?: string;
    question?: string;
    original_reply?: string;
    target?: string;
  },
) {
  return request<{
    id: number;
    target?: string;
    status: string;
  }>('/api/v1/live/rooms/' + roomId + '/agent-learning/sessions', {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}

export function createAgentLearningTurn(roomId: number, sessionId: number, feedback: string) {
  return request<{
    session: { id: number; target?: string; status: string };
    result: {
      id: number;
      target: string;
      result_text: string;
      memory_type?: string;
      matched_memory_item_id?: number;
    };
  }>('/api/v1/live/rooms/' + roomId + '/agent-learning/sessions/' + sessionId + '/turns', {
    method: 'POST',
    body: JSON.stringify({ feedback }),
  });
}

export function adoptAgentLearningSession(roomId: number, sessionId: number) {
  return request<{
    memory: { id: number; target: string; memory_type: string };
    version: { id: number; version_no: number; content_text: string };
  }>('/api/v1/live/rooms/' + roomId + '/agent-learning/sessions/' + sessionId + '/adopt', {
    method: 'POST',
  });
}

export function transcribeRoomAgentVoice(roomId: number, audio: Blob, filename: string) {
  const form = new FormData();
  form.append('file', audio, filename);
  return request<{ text: string; task_id?: string }>(
    '/api/v1/rooms/' + roomId + '/agent-voice/transcribe',
    { method: 'POST', body: form },
  );
}
export const getMarketingCampaigns = () => request<{items:import('./types').MarketingCampaign[]}>('/api/v1/shop/marketing-campaigns');
export const claimFreeMarketingOrder = (id:number) => request<{order:ShopOrder}>('/api/v1/shop/orders/'+id+'/free-claim',{method:'POST',body:'{}'});
