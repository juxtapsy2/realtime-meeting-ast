import { useState } from 'react';
import { Meeting } from './types';
import { AuthGate } from './features/auth/AuthGate';
import { MeetingList } from './features/meetings/MeetingList';
import { MeetingRoom } from './features/meetings/MeetingRoom';
import { MeetingHistory } from './features/meetings/MeetingHistory';

function App() {
  const [selectedMeeting, setSelectedMeeting] = useState<Meeting | null>(null);

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
