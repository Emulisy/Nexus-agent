/**
 * Application root for session restoration, routing, and global notifications.
 * Route guards read auth.bootstrapped so protected pages wait for the initial
 * refresh-token exchange before deciding whether to redirect.
 */
import { useEffect } from 'react';
import { useAuth } from '@/stores/auth';
import { AppRoutes } from '@/router';
import { ToastHost } from '@/components/ui/ToastHost';

export default function App() {
  const bootstrap = useAuth((s) => s.bootstrap);

  useEffect(() => {
    void bootstrap();
  }, [bootstrap]);

  return (
    <>
      <AppRoutes />
      <ToastHost />
    </>
  );
}
