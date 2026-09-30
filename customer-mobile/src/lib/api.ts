import type {
  AgentChatResponse,
  AgentDecisionEnqueueResult,
  AgentDecisionSnapshot,
  Bootstrap,
  CatalogItem,
  LiveAgentPlan,
  LiveAddressingStrategy,
  LiveDevice,
  LiveRuntimeSnapshot,
  ListResponse,
  PublicSystemConfig,
  ResourceDashboard,
  Room,
  RoomEvent,
  RoomSessionStats,
  ShopOrder,
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
export const getRoomLiveAgentPlans = (roomId: number) =>
  request<{ items: LiveAgentPlan[] }>('/api/v1/rooms/' + roomId + '/live-agent-plans');
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
export const getMembershipOffers = () => request<ListResponse<CatalogItem>>('/api/v1/shop/memberships');
export const getTimeCardOffers = () => request<ListResponse<CatalogItem>>('/api/v1/shop/time-cards');
export const getDeviceOffers = () => request<ListResponse<CatalogItem>>('/api/v1/shop/devices');
export const getOrders = () => request<ListResponse<ShopOrder>>('/api/v1/shop/orders');

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
        { title: '订单', to: '/orders', section: '终端' },
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

export function transcribeRoomAgentVoice(roomId: number, audio: Blob, filename: string) {
  const form = new FormData();
  form.append('file', audio, filename);
  return request<{ text: string; task_id?: string }>(
    '/api/v1/rooms/' + roomId + '/agent-voice/transcribe',
    { method: 'POST', body: form },
  );
}
