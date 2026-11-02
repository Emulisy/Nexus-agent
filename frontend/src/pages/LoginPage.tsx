/**
 * Login and registration share a controlled form and authentication actions.
 * Registration is followed by the same login request as an existing account.
 * A restored authenticated user is redirected to chat; request errors stay in
 * the form context through a toast and validation animation.
 */
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Navigate, useNavigate } from 'react-router-dom';
import { gsap, shake } from '@/lib/gsap';
import { useAuth } from '@/stores/auth';
import { toast } from '@/stores/ui';
import { errMsg } from '@/lib/request';
import { Button, Input } from '@/components/ui/primitives';
import { Brand } from '@/components/ui/Brand';
import { Icon } from '@/components/ui/Icon';
import '@/styles/auth.css';

type Mode = 'login' | 'register';

export function LoginPage() {
  const navigate = useNavigate();
  const { user, bootstrapped, login, register } = useAuth();

  const cardRef = useRef<HTMLDivElement>(null);
  const [mode, setMode] = useState<Mode>('login');
  const [username, setUsername] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [loading, setLoading] = useState(false);

  /**
   * Scope the initial card animation to this mount. Reverting its GSAP context
   * removes inline animation state during unmount and development remount checks.
   */
  useEffect(() => {
    const ctx = gsap.context(() => {
      gsap.fromTo(
        cardRef.current,
        { opacity: 0, y: 24, scale: 0.98 },
        { opacity: 1, y: 0, scale: 1, duration: 0.55, ease: 'power3.out' },
      );
    });
    return () => ctx.revert();
  }, []);

  if (bootstrapped && user) return <Navigate to="/chat" replace />;

  /**
   * Validate required credentials, register if requested, then establish the
   * login session before navigation. Pending state is cleared for both success
   * and failure so a rejected request does not leave the form disabled.
   */
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!username || !password) {
      if (cardRef.current) shake(cardRef.current);
      toast.err('Please enter your username and password');
      return;
    }
    setLoading(true);
    try {
      if (mode === 'register') {
        await register({ username, email, password });
        toast.ok('Account created. You are now signed in.');
      }
      await login({ username, password });
      navigate('/chat');
    } catch (err) {
      if (cardRef.current) shake(cardRef.current);
      toast.err(errMsg(err));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="auth-page">
      <header className="auth-header">
        <Brand />
        <span className="auth-header-note"><span />YOUR AI WORKSPACE</span>
      </header>

      <main className="auth-main">
        <section className="auth-intro" aria-label="SupaAgent AI workspace">
          <p className="auth-eyebrow"><span />INTELLIGENCE, IN SYNC.</p>
          <h2 className="auth-editorial">Make room for ideas.<br /><span>Put AI to work.</span></h2>
          <p className="auth-intro-copy">Connect conversations, knowledge, and workflows.<br />Give your ideas room to grow in one focused space.</p>

          <div className="auth-orbit" aria-hidden="true">
            <div className="auth-orbit-grid" />
            <div className="auth-orbit-ring auth-orbit-ring-outer" />
            <div className="auth-orbit-ring auth-orbit-ring-middle" />
            <div className="auth-orbit-ring auth-orbit-ring-inner" />
            <div className="auth-orbit-cross auth-orbit-cross-x" />
            <div className="auth-orbit-cross auth-orbit-cross-y" />
            <span className="auth-orbit-node auth-orbit-node-one"><Icon name="knowledge" /></span>
            <span className="auth-orbit-node auth-orbit-node-two"><Icon name="workflow" /></span>
            <span className="auth-orbit-node auth-orbit-node-three"><Icon name="chat" /></span>
            <div className="auth-orbit-core"><Icon name="agent" /></div>
            <span className="auth-orbit-label">CONNECTED INTELLIGENCE</span>
            <span className="auth-orbit-coordinate">S / A — 01</span>
          </div>

          <div className="auth-capabilities">
            <span><Icon name="chat" />AI chat</span>
            <span><Icon name="knowledge" />Shared knowledge</span>
            <span><Icon name="workflow" />Workflow automation</span>
          </div>
        </section>

        <section className="auth-form-area" aria-label={mode === 'login' ? 'Sign in' : 'Register'}>
          <div ref={cardRef} className="auth-card">
            <div className="auth-card-corner" aria-hidden="true"><span /><span /><span /></div>
            <div className="mb-8">
              <p className="auth-card-eyebrow">{mode === 'login' ? 'WELCOME BACK' : 'GET STARTED'}</p>
              <h1 className="auth-card-title">{mode === 'login' ? 'Welcome back' : 'Start something new'}</h1>
              <p className="mt-3 text-sm leading-relaxed text-fog">{mode === 'login' ? 'Sign in to SupaAgent and pick up where you left off.' : 'Create an account and bring your ideas to life.'}</p>
            </div>

          <form onSubmit={submit} className="space-y-5">
            <div>
              <label htmlFor="auth-username" className="field-label">Username</label>
              <Input
                id="auth-username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="Enter your username"
                autoComplete="username"
                className="auth-input"
              />
            </div>
            {mode === 'register' && (
              <div>
                <label htmlFor="auth-email" className="field-label">Email</label>
                <Input
                  id="auth-email"
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder="you@example.com"
                  autoComplete="email"
                  className="auth-input"
                />
              </div>
            )}
            <div>
              <label htmlFor="auth-password" className="field-label">Password</label>
              <Input
                id="auth-password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="••••••••"
                autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
                className="auth-input"
              />
            </div>

            <Button type="submit" loading={loading} className="auth-submit mt-2 w-full">
              {mode === 'login' ? 'Sign in' : 'Create account'}<Icon name="arrow" className="h-4 w-4" />
            </Button>
          </form>

          <div className="auth-mode-switch">
            {mode === 'login' ? 'New to SupaAgent?' : 'Already have an account?'}
            <button
              className="ml-1.5 rounded font-medium text-lime transition-colors hover:text-bone focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-lime"
              onClick={() => setMode(mode === 'login' ? 'register' : 'login')}
            >
              {mode === 'login' ? 'Sign up' : 'Sign in'}
            </button>
          </div>
          </div>
          <p className="auth-form-caption">A CLEAR SPACE FOR YOUR NEXT IDEA.</p>
        </section>
      </main>
      <footer className="auth-footer"><span>SupaAgent · Give your ideas room to grow</span><span>DESIGNED FOR FOCUS.</span></footer>
    </div>
  );
}
