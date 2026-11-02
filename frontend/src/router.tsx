/**
 * Route definitions and client-side access guards.
 * Protected routes wait for session restoration, then require a current user.
 * Admin pages add a role check; backend authorization remains authoritative.
 */
import { Navigate, Route, Routes, useLocation } from 'react-router-dom';
import type { ReactNode } from 'react';
import { useAuth } from '@/stores/auth';
import { AppLayout } from '@/components/layout/AppLayout';
import { Spinner } from '@/components/ui/primitives';
import { LoginPage } from '@/pages/LoginPage';
import { ChatPage } from '@/pages/chat/ChatPage';
import { AgentsPage } from '@/pages/agents/AgentsPage';
import { MemoriesPage } from '@/pages/memories/MemoriesPage';
import { KnowledgeBasesPage } from '@/pages/kbs/KnowledgeBasesPage';
import { KBDetailPage } from '@/pages/kbs/KBDetailPage';
import { WorkflowsPage } from '@/pages/workflows/WorkflowsPage';
import { WorkflowRunsPage } from '@/pages/workflows/WorkflowRunsPage';
import { ModelsAdminPage } from '@/pages/admin/ModelsAdminPage';
import { IntegrationsPage } from '@/pages/admin/IntegrationsPage';
import { NotFoundPage } from '@/pages/NotFoundPage';

/**
 * Defer authentication decisions until bootstrap settles so a page reload does
 * not redirect a recoverable session prematurely. The attempted pathname is
 * recorded in navigation state, although the current login page returns to chat.
 */
function RequireAuth({ children }: { children: ReactNode }) {
  const { user, bootstrapped } = useAuth();
  const location = useLocation();
  if (!bootstrapped) return <BootScreen />;
  if (!user) return <Navigate to="/login" state={{ from: location.pathname }} replace />;
  return <>{children}</>;
}

/**
 * Hide admin pages from non-admin users after authentication has succeeded.
 * This is a navigation guard, not a substitute for server-side authorization.
 */
function RequireAdmin({ children }: { children: ReactNode }) {
  const user = useAuth((s) => s.user);
  if (user?.role !== 'admin') return <Navigate to="/chat" replace />;
  return <>{children}</>;
}

/** Startup placeholder shown while the persisted refresh token is being checked. */
function BootScreen() {
  return (
    <div className="flex h-full items-center justify-center gap-3 text-fog">
      <Spinner />
      <span className="text-sm">Restoring your session…</span>
    </div>
  );
}

/**
 * Keep the authenticated shell mounted around feature pages. The login route
 * lives outside the shell, and the wildcard uses the authenticated fallback view.
 */
export function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />

      <Route
        element={
          <RequireAuth>
            <AppLayout />
          </RequireAuth>
        }
      >
        <Route path="/chat" element={<ChatPage />} />
        <Route path="/agents" element={<AgentsPage />} />
        <Route path="/memories" element={<MemoriesPage />} />
        <Route path="/kbs" element={<KnowledgeBasesPage />} />
        <Route path="/kbs/:id" element={<KBDetailPage />} />
        <Route path="/workflows" element={<WorkflowsPage />} />
        <Route path="/workflows/runs" element={<WorkflowRunsPage />} />
        <Route
          path="/admin/models"
          element={
            <RequireAdmin>
              <ModelsAdminPage />
            </RequireAdmin>
          }
        />
        <Route
          path="/admin/integrations"
          element={
            <RequireAdmin>
              <IntegrationsPage />
            </RequireAdmin>
          }
        />
        <Route path="/" element={<Navigate to="/chat" replace />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  );
}
