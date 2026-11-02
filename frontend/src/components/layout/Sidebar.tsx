/**
 * Workspace navigation driven by a single route metadata list.
 * Admin links are hidden for ordinary users, and NavLink provides active-route
 * state. Labels remain available through accessible names when the responsive
 * layout hides their visible text.
 */
import { NavLink } from 'react-router-dom';
import { clsx } from 'clsx';
import { useAuth } from '@/stores/auth';
import { Brand } from '@/components/ui/Brand';
import { Icon, type IconName } from '@/components/ui/Icon';

/**
 * Navigation metadata shared by ordinary/admin sections. Hidden visible labels
 * still have title/aria-label text in the compact layout.
 */
interface NavItem {
  to: string;
  label: string;
  icon: IconName;
  isAdmin?: boolean;
}

const NAV: NavItem[] = [
  { to: '/chat', label: 'Chat', icon: 'chat' },
  { to: '/agents', label: 'Agents', icon: 'agent' },
  { to: '/memories', label: 'Memories', icon: 'memory' },
  { to: '/kbs', label: 'Knowledge bases', icon: 'knowledge' },
  { to: '/workflows', label: 'Workflows', icon: 'workflow' },
  { to: '/admin/models', label: 'Models', icon: 'models', isAdmin: true },
  { to: '/admin/integrations', label: 'Integrations', icon: 'integrations', isAdmin: true },
];

export function Sidebar() {
  const user = useAuth((s) => s.user);

  /**
   * Let NavLink compute active state from the current URL rather than keeping
   * a second selected-route state in the sidebar.
   */
  const renderItem = (n: NavItem) => (
    <NavLink
      key={n.to}
      to={n.to}
      title={n.label}
      aria-label={n.label}
      className={({ isActive }) => clsx(
        'nav-link group',
        isActive && 'nav-link-active',
      )}
    >
      {({ isActive }) => <>
        <Icon name={n.icon} className="h-[19px] w-[19px] shrink-0" />
        <span className="hidden md:inline">{n.label}</span>
        {isActive && <span aria-hidden="true" className="ml-auto hidden text-lime/65 md:block"><Icon name="arrow" className="h-3.5 w-3.5" /></span>}
      </>}
    </NavLink>
  );

  return (
    <aside className="app-sidebar">
      <div className="flex h-[84px] shrink-0 items-center justify-center md:justify-start md:px-5">
        <Brand compact />
      </div>
      <nav aria-label="Main navigation" className="flex-1 overflow-y-auto px-2 pt-5 md:px-3">
        <div className="section-title mb-4 hidden px-3 md:block">Workspace</div>
        <div className="space-y-1.5">{NAV.filter((n) => !n.isAdmin).map(renderItem)}</div>
        {user?.role === 'admin' && <div className="mt-7 border-t border-smoke/70 pt-5">
          <div className="section-title mb-4 hidden px-3 md:block">Management</div>
          <div className="space-y-1.5">{NAV.filter((n) => n.isAdmin).map(renderItem)}</div>
        </div>}
      </nav>
      <div className="mx-3 mb-5 mt-5 border-t border-smoke/70 pt-4 md:mx-5">
        <div className="hidden items-center justify-between text-[11px] text-ash md:flex"><span className="font-medium tracking-tight">SupaAgent</span><span className="rounded border border-smoke/70 bg-white/60 px-1.5 py-0.5 font-mono text-[9px]">v1.0</span></div>
        <span className="block text-center text-[9px] text-ash md:hidden">v1.0</span>
      </div>
    </aside>
  );
}
