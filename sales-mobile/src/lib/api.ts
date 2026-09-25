import type {
  AgentChatResponse,
  Bootstrap,
  ListResponse,
  PublicSystemConfig,
  SalesCustomer,
  SalesPerformanceResponse,
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
      // Keep fallback.
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
export const getSalesCustomers = () => request<ListResponse<SalesCustomer>>('/api/v1/sales/customers');

export function getSalesPerformance(period = new Date().toISOString().slice(0, 7)) {
  return request<SalesPerformanceResponse>(
    '/api/v1/admin/sales/performance?period=' + encodeURIComponent(period) + '&page=1&page_size=50',
  );
}

export function chatInternalAgent(
  message: string,
  history: Array<{ role: 'user' | 'agent'; text: string }>,
) {
  return request<AgentChatResponse>('/api/v1/internal-agent/chat', {
    method: 'POST',
    body: JSON.stringify({
      message,
      history,
      current_path: location.pathname,
      navigation: [
        { title: '工作台', to: '/', section: '销售手机端' },
        { title: '我的客户', to: '/customers', section: '销售手机端' },
        { title: '业务方案', to: '/business', section: '销售手机端' },
        { title: '业绩', to: '/performance', section: '销售手机端' },
        { title: '我的', to: '/me', section: '销售手机端' },
      ],
    }),
  });
}
