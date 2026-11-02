/**
 * Route title and account actions for the authenticated shell.
 * Nested resource URLs inherit their top-level section title, while admin
 * sections use explicit labels. Logout clears authentication through the store
 * before navigating to the login page.
 */
import { useLocation, useNavigate } from 'react-router-dom';
import { useAuth } from '@/stores/auth';
import { Badge } from '@/components/ui/primitives';
import { Icon } from '@/components/ui/Icon';

const TITLES: Record<string, string> = {
  chat: 'Chat', agents: 'Agents', memories: 'Memories', kbs: 'Knowledge bases', workflows: 'Workflows',
};

export function Topbar() {
  const user = useAuth((s) => s.user);
  const logout = useAuth((s) => s.logout);
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const pageTitle = pathname.startsWith('/admin/models') ? 'Models'
    : pathname.startsWith('/admin/integrations') ? 'Integrations'
    : TITLES[pathname.split('/')[1]] ?? 'Workspace';

  /**
   * Wait for the store logout action before navigation; its finally block
   * clears local credentials even when remote revocation fails.
   */
  const handleLogout = async () => {
    await logout();
    navigate('/login');
  };

  return (
    <header className="app-topbar">
      <div className="flex min-w-0 items-center gap-3 text-xs">
        <span className="hidden font-medium tracking-tight text-ash sm:inline">SupaAgent</span>
        <span aria-hidden="true" className="hidden text-smoke sm:inline">/</span>
        <span className="truncate font-medium text-mist">{pageTitle}</span>
      </div>
      {user && <div className="flex shrink-0 items-center gap-2.5 sm:gap-3">
        <span className="hidden sm:inline"><Badge tone={user.role === 'admin' ? 'lime' : 'gray'}>{user.role}</Badge></span>
        <div className="flex items-center gap-2.5">
          <span aria-hidden="true" className="flex h-8 w-8 items-center justify-center rounded-[11px] border border-white bg-gradient-to-br from-lime/15 to-white text-xs font-semibold uppercase text-lime shadow-panel">{user.username.slice(0, 1)}</span>
          <span className="hidden max-w-32 truncate text-sm font-medium text-mist sm:inline">{user.username}</span>
        </div>
        <span aria-hidden="true" className="mx-1 h-4 w-px bg-smoke" />
        <button onClick={handleLogout} title="Sign out" className="flex items-center gap-1.5 rounded-lg px-2 py-2 text-xs text-fog transition-colors hover:bg-coral/5 hover:text-coral focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-lime">
          <Icon name="logout" className="h-4 w-4" /><span className="hidden sm:inline">Sign out</span><span className="sr-only sm:hidden">Sign out</span>
        </button>
      </div>}
    </header>
  );
}
