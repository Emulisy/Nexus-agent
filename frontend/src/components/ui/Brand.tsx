/**
 * Shared SVG brand mark and wordmark for the shell, login page, and chat.
 * The mark uses currentColor so callers control tone and size through classes;
 * compact mode changes wordmark visibility for constrained navigation layouts.
 */
import { clsx } from 'clsx';

/**
 * Decorative mark hidden from assistive technology; surrounding text or the
 * parent control provides the accessible name.
 */
export function BrandMark({ className }: { className?: string }) {
  return (
    <span className={clsx('brand-mark', className)} aria-hidden="true">
      <svg viewBox="0 0 32 32" fill="none" className="h-6 w-6">
        <path d="M23 8H13a4 4 0 0 0 0 8h6a4 4 0 0 1 0 8H9" stroke="currentColor" strokeWidth="3.2" strokeLinecap="round" />
      </svg>
    </span>
  );
}

/** Show the shared wordmark, hiding its text below md when compact is enabled. */
export function Brand({ compact = false, className }: { compact?: boolean; className?: string }) {
  return (
    <div className={clsx('flex items-center gap-3', className)}>
      <BrandMark />
      <span className={clsx('whitespace-nowrap text-[19px] font-semibold tracking-[-0.055em] text-bone', compact && 'hidden md:inline')}>
        Supa<span className="font-normal text-mist">Agent</span>
      </span>
    </div>
  );
}
