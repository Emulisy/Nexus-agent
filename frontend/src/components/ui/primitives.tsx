/**
 * Small UI components that share the global component classes.
 * Native input/button props are forwarded so pages retain HTML behavior and
 * accessibility attributes. StatusBadge translates backend status metadata,
 * while structural components provide consistent spacing and empty states.
 */
import { clsx } from 'clsx';
import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode, SelectHTMLAttributes, TextareaHTMLAttributes } from 'react';
import { STATUS_LABEL } from '@/lib/format';

type ButtonVariant = 'primary' | 'ghost' | 'danger';

/** Shared button appearance and pending state layered over native button props. */
interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  loading?: boolean;
}

const VARIANT_CLASS: Record<ButtonVariant, string> = {
  primary: 'btn-primary',
  ghost: 'btn-ghost',
  danger: 'btn-danger',
};

/**
 * Forward native semantics while disabling the control for explicit disabled
 * or loading state. The optional spinner leaves the action label visible.
 */
export function Button({ variant = 'primary', loading, className, children, disabled, ...rest }: ButtonProps) {
  return (
    <button
      className={clsx(VARIANT_CLASS[variant], className)}
      disabled={disabled || loading}
      {...rest}
    >
      {loading && <Spinner className="h-3.5 w-3.5" />}
      {children}
    </button>
  );
}

/** Apply the shared field surface while retaining the native input contract. */
export function Input({ className, ...rest }: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={clsx('field', className)} {...rest} />;
}

/** Use the field surface and delegate controlled value, sizing, and labels to callers. */
export function Textarea({ className, ...rest }: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea className={clsx('field resize-none', className)} {...rest} />;
}

/** Keep native select behavior while applying the same field classes as text inputs. */
export function Select({ className, children, ...rest }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select className={clsx('field appearance-none', className)} {...rest}>
      {children}
    </select>
  );
}

type BadgeTone = 'lime' | 'teal' | 'coral' | 'pulse' | 'violet' | 'gray';

const TONE_CLASS: Record<BadgeTone, string> = {
  lime: 'border-lime/15 text-lime bg-lime/5',
  teal: 'border-teal/15 text-teal bg-teal/5',
  coral: 'border-coral/15 text-coral bg-coral/5',
  pulse: 'border-pulse/15 text-pulse bg-pulse/5',
  violet: 'border-violet/15 text-violet bg-violet/5',
  gray: 'border-smoke/70 text-fog bg-graphite/70',
};

/** Render compact noninteractive metadata with semantic color selection. */
export function Badge({ tone = 'gray', children, className }: { tone?: BadgeTone; children: ReactNode; className?: string }) {
  return (
    <span
      className={clsx(
        'inline-flex items-center whitespace-nowrap rounded-full border px-2.5 py-0.5 text-[11px] font-medium leading-5',
        TONE_CLASS[tone],
        className,
      )}
    >
      {children}
    </span>
  );
}

const TONE_OF: Record<string, BadgeTone> = { ok: 'pulse', warn: 'lime', err: 'coral', info: 'teal' };

/**
 * Translate a known backend status to its localized label/tone. Unknown
 * statuses remain visible as their original string instead of disappearing.
 */
export function StatusBadge({ status }: { status: string }) {
  const info = STATUS_LABEL[status] ?? { text: status, tone: 'info' as const };
  return <Badge tone={TONE_OF[info.tone] ?? 'gray'}>{info.text}</Badge>;
}

/**
 * Visual loading indicator with an accessible loading label; callers supply
 * size overrides while the default dimensions fit inline actions.
 */
export function Spinner({ className }: { className?: string }) {
  return (
    <span
      className={clsx(
        'inline-block animate-spin rounded-full border-2 border-current border-t-transparent',
        className ?? 'h-4 w-4',
      )}
      aria-label="Loading"
    />
  );
}

/**
 * Show an empty-result explanation and an optional contextual action.
 * Icons are optional so the same container works for empty lists and invalid resources.
 */
export function EmptyState({ icon, title, hint, action }: { icon?: ReactNode; title: string; hint?: string; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 px-4 py-20 text-center">
      {icon && <div className="empty-state-icon">{icon}</div>}
      <p className="text-base font-medium text-bone">{title}</p>
      {hint && <p className="max-w-sm text-sm leading-6 text-ash">{hint}</p>}
      {action && <div className="mt-3">{action}</div>}
    </div>
  );
}

/**
 * Shared page title/description/action layout. The action region wraps on
 * smaller screens rather than constraining the title width.
 */
export function PageHeader({ title, desc, action }: { title: string; desc?: string; action?: ReactNode }) {
  return (
    <div className="page-header">
      <div className="min-w-0">
        <div className="page-header-accent" aria-hidden="true" />
        <h1>{title}</h1>
        {desc && <p className="mt-2 max-w-2xl text-sm leading-6 text-fog">{desc}</p>}
      </div>
      {action && <div className="flex shrink-0 items-center gap-2 sm:pt-5">{action}</div>}
    </div>
  );
}

/**
 * Visual panel with an optional click callback and hover affordance.
 * It remains a div; callers choose any additional keyboard/link semantics.
 */
export function Card({ className, children, onClick }: { className?: string; children: ReactNode; onClick?: () => void }) {
  return (
    <div
      className={clsx(
        'panel p-5 transition-all duration-200',
        onClick && 'cursor-pointer hover:border-lime/30 hover:shadow-float',
        className,
      )}
      onClick={onClick}
    >
      {children}
    </div>
  );
}
