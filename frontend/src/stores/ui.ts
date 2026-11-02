/**
 * Global transient notifications shared by pages and request-driven actions.
 * Monotonic IDs keep duplicate messages independently dismissible. Each toast
 * expires after 3.2 seconds; ToastHost provides the visual presentation.
 */
import { create } from 'zustand';

export type ToastTone = 'ok' | 'err' | 'info';

/** One queued notification; IDs distinguish identical messages and support dismissal. */
export interface Toast {
  id: number;
  tone: ToastTone;
  message: string;
}

interface UIState {
  toasts: Toast[];
  push: (tone: ToastTone, message: string) => void;
  remove: (id: number) => void;
}

let seq = 0;

/**
 * Queue notifications independently of the routed page that produced them.
 * Expiry removes by ID, so manual dismissal and timeout can safely overlap.
 */
export const useUI = create<UIState>((set) => ({
  toasts: [],
  push: (tone, message) => {
    const id = ++seq;
    set((s) => ({ toasts: [...s.toasts, { id, tone, message }] }));
    setTimeout(() => {
      set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }));
    }, 3200);
  },
  remove: (id) => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
}));

/**
 * Imperative helpers for components and store actions that do not need a hook
 * subscription just to publish a notification.
 */
export const toast = {
  ok: (msg: string) => useUI.getState().push('ok', msg),
  err: (msg: string) => useUI.getState().push('err', msg),
  info: (msg: string) => useUI.getState().push('info', msg),
};
