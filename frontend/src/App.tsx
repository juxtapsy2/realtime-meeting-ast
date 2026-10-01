import { useEffect, useState } from 'react';
import { Meeting } from './types';
import { fetchCurrentUser } from './api/auth';
import { AdminPage } from './features/admin/AdminPage';
import { AuthGate } from './features/auth/AuthGate';
import { MeetingList } from './features/meetings/MeetingList';
import { MeetingRoom } from './features/meetings/MeetingRoom';
import { MeetingHistory } from './features/meetings/MeetingHistory';

function App() {
  const [selectedMeeting, setSelectedMeeting] = useState<Meeting | null>(null);
  const [showAdmin, setShowAdmin] = useState(false);
  const [isAdmin, setIsAdmin] = useState(false);
  const [isSuperAdmin, setIsSuperAdmin] = useState(false);

  useEffect(() => {
    fetchCurrentUser().then((user) => {
      setIsAdmin(user?.isAdmin ?? false);
      setIsSuperAdmin(user?.isSuperAdmin ?? false);
    });
  }, []);

  if (showAdmin) {
    return <AdminPage onBack={() => setShowAdmin(false)} isSuperAdmin={isSuperAdmin} />;
  }

  if (selectedMeeting) {
    if (selectedMeeting.status === 'completed') {
      return (
        <MeetingHistory
          meeting={selectedMeeting}
          onBack={() => setSelectedMeeting(null)}
        />
      );
    }
    return (
      <MeetingRoom
        meeting={selectedMeeting}
        onBack={() => setSelectedMeeting(null)}
        onMeetingEnded={() => setSelectedMeeting((prev) =>
          prev ? { ...prev, status: 'completed' } : prev
        )}
      />
    );
  }

  return (
    <div className="min-h-screen bg-gray-50">
      <div className="max-w-4xl mx-auto px-6 pt-6 flex items-center justify-between">
        <h1 className="text-xl font-bold text-gray-900">Meeting Assistant</h1>
        {isAdmin && (
          <button
            onClick={() => setShowAdmin(true)}
            className="px-3 py-1.5 text-sm rounded-lg border border-gray-300 text-gray-600 hover:bg-gray-100"
          >
            Admin
          </button>
        )}
      </div>
      <MeetingList onSelectMeeting={setSelectedMeeting} />
    </div>
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
