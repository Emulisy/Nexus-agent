/**
 * Display formatting shared by chat and management pages.
 * Missing or invalid timestamps render as empty strings. Status metadata maps
 * backend document/workflow states to localized labels and presentation tones.
 */
/**
 * Format recent timestamps relative to the current clock; older values fall
 * back to a localized date. Invalid or absent input produces no label.
 */
export function relativeTime(iso?: string | null): string {
  if (!iso) return '';
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return '';
  const diff = Date.now() - t;
  const min = 60_000;
  const hour = 60 * min;
  const day = 24 * hour;
  if (diff < min) return 'Just now';
  if (diff < hour) return `${Math.floor(diff / min)} min ago`;
  if (diff < day) return `${Math.floor(diff / hour)} hr ago`;
  if (diff < 2 * day) return 'Yesterday';
  return new Date(iso).toLocaleDateString('en-US');
}

/**
 * Format an ISO timestamp as local calendar date and minute precision.
 * This uses the browser timezone rather than converting to a fixed server timezone.
 */
export function fullTime(iso?: string | null): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/**
 * Scale bytes through binary KB/MB/GB units with compact display precision.
 * Missing, zero, and negative values share the zero-byte label.
 */
export function fileSize(bytes?: number | null): string {
  if (!bytes || bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB'];
  let i = 0;
  let v = bytes;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`;
}

/** Abbreviate positive token counts at the thousand boundary for compact tables. */
export function tokenCount(n?: number | null): string {
  if (!n || n <= 0) return '0';
  if (n < 1000) return String(n);
  return `${(n / 1000).toFixed(1)}k`;
}

/** Bound a display preview with an ellipsis; the original source value is unchanged. */
export function truncate(s: string, max = 80): string {
  return s.length > max ? `${s.slice(0, max)}…` : s;
}

/**
 * Localized display metadata shared by document ingestion and workflow runs.
 * Consumers provide a readable fallback for backend states absent from this map.
 */
export const STATUS_LABEL: Record<string, { text: string; tone: 'ok' | 'warn' | 'err' | 'info' }> = {
  ready: { text: 'Ready', tone: 'ok' },
  succeeded: { text: 'Succeeded', tone: 'ok' },
  active: { text: 'Active', tone: 'ok' },
  connected: { text: 'Connected', tone: 'ok' },
  pending: { text: 'Queued', tone: 'info' },
  parsing: { text: 'Parsing', tone: 'info' },
  chunking: { text: 'Chunking', tone: 'info' },
  embedding: { text: 'Embedding', tone: 'info' },
  running: { text: 'Running', tone: 'info' },
  waiting_approval: { text: 'Awaiting approval', tone: 'warn' },
  failed: { text: 'Failed', tone: 'err' },
  error: { text: 'Error', tone: 'err' },
  canceled: { text: 'Canceled', tone: 'err' },
  disconnected: { text: 'Disconnected', tone: 'warn' },
  unknown: { text: 'Unknown', tone: 'warn' },
};
