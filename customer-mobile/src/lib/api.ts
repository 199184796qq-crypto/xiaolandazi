import type {
  AgentChatResponse,
  Bootstrap,
  CatalogItem,
  ListResponse,
  PublicSystemConfig,
  ResourceDashboard,
  Room,
  ShopOrder,
} from './types';

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  if (init?.body !== undefined && !headers.has('Content-Type')) {
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
