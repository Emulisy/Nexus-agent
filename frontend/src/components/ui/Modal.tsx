/**
 * Portal-based dialog used by editors, inspectors, and confirmation flows.
 * The overlay and panel animate on open; Escape and backdrop clicks request
 * closure through onClose. Animation and keyboard handling have separate
 * lifecycles. Callers own pending-operation state and the open flag.
 */
import { useEffect, useRef, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { gsap } from '@/lib/gsap';

interface ModalProps {
  open: boolean;
  title: string;
  onClose: () => void;
  children: ReactNode;
  /** Optional caller-owned action area, separated from scrollable content visually. */
  footer?: ReactNode;
  /** Tailwind max-width class supplied by inspectors and larger editors. */
  width?: string;
}

export function Modal({ open, title, onClose, children, footer, width = 'max-w-lg' }: ModalProps) {
  const overlayRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);

  /**
   * Animate only when opening. Parent renders can replace onClose while typing
   * and must not reset the entry tweens. Cleanup reverses their inline styles.
   */
  useEffect(() => {
    if (!open) return;
    const ctx = gsap.context(() => {
      gsap.fromTo(overlayRef.current, { opacity: 0 }, { opacity: 1, duration: 0.2 });
      gsap.fromTo(
        panelRef.current,
        { opacity: 0, y: 16, scale: 0.97 },
        { opacity: 1, y: 0, scale: 1, duration: 0.3, ease: 'power3.out' },
      );
    });
    return () => ctx.revert();
  }, [open]);

  useEffect(() => {
    if (!open) return;
    /**
     * Escape requests closure through the same callback as the close button.
     * This dialog provides semantics but does not implement a focus trap or restoration.
     */
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  if (!open) return null;

  return createPortal(
    <div
      ref={overlayRef}
      className="fixed inset-0 z-50 flex items-center justify-center bg-bone/15 p-4 backdrop-blur-md"
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <div ref={panelRef} role="dialog" aria-modal="true" aria-label={title} className={`panel modal-panel w-full ${width} max-h-[85vh] overflow-y-auto p-6 shadow-float`}>
        <div className="mb-5 flex items-center justify-between gap-3">
          <h2 className="text-base font-semibold text-bone">{title}</h2>
          <button
            className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-ash transition-colors hover:bg-graphite hover:text-bone"
            onClick={onClose}
            aria-label="Close"
          >
            ✕
          </button>
        </div>
        {children}
        {footer && <div className="mt-6 flex justify-end gap-2 border-t border-smoke/70 pt-5">{footer}</div>}
      </div>
    </div>,
    document.body,
  );
}
