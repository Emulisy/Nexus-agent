/**
 * Authentication payloads and the public user projection returned by /me.
 * Token storage belongs to the auth store; these types describe wire payloads
 * and do not contain persistence or refresh scheduling behavior.
 */
/** Public identity used by route guards, account controls, and role-based navigation. */
export interface User {
  id: number;
  username: string;
  email?: string;
  /** Determines client-side admin navigation; API authorization is checked separately. */
  role: 'admin' | 'user';
  avatar_url?: string;
}

/**
 * Token exchange result returned by login and refresh. The frontend rotates the
 * persisted refresh token whenever this payload is received.
 */
export interface TokenPair {
  access_token: string;
  refresh_token: string;
  /** Access token lifetime in seconds. */
  expires_in?: number;
}

/** Registration form payload; a separate login request establishes the session. */
export interface RegisterReq {
  username: string;
  email: string;
  password: string;
}

/** Username/password credentials submitted to the login endpoint. */
export interface LoginReq {
  username: string;
  password: string;
}
