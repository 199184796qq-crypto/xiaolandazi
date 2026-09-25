import { writable } from 'svelte/store';
import { getBootstrap } from './api';
import type { Bootstrap } from './types';

interface SessionState {
  bootstrap: Bootstrap | null;
  loading: boolean;
  initialized: boolean;
  error: string;
}

export const session = writable<SessionState>({
  bootstrap: null,
  loading: false,
  initialized: false,
  error: '',
});

export async function loadSession() {
  session.update((state) => ({ ...state, loading: true, error: '' }));
  try {
    const bootstrap = await getBootstrap();
    session.set({ bootstrap, loading: false, initialized: true, error: '' });
    return bootstrap;
  } catch (error) {
    session.set({
      bootstrap: null,
      loading: false,
      initialized: true,
      error: error instanceof Error ? error.message : '读取登录状态失败',
    });
    throw error;
  }
}

export function applySession(bootstrap: Bootstrap) {
  session.set({ bootstrap, loading: false, initialized: true, error: '' });
}

export function clearSession() {
  session.set({ bootstrap: null, loading: false, initialized: true, error: '' });
}
