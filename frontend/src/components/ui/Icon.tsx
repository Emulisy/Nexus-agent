/**
 * Central inline SVG icon set for navigation and feature surfaces.
 * IconName restricts callers to defined paths; forwarded SVG props provide size,
 * color, and accessibility overrides without duplicating path data.
 */
import type { ReactNode, SVGProps } from 'react';

/** Names supported by the fixed inline path catalog. */
export type IconName = 'chat' | 'agent' | 'memory' | 'knowledge' | 'workflow' | 'models' | 'integrations' | 'logout' | 'arrow';

const PATHS: Record<IconName, ReactNode> = {
  chat: <><path d="M20 11.5a7.5 7.5 0 0 1-7.5 7.5H5l-3 3v-9.5A7.5 7.5 0 0 1 9.5 5H12" /><path d="M15 3h7v7M22 3l-8 8" /></>,
  agent: <><rect x="4" y="6" width="16" height="14" rx="4" /><path d="M12 3v3M2 11v4M22 11v4M9 16h6" /><path d="M8.5 11h.01m7 0h.01" strokeWidth="3" /></>,
  memory: <><rect x="5" y="5" width="14" height="14" rx="3" /><path d="M9 9h6v6H9zM9 2v3m6-3v3M9 19v3m6-3v3M2 9h3m-3 6h3m14-6h3m-3 6h3" /></>,
  knowledge: <><path d="M12 6v15m0-15C9 3.5 5.5 3.5 2 5v14c3.5-1.5 7-1.5 10 1 3-2.5 6.5-2.5 10-1V5c-3.5-1.5-7-1.5-10 1Z" /><path d="m6 9 2 .5m-2 4 2 .5m8-4.5 2-.5m-2 5 2-.5" /></>,
  workflow: <><rect x="3" y="3" width="6" height="6" rx="1.5" /><rect x="15" y="15" width="6" height="6" rx="1.5" /><path d="M9 6h6a3 3 0 0 1 3 3v6M6 9v6a3 3 0 0 0 3 3h6" /></>,
  models: <path d="m12 2 9 5v10l-9 5-9-5V7l9-5Zm0 10 9-5M12 12 3 7m9 5v10" />,
  integrations: <path d="m8 3 4 4-5 5-4-4 5-5Zm9 9 4 4-5 5-4-4 5-5ZM8 16l8-8M4 20l3-3m10-10 3-3" />,
  logout: <path d="M9 4H5a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h4m6-4 4-4-4-4m4 4H9" />,
  arrow: <path d="M4 12h16m-6-6 6 6-6 6" />,
};

/**
 * Render a decorative icon by default. Forwarded SVG props can override the
 * base attributes when a caller needs a different accessible presentation.
 */
export function Icon({ name, ...props }: { name: IconName } & SVGProps<SVGSVGElement>) {
  return <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" {...props}>{PATHS[name]}</svg>;
}
