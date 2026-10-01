import { ReactNode, useEffect, useState } from 'react';
import { fetchCurrentUser, logout } from '../../api/auth';

type AuthState = 'loading' | 'authed' | 'denied';

const GOOGLE_START_URL = '/api/auth/google/start';

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
        setCurrentUser(user);
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
      <div className="min-h-screen bg-gray-50 flex items-center justify-center text-gray-400">
        Checking access...
      </div>
    );
  }

  if (state === 'denied') {
    return (
      <div className="min-h-screen bg-gray-50 flex items-center justify-center px-4">
        <div className="w-full max-w-sm bg-white rounded-xl shadow-sm border border-gray-200 p-8">
          <h1 className="text-lg font-medium text-gray-900 mb-1">Access restricted</h1>
          <p className="text-sm text-gray-500 mb-6">
            This meeting assistant is available to authorized users only.
            Sign in with the Google account allowed by your organization.
          </p>
          {denied && (
            <p className="text-sm text-red-600 mb-4">
              Your Google account is not on the authorized list.
            </p>
          )}
          <a
            href={GOOGLE_START_URL}
            className="flex items-center justify-center gap-2 w-full px-3 py-2 text-sm font-medium text-white bg-indigo-600 rounded-lg hover:bg-indigo-700"
          >
            Continue with Google
          </a>
        </div>
      </div>
    );
  }

  return (
    <>
      {currentUser && (
        <div className="fixed top-3 right-3 z-50 flex items-center gap-3 text-xs">
          <span className="text-gray-400">{currentUser}</span>
          <button
            onClick={handleLogout}
            className="px-2.5 py-1 rounded-lg border border-gray-300 text-gray-600 hover:bg-gray-100"
          >
            Sign out
          </button>
        </div>
      )}
      {children}
    </>
  );
}