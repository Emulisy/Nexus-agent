/**
 * Authenticated workspace shell with navigation, account controls, and the
 * nested route outlet. The shell survives child-route changes; its content
 * container receives the shared entry animation when the pathname changes.
 */
import { useEffect, useRef } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { Sidebar } from './Sidebar';
import { Topbar } from './Topbar';
import { pageEnter } from '@/lib/gsap';

export function AppLayout() {
  const location = useLocation();
  const mainRef = useRef<HTMLDivElement>(null);

  /**
   * Reanimate the existing content container on pathname changes. Query-string
   * changes alone do not trigger another page entrance.
   */
  useEffect(() => {
    if (mainRef.current) pageEnter(mainRef.current);
  }, [location.pathname]);

  return (
    <div className="app-canvas">
      <div className="app-shell">
        <Sidebar />
        <div className="flex min-w-0 flex-1 flex-col">
          <Topbar />
          <main ref={mainRef} className="app-content">
            <Outlet />
          </main>
        </div>
      </div>
    </div>
  );
}
