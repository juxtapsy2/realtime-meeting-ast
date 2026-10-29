import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { Meeting } from './types';
import { fetchCurrentUser } from './api/auth';
import { Navbar } from './components/Navbar';
import { AdminPage } from './features/admin/AdminPage';
import { AuthGate, useAuth } from './features/auth/AuthGate';
import { MeetingList } from './features/meetings/MeetingList';
import { MeetingRoom } from './features/meetings/MeetingRoom';
import { MeetingHistory } from './features/meetings/MeetingHistory';
import { MyProviderSettings } from './features/providers/MyProviderSettings';

function App() {
  const [selectedMeeting, setSelectedMeeting] = useState<Meeting | null>(null);
  const [showAdmin, setShowAdmin] = useState(false);
  const [showProviders, setShowProviders] = useState(false);
  const [isAdmin, setIsAdmin] = useState(false);
  const [isSuperAdmin, setIsSuperAdmin] = useState(false);
  const { email, logout } = useAuth();

  useEffect(() => {
    fetchCurrentUser().then((user) => {
      setIsAdmin(user?.isAdmin ?? false);
      setIsSuperAdmin(user?.isSuperAdmin ?? false);
    });
  }, []);

  // The meeting room owns the whole top edge (connection status,
  // Start/Pause/End, mic controls), so the user navbar steps aside there
  // instead of overlapping its action buttons. Every other view sits under it.
  const inMeetingRoom = selectedMeeting !== null && selectedMeeting.status !== 'completed';

  // Every view fills exactly one screen: the navbar (when shown) and the view
  // share the shell's height, so the window itself never scrolls and each
  // view scrolls only where it intends to (the table, its panels).
  const shell = (view: ReactNode) => (
    <div className="h-screen flex flex-col bg-shell">
      {!inMeetingRoom && <Navbar email={email} onLogout={() => void logout()} />}
      {view}
    </div>
  );

  if (showAdmin) {
    return shell(<AdminPage onBack={() => setShowAdmin(false)} isSuperAdmin={isSuperAdmin} />);
  }

  if (showProviders) {
    return shell(<MyProviderSettings onBack={() => setShowProviders(false)} />);
  }

  if (selectedMeeting) {
    if (selectedMeeting.status === 'completed') {
      return shell(
        <MeetingHistory meeting={selectedMeeting} onBack={() => setSelectedMeeting(null)} />,
      );
    }
    return shell(
      <MeetingRoom
        meeting={selectedMeeting}
        onBack={() => setSelectedMeeting(null)}
        onMeetingEnded={() => setSelectedMeeting((prev) =>
          prev ? { ...prev, status: 'completed' } : prev
        )}
      />,
    );
  }

  return shell(
    <div className="flex-1 min-h-0 flex flex-col bg-shell">
      <div className="shrink-0 max-w-7xl w-full mx-auto px-6 pt-6 flex items-center justify-between">
        <div className="flex items-center gap-2.5">
          <span className="w-2.5 h-2.5 rounded-full bg-brand-500" aria-hidden />
          <h1 className="text-xl font-bold tracking-tight text-ink">Meeting Assistant</h1>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setShowProviders(true)}
            className="px-3 py-1.5 text-sm rounded-lg border border-steel/50 text-ink hover:bg-canvas transition-colors"
          >
            Provider settings
          </button>
          {isAdmin && (
            <button
              onClick={() => setShowAdmin(true)}
              className="px-3 py-1.5 text-sm rounded-lg border border-steel/50 text-ink hover:bg-canvas transition-colors"
            >
              Admin
            </button>
          )}
        </div>
      </div>
      <MeetingList onSelectMeeting={setSelectedMeeting} />
    </div>,
  );
}

function AppRoot() {
  return (
    <AuthGate>
      <App />
    </AuthGate>
  );
}

export default AppRoot;
