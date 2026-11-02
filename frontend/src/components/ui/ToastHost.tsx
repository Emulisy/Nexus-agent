/**
 * Render the global notification queue above routed content.
 * Each item animates only when mounted; the store owns expiry and manual removal.
 * The portal keeps notifications outside page stacking contexts without moving focus.
 */
import { useEffect, useRef } from 'react';
import { createPortal } from 'react-dom';
import { gsap } from '@/lib/gsap';
import { useUI, type Toast } from '@/stores/ui';
import { clsx } from 'clsx';

const TONE_CLASS = {
  ok: 'border-pulse/50 text-pulse',
  err: 'border-coral/50 text-coral',
  info: 'border-teal/50 text-teal',
} as const;

/**
 * Animate a newly inserted notification once. Store IDs keep unchanged
 * items mounted while other notifications are added or removed.
 */
function ToastItem({ t }: { t: Toast }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    gsap.fromTo(
      ref.current,
      { opacity: 0, y: -14 },
      { opacity: 1, y: 0, duration: 0.3, ease: 'power3.out' },
    );
  }, []);
  return (
    <div
      ref={ref}
      className={clsx(
        'pointer-events-auto max-w-[calc(100vw-2rem)] rounded-xl border bg-paper/95 px-5 py-3 text-sm shadow-float backdrop-blur',
        TONE_CLASS[t.tone],
      )}
    >
      {t.message}
    </div>
  );
}

/**
 * Portal the queue into document.body so routed containers cannot clip it.
 * An empty queue renders nothing; expiry remains controlled by the UI store.
 */
export function ToastHost() {
  const toasts = useUI((s) => s.toasts);
  if (toasts.length === 0) return null;
  return createPortal(
    <div className="pointer-events-none fixed left-1/2 top-5 z-[100] flex -translate-x-1/2 flex-col items-center gap-2">
      {toasts.map((t) => (
        <ToastItem key={t.id} t={t} />
      ))}
    </div>,
    document.body,
  );
}
