import { useState } from 'react';
import { Meeting } from './types';
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
      />
    );
  }

  return (
    <div className="min-h-screen bg-gray-50">
      <MeetingList onSelectMeeting={setSelectedMeeting} />
    </div>
  );
}

export default App;
