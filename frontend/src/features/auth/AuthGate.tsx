import { createContext, ReactNode, useContext, useEffect, useState } from 'react';
import { fetchCurrentUser, logout } from '../../api/auth';

type AuthState = 'loading' | 'authed' | 'denied';

const GOOGLE_START_URL = '/api/auth/google/start';

// The signed-in identity and the logout action, for views that render user
// actions (App places them in the top navbar). Provided only in the authed
// state, which is the only state in which children render at all.
type AuthContextValue = { email: string; logout: () => Promise<void> };
const AuthContext = createContext<AuthContextValue | null>(null);

export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext);
  if (!value) {
    throw new Error('useAuth must be used inside AuthGate');
  }
  return value;
}

export function AuthGate({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>('loading');
  const [denied, setDenied] = useState(false);
  const [currentUser, setCurrentUser] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const deniedByAllowlist = new URLSearchParams(window.location.search).get('error') === 'denied';
    setDenied(deniedByAllowlist);
    if (deniedByAllowlist) {
      window.history.replaceState({}, '', window.location.pathname);
    }
    fetchCurrentUser()
      .then((user) => {
        if (cancelled) return;
        setCurrentUser(user ? user.email : null);
        setState(user !== null ? 'authed' : 'denied');
      })
      .catch(() => {
        if (!cancelled) setState('denied');
      });
    return () => {
      cancelled = true;
    };
  }, []);

  async function handleLogout() {
    await logout();
    setCurrentUser(null);
    setState('denied');
  }

  if (state === 'loading') {
    return (
      <div className="min-h-screen bg-shell flex items-center justify-center text-muted">
        Checking access...
      </div>
    );
  }

  if (state === 'denied') {
    return (
      <div className="min-h-screen bg-shell flex items-center justify-center px-4">
        <div className="w-full max-w-sm bg-canvas rounded-xl border border-steel/30 p-8">
          <div className="flex items-center gap-2.5 mb-4">
            <span className="w-2.5 h-2.5 rounded-full bg-brand-500" aria-hidden />
            <h1 className="text-lg font-semibold tracking-tight text-ink">Access restricted</h1>
          </div>
          <p className="text-sm text-muted mb-6">
            This meeting assistant is available to authorized users only.
            Sign in with the Google account allowed by your organization.
          </p>
          {denied && (
            <p className="text-sm text-coral-700 mb-4">
              Your Google account is not on the authorized list.
            </p>
          )}
          <a
            href={GOOGLE_START_URL}
            className="flex items-center justify-center gap-2 w-full px-3 py-2 text-sm font-semibold text-ink bg-brand-500 rounded-lg hover:bg-brand-600 hover:text-canvas transition-colors"
          >
            Continue with Google
          </a>
        </div>
      </div>
    );
  }

  return (
    <AuthContext.Provider value={{ email: currentUser ?? '', logout: handleLogout }}>
      {children}
    </AuthContext.Provider>
  );
}