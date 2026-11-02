/**
 * Own the current user and token lifecycle. Access tokens remain in memory;
 * refresh tokens use localStorage under nx.refresh. Startup exchanges a persisted
 * refresh token before loading /me, while logout and expiry clear local state.
 * The request layer receives callbacks instead of importing this store directly.
 */
import { create } from 'zustand';
import { bindAuthHooks, http } from '@/lib/request';
import type { LoginReq, RegisterReq, TokenPair, User } from '@/types/auth';

interface AuthState {
  /** Access tokens stay in memory; refresh tokens survive reloads. */
  accessToken?: string;
  user?: User;
  /** Route guards wait for session restoration before redirecting. */
  bootstrapped: boolean;

  bootstrap: () => Promise<void>;
  login: (req: LoginReq) => Promise<void>;
  register: (req: RegisterReq) => Promise<void>;
  logout: () => Promise<void>;
  setToken: (token: string) => void;
  /**
   * Clear credentials after the request client cannot refresh an expired session.
   * Route guards react to the missing user and redirect protected pages.
   */
  expire: () => void;
}

const REFRESH_KEY = 'nx.refresh';

export const useAuth = create<AuthState>((set) => ({
  accessToken: undefined,
  user: undefined,
  bootstrapped: false,

  /**
   * Restore identity from a persisted refresh token before marking startup complete.
   * The access token is installed before /me so the identity request is authenticated.
   * A failed restoration clears the persisted refresh credential and lets route
   * guards decide which screen to show.
   */
  bootstrap: async () => {
    const refreshToken = localStorage.getItem(REFRESH_KEY);
    if (refreshToken) {
      try {
        const pair = await http.post<TokenPair>('/auth/refresh', {
          refresh_token: refreshToken,
        });
        localStorage.setItem(REFRESH_KEY, pair.refresh_token);
        set({ accessToken: pair.access_token }); // Set the token before fetching the authenticated user.
        const user = await http.get<User>('/me');
        set({ user });
      } catch {
        localStorage.removeItem(REFRESH_KEY);
      }
    }
    set({ bootstrapped: true });
  },

  /**
   * Persist the rotated refresh credential, install the access token, and load
   * the public user projection required by route guards and account controls.
   */
  login: async (req) => {
    const pair = await http.post<TokenPair>('/auth/login', req);
    localStorage.setItem(REFRESH_KEY, pair.refresh_token);
    set({ accessToken: pair.access_token });
    const user = await http.get<User>('/me');
    set({ user });
  },

  /** Create the account only; LoginPage follows this with login to establish tokens. */
  register: async (req) => {
    await http.post<{ id: number }>('/auth/register', req);
  },

  /**
   * Attempt server-side revocation, then clear local credentials even if the
   * request fails so the browser does not retain an authenticated identity.
   */
  logout: async () => {
    try {
      await http.post('/auth/logout');
    } finally {
      localStorage.removeItem(REFRESH_KEY);
      set({ accessToken: undefined, user: undefined });
    }
  },

  setToken: (token) => set({ accessToken: token }),

  expire: () => {
    localStorage.removeItem(REFRESH_KEY);
    set({ accessToken: undefined, user: undefined });
  },
}));

/**
 * Bind once after store creation. The callbacks read current state at call
 * time, avoiding a request-to-store import cycle or stale token closure.
 */
bindAuthHooks(
  () => useAuth.getState().accessToken,
  () => useAuth.getState().expire(),
  (t) => useAuth.getState().setToken(t),
);
