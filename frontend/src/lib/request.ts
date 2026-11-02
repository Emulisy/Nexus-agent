/**
 * Shared JSON request client for the /api/v1 response envelope.
 * Requests attach the in-memory access token, unwrap successful data, and turn
 * business errors into ApiError. Concurrent 401 responses share one token refresh
 * and retry their original requests once. Streaming chat has a separate transport
 * in sse.ts and does not inherit this refresh-and-replay behavior.
 */
import type { Envelope } from '@/types/api';

const BASE = '/api/v1';

let getAccessToken: () => string | undefined = () => undefined;
let onAuthExpired: () => void = () => undefined;
let onTokenRefreshed: (token: string) => void = () => undefined;

/** Inject auth callbacks to avoid a dependency on the store. */
export function bindAuthHooks(
  getter: () => string | undefined,
  expired: () => void,
  refreshed: (token: string) => void,
) {
  getAccessToken = getter;
  onAuthExpired = expired;
  onTokenRefreshed = refreshed;
}

/** Share one refresh request across concurrent 401 responses. */

let refreshing: Promise<string | null> | null = null;

/**
 * Exchange the persisted refresh token without using request(), preventing a
 * recursive 401 retry. Rotate the stored refresh token and synchronously update
 * the auth store before releasing callers that are waiting to replay requests.
 * Missing credentials, invalid envelopes, and transport failures resolve to null.
 */
async function doRefresh(): Promise<string | null> {
  const refreshToken = localStorage.getItem('nx.refresh');
  if (!refreshToken) return null;
  try {
    const res = await fetch(`${BASE}/auth/refresh`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: refreshToken }),
    });
    const env = (await res.json()) as Envelope<{
      access_token: string;
      refresh_token: string;
    }>;
    if (res.status !== 200 || env.code !== 0 || !env.data) return null;
    localStorage.setItem('nx.refresh', env.data.refresh_token);
    onTokenRefreshed(env.data.access_token);
    return env.data.access_token;
  } catch {
    return null;
  }
}

/**
 * Coalesce concurrent refresh attempts into one promise. All waiting requests
 * receive the same outcome; finally clears the shared slot so a later expiry
 * can start another exchange.
 */
function refreshOnce(): Promise<string | null> {
  refreshing ??= doRefresh().finally(() => {
    refreshing = null;
  });
  return refreshing;
}

/**
 * Application error with both backend code and HTTP status. Pages can display
 * message while callers retain structured information for status-specific handling.
 */
export class ApiError extends Error {
  /** Backend application code, distinct from the transport status. */
  readonly code: number;
  /** HTTP status retained for diagnostics and status-specific callers. */
  readonly status: number;

  constructor(code: number, message: string, status: number) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.status = status;
  }
}

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE';
  /** JSON body serialized by request(); use formData for file uploads. */
  body?: unknown;
  /** Additional URL parameters; undefined and empty-string values are omitted. */
  query?: Record<string, string | number | boolean | undefined>;
  /** Set false to skip the single refresh-and-replay attempt on HTTP 401. */
  retry401?: boolean;
  /** Leave Content-Type unset so the browser supplies the multipart boundary. */
  formData?: FormData;
  /** Optional cancellation signal for ordinary REST requests. */
  signal?: AbortSignal;
}

/**
 * Serialize defined, nonempty query values. False and zero are retained because
 * they can be meaningful filters; omitted/empty fields do not reach the URL.
 */
function buildQuery(query?: RequestOptions['query']): string {
  if (!query) return '';
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(query)) {
    if (v !== undefined && v !== '') qs.set(k, String(v));
  }
  const s = qs.toString();
  return s ? `?${s}` : '';
}

/**
 * Attach authentication, serialize the request body, and unwrap the server envelope.
 * A successful refresh replays the same options once with retry401 disabled.
 * JSON and FormData are separate paths so multipart boundaries remain browser-owned.
 * Network errors propagate; malformed response JSON becomes an HTTP-context ApiError.
 */
export async function request<T>(
  path: string,
  options: RequestOptions = {},
): Promise<T> {
  const { method = 'GET', body, query, retry401 = true, formData, signal } = options;

  const headers: Record<string, string> = {};
  const token = getAccessToken();
  if (token) headers.Authorization = `Bearer ${token}`;

  let payload: BodyInit | undefined;
  if (formData) {
    payload = formData;
  } else if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    payload = JSON.stringify(body);
  }

  const res = await fetch(
    `${BASE}${path}${buildQuery(query)}`,
    { method, headers, body: payload, signal },
  );

  // Retry once after refreshing the access token.
  if (res.status === 401 && retry401) {
    const newToken = await refreshOnce();
    if (newToken) {
      return request<T>(path, { ...options, retry401: false });
    }
    onAuthExpired();
    throw new ApiError(20101, 'Your session has expired', 401);
  }

  /**
   * HTTP error responses are expected to use the same JSON envelope. A non-JSON
   * response keeps its HTTP status in the fallback error.
   */
  let env: Envelope<T>;
  try {
    env = (await res.json()) as Envelope<T>;
  } catch {
    throw new ApiError(-1, `Service error (HTTP ${res.status})`, res.status);
  }

  if (env.code !== 0) {
    throw new ApiError(env.code, env.message || 'Request failed', res.status);
  }
  return env.data as T;
}

/**
 * Verb-specific entry points used by API modules. These helpers preserve the
 * shared authentication/envelope behavior and return the unwrapped data field.
 */
export const http = {
  get: <T>(path: string, query?: RequestOptions['query']) =>
    request<T>(path, { method: 'GET', query }),
  post: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'POST', body }),
  put: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'PUT', body }),
  delete: <T>(path: string) => request<T>(path, { method: 'DELETE' }),
  upload: <T>(path: string, formData: FormData) =>
    request<T>(path, { method: 'POST', formData }),
};

/**
 * Convert known request failures and ordinary Errors to display text without
 * requiring each page to know the error representation.
 */
export function errMsg(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof Error) return e.message;
  return 'Unknown error';
}
